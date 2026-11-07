# Lane rev1107-b04 — B-04 remediation (staged working notes)

Review: REVIEW-2026-11-07-07, issue B-04 (P1). Base: master `bf9b96ff`.
Worktree target: `~/workspace/worktrees/graalgo/rev1107-b04`,
branch `wt/graalgo-independent-codebase-review/rev1107-b04`.

## Mechanism — verified from disk truth (main tree, read-only)

1. `GoOs.resetState` (`internal/backend/interop/src/main/java/graalgo/p3os/GoOs.java:378-380`)
   does `HANDLES.clear(); NEXT_COOKIE.set(16);` — drops every open
   file/socket/dir handle without closing it (fd leak), then reissues
   cookies from 16.
2. `GoNet.resetForTest` (`internal/backend/interop/src/main/java/graalgo/p3net/GoNet.java:172-177`)
   only clears `STATES`; `GoNetpoll.resetForTest`
   (`.../p3net/GoNetpoll.java:199-218`) clears `DESCS`/`BY_SOCK`/selector —
   but neither is called by `GoOs.init`/`resetState`. Stale `SockState` /
   `PollDesc` keyed by a reused cookie can alias a new handle
   (`GoNetpoll.java:428,498` `BY_SOCK.get(sockCookie)`;
   `GoNet.sockState` consulted at `GoNet.java:439,474,514,541,672,696,743,775,889,921`).
3. The close path already does the right thing per handle: `GoOs.close`
   (`GoOs.java:755-779`) removes the handle, calls
   `GoNetpoll.evictForClose(cookie)` for `KIND_SOCKET` (wakes parked
   waiters, deregisters, cancels the key), then `handle.close()`.
   `ChannelHandle.close()` is a no-op for stdio (`closeUnderlying=false`),
   so closing all handles can never close the real stdio streams.
4. Sockets share the cookie namespace (`registerSocketChannel`,
   `GoOs.java:638-644`) and are closed through the same `close()` path
   (`KIND_SOCKET = 5`).
5. No `static {}` blocks in `GoNet`/`GoNetpoll`, and no `GoOs.*` reference
   in their static field initializers (grep-verified) — calling their
   `resetForTest()` from `GoOs.resetState`, including during the `GoOs`
   static initializer (empty handle table, loop no-ops), is class-load safe.
6. Production caller of `GoOs.init`: only `P3SyncMain.java:515`.
   Test callers: `GoNetpollTest` setup/teardown (calls all three resets by
   hand — idempotent after the fix, left untouched) and `GoOsErrnoTest`
   (10× `GoOs.init`, no net state — harmless).

## Fix (see B04.patch — applies to the worktree)

In `GoOs.resetState`, before resetting the cookie space:
- Close every non-stdio handle through the normal `close(cookie)` path
  (snapshot the key set first; skip cookies 1-3, which are re-registered
  below and must never be closed underneath). Each close is best-effort
  (`catch RuntimeException`, continue) so one bad handle cannot abort the
  reset and strand the rest — `close()` itself already converts
  `IOException` to a return code.
- Then call `graalgo.p3net.GoNet.resetForTest()` and
  `graalgo.p3net.GoNetpoll.resetForTest()` (fully-qualified: `p3net` is a
  different package; no new import needed — matches the existing
  fully-qualified `graalgo.p3net.GoNetpoll.evictForClose` call in `close()`).
- Order matters: handles first (socket closes evict their poll descs),
  net resets second (clears any straggler state + tears down the
  selector/poller thread), cookie space last.
- Javadoc updates: `init` (now a complete per-run reset incl. net state),
  `GoNet.resetForTest` / `GoNetpoll.resetForTest` (now the canonical reset
  entry points called by `GoOs.init`, not test-only hooks).

No other production file changes. In particular NO change to
`internal/backend/runtime/.../launcher/Main.java` — that is the B-02
lane's file (it will call `GoOs.init(argv)` in `runOne`/`runBatch` and
thereby gets the complete reset for free; if B-02 added its own
`GoNet`/`GoNetpoll` resets they are idempotent and harmless, but it
should not need to).

## Test (see B04.patch — new file + runner registration)

New `src/test/java/graalgo/p3net/GoOsResetTest.java` (package
`graalgo.p3net`, so package-private `GoNet.sockState` and
`GoNetpoll.descForTest` are reachable), registered in
`IfaceTestRunner` after `GoNetpollTest`:

