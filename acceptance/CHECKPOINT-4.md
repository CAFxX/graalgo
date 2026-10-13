# M9 Acceptance Checkpoint 4 — TLS REGRESSION FOUND
- Date: 2026-10-13 ~16:30 CEST
- FINDING: TLS handshake test (corpus/wave3/crypto/tls/01_handshake.go) HANGS on m9@3550c49
- GMP=1: timeout after 90s (no output)
- GMP=2: 4/4 timeouts after 120s each (rc=124)
- The x509 lane reported GMP=1 3/3 byte-identical and GMP=2 2/3 — this is a REGRESSION
- Suspect: one of the post-x509 merges (reflect-embed C-1 touched IfaceMethodTable.java, generality F-1/F-2 touched frontend)
- Status: REPORTED AS BUG, not fixed (verification-only mandate)
- Wave differentials: still running in background
