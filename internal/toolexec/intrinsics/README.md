# intrinsics — the intrinsic registry (status: **new**)

[registry.txt](registry.txt) lists every Go symbol the backend implements in
Java: `intrinsic` (no Go body: assembly, runtime linkname, facade
body-less function) or `replace` (only what gc itself intrinsifies, or a
`TEMPORARY (BUG-NNNN)` entry). `graalgo-tool link` resolves against it — a
reachable body-less function without an entry fails the link — and
[gen/](gen/) generates the Java dispatch table `IntrinsicTable.java` in
[graalgo/go/intrinsics/](../../backend/truffle/src/main/java/graalgo/go/intrinsics/).

**Add an intrinsic:** add the line (grammar in the file header), implement
the static method in the named `graalgo.go.intrinsics` class, `make
generate`, add a program to [test/m1r/programs/](../../../test/m1r/programs/).

**Not here:** v1's `graalgo.runtime.Intrinsics` (legacy, deleted at M3).

**Test:** `cd internal/toolexec && go test ./intrinsics` (incl.
`TestTemporaryEntries`), `make test-m1r`.
