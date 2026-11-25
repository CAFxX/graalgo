# Independent Review 1 (tooling) — graalgo P3-M6 stdlib fork mechanics

- **Reviewer:** Reviewer 1 (tooling)
- **Scope:** commits cf59713 (stdlib-vendor), bef101b (stdlib-excl + exclusions.yaml v1), 188b90a (stdlib-linkdiff), 62cc214 (asm strategy — tool-adjacent, not re-reviewed here), d1195ca (ADR addendum, no tool code)
- **HEAD reviewed:** d1195ca48dd010f8a193aa76f0df640366998fd8
- **Verdict: PASS-with-findings** — 0 P0, 1 P1, 3 P2. All three tools are sound at their core: suites green, `go vet` clean, red-green claims reproduced, adversarial inputs handled, published artifacts re-derived byte-for-byte.

## Method

Ran every suite myself (`go test -count=1`, `go vet`), reproduced two red-green claims per tool with my own inverse mutations (applied in-tree, watched FAIL, reverted, watched green; tree verified clean after), hand-attacked each tool (symlinks, FIFOs, manifest path traversal, block-comment linkname decoys, malformed directives, build-tag-gated files, plausible-but-wrong reason codes, dual .s+cgo dirs), and re-derived the published measurement and trial reports from scratch.

## Tool 1: tools/stdlib-vendor (cf59713)

8 tests green; vet clean; no external deps; no placeholders.

**Red-green reproduced (my mutations):**
1. Added a `VendoredAt` timestamp field to the manifest schema → `TestVendorManifestDeterministic` FAIL (manifests differ across runs); reverted → green.
2. Removed the size check + SHA-256 comparison from `verifyTree` → `TestVerifyDetectsTamper` FAIL ("verify passed after tampering with pkg/b.go"); reverted → green.

**Attacks (all handled):**
- Symlink inside source tree → refused: `refusing to vendor symlink "pkg/evil_link"`.
- FIFO inside source tree → refused: `refusing to vendor non-regular file`.
- Crafted manifest with `"path": "../escape.txt"` → verify fails (missing), no escape from target dir.

**Real-artifact spot checks:** manifest has 12,018 entries, sorted, no dupes; 3 sampled entries (first/middle/last) re-hashed from `~/workspace/graalgo-stdlib/go1.27.1/` — hash and size match; pin reads `go1.27.1`.

## Tool 2: tools/stdlib-linkdiff (188b90a)

13 tests green; vet clean; no external deps; no placeholders.

**Red-green reproduced (my mutations):**
1. Applied the shipped `redgreen/mutation-tagfilter.patch` (disables the `go/build` platform filter) → `TestBuildTagGatedExcluded` FAIL (darwin-gated linkname counted); reverted → green.
2. Neutered the `Linknames.register` extraction loop in `provided.go` (compiles, adds nothing) → `TestProvidedExtraction` + `TestSatisfied` FAIL (`provided set missing "runtime.fastrand"` …); reverted → green.

**Provided-set extraction is mechanical (never hand-maintained)** — spot-checked 3 symbols end-to-end via `--snapshot`:
- `runtime.nanotime` → `p3link LinknameIntrinsics.java Linknames.register` (verified at `LinknameIntrinsics.java:65`),
- `sync.(*Mutex).Lock` → `synthetic "sync": p3frontend/importer.go syncMethodSpecs method`,
- `time.Now` → both the p3frontend synthetic builder and the phase1 embedded source (merged provenance, by design).
Total: 241 symbols. Cross-checked builder completeness independently: every MISSING trial target inside a synthetic package (`runtime.*`, `sync.*`, `time.*`, …) is an unexported runtime-internal symbol (e.g. `runtime.throw`, `sync.fatal`, `time.now`) — correct MISSING by design. The only exported-looking ones, `sync/atomic.{CompareAndSwapPointer,StorePointer,SwapPointer}`, are genuinely absent from the `syntheticAtomicPackage` builder (unsafe.Pointer atomics not in the P3-M4 surface) — the tool reports the builder faithfully; whether the builder *should* have them is a P3-M4 scope question, not a tool bug. The extractor's fiveOps/name-concatenation logic (`addFunc("Load"+nm, …)` via map-index value sets) is exercised by `TestProvidedExtraction` (`sync/atomic.LoadInt64`, `CompareAndSwapBool` present; `(*Bool).Add` correctly absent).

