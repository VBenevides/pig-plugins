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

Provenance: `PROVENANCE.json` pins the SHA-256 digest of the source tree (sorted
relative paths and bytes, excluding `node_modules` and `.git`). The generator
refuses to run (exit 3) while the digest is unpinned and exits 4 on a mismatch.
`--print-digest` prints the digest of `HASHLINE_TS_SOURCE`. The digest is
currently **unpinned**: the source used for the committed fixture is not
available, so regeneration is blocked until it is recovered and pinned.

Intentional divergences from the original are asserted in the Go tests, not in
the fixture (for example, sanitized malformed-anchor diagnostics and exact-byte
versions).
