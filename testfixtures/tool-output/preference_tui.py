"""Verify real Ctrl+O rendering, preference persistence, and session resume."""
import errno
import fcntl
import json
import os
import pty
import re
import select
import signal
import struct
import subprocess
import sys
import termios
import time

binary, settings_path = sys.argv[1:]
ansi = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)")


def claim_terminal():
    os.setsid()
    fcntl.ioctl(0, termios.TIOCSCTTY, 0)


def run_session(resume):
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 80, 120, 0, 0))
    args = [binary, "--provider", "mock", "--model", "mock-model"]
    if resume:
        args.append("--continue")
    process = subprocess.Popen(args, stdin=slave, stdout=slave, stderr=slave,
                               preexec_fn=claim_terminal)
    os.close(slave)
    output = bytearray()

    def wait_for(predicate, description):
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            if process.poll() is not None:
                raise RuntimeError(f"host exited {process.returncode}: {output[-12000:]!r}")
            readable, _, _ = select.select([master], [], [], 0.05)
            if readable:
                chunk = os.read(master, 65536)
                if b"\x1b[6n" in chunk:
                    os.write(master, b"\x1b[1;1R")
                if b"\x1b[c" in chunk:
                    os.write(master, b"\x1b[?1;2c")
                if b"\x1b[16t" in chunk:
                    os.write(master, b"\x1b[6;18;9t")
                output.extend(chunk)
            if len(output) > 2_000_000:
                raise RuntimeError("terminal output exceeded fixture bound")
            text = ansi.sub("", output.decode("utf-8", errors="replace"))
            if predicate(text):
                return text
        raise RuntimeError(f"timed out waiting for {description}: {output[-12000:]!r}")

    def saved_preference(value):
        try:
            with open(settings_path, encoding="utf-8") as settings:
                return json.load(settings).get("toolsExpanded") is value
        except json.JSONDecodeError as error:
            # The host's locked settings writer truncates in place. This external
            # reader retries an in-progress write only within wait_for's deadline.
            print(f"Preference read during save; retrying: {error}", file=sys.stderr)
            return False

    try:
        if resume:
            wait_for(lambda text: "TOOL-ROW-01" in text and "TOOL-ROW-30" in text,
                     "resumed expanded tool output")
            output.clear()
            os.write(master, b"\x0f")
            wait_for(lambda _: saved_preference(False), "persisted collapsed preference")
        else:
            wait_for(lambda text: "mock-model" in text or "Mock" in text, "editor startup")
            output.clear()
            os.write(master, b"show tool output\r")
            text = wait_for(lambda text: "tool-preview-done" in text, "tool completion")
            if len(set(re.findall(r"TOOL-ROW-\d+", text))) > 10:
                raise RuntimeError("default collapsed tool preview showed more than ten rows")
            output.clear()
            os.write(master, b"\x0f")
            wait_for(lambda text: "TOOL-ROW-01" in text and "TOOL-ROW-30" in text
                     and saved_preference(True), "expanded output and saved preference")
        os.write(master, b"/quit\r\r")
        # Drain shutdown rendering so a full-screen repaint cannot fill the PTY
        # and block the host before it reaches its joined preference worker.
        deadline = time.monotonic() + 10
        while process.poll() is None and time.monotonic() < deadline:
            readable, _, _ = select.select([master], [], [], 0.05)
            if readable:
                try:
                    output.extend(os.read(master, 65536))
                    if len(output) > 2_000_000:
                        raise RuntimeError("shutdown output exceeded fixture bound")
                except OSError as error:
                    if error.errno != errno.EIO:
                        raise
                    break  # Linux reports EIO when the PTY slave closes.
        if time.monotonic() >= deadline and process.poll() is None:
            raise RuntimeError(f"timed out waiting for /quit: {output[-12000:]!r}")
        process.wait(timeout=max(0.1, deadline - time.monotonic()))
        if process.returncode != 0:
            raise RuntimeError(f"host shutdown failed: {process.returncode}")
    finally:
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
        os.close(master)


run_session(False)
run_session(True)
print("Ctrl+O expands output, persists across restart/resume, and collapses again")
