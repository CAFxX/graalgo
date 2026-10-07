# P3-M7 integration inventory (coordinator, 2026-10-07 ~03:45 UTC)

## Master baseline (all on master)
- 3e06ff3 (base) → 60415fe, 26bf23d (infra: wave1 harness + checklist)
- 0bdce3d — gofrontend Makefile rebuild fix (from m7-errors-sort@056e40a)
- 5ad29b7 — nil-interface NPE fix (GoInterface.narrow, 5 files)
- 0dc9616 — red-green note for NPE fix

## Worker branches (content-addressed, NOT name-addressed)
- `m7-errors-sort` @ a4fa2bf — CLEAN, 9 commits: errors+sort complete
  (synth sources, exporter wiring, mirrors, importer, loader+drift, corpus,
  redgreen, DESIGN.md). Corpus: errors 14/14, sort 20/20.
- `m7-enc-json` @ 2ebe8c4 — enc-core's 8 synthetic sources
  (synth_base32/base64/hex/binary/ascii85/pem/asn1/encoding.go + mrdesugar_m7.go).
  ALSO contains 4d08294 (NPE fix duplicate of master's 5ad29b7).
  Currently checked out; has enc-json worker's UNCOMMITTED changes (export.go etc).
- `m7-enc-core` @ 8992dc2 — csv/gob worker: 1836c8e (wire csv/gob sources),
  8992dc2 (mirrors + drift tests for csv/gob).
- `m7-strings` @ 08498e6 — "wire encoding/csv + encoding/gob" =
  synth_enc_csv_gob.go (2785 lines, "restores missing file") + synth_machinery.go.
  NOTE: on the STRINGS worker's branch but contains CSV/GOB work. Reconcile
  with m7-enc-core at integration (which version of synth_enc_csv_gob.go wins?).
- `m7-math-polluted` @ 12e5fcd — strconv worker's synth_strconv.go (+ csv/gob
  commits 44d5266, bdce031 underneath — reconcile with m7-enc-core).
- Empty branches (workers created, no commits yet): m7-bytes, m7-enc-primitives,
  m7-enc-xml, m7-strconv, m7-bytes-2 (=4d08294, obsolete duplicate).
- `merge-int` — NOT M7 (M6 loopcap merge). Leave alone.
- Stashes on m7-math: stash@{0} "importer temp", stash@{1} "w7-enc-core-wip"
  — inspect at integration; may hold recoverable work.

## Still running (7)
strings, bytes, strconv, math, enc-csv-gob, enc-json, enc-xml.
All told: commit ONLY to m7-<pkg>, never touch other branches/files,
report commit hashes + file lists.

## Integration plan (when all workers done)
1. For each package, identify definitive file set by content.
2. Clean worktree at master; cherry-pick/apply per-package files.
3. Reconcile csv/gob duplicates (m7-enc-core vs m7-strings@08498e6 vs
   m7-math-polluted@{44d5266,bdce031}).
4. Inspect stashes for recoverable work.
5. Full suites + all wave1 corpora green.
6. Spawn enc-core WIRING follow-up (exporter, mirrors, importer, drift, corpus,
   redgreen) using committed 2ebe8c4 sources.
7. Two independent reviews → remediation → sign-off.

## Open questions for integration
- Does m7-enc-core have synth_enc_csv_gob.go, or only wiring? (m7-strings says
  "restores ... was missing from tree")
- enc-core wiring follow-up: who does it (new worker vs coordinator)?
- errors-sort nil guards now redundant with runtime NPE fix — harmless, keep.

