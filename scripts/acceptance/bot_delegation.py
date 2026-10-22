#!/usr/bin/env python3
"""Opt-in native Bot delegation regression against an isolated local server.

Requires Python websockets, Docker psql access to the isolated database, two ready
fixture Bots, and a private token file. All model responses and file bytes are
synthetic. This changes only the supplied fixture Bots' model settings. Never run
against production. See --help; the evidence output never contains credentials.
"""
import argparse
import asyncio
import base64
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

INPUT = b"delegation regression original\n"
OUTPUT = b"delegation regression specialist output\n"


def main():
    p = argparse.ArgumentParser(description=__doc__)
    for name in ["base-url", "source-bot", "target-bot", "token-file", "pg-container", "pg-user", "database", "evidence"]:
        p.add_argument("--" + name, required=True)
    p.add_argument("--model-port", type=int, default=18281)
    p.add_argument("--restart-helper", help="optional executable that restarts only this isolated server and queue")
    p.add_argument("--allow-isolated-fixture-writes", action="store_true")
    a = p.parse_args()
    if not a.allow_isolated_fixture_writes or urllib.parse.urlparse(a.base_url).hostname not in {"127.0.0.1", "localhost", "::1"}:
        p.error("explicit opt-in and loopback server are required")
    source, target = str(uuid.UUID(a.source_bot)), str(uuid.UUID(a.target_bot))
    assert source != target
    evidence = Path(a.evidence)
    evidence.mkdir(parents=True, exist_ok=True)
    token = Path(a.token_file).read_text().strip()
    errors, result = [], {}
    release = threading.Event()
    model_calls = []
    hashes = [hashlib.sha256(INPUT).hexdigest(), hashlib.sha256(OUTPUT).hexdigest()]

    def request(path, data=None, method=None, anonymous=False, headers=None, binary=False):
        h = {"Content-Type": "application/json"}
        if not anonymous:
            h["Authorization"] = "Bearer " + token
        h.update(headers or {})
        req = urllib.request.Request(a.base_url + path, data=data if isinstance(data, bytes) else None if data is None else json.dumps(data).encode(), headers=h, method=method)
        try:
            with urllib.request.urlopen(req, timeout=15) as res:
                body = res.read()
                return res.status, dict(res.headers), body if binary else json.loads(body) if body else None
        except urllib.error.HTTPError as e:
            return e.code, dict(e.headers), e.read() if binary else json.loads(e.read())

    def api(path, data=None, method=None):
        status, _, body = request(path, data, method)
        assert status < 300, (path, status, body.get("code") if isinstance(body, dict) else None)
        return body

    def sql(query):
        proc = subprocess.run(["docker", "exec", a.pg_container, "psql", "-U", a.pg_user, "-d", a.database, "-At", "-c", query], check=True, text=True, capture_output=True)
        return json.loads(proc.stdout.strip() or "[]")

    def runs(session):
        return sql("SELECT coalesce(json_agg(x),'[]'::json) FROM (SELECT run_id,state,invocation_id,error_code FROM session_runs WHERE session_id='" + session + "' ORDER BY created_at) x")

    def check(ok, message):
        if not ok:
            errors.append(message)
        print(("PASS " if ok else "FAIL ") + message, flush=True)

    class Model(http.server.BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_POST(self):
            req = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            messages = req.get("messages", [])
            last_user = next((str(m.get("content", "")) for m in reversed(messages) if m.get("role") == "user"), "")
            ids = [m.get("tool_call_id") for m in messages if m.get("role") == "tool"]
            specialist = req["model"].endswith("specialist")
            model_calls.append({"model": req["model"], "tool_result_ids": ids})
            call = None
            if specialist:
                if not release.wait(60):
                    text = "fixture deadline exceeded"
                elif "new-output" not in ids:
                    call = ("new-output", "send", {"platform": "web", "target": target, "attachments": [{"type": "file", "base64": base64.b64encode(OUTPUT).decode(), "mime": "text/plain", "name": "output.txt"}]})
                elif "return-input" not in ids:
                    call = ("return-input", "send", {"platform": "web", "target": target, "attachments": [{"type": "file", "content_hash": hashes[0], "mime": "text/plain", "name": "original.txt"}]})
                else:
                    text = "SPECIALIST_FINAL"
            elif "execution state:" in last_user:
                text = "SUPERVISOR_FINAL"
            elif "delegate" in ids:
                text = "SUPERVISOR_INITIAL_DONE"
            else:
                result["initial_admission"] = sql("SELECT json_build_object('state',r.state,'input_keys',(SELECT json_agg(k) FROM jsonb_object_keys(r.input_json) k),'input_asset_hashes',r.input_json#>'{resume,input_asset_hashes}','owner_matches',r.input_json#>>'{resume,user_id}'=s.created_by_user_id::text,'fence_matches',r.fencing_token=s.runtime_fencing_token,'history_asset_links',(SELECT count(*) FROM bot_history_message_assets a JOIN bot_history_messages m ON m.id=a.message_id WHERE m.session_id=s.id)) FROM session_runs r JOIN bot_sessions s ON s.id=r.session_id WHERE r.session_id='" + session + "' ORDER BY r.created_at DESC LIMIT 1")
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
    suffix = uuid.uuid4().hex[:8]
    provider = api("/providers", {"name": "delegation-regression-" + suffix, "client_type": "openai-completions", "config": {"base_url": f"http://127.0.0.1:{a.model_port}/v1", "api_key": "synthetic"}})
    for bot, role in [(source, "supervisor"), (target, "specialist")]:
        model = api("/models", {"model_id": "regression-" + role, "name": "regression-" + role, "provider_id": provider["id"], "type": "chat", "enable": True, "config": {"compatibilities": ["tool-call"], "context_window": 131072, "thinking_mode": "none"}})
        api("/bots/" + bot + "/settings", {"chat_model_id": model["id"], "chat_runtime": "model", "compaction_enabled": False, "reasoning_enabled": False, "tool_approval_config": {"enabled": False, "read": {"mode": "allow"}, "write": {"mode": "allow"}, "exec": {"mode": "allow"}}}, "PUT")
    session = api("/bots/" + source + "/sessions", {"type": "chat", "title": "Delegation attachment regression " + suffix})["id"]
    boundary = "regression-boundary"
    upload = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="original.txt"\r\nContent-Type: text/plain\r\n\r\n'.encode() + INPUT + f"\r\n--{boundary}--\r\n".encode())
    status, _, uploaded = request("/bots/" + source + "/media", upload, headers={"Content-Type": "multipart/form-data; boundary=" + boundary})
    assert status == 201 and uploaded["content_hash"] == hashes[0]
    status, _, _ = request("/bots/" + source + "/sessions/" + session + "/delegations", {"request_id": str(uuid.uuid4()), "target_bot_id": target, "message": "unlinked input must be rejected", "attachments": [{"content_hash": hashes[0]}]})
    check(status == 404, "Bot-owned upload without session/admitted input is rejected")

    async def send_initial():
        url = a.base_url.replace("http://", "ws://").replace("https://", "wss://")
        invocation = str(uuid.uuid4())
        async with websockets.connect(url + "/bots/" + source + "/web/ws", additional_headers={"Authorization": "Bearer " + token}) as ws:
            await ws.send(json.dumps({"type": "message", "session_id": session, "invocation_id": invocation, "text": "Delegate this newly attached original", "workspace_target_id": "native", "attachments": [{"type": "file", "content_hash": hashes[0], "mime": "text/plain", "name": "original.txt", "size": len(INPUT)}]}))
            deadline = time.time() + 45
            while time.time() < deadline:
                try:
                    await asyncio.wait_for(ws.recv(), .2)
                except asyncio.TimeoutError:
                    pass
                rows = await asyncio.to_thread(runs, session)
                if rows and rows[0]["state"] in {"completed", "failed", "aborted", "lost"}:
                    return rows[0]
        raise AssertionError("supervisor did not finish")

    try:
        initial = asyncio.run(send_initial())
        check(initial["state"] == "completed", "initial supervisor completes before specialist")
        base = "/bots/" + source + "/sessions/" + session + "/delegations"
        items = api(base)["items"]
        check(len(items) == 1, "native delegate_to_bot accepts a same-turn new attachment")
        if not items:
            # Continue the independent output chain after durable input exists.
            # This does not turn the failed native-submit assertion into a pass.
            items = [api(base, {"request_id": str(uuid.uuid4()), "target_bot_id": target, "message": "synthetic specialist task", "attachments": [{"content_hash": hashes[0]}]})]
        view = items[0]
        release.set()
        for _ in range(180):
            view = api(base + "/" + view["delegation_id"])
            if view["state"] in {"completed", "failed", "unknown"} and view["delivery_state"] in {"accepted", "unknown", "unavailable"}:
                break
            time.sleep(.2)
        check(view["state"] == "completed" and view["delivery_state"] == "accepted", "specialist completes and result delivery is accepted")
        for _ in range(100):
            source_runs = runs(session)
            if len(source_runs) >= 2 and all(x["state"] == "completed" for x in source_runs):
                break
            time.sleep(.2)
        check(len(source_runs) == 2 and all(x["state"] == "completed" for x in source_runs), "exactly one supervisor follow-up completes")
        check({x["content_hash"] for x in view["attachments"]} == set(hashes), "both original and new output appear in delegation results")
        assistant_hashes = sql("SELECT coalesce(json_agg(a.content_hash),'[]'::json) FROM bot_history_message_assets a JOIN bot_history_messages m ON m.id=a.message_id WHERE m.session_id='" + session + "' AND m.role='assistant'")
        check(set(assistant_hashes) == set(hashes), "supervisor final assistant preserves both attachment references")
        downloads = []
        for h, content in zip(hashes, [INPUT, OUTPUT]):
            path = base + "/" + view["delegation_id"] + "/attachments/" + h
            status, headers, data = request(path, binary=True)
            check(status == 200 and data == content, "delegation original bytes match " + h[:12])
            check(request(path, anonymous=True, binary=True)[0] == 401, "anonymous delegation attachment is rejected")
            downloads.append({"hash": h, "status": status, "bytes": len(data), "no_store": headers.get("Cache-Control") == "no-store"})
        owner_checks = [(source, hashes[0], 200), (target, hashes[0], 404), (target, hashes[1], 200), (source, hashes[1], 404)]
        for bot, h, expected in owner_checks:
            status, _, data = request("/bots/" + bot + "/media/" + h, binary=True)
            check(status == expected, "original owner separation " + h[:12] + " expected " + str(expected))
            if expected == 200:
                status, _, data = request("/bots/" + bot + "/media/" + h, headers={"Range": "bytes=0-4"}, binary=True)
                check(status == 206 and len(data) == 5, "original Range request returns 206 and five bytes")
        before = (source_runs, runs(view["target_session_id"]), len(model_calls))
        if a.restart_helper:
            # Simulate loss of the delivery acknowledgement, not business state.
            # Keep the actual PG ledger, AOF queue and synthetic model alive.
            updated = sql("WITH changed AS (UPDATE bot_delegations SET delivery_state='unknown',revision=revision+1 WHERE id='" + str(uuid.UUID(view["delegation_id"])) + "' AND state='completed' RETURNING id) SELECT json_agg(id) FROM changed")
            assert updated == [view["delegation_id"]]
            subprocess.run([str(Path(a.restart_helper).resolve())], check=True)
        with concurrent.futures.ThreadPoolExecutor(max_workers=16) as ex:
            retried = list(ex.map(lambda _: api(base + "/" + view["delegation_id"] + "/delivery", {}), range(16)))
        check(all(x["delegation_id"] == view["delegation_id"] for x in retried) and before == (runs(session), runs(view["target_session_id"]), len(model_calls)), "16 HTTP delivery retries preserve task and business execution")
        repeated = api(base, {"request_id": view["request_id"], "target_bot_id": target, "message": "synthetic specialist task", "attachments": [{"content_hash": hashes[0]}]})
        check(repeated["delegation_id"] == view["delegation_id"] and before == (runs(session), runs(view["target_session_id"]), len(model_calls)), "same request after reopening preserves original task and runs")
        if a.restart_helper:
            for h, content in zip(hashes, [INPUT, OUTPUT]):
                status, _, data = request(base + "/" + view["delegation_id"] + "/attachments/" + h, binary=True)
                check(status == 200 and data == content, "attachment remains readable after server and queue restart " + h[:12])
        result.update(source_bot=source, target_bot=target, source_session=session, view=view, source_runs=source_runs, downloads=downloads, model_calls=model_calls)
    finally:
        release.set()
        httpd.shutdown()
        result["failures"] = errors
        (evidence / "results.json").write_text(json.dumps(result, indent=2))
    if errors:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
