#!/usr/bin/env python3
"""Small fixture-only regression checks; never compile or install real binaries."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parent.parent


class BuildScriptsTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name) / "repo"
        self.root.mkdir()
        (self.root / "scripts").mkdir()
        for name in ("dev_build.sh", "install.sh", "pig-plugins.sh"):
            shutil.copy2(ROOT / "scripts" / name, self.root / "scripts" / name)
        shutil.copy2(ROOT / "COMPATIBILITY.json", self.root / "COMPATIBILITY.json")
        # Source is an ancestor of build staging: unrestricted cp recursively
        # copies its own output. These fixtures must never reach staged source.
        for name in (".git", ".agent-work/worktrees/nested", "build/previous", "node_modules", ".ouro"):
            directory = self.root / name
            directory.mkdir(parents=True)
            (directory / "sentinel").write_text("do not copy")
        (self.root / "cmd/pig").mkdir(parents=True)
        (self.root / "coding/extension/host/subprocess").mkdir(parents=True)
        (self.root / "go.mod").write_text("module fixture\n")
        (self.root / "piglet.yaml").write_text("fixture\n")
        (self.root / "prompts/agent").mkdir(parents=True)
        for name in ("SYSTEM.md", "APPEND_SYSTEM.md", "AGENTS.md"):
            (self.root / "prompts/agent" / name).write_text("fixture\n")
        (self.root / "skills/example").mkdir(parents=True)
        (self.root / "skills/example/SKILL.md").write_text("fixture\n")
        self.sdk = Path(self.tmp.name) / "sdk"
        self.sdk.mkdir()
        (self.sdk / "go.mod").write_text("module sdk\n")
        self.tools = Path(self.tmp.name) / "tools"
        self.tools.mkdir()
        self.env = dict(os.environ, PATH=f"{self.tools}:{os.environ['PATH']}",
                        PIG_SOURCE_ROOT=str(self.root), SDK_FIXTURE=str(self.sdk),
                        PIG_HOME=str(Path(self.tmp.name) / "home"),
                        PIG_PLUGINS_LINK_DIR=str(Path(self.tmp.name) / "bin"),
                        TOOL_LOG=str(Path(self.tmp.name) / "tool.log"))
        for name in ("GOFLAGS", "GOMAXPROCS", "PIG_BUILD_JOBS", "PIG_CODING_AGENT_DIR"):
            self.env.pop(name, None)
        self.tool("pig", 'echo "pig 0.4.1"\n')
        self.tool("node", "exit 0\n")
        self.tool("git", '''case "$*" in
    *ls-files*) printf 'go.mod\\0piglet.yaml\\0prompts/agent/SYSTEM.md\\0prompts/agent/APPEND_SYSTEM.md\\0prompts/agent/AGENTS.md\\0' ;;
    *) exit 0 ;;
esac
''')
        self.tool("go", '''printf '%s|%s|%s\\n' "${GOFLAGS:-}" "${GOMAXPROCS:-}" "$*" >> "$TOOL_LOG"
case "$1" in
    run) python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["dependencies"]["pig"]["version"])' "$3" ;;
    list) echo "$SDK_FIXTURE" ;;
    build)
        for name in .git .agent-work build node_modules .ouro; do
            [ ! -e "$PWD/$name" ] || exit 42
        done
        [ "${FAIL_BUILD:-0}" != 1 ] || { echo 'fixture compile failed' >&2; exit 9; }
        while [ "$1" != -o ]; do shift; done
        cp "$BUILDER_FIXTURE" "$2"
        chmod +x "$2"
        ;;
esac
''')
        self.tool("builder", '''echo 'fixture fused progress' >&2
while [ "$1" != --out ]; do shift; done
printf '#!/bin/sh\\nexit 0\\n' > "$2"
chmod +x "$2"
''')
        self.env["BUILDER_FIXTURE"] = str(self.tools / "builder")

    def tool(self, name, body):
        path = self.tools / name
        path.write_text("#!/bin/sh\nset -eu\n" + body)
        path.chmod(0o755)

    def run_script(self, name, *args):
        return subprocess.run([str(self.root / "scripts" / name), *map(str, args)],
                              env=self.env, capture_output=True, text=True, timeout=10)

    def test_staging_progress_and_default_limits(self):
        out = self.root / "build/result"
        result = self.run_script("dev_build.sh", out)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, f"{out}\n")
        for stage in range(1, 7):
            self.assertIn(f"[{stage}/6]", result.stderr)
        self.assertIn("fixture fused progress", result.stderr)
        self.assertIn("-p=2|2|", Path(self.env["TOOL_LOG"]).read_text())
        self.assertFalse(list((self.root / "build").glob("dev-source.*")))

    def test_failure_preserves_existing_binary(self):
        out = self.root / "build/result"
        out.write_text("previous binary")
        self.env["FAIL_BUILD"] = "1"
        result = self.run_script("dev_build.sh", out)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("fixture compile failed", result.stderr)
        self.assertEqual(out.read_text(), "previous binary")
        self.assertFalse(list((self.root / "build").glob("dev-source.*")))

    def test_explicit_limits(self):
        self.env.update(PIG_BUILD_JOBS="3", GOMAXPROCS="4", GOFLAGS="-tags=fixture")
        result = self.run_script("dev_build.sh")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("-tags=fixture -p=3|4|", Path(self.env["TOOL_LOG"]).read_text())

    def test_install_streams_build_progress(self):
        result = self.run_script("install.sh")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("fixture fused progress", result.stderr)
        for stage in range(1, 7):
            self.assertIn(f"install: [{stage}/6]", result.stderr)
        self.assertTrue((Path(self.env["PIG_PLUGINS_LINK_DIR"]) / "pig-plugins").is_symlink())
        launcher = Path(self.env["PIG_HOME"]) / "bin/pig-plugins"
        self.assertIn("--update", launcher.read_text())
        self.assertTrue((launcher.parent / "pig-plugins-native").is_file())

    def test_install_failure_does_not_install_prompts(self):
        self.env["FAIL_BUILD"] = "1"
        result = self.run_script("install.sh")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("fixture compile failed", result.stderr)
        self.assertIn("install: build failed", result.stderr)
        self.assertFalse((Path(self.env["PIG_HOME"]) / "agent/SYSTEM.md").exists())

    def test_launcher_update_download_and_execution(self):
        self.tool("curl", '''while [ "$1" != -o ]; do shift; done
printf '#!/bin/sh\\nprintf "%%s" "$PIG_PLUGINS_UPDATE" > "$TOOL_LOG"\\n' > "$2"
''')
        result = self.run_script("pig-plugins.sh", "--update")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(Path(self.env["TOOL_LOG"]).read_text(), "1")

    def test_failed_update_download_is_not_executed(self):
        self.tool("curl", '''while [ "$1" != -o ]; do shift; done
printf 'touch "$TOOL_LOG"\\n' > "$2"
exit 22
''')
        result = self.run_script("pig-plugins.sh", "--update")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(Path(self.env["TOOL_LOG"]).exists())

    def test_update_reinstalls_compatible_pig(self):
        self.tool("curl", '''while [ "$1" != -o ]; do shift; done
printf '#!/bin/sh\\nprintf "%%s" "$PIG_VERSION" > "$TOOL_LOG"\\n' > "$2"
''')
        (self.root / "COMPATIBILITY.json").write_text(
            '{"dependencies":{"pig":{"path":"github.com/MichaelKinsy/PiG","version":"0.4.2"}}}\n')
        self.env["PIG_PLUGINS_UPDATE"] = "1"
        self.env["PIG_INSTALL_DIR"] = str(self.tools)
        result = self.run_script("install.sh")
        self.assertEqual(result.returncode, 0, result.stderr)
        log = Path(self.env["TOOL_LOG"]).read_text()
        self.assertTrue(log.startswith("0.4.2"))
        self.assertIn("extensions/sdk@v0.4.2", log)

    def test_launcher_preserves_arguments_and_host_version(self):
        native = Path(self.env["PIG_HOME"]) / "bin/pig-plugins-native"
        native.parent.mkdir(parents=True)
        native.write_text('#!/bin/sh\nif [ "$1" = --version ]; then echo 0.4.1+1.0.3; else printf "%s|%s|%s" "$PIG_PLUGINS_HOST_VERSION" "$1" "$2"; fi\n')
        native.chmod(0o755)
        result = self.run_script("pig-plugins.sh", "-p", "hello world")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "0.4.1+1.0.3|-p|hello world")

    def test_missing_compatibility_stops_before_build(self):
        (self.root / "COMPATIBILITY.json").unlink()
        result = self.run_script("install.sh")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((Path(self.env["PIG_HOME"]) / "bin/pig-plugins-native").exists())
        self.assertNotIn("build -v", Path(self.env["TOOL_LOG"]).read_text())


if __name__ == "__main__":
    unittest.main()
