# R-12 remediation lane rev1107-22-r12 — EVIDENCE (BLOCKED-quota)

Date: 2026-11-07 ~22:12 CET. Lane: rev1107-22-r12 (re-dispatch of rev1107-r12).
Issue: `tools/godebug-diff/godebug-diff.sh` dead + would dirty tracked REPORT.md (P1, REVIEW-2026-11-07-07 R-12, carried by REVIEW-2026-11-07-22).
HEAD verified: `59f0a247` (main repo, clean tree).

## 1. Prior-lane verification (recovery, not duplication)

- Registry `~/workspace/system/work-registry/rev1107-r12.json`: status `active`,
  heartbeat `2026-11-07T10:36:20+00:00` (~11.5h stale at lane start), description
  "no worktree claimed, no files edited". No live agent/process for it
  (ps grep: only unrelated `/var/rev1107-20-f04/work/runner.sh`).
- Branch `wt/graalgo-independent-codebase-review/rev1107-r12` exists at base
  `bf9b96ff` — zero commits. No worktree (`ls ~/workspace/worktrees/graalgo/rev1107-r12`
  → No such file or directory).
- Verdict: prior lane is DEAD. Proceeding under new stream rev1107-22-r12 is
  recovery, not duplication. (The earlier consolidated lane rv1107-03 is likewise
  dead per REVIEW-22 and covers nothing live.)
- Prior lane's durable staged fix survives at
  `~/workspace/goals/graalgo-go-toolchain-on-graalvm-truffle/hidden_files/codebase-review/R12-godebug-diff.sh`
  (8985 bytes) + `R12-RESUME.md`. This lane re-verified it instead of rewriting it.

## 2. Mechanism re-verified from disk truth at HEAD 59f0a247

- RED reproduced exactly (main tree, read-only): `bash tools/godebug-diff/godebug-diff.sh`
  → `godebug-diff.sh: line 20: cd: ./../../phases/phase2: No such file or directory`,
  then `build failed`, exit 1. `git status` clean afterwards (nothing written).
- `phases/` does not exist in the tree. Tracked files in the tool dir:
  `tools/godebug-diff/REPORT.md`, `tools/godebug-diff/godebug-diff.sh` (both tracked;
  the script overwrites REPORT.md on success → tree-dirtying confirmed by reading
  the script tail).
- Probes still exist: `test/corpus/phase2/godebug/{db02_panicnil,db03_alloc,db04_spin}.go`
  (plus db01_hello, db05_panicnil_uncaught).
- `SchedMain` still exists: `internal/backend/runtime/src/main/java/graalgo/sched/launcher/SchedMain.java`.
- Comparator formats verified against live sources:
  - `SCHED <ms>ms: goroutines=` — `internal/backend/runtime/src/main/java/graalgo/sched/SchedTrace.java:70`
  - `SCHED-DETAIL <ms>ms: quantum=` — same file, line 74 (note: prior lane cited the
    launcher dir; the emitter is `graalgo.sched.SchedTrace`, same class, still present)
  - `gc <n> @<secs>s ...` — `internal/backend/runtime/src/main/java/graalgo/sched/launcher/GcTrace.java:67-71`
    (Locale.ROOT, `%.3f` — matches the `^gc [0-9]+ @` comparator)
- Classpath: runtime Makefile `RUN_CP := $(OUT_MAIN):$(TRUFFLE_CLASSES):$(CP)` with
  `CP := $(INTEROP_CLASSES):$(jars)` — the staged script's
  `runtime:truffle:interop:jars` order is equivalent.
- Root Makefile `build:` (line 128) builds truffle + runtime + interop. `make build`
  is the correct replacement for `make -C phases/phase2 main`.
- Report location: root `.gitignore:23` `.tmp/` — `tools/godebug-diff/.tmp/REPORT.md`
  is gitignored, so the staged script's report path cannot dirty the tree.
- Doc reference: `docs/arch/phases/phase2/GODEBUG.md` exists (staged script's updated ref).
- Only commit between the morning base `bf9b96ff` and `59f0a247` is B-02
  (7 files: launcher `Main.java`, `GoOsSurfaceInitializer`, test) — nothing the
  staged script depends on. `git diff --stat bf9b96ff 59f0a247` confirms.
- Staged script diff vs current script: header + path/CP/build/report changes only,
  comparators untouched except the panicnil non-empty-stdout guard. `bash -n` OK.
  sha256 `48d0e0d309def30b8546babb7c5e8d4416faf1114fdfc875dc0dfa5e8e518395`.

## 3. Quota gate — FAILED (this lane is BLOCKED)

- Real 8 KB home write probe (`python3`, `/home/hatch/workspace/.quota-probe-r12`):
  `OSError: [Errno 122] Disk quota exceeded`. Same failure class as REVIEW-22's
  21:56 CET probe.
- `/tmp` 8 KB probe SUCCEEDED (tmpfs 478M avail). The sandbox >4 KB single-write
  EPERM limit from the morning lane is NOT currently reproducing on /tmp.
- Per the brief: NO worktree claimed, NO registry entry written, NO commit,
  NO files edited in any repo tree. Main-tree `git status` clean at end of lane.
- Memory-log append (`~/memory/2026-11-07.md`) NOT attempted: home writes are
  EDQUOT-blocked (probe above is the evidence); the outcome is recorded here
  and in the final report instead.

## 4. What is staged here (/tmp/rev1107-22-r12/)

- `godebug-diff.sh` — the fixed script, `cmp`-verified byte-identical to the
  durable goal-hidden_files copy (sha256 above). 8985 bytes.
- `EVIDENCE.md` — this file.
- `RESUME.md` — resume steps for the successor lane.
- WARNING: /tmp is tmpfs (512M, 7% used at staging) and does NOT survive a reboot.
  The durable copy remains the goal-hidden_files one; this /tmp copy exists only
  so the successor lane has a verified staged artifact even if it starts from
  this handoff alone.
