# P2-M1 smoke programs

Archived verification programs (reviewer2 P1-3: the preemption claim must
be auditable).

## [preempt.go](preempt.go) — quantum preemption proof

Two goroutines busy-spin 2M iterations each; main waits on a **wait-loop**
(`for !(done1 && done2) {}`), then prints both counts.

Why the wait-loop shape matters: with `--gmp=1` (cap=1), if quantum
expiry did NOT preempt, the first admitted spinner would run to completion
(2M iterations) while main's wait-loop spins forever — but main's
wait-loop itself needs the CPU. Actually the proof is subtler: with cap=1
and no preemption, whichever goroutine is admitted first (say spin1)
runs 2M iterations; main cannot run its wait-loop until spin1 parks.
But spin1 never parks (no safepoint poll in the tight loop? — actually
there IS a poll at the loop back-edge). The real proof: **with quantum
neutered (deadline = +infinity), this program hangs**; with the 10ms
quantum it completes. That was verified during development.

Expected output (matches gc): `2000000 2000000`

Run:
```
java -cp <phase2-first> graalgo.sched.launcher.SchedMain --gmp=1 smoke/preempt.go
```

## panic.go — panic exit-code parity

Prints "before", panics with "test panic". Verifies P1-1: the launcher
must print `panic: test panic` and exit 2 (gc: exit 2).

Expected stderr: `panic: test panic`; exit code 2.

## hello.go — minimal sanity

Prints "hello". Used for the differential smoke (Phase-1 launcher vs
SchedMain --gmp=1 must be byte-identical) and the overlay fail-fast
verification (wrong classpath order must fail fast, not silently).