1. `testResetClosesHandlesAndClearsNetState` — warm-up open/reset cycle
   (triggers lazy JVM/selector fd allocation so the measured cycle is
   balanced), then: open temp file (cookie 16) + socket
   (`AF_INET`/`SOCK_STREAM`, cookie 17) + `pollOpen` (ctx, `SockState`
   and `PollDesc` present, 1 fd referencing the temp file, baseline+1
   socket fds via `/proc/self/fd` symlink targets); `GoOs.init`;
   assert 0 fds reference the temp file, socket fds back to baseline
   (proves channels actually closed, not just dropped); reopen file →
   cookie 16 reissued; assert `GoNet.sockState(16) == null` and
   `descForTest(oldCtx) == null` (proves no stale net state under the
   reused cookie); assert `pollOpen(16)` returns 0 with `ENOTSOCK`
   (behavioral: the net layer does not treat the new file as a socket);
   assert the reissued handle closes exactly once (`0`, then `-EBADF`).
   Pre-fix behavior fails this test on the fd counts and the stale-state
   assertions (verified by reading the old `resetState`, not by execution).
   fd assertions are skipped if `/proc/self/fd` is unavailable
   (non-Linux); on this Linux CI they run.
2. `testNetpollerUsableAfterReset` — after a reset, socket + `pollOpen`
   + `pollUnblock` + `pollClose` + `GoOs.close` round-trips (proves the
   reset did not wedge poller re-init).

## Verification (to run in the worktree once claimed)

- `~/workspace/system/bin/load-shed.sh backoff` first (CLEAR required).
- `make -C internal/backend/interop test` (builds truffle+runtime+interop,
  runs `IfaceTestRunner`; must exit 0 — includes the two new tests plus
  the existing `GoNetpollTest`/`GoOsErrnoTest` suites).
- `make -C internal/backend/interop check` (placeholder/debug gates).
- Confirm the new tests FAIL on the unpatched tree (stash the fix, run
  only the new test class if the runner allows — otherwise note as
  verified-by-reading) — red-green check before reporting done.

## Environment blocker log (2026-11-07, UTC)

- ~06:59 home writes fail `EDQUOT` (quota flapping, known pattern).
- ~07:03–07:06 shell writes succeed intermittently; `python3` writes
  fail `EPERM` consistently (incl. `/tmp`); `git` ref writes fail;
  `javac` class writes fail; `perl`/`node`/`dd`/C-probe writes succeed
  at the same instants — later evidence shows the failure is per-write
  flapping, not per-process: `git commit` on `/tmp` OK, `git branch`
  seconds later EPERM; a bash `printf` redirect fails 07:13 after
  succeeding 07:12. `strace` itself is blocked (EPERM).
- Consequence: `wt.py new` cannot save the registry (python `json.dump`
  write EPERM), `git worktree add` cannot write the ref `.lock`.
  Retry loop for the `wt.py` claim: 10/10 attempts failed identically
  (07:12–07:17 UTC), same `json.dump` EPERM. No registry entry was
  created (verified: 0 matches for rev1107-b04) and no worktree dir
  exists — nothing partial to clean up.
- Disk picture (du, 07:1x UTC): `~/workspace/worktrees` 146G apparent,
  972 registry entries; `~/workspace/turso` 30G; main repo 816M.
  Device has 38G free — this is a user quota at its limit, flapping
  as other agents write/delete, not a full disk. No worktree may be
  reaped without his word (standing "hold off" order) — the coordinator
  must decide the quota remedy; this lane reaps nothing.
- Workaround in progress (07:19 UTC): compile-check the staged patch
  in /tmp (`/tmp/stage-b04`, mirrored tree + `git apply` of B04.patch +
  `javac` of the 4 touched/added files against the existing
  `out/classes`). `muse.write`/`muse.edit` (agent tools) write
  reliably; the patch and test are staged here so no work is lost if
  the session dies. `git apply --check` against master `bf9b96ff`
  already PASSES (verified 07:1x UTC, main tree untouched).

## Status at handoff (final for this dispatch)

- BLOCKED on environment (write flap / quota): no worktree, no build,
  no test run yet. Fix + test are fully designed, written, and staged:
  `B04.patch` (verified to apply cleanly) + this file.
- Compile-check evidence (07:27 UTC): the new `GoOsResetTest.java`
  (byte-identical to the patch version, verified by diff) was run
  through `javac -Xlint:deprecation -Werror` against the existing
  built classes 11 times across 3 output filesystems
  (/tmp, /dev/shm, /var/tmp). Every run: ZERO source errors — the
  only error in each run is `error while writing GoOsResetTest:
  Operation not permitted` (the .class write). The test file
  typechecks cleanly under the project's own flags; the write path
  for multi-KB outputs is currently fully blocked (24/24 `cp`
  attempts also failed; only 1-block shell writes occasionally land).
  Production-side compile (`GoOs`/`GoNet`/`GoNetpoll` hunks) + the
  full `make test` run remain for the resume lane — the production
  change is 25 lines of simple Java, and `git apply --check` already
  passed.
- Resume path for the coordinator: when home writes are healthy again,
  `wt.py new graalgo rev1107-b04 --chat graalgo-independent-codebase-review`,
  then `git apply` the staged `B04.patch` in the worktree, then
  `make -C internal/backend/interop test` (+ `check`) after a backoff
  CLEAR. No re-design needed. Do NOT hand-merge or work in main.
- Main tree verified untouched: no rev1107-b04 worktree/branch, no
  index.lock left behind, no registry entry created.
