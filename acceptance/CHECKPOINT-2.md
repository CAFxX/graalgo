# M9 Acceptance Checkpoint 2 — Phase 1 suite
- Date: 2026-10-13 ~16:00 CEST
- Phase 1 full suite: 180 PASS, 2 FAIL
- FAIL 1: MainTest.testCheckRejectsVariadicPacking — `--check` on variadic packing call: expected exit 2, got 0
- FAIL 2: MainTest.testCheckRejectsClearBuiltin — `--check` on `clear(m)`: expected exit 2, got 0
- Both are frontend --check validation strictness gaps (accepts what should be rejected)
- Log: acceptance/phase1-test.log
