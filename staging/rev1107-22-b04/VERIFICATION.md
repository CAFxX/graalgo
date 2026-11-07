# Lane rev1107-22-b04 — B-04 verification record (staged, quota-blocked)

Issue: P1 B-04 (REVIEW-2026-11-07-07, carried by REVIEW-2026-11-07-22):
`GoOs.resetState` drops handles without closing; net state survives cookie reset
(aliasing + fd leak). Live in the plain launcher's batch path since `59f0a247`
(`Main.runOne` calls `GoOs.init` per `--batch` entry).

Status: **BLOCKED-quota. Fix verified RED→GREEN in /tmp scratch; no worktree,
no registry entry, no commit — home quota EDQUOT-exhausted (see §1).**
Head verified against: `59f0a247` (main tree untouched, `git status` clean).

## 1. Environment — quota gate (failed, as briefed)

- Real 8 KB home write probe (`~/workspace/.quota-probe-rev1107-22-b04`):
  `PROBE_FAIL [Errno 122] Disk quota exceeded` at ~22:14 CET.
- `/tmp` 94% full (33M free; sibling review-22 lanes' scratch, not mine —
  my lane uses 3.6M; nothing of theirs deleted). `/var/tmp` 100% full.
  `/dev/shm` 92% full (72M free) — used for the full test-classes build.
- Per the brief: no worktree claimed, no registry entry written, no commit.
  All work staged under `/tmp/rev1107-22-b04/` (see §7).

## 2. Prior-lane verification (recovery, not duplication)

Registry `~/workspace/system/work-registry/rev1107-b04.json`: status
`resuming`, heartbeat `2026-11-07T12:29:08Z` (~9.7h stale at lane start).
Branch `wt/graalgo-independent-codebase-review/rev1107-b04` exists at base
`bf9b96ff` with no commits. Worktree path
`~/workspace/worktrees/graalgo/rev1107-b04` does NOT exist. No `b04`
process alive (`ps` clean). **Prior lane is dead — proceeded.**
But it left staged work, adopted (not duplicated): `B04.patch` (12,532 bytes)
+ `DESIGN.md`, copied byte-identical (`cmp` clean) to this staging dir.
Its checkpoint: fix+test staged, test file typechecked clean (11/11 javac
runs, only .class write blocked); full `make test` deferred to resume.

## 3. Mechanism re-verified from disk truth at HEAD 59f0a247

- `GoOs.resetState` (`internal/backend/interop/src/main/java/graalgo/p3os/GoOs.java:378-380`):
  still `HANDLES.clear(); NEXT_COOKIE.set(16);` — unchanged since the finding.
- `GoNet.STATES` (`.../p3net/GoNet.java:171`) cleared only by
  `GoNet.resetForTest` (:172-177); `GoNetpoll` `DESCS`/`BY_SOCK`/`REG_Q`/
  `NEXT_CTX`/selector (`GoNetpoll.java:190-196`) cleared only by
  `GoNetpoll.resetForTest` (:199-218). Neither is called by `GoOs.init`.
- Aliasing sites confirmed: `GoNetpoll.java:428,498` (`BY_SOCK.get(sockCookie)`);
  `GoNet.sockState` consulted at `GoNet.java:439,474,514,541,672,696,743,775,889,921`.
- Batch path confirmed live: `Main.java:247-249` (`runOne` → `initializeOsSurface`
  per entry), `Main.java:412,450-452` (`runBatch` → `runOne` per entry);
  `P3SyncMain.java:515` (`GoOs.init` once per invocation).
- `git apply --check B04.patch` in the main repo: **exit 0** (clean against
  `59f0a247`; B-02 touched only the launcher, not these files).

## 4. Patch review (independent re-derivation, not blind adoption)

Adopted `B04.patch` as-is after verifying each of its claims:

- `GoOs.close(Object)` (:755-779): unknown cookie → `-EBADF` (no throw);
  `KIND_SOCKET` → `GoNetpoll.evictForClose` first, then `handle.close()`;
  only `IOException` caught internally. The patch's `catch (RuntimeException)`
  around each reset close covers the residual unchecked paths (e.g. a
  `DirHandle.close()` NPE if its stream were null — constructor guarantees
  non-null, so defense-in-depth only).
