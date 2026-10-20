#!/usr/bin/env python3
"""Exercise literal slash input and questionnaire cancellation in a real bundle."""
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

master, slave = pty.openpty()
fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 36, 110, 0, 0))
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
            return
    raise AssertionError(f"waiting for {text!r}: {clean[-5000:]}")
def submit(text):
    output.clear()
    os.write(master, b"\x1b[200~" + text.encode() + b"\x1b[201~")
    wait(text)
    os.write(master, b"\r")
try:
    wait("smart-approve-lancet on")
    submit("/this-is-literal")
    wait("literal accepted")
    for key, answer in ((b"q", "first cancelled"), (b"\x1b", "second cancelled")):
        submit("ask a question")
        wait("Fixture choice?")
        fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack("HHHH", 28, 75, 0, 0))
        os.killpg(process.pid, signal.SIGWINCH)
        os.write(master, key)
        wait(answer)
    submit("ask custom question")
    wait("Fixture choice?")
    os.write(master, b"\x1b[B\x1b[B")
    os.write(master, b"q")
    wait("q")
    os.write(master, b"\r")
    wait("custom accepted")
    print("PASS: literal slash, q/Escape cancellation, resize and custom q input")
finally:
    if process.poll() is None:
        os.killpg(process.pid, signal.SIGTERM)
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        os.killpg(process.pid, signal.SIGKILL)
        process.wait(timeout=5)
    os.close(master)
