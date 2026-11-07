# R20-F03 design note — disk import honors go.mod pins

Lane: rev1107-20-f03. Review: REVIEW-2026-11-07-20 § R20-F03 (HEAD 59f0a247).
Staged patch: `R20-F03.patch` (applies cleanly to HEAD; see RED-GREEN.md).
Status: QUOTA-STAGED — no worktree claimed, nothing landed (see RESUME.md).

## 1. Mechanism verified from disk truth (2026-11-07)

- `internal/frontend/diskimport.go:169-176` (HEAD): the module-cache loop
  picks `best` by `compareSemver(ver, best) > 0` — highest cached version,
  unconditionally. Confirmed by reading the file.
- No `go.mod`/`require` reading exists in production code in
  `internal/frontend/` or `cmd/graalgo/`:
  - `internal/frontend/p3frontend/loader.go:370-396` (`findModule`) reads
    go.mod only for the `module` line.
  - `cmd/graalgo/test.go:290` (`parseModulePath`) and
    `cmd/graalgo/testsynth.go:1578` (`moduleForDir`) read only the module path.
  - No `golang.org/x/mod` import anywhere in the repo; the frontend module
    (`internal/frontend/go.mod`) has zero dependencies.
  - All other `go.mod` hits are vendored `go/*` doc comments or tests.
- The v1 frontend has no notion of the importing module at all:
  `getDiskPackage` / `resolveDiskPackageDir` take only the import path, and
  their callers (`export.go:620,738,829,4422,11997`, `mr_disk.go`,
  `mrdesugar_m5.go`) pass no module context.
- The anchor exists, though: `export.go` `importingDir(paths,
  testPackageDir)` already derives the importing package's directory from
  the input files (with the `--test-package-dir` override), and
  `ExportFiles` is the single entry point (`main.go` → `ExportFiles`, one
  call per process; `ExportFile` delegates to it).
- Error propagation is already loud-capable: `overlayImporter.Import`
  (`export.go:604-626`) returns `gofrontend: import %q: %v` for any error
  that is NOT `errDiskNotFound`, and only `errDiskNotFound` degrades to the
  generic WS G scope error. A pin error that does not wrap
  `errDiskNotFound` therefore reaches the user verbatim.
- No `t.Parallel` in any `internal/frontend/*_test.go` (verified); no
  `GOMODCACHE` use in frontend tests. Tests are sequential, so
  process-global anchor state is safe (see §2).

## 2. Design (what the patch does)

`internal/frontend/diskimport.go` + one line in `export.go`; new test file
`internal/frontend/diskimport_version_test.go`. No new dependencies, no
package-name keying, no synthetic packages: the mechanism is uniform for
every module — pins come from the user's own go.mod, which is data, not a
special case.

1. **Anchor** — `ExportFiles` calls `setDiskAnchor(idir)` with the already
   computed `importingDir`. The anchor is process-global (gofrontend is
   single-shot) and constant for the whole export, including recursive
   disk imports: the MAIN module's pins govern the build, exactly as the
   main module's require set seeds gc's build list.
2. **Pins** — `pinsForAnchor()` walks up from the anchor to the nearest
   go.mod (same walk as p3frontend `findModule`) and parses it once per
   anchor with a hand-rolled `parseGoModPins` (require + replace,
   single-line and block forms, `//` comments, `// indirect`). No x/mod:
   the module has no dependencies and fetching one is quota-blocked
   anyway. A `replace` with a version overrides `require`; a `replace`
   with a local path overrides both. Local paths resolve against the
   go.mod's directory.
3. **Pin phase** — inside the module-cache leg of `resolveDiskPackageDir`,
   BEFORE the legacy loop: for each progressively shorter prefix of the
   import path (longest first), a pin for that prefix is authoritative.
   Pin lookup uses the UNESCAPED path (go.mod names are unescaped);
   directory construction uses the escaped elements, as the legacy loop
   does. Longest pinned prefix wins.
   - Version pin: exact `<base>@<version>` dir or a LOUD error naming the
     module, the pinned version, the cache, and the versions actually
     present, with a `go mod download <module>@<version>` hint. No
     fall-through to the newest version, and no fall-through to
     GOPATH/GOROOT — in module mode gc would fail the build.
   - Local-path replace: the replacement dir (+ subpath) or a LOUD error
     naming it.
4. **Legacy fallback preserved** — with no pin for any prefix, the
   existing highest-version loop runs byte-identical. This keeps GOPATH
   mode, unpinned modules, and the probe harness (single-version cache)
   working unchanged.
5. **Cache invalidation** — `setDiskAnchor` clears `diskCache`,
   `diskDirCache`, `diskBuilding` when the anchor changes, so sequential
   in-process `ExportFiles` calls (tests) can never observe a version
   resolved under another main module. `diskDirCache` is keyed by import
   path alone, which is sound only because the anchor is fixed per
   export and the caches are cleared on anchor change; the invariant is
   stated in the code comment and pinned by
   `TestDiskImportAnchorChangeClearsCache`.

## 3. Deliberate scope limits (not bugs, not hidden)

- **Main module only, no full MVS.** Only the main module's require set
  is consulted. A separate module reachable only via a transitive
  require is not in the pin set: if the main module also pins an ancestor
  module, the ancestor pin wins over the transitive module. This is the
  exact scope the review asked for ("resolve the version from the
  importing module's go.mod require directives"); full MVS (reading each
  dependency's go.mod, graph pruning, go.sum) is a follow-up lane, and
  the limitation is stated in the code comment so the reviewer can
  challenge it.
- **Module not required at all.** If a go.mod exists but names no pin
  for the imported module, the legacy highest-version selection runs.
  gc would fail such a build ("no required module provides package").
  Failing loudly there too is a separate behavior change (it would also
  need care around the probe harness); filed as follow-up, not smuggled
  into this lane.
- **Parser limits.** `exclude`, `retract`, `go`, `toolchain`, `module`
  lines are ignored (irrelevant to selection). A `replace` with a bare
  single token that is not a path (`=> example.com/x` with no version)
  is invalid go.mod and the directive is ignored, letting `require`
  govern. Pseudo-versions compare only by exact string match against
  the cache dir name, which is correct because the cache dir name IS
  the version string.

## 4. Incidental finding (not this lane's scope)

While reading the loop: the LEGACY loop builds `subPath` from ESCAPED
elements (`elems[i:]`), so a subpackage element with an uppercase letter
(e.g. `example.com/mod/Sub`) is looked up as `!sub` under the module
dir, while the module cache stores subdirectories literally. The pin
phase mirrors the legacy construction deliberately (same resolution
except version choice), so this latent bug is preserved, not fixed,
here. Worth a separate finding: verify against a real cache whether
uppercase subpackage dirs resolve at all.

## 5. Tests in the patch

`internal/frontend/diskimport_version_test.go` (12 tests): pin honored,
missing pin fails loud (unit + e2e), subpackage pin, replace version,
replace local path (+ missing path fails loud), escaped-module pin,
anchor-change cache clearing, `parseGoModPins` table, no-pin legacy
fallback, and two full-`ExportFiles` e2e tests (pinned-valid vs
broken-v1.6.0; missing-pin error text). The e2e tests compile against
pre-fix code and fail there — genuine behavioral RED; see RED-GREEN.md
for what was executed vs derived.
