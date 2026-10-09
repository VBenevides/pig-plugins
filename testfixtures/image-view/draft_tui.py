"""Exercise image paste, gallery resize, and real submission in the bundled editor."""
import fcntl
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
import time

binary, image_path = sys.argv[1:]
protocol = os.environ.get("PIG_TEST_PREVIEW_PROTOCOL", "none")
master, slave = pty.openpty()
fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))


def claim_terminal():
    os.setsid()
    fcntl.ioctl(0, termios.TIOCSCTTY, 0)


# A private backend exercises Ctrl+V without reading or changing the real clipboard.
clipboard_dir = tempfile.TemporaryDirectory(prefix="pig-image-clipboard-")
backend = os.path.join(clipboard_dir.name, "wl-paste")
with open(backend, "w", encoding="utf-8") as script:
    script.write(
        f"#!{sys.executable}\nimport os, sys\n"
        "if '--list-types' in sys.argv:\n    print('image/png')\n"
        "elif '--type' in sys.argv:\n"
        "    with open(os.environ['PIG_TEST_CLIPBOARD_IMAGE'], 'rb') as image:\n"
        "        sys.stdout.buffer.write(image.read())\n"
        "else:\n    sys.exit(1)\n"
    )
os.chmod(backend, 0o700)
environment = dict(os.environ, PATH=clipboard_dir.name + os.pathsep + os.environ.get("PATH", ""),
                   WAYLAND_DISPLAY="pig-test-clipboard", PIG_TEST_CLIPBOARD_IMAGE=image_path)
process = subprocess.Popen(
    [binary, "--provider", "mock", "--model", "mock-model", "--no-session"],
    stdin=slave, stdout=slave, stderr=slave, preexec_fn=claim_terminal, env=environment,
)
os.close(slave)
output = bytearray()
ansi = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)")


def wait_for(predicate, description):
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise RuntimeError(f"PiG exited {process.returncode}: {output[-12000:]!r}")
        readable, _, _ = select.select([master], [], [], 0.1)
        if not readable:
            continue
        chunk = os.read(master, 65536)
        if b"\x1b[6n" in chunk:
            os.write(master, b"\x1b[1;1R")
        if b"\x1b[c" in chunk:
            os.write(master, b"\x1b[?1;2c")
        if b"\x1b[16t" in chunk:
            os.write(master, b"\x1b[6;18;9t")
        output.extend(chunk)
        if len(output) > 2_000_000:
            raise RuntimeError("terminal output exceeded smoke-test bound")
        text = ansi.sub("", output.decode("utf-8", errors="replace"))
        if predicate(text):
            return text
    raise RuntimeError(f"timed out waiting for {description}: {output[-12000:]!r}")


def has_preview(text):
    if protocol == "kitty":
        return b"\x1b_Ga=T" in output
    if protocol == "iterm2":
        return b"\x1b]1337;File=" in output
    return "▀" in text


try:
    wait_for(lambda text: "mock-model" in text or "Mock" in text, "editor startup")
    os.write(master, ("\x1b[200~inspect " + image_path + "\x1b[201~").encode())
    wait_for(
        lambda text: "inspect [Image #1]" in text and has_preview(text),
        "automatic draft marker and image preview before submission",
    )
    output.clear()
    os.write(master, b" \x16")
    wait_for(
        lambda text: "inspect [Image #1] [Image #2]" in text
        and re.search(r"\[Image #1\] {2,}\[Image #2\]", text)
        and has_preview(text),
        "both draft previews side by side after Ctrl+V",
    )
    output.clear()
    fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 40, 0, 0))
    os.killpg(process.pid, signal.SIGWINCH)
    wait_for(
        lambda text: "[Image #1]" in text and "[Image #2]" in text and has_preview(text),
        "both draft previews after narrow resize",
    )
    # Submit immediately after an edit, without waiting for another 100 ms draft scan.
    output.clear()
    os.write(master, b"\x1b[200~ Explain these images\x1b[201~\r")
    wait_for(lambda text: "draft images delivered" in text, "model response after image submission")
    print(f"{protocol}: two draft previews wrapped on resize and were submitted to the model")
finally:
    if process.poll() is None:
        os.killpg(process.pid, signal.SIGTERM)
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        os.killpg(process.pid, signal.SIGKILL)
        process.wait(timeout=5)
    os.close(master)
    clipboard_dir.cleanup()
