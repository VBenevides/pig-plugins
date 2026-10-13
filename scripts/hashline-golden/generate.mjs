// Captures read/edit results from the ORIGINAL TypeScript harness-hashline into
// internal/hashline/testdata/ts-golden.json. The Go port is compared against this file only; it is never regenerated
// from Go. The TypeScript source is NOT part of this repository; pass its directory explicitly:
//   HASHLINE_TS_SOURCE=/path/to/harness-hashline node scripts/hashline-golden/generate.mjs
// Run from the repository root. See scripts/hashline-golden/README.md.
import { cpSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { createHash } from "node:crypto";

if (!process.env.HASHLINE_TS_SOURCE) {
  console.error("HASHLINE_TS_SOURCE must name the original TypeScript harness-hashline directory; no default location is assumed.");
  process.exit(2);
}
const SOURCE = resolve(process.env.HASHLINE_TS_SOURCE);
const OUT = join(process.cwd(), "internal/hashline/testdata/ts-golden.json");

// Digest of every source file (relative path + bytes, sorted) excluding node_modules and .git.
function treeDigest(root) {
  const files = [];
  const walk = (directory) => {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      if (entry.name === "node_modules" || entry.name === ".git") continue;
      const full = join(directory, entry.name);
      if (entry.isDirectory()) walk(full);
      else if (entry.isFile()) files.push(full);
    }
  };
  walk(root);
  const hash = createHash("sha256");
  for (const file of files.map((f) => f.slice(root.length + 1)).sort()) {
    hash.update(file + "\0").update(readFileSync(join(root, file))).update("\0");
  }
  return hash.digest("hex");
}
const digest = treeDigest(SOURCE);
if (process.argv.includes("--print-digest")) {
  console.log(digest);
  process.exit(0);
}
const provenance = JSON.parse(readFileSync(join(import.meta.dirname, "PROVENANCE.json"), "utf8"));
if (!provenance.sourceTreeSha256) {
  console.error("PROVENANCE.json has no pinned sourceTreeSha256; recover the original source, run with --print-digest, and pin it before regenerating.");
  process.exit(3);
}
if (provenance.sourceTreeSha256 !== digest) {
  console.error(`source digest ${digest} does not match pinned ${provenance.sourceTreeSha256}; refusing to regenerate.`);
  process.exit(4);
}

const work = realpathSync(mkdtempSync(join(tmpdir(), "hashline-golden-")));
const pkg = join(work, "pkg");
cpSync(SOURCE, pkg, { recursive: true });
// Stub of the host package: stock read is a marker, the mutation queue just runs the function.
const stub = join(work, "node_modules/@earendil-works/pi-coding-agent");
mkdirSync(stub, { recursive: true });
writeFileSync(join(stub, "package.json"), JSON.stringify({ name: "@earendil-works/pi-coding-agent", type: "module", main: "index.js" }));
writeFileSync(
  join(stub, "index.js"),
  `export const createReadToolDefinition = () => ({ execute: async () => ({ content: [{ type: "text", text: "<stock read>" }], details: {} }) });
export const withFileMutationQueue = async (_path, fn) => fn();`,
);

const tools = {};
const mod = await import(pathToFileURL(join(pkg, "index.ts")).href);
mod.default({ registerTool: (tool) => (tools[tool.name] = tool) });
const anchors = await import(pathToFileURL(join(pkg, "anchors.ts")).href);
const outline = await import(pathToFileURL(join(pkg, "outline.ts")).href);

const sha = (text) => createHash("sha1").update(text, "utf8").digest("hex").slice(0, 4);
const anchor = (content, line) => `${line}#${sha(anchors.parseFile(content).lines[line - 1].text)}`;
const PLACEHOLDER = "<CWD>";
const scrub = (text) => text.split(work).join(PLACEHOLDER);

function pythonSource(count) {
  const lines = ["import os", ""];
  for (let i = 0; i < count; i++) lines.push(`def func_${i}(value):`, `    total = value + ${i}`, "    return total", "");
  return lines.join("\n");
}

