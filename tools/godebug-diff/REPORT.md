# godebug-diff report

Generated: 2026-11-25T13:42:13Z
Probes: test/corpus/phase2/godebug (db02_panicnil.go, db03_alloc.go, db04_spin.go)
Backends: stock gc go1.27.1 (go run) vs graalgo (graalgo-tool, graalgo-bundle-run)

```
FAIL schedtrace: graalgo emitted <3 SCHED lines (0)
FAIL scheddetail: no SCHED-DETAIL lines
FAIL gctrace: graalgo emitted <2 gc cycle lines (0)
PASS panicnil: stdout byte-identical: recovered: <nil> (<nil>)

```

Result: 1 passed, 3 failed

On the new path (m3-plan D3) the emulated scheduler/GC trace knobs (schedtrace, scheddetail,
gctrace) are not implemented yet: WP-GODEBUG (docs/arch/RECOVERY.md). panicnil matches gc.
