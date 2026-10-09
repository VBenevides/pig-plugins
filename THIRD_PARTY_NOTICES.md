# Third-party notices

This repository's own code is MIT-licensed (see `LICENSE`). Several extensions are Go ports of
third-party TypeScript packages. Each port reimplements the original behavior and keeps its
copyright notice below. Versions are the ones the parity fixtures were captured from.

## Ported extensions

| Extension | Original | Author | Licence |
|---|---|---|---|
| `auto-models` | [`pi-auto-models@0.1.14`](https://github.com/Fatpandac/pi-auto-models) | Fatpandac | MIT |
| `better-footer` | [`pi-better-footer@0.1.3`](https://github.com/roy-tian/pi-better-footer) | Roy Tian | MIT |
| `web-search` | [`pi-web-search@1.6.0`](https://github.com/ttttmr/pi-web-search) | ttttmr | MIT (package.json) |
| `ask-user-question` | [`@juicesharp/rpiv-ask-user-question@2.12.0`](https://github.com/juicesharp/rpiv-mono/tree/main/packages/rpiv-ask-user-question) | juicesharp | MIT |
| `todo` | [`pi-todotools`](https://github.com/code-yeongyu/pi-todotools/tree/50b85f7e39c94a8fa8253eb3628515f188a42a1c) | Yeongyu Kim; Oh My Pi: Mario Zechner, Can Bölük | MIT |
| `lsp` | `pi-lsp@0.1.7` (npm) | not stated in the package | MIT |
| `smart-approve-lancet` | [VBenevides/smart-approve-lancet](https://github.com/VBenevides/smart-approve-lancet) | Vinicius Benevides (this repository's author) | MIT |
| `pi-curator` | [VBenevides/pi-curator](https://github.com/VBenevides/pi-curator) | Vinicius Benevides (this repository's author) | MIT |

Limits of this record:

- `pi-web-search@1.6.0` and `@diegopetrucci/pi-todo@0.1.11` ship no `LICENSE` file in their npm archives. Their
  `package.json` declares MIT. The `pi-extensions` repository (which contains the todo extension) publishes an MIT
  licence for Diego Petrucci. No copyright holder text exists for `pi-web-search`, so none is claimed below beyond the
  declared author.
- `pi-lsp@0.1.7` declares no author or repository. Its `LICENSE` reads `Copyright (c) 2026` with no holder.

The phased todo Go port pins pi-todotools at `50b85f7e39c94a8fa8253eb3628515f188a42a1c`.
Its upstream MIT license and origin notice are preserved in `extensions/todo/LICENSE` and
`extensions/todo/NOTICE`. The model, operations, guidance, Markdown helpers and phase rendering
derive from Oh My Pi commit `9fd6e97113f5ed3a847e66d346970efdf8afcad9` (v17.0.5):
Copyright (c) 2025 Mario Zechner; Copyright (c) 2025-2026 Can Bölük.
The old Diego Petrucci notice below is retained for historical fixtures used to test migration.

## Native image-preview port

`extensions/pi-image-view/` ports `pi-image-view@0.4.0`, the alchemistklk fork of RielJ/pi-image-preview, to Go.
Its MIT licence is retained in `extensions/pi-image-view/LICENSE`. The pinned TypeScript reference source and
upstream archive integrity metadata are retained in `testfixtures/image-view/upstream/`; they are not bundled.

## Prompt material

The ADHD output rules draw on [i-have-adhd](https://github.com/ayghri/i-have-adhd) (MIT). Its notice is in
`prompts/sources/i-have-adhd-LICENSE`.

## Model and libraries

- **LANCET Nano model** (downloaded by `lancet setup`, not stored here): [TannerMidd/LANCET-model](https://github.com/TannerMidd/LANCET-model) v0.4.3.
  The model weights, tokenizer and metadata are under Apache-2.0; see that repository's `bundle/MODEL-LICENSE.md`.
  `internal/lancet` is a native Go implementation of its classifier.
- **ONNX Runtime** 1.30.0 (Microsoft, MIT): downloaded at setup from its GitHub release; not redistributed here.
- **`github.com/dlclark/regexp2`** (Doug Clark, MIT), **`github.com/shota3506/onnxruntime-purego`** (Shota Sugiura, MIT) and
  **`github.com/ebitengine/purego`** (Apache-2.0) are Go module dependencies.
- **`golang.org/x/image`** v0.45.0 (The Go Authors, BSD-3-Clause) supplies WebP decoding and resizing.
  Its license is retained in `extensions/pi-image-view/X_IMAGE_LICENSE`.

## Licence texts

### pi-auto-models (Fatpandac)

```text
MIT License

Copyright (c) 2026 Fatpandac

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

### pi-better-footer (Roy Tian)

```text
MIT License

Copyright (c) 2026 Roy Tian

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

### @juicesharp/rpiv-ask-user-question (juicesharp)

```text
MIT License

Copyright (c) 2026 juicesharp

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

### pi-extensions todo (Diego Petrucci)

```text
MIT License

Copyright (c) 2026 Diego Petrucci

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

### pi-lsp

```text
MIT License

Copyright (c) 2026

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```
