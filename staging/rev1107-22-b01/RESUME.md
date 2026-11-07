# RESUME — rev1107-22-b01 (B-01, TLS unwrap error vs EOF)

## State on resume (this run, 2026-11-07 ~23:15 CET)

- Original lane was cancelled mid-flight (coordinator run ended, NOT a
  stop decision). Its staged work survived intact in
  `/tmp/rev1107-22-b01/` and was RESUMED, not restarted: pristine copies
  verified byte-identical to HEAD `59f0a247`; patched files consistent
  across `patched/` and `src-tree-patched/`; the fix design verified
  against disk truth before any new work.
- This resume run re-verified RED→GREEN fresh (see EVIDENCE.md §3),
  re-ran the Go checks, generated a clean `b01.patch`, and wrote
  EVIDENCE.md + this file. Nothing was committed or pushed.
- **BLOCKED-quota**: real 8 KB home write failed `Errno 122 Disk quota
  exceeded` (~23:15 CET). Load CLEAR (1.70, no frozen processes).
  No worktree claimed, no registry entry, no commit — per the brief.
  /tmp is 99% full (8.4M free) from concurrent lanes; my new writes
  are <1M. Do not delete other lanes' staging.

## What is staged (all under /tmp/rev1107-22-b01/)

- `b01.patch` (600 lines, 6 files) — the whole fix; `git apply --check`
  passes against HEAD `59f0a247` and reproduces every staged file
  byte-identically (verified this run).
- `patched/` — the 6 fixed/new files at repo-relative paths:
  `internal/backend/interop/src/main/java/graalgo/p3tls/TlsRuntime.java`,
  `internal/frontend/synth_crypto_tls.go`,
  `internal/backend/interop/src/main/java/graalgo/p3link/LinknameIntrinsics.java`,
  `internal/backend/interop/src/test/java/graalgo/iface/IfaceTestRunner.java`,
  `internal/backend/interop/src/test/java/graalgo/p3tls/TlsUnwrapErrorTest.java` (new),
  `test/regression/tls-unwrap-error/mismatch_test.go` (new).
- `EVIDENCE.md` — mechanism re-verification, fix design, RED→GREEN logs,
  Go checks, patch integrity, residuals.
- `verify/` — fresh evidence from this run: `classes-pristine/`,
  `classes-patched/`, `classes-testonly/` (fresh javac output),
  `driver5-red-green.log`, `testmain-red-green.log`, `go-vet.log`,
  `gofmt-orig.diff`, `gofmt-patched.diff`. (Scratch git repos used for
  patch generation/verification were deleted to save /tmp.)
- Pre-existing from the original lane (verified, reused, not rebuilt):
  `driver/` (Driver–Driver5 + TestMain probes), `classes-pristine/`,
  `classes-patched/`, `classes-testonly/`, `src-tree-pristine/`,
  `src-tree-patched/`, `src/TlsRuntime.java.orig`, `extract/` +
  `extract.py`, `vetmod/` (gc vet module for the extracted tls source),
  `helloworld/`, `gocache/` (64M — trim if /tmp pressure persists).

## Resume steps (on a successful real 8 KB home write)

1. Register `graalgo-rev1107-22-b01`; claim worktree `rev1107-22-b01`
   (`wt.py new graalgo rev1107-22-b01 --chat f7467b85-7da6-4da0-b9ec-94631a60dafe`
   — the chat id from the original dispatch; verify at claim time).
2. Apply `b01.patch` in the worktree; confirm `git status` shows exactly
   the 6 files above.
3. Run, in the worktree: the interop suite (`IfaceTestRunner`, incl. the
   new `TlsUnwrapErrorTest`), the Go regression test
   `test/regression/tls-unwrap-error` (both legs: corrupt record → error,
   clean close → io.EOF), then `make test-ci` + `make snapshot-check`.
   Expect the regression test to fail pre-fix (io.EOF) — if the worktree
   base has moved past `59f0a247`, re-verify the mechanism at the new
   HEAD first and rebase the patch if needed.
4. Commit with explicit pathspecs; do NOT integrate (independent review
   + CI on the exact SHA still owed).
5. Note: `/tmp` is tmpfs — lost on reboot. `b01.patch` is the durable
   artifact; if this staging dir is lost, the patch is reproducible from
   EVIDENCE.md §2, but the RED→GREEN logs would need re-running.

## Fix summary (for the reviewer)

`TlsRuntime.unwrap()` returned null for both clean close and SSLEngine
failure; Go mapped every null to `io.EOF`. Fix adds a per-engine
last-error channel (`unwrapError` trap, recorded on exception, cleared
per unwrap call and on close) and makes all three Go nil-unwrap sites
(handshake drain, handshake read, `__tlsConnRead`) consult it before
returning EOF. No bridges, no API break, public behavior change is
exactly: corrupt-record/fatal-alert reads now return
`tls: unwrap failed: <SSLException>` instead of `io.EOF`.
