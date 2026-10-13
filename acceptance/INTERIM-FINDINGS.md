# M9 Acceptance Interim Findings — 2026-10-13 ~16:35 CEST
Base: m9@3550c49

## Phase 1: 180 PASS, 2 FAIL
- FAIL: testCheckRejectsVariadicPacking (--check accepts variadic packing, should reject with exit 2)
- FAIL: testCheckRejectsClearBuiltin (--check accepts clear(), should reject with exit 2)

## Phase 2: 194 PASS, 0 FAIL

## Phase 3 waves: IN PROGRESS (background)

## CRITICAL: TLS handshake regression
- corpus/wave3/crypto/tls/01_handshake.go HANGS on m9@3550c49
- GMP=1: timeout 90s, no output
- GMP=2: 4/4 timeouts 120s each
- x509 lane (on its branch) reported GMP=1 PASS; GMP=2/4 "reliability pending"
- REGRESSION from post-x509 merges. Suspects:
  - 3550c49 (reflect-embed C-1): touched IfaceMethodTable.java (itabFor public)
  - 5b1b331 (generality F-1/F-2): touched frontend synth_machinery.go, mrdesugar_m5.go

## Pending
- Wave 1/2/3 differentials
- Cross-implementation echo test (blocked by handshake hang)
- External-egress tests (no artifacts found in tree)
- gotest trial (plan exists, not implemented)