**Trial report re-verified:** `linkname-trial-go1.27.1.json` — 615 linknames, 8 SATISFIED, 1 ROUTED (`runtime.writeBarrier -> const:false`), 606 MISSING, `skip_list_size`=525 distinct targets, 0 parse errors, 1799 `_test.go` excluded, 1641 constraint-excluded. `sum(linkname_refs) == 606` over the skip-list; targets unique; statuses consistent.

**Attacks (hand-built tree, real tool run):** block-comment `/*go:linkname*/` decoy not extracted (matches gc); `//go:build ignore` file constraint-excluded; `_test.go` file excluded; 3-field directive → MISSING with reason (never dropped); local≠declared → MISSING with the gc rationale; self-link → `pkg.selfonly` MISSING; duplicate local names across files detected. A dir with `.s` + `import "C"` classifies coupled with reasons `["asm","cgo"]`.

## Tool 3: tools/stdlib-excl (bef101b)

21 tests green; vet clean; no external deps; no placeholders.

**Red-green reproduced (my mutations):**
1. Neutered the version-stale check in `validate.go` → `TestValidateRejectsVersionStaleChange` FAIL; reverted → green. Also verified end-to-end: appended an entry to `exclusions.yaml` without a version bump → `validate` REJECTs "list changed without a version bump (still version 1); bump version: to 2" against the git-HEAD baseline; file restored byte-identical.
2. Neutered cgo detection in `measure.go` → `TestMeasureClassifiesCgo` FAIL; reverted → green.

**Measurement re-derived from scratch:** `measure --src ~/workspace/graalgo-stdlib/go1.27.1` reproduces the published `coupled-packages-go1.27.1.json` exactly — 399 dirs, 236 pure / 142 coupled / 21 not-linux-amd64, 274 test binaries, per-dir class+reasons identical on all 399 dirs. The "verified against go list" claim holds: `go/build` selection (GoFiles+CgoFiles+SFiles) matches `go list` exactly on os/sync/net/runtime/reflect/math/crypto-sha256 for linux/amd64.

**Exclusion list v1 evidence:** both entries check out — `runtime/cgo → CGO` (SPEC §3.8 stubs it unsupported; SPEC §3.7/GG-0058 bans `import "C"` in module code; measured `coupled [asm cgo linkname build-tags]`), `plugin → UNSUPPORTED_FEATURE` (SPEC §3.8 stubs it unsupported; §7.7 full scope). Cap math: 274 test binaries → floor(5·274/100) = 13 binds (25 does not); 2 entries < 13. `validate` on the real list: OK. GG-0033 compatibility verified by replicating `LoadExclusions` line semantics over the real file: version 1, exactly the 2 entries with correct reasons; the `reason_codes:` section is invisible to the GG-0033 readers (`reason_codes:`/`- code:`/`description:` match none of their line prefixes).

**Attacks:** crafted yaml with `reason: UNSUPPORTED` (plausible-but-wrong) and `reason: cgo` (lowercase) → both REJECTed as unknown codes. The 15% drift trigger boundary (`DriftReopensBoundary`, strict >15%) is unit-tested on both sides.

## Findings

