# BE-06 re-derive — resume (run when home quota opens)

## Preconditions

1. Probe home quota FIRST with a real 8 KB unique-content write
   (`head -c 8192 /dev/urandom > ~/workspace/.quota-probe-<unique>` + `wc -c`
   to verify the byte count). A 0-byte touch proves nothing. Proceed only
   if the write succeeds.
2. Confirm HEAD is still `59f0a247`. If master has moved, rebase the patch
   onto the new HEAD and re-verify (a rebase voids any prior review).

## Apply

```sh
# Register + claim worktree (registry id suggestion: graalgo-be06-rederive)
~/workspace/system/bin/wt.py new graalgo be06-rederive --chat f7467b85-7da6-4da0-b9ec-94631a60dafe
cd ~/workspace/worktrees/graalgo/be06-rederive   # verify the actual path wt.py prints
git apply /tmp/be06/be06.patch
git apply --check /tmp/be06/be06.patch   # must be clean; patch sha256:
# 42c5f3d48e759fe53d604abd1a73b5b7ad81ac1e21c305c7b2e9975698d20940
git status --porcelain   # expect exactly the 5 files below, nothing else
```

Files in the patch (5, +251/−4, vs HEAD `59f0a247`):
- `internal/backend/runtime/src/main/java/graalgo/sched/launcher/SchedMain.java`
- `internal/backend/runtime/src/main/java/graalgo/launcher/Main.java`
- `internal/backend/runtime/src/main/java/graalgo/launcher/README.md`
- `internal/backend/runtime/src/test/java/graalgo/sched/SchedTestRunner.java`
- `internal/backend/runtime/src/test/java/graalgo/sched/launcher/SchedMainOsArgsTest.java` (new)

## Verify (in the worktree; backoff-gate first, every command under nice)

```sh
~/workspace/system/bin/load-shed.sh backoff   # must exit 0 before any build/test
nice -n 10 make -C internal/backend/runtime test   # full SchedTestRunner suite
# If the test JVM is SIGKILLed under load, cap its heap explicitly (-Xmx768m
# fixed two rc=137 kills in scratch verification; see EVIDENCE.md).
nice -n 10 make all   # per-push gate (generate, build, snapshot-check, check, test-ci)
```

Expected in-worktree results (already demonstrated in scratch — re-confirm,
do not trust this note): `SchedMainOsArgsTest` 5/5, `LauncherOsArgsTest` 7/7,
`GoDebugTest` 27/27 (run from the runtime module dir).

## Land

1. Commit with explicit pathspecs (never bare `git commit` on the shared tree).
2. Push immediately; watch CI to verdict (green or explained failure) on the
   exact SHA before calling the lane done.
3. Report completion to the coordinator — DO NOT integrate/merge yourself.
   Independent review is owed on the exact tip SHA; a rebase voids review.
4. On success, remove the scratch: `rm -rf /tmp/be06-rederive /tmp/be06`
   (keep until the lane is done — the patch + evidence live only here).

## Scratch reuse (optional, avoids recompiling)

Prebuilt scratch classes are at `/tmp/be06-rederive/classes/` (runtime,
interop, test, minirunner). Re-run a single class with the GREEN classpath
from EVIDENCE.md (scratch dirs first, then prebuilt `out/classes`), with
`GRAALGO_GOFRONTEND=/var/tmp/b02-verify/internal/frontend/gofrontend` and
`-Djava.io.tmpdir=/tmp/be06-rederive/tmp`. The `MiniRunner` there is
scratch-only test tooling, NOT part of the patch.
