# test/m1r — upstream std on the new path (status: **new**, per-push)

Named after toolexec milestone **M1r** (the runtime floor: intrinsic
registry, pointer model, descriptor images, first syscalls). [run.sh](run.sh)
builds each program of [programs/](programs/) against graalgo's
[stdlib fork](../../stdlib/README.md) with `go build -toolexec graalgo-tool`,
runs it on the new node package and compares stdout, normalized stderr and
exit status with gc.

**Add a test:** a `package main` in [programs/](programs/) that imports the
std packages under test — the place for intrinsic and std-behavior fixes
(e.g. [reflectlite_views.go](programs/reflectlite_views.go),
[preempt_spin.go](programs/preempt_spin.go)).

**Goroutine mode:** every program runs in the default carrier mode
(goroutines as continuations on carrier threads, GG-0074,
[ADR-0018](../../docs/dev/adr/ADR-0018-carrier-goroutines.md)).
[spawn_parked.go](programs/spawn_parked.go) keeps 20,000 goroutines parked at
once (functional output only); that their creation does not slow down with the
live count is a wall-clock property, checked in the benchmark tier by
[spawn_flat.go](../bench/scaling/spawn_flat.go) (`make bench-spawn-flat`,
weekly), never here. `make test-goroutine-modes` reruns a few programs in the
opt-in thread-per-goroutine mode. [cancel.sh](cancel.sh) builds [cancel/probe.go](cancel/probe.go)
(goroutines parked and spinning) and cancels it with
`Context.close(true)` in both modes: bounded, cancelled, no thread left.
[tools.sh](tools.sh) runs Truffle tools on [tools/probe.go](tools/probe.go) in
both modes (`graalgo.launcher.ToolsProbe`: a statement/root
`ExecutionListener` sees `main.fib`'s exact calls and statements at their
lines, a collecting `CPUSampler` records Go names and `main.go` sections —
in both goroutine modes, ADR-0020).

**Not here:** `sync`/`time`/syscall programs ([test/m1c/](../m1c/README.md)),
import-free language programs ([test/m1a/](../m1a/README.md)).

**Run:** `make test-m1r` (~20 s) or `make test-m1r M1R_PROGRAMS="test/m1r/programs/x.go"`.
