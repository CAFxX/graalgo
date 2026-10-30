# godebug-diff report

Generated: 2026-10-05T15:53:56Z
Probes: [test/corpus/phase2/godebug/](../../test/corpus/phase2/godebug) (db02_panicnil.go, db03_alloc.go, db04_spin.go)
Backends: stock gc go1.27.1 (/home/hatch/tools/go/bin/go run) vs graalgo SchedMain

```
PASS schedtrace: gc SCHED lines=22, graalgo SCHED lines=86 (increasing), stdout identical
PASS scheddetail: SCHED-DETAIL lines=100, negative control clean
PASS gctrace: gc cycle lines=19, graalgo cycle lines=3, stdout identical
PASS panicnil: stdout byte-identical: recovered: <nil> (<nil>)

```

Result: 4 passed, 0 failed