### P1-1 — `validate`'s default `--src` can never find the vendored tree (dead branch)
- **File:** `tools/stdlib-excl/validate.go`, `defaultSrcRoot` (~line 330).
- **What I did:** ran `go run . validate` (the exact `make -C phases/phase3 check` gate invocation, no `--src`) on the real tree.
- **Expected:** per the usage text and code comment, "default: vendored tree when present" — src should resolve to `~/workspace/graalgo-stdlib/go1.27.1/`.
- **Actual:** src resolved to `/home/hatch/tools/go-1.27.1/src` (the `go env GOROOT` fallback). The vendored branch probes `…/graalgo-stdlib/<ver>/src`, but `vendorTree` copies `$GOROOT/src` *contents* directly into `…/graalgo-stdlib/<ver>/` (flat layout — confirmed: no `src/` subdir exists there), so the probe can never succeed.
- **Impact today:** nil — the manifest records `sourceGoroot: /home/hatch/tools/go-1.27.1`, so the fallback checks byte-identical content. But if the vendored tree is ever regenerated from a different source (the whole point of VENDORING.md's update procedure), the freshness check (#5) and the test-binary count would silently validate the wrong tree. The branch is dead by construction, not by edge case.

### P2-1 — linkdiff never reports unused/stale routes
- **File:** `tools/stdlib-linkdiff/report.go`, `Classify` (`rep.RoutesApplied = routes`).
- **What I did:** ran the tool over a fixture tree containing no `runtime.writeBarrier` linkname.
- **Expected:** the `routes.txt` writeBarrier route reported as matched-zero / flagged stale.
- **Actual:** it is listed under "Routes applied" with no indication it matched nothing. A route whose target disappears from the tree (or was mistyped) sits silently; the per-release workflow relies on manual diffing of `routes.txt`. Suggest emitting per-route match counts or warning on zero-match routes.

### P2-2 — vendor manifest determinism claim omits the same-machine qualifier
- **File:** `tools/stdlib-vendor/vendor.go` (`writeManifest` comment) and `phases/phase3/stdlib-fork/REDGREEN.md` §(a).
- **What I did:** inspected the manifest schema.
- **Expected vs actual:** the manifest embeds absolute machine paths (`sourceGoroot`, `targetDir`), so "two vendor runs over identical sources produce byte-identical manifests" holds only on the same machine. The claim should say so (or normalize those fields).

### P2-3 — measure emits `"reasons": null` for pure dirs instead of `[]`
- **File:** `tools/stdlib-excl/measure.go`, `classifyDir` (nil `reasons` slice marshals as null).
- **What I did:** `measure` over a fixture with a pure dir; JSON shows `"reasons": null`.
- **Expected vs actual:** consumers of the versioned JSON must handle null; `[]` would be the cleaner contract for a versioned machine-readable report.

## Out of scope / not findings

- `sync/atomic.{CompareAndSwapPointer,StorePointer,SwapPointer}` MISSING: faithful extraction of the P3-M4 builder surface; builder-scope question, not a tool bug.
- d1195ca touches only `phases/phase3/adr/ADR-0001-addendum-p3d1-reexamination.md` — no tool code.
- `stdlib-excl` module path (`module stdlib-excl`) is inconsistent with the other tools' `graalgo/tools/…` paths — cosmetic, not a finding.
- Other working-tree modifications observed during review (research survey checkpoints, phase1 hidden_files) are other workers' in-flight work; untouched.

## Red-green reproduction summary

| Tool | Mutation applied | Test | Red | Reverted green |
|---|---|---|---|---|
| stdlib-vendor | `VendoredAt` timestamp in manifest schema | TestVendorManifestDeterministic | ✅ FAIL | ✅ |
| stdlib-vendor | removed size+SHA-256 checks in `verifyTree` | TestVerifyDetectsTamper | ✅ FAIL | ✅ |
| stdlib-linkdiff | disabled `go/build` platform filter (shipped patch) | TestBuildTagGatedExcluded | ✅ FAIL | ✅ |
| stdlib-linkdiff | neutered `Linknames.register` extraction | TestProvidedExtraction, TestSatisfied | ✅ FAIL | ✅ |
| stdlib-excl | neutered version-stale check | TestValidateRejectsVersionStaleChange | ✅ FAIL | ✅ |
| stdlib-excl | neutered cgo detection | TestMeasureClassifiesCgo | ✅ FAIL | ✅ |

All six red-green claims reproduce as documented (the tag-filter patch is even shipped in `redgreen/`). The suites are non-tautological.
