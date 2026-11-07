# R-13 lane evidence — rev1107-22-r13 (REVIEW-2026-11-07-22, HEAD 59f0a247)

Lane: graalgo-rev1107-22-r13. Status: **BLOCKED-quota** — fix staged here,
no worktree claimed, no registry entry, no commit (per lane brief).

## 1. Mechanism re-verified from disk truth (main repo, read-only)

HEAD in `~/workspace/graalgo`: `59f0a247088f0e0b6db7e0623076866727dd873c`
(`59f0a247 B-02: plain launcher initializes the Go os surface`).

- `test/e2e/run_module.sh:23-24` (old): `GR` defaults to
  `$HOME/workspace/worktrees/graalgo/m9-modprobe`; `P3/P2/P1` are
  `$GR/phases/phase{3,2,1}`.
- `test -d ~/workspace/worktrees/graalgo/m9-modprobe` → **MISSING**.
- `ls phases` in repo root → **No such file or directory**.
- `ls "$P1"/lib/*.jar` (with the old default) → `No such file or directory`
  (glob left unexpanded). The old script's `CP` therefore ends with an empty
  jar segment and the `java -cp` invocation can never load P3SyncMain: all 8
  uuid cases fail. `bash -n test/e2e/run_module.sh` → SYNTAX-OK (the script
  is syntactically fine; it is the paths that are dead).
- Nothing invokes the script: `grep -rn "run_module|test/e2e" Makefile
  .github/` → no hits (only checksum-log noise in test-results). `make test`
  runs only the three backend module suites; the uuid milestone has no live
  gate. CONFIRMED.
- Live layout that replaces it: `internal/backend/graalgo-sync-run`
  (executable wrapper; owns JVM + classpath interop/runtime/truffle
  `out/classes` + `truffle/lib/*.jar`; forwards `--gmp=N` to P3SyncMain —
  verified in the wrapper source, `exec ... P3SyncMain "$@"`).
  P3SyncMain accepts `--gmp=N` (`P3SyncMain.java:162-169`) and multiple
  `file.go` args (`:693`). `graalgo-sync-run` requires `make build` first
  (checked: `P3SyncMain.class` exists in the main tree).
- gofrontend resolution: `ToolPaths.gofrontend()` walks up to
  `internal/frontend/gofrontend` (built by `make generate`); absent in the
  main tree today (never generated there — expected, not a finding).
- Module imports on the graalgo leg are viable: `internal/frontend/diskimport.go`
  resolves third-party imports from `$GOMODCACHE/<path>@<highest version>`
  (then GOPATH/src, then GOROOT/src). `uuid@v1.6.0` IS in the module cache
  (`~/go/pkg/mod/github.com/google/uuid@v1.6.0`). BUG-MODPROBE-005/006/007
  fixes are all present in-tree (`mr_disk.go:383,443` hoistNestedMRInBlock;
  `export.go:11997` disk fallback for qualified const/var; WS G overlay
  importer `export.go:620`). So the 8 cases have a real chance of passing;
  the verification run is still owed (see RESUME.md).
- `test/e2e/uuid/go.sum` was never tracked (`git ls-files test/e2e/` lists
  no go.sum). The gc leg (`GOFLAGS=-mod=mod`) writes it on first run —
  confirmed empirically: the /tmp probe copy gained a `go.sum` during the
  wiring attempt (staged here as `go.sum.generated`, evidence only).
- CI wiring point: `.github/workflows/ci.yml` `full-suite` job (weekly
  Mon 04:00 + workflow_dispatch, `timeout-minutes: 360`) runs `make test`.
  Wiring `test-e2e` into `make test` therefore puts the probe in the weekly
  full-suite job, NOT per-push `make all`. No CI workflow change needed.

## 2. Quota probe (before any worktree claim — FAILED)

`printf 8 KB > ~/workspace/.quota-probe-rev1107-22-r13` →
`bash: ...: Disk quota exceeded`. No partial file left behind
(`ls` → No such file). Home quota EDQUOT-exhausted despite `df` showing
36G avail on /home/hatch (quota, not disk full). `/var/tmp` 100% full
(1.5M free). `/tmp` 512M tmpfs, 59–64M free at run time, heavily contested
by sibling lanes' staging dirs (r06 84M, fix0006 72M, r09 71M, f1 65M,
s04 40M) — not mine to delete. `/dev/null` is still a regular empty file
(REVIEW-20 anomaly, persists).

