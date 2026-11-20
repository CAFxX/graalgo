# girstat — emitter coverage and GIR size over std (status: **new**, dev tool)

Runs the emitter over a package set (default: all of std in graalgo's build
configuration) and reports coverage per AST node kind, unsupported
constructs, GIR size per section and encode/decode speed; every package is
round-tripped through the Go codec. With `-out DIR` it writes the GIR files
for the Java round trip (`make -C internal/backend/gir test GIR_EXTRA=DIR`).
Background: [toolexec-design.md](../../../../docs/arch/toolexec-design.md) §12 P3.

**Not a gate.** Run: `cd internal/toolexec && go run ./cmd/girstat [-out dir] [patterns...]`.
