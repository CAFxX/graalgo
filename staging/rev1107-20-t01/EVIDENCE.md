# R20-T01 remediation — RED/GREEN evidence (lane rev1107-20-t01)

Issue: REVIEW-2026-11-07-20 § R20-T01 (P1) — diffharness `typeparam run` /
`srctest run` accept zero comparisons as PASS; `snapshot record` writes a
0-program snapshot. HEAD verified against: graalgo master at lane dispatch
(59f0a247 per brief; files read from `~/workspace/graalgo`).

Staging dir: `/tmp/rev1107-20-t01/` (home quota CLOSED — see RESUME.md).
- `pristine/` — untouched copy of `test/differential` (diff base)
- `red/` — pristine + new tests only (RED demonstration)
- `work/` — pristine + new tests + fix (final state, GREEN)
- `rev1107-20-t01.patch` — unified diff pristine→work (5 files, +94/−0
  content lines); verified: `patch -p1` from repo root applies cleanly and
  reproduces `work/` byte-identical (`diff -r` clean).
- `fixtures/` — CLI fixtures (empty dir, exclusions yaml, backends.json,
  all-excluded corpus)
- `diffharness-red`, `diffharness-fixed` — built binaries (removed at
  handoff to save /tmp; rebuild with `go build ./cmd/diffharness`)

Go env for every command below (home quota closed, so no default cache):
`PATH=$HOME/tools/go/bin:$PATH GOCACHE=/tmp/rev1107-20-t01/gocache
GOTMPDIR=/tmp/rev1107-20-t01/tmp GOPATH=/tmp/rev1107-20-t01/gopath
GOFLAGS=-mod=mod GOPROXY=off`. Go 1.27.1.

## Mechanism verified from disk truth (before fixing)

- `typeparam.Run` (`internal/typeparam/typeparam.go:276-332`): `included`
  empty → `comps = make([]Comparison, 0)`, workers no-op, returns
  `([], nil)`. Only two callers: `cmdTypeparamRun`, `cmdSrctestRun`.
- `typeparam.Scan` (`:71-91`) / `srctest.Scan` (`srctest.go:70+`): empty dir
  → `([], nil)`; only a `ReadDir` error fails.
- `cmdTypeparamRun` (`main.go:451-517`) / `cmdSrctestRun` (`:567-636`):
  print "0 comparisons, 0 diverged" and return nil → exit 0; then
  `run_conformance.sh` (`set -euo pipefail`) prints "CONFORMANCE GREEN".
- `cmdSnapshotRecord` (`snapshot.go:288-360`): no guard on `len(progs)==0`;
  `DiscoverCorpus` errors only on a dir with zero program dirs, so an
  exclusions list covering every program records a 0-program snapshot.
- `cmdSnapshotCheck`: NOT touched — guarded already (recorded-missing loop
  + exclusion-drift gate), per the brief's correction.
- `cmdRun` / `loadCorpus`: NOT touched — out of scope (REVIEW-18 TL-03).

## The fix (5 files, all under test/differential/)

1. `internal/typeparam/typeparam.go` — `Run`: after the `included` filter,
   `if len(included) == 0 { return nil, fmt.Errorf("zero programs to
   compare (scanned %d files, all excluded or none found): refusing a
   vacuous PASS", len(files)) }`. Single choke point covering both commands
   (the only two callers).
2. `cmd/diffharness/main.go` — `cmdTypeparamRun` and `cmdSrctestRun`: after
   `Run`, `if len(comps) == 0 { return fmt.Errorf("<cmd> run: zero
   comparisons (scanned %d files): refusing a vacuous PASS", len(files)) }`.
   Defense in depth per the brief's literal instruction; commented as such
   (Run already errors first). Deliberate layering for the reviewer.
3. `cmd/diffharness/snapshot.go` — `cmdSnapshotRecord`: after
   `loadSnapshotDeps`, `if len(progs) == 0 { return fmt.Errorf("snapshot
   record: zero programs to record (corpus %s, %d excluded): refusing to
   write a vacuous snapshot", sf.corpus, len(skipped)) }`.
4. `internal/typeparam/typeparam_test.go` — `TestRunEmptyKeptSet` (all files
   excluded → error) and `TestRunNoFiles` (nil files → error). Minimal
   config: two backends `gc-a`/`graalgo`; no backend is ever invoked.
5. `cmd/diffharness/snapshot_test.go` — `TestSnapshotRecord_ZeroKeptSet`:
   one-program corpus, config excludes it, dummy `/bin/false` backend
   (never invoked — guard fires first); asserts error AND no file written.
   `-toolchain go1.27.1 -runs 1` keep it hermetic.

NOT changed: `scan` modes (informational, not a gate), `snapshot check`,
`cmdRun`/`loadCorpus`, any production/backend code. No bridges, no
name-keyed special cases; the guards are structural (empty kept set).

## RED (pristine code + new tests, `red/`)

Unit tests (all three FAIL as required — the tests are falsifiable):
- `TestRunEmptyKeptSet`: `Run with all files excluded: want error, got nil (vacuous PASS)`
- `TestRunNoFiles`: `Run with no files: want error, got nil (vacuous PASS)`
- `TestSnapshotRecord_ZeroKeptSet`: pristine record printed
  `record: run 1/1 complete (0 programs)` /
  `record: wrote 0 programs (+1 excluded) to .../snap.json` and returned nil.

CLI, built pristine binary (valid backends.json with `/bin/true` templates;
an earlier run with template-less backends failed at `LoadConfig` with
`backend "gc-a" has empty build template` — fixture fixed, not code):
- `typeparam run -dir <empty>`: `typeparam run: 0 comparisons, 0 diverged,
  0 excluded`, **exit 0**, report JSON `{"total": 0, ...}`.
- `srctest run -dir <empty>`: same shape, **exit 0**.
- `snapshot record` (all-excluded corpus): `record: wrote 0 programs
  (+1 excluded)`, **exit 0**, snapshot file contains `"programs": null`.

## GREEN (fixed code, `work/`)

- `gofmt -l cmd internal`: clean. `go vet ./...`: clean.
- `go test ./cmd/... ./internal/... -count=1`: all 5 packages ok
  (cmd/diffharness, harness, mutate, srctest, typeparam); the 3 new tests
  PASS; no regressions (full verbose list in the lane's shell history —
  rerun the same command to reproduce).
- CLI, built fixed binary:
  - `typeparam run`: `diffharness typeparam: error: zero programs to
    compare (scanned 0 files, all excluded or none found): refusing a
    vacuous PASS`, **exit 1**, no report file written.
  - `srctest run`: same error via the shared `Run` guard, **exit 1**, no
    report file written.
  - `snapshot record`: `diffharness snapshot: error: snapshot record: zero
    programs to record (corpus ..., 1 excluded): refusing to write a
    vacuous snapshot`, **exit 1**, no snapshot file written.

## Still owed (quota-blocked, see RESUME.md)

- Claim worktree + apply patch + re-run `go test ./cmd/... ./internal/...`
  in the worktree (must reproduce GREEN from the applied patch, not from
  `/tmp/work`).
- Independent review before merge (reviewer checks the deliberate
  Run+command double guard; the command-level checks are unreachable while
  Run guards — remove them if the reviewer prefers Run-only).
- `make all` / `make snapshot-check` unaffected (no behavior change when
  the kept set is non-empty; guards only fire on zero programs).
- Work-registry entry could NOT be created (registry write fails EDQUOT);
  lane assignment lives in this file + the dispatch handoff.
