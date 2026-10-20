#!/usr/bin/env python3
"""Credential-isolated POSIX fused-TUI regression against a loopback Claude stub.

Usage: python3 scripts/test_fused_diagnostics.py /absolute/path/to/patched-pig
No installed binary, user config, credentials, or remote model service is used.
"""

import fcntl
import http.server
import json
import os
import pty
import select
import re
import struct
import subprocess
import sys
import tempfile
import termios
import threading
import time
from pathlib import Path


def check(binary, work):
    bodies = []
    lock = threading.Lock()
    first_request = threading.Event()
    release_error = threading.Event()

    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, format, *args):
            pass  # The synthetic responses and assertions are the test output.

        def do_POST(self):
            self.connection.settimeout(15)
            length = int(self.headers.get("Content-Length", "0"))
            if not 0 <= length <= 2 * 1024 * 1024:
                self.send_error(413)
                return
            body = json.loads(self.rfile.read(length))
            with lock:
                if len(bodies) >= 8:
                    self.send_error(500, "fixture request budget exceeded")
                    return
                bodies.append(body)
                attempt = len(bodies)
            if attempt == 1:
                first_request.set()
                if not release_error.wait(15):
                    self.send_error(500, "fixture synchronization timeout")
                    return
            code = 429 if attempt <= 2 else 400
            payload = json.dumps(
                {
                    "type": "error",
                    "error": {
                        "type": "rate_limit_error"
                        if code == 429
                        else "invalid_request_error",
                        "message": "fixture-rate-limit"
                        if code == 429
                        else "fixture-stop",
                    },
                }
            ).encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)

    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    agent = work / "agent"
    agent.mkdir()
    (agent / "models.json").write_text(
        json.dumps(
            {
                "providers": {
                    "diagnostic-fixture": {
                        "api": "anthropic-messages",
                        "baseUrl": f"http://127.0.0.1:{server.server_port}",
                        "apiKey": "synthetic-fixture-only",
                        "models": [{"id": "fake-claude", "name": "fake-claude"}],
                    }
                }
            }
        )
    )
    (agent / "settings.json").write_text(
        json.dumps(
            {
                "quietStartup": True,
                "compaction": {"enabled": False},
                "retry": {
                    "enabled": True,
                    "maxRetries": 1,
                    "baseDelayMs": 1,
                    "maxAgentDelayMs": 1,
                    "provider": {"maxRetries": 0},
                },
            }
        )
    )
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
    process = None
    transcript = bytearray()

    def pump_until(predicate, description, timeout=15):
        if process is None:
            raise AssertionError("TUI process not started")
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            readable, _, _ = select.select([master], [], [], 0.05)
            if readable:
                try:
                    chunk = os.read(master, 65536)
                except OSError as error:
                    raise AssertionError(
                        f"PTY closed while waiting for {description}"
                    ) from error
                if not chunk:
                    raise AssertionError(f"PTY EOF while waiting for {description}")
                transcript.extend(chunk)
                if len(transcript) > 4 * 1024 * 1024:
                    raise AssertionError("TUI output exceeded fixture limit")
            if predicate():
                return
            if process.poll() is not None:
                raise AssertionError(
                    f"TUI exited {process.returncode}: {transcript[-2000:]!r}"
                )
        text = re.sub(
            r"\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))",
            "",
            transcript.decode("utf-8", errors="replace"),
        )
        visible = "\n".join(
            dict.fromkeys(line.strip() for line in text.splitlines() if line.strip())
        )
        raise AssertionError(
            f"timeout waiting for {description}: {visible[-12000:]}"
        )

    try:
        env = {
            "PATH": os.environ["PATH"],
            "HOME": str(work),
            "TERM": "xterm-256color",
            "PIG_CODING_AGENT_DIR": str(agent),
            "PI_OFFLINE": "1",
        }
        process = subprocess.Popen(
            [
                str(binary),
                "--offline",
                "--no-extensions",
                "--no-skills",
                "--no-prompt-templates",
                "--no-context-files",
                "--no-tools",
                "--no-session",
                "--tui-mode",
                "regular",
                "--provider",
                "diagnostic-fixture",
                "--model",
                "fake-claude",
            ],
            cwd=work,
            env=env,
            stdin=slave,
            stdout=slave,
            stderr=slave,
            start_new_session=True,
        )
        os.close(slave)
        slave = None
        # Terminal capability negotiation may request replies before the editor starts.
        os.write(master, b"\x1b[?1;2c\x1b[1;1R")
        # 0.5.1 paints the model before managed-tool setup finishes; Enter is
        # deliberately rejected during that window. This badge is published
        # by session_start after the normal submit handler is installed.
        pump_until(
            lambda: b"fake-claude" in transcript and b"smart-approve-lancet lax" in transcript,
            "session/editor startup",
        )
        os.write(master, b"initial request")
        pump_until(lambda: b"initial request" in transcript, "initial draft rendering")
        os.write(master, b"\r")
        pump_until(first_request.is_set, "initial loopback request")
        os.write(master, b"draft marker\x1b[D\x1b[D")
        # Drain an editor render before releasing the synchronized provider error.
        pump_until(lambda: b"draft marker" in transcript, "draft rendering")
        release_error.set()
        pump_until(lambda: b"Retry failed" in transcript, "retry exhaustion")
        os.write(master, b"X\r")
        pump_until(lambda: len(bodies) >= 3, "post-error draft submission")
        with lock:
            captured = list(bodies)
        assert len(captured) == 3, f"unexpected request count: {len(captured)}"
        messages = captured[-1]["messages"]
        content = messages[-1]["content"]
        text = (
            content
            if isinstance(content, str)
            else "".join(
                block.get("text", "")
                for block in content
                if block.get("type") == "text"
            )
        )
        assert text == "draft markXer", f"draft/cursor changed by errors: {text!r}"
        assert "fixture-rate-limit" not in json.dumps(messages), (
            "error injected into model input"
        )
        assert b"fixture-rate-limit" in transcript, "provider failure hidden from TUI"
        print(
            "PASS: fused TUI keeps Claude 429/retry errors visible and preserves draft/cursor/Enter input"
        )
    finally:
        release_error.set()
        if process is not None and process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)
        if slave is not None:
            os.close(slave)
        os.close(master)
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)


def main():
    if len(sys.argv) != 2:
        raise SystemExit(__doc__)
    binary = Path(sys.argv[1]).resolve(strict=True)
    root = Path(__file__).resolve().parent.parent / ".agent-work" / "tmp"
    root.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(
        prefix="fused-diagnostics-", dir=root
    ) as directory:
        check(binary, Path(directory))


if __name__ == "__main__":
    main()
