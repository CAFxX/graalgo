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

**Not here:** `sync`/`time`/syscall programs ([test/m1c/](../m1c/README.md)),
import-free language programs ([test/m1a/](../m1a/README.md)).

**Run:** `make test-m1r` (~15 s) or `make test-m1r M1R_PROGRAMS="test/m1r/programs/x.go"`.
