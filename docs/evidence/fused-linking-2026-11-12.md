# Fused ELF verification: 2026-11-12

A fresh Linux/amd64 build was made with:

```sh
CGO_ENABLED=0 ./scripts/dev_build.sh .agent-work/tmp/pig-plugins-cgo0
python3 scripts/verify_static.py .agent-work/tmp/pig-plugins-cgo0
```

The build passed. The strict static check returned 1, as expected for a dynamic artifact.
Binary SHA-256: `9bacd537f9dcf92bf488001d84d7414a3900369b0f8bbd744eab630fa14fe182`.
The installed binary was also inspected and was dynamic. No user installation was changed.

## Artifact facts

- Go build metadata records `CGO_ENABLED=0`, `GOOS=linux`, and `GOARCH=amd64`.
- `file` and `readelf -l` report interpreter `/lib64/ld-linux-x86-64.so.2`.
- `readelf -d` reports `libdl.so.2`, `libpthread.so.0`, and `libc.so.6` as required libraries. `ldd` resolves them on this host.
- `internal/lancet/ort.go` uses `onnxruntime-purego`. Its purego dependency explicitly imports dynamic dlopen symbols even without CGO. Disabling CGO does not remove this dependency.

The verifier reads ELF metadata without executing the binary. Use `make verify-static BINARY=<final-path>` to check a built or installed artifact. Exit 0 means no interpreter or required shared libraries were found. Exit 1 reports dynamic requirements. Exit 2 reports an inspection error. This is ELF-specific and requires readelf and Go on the inspection host.

`make install` still requests a CGO-free build. It does not promise full static linking. Do not add misleading linker flags or disable the ONNX feature silently. A fully static distribution needs a supported design for dynamic runtime dependencies and separate compatibility qualification. That acceptance gate remains blocked; the verifier does not make it static.

## Offline startup smoke checks

Both checks ran only `--version`, with no network, no credentials, read-only root filesystems, a temporary HOME, and a read-only binary mount. No task setup, model call, benchmark, or grading ran.

| Existing local image | Image ID | Exit |
|---|---|---:|
| NodeBB benchmark image ending in `0e07f3c9...vf2cf3cbd...` | `sha256:e312e0149203d5ab5ec9f201c7d407adc3ba9c4bbd3844c9d4d9030dd9d3c1be` | 0 |
| python:3.14-slim | `sha256:be31a1a6191501ab98cbcbc1a2272efb848fbfee6b5728296c083be1bf69b211` | 0 |

Both printed `0.5.1+1.3.0.1402973a`. This fresh artifact can start in these two images. It is not proof of full static linking or full task compatibility, and it does not reproduce the older artifact's glibc failure.
