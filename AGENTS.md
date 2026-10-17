# AGENTS.md — operating manual for graalgo

This is the working contract for agents (and humans) editing this repo.
Where things live: [docs/REPO_STRUCTURE.md](docs/REPO_STRUCTURE.md).

## 1. Build & test

- Toolchain: `~/tools/go/bin/go` (go1.27.1). Export it on `PATH` or pass
  `GO=~/tools/go/bin/go make ...`.
- `make all` — the per-push gate: regenerate generated artifacts
  (`generate`), compile everything (`build`), verify behavior snapshots
  (`snapshot-check`), run the grep/format gates (`check`), run the fast
  in-JVM tests (`test-ci`). Then CI runs `git diff --exit-code`: if `make
  all` regenerates any tracked file differently from what's committed, the
  build fails. **Push only after `make all` is green locally.**
- `make check` — placeholder / debug-leftover / netpoller / exclusion-list
  grep gates + gofrontend gofmt/vet/tests.
- `make test` — the full differential suite (30+ minutes). Not per-push;
  for local verification and nightly runs. Invoke explicitly.
- `make snapshot-check` — re-runs the differential corpus under graalgo and
  fails on drift from the committed baseline in
  [test/snapshots/](test/snapshots/). Any commit that changes observable
  behavior must fail loudly here — never silently.
- `make snapshot` — **re-records** the baseline. Run only for a toolchain
  upgrade or a deliberately re-baselined behavior change, then commit the
  result. Never run to make a red check green.
- Per-package Go tests: `go test ./...` inside the package's own module
  directory (see [docs/REPO_STRUCTURE.md](docs/REPO_STRUCTURE.md)).
- Differential harness: [test/differential/run.sh](test/differential/run.sh)
  (moved here from `tools/diffharness/` with the test-reorganization lane,
  2026-10-13).

## 2. Worktree discipline

- `master` is **integration-only**: never edit, build, or commit there. It
  advances by fast-forward merges only.
- Claim an exclusive worktree per lane:
  `~/workspace/system/bin/wt.py new graalgo <stream> --chat <chat-id>`
  (branch `wt/<chat>/<stream>`, registry-enforced exclusivity). Work only in
  your worktree; the coordinator integrates via `wt.py integrate` (merge
  lock + pristine-main check + `--ff-only`; non-ff → you rebase in your
  worktree and re-verify).
- Commit with **explicit pathspecs** — never bare `git commit` — when anyone
  else may have staged changes. On this shared tree, never `git stash`
  blindly: check `git status` for others' in-flight files first.
- Every commit is pushed immediately after landing, and CI is watched to
  verdict (green or explained failure) before the lane is called done.
- **Review trailers (his 2026-10-15 order, mechanically enforced):** every
  commit merged to master carries `Reviewed-by:` + `Review-verdict: clean`
  trailers from the independent review; CI fails pushes that lack them.
  Convention: [docs/dev/review-trail.md](docs/dev/review-trail.md).

## 3. Correctness contract

- graalgo's observable behavior is defined by `gc` (the reference Go
  toolchain). **Any change that can alter behavior must run the differential
  probe** (`make snapshot-check`, and the corpus differential for the
  affected area) and show no drift — or the drift is deliberate, reviewed,
  and re-baselined with `make snapshot` in the same change.
- Tests are never tautological: every test must be demonstrably capable of
  failing. If a bug escapes, the suite was insufficient — hunt the error
  class, patch the process, don't just fix the instance.
- Binary done-gates for every change: spec exists; no placeholders; suite
  green; independent review findings remediated and re-tested. "Works in the
  happy path" is not done.

## 4. Generality rule

- Fixes are **structural, never name-keyed**: fix the mechanism, never
  special-case a package, symbol, or test name. A fix that only works for
  the failing case is not a fix.
- **Upstream Go is never modified.** [stdlib/patches/](stdlib/patches/)
  holds minimal diffs against upstream sources when a package cannot
  compile unmodified — the fix still goes into graalgo, with the patch
  documenting what unblocks the real sources (see
  [docs/REPO_STRUCTURE.md](docs/REPO_STRUCTURE.md) §Principles).
- Synthetic stdlib packages are honest workarounds, not destinations: each
  implementation (in [internal/frontend/](internal/frontend/) as `synth_*.go`)
  states why it is synthetic and what root-cause fix retires it. The full
  bridge inventory — every grandfathered bridge, its root-cause fix, and the
  phased removal order — lives in
  [docs/arch/bridge-inventory.md](docs/arch/bridge-inventory.md); the
  no-bridges gate ([.github/scripts/check_no_bridges.py](.github/scripts/check_no_bridges.py))
  grandfathers exactly what is inventoried there.

## 5. No-debris rule

- `hidden_files/` must **never reappear**. Per-run evidence belongs in
  commit messages, not in untracked scratch directories.
- Build artifacts (`out/`, `*.err`, binaries) are gitignored and deleted,
  never committed.
- Research scratch (wave logs, run dumps, speculative drafts) is
  consolidated into [docs/arch/](docs/arch/) / [docs/dev/](docs/dev/) or
  deleted — never left lying in the tree.

## 6. Performance sensitivity

- This project is a performance play (JIT-compiled Go on GraalVM). Treat
  every commit as performance-sensitive: no accidental quadratic paths, no
  allocations on hot paths, no synchronized I/O in the scheduler loop.
- **Every VM command runs under `nice`** — default `nice -n 10`, `nice -n
  19` for heavy batch compute (benchmarks, sweeps, bulk test runs). Never
  renice the agent runtime or system daemons — worker compute only.
- Before dispatching heavy work (workers, builds, test runs), run
  `~/workspace/system/bin/load-shed.sh backoff`: exit 0 = clear, exit 1 =
  back off and retry later. Don't pile onto a saturated box.
- Heavy embarrassingly-parallel batch work goes to GitHub Actions via the
  fanout repo by default; the VM keeps the tight edit/run/debug loop.

## 7. Where things live

[docs/REPO_STRUCTURE.md](docs/REPO_STRUCTURE.md) is the authoritative map:
full directory tree with one-line descriptions, the old→new mapping table
from the research era, layout principles, and what is intentionally not in
the repo. When you add a directory or move code, update that document in
the same change — **docs are part of the change, never a follow-up** — and
every file/dir name you write in a doc is a clickable relative link
(mechanically enforced by
[.github/scripts/check_markdown_links.py](.github/scripts/check_markdown_links.py);
run it before pushing doc changes).
