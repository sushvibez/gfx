# Contributing

Thanks for looking. gfx is a Go framework for 2D UI and 3D over WebGPU
with no cgo, and it is kept small enough that one person can hold the
whole architecture in their head. Contributions that keep it that way are
welcome.

## Before you start

- Read [DESIGN.md](DESIGN.md) for the layer you are touching. It is the
  authoritative architecture document and carries *Status* notes where
  the implementation deviates from the design. Trust those notes over
  the surrounding prose, and update them when you close a gap.
- Check [TODO.md](TODO.md) for what is open. Big changes are best
  discussed in an issue first.

## Ground rules

- **Layers point downward only.** `core`/`mem`/`ffi` → `app` → `gpu` →
  `ecs`/`render` → `scene`/`ui` → `md` → `script`. The test in
  `internal/layering` fails on any upward import.
- **No cgo.** Native libraries load through `ffi` (purego / syscall);
  the browser goes through `syscall/js`. The only exception is the
  Android `NativeActivity` glue.
- **Every platform stays green.** Platform-specific code goes behind
  build-tagged files; `app.Window` methods must exist on every backend
  (return an error where a feature is X11-only), and GPU readback is
  native-only, so `js` needs a stub.
- **Single-owner threading.** Properties and UI objects belong to the
  event-loop goroutine; other goroutines use `loop.Post`.
- **Hot paths allocate nothing.** Target 0 allocs/op and add a benchmark
  when you touch one. APIs take value types (`Vec3`, not `*Vec3`).
- **WGSL only** for shaders; the `gpu` package stays a thin mirror of
  WebGPU with explicit `Release`. Convenience belongs in `render`.
- **Headless first.** Every example and every visual feature should be
  verifiable with `-screenshot out.png` and no display.

## The check

```bash
scripts/check.sh
```

runs gofmt, build+vet for `linux`, `windows` and `js/wasm`, the layering
test, every test, and a headless screenshot smoke (when wgpu-native is
present — `scripts/fetch-wgpu.sh`). CI runs the same script; please run
it before opening a pull request. On Windows, run it from Git Bash.

To run one package's tests: `go test ./ui/...`; one test:
`go test -run TestName ./core/...`. The examples are a separate module:
`cd examples && go test ./...`.

## Pull requests

- One change per PR, with the reasoning in the description.
- Add or extend a test; for anything visual, a headless screenshot test
  or a note on what you checked by eye.
- Keep `TODO.md` current: if your change opens or closes an item, change
  the file in the same commit.
- Commit messages: a short imperative summary line, then the why.

## Licence

By contributing you agree that your contributions are licensed under the
Apache License 2.0, like the rest of the project.

You also grant the project's maintainer (Sushil Kumar, and anyone he
passes the maintenance of the project to) a perpetual, worldwide,
non-exclusive, royalty-free, irrevocable licence to use, modify and
distribute your contributions under any other licence approved by the
Open Source Initiative, so the project can change its licence in future
without having to trace every past contributor. You keep the copyright
in your contributions and every right the Apache License gives you.

Sign off each commit to certify that you wrote the change or otherwise
have the right to submit it under these terms (the Developer Certificate
of Origin, https://developercertificate.org):

    git commit -s

which adds a line `Signed-off-by: Your Name <you@example.com>` to the
commit message. Pull requests with unsigned commits cannot be merged.
