import importlib.util
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

_spec = importlib.util.spec_from_file_location(
    "verify_static", Path(__file__).with_name("verify_static.py")
)
assert _spec is not None and _spec.loader is not None
verify_static = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(verify_static)


class StaticInspectionTest(unittest.TestCase):
    def artifact(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        binary = Path(directory.name) / "binary"
        binary.write_bytes(b"\x7fELFfixture")
        return binary

    def test_cgo_zero_does_not_override_dynamic_evidence(self):
        with patch.object(
            verify_static,
            "command",
            side_effect=[
                "  INTERP 0x1\n [Requesting program interpreter: /lib/loader]\n",
                "0x1 (NEEDED) Shared library: [libc.so.6]",
                "build CGO_ENABLED=0",
            ],
        ):
            report = verify_static.inspect(self.artifact())
        self.assertFalse(report["fully_static"])
        self.assertEqual(report["needed"], ["libc.so.6"])
        self.assertEqual(report["interpreter"], ["/lib/loader"])

    def test_elf_without_interpreter_or_dependencies_is_static(self):
        with patch.object(
            verify_static,
            "command",
            side_effect=[
                " LOAD 0x0",
                "There is no dynamic section in this file.",
                "CGO_ENABLED=0",
            ],
        ):
            self.assertTrue(verify_static.inspect(self.artifact())["fully_static"])

    def test_non_elf_is_not_accepted(self):
        binary = self.artifact()
        binary.write_text("not ELF")
        with self.assertRaises(ValueError):
            verify_static.inspect(binary)

    def test_inspection_failure_is_observable(self):
        failure = subprocess.CalledProcessError(1, ["readelf"], stderr="bad image")
        with (
            patch.object(verify_static.subprocess, "run", side_effect=failure),
            self.assertRaisesRegex(RuntimeError, "bad image") as caught,
        ):
            verify_static.command(["readelf", "x"])
        self.assertIs(caught.exception.__cause__, failure)


if __name__ == "__main__":
    unittest.main()
