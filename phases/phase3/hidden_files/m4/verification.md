# P3-M4 Remediation — Final Verification (GG-0020)

**Date:** 2026-10-06
**Tree:** `e46c965` (all findings remediated; remediation commits
`c9a4737` P0-1, `6f6b25c` P1-1, `81cade7` P1-2/P2-4, `7ac7cde`
P2-1/P2-2/P2-3/P2-5, `17e5a80` methodology P2-1–P2-4, `a176523`
review reports, `e46c965` P1-2 compile fix)
**Method:** every command below was re-run on the final tree with
forced rebuilds (`out/classes/.built` removed per phase); the output
tail lines are pasted verbatim from observed runs. Nothing here is
invented.

> Process note: a first battery run (~12:00 UTC) was discarded — it
> ran against stale phase-1 classes because the P1-2 commit `81cade7`
> did not compile phase-1 (`SelectorNode`: `this` referenced from the
> static `readFieldPath`; the battery's own `make` caught it). Fixed
> in `e46c965` (additive `readFieldPath` overload). A second
> incident: `/tmp` (512M tmpfs) was wiped mid-battery by an external
> cleanup, killing the battery script and its log after most
> sections had completed and been observed. The two unfinished
> corpora (sync, traceback) were re-run afterwards on the unchanged
> tree; their logs are kept under `hidden_files/m4/logs/`
> (workspace, not `/tmp`).

## Suites

### Phase-1 — `make -C phases/phase1 test` (forced rebuild)

```
$ rm -f phases/phase1/out/classes/.built && make -C phases/phase1 test 2>&1 | tail -2
208 tests, 0 failures
make: Leaving directory '/home/hatch/workspace/graalgo/phases/phase1'
```

### Phase-1 — `make -C phases/phase1 check`

```
$ make -C phases/phase1 check 2>&1 | grep -E "gate clean|FAILED|Error"
placeholder gate clean
debug-leftover gate clean
$ echo rc=$?
rc=0
```

(gofrontend `build.sh check` selftests all ok —
`hidden_files/m4/logs/phase1-check.log`.)

### Phase-2 — `make -C phases/phase2 test` (forced rebuild)

```
$ rm -f phases/phase2/out/classes/.built && make -C phases/phase2 test 2>&1 | tail -2
194 tests, 0 failures
make: Leaving directory '/home/hatch/workspace/graalgo/phases/phase2'
```

### Phase-2 — `make -C phases/phase2 check`

```
placeholder gate clean
debug-leftover gate clean
netpoller-deletion gate clean
off-heap/barrier gate clean
pin-surface gate clean
```

### p3frontend — `go test -count=1 ./...` in `phases/phase3/src/p3frontend`

```
$ go test -count=1 ./...
ok  	t3frontend	38.017s
```

### Phase-3 — `make -C phases/phase3 test` (forced rebuild)

```
$ rm -f phases/phase3/out/classes/.built phases/phase3/out/test-classes/.built && make -C phases/phase3 test 2>&1 | tail -2
29 tests, 0 failures
make: Leaving directory '/home/hatch/workspace/graalgo/phases/phase3'
```

(29 = 26 pre-existing + `ItabForIntrinsicTest` rewritten to 3 tests +
`LinknameEndToEndTest` 2 new tests.)

### Phase-3 — `make -C phases/phase3 check`

```
placeholder gate clean
debug-leftover gate clean
```

## Differential corpora (vs stock gc 1.27.1, `bash run_differential.sh`)

| Corpus | Result (verbatim tail) |
|---|---|
| atomic | `== differential: 8 passed, 0 failed ==` |
| fastrand | `== differential: 8 passed, 0 failed ==` |
| iface | `== differential: 13 passed, 0 failed ==` |
| map | `== differential: 9 passed, 0 failed ==` |
| slice | `== differential: 6 passed, 0 failed ==` |
| sync | `== differential: 25 passed, 0 failed ==` |
| traceback | `== differential: 4 passed, 0 failed ==` |

## Remediation red-greens re-verified on the final tree

- **R1 (P0-1 atomic aliasing):** `at03_atomic_pointers` in the atomic
  corpus run above (gc-identical, gmp=1+4).
- **R2 (P1-1 fastrandn(0)):** `fr03_zero` in the fastrand corpus run above.
- **R3 (P1-2 panic frame line):** `mp04_nil_map_inner` in the map corpus
  run above (full normalized stderr, gc-identical).
- **R4 (P2-2 no leak):**
  `LinknameEndToEndTest.testNoLeakAcrossProgramsInOneJvm` in the
  phase-3 suite run above (also verified RED by stashing the
  `Linknames.clear()` line: program B printed the stale `42`).
- **R5 (P2-1 one namespace):** `ItabForIntrinsicTest` (3 tests) in the
  phase-3 suite run above (collision probe showed
  `COLLISION: B got A's itab` on the old code).
- **P2-3 callGo e2e:**
  `LinknameEndToEndTest.testCallGoEndToEndPrints42` in the phase-3
  suite run above.
- **P2-5 arg-unwrap assert:** exercised by the callGo e2e test above
  (any-typed arg for a concrete param passes through
  `GoFuncRootNode`'s unwrap+assert on the rebuilt classes).
