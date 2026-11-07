# RESUME.md — rev1107-22-r05 (R-05, `tools/mutation` dead) — resume note

**Status: FIX COMPLETE AND VERIFIED RED→GREEN. LANDING BLOCKED ON HOME QUOTA ONLY.**

## What is staged in /tmp/rev1107-22-r05/

- `mutation.patch` — the complete fix (1362 lines; `tools/mutation/main.go`, `mutants.json`, new `main_test.go`, new `README.md`). Applies cleanly to HEAD `59f0a247` (`git apply --check` clean).
- `tools-mutation/` — the patched tree (== `patchcheck/tools/mutation/`).
- `mutation-new` — binary built from the staged code (4.4 MB; also at `/run/rev1107-22-r05-tmp/mutation-new`).
- `EVIDENCE.md` — full RED→GREEN evidence (read this first).
- `red/` — RED reproduction inputs: HEAD `main.go`, `mutation-old` binary, `probe-spec.json`, `red-run.log`.
- `logs/` — `gotest-resume.log` (14/14 PASS), `java-mutant-leg-resume.log` (4/4 COMPILE OK), `go-leg-resume.log` (control + M-G01 BUILD OK), plus the prior lane's logs.
- `java-leg/`, `go-leg/` — mutant compile/build artifacts.
- `go-leg.sh`, `java-mutant-leg.sh` — leg scripts (note: `go-leg.sh` hardcodes `/tmp/gocache`; use `/run` for the cache when `/tmp` is full — see EVIDENCE.md).

## What the resume lane did vs the prior lane

- Re-verified the R-05 mechanism from disk truth at HEAD `59f0a247` (matches the review).
- Verified the staged patch applies cleanly; verified staged `tools-mutation/` == `patchcheck/`.
- Re-ran `go vet` (clean) and `go test` (14/14 PASS) with a `/run` GOCACHE after `/tmp` ENOSPC killed the first attempts (environmental).
- Reproduced RED with the old binary (exit 1, baseline-not-green) and GREEN with the new binary (`--dry-run` exit 0; full run fails fast exit 1 naming the missing `Runner.class` + make target).
- Completed the Go overlay-build leg the prior lane never finished (control + M-G01 both BUILD OK, 330-entry merged overlay).
- Confirmed no `phases/` resurrection in the repo.

## Prior-lane recovery note (closed)

- The old worktree `~/workspace/worktrees/graalgo/rev1107-r05` exists on disk but is a hollow shell (all files 0 bytes, corrupt git index, branch at base `bf9b96ff`); its registry entry is `.tmp` only. Adopting it is pointless — it contains nothing. It was NOT adopted (quota forbids claiming anyway). Coordinator may reap it.
- No worktree was claimed by this lane (quota rule). Nothing was committed or pushed.

## To land (coordinator, when home quota clears)

1. Probe home with a real 8 KB write; only proceed on success.
2. Claim/adopt a worktree (`wt.py new graalgo rev1107-22-r05 --chat graalgo-independent-codebase-review`, or adopt a fresh one), apply `mutation.patch`, commit.
3. Re-run `go vet ./... && go test ./...` in `tools/mutation` in the worktree (GOCACHE on `/run` if `/tmp` is still full).
4. Dispatch the **independent reviewer** — do NOT integrate directly. Reviewer checks: no bridges, contract per `README.md`, tests pin the repointed paths.
5. Optional end-to-end confirmation (needs `Runner.class` + `gofrontend` built first, both home-tree builds): run `mutation-new [-test-tmpdir /run/...] <repo> tools/mutation/mutants.json` and expect per-mutant KILLED/SURVIVED verdicts, exit 0. Not required for the fix's correctness — the tool's own fail-fast already proves the repoint — but it is the first real mutant run since the repoint.

## Gaps / caveats

- No end-to-end mutant run yet (missing `Runner.class` + `gofrontend` in the repo; building them is home-quota-blocked). All other legs are green.
- `/run/rev1107-22-r05-*` caches are tmpfs scratch (reboot-loses); the durable deliverables are all in `/tmp/rev1107-22-r05/`.
