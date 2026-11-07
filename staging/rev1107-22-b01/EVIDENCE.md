# B-01 — TLS unwrap conflates errors with clean EOF — EVIDENCE

Lane: rev1107-22-b01 (resumed). Issue: REVIEW-2026-11-07-07 B-01 (P1),
carried by REVIEW-2026-11-07-22. HEAD verified: `59f0a247`.
Status: **fix staged + RED→GREEN verified (Java level); BLOCKED-quota —
no worktree, no commit, no push.** Resume lane must still run the Go
regression test end-to-end (`test/regression/tls-unwrap-error`) and the
interop suite in a worktree; see RESUME.md.

## 1. Mechanism re-verified from disk truth at HEAD 59f0a247

- `internal/backend/interop/src/main/java/graalgo/p3tls/TlsRuntime.java`:
  `unwrap()` returns `null` on `Status.CLOSED` (:576) AND on *any*
  `Exception` (:612-613, catch-all). Javadoc: "null if the peer closed
  or on error". Confirmed by reading the file, not by trusting the review.
- Go consumer `internal/frontend/synth_crypto_tls.go:900-902`
  (`__tlsConnRead`): `app := unwrapTrap(...); if app == nil { return
  {0, io.EOF} }`. So a corrupt record / fatal alert / SSLException
  mid-connection surfaces as clean EOF. gc returns the alert error.
- `wrap` has the same null-on-exception shape but the Go side checks nil
  there, so only the unwrap/read half is live-broken (as filed).
- Handshake also discards unwrap results (:798, :808) — the fix covers
  both sites (see §2).

## 2. The fix (6 files, `b01.patch`, 475 insertions / 4 deletions)

Design: separate error channel, no API break, no bridge (general
mechanism — a per-engine last-error accessor, same shape as the
existing `hasPending` trap).

- `TlsRuntime.java`: new `UNWRAP_ERRORS` map (handle → `String.valueOf(e)`).
  `unwrap()` clears it on entry, records the exception in the catch-all
  and returns null; a clean `CLOSED` records nothing. New
  `unwrapError(handle)` trap returns the recorded message as Go bytes,
  or null (clean close / no failure). `close()` clears the entry.
- `synth_crypto_tls.go`: new `unwrapErrorTrap` linkname + `Conn.unwrapErr()`
  helper. All three nil-unwrap sites now consult it: handshake drain
  (:~778), handshake read (:~793), `__tlsConnRead` (:~908). Nil + recorded
  error → `tls: unwrap failed: <msg>`; nil + no error → `io.EOF` (unchanged).
- `LinknameIntrinsics.java`: registers `graalgo.tls.unwrapError` (11 → 12
  tls intrinsics). No test anywhere asserts the old count (grepped).
- `TlsUnwrapErrorTest.java` (new, registered in `IfaceTestRunner.java`):
  corrupt record → null unwrap + non-null error containing
  "Unrecognized SSL message"; fresh engine → no error before/after a
  clean empty unwrap; fresh engine → no stale error.
- `test/regression/tls-unwrap-error/mismatch_test.go` (new): Go-level
  end-to-end. Corrupt one byte of the first post-handshake app-data
  record → server `Read` must return a non-EOF error containing
  "unwrap failed" (pre-fix: `io.EOF`); genuine close_notify → still
  `io.EOF`. Discovered automatically by `Makefile` (`REGRESSION_TEST_DIRS
  := $(wildcard test/regression/*/)`). Certs copied from `tls-wrap-hang`.

## 3. RED→GREEN evidence (this run, fresh compile + run in /tmp)

JDK: OpenJDK 25.0.4.1. `verify/driver5-red-green.log`,
`verify/testmain-red-green.log` (fresh `javac` of HEAD vs patched
sources; `Driver5` probes via reflection so it compiles against either).

