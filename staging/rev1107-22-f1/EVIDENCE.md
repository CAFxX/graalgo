# EVIDENCE — rev1107-22-f1: F1 (P0) + F2 (P1), linkdiff synthetic-scan machinery + per-package retirement inventory

- Lane: rev1107-22-f1 (REVIEW-2026-11-07-22, lane map row F1+F2).
- HEAD verified against: `59f0a247` (main repo `~/workspace/graalgo`, read-only; `git rev-parse HEAD` = `59f0a247088f0e0b6db7e0623076866727dd873c`). Main tree left clean (`git status --porcelain` empty after all work).
- Verdict: **FIX VERIFIED RED→GREEN, staged, BLOCKED-quota** (no worktree, no commit, no push). Resume per `RESUME.md`.

## 1. Mechanism re-verified from disk truth at HEAD 59f0a247 (not trusted from the review)

- `tools/stdlib-linkdiff/` at HEAD still contains the synthetic machinery exactly as filed: `synthetic` mentions per file — `main.go` 9, `main_test.go` 6, `provided.go` 32, `report.go` 28, `scan.go` 12, `synth.go` 52, `synth_test.go` 29. Live files: `synth.go` (465 lines: `loadSyntheticRegistry`, `scanSyntheticLinknames`, `markShadowed`, `targetPackage`) + `synth_test.go` (310 lines).
- `main.go:252-270` loads the synthetic registry and merges synthetic linknames into the scan; `provided.go` extracts synthetic packages as provided sources 2–4 (p3frontend `synth_time_context.go` builders/sources + every `syntheticSources` entry).
- Counts at HEAD: 140 top-level `synth_*.go` files (REVIEW-22 correction — the morning review's 129/141 figures are superseded); `syntheticSources` map in `internal/frontend/synth_machinery.go` has **130 keys** (extracted mechanically here, see §4).
- No other repo code depends on the deleted symbols: `grep -rn loadSyntheticRegistry|scanSyntheticLinknames|markShadowed|SyntheticSourcesScanned|SyntheticSkipped|ShadowedBy` over `tools/ docs/ Makefile .github/` returns hits only inside `tools/stdlib-linkdiff/` itself; zero hits in the fixed tree.

## 2. The fix (scope per REVIEW-2026-11-07-07: linkdiff machinery first; full synthetic removal is the Phase 3 program)

`fix.patch` (2,890 lines, sha256 `30259514da024355ee2dcc89fbb06d02ef6190213dcd2187010c5c116abaca65`) — 11 paths:

| Change | Files |
|---|---|
| Modified | `tools/stdlib-linkdiff/{README.md, main.go, main_test.go, provided.go, report.go, scan.go}` |
| Added | `tools/stdlib-linkdiff/nosynth_test.go` (155 lines, 3 new tests) |
| Deleted | `tools/stdlib-linkdiff/{synth.go, synth_test.go}` (775 lines) |
| Added | `docs/arch/synthetic-retirement-inventory.md` (130-row per-package inventory) |
| Modified | `docs/arch/bridge-inventory.md` (2 hunks: partial-retirement note on the Phase 3 entry + on B-4; closes the R-26 P2 gap for the linkdiff surface) |

What changed, mechanically:
- `main.go`: deleted the M10 synthetic-registry load/merge/shadow block (`:252-270`); header rewritten — provided = P3-M4 intrinsic registry only; tool version is now v2.0.0 with an explicit v1.x-incomparability note.
- `provided.go` (28,726 → 2,127 bytes): deleted all synthetic extraction (`extractP3FrontendFile`, `extractEmbeddedSynthetics`, the whole `syntheticSources` walk, `go/ast`/`go/parser` imports); the only source is `LinknameIntrinsics.java` `Linknames.register(...)`. The `Skipped` field survives as legacy-only so old snapshots still load; fresh extraction never populates it.
- `report.go` (18,219 → 15,336 bytes): deleted `ShadowedBy`, `LiveRefs`/`LiveExampleFiles`, `SyntheticSourcesScanned`/`SyntheticLinknames`, `LiveSkipListSize`, `SyntheticParseErrors`, the live/total split and the "Synthetic sources that failed to parse" section; `toolVersion` 1.1.0 → 2.0.0; self-links are SATISFIED by construction (no shadow fall-through); `targetPackage` + `isExported` moved here verbatim from deleted `synth.go` (the Markdown grouping still needs them; they never depended on any synthetic mechanism).
- `scan.go`: deleted `ShadowedBy` from `Linkname` and `SyntheticSourcesScanned`/`SyntheticSkipped` from `ScanResult`.
- `main_test.go`: `TestProvidedExtraction` rewritten — the provided set must now equal the registry's distinct symbols exactly (the registry file itself is the mechanical oracle); all synthetic spot-checks (`graalgo.os.open`, `os.Open`, `time.Now`, `context.Background`, …) removed.
- `nosynth_test.go` (new): `TestNoSyntheticCredit` (35 synthetic-only witness symbols — `os.Open`, `time.Now`, `syscall.Getenv`, `maps.Clone`, `slices.Sort`, `crypto/sha256.New`, … — must not be provided), `TestReportHasNoSyntheticFields` (no `synthetic_`/`live_refs`/`live_skip_list_size`/`synthetic/` fields, version 2.0.0), `TestSyntheticTargetIsMissing` (`syscall.Exit` MISSING with the intrinsic-surface reason; `runtime.fastrand` still SATISFIED — positive control), `TestTargetPackage` (moved-helper cases unchanged).
- `docs/arch/synthetic-retirement-inventory.md`: one row per synthetic package (130 live + 2 dedicated stubs `runtime`/`unsafe` + 7 retired precedents as model rows). See §4.
- No bridges: no new synthetic package, no import overlay, no package-name-keyed special case, no shim. The only name-keyed thing removed was the `synthetic/<importPath>` handling itself.

Cosmetic note: `git apply` reports one whitespace warning — `fix.patch:252: new blank line at EOF` (trailing blank line at the end of the generated inventory doc). Harmless; the patch applies cleanly.

## 3. RED→GREEN — re-run by THIS lane in this session (not carried from the original lane)

Toolchain: `~/tools/go/bin/go`, `GOCACHE=/tmp/rev1107-22-f1/gocache`, `GOPATH=/tmp/rev1107-22-f1/gopath`, all under `nice -n 10`. Load was CLEAR (`load-shed.sh backoff` rc 0, load 1.51) before the heavy steps.

- RED (`evidence/red-rerun.log`, this session): new tests against pristine HEAD code — `go test -run 'TestProvidedExtraction|TestNoSyntheticCredit|TestReportHasNoSyntheticFields'` → **rc=1, exactly the 3 new tests FAIL** (35 "provided set credits synthetic-only symbol" lines). The original lane's full-suite RED log (`evidence/red-test.log`, 43.0s) shows the same 3 failures with 16 passes.
- GREEN (`evidence/green-rerun.log`, this session): `go test -count=1 -v ./...` on the fixed tree → **19/19 PASS, `ok graalgo/tools/stdlib-linkdiff 0.676s`**; `go vet ./...` clean; `gofmt -l` clean. Matches the original lane's `evidence/green-test.log` (19 PASS).
- End-to-end binary reproduction (this session): the staged `old-linkdiff` and `new-linkdiff` binaries were re-run with the same args as the evidence (`--goroot /home/hatch/tools/go/src --graalgo /home/hatch/workspace/graalgo`):
  - `tmp/old-rerun.json` vs `evidence/old-report.json`: **all keys identical except `generated`** (v1.1.0: linknames 754, satisfied 267, missing 486, routed 1, provided 5,699 symbols, skip-list 421 / live 155, `synthetic_sources_scanned` 130, `synthetic_linknames` 139).
  - `tmp/new-rerun.json` vs `evidence/new-report.json`: **all keys identical except `generated`** (v2.0.0: linknames 615, satisfied 264, missing 351, routed 0, provided 155 symbols, skip-list 337, zero synthetic/live fields).
  - The v1→v2 delta reconciles exactly: 754−615=139 synthetic linknames removed = 135 missing + 3 satisfied + 1 routed. Provided symbols 5,699 → 155: the old tool credited ~5.5k synthetic exports; the new tool credits only the intrinsic registry.
  - Side observation (corroborates the defect): the old binary prints `WARNING: synthetic scan skipped: synthetic "regexp/syntax" … imports must appear before other declarations` — the v1 synthetic scan itself choked on a broken synthetic source.

## 4. Retirement inventory — verified, not trusted

- `inventory-rows.txt`: 130 `ROW` lines; row keys == `syntheticSources` map keys **exactly as sets** (verified mechanically against `internal/frontend/synth_machinery.go` in this session; 130 keys, 0 in either direction missing).
- `dedicatedSyntheticStubs` in `internal/frontend/export.go`: 2 keys (`runtime`, `unsafe`) — matches the doc's "130 + 2 = 132 shadowed import paths".
- "All 130 live packages are needs design": verified — a header scan (`head -40` of every non-test `synth_*.go`) finds **zero** files whose header states a retirement condition. The only `retir*` hits are not package-retirement conditions: `synth_xml.go:66` retires internal Reader/Writer helpers, `synth_machinery.go` mentions already-retired packages (reflect, net/http). All 130 rows carry "needs design" (grep −v confirms only the `KEYS 130` header differs).
- Reproducibility: `gen-inventory.py` re-run in this session regenerates `patched/docs/arch/synthetic-retirement-inventory.md` **byte-identically** (`cmp` clean). If staging is lost, the inventory regenerates from `inventory-rows.txt` + `gen-inventory.py` (both in this dir).
- Counts context: the review's "140 top-level `synth_*.go` files" counts files, not packages — several files carry multiple packages' sources, a few carry none (test helpers). The inventory's unit is the package (130), not the file.

## 5. Staged files (all under /tmp/rev1107-22-f1/ — tmpfs, does NOT survive a reboot)

- `fix.patch` (2,890 lines, sha256 above) — the complete fix; verified: `git apply --check` clean against pristine HEAD copies, applied result reproduces the fixed tree byte-for-byte (scratch repos in `patchwork/`).
- `patched/` — the fixed tree, repo-relative layout: `patched/tools/stdlib-linkdiff/` (10 files; `synth.go`/`synth_test.go` absent by design) + `patched/docs/arch/{synthetic-retirement-inventory.md, bridge-inventory.md}`. `patched/` == `verify-new/` for the linkdiff files (`diff -r` clean apart from unchanged `redgreen/` + `testdata/`, which are not part of the patch).
- `verify-new/` / `verify-old/` — fixed / pristine linkdiff trees used for the RED/GREEN runs (symlink `internal/` → main repo; `work/new/` == `verify-new/`, `work/old/` == HEAD, both `diff -rq` clean).
- `evidence/` — `green-test.log` (orig. lane), `green-rerun.log` (this session), `red-test.log` (orig. lane), `red-rerun.log` (this session), `old/new-report.{json,md}`, `old/new-provided.json`, `tmp/new-rerun.json`, `tmp/old-rerun.json`.
- `old-linkdiff` / `new-linkdiff` — v1.1.0 / v2.0.0 binaries (reused, not rebuilt).
- `gen-inventory.py`, `inventory-rows.txt` — inventory generator + rows.
- `gocache/` / `gopath/` — reused Go build cache (do not delete before the resume run; saves a cold rebuild).
- `patchwork/` — scratch git repos used to generate + verify `fix.patch` (pristine commit, fixed worktree, apply-test clone). **Deleted at end of run to free /tmp space** (verification result recorded here and in §5's patch check; the patch file itself is self-contained and its sha256 is above).

## 6. What is NOT done / owed on resume

- No worktree claimed, no registry entry, no commit, no push, no integration — quota gate failed (see §7). Independent review + integration still owed after landing.
- `make test` / CI wiring for the linkdiff suite is out of scope (R-10 owns tool-suite wiring); the suite itself is verified here in isolation.
- The 130 synthetic packages still exist in `internal/frontend/` — their removal is the Phase 3 program; this lane only stops the tool crediting them and inventories them.
- The v2 skip-list (337) is the new baseline; the SPEC §3.6 growth check restarts at the first v2 report (per README). The next v2 report is the first comparable one.
- Pre-existing, untouched: the `runtime.writeBarrier` stale-route warning (1 route, both binaries); tracked `redgreen/` output files (R-18 P2).

## 7. Quota gate (the blocker)

- Real 8 KB home write probe failed `OSError: [Errno 122] Disk quota exceeded` at ~22:14 CET this session (`/home/hatch/.quota-probe-f1`, removed after the failed attempt — nothing was written). Same result as the original lane (~21:xx) and as REVIEW-22 (~21:56). `/tmp` 82% full (97M free); `/var/tmp` was 100% full per REVIEW-22 (not re-checked — not needed for this lane).
- Consequence: staging only, in `/tmp/rev1107-22-f1/`. Copy this dir to persistent disk on quota recovery — it does not survive a reboot.
