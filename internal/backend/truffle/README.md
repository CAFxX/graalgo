# internal/backend/truffle — the core JVM module (status: **new + shared + v1**)

One `javac` module (nodes and runtime call each other) that holds **both**
pipelines. Know which package you are in (`src/main/java/graalgo/`):

| Status | Packages |
|---|---|
| **new** (build here) | [`go`](src/main/java/graalgo/go/): `go.loader` (bundle, lazy function units, generics), `go.types` (descriptors, layouts, itabs), `go.values` (flat value model: `Flat`, `Ptr`, `Slice`, `Iface`, …), `go.nodes` (typed node families), `go.build` (`NodeFactory` over GIR), `go.runtime` (panics, `G`, scheduler adapter), `go.intrinsics` (registry implementations, generated `IntrinsicTable`); [`syscall`](src/main/java/graalgo/syscall/) (Panama executor, generated `SyscallTable`) |
| **shared runtime** (stays after M3) | `sched` (scheduler core), `runtime` and `runtime.sync` (minus `Intrinsics`), `safepoint`, `jfr`, `language` (`GoLanguage`, `GoContext`: both paths; v1 halves go at M3), `launcher` (`BundleMain`, `GoPolyglot`; **not** `Main`) |
| **v1, deleted at M3** | `frontend` (JSON `NodeFactory`, ~110 node classes), `frontend.v2`, `values` (nested-`GoStruct` value model), `sched.launcher` (`SchedMain`), `launcher.Main`, `runtime.Intrinsics` |
| v1-only today, not in §9.1 | `sched.nodes` (`GoStatementNode`), `sched.audit` (`PinAudit`), `launcher.ToolPaths`: reached only from `SchedMain`/`Main` — re-check at M3 |
| test-only | `src/test/java`: `graalgo.test` (v1 corpus `Runner`), `sched.torture`, per-package tests |

The authoritative delete list is [toolexec-design.md §9.1](../../../docs/arch/toolexec-design.md);
the remaining v1 edges inside shared code are the "Layering debt" of
[RECOVERY.md](../../../docs/arch/RECOVERY.md), grandfathered in
[layering_allowlist.txt](../../../.github/scripts/layering_allowlist.txt) and
checked by [check_layering.py](../../../.github/scripts/check_layering.py).
The package READMEs under `graalgo/` are v1-era. Also here: [lib/](lib/)
(pinned Truffle jars), the GIR codec sources of [../gir/](../gir/README.md).
Design: [truffle-design.md](../../../docs/arch/truffle-design.md) §15.

**Test:** `make build test-java-fast pe-check-m1a`; `make test-ci` (smoke +
JIT guard); new path end to end: `make test-m1a test-m1w13`.
