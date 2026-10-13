# Hashline golden-fixture generator

`generate.mjs` records the behavior of the original TypeScript `harness-hashline`
extension into `internal/hashline/testdata/ts-golden.json`. The Go tests compare
the Go implementation against that file; the file is never regenerated from Go.

The TypeScript source is not stored in this repository. Supply it explicitly:

```sh
HASHLINE_TS_SOURCE=/path/to/harness-hashline node scripts/hashline-golden/generate.mjs
```

Run from the repository root with Node 20 or later. The script copies the source
into a temporary directory, stubs the host package, and writes the fixture.
Intentional divergences from the original are asserted in the Go tests, not in
the fixture (for example, sanitized malformed-anchor diagnostics and exact-byte
versions).