// ---- files used by read cases (text only; binary/image behavior is not delegated to TS in Go) ----
const readFiles = {
  "small.txt": "alpha\nbeta\n\n  gamma  \n",
  "fifty.txt": Array.from({ length: 50 }, (_, i) => `line ${i + 1}`).join("\n") + "\n",
  "crlf.txt": "a\r\nb\r\n\r\nc\r\n",
  "bom.txt": "\ufeffhead\nbody\n",
  "nonewline.txt": "x\ny",
  "unicode.txt": "café 😀\nnaïve ✓\n\ttabbed\n",
  "empty.txt": "",
  "newline-only.txt": "\n",
  "longline.txt": "short\n" + "é".repeat(1999) + "😀tail" + "\n" + "z".repeat(2500) + "\nlast\n",
  "big.py": pythonSource(150),
  "data.json": Array.from({ length: 1000 }, (_, i) => `  "k${i}": ${i},`).join("\n"),
  "wide.py": 'def a():\n    return "' + "x".repeat(30000) + '"\n',
  "long.txt": Array.from({ length: 3000 }, (_, i) => `row ${i + 1}`).join("\n"),
  "wide-lines.txt": Array.from({ length: 700 }, (_, i) => `${"w".repeat(100)} ${i + 1}`).join("\n"),
  "notes.md": ["# Title", "intro", "## A", "```", "# not a heading", "```", "## B", "", "### B1", "text", "# Next", ...Array.from({ length: 420 }, (_, i) => `filler ${i}`)].join("\n"),
  "big.ts": [
    'import x from "y";', "",
    "export interface Options {", "  a: string;", "}", "",
    "export type Id = string;", "",
    "export class Box<T> extends Base {", "  private value = 1;", "  constructor(a: number) {", "    this.value = a;", "  }",
    "  async get(): Promise<T> {", "    if (this.value) {", "      return this.value;", "    }", "  }", "}", "",
    "export async function load(path: string) {", "  return path;", "}",
    "export const run = async (a: number) => {", "  return a;", "};", "",
    "/*", "function hidden() {", "}", "*/", "// function line() {}",
    ...Array.from({ length: 420 }, (_, i) => `const filler${i} = ${i};`),
  ].join("\n"),
  "big.go": [
    "package a", "", "type T struct {", "\tX int", "}", "", "func (t *T) M() int {", "\treturn t.X", "}", "",
    "func F(", "\ta int,", ") {", "}", "",
    ...Array.from({ length: 420 }, (_, i) => `var filler${i} = ${i}`),
  ].join("\n"),
  "big.rs": [
    "pub struct S {", "    a: u8,", "}", "", "impl S {", "    #[inline]", "    pub fn new() -> Self {", "        S { a: 1 }", "    }", "}", "",
    "pub(crate) async fn run() {", "}", ...Array.from({ length: 420 }, (_, i) => `// filler ${i}`),
  ].join("\n"),
  "Big.java": [
    "public class A {", "    @Override", "    public void run(int a) {", "        foo(a);", "    }", "", "    A() {", "    }", "}",
    ...Array.from({ length: 420 }, (_, i) => `// filler ${i}`),
  ].join("\n"),
  "big.c": [
    "#include <x.h>", "static int add(int a, int b)", "{", "    return a + b;", "}", "", "struct P {", "    int x;", "};",
    ...Array.from({ length: 420 }, (_, i) => `/* filler ${i} */`),
  ].join("\n"),
  "big.sh": ["#!/bin/sh", "usage() {", "  echo hi", "}", "function other {", "  :", "}", ...Array.from({ length: 420 }, (_, i) => `# filler ${i}`)].join("\n"),
  "python-mixed.py": [
    "import os", "", "@dataclass", "class A:", "    x: int = 1", "", "    def f(self,", "          y):", "        def inner():", "            pass", "        return 1", "",
    "    @property", "    def g(self):", "        return 2", "", "def top(", "    a,", "):", "    return a", ...Array.from({ length: 420 }, (_, i) => `# filler ${i}`),
  ].join("\n"),
  "no-newline-big.txt": Array.from({ length: 450 }, (_, i) => `n${i}`).join("\n"),
};

