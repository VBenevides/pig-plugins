#!/usr/bin/env python3
"""Smoke-test a built distribution without downloading or installing anything.

PIG_PLUGINS_TEST_BINARY=/absolute/path/to/pig-plugins python3 scripts/test_distribution_update.py
"""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


@unittest.skipUnless(os.environ.get("PIG_PLUGINS_TEST_BINARY"), "set PIG_PLUGINS_TEST_BINARY to a fused build")
class DistributionUpdateTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.home = Path(self.tmp.name)
        self.tools = self.home / "tools"
        self.tools.mkdir()
        self.marker = self.home / "installed"
        self.env = dict(os.environ, HOME=str(self.home), PIG_HOME=str(self.home / ".pig"),
                        PIG_CODING_AGENT_DIR=str(self.home / ".pig/agent"),
                        PATH=f"{self.tools}:{os.environ['PATH']}", PI_OFFLINE="1",
                        UPDATE_MARKER=str(self.marker))
        self.binary = str(Path(os.environ["PIG_PLUGINS_TEST_BINARY"]).resolve())
        curl = self.tools / "curl"
        curl.write_text('''#!/bin/sh
set -eu
while [ "$1" != -o ]; do shift; done
printf '%s\n' 'printf "%s|%s" "$PIG_PLUGINS_UPDATE" "$PIG_PLUGINS_HOST_VERSION" > "$UPDATE_MARKER"' > "$2"
exit "${DOWNLOAD_EXIT:-0}"
''')
        curl.chmod(0o755)

    def run_binary(self, *args):
        return subprocess.run([self.binary, *args], env=self.env, cwd=self.home,
                              capture_output=True, text=True, timeout=30)

    def test_self_update_routes(self):
        for args in (("update",), ("--update",), ("update", "--self"),
                     ("update", "pig"), ("update", "--force")):
            with self.subTest(args=args):
                self.marker.unlink(missing_ok=True)
                result = self.run_binary(*args)
                self.assertEqual(result.returncode, 0, result.stderr)
                marker = self.marker.read_text()
                self.assertTrue(marker.startswith("1|"), marker)
                self.assertGreater(len(marker), 2)

    def test_partial_download_does_not_execute(self):
        self.env["DOWNLOAD_EXIT"] = "22"
        result = self.run_binary("update")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("download pig-plugins installer", result.stderr)
        self.assertFalse(self.marker.exists())

    def test_extensions_only_does_not_rebuild_distribution(self):
        result = self.run_binary("update", "--extensions")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(self.marker.exists())

    def test_update_alias_rejects_extra_arguments(self):
        result = self.run_binary("--update", "extra")
        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertFalse(self.marker.exists())


if __name__ == "__main__":
    unittest.main()
