"""Run the isolated fullscreen regression: python3 scripts/test_fused_interaction_pages.py /path/to/pig-plugins."""

import contextlib
import fcntl
import http.server
import json
import os
import pty
import re
import select
import signal
import struct
import subprocess
import sys
import tempfile
import termios
import threading
import time
import zlib
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
bodies = []
failures = []


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        pass  # Synthetic responses and assertions are the test output.

    def do_POST(self):
        try:
            self.connection.settimeout(15)
            n = int(self.headers.get("Content-Length", "0"))
            if not 0 < n < 2 * 1024 * 1024:
                self.send_error(413)
                return
            body = json.loads(self.rfile.read(n))
            bodies.append(body)
            index = len(bodies)
            if index > 4:
                self.send_error(500)
                return
            if index == 1:
                tool = next(
                    t["name"]
                    for t in body["tools"]
                    if t["name"].split("__")[-1] == "todo"
                )
                block = {
                    "type": "tool_use",
                    "id": "fixture-todo",
                    "name": tool,
                    "input": {},
                }
                data = json.dumps(
                    {"op": "init", "items": [f"panel item {i:02d}" for i in range(15)]}
                )
                delta = {"type": "input_json_delta", "partial_json": data}
                reason = "tool_use"
            else:
                block = {"type": "text", "text": ""}
                delta = {"type": "text_delta", "text": f"fixture answer {index}"}
                reason = "end_turn"
            events = [
                (
                    "message_start",
                    {
                        "type": "message_start",
                        "message": {
                            "id": f"fixture-{index}",
                            "type": "message",
                            "role": "assistant",
                            "model": "fake-claude",
                            "content": [],
                            "usage": {"input_tokens": 10, "output_tokens": 0},
                        },
                    },
                ),
                (
                    "content_block_start",
                    {"type": "content_block_start", "index": 0, "content_block": block},
                ),
                (
                    "content_block_delta",
                    {"type": "content_block_delta", "index": 0, "delta": delta},
                ),
                ("content_block_stop", {"type": "content_block_stop", "index": 0}),
                (
                    "message_delta",
                    {
                        "type": "message_delta",
                        "delta": {"stop_reason": reason, "stop_sequence": None},
                        "usage": {"output_tokens": 5},
                    },
                ),
                ("message_stop", {"type": "message_stop"}),
            ]
            payload = "".join(
                f"event: {event}\ndata: {json.dumps(data)}\n\n"
                for event, data in events
            ).encode()
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
        except (OSError, ValueError, KeyError, StopIteration) as error:
            failures.append(repr(error))
            self.send_error(500)


def stop_process(process):
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=5)
    # Stop extension children before the temporary home is removed.
    try:
        os.killpg(process.pid, signal.SIGKILL)
    except ProcessLookupError:
        return  # The process group has already exited.