const readCases = [];
const addRead = (name, file, params = {}) => readCases.push({ name, file, params: { path: file, ...params } });
for (const file of Object.keys(readFiles)) addRead(`bare ${file}`, file);
addRead("range middle", "fifty.txt", { offset: 10, limit: 3 });
addRead("range tail", "fifty.txt", { offset: 49 });
addRead("range head limit", "fifty.txt", { limit: 2 });
addRead("range limit beyond", "fifty.txt", { offset: 40, limit: 500 });
addRead("offset beyond end", "fifty.txt", { offset: 99 });
addRead("offset zero", "small.txt", { offset: 0 });
addRead("limit fractional", "small.txt", { limit: 1.5 });
addRead("offset on empty", "empty.txt", { offset: 1 });
addRead("range capped by lines", "long.txt", { offset: 1, limit: 3000 });
addRead("range capped by lines from 100", "long.txt", { offset: 100 });
addRead("range capped by bytes", "wide-lines.txt", { offset: 1, limit: 700 });
addRead("range of truncated long line", "longline.txt", { offset: 2, limit: 2 });
addRead("range on large file", "big.py", { offset: 3, limit: 4 });
addRead("range crlf", "crlf.txt", { offset: 2, limit: 2 });
addRead("tilde-free abs path", "small.txt", { path: "<CWD>/small.txt" });
addRead("at-prefixed path", "small.txt", { path: "@small.txt" });

const readResults = [];
for (const entry of readCases) {
  const params = { ...entry.params, path: entry.params.path.replace(PLACEHOLDER, work) };
  let outcome;
  try {
    const result = await tools.read.execute("id", params, undefined, undefined, { cwd: work });
    outcome = { text: scrub(result.content[0].text), details: result.details };
  } catch (error) {
    outcome = { error: scrub(error.message) };
  }
  readResults.push({ name: entry.name, file: entry.file, params: entry.params, outcome });
}

// materialise the read fixtures so they can be read by the TS tool
for (const [name, content] of Object.entries(readFiles)) writeFileSync(join(work, name), content);
// (cases above ran before the write; rerun now that the files exist)
readResults.length = 0;
for (const entry of readCases) {
  const params = { ...entry.params, path: entry.params.path.replace(PLACEHOLDER, work) };
  let outcome;
  try {
    const result = await tools.read.execute("id", params, undefined, undefined, { cwd: work });
    outcome = { text: scrub(result.content[0].text), details: result.details };
  } catch (error) {
    outcome = { error: scrub(error.message) };
  }
  readResults.push({ name: entry.name, file: entry.file, params: entry.params, outcome });
}

// ---- edit cases ----
const base = "one\ntwo\nthree\nfour\nfive\n";
const editCases = [];
const addEdit = (name, content, edits, file = "f.txt") => editCases.push({ name, file, content, edits, build: undefined });
const A = (content, n) => anchor(content, n);

