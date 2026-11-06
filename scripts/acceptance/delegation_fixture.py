#!/usr/bin/env python3
"""Start a fresh local PG/Valkey/server fixture and run native delegation checks.

Requires Docker, docker-init, Go (go.mod version), Python and requirements.txt.
Creates only labelled containers and files under a new --root. Credentials are
random, local, and stored mode 0600; never print or commit fixture-private.json.
No existing config, database, cloud account, provider or workspace is reused.
Resources remain for inspection. `stop` only stops this fixture's owned resources.
"""
import argparse
import base64
from collections import Counter
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import signal
import socket
import string
import subprocess
import time
import tomllib
import urllib.error
import urllib.request
import uuid

REPO = Path(__file__).resolve().parents[2]
LABEL = "memoh.delegation.acceptance"


class FixtureCommandError(RuntimeError):
    pass


def command(args, **kwargs):
    result = subprocess.run(args, text=True, capture_output=True, **kwargs)
    if result.returncode:
        # Container bootstrap arguments contain generated private credentials.
        # Never include argv or captured diagnostics in a public traceback.
        raise FixtureCommandError("fixture command failed: " + args[0] + " (private arguments withheld)")
    return result.stdout.strip()


def private_json(path, value):
    path.write_text(json.dumps(value, indent=2))
    path.chmod(0o600)


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def inspect_container(state, name):
    info = json.loads(command(["docker", "inspect", name]))[0]
    if info["Config"]["Labels"].get(LABEL) != state["fixture_id"]:
        raise RuntimeError("container is not owned by this fixture")
    return info


def wait_until(check, timeout=60):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        try:
            if check():
                return
        except (OSError, urllib.error.URLError, FixtureCommandError):
            pass
        time.sleep(.1)
    raise RuntimeError("fixture readiness deadline exceeded")


def api(state, path, data=None):
    headers = {"Content-Type": "application/json"}
    token = Path(state["token_file"])
    if token.exists():
        headers["Authorization"] = "Bearer " + token.read_text().strip()
    req = urllib.request.Request(state["base_url"] + path, headers=headers, data=None if data is None else json.dumps(data).encode())
    with urllib.request.urlopen(req, timeout=20) as response:
        return json.load(response)


def fixture_env(state):
    # Do not inherit production original roots, connectors, RPC credentials or
    # telemetry exporters from the invoking shell.
    env = {k: v for k, v in os.environ.items() if not k.startswith(("MEMOH_", "OTEL_"))}
    env.update(CONFIG_PATH=state["config"], OTEL_SDK_DISABLED="true")
    return env