def check(binary):
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    process = None
    master, slave = pty.openpty()
    transcript = bytearray()

    def pump(predicate, description, timeout=20):
        assert process is not None, "TUI process not started"
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if failures:
                raise AssertionError(failures)
            ready, _, _ = select.select([master], [], [], 0.05)
            if ready:
                try:
                    chunk = os.read(master, 65536)
                except OSError as error:
                    if process.poll() is None:
                        raise AssertionError("Failed to read the TUI") from error
                    break
                transcript.extend(chunk)
                if len(transcript) > 4 * 1024 * 1024:
                    raise AssertionError("TUI output exceeds fixture budget")
                # Reply to terminal queries without changing user input.
                if b"\x1b[c" in chunk or b"\x1b[>c" in chunk:
                    os.write(master, b"\x1b[?1;2c")
                if b"\x1b[6n" in chunk:
                    os.write(master, b"\x1b[1;1R")
            if predicate():
                return
            if process.poll() is not None:
                break
        text = re.sub(
            r"\x1b\[[0-?]*[ -/]*[@-~]", "", transcript.decode(errors="replace")
        )
        raise AssertionError(f"{description}: {text[-10000:]}")

    try:
        scratch = ROOT / ".agent-work" / "tmp"
        scratch.mkdir(parents=True, exist_ok=True)
        with contextlib.ExitStack() as stack:
            directory = stack.enter_context(
                tempfile.TemporaryDirectory(dir=scratch, prefix="fused-pages-")
            )
            work = Path(directory)
            picture = work / "retained.png"

            def png_chunk(kind, data):
                return (
                    struct.pack(">I", len(data))
                    + kind
                    + data
                    + struct.pack(">I", zlib.crc32(kind + data))
                )

            picture.write_bytes(
                b"\x89PNG\r\n\x1a\n"
                + png_chunk(b"IHDR", struct.pack(">IIBBBBB", 2, 2, 8, 6, 0, 0, 0))
                + png_chunk(
                    b"IDAT", zlib.compress((b"\x00" + b"\xff\x00\x00\xff" * 2) * 2)
                )
                + png_chunk(b"IEND", b"")
            )
            agent = work / "agent"
            agent.mkdir()
            (agent / "models.json").write_text(
                json.dumps(
                    {
                        "providers": {
                            "page-fixture": {
                                "api": "anthropic-messages",
                                "baseUrl": f"http://127.0.0.1:{server.server_port}",
                                "apiKey": "synthetic-fixture-only",
                                "models": [
                                    {
                                        "id": "fake-claude",
                                        "name": "fake-claude",
                                        "input": ["text", "image"],
                                    }
                                ],
                            }
                        }
                    }
                )
            )
            (agent / "settings.json").write_text(
                json.dumps(
                    {
                        "quietStartup": False,
                        "compaction": {"enabled": False},
                        "retry": {"enabled": False},
                    }
                )
            )
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
            env = {
                "PATH": os.environ["PATH"],
                "HOME": str(work),
                "TERM": "xterm-kitty",
                "TERM_PROGRAM": "kitty",
                "PIG_CODING_AGENT_DIR": str(agent),
                "PI_OFFLINE": "1",
            }
            process = subprocess.Popen(
                [
                    str(binary),
                    "--offline",
                    "--no-skills",
                    "--no-prompt-templates",
                    "--no-context-files",
                    "--no-session",
                    "--provider",
                    "page-fixture",
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
            stack.callback(stop_process, process)
            os.close(slave)
            slave = None
            pump(lambda: b"smart-approve-lancet lax" in transcript, "startup")
            pump(
                lambda: b"PiG - " in transcript and b"PiG Plugins - " in transcript,
                "startup header versions",
            )
            os.write(master, f"\x1b[200~first page prompt {picture} \x1b[201~".encode())
            pump(lambda: b"[Image #1]" in transcript, "draft image")
            os.write(master, b"\r")
            pump(lambda: b"fixture answer 2" in transcript, "first answer")
            pump(
                lambda: b"Interaction 1/1" in transcript and b"\x1b_Ga=T" in transcript,
                "first interaction and inline image",
            )
            assert b"Tasks" in transcript and b"panel item 02" in transcript
            assert (
                b"Pending (15)" in transcript and b"Done / Closed (0)" in transcript
            ), transcript.decode(errors="replace")[-8000:]
            os.write(master, b"second page prompt\r")
            pump(lambda: b"fixture answer 3" in transcript, "second answer")
            assert b"Page 2 / 2" in transcript
            assert "Alt+←/→ or Alt+H/L".encode() in transcript
            pump(lambda: b"Interaction 2/2" in transcript, "second interaction")
            picture.unlink()
            for blob in (agent / "image-view" / "blobs").glob("*"):
                blob.unlink()
            for previous, following in (
                (b"\x1bh", b"\x1bl"),
                (b"\x1b[1;3D", b"\x1b[1;3C"),
            ):
                mark = len(transcript)
                os.write(master, previous)
                pump(
                    lambda mark=mark: (
                        b"Interaction 1/2" in transcript[mark:]
                        and (
                            b"\x1b_Ga=T" in transcript[mark:]
                            or b"\x1b_Ga=p" in transcript[mark:]
                        )
                    ),
                    "previous page with retained image",
                )
                mark = len(transcript)
                os.write(master, following)
                pump(
                    lambda mark=mark: (
                        b"Page 2 / 2" in transcript[mark:]
                        and b"Interaction 2/2" in transcript[mark:]
                    ),
                    "next page",
                )
            assert len(bodies) == 3, len(bodies)
            encoded = json.dumps(bodies[-1]["messages"])
            assert "first page prompt" in encoded and "second page prompt" in encoded
            assert "fixture answer 2" in encoded and "fixture-todo" in encoded
            assert "Page 1 /" not in encoded and "click to focus" not in encoded
            print(
                "PASS: startup header, interaction footer, retained inline images, task columns, page keys, and complete model history"
            )
    finally:
        if slave is not None:
            os.close(slave)
        os.close(master)
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit(__doc__)
    check(Path(sys.argv[1]).resolve(strict=True))
