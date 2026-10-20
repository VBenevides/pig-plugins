#!/usr/bin/env python3
"""Exercise real Smart Approve choice ordering, default Enter and cancellation."""
import fcntl
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
from pathlib import Path

master, slave = pty.openpty()
fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 38, 110, 0, 0))
def terminal():
    os.setsid()
    fcntl.ioctl(0, termios.TIOCSCTTY, 0)
process = subprocess.Popen([sys.argv[1], "--provider", "mock", "--model", "mock-model", "--offline"], stdin=slave, stdout=slave, stderr=slave, preexec_fn=terminal)
os.close(slave)
output = bytearray()
ansi = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)")
def wait(text):
    deadline = time.monotonic() + 15
    clean = ""
    while time.monotonic() < deadline:
        ready, _, _ = select.select([master], [], [], .05)
        if not ready:
            continue
        chunk = os.read(master, 65536)
        for query, reply in ((b"\x1b[6n", b"\x1b[1;1R"), (b"\x1b[c", b"\x1b[?1;2c"), (b"\x1b[16t", b"\x1b[6;18;9t")):
            if query in chunk:
                os.write(master, reply)
        output.extend(chunk)
        if len(output) > 4_000_000:
            raise AssertionError("TUI output exceeded bound")
        clean = ansi.sub("", output.decode("utf-8", "replace"))
        if text in clean:
            return clean
    raise AssertionError(f"waiting for {text!r}: {clean[-6000:]}")
def submit(text):
    output.clear()
    os.write(master, b"\x1b[200~" + text.encode() + b"\x1b[201~")
    wait(text)
    os.write(master, b"\r")
try:
    wait("smart-approve-lancet lax")
    for name, keys in (("once", b"\r"), ("deny", b"\x1b[B\r"), ("q", b"q"), ("escape", b"\x1b"), ("ctrl", b"\x03")):
        submit("approval " + name)
        clean = wait("Always allow this command and item")
        start = clean.index("Protected path:")
        choices = clean[start:]
        allow_position = list(re.finditer(r"\bAllow\b", choices))[-1].start()
        assert allow_position < choices.index("Deny") < choices.index("Always allow this command and item"), choices
        if name == "q":
            fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack("HHHH", 29, 100, 0, 0))
            os.killpg(process.pid, signal.SIGWINCH)
            os.write(master, b"\x1b[6~\x1b[5~")
        os.write(master, keys)
        wait("settled " + name)
        path = Path(".env." + name)
        if name == "once":
            assert path.read_text() == "allowed once"
            assert not (Path.home() / ".pig/agent/smart-approve-lancet-allow.json").exists()
        else:
            assert not path.exists(), "denied write executed: " + name
    print("PASS: Allow/Deny/Always order, default Enter allows once without grant, deny and q/Escape/Ctrl+C cancellation after resize")
finally:
    if process.poll() is None:
        os.killpg(process.pid, signal.SIGTERM)
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        os.killpg(process.pid, signal.SIGKILL)
        process.wait(timeout=5)
    os.close(master)
