# internal/backend — the JVM side (status: **mixed**, see each entry)

Plain-`javac` modules (each with its own `Makefile`, no Maven/Gradle) and
the shell launchers that run them. `make build` builds truffle, then interop.

| Path | Status | What |
|---|---|---|
| [truffle/](truffle/README.md) | new + shared + v1 | the core module: new node package `graalgo.go.*`, shared runtime, v1 nodes — split explained in its README |
| [gir/](gir/README.md) | new | Java GIR codec, generated from the schema |
| [interop/](interop/README.md) | **v1, deleted at M3** | `graalgo.p3*` stdlib stand-ins + `P3SyncMain` |
| [graalgo-bundle-run](graalgo-bundle-run) | new | runner of executables linked by graalgo-tool (`graalgo.launcher.BundleMain`); the stub execs it |
| [graalgo-sync-run](graalgo-sync-run) | **v1, deleted at M3** | runs one `.go` file via gofrontend + `P3SyncMain` (the "product launcher" of the v1 gates) |
| [graalgo-run](graalgo-run) | **v1, deleted at M3** | runs one `.go` file via `graalgo.launcher.Main` |
| [fast-cycle/](fast-cycle/README.md) | shared (its `run`/`watch` are v1) | warm-JVM incremental compile / Java test loop (`make fast-build`, `fast-test`) |

**Not here:** anything that runs at `go build` time — that is
[internal/toolexec/](../toolexec/README.md). Map: [docs/MAP.md](../../docs/MAP.md).

**Test:** `make build test-java-fast test-gir`; new path end to end:
`make test-m1a test-m1r test-m1c test-m1w13 test-imgcheck`.
