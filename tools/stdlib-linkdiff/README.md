# stdlib-linkdiff

The graalgo linkname-diff tool (P3-M6, GG-0021; SPEC §3.6).

## Tool-rot remediation (2026-10-19)

Decision: **repaired**, not retired. This tool is the mechanical
enforcement of SPEC §3.6's skip-list growth kill trigger, so it must run
on every Go upgrade; the twin relationship with `stdlib-excl` (canonical
tree resolution + pin/manifest cross-check) was worth keeping. The
2026-10-14 `phases/` removal had left it dead on startup (manifest/pin
reads and all three provided-set extractions joined into the removed
tree); every path is now repointed at the live layout and the tool
re-verified against the vendored go1.27.1 tree.

Scans a Go source tree for `//go:linkname` directives and classifies each
target as **SATISFIED** (provided by graalgo), **ROUTED** (explicit entry
in [routes.txt](routes.txt)), or **MISSING** (lands on the skip-list — never silently
dropped). The JSON report carries `skip_list_size` as a prominent,
machine-readable top-level field: SPEC §3.6's kill trigger is skip-list
growth > 15 twice per release.

No external dependencies. `go vet` clean.

## Provided set — exactly where each list comes from

The provided symbol set is extracted mechanically from the graalgo tree
(`--graalgo`, default `$GRAALGO_ROOT` else `~/workspace/graalgo`) every
run — never hand-maintained — so per-release drift shows up as a snapshot
diff rather than silent staleness.

1. **P3-M4 intrinsic registry** — `Linknames.register("symbol", …)` calls in
   [internal/backend/interop/src/main/java/graalgo/p3link/LinknameIntrinsics.java](../../internal/backend/interop/src/main/java/graalgo/p3link/LinknameIntrinsics.java)
   (Go→intrinsic direction). Today: `runtime.fastrand`,
   `runtime.fastrandn`, `runtime.nanotime`,
   `graalgo.runtime.Linknames.callGo`.
2. **Synthetic `runtime`, `fmt`, `sync`, `sync/atomic`** — the builder
   functions in
   [internal/frontend/p3frontend/importer.go](../../internal/frontend/p3frontend/importer.go)
   (`syntheticRuntimePackage`, `syntheticFmtPackage`,
   `syntheticSyncPackage`, `syntheticAtomicPackage`), plus the
   package-level `syncMethodSpecs` map and `syncLockerInterface`
   (same file). Method symbols use gc qualification:
   `sync.(*Mutex).Lock`, `sync/atomic.(*Int64).Load`.
3. **Synthetic `time`, `context`** — `syntheticTimePackage` /
   `syntheticContextPackage` in
   [internal/frontend/p3frontend/synth_time_context.go](../../internal/frontend/p3frontend/synth_time_context.go).
4. **Normative `time`/`context` sources** — the `syntheticTimeSource` /
   `syntheticContextSource` embedded Go sources in
   [internal/frontend/synth_time_context.go](../../internal/frontend/synth_time_context.go)
   (parsed with go/parser;
   only exported declarations are taken). The p3frontend mirrors of (3)
   pin these via signature-equality tests.
5. **Embedded synthetic stdlib packages (M10)** — every package in the
   `syntheticSources` registry
   ([internal/frontend/synth_machinery.go](../../internal/frontend/synth_machinery.go)): the embedded Go source
   is resolved mechanically (const fragments, `+` chains, parenthesized
   concatenations) and parsed with go/parser; exported declarations are
   qualified by import path. A source that fails to parse is recorded
   on the report's `synthetic_parse_errors` (loud, never silent) and
   skipped — one broken synthetic package must not block the diff.

`--snapshot PATH` writes the extracted set as JSON for the per-release
record; `--provided PATH` reads a snapshot instead of extracting.

## Routes

[routes.txt](routes.txt): one `target -> replacement # reason` per line; the reason
is mandatory. A routed target is not on the skip-list. Add a route when
a MISSING target is triaged as deliberately unprovided (with its
justification and source).

## Report