addEdit("replace+insert+delete", base, [
  { op: "replace", anchor: A(base, 1), lines: ["ONE", "ONE-B"] },
  { op: "insert_after", anchor: A(base, 3), lines: ["three-and-a-half"] },
  { op: "delete", anchor: A(base, 4), end: A(base, 5) },
]);
addEdit("range replace and insert_before", base, [
  { op: "replace", anchor: A(base, 2), end: A(base, 3), lines: ["middle"] },
  { op: "insert_before", anchor: A(base, 1), lines: ["zero"] },
]);
addEdit("insert at both ends", base, [
  { op: "insert_before", anchor: A(base, 1), lines: ["top"] },
  { op: "insert_after", anchor: A(base, 5), lines: ["bottom", "bottom2"] },
]);
addEdit("delete everything", base, [{ op: "delete", anchor: A(base, 1), end: A(base, 5) }]);
addEdit("delete first", base, [{ op: "delete", anchor: A(base, 1) }]);
addEdit("delete last", base, [{ op: "delete", anchor: A(base, 5) }]);
addEdit("delete middle", base, [{ op: "delete", anchor: A(base, 3) }]);
addEdit("replace with empty-line entries", base, [{ op: "replace", anchor: A(base, 2), lines: ["", "  indented", ""] }]);
addEdit("replace single line with itself", base, [{ op: "replace", anchor: A(base, 1), lines: ["one"] }]);
addEdit("delete adjacent separate ops", base, [
  { op: "delete", anchor: A(base, 2) },
  { op: "delete", anchor: A(base, 3) },
]);
addEdit("insert after replaced range end", base, [
  { op: "replace", anchor: A(base, 1), end: A(base, 2), lines: ["x"] },
  { op: "insert_before", anchor: A(base, 3), lines: ["y"] },
]);
addEdit("insert before first replaced line", base, [
  { op: "replace", anchor: A(base, 2), end: A(base, 3), lines: ["x"] },
  { op: "insert_before", anchor: A(base, 2), lines: ["y"] },
]);
addEdit("summary merges regions", base, [
  { op: "replace", anchor: A(base, 1), lines: ["A"] },
  { op: "replace", anchor: A(base, 4), lines: ["D"] },
]);
const many = Array.from({ length: 200 }, (_, i) => `l${i + 1}`).join("\n") + "\n";
addEdit("summary distant regions", many, [
  { op: "replace", anchor: A(many, 5), lines: ["five"] },
  { op: "replace", anchor: A(many, 150), lines: ["one fifty"] },
]);
addEdit("summary capped at 80 rows", many, [{ op: "replace", anchor: A(many, 10), end: A(many, 150), lines: Array.from({ length: 120 }, (_, i) => `n${i}`) }]);
const crlf = "a\r\nb\r\nc\r\n";
addEdit("crlf insert", crlf, [{ op: "insert_after", anchor: A(crlf, 2), lines: ["new"] }]);
addEdit("crlf replace keeps others", crlf, [{ op: "replace", anchor: A(crlf, 1), lines: ["A", "A2"] }]);
const mixed = "a\nb\r\nc\n";
addEdit("mixed eol uses first style", mixed, [{ op: "insert_after", anchor: A(mixed, 1), lines: ["n"] }]);
const noNl = "x\ny";
addEdit("no final newline replace last", noNl, [{ op: "replace", anchor: A(noNl, 2), lines: ["Y"] }]);
addEdit("no final newline insert after last", noNl, [{ op: "insert_after", anchor: A(noNl, 2), lines: ["z"] }]);
addEdit("no final newline delete last", noNl, [{ op: "delete", anchor: A(noNl, 2) }]);
addEdit("no final newline insert before last", noNl, [{ op: "insert_before", anchor: A(noNl, 2), lines: ["m"] }]);
const bom = "\ufeffa\nb\n";
addEdit("bom preserved", bom, [{ op: "replace", anchor: A(bom, 1), lines: ["A"] }]);
const uni = "café 😀\nnaïve ✓\n";
addEdit("unicode lines", uni, [{ op: "replace", anchor: A(uni, 2), lines: ["näive ✗ 日本語"] }]);
addEdit("whitespace-only line anchor", "a\n  \nb\n", [{ op: "delete", anchor: A("a\n  \nb\n", 2) }]);
addEdit("blank line anchor", "a\n\nb\n", [{ op: "replace", anchor: A("a\n\nb\n", 2), lines: ["filled"] }]);
addEdit("anchor with surrounding spaces", base, [{ op: "delete", anchor: `  ${A(base, 2)}  ` }]);
addEdit("stale same line number", base, [{ op: "delete", anchor: `2#${sha("TWO")}` }]);
addEdit("stale hash 0000", base, [{ op: "delete", anchor: "3#0000" }]);
addEdit("out of range", base, [{ op: "delete", anchor: "99#abcd" }]);
addEdit("hash only", base, [{ op: "delete", anchor: "abcd" }]);
addEdit("line only", base, [{ op: "delete", anchor: "2" }]);
addEdit("hash with leading hash", base, [{ op: "delete", anchor: "#abcd" }]);
addEdit("non hex hash", base, [{ op: "delete", anchor: "2#zzzz" }]);
addEdit("uppercase hash", base, [{ op: "delete", anchor: `2#${sha("two").toUpperCase()}` }]);
addEdit("line zero", base, [{ op: "delete", anchor: "0#abcd" }]);
addEdit("line unsafe integer", base, [{ op: "delete", anchor: "99999999999999999999#abcd" }]);
addEdit("anchor number type", base, [{ op: "delete", anchor: 5 }]);
addEdit("anchor null", base, [{ op: "delete", anchor: null }]);
addEdit("end null", base, [{ op: "delete", anchor: A(base, 1), end: null }]);
addEdit("end before anchor", base, [{ op: "delete", anchor: A(base, 3), end: A(base, 1) }]);
addEdit("end stale", base, [{ op: "delete", anchor: A(base, 1), end: "3#0000" }]);
addEdit("end on insert", base, [{ op: "insert_after", anchor: A(base, 1), end: A(base, 2), lines: ["x"] }]);
addEdit("delete with lines", base, [{ op: "delete", anchor: A(base, 1), lines: ["x"] }]);
addEdit("replace without lines", base, [{ op: "replace", anchor: A(base, 1) }]);
addEdit("replace empty lines", base, [{ op: "replace", anchor: A(base, 1), lines: [] }]);
addEdit("insert empty lines", base, [{ op: "insert_after", anchor: A(base, 1), lines: [] }]);
addEdit("lines with newline", base, [{ op: "replace", anchor: A(base, 1), lines: ["a\nb"] }]);
addEdit("lines with carriage return", base, [{ op: "replace", anchor: A(base, 1), lines: ["a\rb"] }]);
addEdit("lines not strings", base, [{ op: "replace", anchor: A(base, 1), lines: ["a", 2] }]);
addEdit("lines not array", base, [{ op: "replace", anchor: A(base, 1), lines: "a" }]);
addEdit("unknown op", base, [{ op: "append", anchor: A(base, 1) }]);
addEdit("missing op", base, [{ anchor: A(base, 1), lines: ["x"] }]);
addEdit("op not object", base, ["delete"]);
addEdit("op array", base, [[]]);
addEdit("op null", base, [null]);
addEdit("empty edits", base, []);
addEdit("edits not array", base, "x");
addEdit("edits object", base, {});
addEdit("empty file", "", [{ op: "delete", anchor: "1#abcd" }]);
addEdit("newline-only file", "\n", [{ op: "replace", anchor: A("\n", 1), lines: ["x"] }]);
addEdit("ambiguous duplicate", base, [{ op: "delete", anchor: A(base, 2) }, { op: "replace", anchor: A(base, 2), lines: ["x"] }]);
addEdit("ambiguous range overlap", base, [
  { op: "replace", anchor: A(base, 1), end: A(base, 3), lines: ["x"] },
  { op: "delete", anchor: A(base, 3) },
]);
addEdit("ambiguous touching ranges are fine", base, [
  { op: "replace", anchor: A(base, 1), end: A(base, 2), lines: ["x"] },
  { op: "replace", anchor: A(base, 3), end: A(base, 4), lines: ["y"] },
]);
addEdit("same gap", base, [
  { op: "insert_after", anchor: A(base, 1), lines: ["x"] },
  { op: "insert_before", anchor: A(base, 2), lines: ["y"] },
]);
addEdit("insert inside range", base, [
  { op: "replace", anchor: A(base, 1), end: A(base, 3), lines: ["x"] },
  { op: "insert_after", anchor: A(base, 1), lines: ["inside"] },
]);
addEdit("insert inside range end gap", base, [
  { op: "replace", anchor: A(base, 1), end: A(base, 3), lines: ["x"] },
  { op: "insert_after", anchor: A(base, 3), lines: ["after"] },
]);
addEdit("bad op after good op", base, [
  { op: "delete", anchor: A(base, 1) },
  { op: "delete", anchor: "3#0000" },
]);
addEdit("first error wins", base, [
  { op: "delete", anchor: "99#abcd" },
  { op: "bogus", anchor: "1#abcd" },
]);
addEdit("identical result", "a\nb\n", [{ op: "replace", anchor: A("a\nb\n", 1), lines: ["a"] }]);
addEdit("identical result via insert-delete", "a\nb\n", [{ op: "delete", anchor: A("a\nb\n", 1) }, { op: "insert_before", anchor: A("a\nb\n", 2), lines: ["a"] }]);
const edgePath = "sub/nested.txt";
addEdit("nested relative path", base, [{ op: "delete", anchor: A(base, 1) }], edgePath);

