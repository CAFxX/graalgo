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
