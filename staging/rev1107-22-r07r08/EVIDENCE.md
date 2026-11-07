# EVIDENCE — lane rev1107-22-r07r08 (R-07 + R-08), BLOCKED on quota

- **Lane:** `graalgo-rev1107-22-r07r08` (REVIEW-2026-11-07-22, HEAD `59f0a247`)
- **Date:** 2026-11-07 ~22:12–22:30 CET
- **Outcome:** fix staged + verified at stub level in `/tmp`; **BLOCKED — home quota
  EDQUOT-exhausted**. No worktree claimed, no registry entry written, no commit
  made, no file modified in the main repo. See `RESUME.md` to finish.

## 1. Mechanism re-verified from disk truth (HEAD 59f0a247, read-only)

### R-07 — `make graalgo` is not `.PHONY` (P1) — CONFIRMED
- `Makefile:111`: `.PHONY: all generate gen-synth build check test-ci
  test-regression test-p3frontend test-v2 test clean snapshot snapshot-check
  fast-build fast-run fast-test fast-watch` — omits `graalgo`, `gate1-pilot`,
  `gate1`.
- `Makefile:135-137`: `graalgo:` target, no prerequisites, recipe
  `$(GO) build -o graalgo ./cmd/graalgo/`.
- `Makefile:171`: `test-regression: generate build graalgo` (review said 174;
  disk truth is 171).
- `Makefile:331-334`: `clean:` recurses into the three backend dirs only —
  does NOT remove `./graalgo`.
- Consequence (make semantics): after the first build, `make graalgo` is a
  no-op and `test-regression` tests the old driver binary. CI unaffected
  (fresh checkout); local/dev-loop runs silently test stale code.

### R-08 — `make gate1-pilot` cannot fail (P1) — CONFIRMED
- `Makefile:224-231`: loop body ends `... --graalgo-run
  "$(CURDIR)/internal/backend/graalgo-run" || true;`, then `@echo "Gate 1
  pilot complete. ..."` unconditionally. A pilot with all five packages
  diverging still exits 0.
