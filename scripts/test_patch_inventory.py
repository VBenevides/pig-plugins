#!/usr/bin/env python3
"""Check the supported patch inventory and current script/document references."""
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parent.parent
PATCHES = [
    "0001-accounts-host.patch",
    "0002-accounts-sdk.patch",
    "0003-editor-images-host.patch",
    "0004-editor-images-sdk.patch",
    "0005-provider-recovery-host.patch",
    "0006-provider-recovery-sdk.patch",
    "0007-distribution-host.patch",
    "0008-settings-diagnostics-host.patch",
    "0009-compaction-sdk.patch",
    "0010-tmux-images-host.patch",
    "0011-editor-navigation-host.patch",
]

class PatchInventoryTest(unittest.TestCase):
    def test_exact_inventory(self):
        self.assertEqual(sorted(p.name for p in (ROOT / "patches/pig").glob("*.patch")), PATCHES)
        for name in PATCHES:
            text = (ROOT / "patches/pig" / name).read_text()
            self.assertIn("diff --git a/", text, name)
            paths = re.findall(r"^diff --git a/(.*?) b/", text, re.M)
            self.assertEqual(len(paths), len(set(paths)), name)

    def test_build_and_workspace_cover_each_patch_once_in_order(self):
        for script, expected in (
            ("scripts/dev_build.sh", [p for p in PATCHES if p.endswith("-host.patch")] + [p for p in PATCHES if p.endswith("-sdk.patch")]),
            ("scripts/gowork.sh", [p for p in PATCHES if p.endswith("-sdk.patch")]),
        ):
            actual = re.findall(r"patches/pig/([\w-]+\.patch)", (ROOT / script).read_text())
            self.assertEqual(actual, expected, script)

    def test_current_document_references_resolve(self):
        for name in ("README.md", "docs/cursor-direct.md", "docs/pig-051-port.md"):
            for patch in re.findall(r"\b\d{4}-[\w-]+\.patch", (ROOT / name).read_text()):
                self.assertIn(patch, PATCHES, name)

if __name__ == "__main__":
    unittest.main()