## 2026-10-07 ~03:35 UTC update
- strconv worker DONE (4/10): source complete @ aed88b7 (on m7-strconv, supersedes
  12e5fcd on m7-math-polluted), redgreen @ 5578323. Differential: 20k random
  float64s bit-exact, all formats/precisions, adversarial quotes, IsPrint sweep.
  1 carried divergence: 1-bit NaN payload (unobservable via API).
  WIRING PENDING (exporter, mirror, drift, corpus) — blocked by tree chaos;
  integration script at work/apply_integration.py (worker's scratch).
- Pattern emerging: sources land, wiring blocked. Plan: after all package
  workers finish, spawn per-package WIRING follow-ups (enc-core, strconv, ...).

## 2026-10-07 ~03:40 UTC update
- enc-csv-gob worker DONE (5/10): commits 1836c8e (sources+machinery),
  8992dc2 (mirrors+drift, PASS), 93a0d61 (corpus README+redgreen; landed on
  m7-strings by chaos). Differential: csv ~2300 cases, gob 60+ byte-identical,
  7 real bugs fixed. Reconciliation: m7-enc-core vs m7-strings@08498e6
  synth_enc_csv_gob.go are IDENTICAL (diff 0) — no conflict.
  WIRING PENDING: importer.go 2-case hunk, export.go + mrdesugar_m5.go hunks
  (lost to chaos; designs documented in worker report).
- Wiring follow-ups needed: enc-core, strconv, enc-csv-gob (+ TBD for
  strings/bytes/math/enc-json/enc-xml).

## 2026-10-07 ~03:50 UTC update
- bytes worker DONE (6/10): m7-bytes @ 9019842 (clean, 2 commits, 8 files,
  11k insertions). Sources (54 funcs + Buffer/Reader + inlined unicode tables
  verified over all 1,114,112 runes) + mirror + drift + exporter/p3frontend
  wiring. Differential vs gc ALL PASS. Phase-1 211 green.
  DEPENDENCY: imports synthetic "errors" → needs m7-errors-sort integrated.
  8 omissions pinned (iter.Seq ×5, unicode.SpecialCase ×3) with re-test triggers.
  Linkname: bytes //go:linkname Repeat is self-link, satisfied by provision;
  M6 linkdiff tool gap flagged (misses free-floating linkname directives).
  MISSING (post-integration follow-up): corpus/wave1/bytes/ programs, redgreen.md.

## 2026-10-07 ~04:00 UTC update
- strings worker DONE (7/10): m7-strings @ 77b5c6d (source+wiring, 75KB synth,
  inlined unicode tables) + 5d7c9c1 (corpus 10 programs + redgreen).
  gc differential: ~200 fixed + 4000 fuzz ALL MATCH; end-to-end byte-identical.
  KNOWN ISSUE: TestStringsSyntheticSignatures fails (rune renders int32 in
  mirror); one-line fix identified but UNCOMMITTED, saved at
  phases/phase3/hidden_files/m7-tmp/strings/synth_strings.go.final — apply at
  integration. Suites NOT run (chaos) — must run at integration.
  v1 runtime limitations documented (5, pre-existing). Omissions pinned
  (SpecialCase ×3, io ×2, iter.Seq ×5).
  NOTE: m7-strings also contains 2c7ea39 "red-green evidence for encoding/xml"
  (enc-xml worker committed to wrong branch) — sort by content at integration.

## 2026-10-07 ~04:10 UTC update
- enc-json worker DONE (8/10): f3c078f (synth_json.go, 2359 lines, on m7-enc-json)
  + 8063b3b (synth_machinery wiring, landed on m7-strings by chaos).
  Honest subset over dynamic any-model; byte-identical for supported subset.
  CRITICAL FINDING: gc 1.27.1 toolchain uses v2/jsontext-based encoding/json
  internally while vendored source is v1 — error messages differ (invalid UTF-8,
  escapes, number errors); worker matches TOOLCHAIN empirically. This affects
  how corpus expectations are written (vs toolchain, not vendored source).
  Dev source: hidden_files/m7-tmp/enc-json/dev/json/*.go.
  WIRING PENDING: export.go, mrdesugar_m5.go, p3frontend mirror, drift, corpus,
  redgreen — follow-up needed.

## 2026-10-07 ~04:20 UTC update
- enc-xml worker DONE (9/10): 9 commits on m7-enc-xml (bda1b48 source 3051 lines,
  bc0ff04 mirror+drift, 46e80d8 exporter wiring, 315f918 mrdesugar, 8eb6c66
  importer, cdf6821 corpus 4 programs, 0a3d98e redgreen, b1100a7 + c5a0a2b fixes).
  White-box differential PASS (~70 adversarial inputs, 252 HTML entities).
  Supported: token stream (Token/RawToken via mrXmlToken desugar), full grammar,
  EncodeToken, escaping. Fail-fast: Unmarshal/Marshal (reflect/P3-M8),
  CharsetReader (io/P3-M8). Structural Reader/Writer instead of io (P3-M8
  re-test). KNOWN ISSUE: drift test still fails on byte/uint8 rendering +
  CopyToken signature — complete at integration.
  NOTE: m7-enc-xml also contains non-xml commits (2ebe8c4, 4d08294, 6e5af41) —
  integrate by content. Worker also left commits 0a77fa7, 92f8bd4, 2c7ea39 on
  m7-strings (accidental) — content already cherry-picked to m7-enc-xml.

## 2026-10-07 ~04:30 UTC update — ALL 10 WORKERS DONE
- math worker DONE (10/10): m7-math, 6 commits (7de8703 source 3748 lines,
  28b2685 exporter, c64ac0f mirror+drift, 5818e3e 6 Java nodes, 5b7e01a corpus
  5/5 PASS, 3d4e0eb redgreen). CRITICAL: StrictMath != gc for transcendentals
  (11k-35k mismatches/50k); only Sqrt/FMA are JVM intrinsics (0/50,071
  mismatches); rest pure-Go verbatim gc ports. Carried: Exp FMA-path only
  (146 1-ulp diffs on non-FMA hosts — re-test trigger).

## INTEGRATION PHASE STARTS
Order:
1. Merge content-complete branches into integration worktree (master@d493a2a):
   m7-errors-sort (a4fa2bf), m7-bytes (9019842), m7-math (6 commits),
   m7-strings (77b5c6d, 5d7c9c1 + apply .final mirror fix),
   m7-enc-xml (9 xml commits by content), m7-enc-core csv/gob (1836c8e, 8992dc2),
   enc-core sources (m7-enc-json@2ebe8c4), strconv source (aed88b7),
   enc-json source (f3c078f).
2. Spawn wiring follow-ups: enc-core, strconv, enc-csv-gob, enc-json
   (exporter+mirror+importer+drift+corpus+redgreen).
3. Bytes corpus + redgreen follow-up; strings drift fix verification.
4. Full suites + all corpora green → 2 independent reviews → sign-off.
