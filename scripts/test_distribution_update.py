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
        self.downloads = self.home / "downloads"
        self.downloads.mkdir()
        self.env = dict(os.environ, HOME=str(self.home), PIG_HOME=str(self.home / ".pig"),
                        PIG_CODING_AGENT_DIR=str(self.home / ".pig/agent"),
                        PATH=f"{self.tools}:{os.environ['PATH']}", PI_OFFLINE="1",
                        UPDATE_MARKER=str(self.marker), TMPDIR=str(self.downloads))
        self.binary = str(Path(os.environ["PIG_PLUGINS_TEST_BINARY"]).resolve())
        curl = self.tools / "curl"
        curl.write_text('''#!/bin/sh
set -eu
while [ "$1" != -o ]; do url=$1; shift; done
case "$url" in
  */git/ref/heads/main)
    printf '{"object":{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}' > "$2" ;;
  */aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/VERSION)
    printf '%s' "${REMOTE_VERSION:-999.0.0}" > "$2" ;;
  */aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/scripts/install.sh)
    printf '%s\\n' 'printf "%s|%s|%s" "$PIG_PLUGINS_UPDATE" "$PIG_PLUGINS_HOST_VERSION" "$PIG_PLUGINS_COMMIT" > "$UPDATE_MARKER"' 'exit "${INSTALL_EXIT:-0}"' > "$2"
    exit "${DOWNLOAD_EXIT:-0}" ;;
  *) exit 1 ;;
esac
''')
        curl.chmod(0o755)

    def run_binary(self, *args):
        return subprocess.run([self.binary, *args], env=self.env, cwd=self.home,
                              capture_output=True, text=True, timeout=30)

    def test_self_update_routes(self):
        for args in (("update",), ("--update",), ("update", "--self"),
                     ("update", "pig"), ("update", "--force"),
                     ("update", "--self", "--force"),
                     ("update", "--self", "--extensions"),
                     ("update", "--self", "--extensions", "--force")):
            with self.subTest(args=args):
                self.marker.unlink(missing_ok=True)
                result = self.run_binary(*args)
                self.assertEqual(result.returncode, 0, result.stderr)
                marker = self.marker.read_text()
                self.assertTrue(marker.startswith("1|"), marker)
                self.assertGreater(len(marker), 2)
                self.assertTrue(marker.endswith("|" + "a" * 40), marker)
                self.assertFalse(list(self.downloads.glob("pig-plugins-update.*")))

    def test_equal_or_older_remote_does_not_install(self):
        local = (Path(__file__).resolve().parent.parent / "VERSION").read_text().strip()
        for remote in (local, "0.0.0"):
            with self.subTest(remote=remote):
                self.env["REMOTE_VERSION"] = remote
                result = self.run_binary("--update")
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn("no update needed", result.stdout)
                self.assertFalse(self.marker.exists())
                self.assertFalse(list(self.downloads.glob("pig-plugins-update.*")))

    def test_invalid_remote_does_not_install(self):
        self.env["REMOTE_VERSION"] = "not-a-version"
        result = self.run_binary("--update")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("compare plugins versions", result.stderr)
        self.assertFalse(self.marker.exists())

    def test_installer_failure_cleans_up_and_can_retry(self):
        self.env["INSTALL_EXIT"] = "9"
        result = self.run_binary("update")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("update pig-plugins", result.stderr)
        self.assertTrue(self.marker.exists())
        self.assertFalse(list(self.downloads.glob("pig-plugins-update.*")))
        self.env.pop("INSTALL_EXIT")
        result = self.run_binary("update")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(list(self.downloads.glob("pig-plugins-update.*")))

    def test_partial_download_does_not_execute(self):
        self.env["DOWNLOAD_EXIT"] = "22"
        result = self.run_binary("update")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("download pig-plugins installer", result.stderr)
        self.assertFalse(self.marker.exists())
        self.assertFalse(list(self.downloads.glob("pig-plugins-update.*")))

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
