# rev1107-20-f04 — R20-F04 remediation lane report

- **Issue:** R20-F04 (P1, REVIEW-2026-11-07-20): `graalgo run/build/vet/fmt
  <import-path>` fails — import paths are never resolved.
- **Base:** repo `~/workspace/graalgo`, HEAD `59f0a247088f0e0b6db7e0623076866727dd873c`
- **Status:** FIX COMPLETE AND VERIFIED IN SCRATCH — **BLOCKED ON LANDING by home
  quota (EDQUOT)**. No worktree could be claimed, no registry entry written,
  no commit made. A quota-wait runner is armed (see §8); it lands the
  verified patch when quota clears. **No merge/integration done or attempted;
  independent review on the exact SHA is still required before any merge.**

## 1. Mechanism re-derived from disk truth (confirmed, not assumed)

- `cmd/graalgo/main.go:239-280` `resolveFiles`: every argument goes through
  `os.Stat`; on stat failure only a `.go` suffix passes through, anything
  else returns `no such file or directory: <arg>`. No import-path handling
  anywhere. Callers: `run` (`commands.go:92`), `build` (`:200`), `vet`
  (`:259`), `fmt` (`:293`).
- Same defect, second function: `cmd/graalgo/test.go` `resolveTestDirs`
  (`:145-165`, used only by `cmdTest`) is also `os.Stat`-only, so
  `graalgo test <import-path>` fails identically. The review named only
  `resolveFiles` + the four commands; this lane also fixes `resolveTestDirs`
  (same one-mechanism change, same resolver) and says so explicitly — the
  reviewer may drop that hunk if the scope is judged too wide.
- The resolver already exists in the same package: `testsynth.go:1602`
  `importDirForPath(dir, importPath)` maps import path → source directory
  (same-module via `moduleForDir` go.mod walk-up, stdlib via GOROOT,
  third-party via module cache then GOPATH/src). It is the driver's existing
  "`go list` equivalent", already used by the test driver. No new dependency,
  no new lookup logic, no bridge.
- `gc` ground truth, probed this run (stock go1.27.1, scratch module
  `example.com/m`, GOCACHE/GOPATH on /var): `go build mypkg` (bare directory
  name) fails with `package mypkg is not in std` — bare names are import
  paths, never filesystem directories; `go run example.com/m/mypkg` prints
  `hi`, exit 0; `go vet example.com/m/mypkg` exit 0.
- Usage strings advertise `[packages]` (`main.go:108,122` build/test/vet,
  `run` advertises `[package]`); only filesystem paths and directories
  worked. Zero tests covered `resolveFiles` or any import-path argument
  before this fix (verified by grep).

## 2. Design decisions

- **Reuse, don't reimplement:** the fix wires the existing
  `importDirForPath` into `resolveFiles`/`resolveTestDirs` via one new
  helper, `packageDirForArg` (`main.go`, appended after `resolveFiles`).
  `cmd/graalgo` cannot import `internal/frontend` (separate module), so the
  frontend's `resolveDiskPackageDir` was not an option; the package-main
  resolver is the correct reuse.
- **Import-first precedence, like `go`:** a bare name is looked up as an
  import path first; only if that fails does the caller's existing
  filesystem handling run (so bare-directory invocations that work today
  keep working). Consequences, pinned by test:
  `graalgo build fmt` means stdlib `fmt` even when a `./fmt` directory
  exists (use `./fmt` for the directory) — a deliberate, documented change
  from the old always-local behavior, in the direction of `gc`. Explicit
  filesystem arguments (`./`, `../`, `.`, absolute paths, `.go` files)
  skip the lookup entirely (`isFilesystemArg`).
- **`go list` subprocess: considered, rejected.** Shelling out to the stock
  `go` binary would give go.mod-correct versions for free, but it makes the
  driver depend on a `go` binary being installed (graalgo must work where
  only graalgo is installed), adds a subprocess + env-plumbing failure mode
  to the argument path, and duplicates nothing — the lookup already exists
  in-package. The in-package resolver is the right call.
- **Version skew, NOT fixed here (R20-F03 owns it):** `findInModCache`
  takes the first matching cached version; the frontend's disk importer
  takes the highest. So driver-resolved files and frontend-resolved imports
  of the same module-cache path could come from different versions when
  several are cached. Changing version selection in the driver alone would
  just move the skew; the durable fix is F03's unified version policy
  (go.mod pins). Documented in the code comment and in the module-cache
  test; the common cases (single cached version, harness-pinned cache)
  have no skew.