const editResults = [];
for (const entry of editCases) {
  const target = join(work, entry.file);
  mkdirSync(join(target, ".."), { recursive: true });
  writeFileSync(target, entry.content);
  const params = { path: entry.file, edits: entry.edits };
  let outcome;
  try {
    const result = await tools.edit.execute("id", params, undefined, undefined, { cwd: work });
    outcome = { text: scrub(result.content[0].text), details: result.details };
  } catch (error) {
    outcome = { error: scrub(error.message) };
  }
  editResults.push({ name: entry.name, file: entry.file, content: entry.content, edits: entry.edits, outcome, after: readFileSync(target, "utf8") });
}

// ---- pure outline cases over arbitrary sources ----
const outlineSources = Object.entries(readFiles)
  .filter(([name]) => outline.languageOf(name) !== undefined)
  .map(([name, content]) => ({ name, lines: content.split("\n") }));
for (const [name, content] of Object.entries({
  "unknown.json": "{}\n",
  "short.ts": "const a = 1;\n",
  "arrow.ts": "const f = (a: number): number => a;\nlet g = async function () {}\nvar h = x => x\n",
  "class.ts": "export abstract class A {\n  static async *gen() {}\n  get v() { return 1 }\n  #p() {}\n  if (x) {\n  }\n}\nnamespace N.M {\n  function inner() {}\n}\nenum E {\n  A,\n}\ndeclare const enum CE {}\n",
  "kotlin.kt": "sealed class S {\n    suspend fun run(a: Int): Int {\n        return a\n    }\n}\nobject O {\n}\ndata class D(val a: Int)\n",
  "cpp.cpp": "namespace ns {\nclass C {\npublic:\n    C();\n    ~C() {}\n    int size() const;\n};\n}\nint main(int argc, char** argv) {\n  return 0;\n}\nstatic inline void helper(void) {\n}\n",
  "trait.rs": "pub trait T {\n    fn a(&self);\n}\nmod m {\n    pub fn inner() {}\n}\nunsafe impl<T> Send for X<T> where T: Y {\n}\nenum E { A }\n",
  "pyasync.py": "async def run():\n    pass\n\nclass K:\n    async def go(self):\n        pass\n",
  "headings.md": "# A\n\n## B\n\n# C #\n\n```\n# no\n```\n~~~\n# no\n~~~\n### D\n",
  "go-generics.go": "package p\n\nfunc (l *List[T]) Add(v T) {\n}\n\ntype Pair[K comparable, V any] struct {\n}\n\ntype Alias = int\n\ntype I interface {\n}\n",
  "blockcomment.ts": "/* start */ function real() {\n}\n/* multi\nfunction hidden() {}\n*/ const after = 1;\nfunction second() {\n}\n",
  "tabs.py": "def f():\n\tdef g():\n\t\tpass\n\treturn 1\n",
})) outlineSources.push({ name, lines: content.split("\n") });

const outlineResults = outlineSources.map(({ name, lines }) => {
  const symbols = outline.buildOutline(name, lines);
  return { name, lines, symbols, rendered: symbols.length === 0 ? null : outline.renderOutline(symbols) };
});

// ---- more anchors-level cases: hashes for tricky text ----
const hashTexts = ["", " ", "a", "café", "😀", "line with trailing space ", "\ttab", "日本語", "x".repeat(5000), "\u00a0nbsp"];
const hashes = hashTexts.map((text) => ({ text, hash: sha(text), anchor: anchors.formatAnchor(7, text) }));

writeFileSync(OUT, JSON.stringify({ source: SOURCE, readFiles, readResults, editResults, outlineResults, hashes }, null, 1) + "\n");
rmSync(work, { recursive: true, force: true });
console.log(`wrote ${OUT}: ${readResults.length} read, ${editResults.length} edit, ${outlineResults.length} outline, ${hashes.length} hash cases`);
