# R05 vet printf finisher — EVIDENCE (VERIFIED, red→green + full suite green)

Lane: review-05-vet-printf (finisher re-dispatch after prior finisher died on
transport failure before doing any work)
Branch: wt/graalgo-independent-codebase-review/review-05-vet-printf
Base: 20a0399b (HEAD, verified this run)
Worktree: ~/workspace/worktrees/graalgo/review-05-vet-printf
Findings: R05-V-02 (P0, %s/%q false positives on error/Stringer/named []byte)
  + R05-V-06 (P1, precision `*` does not consume an argument)
Source: ~/workspace/goals/graalgo-go-toolchain-on-graalvm-truffle/hidden_files/codebase-review/REVIEW-2026-11-09-05.md

## Disk state verified this run (10:39-10:57 CET, 2026-11-09)
- `git status --short`: ` M internal/vet/printf.go` + `?? internal/vet/printf_test.go`
  (+ `?? r05-verify-runner.log`, the detached runner's log) — nothing else.
- `git diff --stat`: internal/vet/printf.go, 99 insertions, 4 deletions.
- Fix markers present: starPrec, implementsErrorOrStringer, isByteSliceOrArray.
- NOTE: file mtimes 10:35/10:36 CET, not 10:02 CET as the brief stated;
  content was re-audited from disk truth this run (see below).
- No live processes on the worktree; no other finisher live (coordinator-verified).

## Fix audit (zero-load, from the diff + finding text)
- R05-V-06: new `starPrec` flag on printfVerb; parsePrintfVerbs sets it on
  precision `*`; checkPrintfFormat consumes one argument per `*` in width
  AND precision BEFORE the value argument (old code consumed starWidth only,
  and AFTER the value arg). Matches the finding's suggested fix exactly.
- R05-V-02: case 's','q' now accepts (a) basic string incl. named string
  types via Underlying, (b) byte slice/array incl. named variants (net.IP,
  [N]byte) via isByteSliceOrArray, plus one pointer level only (*[]byte ok,
  **[]byte still flagged, *string still flagged), (c) error / fmt.Stringer
  via types.Implements incl. pointer-receiver Stringer (*T checked too),
  (d) any interface type (deliberate: dynamic type unknown).
  stringerInterface is built structurally (no fmt-package resolution needed);
  NewSignatureType arity verified correct for go1.27 (6 params incl.
  recvTypeParams) via `go doc`.
- Tests (new file internal/vet/printf_test.go, 208 lines, 5 tests):
  positive accept cases, negative control expecting exactly 9 diagnostics,
  star-arg consumption, line-discriminated alignment proof (value-arg line 8),
  missing-arg counting. No helper-name collisions with existing vet tests.
  gofmt clean on both files. No other file references starWidth/starPrec.
- One deliberate over-acceptance, noted not blocked: any interface type is
  accepted for %s/%q (false-negative risk only, e.g. interface{Foo()}),
  beyond the finding's letter; the finding's false-positive concern is met.

## Verification: RUN — RED→GREEN + full vet suite GREEN (2026-11-09, after reboot #116)
- Resume dispatch 11:58 CET (boot 53efdb22); re-verified disk state first:
  HEAD still 20a0399b, fix uncommitted, no live owner, no post-boot commits.
- 8 MiB home quota probe PASSED at ~11:59 CET (quota window reopened after
  the rev1109-p0-4-fuzz-soak worktree removal at 10:53Z); backoff was active
  (load 18–23, frozen processes) until ~11:14 UTC — self-gating runner
  (r05-verify-runner.sh, pid 44767, relaunched 11:00 UTC) waited, then ran.
- RED (printf.go reverted to HEAD, new tests kept): rc=1 — 4 of 5 new tests
  FAIL as designed (StringVerbAcceptsValidTypes, StarArgsConsumeArguments,
  PrecisionStarChecksValueArgument, StarArgsCountTowardMissingArgument);
  the negative control (StillRejectsInvalidTypes) passes on old code too,
  as expected (old code over-flagged). Log: r05-verify-red.log.
- Restore: fix restored byte-identical (sha256sum -c OK, rc=0).
- GREEN targeted: rc=0 — all 5 new tests PASS (17.2s). Log: r05-verify-green.log.
- FULL vet suite: rc=0 — ok github.com/CAFxX/graalgo/internal/vet, 251.6s,
  34 PASS, 0 FAIL. Log: r05-verify-full.log.
- Committed this run (explicit pathspecs only): internal/vet/printf.go,
  internal/vet/printf_test.go, R05-VET-PRINTF-EVIDENCE.md. No push, no
  rebase, no integrate — master is at a0df481b; integration is the
  coordinator's call. Runner logs (r05-verify-*.log/.rc/.txt) stay untracked
  in the worktree as raw evidence.

## Earlier block (superseded — verification has now run)
- 09:39 UTC: 8 MiB unique-content probe on HOME PASSED (transient window).
- 09:44 UTC: append to ~/memory/2026-11-09.md FAILED, EDQUOT.
- 09:44 UTC: fresh 8 MiB probe FAILED, EDQUOT (probe file removed after).
- 09:56 UTC: backoff CLEAR (load 2.19, no frozen processes) but 8 MiB probe
  FAILED again, EDQUOT.
- Self-gating runner (hidden_files/r05-verify-runner.sh, pid 109438, launched
  09:43 UTC) waited on backoff, saw CLEAR, ran its quota re-probe, FAILED,
  and exited (rc 3 path) — it could not write its summary/rc files because
  new-file creation on HOME fails under EDQUOT. No verification ran; the
  worktree was never touched by the runner (red step never started).
- Load gate is OPEN; quota gate is CLOSED. Nothing was committed (no SHA).
  No push, no rebase, no integrate — as briefed.

## Resume instructions (for coordinator / next dispatch)
- Unblocker: home-quota headroom (stale-worktree cleanup authorization
  outstanding; ~143G held). When quota opens, re-probe 8 MiB on HOME first.
- Runner script preserved at:
  ~/workspace/goals/graalgo-go-toolchain-on-graalvm-truffle/hidden_files/r05-verify-runner.sh
  Re-launch it detached (setsid nohup ... & disown); it re-probes quota,
  then runs red->green (targeted tests) + full vet suite, logs + .rc files
  in the worktree. Then commit with EXPLICIT pathspecs only
  (internal/vet/printf.go internal/vet/printf_test.go + this evidence file),
  mark registry entry done.
- Registry: graalgo-review-05-vet-printf, status active (heartbeat kept
  fresh); note field records this block.
- Worktree left intact with the fix uncommitted, exactly as verified.
