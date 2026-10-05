# godebug-diff report

Generated: 2026-10-05T15:32:13Z
Probes: /home/hatch/workspace/graalgo/phases/phase2/corpus/godebug (db02_panicnil.go, db03_alloc.go, db04_spin.go)
Backends: stock gc go1.27.1 (/home/hatch/tools/go/bin/go run) vs graalgo SchedMain

```
PASS schedtrace: gc SCHED lines=28, graalgo SCHED lines=131, stdout identical
PASS scheddetail: SCHED-DETAIL lines=118
PASS gctrace: gc cycle lines=15, graalgo cycle lines=2, stdout identical
PASS panicnil: stdout byte-identical: recovered: <nil> (<nil>)

```

Result: 4 passed, 0 failed
