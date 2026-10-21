"""Exercise the exact published Pi CLI with synthetic HTTP and reader processes.
Usage: python test-contract.py /path/to/pi-1.2.0/dist/bundle/cli.js
No Windows/DPAPI claim: powershell.exe is a deliberately synthetic subprocess.
"""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

source = Path(__file__).resolve().parent
cli = Path(sys.argv[1]).resolve()
requests = []


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        requests.append((self.path, self.headers.get("Authorization"), body["model"]))
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        for delta, finish in [({"role": "assistant", "content": "SYNTHETIC_MODEL_OK"}, None), ({}, "stop")]:
            chunk = {"id": "synthetic", "object": "chat.completion.chunk", "created": 0,
                     "model": body["model"], "choices": [{"index": 0, "delta": delta, "finish_reason": finish}]}
            self.wfile.write(("data: " + json.dumps(chunk) + "\n\n").encode())
        self.wfile.write(b"data: [DONE]\n\n")


server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
threading.Thread(target=server.serve_forever, daemon=True).start()
try:
    with tempfile.TemporaryDirectory(prefix="pi credential fixture ") as temp:
        root = Path(temp)
        config = root / "private agent"
        config.mkdir()
        shutil.copy(source / "read-dpapi.ps1", config)
        # An unrelated provider is available, so a failed Magpie command must not select it.
        (config / "auth.json").write_text(json.dumps({"openai": {"type": "api_key", "key": "SYNTHETIC_OTHER"}}))
        shutil.copy(source / "settings.json.example", config / "settings.json")
        models = json.loads((source / "models.json.example").read_text())
        models["providers"]["magpie"]["baseUrl"] = f"http://127.0.0.1:{server.server_port}/v1"
        models["providers"]["openai"] = {"baseUrl": f"http://127.0.0.1:{server.server_port}/other"}
        (config / "models.json").write_text(json.dumps(models))
        reader = root / "powershell.exe"
        reader.write_text('''#!/bin/sh
[ "$1" = -NoLogo ] && [ "$2" = -NoProfile ] && [ "$3" = -NonInteractive ] && [ "$4" = -File ] || exit 8
[ "$5" = "$PI_CODING_AGENT_DIR/read-dpapi.ps1" ] && [ "$#" = 5 ] || exit 8
printf called >> "$PI_READER_CALLS"
case "$PI_READER_CASE" in
 success) printf SYNTHETIC_DPAPI_KEY ;;
 error) printf SYNTHETIC_DISCARDED_OUTPUT; printf SYNTHETIC_PRIVATE_DIAGNOSTIC >&2; exit 9 ;;
 empty) exit 0 ;;
esac
''')
        reader.chmod(0o700)
        env = dict(os.environ, PI_CODING_AGENT_DIR=str(config), PI_OFFLINE="1", PI_SKIP_VERSION_CHECK="1", PI_TELEMETRY="0",
                   PATH=str(root) + os.pathsep + os.environ["PATH"], PI_READER_CALLS=str(root / "calls"))
        for case in ("success", "error", "empty"):
            requests.clear()
            env["PI_READER_CASE"] = case
            (root / "calls").write_text("")
            result = subprocess.run(["node", str(cli), "--offline", "--provider", "magpie", "--model", "codex/gpt-6.1-sol",
                                     "--no-session", "--no-tools", "--no-extensions", "--no-skills", "--no-prompt-templates",
                                     "--no-context-files", "-p", "Synthetic contract test."],
                                    cwd=root, env=env, text=True, capture_output=True, timeout=30)
            assert (root / "calls").read_text(), "Reader was not invoked"
            combined = result.stdout + result.stderr
            assert not any(value in combined for value in ("SYNTHETIC_DPAPI_KEY", "SYNTHETIC_DISCARDED_OUTPUT", "SYNTHETIC_PRIVATE_DIAGNOSTIC")), "Credential subprocess output leaked"
            if case == "success":
                assert result.returncode == 0 and "SYNTHETIC_MODEL_OK" in result.stdout, "Pi request failed"
                assert requests == [("/v1/chat/completions", "Bearer SYNTHETIC_DPAPI_KEY", "codex/gpt-6.1-sol")], "Unexpected routing/auth"
            else:
                assert result.returncode != 0 and requests == [], "Credential failure did not fail closed"
            print(f"PASS Pi CLI {case}: routing, private stdout, no provider fallback")
finally:
    server.shutdown()
    server.server_close()
