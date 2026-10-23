#!/usr/bin/env python3
"""Static template checks only: no Docker daemon, credentials or services used."""
import json
import os
from pathlib import Path
import subprocess
import tomllib

HERE = Path(__file__).resolve().parent
PROJECT = "memoh-acceptance-40f0a69"
ROOT = "/synthetic-static-check/" + PROJECT
IMAGES = json.loads((HERE / "images.json").read_text())
ENV = {
    **os.environ,
    "ACCEPTANCE_ROOT": ROOT,
    "ACCEPTANCE_POSTGRES_IMAGE": "postgres@sha256:" + "0" * 64,
    "ACCEPTANCE_WEB_PORT": "18082",
}


def compose(override=False, missing=None):
    env = ENV.copy()
    if missing:
        env.pop(missing, None)
    args = ["docker", "compose", "--env-file", os.devnull,
            "--project-name", PROJECT, "-f", str(HERE / "compose.yaml")]
    if override:
        args += ["-f", str(HERE / "workspace-permissions.yaml")]
    return subprocess.run(args + ["config", "--format", "json"], env=env,
                          capture_output=True, text=True, timeout=30, check=False)


def check():
    base = compose()
    assert base.returncode == 0, "base Compose validation failed"
    extended = compose(override=True)
    assert extended.returncode == 0, "explicit permission override validation failed"
    cfg, full = json.loads(base.stdout), json.loads(extended.stdout)
    assert cfg["name"] == PROJECT
    services = cfg["services"]
    assert set(services) == {"postgres", "migrate", "server", "channel", "web"}
    assert services["server"].get("privileged", False) is False
    assert full["services"]["server"]["privileged"] is True
    # Compose's canonical JSON omits default false values.
    full["services"]["server"].pop("privileged")
    assert full == cfg, "permission override must change only privileged"
    assert cfg["networks"]["acceptance"]["internal"] is True
    assert not cfg.get("volumes"), "no external or anonymous named volumes"
    for name, svc in services.items():
        assert svc["pull_policy"] == "never"
        assert svc["restart"] == "no"
        assert not any(k in svc for k in (
            "build", "container_name", "pid", "network_mode", "devices",
            "extra_hosts", "cap_add", "env_file")), name
        assert set(svc["networks"]) == {"acceptance"}
        assert not any("proxy" in k.lower() for k in svc.get("environment", {}))
        for mount in svc.get("volumes", []):
            assert mount["type"] == "bind", name
            assert mount["source"].startswith(ROOT + "/"), name
            assert mount["bind"].get("create_host_path", False) is False, name
            assert "docker.sock" not in mount["source"]
        if name != "web":
            assert not svc.get("ports"), name
        if name in {"migrate", "server", "channel"}:
            assert svc["image"] == IMAGES["server"]
            mounts = {m["target"] for m in svc.get("volumes", [])}
            assert {"/var/lib/containerd", "/opt/memoh/data"} <= mounts | set(svc.get("tmpfs", []))
    assert services["web"]["image"] == IMAGES["web"]
    assert len(services["web"]["ports"]) == 1
    assert services["web"]["ports"][0]["host_ip"] == "127.0.0.1"
    assert services["migrate"]["entrypoint"] == ["/app/memoh-server", "migrate", "up"]
    assert services["postgres"]["environment"]["POSTGRES_PASSWORD_FILE"] == "/run/secrets/pg-password"
    print("PASS: Compose, exact candidate digests, scoped binds, no build/pull, loopback, explicit privilege boundary")

    for key in ("ACCEPTANCE_ROOT", "ACCEPTANCE_POSTGRES_IMAGE", "ACCEPTANCE_WEB_PORT"):
        assert compose(missing=key).returncode != 0, key
    print("PASS: all three missing deployment inputs rejected")

    app = tomllib.loads((HERE / "config.toml.example").read_text())
    assert app["postgres"]["host"] == "postgres"
    assert app["postgres"]["database"] == "acceptance"
    assert app["container"]["default_image"] == IMAGES["workspace_import_tag"]
    assert app["container"]["image_pull_policy"] == "never"
    assert app["container"]["backend"] == "containerd"
    assert app["containerd"]["namespace"] == "acceptance-40f"
    assert app["files"]["originals_root"] == "/opt/memoh/originals"
    assert app["workspace_dependencies"]["offline"] is True
    assert app["session_runtime"] == {"backend": "memory", "cluster": False}
    assert app["pgvector"]["enabled"] is False
    assert app["connect_it"] == {"base_url": "", "api_token": ""}
    for section, key in (("postgres", "password"), ("admin", "password"),
                         ("auth", "jwt_secret"), ("auth", "agent_credentials_encryption_key"),
                         ("internal_rpc", "shared_secret")):
        assert app[section][key].startswith("REPLACE_WITH_ACCEPTANCE_ONLY_")
    assert IMAGES["revision"] in app["container"]["default_image"]
    print("PASS: TOML syntax, workspace import identity, isolated paths, placeholder-only secrets")
    print("NOT RUN: image startup/import, migrations, HTTP/UI/file roundtrip or deployment")


if __name__ == "__main__":
    check()
