# Evidence index — lane rev1107-22-r01 (R-01 P0 + R-14 P1)

Staging dir: `/tmp/rev1107-22-r01/` (tmpfs — copy to persistent disk on recovery).
HEAD verified against: `59f0a247`. Prior lane `rev1107-r01-r14` confirmed dead
(registry heartbeat = creation 07:20:46Z, branch at base `bf9b96ff`, no worktree,
no live process); its 7 files adopted byte-identical (cmp-verified) + 1 new fix
(triage invocation) + 2 doc-only stale-`phases/` fixes = 9 staged files.

## Layout

- `RESUME.md` — status, what changed, new finding, resume steps.
- `EVIDENCE.md` — this index.
- `tools/fuzz-soak/harness/run_batch.py` — fixed (prior lane).
- `tools/fuzz-soak/harness/diff_one.sh` — fixed (prior lane).
- `tools/fuzz-soak/harness/triage.py` — fixed (prior lane).
- `tools/fuzz-soak/run_soak.sh` — fixed (prior lane + this lane's triage-call fix).
- `tools/fuzz-soak/gha/driver/fuzz_driver.py` — fixed (prior lane).
- `tools/fuzz-soak/gha/Dockerfile` — fixed (prior lane).
- `tools/fuzz-soak/gha/README.md` — fixed (prior lane).
- `tools/fuzz-soak/DESIGN.md` — fixed (this lane, doc-only).
- `.github/workflows/fuzz-soak.yml` — fixed (this lane, doc-only).

## Evidence files (evidence/)

| File | Proves |
|---|---|
| `syntax-check.log` | `bash -n` both .sh OK; `py_compile` all 3 .py OK; `fuzz-soak.yml` parses as YAML |
| `red-demo.log` (+ `red-out/`) | RED: HEAD `run_batch.py --seeds 0:1` exits 1, `make: *** .../phases/phase3: No such file or directory`, no `results.jsonl` |
| `triage-invocation.log` | Old triage call shape → argparse exit 2 (`required: --prog`); new shape parses and reaches `minimize()` |
| `gate-test.log` | Verbatim `run_batch.py` fail-loudly block: empty/short/all-HARNESS_ERROR → exit 1; real batch → exit 0 |
| `soak-gate-test.log` | Verbatim `batch_programs_run` (missing/empty → 0, 2-line → 2) and final-report block (zero programs → exit 1, `programs_run: 0`; real batch → exit 0, `programs_run: 2, diverged_total: 1`) |
| `diff-vs-head.txt` | Full unified diff of all 9 staged files vs HEAD `59f0a247` (417 lines) |
| `staged-sha256.txt` | SHA-256 of all 9 staged files |
| `report-block.py`, `bpr-fn.sh`, `bpr-test.sh` | Extracted verbatim code under test (from the staged files) |
| `soak0/`, `soak1/`, `gate-*/` | Synthetic fixtures used by the gate tests |

## What could NOT be verified in this run (quota-blocked)

Full GREEN: `make build` + `make generate` in a worktree followed by a real
2-program `run_soak.sh --seeds 0:2` run — needs home writes (worktree claim,
build outputs). The fail-loudly gates were verified in isolation on verbatim
extracted code; the end-to-end run is step 6 of RESUME.md.
