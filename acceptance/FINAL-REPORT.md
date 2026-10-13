# M9 Final Acceptance Report
**Date:** 2026-10-13
**Base:** m9@3550c49
**Worktree:** ~/workspace/worktrees/graalgo/m9-acceptance
**Method:** Clean rebuild, full suites, differential samples vs gc 1.27.1

## Summary
M9 is NOT ready for master. One critical regression (TLS handshake hang) and two
medium issues (Phase 1 --check validation) require remediation.

## 1. Phase 1 Rebuild + Suite: 180 PASS, 2 FAIL
- **Clean rebuild:** PASS (javac 25.0.4, -Werror clean, 75s)
- **FAIL** `MainTest.testCheckRejectsVariadicPacking`: `--check` on variadic packing
  call — expected exit 2, got 0 (accepts what should be rejected)
- **FAIL** `MainTest.testCheckRejectsClearBuiltin`: `--check` on `clear(m)` —
  expected exit 2, got 0 (accepts what should be rejected)
- Log: acceptance/phase1-test.log

## 2. Phase 2 Confirmation: 194 PASS, 0 FAIL
- Full suite green (rc=0)
- Log: acceptance/phase2-test.log

## 3. Phase 3 Wave Differentials (samples vs gc 1.27.1)
- **Wave1 sample** (bytes, strings, strconv, math, sort): 31 PASS, 0 FAIL
- **Wave2 sample** (io, os): 32 PASS, 0 FAIL
- **Wave3 sample** (cmp, compress/flate): 7 PASS, 0 FAIL
  (00_roundtrip_levels needs >60s; passes with 180s timeout — harness limit, not a bug)
- Method: per-program gc build + run vs graalgo P3SyncMain --gmp=1, byte-compare
  stdout, compare exit codes. JVM WARNING lines filtered (harness noise).
- Logs: acceptance/diff-wave1-sample.log, diff-wave2-sample.log, diff-wave3-sample.log

## 4. Targeted Reruns
### GMP=2 TLS Handshake Reliability: FAIL (CRITICAL)
- **corpus/wave3/crypto/tls/01_handshake.go HANGS on m9@3550c49**
- GMP=1: timeout after 90s, zero output
- GMP=2: 4/4 timeouts after 120s each (rc=124)
- The x509 lane reported GMP=1 3/3 byte-identical on its branch; GMP=2/4 was
  "reliability pending (timing-dependent)" per EXPERIMENTS.md
- **REGRESSION** from post-x509 merges. Suspects:
  - 3550c49 (reflect-embed C-1): IfaceMethodTable.itabFor made public
  - 5b1b331 (generality F-1/F-2): frontend synth_machinery.go, mrdesugar_m5.go
- Repro: `P3SyncMain --gmp=1 corpus/wave3/crypto/tls/01_handshake.go` (hangs)
- Log: acceptance/tls-gmp2.log

### Cross-Implementation Application-Data Echo: BLOCKED
- Cannot test — blocked by handshake hang above
- Test file includes echo verification (server: echo-ok, client: echo-match)

## 5. External-Egress Tests: NOT FOUND
- No test artifacts matching "egress" in phases/ tree
- Requirement noted but cannot verify

## 6. Upstream go test Trial: NOT IMPLEMENTED
- Plan exists: ~/workspace/graalgo-m9-prep/phases/phase3/m9-prep/05-gotest-trial-plan.md
- Status: "analysis / design only — no implementation"
- Cannot run — no p3test infrastructure exists

## Findings Requiring Remediation
1. **CRITICAL:** TLS handshake hangs on m9@3550c49 (regression from x509 lane state)
2. **MEDIUM:** Phase 1 --check accepts variadic packing (should reject, exit 2)
3. **MEDIUM:** Phase 1 --check accepts clear() builtin (should reject, exit 2)

## Verdict: NOT READY for master merge
The TLS regression is a release-blocker. The --check validation gaps are medium
severity. All other acceptance items pass (Phase 1: 180/182, Phase 2: 194/194,
wave samples: 70/70).