- `tools/gate1/run.py:143`: `return 0 if verdict == "PASS" else 1` — run.py
  already exits non-zero on DIVERGE/SKIP, so the Makefile `|| true` is the
  only swallower. Composition with R-02: the both-legs-fail case currently
  yields PASS from run.py (R-02's lane owns that); once R-02 lands, this
  pilot fails it too via run.py's non-zero exit — no further Makefile change
  needed.

## 2. Prior-lane state verified (recovery check, before starting)

### R-08 prior lane `graalgo-rev1107-r08` — DEAD, safe to supersede
- Registry `~/workspace/system/work-registry/graalgo-rev1107-r08.json`:
  status `active`, heartbeat `2026-11-07T07:43:08Z` (~14h stale at 22:12 CET),
  base `bf9b96ff`, checkpoint says "no worktree claimed, no files edited".
- Creator pid 75930: **dead** (`ps -p 75930` empty). No live process matching
  `rev1107|r08` (only an unrelated `rev1107-20-f04` runner).
- Branch `wt/graalgo-independent-codebase-review/rev1107-r08`: **does not
  exist** (only `review1101-r08-tls-*` branches match `r08`).
- Worktree `~/workspace/worktrees/graalgo/rev1107-r08`: **does not exist**
  (`git worktree list` grep).
- Worktrees registry `~/workspace/system/worktrees/registry.json:11727`:
  stale `creating` entry `graalgo/rev1107-r08` (base `bf9b96ff`, dir never
  created) — needs reconciliation by `wt.py` on a future claim of that
  stream, or coordinator cleanup. This lane uses the NEW stream
  `rev1107-22-r07r08`, so no conflict.
- Prior lane's staged work SURVIVES and was adopted: `R08-PATCH.diff` +
  `R08-RESUME.md` in the goal `hidden_files/codebase-review/` dir. The
  R-08 hunk in `FIX.patch` is that patch verbatim (its `git apply --check`
  on `bf9b96ff` was already verified by that lane; the Makefile is unchanged
  between `bf9b96ff` and `59f0a247` — `git diff --stat` shows only 7 launcher
  Java files in `59f0a247`).
- Coordinator note: mark `graalgo-rev1107-r08.json` done/lost once this fix
  lands, so no third dispatch re-covers R-08.

### R-07 prior lane — NONE
- No registry entry, branch, or worktree. Only an interrupted registration
  `rev1107-r07.json.tmp` (registration died mid-write, presumably quota).

## 3. Quota gate — FAILED (this is the blocker)

- Probe (own exec call, 22:1x CET): `dd if=/dev/zero
  of=/home/hatch/probe-rev1107-22-r07r08 bs=8192 count=1` →
  `dd: failed to open '/home/hatch/probe-rev1107-22-r07r08': Disk quota
  exceeded`. `wc -c` confirmed the file does not exist. Probe file removed
  (nothing to remove).
- `/tmp`: 386M avail (tmpfs 512M, 25% used) — staging here is fine.
- `/var/tmp`: 100% full (per REVIEW-22; not re-probed — prior lane's
  `/var/tmp/r08-new-block.txt` not relied on).
- Per the lane brief: no worktree claimed, no registry entry written, no
  commit attempted.

## 4. The fix — `FIX.patch` (staged, verified to apply)

Combined patch, `Makefile` only, three hunks:
1. `.PHONY`: add `graalgo gate1-pilot gate1` (+ 3-line comment explaining why,
   so a future cleanup does not silently reintroduce R-07).
2. `gate1-pilot`: drop `|| true`; accumulate failures in `failed`; run all
   packages, then print `Gate 1 pilot FAILED: ...` and `exit 1` if any failed
   (adopted verbatim from the prior lane's `R08-PATCH.diff`).
3. `clean`: add `rm -f ./graalgo` (last line of the file, line 334).
- `git apply --check` in a scratch repo in `/tmp`: **CLEAN**. Applied file
  is byte-identical to the independently produced `verify/patched-Makefile`
  (`diff` empty). (First hand-written draft had a hunk-count error;
  the staged patch was regenerated mechanically with `diff -u` from the
  verified patched file, so headers/context are exact.)
- Design notes: `.PHONY` (not source prerequisites) is the fix per the
  review — precise prerequisite tracking of the driver build is fragile;
  forcing the rebuild on every `make graalgo` is what `test-regression`
  needs. No `run.py` change (R-02 owns it). No docs change needed
  (no directory added/moved; REPO_STRUCTURE untouched).

## 5. RED→GREEN stub proofs — ALL 12 CHECKS PASSED

Harness: `verify/red/` (original Makefile) vs `verify/green/` (patched),
real `Makefile` copies, stub `$(GO)` (logs invocation, touches `graalgo`),
stub `tools/gate1/run.py` (same exit contract as real: 0 iff PASS else 1;
`$FAIL_PKGS` diverges), stub backend Makefiles (`main`/`clean` targets).
Runner: `verify/run-proofs.sh`, full log: `verify/proof.log`.

| Check | RED (orig) | GREEN (patched) |
|---|---|---|
| `make graalgo` twice → recipe runs | 0 (both `up to date` — stale) | 2 (rebuilds every time) |
| `make clean` with `./graalgo` present | file survives | file removed |
| `gate1-pilot`, 1/5 diverge → exit code | 0 (bug) | 2 (recipe `exit 1` → make rc 2) |
| `gate1-pilot`, 1/5 diverge → packages run | 5 | 5 (+ `Gate 1 pilot FAILED` banner) |
| `gate1-pilot`, all pass → exit code | — | 0, `complete` banner |
| `gate1-pilot`, all diverge → exit / run | — | non-zero / 5 run |

- Exact exit codes re-checked separately: red pilot 1-fail `0`, green pilot
  1-fail `2` (logs: `verify/red-pilot-exit.log`, `verify/green-pilot-exit.log`).
- Proof subtlety (recorded, not a defect): `Makefile:264`
  `SNAPSHOT_TOOLCHAIN := $(shell $(GO) version ...)` runs `go version` at
  parse time on EVERY make invocation, so the stub log also gets one
  `stub-go version` line per make run; the proof counts only `stub-go build`
  lines. First proof run tripped on this (counts 2× expected); corrected and
  re-run green.
- NOT verified here (needs a real worktree + quota): real `go build` of the
  driver, real `gate1-pilot` end-to-end, `make all`, CI on the branch.
  The resume lane must run those (see `RESUME.md`).

## 6. Environment hazards observed (for the resume lane)

- `/dev/null` is a REGULAR FILE (`-rw-rw---- 1 root root 67`, mtime 2026-11-07
  21:13), not a character device — still true (REVIEW-20 flagged it). Writes
  to it accumulate in that file instead of being discarded; reads return its
  67 bytes instead of EOF. `Makefile:264` uses `2>/dev/null`; harmless while
  the process runs as root (file is writable) but the anomaly should be fixed
  at the sandbox level. Avoid relying on `/dev/null` in verification commands.
- Home quota EDQUOT-exhausted at probe time (~22:15 CET); treat as flapping
  until a real write succeeds in the same window as the work.

## 7. Staged inventory (`/tmp/rev1107-22-r07r08/`)

- `FIX.patch` — the fix (2,195 bytes; `git apply --check` CLEAN).
- `EVIDENCE.md` — this file.
- `RESUME.md` — how to finish the lane.
- `verify/orig-Makefile` — HEAD 59f0a247 Makefile copy (17,972 bytes).
- `verify/patched-Makefile` — FIX.patch applied (byte-identical to
  `git apply` result in `verify/apply-test/`).
- `verify/apply-test/` — scratch repo proving `git apply --check` + apply.
- `verify/red/`, `verify/green/` — RED/GREEN sandboxes (Makefile + stubs).
- `verify/stub/` — the three stub sources.
- `verify/run-proofs.sh` + `verify/proof.log` — proof runner and full log.
- `verify/red-pilot-exit.log`, `verify/green-pilot-exit.log` — exact rc logs.