- **FE-09 boundary (no duplication):** FE-09 (DRV-2, REVIEW-18) owns
  build-constraint filtering in directory expansion (`buildableDirFiles`).
  This lane touches only the loop head of `resolveFiles` (import lookup
  before `os.Stat`) and appends helpers at end of file; it does not change
  any expansion behavior. FE-09's patch is staged
  (`/tmp/fe09/FE09-all.patch`, runner armed); this patch was verified to
  apply cleanly and pass the full suite in BOTH orders (FE-09-then-F04 and
  F04-then-FE-09 — see §5). The two lanes share no files except `main.go`
  (disjoint hunks) and no test files (`resolve_test.go` vs
  `resolve_import_test.go`); both reuse the `writeFile` helper from
  `testsynth_test.go:14`.
- **No bridges:** stdlib `go/build` machinery already in the package +
  the driver's own resolver only. No synthetic packages, no
  package-name-keyed logic, no import overlays, no special cases. No files
  outside `cmd/graalgo/` touched.

## 3. The fix

- `cmd/graalgo/main.go`: import `strings`; `resolveFiles` loop head
  rewrites a bare argument to its resolved package directory
  (`if dir, ok := packageDirForArg(a); ok { a = dir }`) before the existing
  `os.Stat`/directory-expansion logic — so an import-resolved directory
  flows through the exact same expansion code as a literal directory
  (and automatically inherits FE-09's constraint filtering when FE-09
  lands). New helpers appended at end of file: `isFilesystemArg`,
  `packageDirForArg` (with the precedence + version notes in comments).
- `cmd/graalgo/test.go`: `resolveTestDirs` gains the same import lookup
  (`if dir, ok := packageDirForArg(p); ok { dirs = append(dirs, dir);
  continue }`) before its `os.Stat` fallback.
- `cmd/graalgo/resolve_import_test.go` (new, 428 lines): 14 tests (§4).

## 4. Tests (new file `cmd/graalgo/resolve_import_test.go`, 14 tests)

Unit — `resolveFiles` (9): same-module import path (module root cwd +
subdir cwd); module path itself (module root); stdlib `fmt` (+ `net/http`,
asserting files under GOROOT/src/<path> and a known file present);
module-cache package + subpackage (single pinned version); GOPATH/src
fallback (empty cache); unknown import path still errors naming the
argument (guard — passed pre-fix too); import-first precedence (local
`./fmt` dir does not shadow stdlib `fmt`; `./fmt` still reaches it);
filesystem forms still work (explicit `.go` file, directory arg, `.`,
nonexistent `.go` pass-through — guards, passed pre-fix too).
Unit — `resolveTestDirs` (1): import path → dir; `.go` file → parent dir;
unknown import path errors.
Command level — fake-backend harness (`writeFakeBackend`: a fake graalgo
root whose `graalgo-sync-run` exits 0 for `--check`/`--emit-wire`
(writing minimal wire JSON) and prints `FAKE-RUN` otherwise, logging
every invocation; precedent: `internal/driver/artifact_test.go`
`TestEmitBinaryRuns`): `TestCmdRunImportPath` (import path + program args
after `--`, verified in the backend log; directory + `.go`-file forms);
`TestCmdBuildImportPath` (import path with `-o`, asserts a real executable
artifact is emitted — compiles the driver launcher, ~0.3s);
`TestCmdVetImportPath` (import path + directory form);
`TestCmdFmtImportPath` (import path + directory + `.go`-file forms,
asserting the file is actually reformatted);
`TestCmdTestImportPath` (import path, real `_test.go` with a test function,
driver synthesized and "run" through the fake backend).

## 5. RED → GREEN evidence (trimmed module copy in scratch)

Trimmed copy: `go.mod` + `cmd/graalgo` + `internal/driver` +
`internal/vet` (same shape as FE-09's). GOCACHE/GOPATH/TMPDIR on overlay
`/var/rev1107-20-f04/work/` (`/tmp` tmpfs too small for a second copy;
home quota exhausted).

- RED (unfixed code + new tests, `work/baseline`): 12 FAIL, 2 PASS.
  Failing: all 9 import-path unit tests except the not-found guard, plus
  `TestResolveTestDirsImportPath` and all 5 command tests. Failure mode is
  exactly the reported defect, e.g.
  `resolveFiles(import path): no such file or directory:
  example.com/m/cmd/hello` and `graalgo run: no such file or directory:
  example.com/m/cmd/hello`. Passing (guards): `TestResolveFilesImportPathNotFound`,
  `TestResolveFilesFilesystemFormsStillWork`. Log: `work/red.log`.
- GREEN (fix applied, `work/repo`): all 14 new tests pass; full
  `go test ./cmd/graalgo/ -count=1` → `ok` (2.2s); `go vet` clean;
  `gofmt -l` clean; `go build ./...` OK. Logs: `work/green-new.log`,
  `work/green-full.log`, `work/vet.log`, `work/build.log`.
- FE-09 order-independence (both sim trees built by `git archive` at
  `59f0a247`): FE-09-then-F04 and F04-then-FE-09 both `git apply --check`
  clean and apply clean; in both trees `test.go` and
  `resolve_import_test.go` are byte-identical to the fix tree and `main.go`
  differs only by FE-09's hunks; full suite green in both (41 tests pass,
  incl. all 14 F04 + all 5 FE-09 tests), vet + gofmt clean. Logs:
  `work/sim-fe09-first-verbose.log`, `work/sim-f04-first-verbose.log`.
