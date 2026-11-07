# RESUME — rev1107-22-f1: F1 (P0) + F2 (P1), linkdiff synthetic-scan machinery + per-package retirement inventory

- Status: **fix verified RED→GREEN and staged; BLOCKED-quota** (no worktree, no commit, no push).
- Base: HEAD `59f0a247`. Full evidence: `EVIDENCE.md` in this dir.
- Staged dir: `/tmp/rev1107-22-f1/` — **tmpfs, does NOT survive a reboot**. On quota recovery, first copy this dir to persistent disk.

## 0. Precondition (quota gate — do this first)

Probe home with a REAL 8 KB write and verify its byte count before claiming anything:

```sh
python3 -c "open('/home/hatch/.quota-probe-f1','wb').write(b'x'*8192)"; ls -l /home/hatch/.quota-probe-f1; rm -f /home/hatch/.quota-probe-f1
```

If it fails `Errno 122 Disk quota exceeded`: STOP, do not claim a worktree, do not commit. Re-verify the staged files are intact (`sha256sum fix.patch` must be `30259514da024355ee2dcc89fbb06d02ef6190213dcd2187010c5c116abaca65`), and report BLOCKED-quota again. A 0-byte touch passing proves nothing; a `/var/tmp` probe proves nothing about home quota.

## 1. Claim the worktree (only after the probe succeeds)

```sh
nice -n 10 ~/workspace/system/bin/load-shed.sh backoff   # must be CLEAR before heavy steps
# register the lane first (work-registry), then:
nice -n 10 ~/workspace/system/bin/wt.py new graalgo rev1107-22-f1 --chat f7467b85-7da6-4da0-b9ec-94631a60dafe
```

Work only in the worktree. Never edit/build/commit in the main tree. Every command under `nice -n 10` (heavy: `-n 19`). Backoff before any compile/test.

## 2. Apply the fix

From the worktree root (repo-relative paths in `fix.patch`):

```sh
git apply --check /tmp/rev1107-22-f1/fix.patch && git apply /tmp/rev1107-22-f1/fix.patch
git status --porcelain   # expect exactly: M README/main/main_test/provided/report/scan.go + bridge-inventory.md;
                         # A nosynth_test.go + synthetic-retirement-inventory.md;
                         # D synth.go + synth_test.go. Nothing else.
```

If the HEAD has moved past `59f0a247`, re-verify the mechanism at the new HEAD first (the F1/F2 files may have drifted); if `git apply` fails, do NOT hand-edit the tree into shape — regenerate the fixed files from `patched/` (repo-relative layout, byte-identical to the verified tree) and re-diff.

## 3. Re-verify RED→GREEN in the worktree (do not trust this note's logs)

```sh
export PATH=~/tools/go/bin:$PATH
cd tools/stdlib-linkdiff
nice -n 10 go vet ./...            # expect clean
nice -n 10 gofmt -l .              # expect empty
nice -n 10 go test -count=1 ./...  # expect 19/19 PASS
```

RED check (proves the new tests can fail): `git stash` the fix (or `git checkout HEAD -- tools/stdlib-linkdiff`), run the three new tests — `TestProvidedExtraction`, `TestNoSyntheticCredit`, `TestReportHasNoSyntheticFields` must FAIL — then restore. (The RED evidence is already in `evidence/red-rerun.log`; this is a re-confirmation, not a first run.)

Optional end-to-end: build the tool and run it (`--goroot <go-src> --graalgo <worktree-root> --json /tmp/v2.json`); expect version `2.0.0`, provided symbols ~155 (registry only — the exact number moves with the registry), and no `synthetic_*`/`live_*` keys in the JSON. Do NOT compare the skip-list size against v1 reports (different baseline by design).

## 4. Commit (explicit pathspecs, no bare `git commit`)

```sh
git add tools/stdlib-linkdiff/README.md tools/stdlib-linkdiff/main.go tools/stdlib-linkdiff/main_test.go \
  tools/stdlib-linkdiff/nosynth_test.go tools/stdlib-linkdiff/provided.go tools/stdlib-linkdiff/report.go \
  tools/stdlib-linkdiff/scan.go tools/stdlib-linkdiff/synth.go tools/stdlib-linkdiff/synth_test.go \
  docs/arch/synthetic-retirement-inventory.md docs/arch/bridge-inventory.md
git commit -m "linkdiff v2.0.0: stop crediting synthetic stdlib packages (REVIEW-2026-11-07-22 F1/F2)

- Delete the synthetic-scan machinery (synth.go, synthetic provided-set
  extraction, shadow marking); the provided set is the P3-M4 intrinsic
  registry only, so the skip-list is the honest retirement worklist.
- Per-package retirement inventory: docs/arch/synthetic-retirement-inventory.md
  (130 live packages, all 'needs design'; 2 dedicated stubs; 7 retired precedents).
- bridge-inventory.md: record the partial retirement (R-26)."
git log --oneline -1   # record branch + SHA in the final report
```

## 5. Report, do NOT integrate

Report branch + SHA to the coordinator. Independent review must pass before any merge; a rebase voids any approval (fresh review on the new tip). Do not merge, do not push to master. If this lane's worktree is no longer needed after the report, `wt.py abandon` it per the Worktree Charter (final action removes the worktree; do not walk away with a registered worktree on disk).

## 6. If staging is lost (reboot before recovery)

Regenerate from the repo + this note:
- `fix.patch` regenerates from `patched/` (fixed tree) vs pristine HEAD copies (`work/old/` is pristine HEAD; `work/new/` == fixed tree). If `patched/` is also lost, the fix is reproducible from `EVIDENCE.md` §2 (file-by-file change list) — but re-verify RED→GREEN from scratch; do not claim the old logs.
- The inventory regenerates from `inventory-rows.txt` + `gen-inventory.py` (rows verified == `syntheticSources` keys; re-check against the then-current HEAD before regenerating).
