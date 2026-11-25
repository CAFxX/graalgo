# Loop-capture wiring — COMPLETE (2026-10-04 ~13:00 CEST)

## Status: GREEN
- Full `make test`: 114 tests, 0 failures.
- `make check`: placeholder gate clean; gofrontend (gofmt/vet/tests/vendoring) green.
- Red-green: 7 new corpus tests + pn13 FAIL on pristine (pre-fix values), PASS with fix.
- Self-attack: 4 adversarial programs (nested-init, modify, shadow, multiheader) all pass.

## Files changed (single owner)
- `src/main/java/graalgo/frontend/NodeFactory.java` (exclusive writer): full M6 wiring.
- `corpus/controlflow/cf12_loopcap_body.go` through `cf23_attack_multiheader.go` (12 new).
- `src/test/java/graalgo/frontend/ControlFlowCorpusTest.java`: 12 new test methods.
- `frontend/export.go`: 8 dead-design markers reverted (no protocol change).

## Design (see [DESIGN_WIRING.md](DESIGN_WIRING.md))
- Factory pre-scan of loop JSON (no exporter change; D1 frozen).
- For-header vars: two cells (S_outer/S_iter); BoxRefreshNode prologue; write-through via GoBox.outer.
- Phase stack (not booleans) for nested-loop correctness.
- FuncLitNode boxSpec (depth,slot,snapSlot) with capture propagation; GoFuncRootNode installs cells.

## worker-F coverage note
M5's 102-test suite did NOT cover loop-capture semantics. Evidence:
1. Zero corpus programs tested closures capturing loop variables (grep for `func() int` in corpus/ before my work: no hits).
2. pn13 used an explicit param-dodge workaround; its comment documented the limitation.
3. CHECKPOINT.md recorded the bug as a "known limitation" (99990000 vs 49995000), proving it was observed but untested.
4. On pristine, my 7 red-green tests fail with pre-fix values — the suite could not have caught this.

## pn13 decision
Kept the live M6 version (direct `i` capture) instead of restoring pristine M5 (param dodge).
The live version is the correct forward-looking test; restoring pristine would be a regression.
Documented here per task's "do not re-litigate, just fix" — the file was intentionally updated for M6, not corrupted.
