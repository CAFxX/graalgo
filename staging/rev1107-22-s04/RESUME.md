# Lane rev1107-22-s04 — resume note (quota-blocked, staged in /tmp)

- **Status:** BLOCKED-quota. Fix + test complete and verified GREEN in a
  standalone harness (see `EVIDENCE.md`); nothing committed, no worktree,
  no registry entry. Home quota was EDQUOT-exhausted for the whole run
  (real 8 KB probe failed ~22:14 CET and re-checked at close — see below).
- **Staged dir:** `/tmp/rev1107-22-s04/` (all files listed in `EVIDENCE.md` §2).
  /tmp had 483M free at run start; the staged tree is ~few MB plus the Go
  build cache (`gocache/`, disposable — delete it first if /tmp is tight).

## Landing recipe (on quota recovery)

1. Re-probe home with a real 8 KB write + byte-count check. If it fails,
   STOP — do not claim a worktree or commit; this note stays valid.
2. Register: suggested id `graalgo-rev1107-22-s04`, then claim a worktree:
   `~/workspace/system/bin/wt.py new graalgo rev1107-22-s04 --chat graalgo-independent-codebase-review`
   (branch `wt/graalgo-independent-codebase-review/rev1107-22-s04`, base HEAD `59f0a247`).
3. In the worktree, from the repo root:
   - `cp /tmp/rev1107-22-s04/deliver/GoFloatFmtTest.java.new internal/backend/runtime/src/test/java/graalgo/runtime/GoFloatFmtTest.java`
   - `patch -p1 < /tmp/rev1107-22-s04/deliver/GoFloatFmt.java.patch`
   - `patch -p1 < /tmp/rev1107-22-s04/deliver/SchedTestRunner.java.patch`
   - Verify the patches still apply (they were dry-run-verified against HEAD
     `59f0a247`; if master has moved, rebase the worktree first and re-check).
4. Verify in the worktree (under `nice -n 10`, after `load-shed.sh backoff` CLEAR):
   - `make -C internal/backend/runtime test` (runs the new test via
     `SchedTestRunner`; also runs the full runtime suite — the affected suite).
   - `make -C internal/backend/runtime check` (placeholder/debug gates).
   - Expected: new test 7/7 PASS; no other suite drift (the implementation
     change alters only `%f` output in the `want <= 0` class — any drift
     elsewhere is a red flag, not this change).
   - If the full runtime build is impractical, the standalone harness in
     `/tmp/rev1107-22-s04/` (`src/`, `test/`, `RunTests.java`, `out-final/`)
     reproduces the green run; but the in-repo `make test` is the real gate.
5. Commit with explicit pathspecs (the three files only), then report
   completion with branch/commits/evidence. Do NOT integrate — independent
   review first (the reviewer must scrutinize the `GoFloatFmt.java` fix hunk
   separately from the test; see `EVIDENCE.md` §5).

## What the reviewer must check

1. The `roundedParts` `want <= 0` fix: is the "one unit of 10^-prec iff exact
   value strictly exceeds half a unit" reasoning correct for all `%f` cases?
   (Ties go to zero because 0 is the even neighbor — confirm against
   Go's `decimal.Round` semantics, not just the vectors.)
2. The test vectors are gc-computed (regenerate from `genvectors.go` +
   `gc-vectors.txt` if in doubt); spot-check a few bit patterns by hand.
3. The untestable tie-break note in the test header (RED3): confirm the
   5-adic impossibility argument, or find a counterexample.
4. The `SchedTestRunner` registration is the only wiring change needed;
   confirm no other runner enumerates runtime tests.

## Open items / risks

- The in-repo `make test` (runtime) has NOT been run — only the standalone
  harness. The joint-round compile (runtime + truffle sources) could surface
  an issue the single-file compile cannot (unlikely: the change touches one
  method body + one new test file + one registration line).
- `/tmp` is tmpfs (512M): if the VM reboots before landing, the staged tree
  is LOST. Mitigation if quota stays closed for long: copy
  `/tmp/rev1107-22-s04/deliver/` + `EVIDENCE.md` + `RESUME.md` to a
  quota-free durable location when one becomes available (do NOT copy into
  the repo tree outside a worktree). The test itself is regenerable from
  `genvectors.go` + `gen_test.py` + `gc-vectors.txt` (all in `deliver/`).
- The P0 bug (§5 of EVIDENCE.md) affects live behavior TODAY: any graalgo
  program printing a sub-1.0 float with `%f` prec 0 (or sub-0.1 with prec 1,
  etc.) that rounds up gets the wrong output (e.g. `fmt.Println(0.625)`
  uses `%g`, unaffected; but `fmt.Printf("%.0f", 0.625)` prints `0` instead
  of `1`). If the coordinator lands the test without the fix, the suite
  goes red — land them together.
