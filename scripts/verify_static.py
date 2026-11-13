#!/usr/bin/env python3
"""Inspect a built ELF artifact without executing it. CGO=0 alone is not proof."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys


def command(args):
    try:
        result = subprocess.run(
            args, check=True, text=True, capture_output=True, timeout=30,
            env={**os.environ, "LC_ALL": "C"},
        )
    except (OSError, subprocess.SubprocessError) as error:
        detail = getattr(error, "stderr", "")
        raise RuntimeError(f"artifact inspection failed: {args[0]}: {error}: {detail}") from error
    return result.stdout


def inspect(binary):
    binary = Path(binary).resolve()
    digest = hashlib.sha256()
    with binary.open("rb") as stream:
        if stream.read(4) != b"\x7fELF":
            raise ValueError(f"not an ELF artifact: {binary}")
        stream.seek(0)
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    program = command(["readelf", "-lW", str(binary)])
    dynamic = command(["readelf", "-dW", str(binary)])
    metadata = command(["go", "version", "-m", str(binary)])
    needed = re.findall(r"\(NEEDED\).*?\[(.*?)\]", dynamic)
    interpreter = re.findall(r"Requesting program interpreter:\s*([^\]]+)", program)
    has_interp = bool(re.search(r"^\s*INTERP\s", program, re.MULTILINE))
    return {
        "path": str(binary), "sha256": digest.hexdigest(),
        "fully_static": not has_interp and not needed,
        "has_interpreter": has_interp, "interpreter": interpreter,
        "needed": needed, "go_build_metadata": metadata,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", help="final installed or built ELF binary")
    args = parser.parse_args()
    try:
        report = inspect(args.binary)
    except (OSError, RuntimeError, ValueError) as error:
        print(f"verify-static: {error}", file=sys.stderr)
        return 2
    print(json.dumps(report, indent=2))
    if not report["fully_static"]:
        print("verify-static: dynamic interpreter or shared libraries remain; CGO_ENABLED=0 is insufficient", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
