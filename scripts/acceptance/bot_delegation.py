#!/usr/bin/env python3
"""Native synthetic delegation acceptance against an explicitly isolated local server.

The shipped delegation_fixture.py provisions all dependencies. This runner uses
real WebSocket admission, application scheduling, PostgreSQL, Valkey, media and
HTTP authorization. Only provider responses and attachment bytes are synthetic.
"""
import argparse
import asyncio
import base64
from collections import Counter
import concurrent.futures
import hashlib
import http.server
import json
from pathlib import Path
import subprocess
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

import websockets

from delegation_fixture import crash_restart_fixture, restart_fixture, validate_busy_restart

INPUT = b"delegation regression original\n"
OUTPUT = b"delegation regression specialist output\n"


def main():
    p = argparse.ArgumentParser(description=__doc__)
    for name in ["base-url", "source-bot", "target-bot", "token-file", "pg-container", "pg-user", "database", "evidence"]:
        p.add_argument("--" + name, required=True)
    p.add_argument("--setup-token-file", help="isolated admin token for model setup and reversible grant revocation")
    p.add_argument("--model-port", type=int, default=18281)
    p.add_argument("--restart-fixture", help="private state file created by delegation_fixture.py")
    p.add_argument("--mode", choices=["idle", "busy", "busy-service", "busy-crash"], default="idle")
    p.add_argument("--allow-isolated-fixture-writes", action="store_true")
    a = p.parse_args()
    if not a.allow_isolated_fixture_writes or urllib.parse.urlparse(a.base_url).hostname not in {"127.0.0.1", "localhost", "::1"}:
        p.error("explicit opt-in and loopback server are required")
    if a.mode in {"busy", "busy-service", "busy-crash"} and not a.restart_fixture:
        p.error("busy recovery requires the owned restart fixture")
    source, target = str(uuid.UUID(a.source_bot)), str(uuid.UUID(a.target_bot))
    assert source != target
    evidence = Path(a.evidence)
    evidence.mkdir(parents=True, exist_ok=True)
    token = Path(a.token_file).read_text().strip()
    setup_token = Path(a.setup_token_file).read_text().strip() if a.setup_token_file else token
    state = json.loads(Path(a.restart_fixture).read_text()) if a.restart_fixture else None
    if state:
        assert (state["base_url"], state["source_bot"], state["target_bot"], state["pg_container"]) == (a.base_url, source, target, a.pg_container)
    errors, result, model_calls = [], {"mode": a.mode}, []
    release, parent_started, release_parent = threading.Event(), threading.Event(), threading.Event()
    hashes = [hashlib.sha256(INPUT).hexdigest(), hashlib.sha256(OUTPUT).hexdigest()]

    def request(path, data=None, method=None, anonymous=False, headers=None, binary=False, setup=False):
        h = {"Content-Type": "application/json"}
        if not anonymous:
            h["Authorization"] = "Bearer " + (setup_token if setup else token)
        h.update(headers or {})
        req = urllib.request.Request(a.base_url + path, data=data if isinstance(data, bytes) else None if data is None else json.dumps(data).encode(), headers=h, method=method)
        try:
            with urllib.request.urlopen(req, timeout=15) as res:
                body = res.read()
                return res.status, dict(res.headers), body if binary else json.loads(body) if body else None
        except urllib.error.HTTPError as e:
            body = e.read()
            return e.code, dict(e.headers), body if binary else json.loads(body) if body else None

    def api(path, data=None, method=None, setup=False):
        status, _, body = request(path, data, method, setup=setup)
        assert status < 300, (path, status, body.get("code") if isinstance(body, dict) else None)
        return body

    def sql(query):
        proc = subprocess.run(["docker", "exec", a.pg_container, "psql", "-U", a.pg_user, "-d", a.database, "-At", "-c", query], check=True, text=True, capture_output=True)
        return json.loads(proc.stdout.strip() or "[]")

    def runs(sid):
        return sql("SELECT coalesce(json_agg(x),'[]'::json) FROM (SELECT run_id,state,invocation_id,error_code FROM session_runs WHERE session_id='" + str(uuid.UUID(sid)) + "' ORDER BY created_at) x")

    def check(ok, message):
        print(("PASS " if ok else "FAIL ") + message, flush=True)
        if not ok:
            errors.append(message)
            raise AssertionError(message)

    def wait_for(read, predicate, label, seconds=45):
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            value = read()
            if predicate(value):
                return value
            time.sleep(.2)
        raise AssertionError("deadline: " + label)

    class Model(http.server.BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_POST(self):
            req = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            messages = req.get("messages", [])
            last_user = next((str(m.get("content", "")) for m in reversed(messages) if m.get("role") == "user"), "")
            ids = [m.get("tool_call_id") for m in messages if m.get("role") == "tool"]
            specialist = req["model"].endswith("specialist")
            phase = "specialist" if specialist else "followup" if "execution state:" in last_user else "held_parent" if "HOLD_PARENT" in last_user else "initial"
            model_calls.append({"model": req["model"], "tool_result_ids": ids, "phase": phase, "sequence": len(model_calls) + 1, "started_at": time.time()})
            call = None
            if specialist:
                assert release.wait(90), "specialist gate deadline"
                if "new-output" not in ids:
                    call = ("new-output", "send", {"platform": "web", "target": target, "attachments": [{"type": "file", "base64": base64.b64encode(OUTPUT).decode(), "mime": "text/plain", "name": "output.txt"}]})
                elif "return-input" not in ids:
                    call = ("return-input", "send", {"platform": "web", "target": target, "attachments": [{"type": "file", "content_hash": hashes[0], "mime": "text/plain", "name": "original.txt"}]})
                else:
                    text = "SPECIALIST_FINAL"
            elif "execution state:" in last_user:
                text = "SUPERVISOR_FINAL"
            elif "HOLD_PARENT" in last_user:
                parent_started.set()
                assert release_parent.wait(90), "parent gate deadline"
                text = "BUSY_PARENT_DONE"
            elif "delegate" in ids:
                text = "SUPERVISOR_INITIAL_DONE"
            else:
                result["initial_admission"] = sql("SELECT json_build_object('state',r.state,'input_asset_hashes',r.input_json#>'{resume,input_asset_hashes}','owner_matches',r.input_json#>>'{resume,user_id}'=s.created_by_user_id::text,'fence_matches',r.fencing_token=s.runtime_fencing_token,'history_asset_links',(SELECT count(*) FROM bot_history_message_assets a JOIN bot_history_messages m ON m.id=a.message_id WHERE m.session_id=s.id)) FROM session_runs r JOIN bot_sessions s ON s.id=r.session_id WHERE r.session_id='" + session + "' ORDER BY r.created_at DESC LIMIT 1")
                call = ("delegate", "delegate_to_bot", {"target_bot_id": target, "message": "synthetic specialist task", "attachments": [{"content_hash": hashes[0]}]})
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.end_headers()

            def chunk(delta, finish=None):
                data = {"id": "local-regression", "object": "chat.completion.chunk", "created": int(time.time()), "model": req["model"], "choices": [{"index": 0, "delta": delta, "finish_reason": finish}]}
                self.wfile.write(("data: " + json.dumps(data) + "\n\n").encode())
                self.wfile.flush()
            try:
                chunk({"role": "assistant"})
                if call:
                    ident, name, args = call
                    chunk({"tool_calls": [{"index": 0, "id": ident, "type": "function", "function": {"name": name, "arguments": json.dumps(args)}}]})
                    chunk({}, "tool_calls")
                else:
                    chunk({"content": text})
                    chunk({}, "stop")
                self.wfile.write(b"data: [DONE]\n\n")
            except (BrokenPipeError, ConnectionResetError):
                pass

    httpd = http.server.ThreadingHTTPServer(("127.0.0.1", a.model_port), Model)
    threading.Thread(target=httpd.serve_forever, daemon=True).start()
    busy_executor = concurrent.futures.ThreadPoolExecutor(max_workers=1)
    try:
        suffix = uuid.uuid4().hex[:8]
        provider = api("/providers", {"name": "delegation-regression-" + suffix, "client_type": "openai-completions", "config": {"base_url": f"http://127.0.0.1:{a.model_port}/v1", "api_key": "synthetic"}}, setup=True)
        for bot, role in [(source, "supervisor"), (target, "specialist")]:
            model = api("/models", {"model_id": "regression-" + role, "name": "regression-" + role, "provider_id": provider["id"], "type": "chat", "enable": True, "config": {"compatibilities": ["tool-call"], "context_window": 131072, "thinking_mode": "none"}}, setup=True)
            api("/bots/" + bot + "/settings", {"chat_model_id": model["id"], "chat_runtime": "model", "compaction_enabled": False, "reasoning_enabled": False, "tool_approval_config": {"enabled": False, "read": {"mode": "allow"}, "write": {"mode": "allow"}, "exec": {"mode": "allow"}}}, "PUT", setup=True)
        session = api("/bots/" + source + "/sessions", {"type": "chat", "title": "Delegation attachment regression " + suffix})["id"]
        result.update(source_bot=source, target_bot=target, source_session=session)
        base = "/bots/" + source + "/sessions/" + session + "/delegations"
        boundary = "regression-boundary"
        upload = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="original.txt"\r\nContent-Type: text/plain\r\n\r\n'.encode() + INPUT + f"\r\n--{boundary}--\r\n".encode())
        status, _, uploaded = request("/bots/" + source + "/media", upload, headers={"Content-Type": "multipart/form-data; boundary=" + boundary})
        check(status == 201 and uploaded["content_hash"] == hashes[0], "synthetic input uploaded under original owner")
        status, _, _ = request(base, {"request_id": str(uuid.uuid4()), "target_bot_id": target, "message": "unlinked input must be rejected", "attachments": [{"content_hash": hashes[0]}]})
        check(status == 404, "Bot-owned upload without session/admitted input is rejected")

        async def send_turn(text, attachments):
            url = a.base_url.replace("http://", "ws://").replace("https://", "wss://")
            invocation = str(uuid.uuid4())
            async with websockets.connect(url + "/bots/" + source + "/web/ws", additional_headers={"Authorization": "Bearer " + token}) as ws:
                await ws.send(json.dumps({"type": "message", "session_id": session, "invocation_id": invocation, "text": text, "workspace_target_id": "native", "attachments": attachments}))
                deadline = time.monotonic() + 90
                while time.monotonic() < deadline:
                    try:
                        await asyncio.wait_for(ws.recv(), .2)
                    except asyncio.TimeoutError:
                        pass
                    except websockets.ConnectionClosed:
                        if a.mode in {"busy-service", "busy-crash"} and text == "HOLD_PARENT":
                            return {"client_disconnected": True, "invocation_id": invocation}
                        raise
                    rows = await asyncio.to_thread(runs, session)
                    current = [r for r in rows if r["invocation_id"] == invocation]
                    if current and current[0]["state"] in {"completed", "failed", "aborted", "lost"}:
                        return current[0]
            raise AssertionError("supervisor did not finish")

        initial = asyncio.run(send_turn("Delegate this newly attached original", [{"type": "file", "content_hash": hashes[0], "mime": "text/plain", "name": "original.txt", "size": len(INPUT)}]))
        check(initial["state"] == "completed" and not release.is_set(), "initial supervisor completes and disconnects before specialist")
        admission = result["initial_admission"]
        check(admission == {"state": "running", "input_asset_hashes": [hashes[0]], "owner_matches": True, "fence_matches": True, "history_asset_links": 0}, "authoritative owner/fence and current input admitted before history transaction persists")
        items = api(base)["items"]
        check(len(items) == 1, "native delegate_to_bot accepts a same-turn new attachment exactly once")
        view = items[0]
        detail = base + "/" + view["delegation_id"]
        result["view"] = view

        def queues():
            return {"source": api("/bots/" + source + "/sessions/" + session + "/queue"), "target": api("/bots/" + target + "/sessions/" + view["target_session_id"] + "/queue")}

        def snapshot():
            return {"source_runs": runs(session), "target_runs": runs(view["target_session_id"]), "model_calls": len(model_calls)}

        def quiet(expected, label):
            # Observe the actual scheduler for a bounded quiet period after all
            # active runs and pending queues have drained; an immediate read is
            # not sufficient to rule out asynchronously scheduled duplicates.
            samples, deadline = [], time.monotonic() + 5
            while time.monotonic() < deadline:
                q, current = queues(), snapshot()
                assert current == expected, label + ": late business execution"
                assert all(r["state"] in ({"completed", "failed", "aborted", "lost"} if a.mode in {"busy-service", "busy-crash"} else {"completed"}) for r in current["source_runs"]), label + ": nonterminal source run"
                assert all(r["state"] == "completed" for r in current["target_runs"]), label + ": nonterminal specialist run"
                assert all(not group[k] for group in q.values() for k in ["steer", "follow_up"]), label + ": pending queue"
                samples.append(round(time.monotonic(), 3))
                time.sleep(.2)
            result.setdefault("quiet_windows", []).append({"label": label, "seconds": 5, "samples": len(samples), "pending_items": 0, "terminal_runs": expected, "final_queues": q})
            check(True, label + ": five-second terminal/empty-queue observation with stable runs and model calls")

        def lose_ack():
            changed = sql("WITH changed AS (UPDATE bot_delegations SET delivery_state='unknown',revision=revision+1 WHERE id='" + str(uuid.UUID(view["delegation_id"])) + "' AND state='completed' RETURNING id) SELECT json_agg(id) FROM changed")
            assert changed == [view["delegation_id"]]

        def retry16():
            with concurrent.futures.ThreadPoolExecutor(max_workers=16) as ex:
                retried = list(ex.map(lambda _: api(detail + "/delivery", {}), range(16)))
            check(len(retried) == 16 and all(x["delegation_id"] == view["delegation_id"] for x in retried), "16 concurrent delivery attempts preserve task identity")

        if a.mode in {"busy", "busy-service", "busy-crash"}:
            busy_future = busy_executor.submit(lambda: asyncio.run(send_turn("HOLD_PARENT", [])))
            check(parent_started.wait(30), "second real supervisor turn is blocked inside local model")
        release.set()
        view = wait_for(lambda: api(detail), lambda x: x["state"] in {"completed", "failed", "unknown"} and x["delivery_state"] in {"accepted", "unknown", "unavailable"}, "specialist terminal")
        check(view["state"] == "completed" and view["delivery_state"] == "accepted", "specialist completes and delivery is accepted")
        if a.mode in {"busy", "busy-service", "busy-crash"}:
            q = queues()
            check(len(q["source"]["follow_up"]) == 1, "busy supervisor has exactly one persisted pending follow-up")
            queued = q["source"]["follow_up"][0]
            before_busy = snapshot()
            result["busy_queue_before"] = q
            result["before_full_restart"] = {"snapshot": before_busy, "view": api(detail), "queue_item": queued, "model_calls": list(model_calls)}
            (evidence / "before-restart.json").write_text(json.dumps(result["before_full_restart"], indent=2))
            if a.mode == "busy-crash":
                result["restart"] = crash_restart_fixture(a.restart_fixture, source, session, queued["enqueued_during_run_id"], evidence)
            else:
                result["restart"] = restart_fixture(a.restart_fixture, queue_only=a.mode != "busy-service")
            if a.mode in {"busy-service", "busy-crash"}:
                samples = []
                deadline = time.monotonic() + 12
                while time.monotonic() < deadline:
                    current = snapshot()
                    samples.append({"at": time.time(), "snapshot": current, "queues": queues(), "view": api(detail)})
                    assert current["target_runs"] == before_busy["target_runs"], "restart repeated or changed specialist business"
                    (evidence / "automatic-recovery-observation.json").write_text(json.dumps(samples, indent=2))
                    time.sleep(.4)
                result["automatic_recovery_samples"] = samples
                result["busy_client_after_restart"] = busy_future.result(timeout=5)
                result["post_restart_attachment_reads"] = []
                for h, content in zip(hashes, [INPUT, OUTPUT]):
                    status, headers, data = request(detail + "/attachments/" + h, binary=True)
                    check(status == 200 and data == content and headers.get("Cache-Control") == "no-store", "original remains intact after busy process restart " + h[:12])
                    result["post_restart_attachment_reads"].append({"hash": h, "status": status, "bytes": len(data)})
                release_parent.set()
                follow_invocation = "follow-up:" + queued["item_id"]
                def drained():
                    current = snapshot()
                    q = queues()
                    follow = [r for r in current["source_runs"] if r["invocation_id"] == follow_invocation]
                    return {"snapshot": current, "queues": q, "followup_runs": follow}
                end = time.monotonic() + 15
                while time.monotonic() < end:
                    observed = drained()
                    result["drain_observation"] = observed
                    (evidence / "drain-observation.json").write_text(json.dumps(observed, indent=2))
                    if len(observed["followup_runs"]) == 1 and observed["followup_runs"][0]["state"] == "completed" and all(not g[k] for g in observed["queues"].values() for k in ["steer", "follow_up"]):
                        break
                    time.sleep(.4)
                check(len(observed["followup_runs"]) == 1 and observed["followup_runs"][0]["state"] == "completed", "automatic recovery completes exactly the original queued follow-up")
                check(all(not g[k] for g in observed["queues"].values() for k in ["steer", "follow_up"]), "recovered source and target queues are drained")
                result["interrupted_parent_runs"] = [r for r in runs(session) if r["run_id"] == queued["enqueued_during_run_id"]]
                check(runs(view["target_session_id"]) == before_busy["target_runs"], "specialist run identity remains unchanged through busy server restart")
                lose_ack()
                retry16()
            else:
                q = queues()
                check(q["source"]["follow_up"] == [queued], "Valkey AOF restart preserves the exact queued follow-up identity")
                lose_ack()
                retry16()
                q = queues()
                check(q["source"]["follow_up"] == [queued] and snapshot() == before_busy, "lost-ack concurrent retries keep one busy queue item and never rerun specialist")
                result["busy_queue_after_restart_and_retry"] = q
                release_parent.set()
                check(busy_future.result(timeout=45)["state"] == "completed", "blocked parent completes after controlled release")
        if a.mode in {"busy-service", "busy-crash"}:
            source_runs = runs(session)
            count = len(source_runs)
            check(len([r for r in source_runs if r["invocation_id"].startswith("follow-up:")]) == 1, "exactly one original follow-up invocation exists")
        else:
            count = 3 if a.mode == "busy" else 2
            source_runs = wait_for(lambda: runs(session), lambda rows: len(rows) >= count and all(x["state"] == "completed" for x in rows), "follow-up terminal")
            check(len(source_runs) == count, "exactly one supervisor follow-up completes")
        check(Counter(x["content_hash"] for x in view["attachments"]) == Counter(hashes), "ledger contains exactly one reference to each original and output")
        links = sql("SELECT coalesce(json_agg(json_build_object('message_id',m.id,'hash',a.content_hash)),'[]'::json) FROM bot_history_message_assets a JOIN bot_history_messages m ON m.id=a.message_id WHERE m.session_id='" + session + "' AND m.role='assistant'")
        check(len(links) == 2 and len({x["message_id"] for x in links}) == 1 and Counter(x["hash"] for x in links) == Counter(hashes), "one supervisor assistant message contains both attachments exactly once")
        result["assistant_links"] = links
        counts = Counter("specialist" if c["model"].endswith("specialist") else "supervisor" for c in model_calls)
        if a.mode in {"busy-service", "busy-crash"}:
            phases = Counter(c["phase"] for c in model_calls)
            result["model_phase_counts"] = dict(phases)
            check(phases["specialist"] == 3 and phases["followup"] == 1 and phases["initial"] == 2, "one specialist business and one follow-up model execution; interrupted parent counted separately")
        else:
            check(counts == {"specialist": 3, "supervisor": count + 1}, "synthetic model call counts match one specialist business execution")
        before = snapshot()
        quiet(before, "initial completion")
        if a.mode == "idle" and a.restart_fixture:
            lose_ack()
            result["restart"] = restart_fixture(a.restart_fixture)
            retry16()
        repeated = api(base, {"request_id": view["request_id"], "target_bot_id": target, "message": "synthetic specialist task", "attachments": [{"content_hash": hashes[0]}]})
        check(repeated["delegation_id"] == view["delegation_id"], "same request after reopening preserves original task")
        quiet(before, "after restart/retry/reopen")
        result["view"], result["source_runs"], result["target_runs"] = api(detail), runs(session), runs(view["target_session_id"])
        downloads = []
        for h, content in zip(hashes, [INPUT, OUTPUT]):
            path = detail + "/attachments/" + h
            status, headers, data = request(path, binary=True)
            check(status == 200 and data == content and headers.get("Cache-Control") == "no-store", "scoped original exact bytes and no-store " + h[:12])
            etag = headers.get("Etag", headers.get("ETag"))
            for value, expected_status, expected_range, expected_bytes in [("bytes=2-6", 206, f"bytes 2-6/{len(content)}", content[2:7]), ("bytes=-4", 206, f"bytes {len(content)-4}-{len(content)-1}/{len(content)}", content[-4:]), (f"bytes={len(content)}-", 416, f"bytes */{len(content)}", None)]:
                status, headers, data = request(path, headers={"Range": value}, binary=True)
                check(status == expected_status and headers.get("Content-Range") == expected_range and (expected_bytes is None or data == expected_bytes) and headers.get("Cache-Control") == "no-store", "scoped Range exact bytes/Content-Range/no-store " + value)
            status, headers, data = request(path, headers={"Range": "bytes=2-6", "If-Range": etag}, binary=True)
            check(status == 206 and data == content[2:7] and headers.get("Cache-Control") == "no-store", "matching If-Range resumes original")
            status, headers, data = request(path, headers={"Range": "bytes=2-6", "If-Range": '"changed"'}, binary=True)
            check(status == 200 and data == content and headers.get("Cache-Control") == "no-store", "mismatching If-Range returns full original")
            status, headers, data = request(path, method="HEAD", binary=True)
            check(status == 200 and data == b"" and headers.get("Content-Length") == str(len(content)) and headers.get("Cache-Control") == "no-store", "scoped HEAD reports original length without body")
            status, headers, _ = request(path, anonymous=True, headers={"Range": "bytes=2-6"}, binary=True)
            check(status == 401 and headers.get("Cache-Control") == "no-store", "anonymous scoped Range rejected without caching")
            downloads.append({"hash": h, "bytes": len(content), "sha256_verified": True, "range_verified": True, "no_store": True})
        for bot, h, expected, content in [(source, hashes[0], 200, INPUT), (target, hashes[0], 404, INPUT), (target, hashes[1], 200, OUTPUT), (source, hashes[1], 404, OUTPUT)]:
            status, _, data = request("/bots/" + bot + "/media/" + h, binary=True)
            check(status == expected and (expected != 200 or data == content), "original owner separation " + h[:12] + " expected " + str(expected))
            if expected == 200:
                status, headers, data = request("/bots/" + bot + "/media/" + h, headers={"Range": "bytes=0-4"}, binary=True)
                check(status == 206 and data == content[:5] and headers.get("Content-Range") == f"bytes 0-4/{len(content)}", "original media Range exact bytes and Content-Range")
        if state and a.setup_token_file:
            # Revoke only the member's specialist grant through the real API;
            # the requester retains access to the source conversation.
            grant_path = "/bots/" + target + "/user-access/" + state["target_grant"]
            api(grant_path, method="DELETE", setup=True)
            try:
                for h in hashes:
                    status, headers, _ = request(detail + "/attachments/" + h, headers={"Range": "bytes=2-6"}, binary=True)
                    check(status == 404 and headers.get("Cache-Control") == "no-store", "live specialist grant revocation rejects scoped Range without caching")
            finally:
                grant = api("/bots/" + target + "/user-access", {"subject_type": "user", "user_id": state["member_id"], "permissions": ["manage"]}, setup=True)
                state = json.loads(Path(a.restart_fixture).read_text())
                state["target_grant"] = grant["id"]
                Path(a.restart_fixture).write_text(json.dumps(state, indent=2))
            check(request(detail + "/attachments/" + hashes[0], binary=True)[0] == 200, "restored explicit grant restores access to original")
        else:
            result["revocation"] = "UNRUN: isolated member/admin fixture required"
        result["downloads"] = downloads
        if a.mode in {"busy-service", "busy-crash"}:
            result["model_calls"] = list(model_calls)
            result["restart_verification"] = validate_busy_restart(result)
            (evidence / "restart-verification.json").write_text(json.dumps(result["restart_verification"], indent=2))
    except Exception as e:
        if not errors:
            errors.append(type(e).__name__ + ": " + str(e))
        raise
    finally:
        release.set()
        release_parent.set()
        httpd.shutdown()
        busy_executor.shutdown(wait=True)
        result["model_calls"], result["failures"] = model_calls, errors
        if result.get("source_session"):
            result["final_source_runs"] = runs(result["source_session"])
            if result.get("view", {}).get("target_session_id"):
                result["final_target_runs"] = runs(result["view"]["target_session_id"])
            result["final_assistant_asset_links"] = sql("SELECT coalesce(json_agg(json_build_object('message_id',m.id,'hash',a.content_hash)),'[]'::json) FROM bot_history_message_assets a JOIN bot_history_messages m ON m.id=a.message_id WHERE m.session_id='" + result["source_session"] + "' AND m.role='assistant'")
        (evidence / "results.json").write_text(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
