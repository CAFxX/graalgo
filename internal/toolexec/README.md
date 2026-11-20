# internal/toolexec — the new path's Go side (status: **new**)

Its own Go module. Everything that runs at `go build` time on the new path:
the `graalgo` launcher, the `-toolexec` wrapper `graalgo-tool`, the GIR
typed IR, the emitter, the linker, the stdlib-fork applier, and the two
single sources of truth shared with the JVM side (intrinsic registry,
syscall role table). Design: [toolexec-design.md](../../docs/arch/toolexec-design.md);
data flow: [docs/MAP.md](../../docs/MAP.md).

- [cmd/](cmd/) — the binaries (`graalgo-launcher`, `graalgo-tool`, `graalgo-stub`, `graalgo-goroot`, `girstat`)
- [emit/](emit/) — go/types-checked package → GIR · [ir/](ir/) — GIR schema and codec
- [link/](link/) — symbol resolution + reachability · [bundle/](bundle/) — program bundle format · [archive/](archive/) — gc archive member
- [intrinsics/](intrinsics/) — intrinsic registry · [syscalls/](syscalls/) — syscall role table
- [fork/](fork/) — applies [stdlib/](../../stdlib/README.md) to an upstream GOROOT
- [argv/](argv/), [launcher/](launcher/), [release/](release/) — command lines, launcher env, tracked Go release
- [girinterp/](girinterp/) — test-only reference interpreter of GIR (never ships)

**Not here:** the JVM backend ([internal/backend/](../backend/README.md)); v1
code — this module must not import `internal/{frontend,driver,vet}`
([check_layering.py](../../.github/scripts/check_layering.py)).

**Test:** `make test-toolexec` (`go vet ./... && go test ./...` here); end
to end on the JVM: `make test-m1a test-m1r`. `make generate` regenerates
`ir/gir_gen.go` and the Java tables after editing `schema.txt`,
`registry.txt` or `table.txt`.