- `gc` probe evidence: `work/gcprobe/` (scratch module + results in §1).

## 6. Quota block — evidence

- Real 8 KB home write probe (`~/workspace/.quota-probe-rev1107-20-f04`)
  → `OSError: [Errno 122] Disk quota exceeded` (this run; home df shows
  36G avail on the device — the quota, not the disk, is exhausted).
- `workreg.py register --id rev1107-20-f04 ...` → `OSError: [Errno 122]
  ... work-registry/rev1107-20-f04.json.tmp`. No registry entry exists for
  this lane; the runner registers when quota clears. (First register
  attempt used a wrong flag, `--stream`; the corrected attempt then failed
  on quota as above.)
- `/var/tmp` 100% full (2.0G, 1.6M free); `/tmp` tmpfs 512M, 142M free at
  run start. All staging is on `/var` (overlay, 4.6G free), with a mirror
  in `/tmp/rev1107-20-f04/`.
- Main tree untouched (`git status` clean at `59f0a247`). No worktree
  claimed, no commit made, no push attempted.

## 7. Staged artifacts (all outside home quota)

- `/var/rev1107-20-f04/work/F04-all.patch` (523 lines, git-applyable at
  repo root; verified `--check` clean at `59f0a247` and on top of FE-09)
  — split into `F04-main.go.patch` / `F04-test.go.patch` /
  `F04-resolve_import_test.go.patch`. Mirror copies in `/tmp/rev1107-20-f04/`.
- `/var/rev1107-20-f04/work/repo/` — trimmed module copy with fix + tests
  applied (the GREEN tree).
- `/var/rev1107-20-f04/work/baseline/` — trimmed copy, unfixed + tests
  (the RED tree).
- `/var/rev1107-20-f04/work/sim/fe09-first/` and `.../sim/f04-first/` —
  `git archive` trees with both patches applied in each order.
- `/var/rev1107-20-f04/work/difftree/` — patch-generation inputs (unused;
  kept for parity with FE-09 layout).
- `/var/rev1107-20-f04/work/gcprobe/` — `gc` precedence probe module.
- `/var/rev1107-20-f04/work/*.log` — red/green/vet/build/sim logs.
- `/tmp/rev1107-20-f04/` — mirror of the patch set + this report.
- Full patch text is also embedded in the lane's final report to the
  coordinator (so the work survives even if scratch is lost).

## 8. Quota-wait runner (armed)

`/var/rev1107-20-f04/work/runner.sh` (log: `.../runner.log`).
Waits for quota (real 20 KB probe, up to 4h), then: registers the lane,
claims worktree `rev1107-20-f04` (`--recover` fallback), verifies HEAD is
still `59f0a247`, applies `F04-all.patch`, re-runs gofmt/vet/full suite
in the worktree, commits (explicit pathspecs), attempts push, marks the
registry entry done. It never merges/integrates. If HEAD moved, it stops
and leaves the patch staged (a moved HEAD means the patch needs
re-verification against the new base; the F04 patch was verified to apply
on top of FE-09, but the runner does not guess about other moves).

## 9. Not done (out of mandate / follow-ups)

- `...` pattern arguments (`./...`, `example.com/m/...`) are still
  unsupported (`./...` errors as before; other patterns report
  "no such file or directory") — pre-existing limitation, unchanged.
- Module-cache version selection (first vs highest vs go.mod pin) is
  R20-F03's territory (§2); this lane deliberately does not change it.
- Independent review of the exact SHA is still owed before any merge.
  The reviewer should check the import-first precedence decision (§2,
  `TestResolveFilesImportPathDoesNotShadowStdlib`), the `resolveTestDirs`
  scope extension (§1), and the fake-backend command tests (§4).
- FE-16 (P2, adjacent, noted by the FE-09 lane): `resolveTestDirs`
  replaces a single-file arg with its directory, so
  `graalgo test foo_test.go` runs the whole package. Not touched — this
  lane's mandate is import-path resolution.
