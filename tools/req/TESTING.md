# TESTING.md — `req` CLI test suite

Test suite for the `req` requirement-registry CLI (`tools/req/`: `add`,
`lint`, `trace`, `tree`, `supersede`, `retire`). 127 test functions in
package `main`, all hermetic.

## Hermeticity contract

Every test builds its registry and scanned tree inside `t.TempDir()`.
**No test touches the real `~/workspace/graalgo/requirements/` tree.**
The only exceptions are the `findRoot` tests, which `chdir` into a temp
dir and restore afterwards. There is no `t.Parallel` anywhere in the
package: several tests swap `os.Stdin`/`os.Stdout`/`os.Stderr` and the
working directory, so all tests run sequentially in one process.

Shared fixtures live in `testutil_test.go`: `testRoot`, `reqFile`
(registry YAML builder), `ctxFlags`, `stdLayout`, `captureOutput`,
`withStdin`, `stubTerminal`, `withCwd`, `snapshotTree` /
`assertTreeIdentical`, `requireErrContains`.

## How to run

```sh
export PATH=~/tools/go/bin:$PATH
cd ~/workspace/graalgo/tools/req

go build ./...            # must succeed
go vet ./...              # must be clean
gofmt -l .                # must print nothing
go test -count=1 ./...    # full suite, no cache
go test -count=1 -race ./...      # race detector
go test -count=1 -shuffle=on ./...# order-independence check
go test -count=1 -coverprofile=/tmp/cover.out .
go tool cover -func=/tmp/cover.out | tail -1
```

`req lint` over the repo must also stay clean (it runs in CI):

```sh
go run . lint --root ~/workspace/graalgo
# lint ok: 70 requirement(s), 396 reference(s)
```

## Coverage: 96.3% of statements

Measured with `go test -coverprofile` (go1.27.1; the suite was also
validated on go1.24.7). Every uncovered block is listed below with its
justification. The guiding principle: unreachable-by-construction code
stays covered by a proof sketch, not by contorting the code to be
testable.

| Location | Lines | Why uncovered |
|---|---|---|
| `main.go` | 12–13 | `main()` is an `os.Exit(run(os.Args[1:]))` wrapper. `run()` is fully tested; calling `main()` in-process is impossible (`os.Exit`), and a subprocess's coverage does not merge into the test profile. |
| `registry.go` | 49–50 | `findRoot`: `os.Getwd` error. Fails only if the cwd was deleted mid-run; unreachable in tests. |
| `registry.go` | 70–71, 78–79 | `resolveCtx`: `filepath.Abs` errors on already-validated paths. |
| `registry.go` | 133–134 | `loadRegistry`: "malformed id". Unreachable: the filename must match `^(MM-\d{4,})\.yaml$` and the record's `id:` must equal the filename, so the ID necessarily matches `^MM-\d{4,}$`. |
| `registry.go` | 139–140 | `loadRegistry`: "duplicate requirement id". Unreachable: filenames are unique within a directory and each record's ID must equal its filename. |
| `registry.go` | 150–151 | `save()`: `yaml.Marshal` error. The struct is plain strings/slices of strings with no custom marshalers; it cannot fail. |
| `scan.go` | 71–72 | `rel = path` fallback when `filepath.Rel` fails. Unreachable: `root` and `scanDir` are absolute by construction (`resolveCtx` applies `filepath.Abs`). |
| `scan.go` | 129–130 | `scanAll` returning the `WalkDir` error. Unreachable: the walk callback never returns non-nil (unreadable paths are skipped by design). |
| `commands.go` | 120–121 | `historicalRef`: `filepath.Rel` error. Unreachable on Linux: both arguments are absolute. |
| `commands.go` | 137–138, 283–284, 569–570 | `scanAll` error propagation in `lint`/`trace`/`supersede`. Defensive only; `scanAll` cannot return a non-nil error (see `scan.go` 129–130 above). |
| `commands.go` | 340–342 | `tree` `[CYCLE]` guard. Unreachable: each requirement has exactly one parent, so the children edges form a forest; the DFS from roots visits each node once. |
| `commands.go` | 596–597 | `planFileEdits` error inside `cmdRetireSupersede`. TOCTOU-only (a file changing between scan and plan); covered at unit level in `supersede_test.go`. |
| `commands.go` | 602–603 | Old-record `os.ReadFile` error. TOCTOU-only (`loadRegistry` just read the same file successfully). |
| `commands.go` | 607–608, 623–624 | `yaml.Marshal` errors when staging registry writes. Unreachable for this struct (see `registry.go` 150–151). |