Per the lane brief: NO worktree claimed, NO registry entry written, NO
commit. All work staged in `/tmp/rev1107-22-r13/`.

## 3. Staged fix (contents of this directory)

- `new/test/e2e/run_module.sh` — rewired runner (full file; copy over the
  original in the worktree). Changes: GR defaults to the repo root derived
  from the script's location; P1/P2/P3 + hand-built CP + JAVA + gofrontend
  block deleted; graalgo legs call `graalgo-sync-run --gmp=N main.go`;
  fail-fast preconditions (exit 2) for GO, GraalVM java, launcher,
  P3SyncMain.class (`make build`), gofrontend (`make generate`); probe dir
  absolutized; differential logic (legs, byte-compare, panic first-line)
  byte-identical to the original.
- `new/Makefile` — patched Makefile (full file).
- `new/test/e2e/README.md` — patched README (Status section corrected:
  graalgo-sync-run, `make test-e2e`, weekly CI; go.sum note).
- `r13-run_module.patch`, `r13-makefile.patch`, `r13-readme.patch`,
  `r13-combined.patch` — unified diffs (`a/`/`b/` labels) generated with
  `diff -u` from the `orig/` copies in this directory.
- `orig/` — pristine copies of the three files at HEAD 59f0a247.
- `go.sum.generated` — go.sum produced by the gc leg's `-mod=mod` run in
  the /tmp probe copy (evidence only; the resume lane regenerates it in
  the real probe dir and commits the verified result).
- `apply-test/` — skeleton tree where the combined patch was applied and
  verified (see §4). Contains stub backend Makefiles; not part of the fix.

## 4. Verification performed (what passed, what is still owed)

PASS:
- `bash -n new/test/e2e/run_module.sh` → SYNTAX-OK.
- `git apply --check r13-combined.patch` on the `orig/` copies → OK;
  applied, and the results are byte-identical to `new/` (`diff -u`
  clean on all three files).
- `make -n test-e2e` (skeleton tree with stub backend Makefiles) →
  exit 0; full recipe resolves: `generate` + `build`, then
  `./test/e2e/run_module.sh <abs>/test/e2e/uuid`. `make -n test` also
  reaches the `test-e2e` step. (First attempt failed only because the
  skeleton lacked `internal/backend/*` dirs — environmental, fixed by
  adding stub dirs.)
- New script fail-fast, live (GR=real repo, /tmp probe dir):
  `run_module.sh probe-uuid case01` → **exit 2**,
  `run_module.sh: gofrontend not built; run 'make generate' at the repo
  root first (missing .../internal/frontend/gofrontend; or set
  GRAALGO_GOFRONTEND)`, and no `.tmp` dir was created (preconditions fire
  before any write). This is the correct behaviour: the main tree has no
  gofrontend binary, and the script now says so loudly instead of failing
  all cases on a garbage classpath.
- Load: `load-shed.sh backoff` → CLEAR (load 6.51) before the heavy steps.

BLOCKED (environmental, not the fix):
- Full wiring test (gc leg + real P3SyncMain leg on case01 with a stub
  gofrontend): the gc leg needs a fresh Go build-cache entry. With
  GOCACHE on /tmp it died `no space left on device` (tmpfs 89% full,
  sibling lanes' staging); with GOCACHE on home it died `disk quota
  exceeded` (the needed cache entry is absent from the 57M home cache).
  The graalgo/JVM leg therefore never ran. The fail-fast test above is
  the deepest live verification possible under the current quota/tmpfs
  state. The resume lane MUST run the full RED→GREEN verification
  (see RESUME.md) — including a real gofrontend build — before the fix
  is reviewable.
- `go.sum`: generated in the /tmp copy (see above) but NOT yet generated
  or committed in the real probe dir (needs a working gc leg).

NOT done (per brief, correctly): no worktree, no registry entry, no
commit, no integration. No code in the main repo tree was modified
(all reads + the two failed probe writes, which left nothing behind).
