# AGENTS.md — operating manual for graalgo

This is the working contract for agents (and humans) editing this repo.
**Start at [docs/MAP.md](docs/MAP.md)**: task → place, the new path's data
flow, and what is legacy v1 (deleted at M3 — never build on it). Full
annotated tree: [docs/REPO_STRUCTURE.md](docs/REPO_STRUCTURE.md). The
current plan of record is [docs/arch/RECOVERY.md](docs/arch/RECOVERY.md);
[docs/arch/SPEC.md](docs/arch/SPEC.md) is authoritative.

## 1. Environment

- Toolchains (see [docs/dev/ENV.md](docs/dev/ENV.md)):
  `export PATH=~/tools/go/bin:~/tools/graalvm-jdk-25/bin:$PATH
  JAVA_HOME=~/tools/graalvm-jdk-25` — Go go1.27.1 at `~/tools/go`, Oracle
  GraalVM JDK 25 at `~/tools/graalvm-jdk-25`. The Makefiles honor `GO=` and
  `GRAALVM_HOME=`.

## 2. Build & test

- `make all` — the per-push gate, in order: `generate` (gofrontend,
  overlay, the 18 generated `synth_*.go`, the GIR Go/Java codecs), `build`
  (truffle + interop Java modules), `snapshot-check`, `check`, the module
  tests `test-v2`, `test-toolexec`, `test-gir`, `test-p3frontend`, then
  `test-ci` (smoke + `test-jit`), `test-java-fast`, `test-regression`,
  and the new path's gates `test-m1a` (import-free corpus vs gc through
  the `-toolexec` path), `pe-check-m1a`, `test-m1w13`, `test-imgcheck`,
  `test-m1r` and `test-m1c` (the Makefile header is the exact list). CI
  then fails if `make all` left any modified **or untracked** file
  (`git diff --exit-code` + `git status --porcelain`).
