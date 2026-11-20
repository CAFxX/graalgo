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

**Carrier mode:** [carrier/](carrier/) holds programs that
[carrier_mode.sh](carrier_mode.sh) runs with
`-Dgraalgo.sched.goroutines=carrier` (goroutines as continuations on carrier
threads, [ADR-0018](../../docs/dev/adr/ADR-0018-carrier-goroutines.md));
`make test-m1r` runs it after run.sh. [spawn_flat.go](carrier/spawn_flat.go)
checks that goroutine creation does not slow down with the live count; it
fails in the default mode by design.

**Not here:** `sync`/`time`/syscall programs ([test/m1c/](../m1c/README.md)),
import-free language programs ([test/m1a/](../m1a/README.md)).

**Run:** `make test-m1r` (~20 s) or `make test-m1r M1R_PROGRAMS="test/m1r/programs/x.go"`.
