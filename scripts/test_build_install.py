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
        self.tool("ast-grep", "exit 0\n")
        self.tool("git", '''case "$*" in
    *ls-files*) printf 'go.mod\\0piglet.yaml\\0prompts/agent/SYSTEM.md\\0prompts/agent/APPEND_SYSTEM.md\\0prompts/agent/AGENTS.md\\0' ;;
    *) exit 0 ;;
esac
''')
        self.tool("go", '''printf '%s|%s|%s\\n' "${GOFLAGS:-}" "${GOMAXPROCS:-}" "$*" >> "$TOOL_LOG"
case "$1" in
    run)
        if [ "$2" = ./cmd/install-ast-grep ]; then
            [ "${FAIL_AST_INSTALL:-0}" != 1 ] || { echo 'fixture dependency failed' >&2; exit 8; }
            mkdir -p "$3"
            printf '#!/bin/sh\\nexit 0\\n' > "$3/ast-grep"
            chmod +x "$3/ast-grep"
        else
            python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["dependencies"]["pig"]["version"])' "$3"
        fi
        ;;
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

    def test_extension_lock_uses_clean_module_snapshot(self):
        # Go factories resolve to the containing module, not the extension directory.
        (self.root / '.agent-work/python').symlink_to(self.tools)
        self.tool('builder', '''
module=$(dirname -- "$3")
for name in .git .agent-work build node_modules .ouro go.work go.work.sum; do
    [ ! -e "$module/$name" ] || { echo "unexpected lock input: $name" >&2; exit 42; }
done
[ -f "$module/go.mod" ]
[ -f "$module/prompts/agent/SYSTEM.md" ]
[ -f "$module/skills/example/SKILL.md" ]
while [ "$1" != --out ]; do shift; done
printf '#!/bin/sh\\nexit 0\\n' > "$2"
chmod +x "$2"
''')
        result = self.run_script('dev_build.sh')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue((self.root / '.agent-work/python').is_symlink())

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
        for stage in range(1, 8):
            self.assertIn(f"install: [{stage}/7]", result.stderr)
        self.assertTrue((Path(self.env["PIG_PLUGINS_LINK_DIR"]) / "pig-plugins").is_symlink())
        binary = Path(self.env["PIG_HOME"]) / "bin/pig-plugins"
        self.assertEqual(binary.read_text(), "#!/bin/sh\nexit 0\n")
        self.assertFalse((binary.parent / "pig-plugins-native").exists())

    def without_system_ast_grep(self):
        (self.tools / "ast-grep").unlink()
        utilities = Path(self.tmp.name) / "utilities"
        utilities.mkdir()
        for name in ("sh", "dirname", "tar", "mkdir", "mktemp", "date", "cmp",
                     "cp", "mv", "rm", "chmod", "ln", "basename", "diff", "cat", "python3"):
            target = shutil.which(name)
            assert target is not None, f"missing fixture utility: {name}"
            (utilities / name).symlink_to(target)
        self.env["PATH"] = f"{self.tools}:{utilities}"

    def test_install_provides_ast_grep_and_reuses_it(self):
        self.without_system_ast_grep()
        result = self.run_script("install.sh")
        self.assertEqual(result.returncode, 0, result.stderr)
        dependency = Path(self.env["PIG_HOME"]) / "bin/ast-grep"
        self.assertTrue(os.access(dependency, os.X_OK))
        self.assertEqual(Path(self.env["TOOL_LOG"]).read_text().count("run ./cmd/install-ast-grep"), 1)
        result = self.run_script("install.sh")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(Path(self.env["TOOL_LOG"]).read_text().count("run ./cmd/install-ast-grep"), 1)

    def test_install_preserves_external_ast_grep(self):
        before = (self.tools / "ast-grep").read_bytes()
        result = self.run_script("install.sh")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.tools / "ast-grep").read_bytes(), before)
        self.assertFalse((Path(self.env["PIG_HOME"]) / "bin/ast-grep").exists())
        self.assertNotIn("run ./cmd/install-ast-grep", Path(self.env["TOOL_LOG"]).read_text())

    def test_dependency_failure_stops_before_build_and_prompts(self):
        self.without_system_ast_grep()
        self.env["FAIL_AST_INSTALL"] = "1"
        result = self.run_script("install.sh")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("fixture dependency failed", result.stderr)
        self.assertIn("ast-grep installation failed", result.stderr)
        self.assertNotIn("build -v", Path(self.env["TOOL_LOG"]).read_text())
        self.assertFalse((Path(self.env["PIG_HOME"]) / "agent/SYSTEM.md").exists())

    def test_launcher_exposes_managed_ast_grep(self):
        self.without_system_ast_grep()
        home_bin = Path(self.env["PIG_HOME"]) / "bin"
        home_bin.mkdir(parents=True)
        dependency = home_bin / "ast-grep"
        dependency.write_text("#!/bin/sh\necho managed-dependency\n")
        dependency.chmod(0o755)
        native = home_bin / "pig-plugins"
        native.write_text('#!/bin/sh\nif [ "$1" = --version ]; then echo fixture; else ast-grep; fi\n')
        native.chmod(0o755)
        result = self.run_script("pig-plugins.sh", "-p", "hello")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "managed-dependency\n")

    def test_install_failure_does_not_install_prompts(self):
        self.env["FAIL_BUILD"] = "1"
        result = self.run_script("install.sh")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("fixture compile failed", result.stderr)
        self.assertIn("install: build failed", result.stderr)
        self.assertFalse((Path(self.env["PIG_HOME"]) / "agent/SYSTEM.md").exists())

    def test_install_retires_legacy_payloads_only_after_success(self):
        home_bin = Path(self.env["PIG_HOME"]) / "bin"
        home_bin.mkdir(parents=True)
        for name in ("pig-plugins", "pig-plugins-native", "pig-plugins-raw"):
            (home_bin / name).write_text("previous binary")
        self.env["FAIL_BUILD"] = "1"
        result = self.run_script("install.sh")
        self.assertNotEqual(result.returncode, 0)
        for name in ("pig-plugins", "pig-plugins-native", "pig-plugins-raw"):
            self.assertEqual((home_bin / name).read_text(), "previous binary")
        self.env.pop("FAIL_BUILD")
        result = self.run_script("install.sh")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((home_bin / "pig-plugins").read_text(), "#!/bin/sh\nexit 0\n")
        self.assertFalse((home_bin / "pig-plugins-native").exists())
        self.assertFalse((home_bin / "pig-plugins-raw").exists())

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

    def test_source_shim_preserves_arguments(self):
        binary = Path(self.env["PIG_HOME"]) / "bin/pig-plugins"
        binary.parent.mkdir(parents=True)
        binary.write_text('#!/bin/sh\nprintf "%s|%s" "$1" "$2"\n')
        binary.chmod(0o755)
        result = self.run_script("pig-plugins.sh", "-p", "hello world")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "-p|hello world")

    def test_missing_compatibility_stops_before_build(self):
        (self.root / "COMPATIBILITY.json").unlink()
        result = self.run_script("install.sh")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((Path(self.env["PIG_HOME"]) / "bin/pig-plugins").exists())
        self.assertNotIn("build -v", Path(self.env["TOOL_LOG"]).read_text())


class PatchInventoryTest(unittest.TestCase):
    def patch_references(self, script):
        text = (ROOT / "scripts" / script).read_text()
        return [line.split('patches/pig/', 1)[1].split('"', 1)[0]
                for line in text.splitlines() if ' apply ' in line and 'patches/pig/' in line]

    def test_every_patch_is_used_and_numbered_consecutively(self):
        patches = sorted(path.name for path in (ROOT / "patches/pig").glob("*.patch"))
        references = self.patch_references("dev_build.sh")
        self.assertEqual(len(references), len(set(references)))
        self.assertEqual(set(patches), set(references))
        self.assertEqual([int(name.split("-", 1)[0]) for name in patches],
                         list(range(1, len(patches) + 1)))

    def test_development_and_build_use_the_same_sdk_patches(self):
        build_sdk = [name for name in self.patch_references("dev_build.sh")
                     if name.endswith("-sdk.patch")]
        self.assertEqual(build_sdk, self.patch_references("gowork.sh"))


if __name__ == "__main__":
    unittest.main()