- `ChannelHandle.close()` is a no-op for stdio (`closeUnderlying=false`);
  the patch skips cookies 1–3 anyway and they are re-registered below.
  `DirHandle.close()` closes its `DirectoryStream`; pipe/socket handles
  (`KIND_PIPE`, `KIND_SOCKET`, `closeUnderlying=true`) are really closed.
- `GoNet.STATES` is the only mutable per-run state in `GoNet`
  (all other statics are constants or pure helpers). `GoNetpoll.resetForTest`
  clears `DESCS`, `BY_SOCK`, `REG_Q`, `NEXT_CTX`, selector, poller thread,
  `lastPollerError`. Order in the patch (handles → net resets → cookie space)
  is correct: socket closes evict their descs first; the net resets clear
  stragglers (e.g. `SockState` for an already-closed handle) and tear down
  the selector/poller thread.
- Class-load safety: `GoOs`'s `static { resetState(...) }` (:285) runs with an
  empty handle table (loop no-ops); `GoNet`/`GoNetpoll` have no `static {}`
  blocks and no `GoOs` reference in field initializers — calling their
  `resetForTest()` from `GoOs.resetState` cannot deadlock on class init.
- No locking hazard: `GoNetpoll.resetForTest` is `synchronized` (class lock)
  but is called after, not during, the close loop; `evictForClose` locks only
  the `PollDesc`, never the class. No caller holds the class lock across
  `GoOs.init`.
- No test relies on handles surviving `init`: every `GoOs.init` in
  `GoOsErrnoTest` is the first statement of its test method; `GoNetpollTest`
  setup/teardown already call all three resets by hand (idempotent after fix).
- Test-file API check: `GoNet.socket(AF_INET=2, SOCK_STREAM=1, 0)`,
  `GoNetpoll.pollOpen/pollOpenErrno/pollUnblock/pollClose/descForTest`,
  `GoNet.sockState` (package-private, test is in `graalgo.p3net` — reachable),
  `GoOs.open/close`, `EBADF=9`, `GoString.fromUtf8` — all exist with the
  signatures the test uses. `pollOpen` on a non-socket cookie returns 0 with
  `ENOTSOCK` via `GoNet.selectableChannel` → `GoOs.socketChannel` (null unless
  `KIND_SOCKET`) — the test's behavioral assertion is sound.
- `IfaceTestRunner` invokes every public no-arg `test*` method by reflection;
  both new test methods qualify.

Known limitations (pre-existing, NOT introduced by the patch — no change made):
- `GoNetpoll.resetForTest` does not clear the `LAST_OPEN_ERRNO` ThreadLocal.
  Benign: the value is only meaningful immediately after a `pollOpen`.
- A `pollOpen` racing a concurrent reset can still strand a registration task
  (pre-existing race in `resetForTest`; production never resets concurrently).
- `resetForTest` keeps its test-hook name although it is now also the
  production reset entry point; the patch updates the javadocs instead of
  renaming (renaming would churn test call sites). A reviewer may ask for the
  rename; it is cosmetic.

## 5. RED → GREEN (this lane, in /tmp scratch)

Toolchain: `~/tools/graalvm-jdk-25/bin/{javac,java}`, flags
`-Xlint:deprecation -Werror` (project flags). All commands under `nice -n 10`;
backoff CLEAR before the compile/run steps (one exception: see §6).

- Scratch mirror: `/tmp/rev1107-22-b04/scratch/root/` (repo-root-relative copy
  of `internal/backend/interop/src` at `59f0a247`); `git apply B04.patch`
  there: clean. (GNU `patch -p5` failed hunk #1 on context matching — a
  `patch(1)` quirk, not a patch defect; `git apply --check` in main + `git
  apply` in scratch both pass.)
- RED (unpatched home `out/classes` + new test, standalone runner):
  `testResetClosesHandlesAndClearsNetState` **FAIL** —
  `fds referencing the temp file after open: want 1 got 2` (the warm-up
  handle leaked by the unpatched reset — exactly the reported fd leak);
  `testNetpollerUsableAfterReset` PASS (expected — usability, not a leak check).
  Log: `evidence/red-run.log`.