- `make pre-land` — **the pre-land checklist; a lane runs it in full
  before reporting done**: `make all`, then `check-links-external` (the
  markdown-links workflow: every external URL, network; local `make
  check` skips them; a 403/429/5xx/timeout that survives the retries is an
  *unverifiable* warning, never a failure — only 404/410/NXDOMAIN fail) and `test-diffharness` (ci.yml's harness self-test),
  then CI's drift check. Never a hand-picked subset of targets: gates
  share inputs (a new `test/m1a/programs` file is also an input of
  `test-toolexec`'s reference-interpreter differential), so "the gate for
  my area passed" is not evidence. Per-push CI outside `pre-land`: only
  the path-filtered `conformance.yml` (needs a full GOROOT/test tree) and
  `fuzz-newpath.yml` (`make fuzz-newpath`; run it when `test/fuzz/`
  changes).
- `make check` — the static gates: pinned-jar checksums; placeholder,
  debug-leftover, v1-netpoller, off-heap/NIO (only `graalgo.syscall` may use
  `MemorySegment`/`Arena`/`java.nio.channels`) and pin-surface grep gates;
  duplicate-FQN; dead-path; MR-table subset; repo gates (tracked-file
  size/binary gate, doc-path existence, GG-id existence, Makefile
  `.PHONY`/header consistency, generated-files list, placeholder/TODO and
  stub ratchets (`unimplemented`, new-path `Unsupported`/`unsupportedf`),
  the new-path/v1 layering gate, the no-bridges gate — each with its own
  planted-violation test); every `.github/scripts/test_*.py`; `tools/req
  lint` (rules (a)–(f)); and the gofrontend `build.sh check`
  (gofmt/vet/tests, vendoring diff, wire-format validator).
- **Forward-only gates** compare against a base: `$GATE_BASE` (CI: the
  push's `before` sha or the PR base), else the merge-base with
  `origin/master`. The no-bridges gate inspects every commit since the
  base; ratchet budgets may not rise above the base's; `LOCKED-TEXT.sum`
  lines are only added and requirement statuses never move back (`req
  lint` rule (f)); `req seal` is one-shot.
- `make test-java-slow` — the slow Java classes (the weekly CI
  `full-suite` job, Mondays). Known
  failures are listed in [test/java/quarantine.txt](test/java/quarantine.txt)
  (class, reason, owner — a RECOVERY.md track/work-package id or
  `lane:<name>` —, expiry — a future date or a RECOVERY.md milestone —;
  validated by `make java-test-lists-check`); the target fails on any failure
  outside the quarantine **and** on a quarantined class that now passes.
- `make test` — the full differential suite (30+ minutes): never per-push,
  never on a shared host without asking; the weekly CI `full-suite` job
  runs it. (Nightly: `mutation.yml`, `fuzz-soak.yml` and `fuzz-newpath.yml` —
  `make fuzz-newpath`, the new path's differential fuzzer; the layer-3
  `stdlib-gate.yml` is manual-only until BUG-0033 is triaged.)
- Per-module Go tests: `go test ./...` inside the module's own directory
  (each of `internal/toolexec`, `internal/frontend/v2`, `tools/*`, … has its
  own `go.mod`).

## 3. Snapshots and expected divergences

- [test/snapshots/](test/snapshots/) records **gc's** behavior on the
  differential corpus — never graalgo's. `make snapshot` re-records gc and
  runs only when a corpus program changes or on a toolchain upgrade.
- `make snapshot-check` runs the corpus under graalgo and fails on any
  divergence from gc's snapshot. A known graalgo divergence is a reviewed,
  reason-coded (SPEC §5.6: `backend-specific`, `unspecified-behavior`,
  `deferred-parity`) entry in
  [test/differential/corpus-exclusions.yaml](test/differential/corpus-exclusions.yaml);
  graalgo bugs carry an id from [docs/dev/bugs.md](docs/dev/bugs.md), and
  each entry pins the `signature:` of its divergence (a listed program that
  diverges in a new way is DRIFT; an unpinned one fails). A listed program
  that now matches gc fails as STALE. A failure that only an infra retry
  makes pass is counted against `-retry-budget` (default 0): flakes are
  bugs. **Never run
  `make snapshot` to silence graalgo** — it cannot (it records gc), and an
  exclusion without review is the same lie.
- `make check` enforces the register rules (`diffharness registers`):
  every `deferred-parity` entry cites a `BUG-NNNN` row of bugs.md, every
  cited id exists, and every individual entry of a differential register
  pins its `signature:` — or says `signature: unpinned (<why>)` when the
  divergence text is not reproducible (timings, timestamps) or the file
  never runs.
- The same rules hold for
  [srctest-exclusions.yaml](test/differential/srctest-exclusions.yaml),
  [typeparam-exclusions.yaml](test/differential/typeparam-exclusions.yaml)
  and [tools/stdlib-fork/exclusions.yaml](tools/stdlib-fork/exclusions.yaml).
- **Timing-dependent programs.** A listed program that is a wall-clock check
  (a timeout, a "too slow" assertion: its outcome follows host speed and
  load, so a perf win or a faster runner makes it match gc) carries
  `timing: true`: matching gc is reported `TIMING` ("passes
  (timing-dependent)"), not STALE, while a divergence is still judged by
  the entry's signature. The marker needs a `BUG-NNNN` in the detail and a
  signature, and the count of marked entries is the shrink-only
  `timing-excl` ratchet (`.github/scripts/ratchet_budget.json`): never use
  it for a program that fails for any other reason.

## 4. Worktrees and commits

- The integration checkout is `~/dev/graalgo` on `master`, which advances by
  **fast-forward merges only**; never edit, build or commit there.
- One git worktree + branch per lane under `~/dev/graalgo-wt/<lane>`
  (`git worktree add ~/dev/graalgo-wt/<lane> -b wt/<area>/<lane> master`).
  Work only in your worktree; the coordinator fast-forwards `master`. A
  non-fast-forward means you rebase in your worktree and re-verify.
- Commit with **explicit pathspecs**; never `git add -A` or a bare
  `git commit -a`. Never `git stash` in a tree someone else may be using.
- Push only when the coordinator asks; watch CI to a verdict before calling
  a lane done. A lane's report states that `make pre-land` passed (with
  its duration) on the commit it hands over.
- The coordinator watches **every** workflow of every push to `master`
  (`gh run list --commit <sha>`) to a verdict before the next merge; a
  red `master` is fixed (or the push reverted) before anything else
  lands.

## 5. Correctness contract

- Observable behavior is defined by `gc`. Any change that can alter
  behavior runs the differential probe for the affected area (a
  `SNAPSHOT_PROGRAMS=` slice of `make snapshot-check`, the relevant corpus
  runner) and shows no new divergence.
- Tests are never tautological: every test (and every gate) must be
  demonstrably capable of failing — gates ship with a test that plants the
  violation they guard. If a bug escapes, hunt the error class and patch the
  process, not just the instance.
- Done means: spec exists; no placeholders; gates green; independent review
  findings remediated and re-tested.
- **Periodic global reviews**: at each milestone (and at least weekly while
  lanes are active) an independent reviewer audits the whole tree against
  SPEC and this file; findings become lane work items with tests, not
  notes.

## 6. Generality, synthetic code, and upstream

- Fixes are **structural, never name-keyed**: never special-case a
  package, symbol, or test name.
- **Upstream Go is never modified.** [stdlib/patches/](stdlib/patches/)
  holds minimal diffs when a package cannot compile unmodified.
- **No new `synth_*.go`.** The existing synthetic stdlib files in
  [internal/frontend/](internal/frontend/) and the Java std
  reimplementations in [internal/backend/interop/](internal/backend/interop/)
  are frozen and are **all deleted at M3** with the v1 pipeline
  ([toolexec-design.md](docs/arch/toolexec-design.md) D11/D12). That is the
  whole interop module (every package in it is a v1 stand-in), not the
  runtime library the new path shares; the complete list of what M3
  deletes and what stays is toolexec-design §9.1. The frozen
  inventory is [docs/arch/bridge-inventory.md](docs/arch/bridge-inventory.md);
  the no-bridges gate
  ([.github/scripts/check_no_bridges.py](.github/scripts/check_no_bridges.py))
  rejects additions.

## 7. Resources and hygiene (shared host)

- Every heavy command runs under `nice -n 19` (`nice -n 10` for short
  interactive builds). One JVM / heavy process at a time per lane; JVMs with
  `-Xmx2g -XX:ActiveProcessorCount=4` (`GRAALGO_JAVA_OPTS`,
  `JAVA_TEST_OPTS`); `make -j2` at most; `SNAPSHOT_JOBS=1` for snapshot
  slices. Kill stray processes you started.
- JVM crash logs go to scratch, never the repo:
  `-XX:ErrorFile=<scratch>/hs_err_pid%p.log`. Scratch lives outside the
  tree (or in a gitignored `.tmp/`); `git status` is clean when a lane
  ends.
- No debris: no `hidden_files/`, build outputs, binaries, archives, JFR
  recordings or logs > 1 MB in git (the size/binary gate enforces it;
  exceptions go in
  [.github/scripts/large_files_allowlist.txt](.github/scripts/large_files_allowlist.txt)
  with a reason). Per-run evidence goes in commit messages.
- Performance: treat every commit as performance-sensitive — no accidental
  quadratic paths, no hot-path allocations, no synchronized I/O in the
  scheduler loop.

## 8. Docs

[docs/REPO_STRUCTURE.md](docs/REPO_STRUCTURE.md) is the authoritative map.
Docs are part of the change: add/move a directory → update the map in the
same commit. Every repo path written in a non-archived doc must exist
(links checked by
[check_markdown_links.py](.github/scripts/check_markdown_links.py),
backticked paths by `check_doc_paths.py` in `make check`); mark a path that
does not exist yet as "(planned)".
