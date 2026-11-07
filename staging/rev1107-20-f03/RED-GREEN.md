# R20-F03 RED/GREEN evidence log

Lane: rev1107-20-f03. Base: graalgo HEAD `59f0a247`. All commands under
`nice -n 10`. Load-shed backoff at start: CLEAR (load 0.43).

## 1. Quota state (why this is a staging, not a landing)

- Real 8 KB home write probe: FAILED — `Disk quota exceeded`
  (`~/workspace/.quota-probe-rev1107-20-f03.bin` not created, rc=1).
  `df` shows 36G avail on /home — the limit is quota, not disk space.
- `df /tmp`: tmpfs 512M, 85–142M free during the run (fluctuating as
  sibling lanes stage); `/var/tmp` 100% full (2.0G, 0 avail).
- Work-registry register: FAILED as expected — `OSError: [Errno 122]
  Disk quota exceeded: '.../work-registry/rev1107-20-f03.json.tmp'`.
  Attempted once with correct CLI args; not retried. No worktree claimed
  (per brief: do not claim until a real home write probe succeeds).
- No `wt.py integrate` / `abandon` run: no worktree exists. Nothing to
  clean up on home; all staging is under `/tmp/rev1107-20-f03/`.

## 2. Mechanism verification (executed, disk truth)

- Read `internal/frontend/diskimport.go` (411 lines): best-version loop
  at :169-176 (`compareSemver(ver, best) > 0`) confirmed.
- `grep -rn "go.mod" internal/frontend/ cmd/graalgo/`: only
  `p3frontend/loader.go` `findModule` (module line only),
  `cmd/graalgo/test.go` `parseModulePath`, `cmd/graalgo/testsynth.go`
  `moduleForDir` (module path only); no `require` parsing in production
  code. `grep -rn modfile`: no hits. `internal/frontend/go.mod`: zero
  dependencies.
- `grep -rn "t.Parallel()" internal/frontend/*_test.go`: no hits
  (anchor design is safe). `grep GOMODCACHE .../*_test.go`: no hits.
- Read `internal/frontend/main.go` (single-shot `ExportFiles` process),
  `export.go` `importingDir`/`ExportFiles` (:861-897) and
  `overlayImporter.Import` (:604-626): pin errors propagate verbatim
  (non-`errDiskNotFound`).

## 3. Patch checks (executed)

- `R20-F03.patch` (693 lines): `internal/frontend/diskimport.go`
  (+194/-5), `internal/frontend/export.go` (+5), new
  `internal/frontend/diskimport_version_test.go` (360 lines).
- `git apply --check --verbose R20-F03.patch` against HEAD 59f0a247:
  **CLEAN** (all three files).
- `gofmt -l` on all three patched files: no output (formatted).
  `gofmt -e` on all three: parse OK.
- NOTE on first patch attempt: the new-file hunk initially failed
  `git apply --check` ("No such file or directory") because it was
  generated with `diff -u /dev/null` — and **`/dev/null` in this VM is
  currently a regular 152-byte file** (root-owned, `-rw-rw----`),
  containing other agents' quota-probe tracebacks (`.probe-run3`,
  `.quota-probe-drv2`). It is NOT a device node. The hunk was
  regenerated with a real empty file + canonical `diff --git` /
  `new file mode` headers and now applies cleanly. The clobbered
  /dev/null is an environment anomaly to flag, not something this lane
  caused or can fix (root-owned device node). All later commands in
  this lane avoided `/dev/null` redirects.

## 4. Standalone harness RED/GREEN (executed)

`harness/main.go` + `harness/pinlogic.go` (the patch's
`parseGoModPins` / `addPinLine` / `pinnedModuleVersion` /
`compareSemver` / `escapeModulePath` extracted VERBATIM from the
patched file by `harness/extract_pinlogic.py`; re-extracted after the
final edit and re-run). Run with caches redirected off home
(`GOCACHE=/tmp/graalgo-gocache`, `GOTMPDIR=/dev/shm`, `HOME` under
`/tmp`); first attempt failed on /tmp space (Go std compile needs more
than the ~138M free), retried against the warm shared cache — rc=0.
Verbatim output:

```
RED  case1: legacy rule selects v1.6.0 despite pin v1.3.0 (bug reproduced)
GREEN case1: pin rule selects v1.3.0
RED  case2: legacy rule silently substitutes v1.6.0 for pinned v1.3.0 (bug reproduced)
GREEN case2: pin rule fails loudly: gofrontend: module example.com/mod: go.mod requires v1.3.0, but module cache /tmp/r20f03cache1089996221 has only [v1.6.0]; run 'go mod download example.com/mod@v1.3.0'
GREEN case3: no pin -> legacy highest v1.6.0 preserved
GREEN case4: replace version v1.3.0 overrides require v1.6.0
GREEN case5: escaped module example.com/Mod pins v1.3.0
ALL HARNESS EXPECTATIONS HOLD
```

This proves the RED (old rule) and GREEN (new rule) for the version-
selection decision itself, on the exact patch text of the pure
functions. The selection simulator in the harness mirrors the patched
`resolveDiskPackageDir` pin-phase/legacy-phase structure.

## 5. NOT executed (blocked by quota — resume lane's job)

- `go test ./internal/frontend -run 'TestDiskImport|TestParseGoModPins'`
  (the 12 new tests, incl. the two full-`ExportFiles` e2e tests).
- Full `go test ./internal/frontend` (regression: existing `ExportFile`
  tests now set the anchor from `testdata` dirs; the walk finds
  `internal/frontend/go.mod`, which has no require directives, so pins
  are empty and behavior must be unchanged — asserted by the suite,
  not yet run).
- `make check` / `make snapshot-check` in a worktree.
- The e2e RED claim ("the two e2e tests fail on pre-fix code") is
  DERIVED, not executed: pre-fix resolution provably selects v1.6.0
  (harness case 1), the v1.6.0 fixture is invalid Go, and
  `buildDiskPackage` type-check failure propagates through
  `overlayImporter.Import` to `ExportFiles` (code path verified by
  reading). The resume lane executes it as part of the test run
  (a `git stash`-free check: run the new tests, then `git stash` the
  fix hunks only... simpler: the harness already carries executed RED;
  the resume lane just runs the suite GREEN).