def start_server(state, state_file):
    env = fixture_env(state)
    with open(Path(state["root"]) / "server-private.log", "a") as log:
        p = subprocess.Popen([state["server_binary"], "serve"], cwd=REPO, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    state["server_pid"] = p.pid
    state["server_start_ticks"] = (Path("/proc") / str(p.pid) / "stat").read_text().split()[21]
    private_json(state_file, state)

    def healthy():
        if p.poll() is not None:
            raise RuntimeError("fixture server exited; inspect server-private.log")
        with urllib.request.urlopen(urllib.request.Request(state["base_url"] + "/health", method="HEAD"), timeout=1) as response:
            return response.status == 200
    wait_until(healthy)


def stop_server(state):
    pid = state.get("server_pid")
    if not pid:
        return
    process = Path("/proc") / str(pid)
    if not process.exists() or (process / "stat").read_text().split()[2] == "Z":
        return
    expected_command = os.fsencode(state["server_binary"]) + b"\0serve\0"
    if process.stat().st_uid != os.getuid() or (process / "cmdline").read_bytes() != expected_command:
        raise RuntimeError("recorded PID is not the fixture server")
    if state.get("server_start_ticks") and (process / "stat").read_text().split()[21] != state["server_start_ticks"]:
        raise RuntimeError("recorded fixture PID has been reused")
    os.kill(pid, signal.SIGTERM)
    wait_until(lambda: not process.exists() or (process / "stat").read_text().split()[2] == "Z", timeout=15)


def restart_fixture(state_file, queue_only=False):
    state_file = Path(state_file).resolve()
    state = json.loads(state_file.read_text())
    before = inspect_container(state, state["queue_container"])["State"]["StartedAt"]
    old_pid = state["server_pid"]
    if not queue_only:
        stop_server(state)
    command(["docker", "restart", state["queue_container"]])
    wait_until(lambda: command(["docker", "exec", state["queue_container"], "valkey-cli", "ping"]) == "PONG")
    after = inspect_container(state, state["queue_container"])["State"]["StartedAt"]
    assert before != after, "queue was not restarted"
    if not queue_only:
        start_server(state, state_file)
        assert state["server_pid"] != old_pid, "server was not restarted"
    return {"queue_restarted": True, "server_restarted": not queue_only, "old_pid": old_pid, "new_pid": state["server_pid"], "queue_started_before": before, "queue_started_after": after}


def crash_restart_fixture(state_file, bot_id, session_id, run_id, evidence):
    state_file = Path(state_file).resolve()
    state = json.loads(state_file.read_text())
    evidence = Path(evidence)
    for key in ['pg_container', 'queue_container']:
        inspect_container(state, state[key])
    bot_id, session_id, run_id = map(lambda x: str(uuid.UUID(x)), [bot_id, session_id, run_id])
    config = tomllib.loads(Path(state['config']).read_text())
    prefix = config['session_runtime']['redis']['key_prefix']
    run_key = prefix + 'run:' + bot_id + ':' + session_id + ':' + run_id
    queue_key = prefix + 'follow_up_queue:' + bot_id + ':' + session_id

    def redis(*args):
        return subprocess.check_output(['docker', 'exec', state['queue_container'], 'valkey-cli', '--raw', *args], text=True).rstrip('\n')

    def snapshot():
        queue = redis('GET', queue_key)
        items = json.loads(queue or '{}').get('items', [])
        sql = "SELECT json_build_object('run_id',run_id,'state',state,'error_code',error_code,'fencing_token',fencing_token,'updated_at',updated_at) FROM session_runs WHERE run_id='" + run_id + "'"
        parent = json.loads(subprocess.check_output(['docker', 'exec', state['pg_container'], 'psql', '-U', state['pg_user'], '-d', state['database'], '-At', '-c', sql], text=True))
        return {'observed_at': time.time(), 'redis_time': redis('TIME').splitlines(), 'original_lease_pttl_ms': int(redis('PTTL', run_key)), 'leader_lease_pttl_ms': int(redis('PTTL', prefix + 'reaper_leader')), 'generation_sha256': hashlib.sha256(redis('GET', prefix + 'generation').encode()).hexdigest(), 'queue_sha256': hashlib.sha256(queue.encode()).hexdigest(), 'queue_items': [{k: x.get(k) for k in ['ID', 'Status', 'EnqueuedDuringRunID', 'InvocationID']} for x in items], 'original_parent': parent}
    pid = state['server_pid']
    process = Path('/proc') / str(pid)
    assert process.exists() and process.stat().st_uid == os.getuid()
    assert (process / 'cmdline').read_bytes() == os.fsencode(state['server_binary']) + b'\x00serve\x00'
    assert (process / 'stat').read_text().split()[21] == state['server_start_ticks']
    started_before = inspect_container(state, state['queue_container'])['State']['StartedAt']
    before = snapshot()
    assert before['original_parent']['state'] == 'running' and before['original_lease_pttl_ms'] > 0 and (len(before['queue_items']) == 1)
    proof = {'method': 'SIGKILL', 'old_pid': pid, 'queue_restarted': False, 'queue_started_before': started_before, 'before_kill': before, 'signal_sent_at': time.time()}
    (evidence / 'crash-lease-proof.json').write_text(json.dumps(proof, indent=2))
    os.kill(pid, signal.SIGKILL)
    wait_until(lambda: not process.exists() or (process / 'stat').read_text().split()[2] == 'Z', timeout=5)
    proof['process_dead_at'] = time.time()
    samples = []
    deadline = time.monotonic() + 8
    while time.monotonic() < deadline:
        sample = snapshot()
        samples.append(sample)
        proof['offline_lease_samples'] = samples
        (evidence / 'crash-lease-proof.json').write_text(json.dumps(proof, indent=2))
        assert sample['original_parent']['state'] == 'running', 'SIGKILL must not execute graceful durable release'
        assert sample['queue_sha256'] == before['queue_sha256'] and sample['generation_sha256'] == before['generation_sha256']
        if sample['original_lease_pttl_ms'] == -2:
            break
        time.sleep(0.15)
    assert samples[-1]['original_lease_pttl_ms'] == -2, 'real original owner lease did not expire within deadline'
    proof['lease_expired_observed_at'] = samples[-1]['observed_at']
    proof['new_server_start_requested_at'] = time.time()
    start_server(state, state_file)
    proof['new_pid'] = state['server_pid']
    proof['server_ready_at'] = time.time()
    proof['server_restarted'] = True
    proof['queue_started_after'] = inspect_container(state, state['queue_container'])['State']['StartedAt']
    assert proof['new_pid'] != pid and proof['queue_started_after'] == started_before
    proof['after_new_server_ready'] = snapshot()
    (evidence / 'crash-lease-proof.json').write_text(json.dumps(proof, indent=2))
    return proof


def validate_busy_restart(result):
    """Assert recovery identity/counts without mistaking an interrupted run for success."""
    mode = result["mode"]
    assert mode in {"busy-service", "busy-crash"}
    assert not result.get("failures")
    before = result["before_full_restart"]
    item = before["queue_item"]
    original_id = item["enqueued_during_run_id"]
    source, target = result["source_runs"], result["target_runs"]
    original = [run for run in source if run["run_id"] == original_id]
    resumed = [run for run in source if run["invocation_id"] == "resume:" + original_id]
    follow = [run for run in source if run["invocation_id"] == "follow-up:" + item["item_id"]]
    assert len(original) == len(follow) == 1
    assert original[0]["state"] == "lost" and follow[0]["state"] == "completed"
    assert target == before["snapshot"]["target_runs"] and len(target) == 1
    assert target[0]["state"] == "completed"
    phases = Counter(call["phase"] for call in result["model_calls"])
    restart = result["restart"]
    assert restart["server_restarted"] and restart["old_pid"] != restart["new_pid"]
    if mode == "busy-service":
        assert original[0]["error_code"] == "session_runtime.interrupted"
        assert len(source) == 4 and len(resumed) == 1 and resumed[0]["state"] == "completed"
        assert phases == {"initial": 2, "specialist": 3, "held_parent": 2, "followup": 1}
        assert restart["queue_restarted"]
        assert restart["queue_started_before"] != restart["queue_started_after"]
        assert all(sample["queues"]["source"]["follow_up"] == [item] for sample in result["automatic_recovery_samples"])
    else:
        assert original[0]["error_code"] == "runtime_owner_lease_expired"
        assert len(source) == 3 and not resumed
        assert phases == {"initial": 2, "specialist": 3, "held_parent": 1, "followup": 1}
        assert restart["method"] == "SIGKILL" and not restart["queue_restarted"]
        assert restart["queue_started_before"] == restart["queue_started_after"]
        baseline, samples = restart["before_kill"], restart["offline_lease_samples"]
        assert baseline["original_lease_pttl_ms"] > 0 and samples[-1]["original_lease_pttl_ms"] == -2
        assert all(sample["original_parent"]["state"] == "running" for sample in samples)
        assert all(sample["queue_sha256"] == baseline["queue_sha256"] and sample["generation_sha256"] == baseline["generation_sha256"] for sample in samples)
        assert restart["signal_sent_at"] <= restart["process_dead_at"] <= restart["lease_expired_observed_at"] <= restart["new_server_start_requested_at"]
    for sample in result["automatic_recovery_samples"]:
        assert sample["snapshot"]["target_runs"] == target
    assert all(not group[key] for group in result["drain_observation"]["queues"].values() for key in ["steer", "follow_up"])
    expected = Counter(x["hash"] for x in result["downloads"])
    links = result["assistant_links"]
    assert len(links) == len(expected) == 2 and len({x["message_id"] for x in links}) == 1
    assert Counter(x["hash"] for x in links) == expected
    assert len(result["quiet_windows"]) == 2
    assert all(window["pending_items"] == 0 and window["seconds"] == 5 for window in result["quiet_windows"])
    return {"mode": mode, "original_parent": original[0], "resumed_parents": resumed,
            "specialist": target[0], "followup": follow[0], "queue_item_id": item["item_id"],
            "model_phase_counts": dict(phases), "old_pid": restart["old_pid"], "new_pid": restart["new_pid"],
            "queue_restarted": restart["queue_restarted"], "specialist_replayed": False,
            "unknown_parent_marked_success": False, "final_asset_links": 2, "final_messages_with_assets": 1}


def run_logged(args, path, env=None):
    with open(path, "w") as log:
        result = subprocess.run(args, cwd=REPO, env=env, stdout=log, stderr=subprocess.STDOUT)
    if result.returncode:
        raise RuntimeError("command failed; inspect " + str(path))


def bootstrap(root):
    root = root.resolve()
    root.mkdir(parents=True, exist_ok=False)
    root.chmod(0o700)
    # Buildx writes client state even when the base image is already cached.
    # Keep it inside this fixture instead of relying on a writable home folder.
    os.environ["DOCKER_CONFIG"] = str(root / "docker-client")
    if shutil.disk_usage(root).free < 4 * 1024**3:
        raise RuntimeError("at least 4 GiB free space required; no automatic pruning")
    init = shutil.which("docker-init")
    if not init:
        raise RuntimeError("Docker's docker-init executable is required on PATH for the tiny workspace fixture")
    fixture_id = uuid.uuid4().hex[:12]
    state = {"root": str(root), "fixture_id": fixture_id, "pg_container": "delegation-pg-" + fixture_id, "queue_container": "delegation-queue-" + fixture_id,
             "workspace_image": "delegation-workspace:" + fixture_id, "pg_user": "acceptance", "database": "acceptance", "pg_password": secrets.token_hex(24),
             "admin_password": secrets.token_hex(24), "server_binary": str(root / "memoh-server"), "bridge_binary": str(root / "bridge"),
             "config": str(root / "config.toml"), "token_file": str(root / "token"), "api_port": free_port(), "rpc_port": free_port(), "model_port": free_port()}
    state["base_url"] = "http://127.0.0.1:" + str(state["api_port"])
    state_file = root / "fixture-private.json"
    private_json(state_file, state)
    # Reuse cached images; pull only these small fixture dependencies if absent.
    for image in ["postgres:18", "valkey/valkey:8-alpine", "node:25-bookworm-slim"]:
        if subprocess.run(["docker", "image", "inspect", image], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode:
            command(["docker", "pull", image])
    for role, image, port, extra in [
        ("pg", "postgres:18", "5432", ["-e", "POSTGRES_USER=acceptance", "-e", "POSTGRES_DB=acceptance", "-e", "POSTGRES_PASSWORD=" + state["pg_password"]]),
        ("queue", "valkey/valkey:8-alpine", "6379", []),
    ]:
        # Docker may assign a different ephemeral host port on restart. Pin a
        # free loopback port so recovery exercises the same configured backend.
        host_port = free_port()
        args = ["docker", "run", "-d", "--name", state[role + "_container"], "--label", LABEL + "=" + fixture_id, "--memory", "512m" if role == "pg" else "128m", "-p", "127.0.0.1:" + str(host_port) + ":" + port] + extra + [image]
        if role == "queue":
            args += ["valkey-server", "--appendonly", "yes", "--appendfsync", "always", "--maxmemory", "64mb", "--maxmemory-policy", "noeviction"]
        command(args)
        info = inspect_container(state, state[role + "_container"])
        state[role + "_port"] = int(info["NetworkSettings"]["Ports"][port + "/tcp"][0]["HostPort"])
        private_json(state_file, state)
    wait_until(lambda: command(["docker", "exec", state["pg_container"], "pg_isready", "-U", "acceptance"]) != "")
    wait_until(lambda: command(["docker", "exec", state["queue_container"], "valkey-cli", "ping"]) == "PONG")
    image_root = root / "workspace-build"
    image_root.mkdir()
    shutil.copyfile(init, image_root / "docker-init")
    (image_root / "docker-init").chmod(0o755)
    (image_root / "Dockerfile").write_text("FROM node:25-bookworm-slim\nCOPY docker-init /usr/bin/tini\nWORKDIR /data\n")
    run_logged(["docker", "build", "-t", state["workspace_image"], str(image_root)], root / "workspace-build.log")
    run_logged(["go", "build", "-p", "3", "-o", state["server_binary"], "./cmd/agent"], root / "build-server.log")
    run_logged(["go", "build", "-p", "3", "-o", state["bridge_binary"], "./cmd/bridge"], root / "build-bridge.log")
    values = dict(state, data_root=str(root / "data"), jwt_secret=secrets.token_hex(32), encryption_key=base64.b64encode(secrets.token_bytes(32)).decode())
    config = string.Template((Path(__file__).with_name("delegation-fixture.toml.in")).read_text()).substitute(values)
    Path(state["config"]).write_text(config)
    Path(state["config"]).chmod(0o600)
    env = fixture_env(state)
    run_logged([state["server_binary"], "migrate", "up"], root / "migrate-raw.log", env)
    schema = command(["docker", "exec", state["pg_container"], "psql", "-U", "acceptance", "-d", "acceptance", "-At", "-c", "SELECT version || ':' || dirty FROM schema_migrations"])
    expected = max(int(p.name.split('_')[0]) for p in (REPO / "db/postgres/migrations").glob("*.up.sql"))
    assert schema == str(expected) + ":false", "full migration chain did not finish cleanly"
    # The Go integration test independently creates another DB and calls the
    # repository's freshMigratedDB. JSON events make every Skip a hard failure.
    env["TEST_POSTGRES_DSN"] = f"postgres://acceptance:{state['pg_password']}@127.0.0.1:{state['pg_port']}/acceptance?sslmode=disable"
    test = "TestBotDelegationPostgresClaimIsolationAndDescendantAssets"
    run_logged(["go", "test", "-p", "3", "-json", "-count=1", "-tags=integration", "./internal/db", "-run", "^" + test + "$"], root / "pg-go-events.jsonl", env)
    events = [json.loads(line) for line in (root / "pg-go-events.jsonl").read_text().splitlines() if line.startswith("{")]
    skips = [e for e in events if e.get("Action") == "skip"]
    passes = [e for e in events if e.get("Action") == "pass" and e.get("Test") == test]
    (root / "pg-go-raw.log").write_text("".join(e.get("Output", "") for e in events))
    assert not skips and len(passes) == 1, "integration must execute exactly once with zero skips"
    (root / "migration-proof.json").write_text(json.dumps({"fresh_container": True, "schema": expected, "dirty": False, "integration_passes": len(passes), "integration_skips": len(skips)}, indent=2))
    start_server(state, state_file)
    auth = api(state, "/auth/login", {"username": "acceptance", "password": state["admin_password"]})
    Path(state["token_file"]).write_text(auth["access_token"])
    Path(state["token_file"]).chmod(0o600)
    for role in ["source", "target"]:
        bot = api(state, "/bots", {"name": "acceptance-" + role, "wait_for_ready": False})
        state[role + "_bot"] = bot["id"]
        private_json(state_file, state)
        wait_until(lambda: api(state, "/bots/" + bot["id"]).get("status") == "ready", timeout=90)
    state["setup_token_file"] = state["token_file"]
    member_password = secrets.token_hex(24)
    member = api(state, "/users", {"username": "delegate-member", "password": member_password, "role": "member"})
    state["member_id"] = member["id"]
    for role in ["source", "target"]:
        grant = api(state, "/bots/" + state[role + "_bot"] + "/user-access", {"subject_type": "user", "user_id": member["id"], "permissions": ["manage"]})
        state[role + "_grant"] = grant["id"]
    auth = api(state, "/auth/login", {"username": "delegate-member", "password": member_password})
    state["token_file"] = str(root / "member-token")
    Path(state["token_file"]).write_text(auth["access_token"])
    Path(state["token_file"]).chmod(0o600)
    private_json(state_file, state)
    print("PASS fresh isolated PG, complete official migrations, zero-skip Go integration, real server and two ready Bots", flush=True)
    return state_file, state


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("action", choices=["run", "check", "stop", "verify"])
    p.add_argument("--root", type=Path, required=True)
    p.add_argument("--modes", nargs="+", choices=["idle", "busy", "busy-service", "busy-crash"], default=["idle", "busy"])
    a = p.parse_args()
    if a.action == "verify":
        proof = validate_busy_restart(json.loads((a.root / "results.json").read_text()))
        print(json.dumps(proof, indent=2))
        return
    if a.action == "stop":
        state = json.loads((a.root / "fixture-private.json").read_text())
        stop_server(state)
        for name in [state["pg_container"], state["queue_container"]]:
            inspect_container(state, name)
            command(["docker", "stop", name])
        print("Stopped only fixture server/PG/Valkey; data and evidence retained")
        return
    if a.action == "check":
        state_file = a.root / "fixture-private.json"
        state = json.loads(state_file.read_text())
        for key in ["pg_container", "queue_container"]:
            inspect_container(state, state[key])
        evidence_root = a.root / ("check-" + uuid.uuid4().hex[:8])
        evidence_root.mkdir()
    else:
        state_file, state = bootstrap(a.root)
        evidence_root = a.root
    for mode in a.modes:
        args = ["python", str(Path(__file__).with_name("bot_delegation.py")), "--base-url", state["base_url"], "--source-bot", state["source_bot"], "--target-bot", state["target_bot"],
                "--token-file", state["token_file"], "--setup-token-file", state["setup_token_file"], "--pg-container", state["pg_container"], "--pg-user", "acceptance", "--database", "acceptance", "--evidence", str(evidence_root / mode),
                "--model-port", str(state["model_port"]), "--restart-fixture", str(state_file), "--mode", mode, "--allow-isolated-fixture-writes"]
        run_logged(args, evidence_root / (mode + "-raw.log"))
        print("PASS native " + mode + " fixture; raw results retained", flush=True)


if __name__ == "__main__":
    main()