## Red-green evidence

Each mutation below was applied to the source, the named tests were
observed to **fail**, the source was restored byte-identical (verified
with `diff`), and the tests were observed to pass again. RG3 was
re-run against the current `scan.go` after the `*_test.go` scan-contract
change (see below).

| # | Mutation | Failing tests observed | Revert |
|---|---|---|---|
| RG1 | `historicalRef` forced to return `false` (rule-(b) historical exemption disabled) | `TestLint_Clean` ("clean tree should lint: 5 lint problem(s)"), `TestLint_RuleB_HistoricalRecordsExempt` ("…: 6 lint problem(s)"). `TestLint_RuleB_RetiredRef` still **passed** — the mutation is precise, not a blanket break. | green |
| RG2 | `commitWrites` rollback loop removed (write error returns without restoring) | `TestCommitWrites_RollbackRestoresOriginals`, `TestCommitWrites_RollbackRemovesNewFiles`, `TestSupersede_MidCommitFailure_Rollback` | green |
| RG3 | `verifies:` annotation no longer promoted to `KindVerifies` in `scanAll` | `TestScan_BasicRefAndVerifies`, `TestScan_VerifiesLineWithTwoIDs`, `TestScan_TestFilesOnlyVerifiesScanned`, `TestLint_Clean` (rule (c) collapses without verifies refs). `TestLint_RuleC_ActiveNeedsVerifies` still passed (negative case, unaffected). | green |
| RG4 | `mintID`: `max+1` → `max` (off-by-one, reuses the max ID) | `TestMintID_Empty`, `TestMintID_GapNotFilled`, `TestMintID_NeverReusesRetired`, `TestCmdAdd_Monotonic`, `TestSupersede_ZeroHits` | green |
| RG5 | `absRefPath` returns the repo-relative path unchanged (no `--root` anchoring — the old buggy behavior) | `TestAbsRefPath`, `TestPlanFileEdits_RootAnchored`, `TestSupersede_WithHits_InteractiveUpdate` | green |

## Failure injection

The suite runs as **root**, so chmod-based write-failure injection is
useless (root bypasses permission bits). Failures are injected with the
directory-as-file trick instead: `os.WriteFile` to a directory path
always fails, even for root.

- **Mid-supersede rollback** (`TestSupersede_MidCommitFailure_Rollback`):
  registry holds MM-0001 (active) + MM-0002 (draft), so `mintID`
  targets `MM-0003.yaml`; that path is blocked with a directory, so the
  registry write fails mid-commit after code-file edits were staged.
  Asserts: the error surfaces, `loadRegistry` skips the directory,
  originals are restored, no new files remain, and the whole tree is
  byte-identical to a pre-run `snapshotTree`
  (`assertTreeIdentical`). This is the test that enforces the task's
  "mid-operation failure leaves the registry byte-identical" bar.
- **`commitWrites` unit level**: failure on the 2nd of 3 writes restores
  write #1 and removes the new file (`TestCommitWrites_RollbackRestoresOriginals`,
  `TestCommitWrites_RollbackRemovesNewFiles`).
- **Registry load**: malformed YAML, dangling symlink, filename/ID
  mismatch, bad status, missing dir (`ReadDir` error).
- **Interactive prompt**: EOF on stdin, unknown key then valid key,
  abort at first/second prompt, non-interactive refusal when stdout is
  not a terminal (`stubTerminal(false)` is mandatory — real stdin under
  `go test` may be a char device).
- **Real `isTerminal`**: `/dev/null` → true (char device), pipe →
  false, closed file → false. Deterministic, no PTY needed.

## Scan contract for `*_test.go` files

Deliberate product decision (2026-10-01, documented in
`docs/requirements-registry.md`): **in `*_test.go` files the scanner
collects only `verifies: MM-XXXX` annotations** (`kind=verifies`).
Plain ID mentions are skipped and rule-(d) sloppy-format checks are
skipped there.

Rationale: test sources must be free to use arbitrary fake IDs in
fixtures and string literals (`MM-0099`, `MM-42`, …). Before this
contract, `req lint` over the repo reported 39 false positives picked
up from the linter's own test files, making lint unusable on a repo
that contains its test suite.

Tradeoff (accepted): plain ID mentions inside test files are **not**
dangling-checked (rule a) and sloppy IDs there are **not**
format-checked (rule d). The only references with semantic meaning in
test files are the `verifies:` annotations driving lint rule (c):
every active requirement still needs ≥1 `verifies:` from somewhere,
test files included.

