# R20-F03 resume note (self-gating)

Lane rev1107-20-f03 — R20-F03: disk import honors go.mod pins.
Staging dir: `/tmp/rev1107-20-f03/` (NOT /var/tmp — it is 100% full).

## State

- Fix + 12 tests staged as `R20-F03.patch` (693 lines, applies cleanly
  to graalgo HEAD `59f0a247` — verified `git apply --check`).
- Design: `DESIGN.md`. Evidence: `RED-GREEN.md`. Harness:
  `harness/` (verbatim-extracted pin logic; RED/GREEN executed, all
  expectations hold).
- NOT landed: home quota was CLOSED (real 8 KB probe failed EDQUOT;
  workreg register failed Errno 122). No worktree claimed, no branch,
  no commits. Do NOT integrate from this note alone — land via the steps
  below, then independent review before merge (standing rule).

## Resume steps (run in order; do NOT spin or poll — if any step's gate fails, stop and report)

1. **Quota gate.** Real home write probe first (representative size,
   byte-count verified — a touch proves nothing):
   `head -c 8192 /dev/urandom > ~/workspace/.quota-probe-rev1107-20-f03.bin && ls -l ~/workspace/.quota-probe-rev1107-20-f03.bin && rm -f ~/workspace/.quota-probe-rev1107-20-f03.bin`
   If EDQUOT: stand down again, leave this note, do NOT poll.
   Also note: `/dev/null` in this VM is currently a clobbered regular
   file (152 B, other lanes' probe tracebacks) — avoid `/dev/null`
   redirects until it is restored; use real scratch files in /tmp.
2. **Register + claim worktree.**
   `python3 ~/workspace/system/bin/workreg.py register --id rev1107-20-f03 --chat graalgo-independent-codebase-review --kind subagent --description "R20-F03: disk import honors go.mod pins" --checkpoint /tmp/rev1107-20-f03/RESUME.md`
   `~/workspace/system/bin/wt.py new graalgo rev1107-20-f03 --chat graalgo-independent-codebase-review`
   Work ONLY in that worktree; never edit/build/commit in main.
3. **Apply the patch in the worktree:**
   `git apply /tmp/rev1107-20-f03/R20-F03.patch`
   (or the split hunks `R20-F03-diskimport.patch`,
   `R20-F03-export.patch`, `R20-F03-test.patch`).
4. **Verify (all under `nice -n 10`, backoff-gated before the heavy steps).**
   - `go test ./internal/frontend -run 'TestDiskImport|TestParseGoModPins' -count=1 -v` — the 12 new tests must pass.
   - `go test ./internal/frontend -count=1` — full package, no regressions.
   - `make check` in the worktree; `make snapshot-check` (differential
     probe — any drift must be explained or re-baselined deliberately).
   - `git diff --exit-code` sanity: `make all` must be green before push
     (per repo AGENTS.md §1).
5. **Commit** with explicit pathspecs
   (`internal/frontend/diskimport.go internal/frontend/export.go internal/frontend/diskimport_version_test.go`),
   push, watch CI to verdict on the exact SHA.
6. **Independent review before merge** (standing order): report branch +
   SHA to the coordinator; do NOT integrate yourself. Final action is
   `wt.py integrate` or `wt.py abandon` per the coordinator's call.
7. **Memory log**: append the outcome to `~/memory/2026-11-07.md`
   (this run could not — home quota closed).

## Known follow-ups (not in this patch)

- Full MVS over the module graph (transitive-only modules; see
  DESIGN.md §3).
- Module-not-required-at-all: gc would fail, graalgo still takes
  highest cached — separate behavior change.
- Latent legacy-loop bug: uppercase subpackage elements are looked up
  escaped (`!sub`) while cache subdirs are literal (DESIGN.md §4).