- GREEN (patched `GoOs`/`GoNet`/`GoNetpoll` compiled to
  `scratch/green-classes`, test to `scratch/green-test-classes`):
  compile exit 0 under `-Werror`; both tests **PASS**.
  Log: `evidence/green-run.log` (`evidence/green-main-compile.log`,
  `green-test-compile.log` are empty = zero warnings/errors).
- Full interop suite (`IfaceTestRunner`, patched test set incl. the new test):
  GREEN **133 PASS / 59 FAIL**; RED baseline (unpatched classes + unpatched
  test set, same harness) **131 PASS / 59 FAIL**. The +2 is exactly the two
  new tests (both PASS inside the full suite too). The 59 failures are
  IDENTICAL by test name, and identical in reason modulo random temp-dir
  suffixes in the quota messages: `Disk quota exceeded` writing to
  `/home/hatch/junit-scratch` (GoOsErrnoTest file tests), `ClassCastException
  [I cannot be cast to [B` (stale prebuilt truffle/runtime classes vs HEAD
  sources — same staleness class as below), missing `gofrontend` binary on
  PATH, unregistered linkname `runtime_pollServerInit` (harness lacks the
  launcher init `make test` provides). **Zero regressions from the patch.**
  Logs: `evidence/rev1107-22-b04-fullsuite-{green,red}.log`,
  `evidence/{green,red}-fail-names.txt`.
- NOT run here (deferred to the resume lane in a worktree): `make test`
  proper (needs home writes + a full rebuild), `make check`, `make
  snapshot-check` (behavior-change gate per repo AGENTS.md §3 — the fix only
  closes previously-leaked handles and clears previously-stale state, but the
  gate should still be run).

## 6. Pre-existing tree state found (for the resume lane / coordinator)

- Prebuilt `runtime/out/classes` is stale vs HEAD `59f0a247`:
  `runtime/src/.../graalgo/launcher/OsSurfaceInitializer.java` exists (B-02)
  but no `OsSurfaceInitializer.class` in `out/classes`. A full-module
  `javac` of interop main sources therefore fails on
  `GoOsSurfaceInitializer` — `make` rebuilds the runtime first via the
  `.built` dependency, so this bites only ad-hoc compiles. Same staleness
  explains the full-suite `ClassCastException`s in this harness.
- **Process note (my error):** the RED baseline full-suite run proceeded
  despite `load-shed.sh backoff` returning BACKOFF (1-min load 13.74 ≥ 12)
  — the command chain did not gate on the exit code. Short run (~33s), no
  harm observed, but the gate was not honored that once; the resume lane
  must gate properly.

## 7. Staged contents (`/tmp/rev1107-22-b04/`)

- `B04.patch` — the fix + test (12,532 bytes, byte-identical to the prior
  lane's staged patch; `git apply --check` clean at `59f0a247`).
- `DESIGN.md` — prior lane's design/verification notes (byte-identical copy).
- `VERIFICATION.md` — this file.
- `RESUME.md` — exact resume steps.
- `evidence/` — `red-run.log`, `green-run.log`, `green-main-compile.log`,
  `green-test-compile.log`, `rev1107-22-b04-fullsuite-{green,red}.log`,
  `{green,red}-fail-names.txt`.
- `scratch/` — patched mirror (`root/`), `green-classes/`,
  `green-test-classes/` (partial, from the /tmp-ENOSPC attempt — superseded
  by `/dev/shm/rev1107-22-b04-test-classes`, see below), `red-test-classes/`,
  `runner-classes/`, `RunResetTest.java`, `red-run.log`, `green-run.log`,
  `green-main-compile.log`, `green-test-compile.log`.
- Ephemeral (NOT in /tmp staging; will vanish on reboot):
  `/dev/shm/rev1107-22-b04-test-classes/` (full patched test-classes, 328K),
  `/dev/shm/rev1107-22-b04-red-test-classes/`,
  `/dev/shm/rev1107-22-b04-fullsuite-{green,red}.log` (copies in `evidence/`).