Regression test: `TestScan_TestFilesOnlyVerifiesScanned`
(`scan_testonly_test.go`) — a `_test.go` fixture containing
`MM-0099` (would-be ref), `MM-42` (would-be format problem) and a
`verifies: MM-0001` asserts only the annotation is collected.
`TestScan_VerifiesLineWithTwoIDs` and
`TestScan_VerifiesIsCaseSensitive` carry `NOTE(scan-contract)`
comments pinning the new behavior.

**Do not write tests asserting the old contract** (plain refs collected
from `_test.go` files). Fixture IDs inside `*_test.go` string literals
are safe by design — keep them there.

## Bugs found

Fixed during this work (all flagged):

1. **Wrong-directory writes on supersede/retire** — `applyDispositions`
   resolved repo-relative hit paths against the process working
   directory instead of `--root`. Running `req supersede --root X`
   from another directory edited/created files in the wrong tree.
   Fixed by anchoring every disposition path at `--root`
   (`absRefPath`); regression tests `TestAbsRefPath`,
   `TestPlanFileEdits_RootAnchored`; red-green RG5.
2. **No atomicity on supersede/retire** — code-file edits and registry
   YAML writes were applied sequentially with no rollback, so a
   mid-operation failure left the registry and code half-updated.
   Fixed by splitting into `planFileEdits` (pure in-memory planning)
   + `commitWrites` (single atomic commit: restores originals and
   removes new files on any write error); regression tests
   `TestSupersede_MidCommitFailure_Rollback`,
   `TestCommitWrites_Rollback*`; red-green RG2.

Reported, not fixed (out of scope for the test-suite task; see below —
item 3 was fixed separately after the suite landed):

3. **Relative `--req-dir` breaks the rule-(b) historical exemption** —
   with a relative `--req-dir`, `historicalRef` calls
   `filepath.Rel(relativeReqDir, absolutePath)`, which errors (verified
   empirically), so it returns `false` and registry-internal refs to
   superseded/retired IDs are falsely flagged. **Fixed 2026-10-01:**
   `resolveCtx` now absolutizes `--root`, `--req-dir`, and `--scan-dir`
   unconditionally (the bug class was mixed relative/absolute pairs, not
   just `--req-dir`); regression test
   `TestLint_RelativeFlagsKeepHistoricalExemption` with red-green
   evidence (fails without the fix: registry history flagged as rule-(b)
   violations; passes with it).
4. **Scanner does not enforce the "directly above" convention** —
   `AGENTS.md` says `// verifies: MM-XXXX` goes directly above the
   test function, but the scanner treats any `verifies:` line as an
   annotation regardless of position. Pinned by
   `TestScan_VerifiesPositionNotEnforced`; enforcement is a product
   decision, not a test bug.

Pinned but intentionally unchanged: the CLI prints "supersedeing"
(from a `"%sing"` format verb) — pre-existing wording, asserted as-is.

## Source changes made for testability (flagged)

1. `main.go`: extracted `run(args []string) int` from `main()` so CLI
   dispatch is testable without a subprocess.
2. `commands.go`: `parseFlags` no longer returns an error (it never
   failed) — removed 5 unreachable `if err != nil` branches.
3. `commands.go`: `isTerminal` changed from func to package-level var —
   test seam for the interactive prompt; production behavior unchanged.
4. `commands.go`: `applyDispositions` replaced by `planFileEdits` +
   `commitWrites` (see bug #2 above).
5. `scan.go`: the `*_test.go` scan contract (product change by the
   parent agent, 2026-10-01 — not mine; my suite was adjusted to it).

No commits were made; no changes to the real requirements tree.

## Environmental note (2026-10-01)

Mid-task, the Go toolchain at `~/tools/go` was swapped from go1.24.7
to go1.27.1 by a concurrent agent. During the swap, several
`go test -race` runs failed intermittently (14, then 2, then 2
failures in scan tests, varying between runs) while plain `go test`
runs stayed green. After the install completed, `go clean -cache`
(to rule out build-cache entries written mid-swap), and repeated
`go test -race` (3×), `-shuffle=on`, and `-race -shuffle=on` runs:
all green, repeatedly. Verdict: environmental, caused by the
concurrent toolchain replacement — not a suite bug. The suite is
deterministic under `-race` and order-independent under `-shuffle=on`
in a stable environment. If similar flakiness ever recurs, check for
concurrent toolchain/environment mutation before suspecting the tests.