RED — pristine (HEAD) `TlsRuntime`:
- `Driver5`: PRIMED-GARBAGE unwrap=null, unwrapError=`<no unwrapError
  method>`; PRIMED-FATAL-ALERT unwrap=null, same; FRESH-EMPTY non-null.
  I.e. a corrupt record is indistinguishable from a clean close.
- `TlsUnwrapErrorTest` vs pristine: `ran=2 failed=2`, exit 1
  (`NoSuchMethodError: TlsRuntime.unwrapError` — the channel does not exist).

GREEN — patched `TlsRuntime`:
- `Driver5`: PRIMED-GARBAGE unwrap=null, unwrapError=
  `javax.net.ssl.SSLException: Unrecognized SSL message, plaintext
  connection?`; PRIMED-FATAL-ALERT unwrap=null, unwrapError=
  `javax.net.ssl.SSLHandshakeException: (handshake_failure) Received fatal
  alert: handshake_failure`; FRESH-EMPTY unwrap=non-null, unwrapError=null.
- `TlsUnwrapErrorTest` vs patched: `PASS testCorruptRecordReportsError`,
  `PASS testNoErrorBeforeAnyUnwrap`, `ran=2 failed=0`, exit 0.

## 4. Go-side checks (this run, gc go1.27.1, `verify/go-vet.log`)

- `go vet ./...` on the patched extracted tls source (`vetmod/`, module
  `tlscheck`): exit 0.
- `gofmt -l` on the committed wrapper `patched/.../synth_crypto_tls.go`:
  clean. The extracted `tls-orig.go` / `tls-patched.go` are both flagged
  by gofmt, identically except line-number shifts (`verify/gofmt-*.diff`,
  81 lines each, same hunks): pre-existing struct-alignment in the
  synthetic source — the patch introduces ZERO new gofmt deviations.
- `extract.py` re-run reproduces the staged extracts; staged
  `vetmod/tls/tls.go` is byte-identical to `extract/tls-patched.go`.
- Staged pristine copies verified byte-identical to HEAD
  (`src/TlsRuntime.java.orig`, `src-tree-pristine/`); staged patched
  files consistent across `patched/` and `src-tree-patched/`.

## 5. Patch integrity (this run)

- `b01.patch` (600 lines, 6 files) generated from HEAD copies in a
  scratch git repo; `git apply --check` passes; applying it to pristine
  HEAD copies reproduces all 6 staged files byte-identically
  (verified with `cmp`, then scratch repos deleted to save /tmp).
- `unwrapTrap` has no other consumers in the repo (only
  `synth_crypto_tls.go`, `TlsRuntime.java`, `LinknameIntrinsics.java`
  reference it), so no call site was missed.

## 6. NOT done / residuals (resume lane must do)

- Go regression test `test/regression/tls-unwrap-error` NOT run
  end-to-end (needs full graalgo build in a worktree; quota-blocked).
  Its clean-close leg is the only coverage of post-handshake
  close_notify → io.EOF: a pre-handshake close_notify throws
  `SSLProtocolException` ("Received close_notify during handshake"),
  so the unit test cannot cover it (documented in the test file).
- Interop suite (`IfaceTestRunner` incl. the new test) NOT run inside
  the repo harness — only the standalone `TestMain` runner above.
- `make test-ci` / `make snapshot-check` NOT run.
- Residual, by design: `unwrap()` on an unknown/closed handle returns
  null with no recorded error (entry removed by `close()`), so the Go
  side reads that as EOF. `__tlsConnRead` already guards the nil-engine
  case (`tls: nil engine`); a closed engine is EOF-shaped. Documented,
  not hidden.
- `typecheck/main.go` in this staging dir is 0 bytes (original lane
  leftover, unused); `vetmod/` is the live Go check. `gocache/` (64M)
  is the original lane's Go cache — trim before re-running if /tmp
  pressure persists.
- /tmp was 79% full at resume, 99% full (8.4M free) by the end of this
  run from other lanes' concurrent staging — my new writes are <1M.
  Do NOT delete other lanes' staging to make room.
