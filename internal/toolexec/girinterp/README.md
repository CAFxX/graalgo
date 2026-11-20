# girinterp — reference interpreter of GIR (status: **new**, test-only)

A deliberately simple, slow interpreter of the emitter's output for
self-contained programs (the whole language except goroutines and
`unsafe`). It takes the GIR at its word, so an emitter bug shows as an
output difference against gc without the JVM. Also the emitter's mutation
oracle (`mutants_test.go`). It never ships and is **not** v1: it stays after
M3 ([toolexec-design.md](../../../docs/arch/toolexec-design.md) §9.1, §13).
Its programs: [testdata/progs/](testdata/progs/); it also runs the
import-free programs of [test/corpus/](../../../test/corpus/).

**Not here:** the real backend ([graalgo/go/](../../backend/truffle/src/main/java/graalgo/go/)).

**Test:** `cd internal/toolexec && go test ./girinterp`.