`--json PATH` (default stdout), `--md PATH` (Markdown summary).
Top-level `skip_list_size` plus the full `skip_list` (distinct MISSING
targets, each with ref count, live ref count, example files, reason),
per-linkname `entries` (each carrying `shadowed_by` when its
real-stdlib file is superseded by a synthetic package), `duplicates`
(local names linked more than once), `provided_by_source` counts,
`routes_applied`, `stale_routes` (routes that matched zero linknames --
also warned about loudly on stderr), `synthetic_parse_errors`
(embedded synthetic sources that failed to parse -- loud, never
silent), and the `constraint_excluded_sample`.

Two headline sizes: `skip_list_size` (the whole scan population --
comparable across releases for the SPEC §3.6 growth check) and
`live_skip_list_size` (targets referenced by at least one live entry:
code graalgo actually compiles -- the actionable work-list subset).
The Markdown report renders the live skip-list grouped by target
package.

### Tree resolution

`--goroot` default -- canonical stdlib-tree resolution (shared with
`stdlib-excl`; the two implementations are documented as twins):
1. the vendored tree for the pinned version,
   `~/workspace/graalgo-stdlib/<ver>/` (flat layout -- the target dir
   IS the src root, no `src/` subdir), used only when the committed
   `vendor-manifest.json` records the same `goVersion` as
   [tools/stdlib-fork/pinned-version.txt](../stdlib-fork/pinned-version.txt) (a manifest/pin skew
   fails loud);
2. otherwise the live toolchain's `GOROOT/src`, announced loudly on
   stderr -- results then pin to the live toolchain, not the vendored
   tree.

An explicit `--goroot` always wins. There are no silent fallbacks:
every resolution announces where the tree came from.

### Build-tag semantics

Files are selected for `--goos/--goarch` (default `linux/amd64`) with
`go/build.MatchFile`: `//go:build` / `// +build` lines plus filename
suffixes. Only the tags `linux`, `amd64`, `unix` (and `--tags`) count as
satisfied. `_test.go` files are excluded from the production contract
(counted separately). [testdata/](testdata/) directories are skipped.

## Per-release workflow (Go upgrade)

Run this on every Go upgrade, as part of the upgrade's Phase 2
(GG-0021):

1. **Snapshot the provided set** (records what graalgo provides at this
   release):
   `stdlib-linkdiff --snapshot tools/stdlib-fork/provided-<ver>.json`
2. **Run the diff** against the new tree (pass it explicitly -- the
   default resolution follows the pin, which still names the old
   version until the cutover; note the flat layout: the target dir IS
   the src root, no `src/` subdir):
   `stdlib-linkdiff --goroot <new-tree>/ --json tools/stdlib-fork/linkname-trial-<ver>.json --md tools/stdlib-fork/linkname-trial-<ver>.md`
3. **Read `SKIP_LIST_SIZE`** from the JSON. Compare against the previous
   release's report. SPEC §3.6 kill trigger: growth > 15 twice.
4. **Triage new MISSING targets**: for each new skip-list entry decide —
   implement the symbol (lands in the P3-M4 registry or a synthetic
   package), add a [routes.txt](routes.txt) route with justification, or accept it
   onto the skip-list with a recorded reason.
5. **Diff [routes.txt](routes.txt)** against the previous release; every added or
   removed route needs its justification reviewed.
6. **Diff the provided snapshot** (`provided-<ver>.json`) against the
   previous one: unexpected shrinkage means a source regressed
   (the extractor fails loudly on missing sources instead).

The trial report naming convention is
`tools/stdlib-fork/linkname-trial-<go-version>.{json,md}`.

## Testing

`go test ./...` — 13 tests over the synthetic fixture tree in
[testdata/fixture/](testdata/fixture/) (a fake GOROOT) plus the real graalgo tree for
provided-set extraction. Red-green evidence for the build-tag filter
lives in [redgreen/](redgreen/): `green.log` (all green), `mutation-tagfilter.patch`
(the filter disabled), `red-tagfilter.log`
(`TestBuildTagGatedExcluded` red under the mutation), `green-rerun.log`
(green after restore).
