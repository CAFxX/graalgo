# DESIGN: `graalgo run` exit-code disambiguation (lane `fix-p1-run-exitcode`)

- **Status:** PROPOSAL — awaiting coordinator approval. DO NOT IMPLEMENT yet.
- **Date:** 2026-10-19
- **Source:** independent review P1, REVIEW-2026-10-19-03 scan, lane C finding #8
- **Scope:** `graalgo run` driver path only (`cmd/graalgo/main.go:graalgoRunProgram`
  + the two backend launcher mains it shells out to).

## 1. Problem

`graalgoRunProgram` (`cmd/graalgo/main.go`) applies the `go run` "exit status N"
treatment to **every** non-zero backend exit code. The backend's own failure
codes (1 = input/internal error, 2 = build failure) share the exit-code space
with program outcomes, so a backend failure is misreported as a program exit.

## 2. Ground truth: what `go run` actually does (empirical, go1.27.1, `~/tools/go`)

Probed 2026-10-19 (see §8 V2 for the probe programs). `go run`:

| Case | stderr | exit |
|---|---|---|
| program `os.Exit(1)` | `exit status 1` | 1 |
| program `os.Exit(2)` | `exit status 2` | 1 |
| program `os.Exit(3)` | `exit status 3` | 1 |
| uncaught panic | panic report, then `exit status 2` | 1 |
| fatal deadlock (`all goroutines are asleep`) | deadlock report, then `exit status 2` | 1 |
| build failure of the target | build errors only, **no** `exit status` line | 1 |
| child killed by signal (self-SIGKILL) | `signal: killed`, **no** `exit status` line | 1 |

Key corrections to the finding's framing:

1. The current driver is **already gc-exact** for program exits *including*
   panics and deadlocks: backend panic → exit 2 → driver prints `exit status 2`,
   rc 1 — exactly what `go run` does. The wart is **only** the backend's own
   failures (codes 1/2 arising before/during launch, not from the program).
2. There is a second, adjacent wart in the same function: a backend killed by a
   signal currently yields `exit status -1` (Go's `ExitError.ExitCode()` is -1
   for signal deaths); `go run` prints `signal: killed`.
3. `cmdRun`'s package path (`commands.go:108`) does a `--check` pre-pass and
   returns the backend's **raw** code — a build failure there exits 2, but
   `go run` exits 1.

## 3. Root cause

The backend JVM's exit code is the only driver←backend channel, and codes 1/2
are overloaded:

- `1` = backend input/internal error **or** program `os.Exit(1)`
- `2` = backend build failure **or** program `os.Exit(2)` **or** uncaught Go
  panic **or** fatal deadlock

(`os.Exit(N)` reaches the driver as the JVM's exit code via
`GoOs.java:1597` → `System.exit(num32(codeArg))`; panics/deadlocks return 2
from the launcher's `run()`.)

## 4. Design decision

**Out-of-band sentinel file.** The driver creates a private temp dir, passes
`--status-file=<path>` as an explicit argv flag (argv, never env — the PKG-001
precedent in `main.go`), and the backend writes a small status document **only
on its own failure paths**. File presence = "the backend itself failed";
absence = "the exit code/signal is the program's outcome".

Rejected alternatives:

- **Reserved exit-code band** (e.g. backend failures exit 125–127): collides
  with legal program codes — `os.Exit(125)` is valid Go. Any band breaks
  passthrough fidelity for some program. Rejected.
- **stderr marker parsing**: the program owns stderr; any marker is spoofable
  and parsing is fragile. Rejected.
- **Keeping the conflation and documenting it**: the finding is a real,
  user-visible divergence from the drop-in contract (`graalgo` must be
  indistinguishable from `go`). Rejected.

## 5. Proposed convention

### 5.1 Backend (both launcher mains)

New shared helper `graalgo.launcher.BackendStatus` in the **runtime** module
(both mains already use `graalgo.launcher.*`; interop→runtime is the existing
direction):

```java
public final class BackendStatus {
    public record RunResult(int exitCode, boolean backendFailure) {}
    public static final String FLAG_PREFIX = "--status-file=";
    public static String extractStatusFile(String[] args); // pre-pass, null if absent
    public static String[] stripStatusFile(String[] args); // args minus the flag
    public static void writeStatusFile(String path, int exitCode); // best-effort, never throws
}
```

`main()` becomes:

```java
public static void main(String[] args) {
    String statusFile = BackendStatus.extractStatusFile(args);
    RunResult r = runDetailed(BackendStatus.stripStatusFile(args));
    if (statusFile != null && r.exitCode() != 0 && r.backendFailure()) {
        BackendStatus.writeStatusFile(statusFile, r.exitCode());
    }
    System.exit(r.exitCode());
}
static int run(String[] args) { return runDetailed(args).exitCode(); } // unchanged seam
static RunResult runDetailed(String[] args) { /* existing body, classified returns */ }
```

- The pre-pass runs **before** the arg loop, so even usage errors during parsing
  still produce a sentinel.
- The flag is stripped before the main loop, so it is never mistaken for an
  unknown flag or an input file, and it never reaches the guest's `os.Args`
  (the guest only sees args after `--`).
- `run(String[]): int` keeps its exact signature: the in-JVM test seam and
  `FastCycleDaemon`'s reflective `run(String[])` lookup are unaffected.
- The flag is **optional**: direct invocations (`graalgo-run`,
  `graalgo-sync-run`, diffharness, batch mode) behave exactly as today —
  exit codes 0/1/2 unchanged, no sentinel. The differential corpus is
  unaffected (it drives the launchers directly and pins 0/1/2).
- Sentinel content: `{"backend_failure":true,"exit_code":N}`. **Existence is
  the signal**; content is diagnostic (driver parses best-effort). Write
  failures are swallowed (best-effort — §7 E5).

Classification (mechanical edit of each `return` site in `runDetailed`):

`P3SyncMain` (`internal/backend/interop/.../p3sync/launcher/P3SyncMain.java`):

- PROGRAM (no sentinel — driver keeps today's `exit status N` treatment, which
  is gc-exact): `sched.panicked()` → 2 (uncaught Go panic); Goexit-deadlock
  in main → 2 (gc: `exit status 2`, probed).
- BACKEND (sentinel): every other non-zero site — `usageError` (1), unreadable
  input (1), gofrontend failure (1/2, incl. build failure), gofrontend
  unlaunchable (1), `--emit-wire` write failure (1), `FrontendException` (1),
  `InterruptedException` (1), `--dump-polymorphism` write failure (1),
  `sched.crashed()` → 1 ("goroutine died from an internal error" —
  the backend's fault, not the program's).

`Main` (`internal/backend/runtime/.../graalgo/launcher/Main.java`, phase-1):

- PROGRAM: `PanicException` → 2; `Goexit` → 2; `StackOverflowError` and the
  SOE cascade → 2 (gc: runaway recursion → runtime abort, exit 2 →
  `exit status 2`); `catch (RuntimeException e)` → 1 — classified PROGRAM
  **deliberately**: it is the M2 stand-in for not-yet-mapped Go panics
  (e.g. divide-by-zero); keeping PROGRAM preserves today's `exit status 1`
  with no regression. The exit-1-vs-gc-2 fidelity gap there is a separate,
  pre-existing WS-F-mapping issue (noted as follow-up, §10).
- BACKEND: unreadable input (1), `FrontendBuildException` (1/2),
  gofrontend unlaunchable (1), `FrontendException` scope/internal (2/1),
  `--dump-polymorphism` write failure (1).
- Batch mode (`runBatch`): unchanged — process exit stays 0, per-entry JSON
  unchanged, no sentinel.

`SchedMain` (phase-2, `graalgo.sched.launcher`): legacy entry point, not invoked
by the driver (only by `tools/` scripts). Out of scope; noted §10.

### 5.2 Driver (`cmd/graalgo/main.go`, `commands.go`)

```go
type backendResult struct {
    code     int       // process exit code; -1 if it didn't exit
    sig      os.Signal // non-nil if killed by a signal
    startErr error     // non-nil if the backend couldn't be launched
}

func runBackend(root string, args []string) backendResult // single exec choke point
```

- `graalgoRun` (used by `--check`/`--emit-wire`/`vet`/`test` paths): code-only
  wrapper as today; signal death or start failure → 1 (downstream only tests
  `!= 0`; cleaner than today's -1 leak).
- `graalgoRunProgram`:
  1. `os.MkdirTemp("", "graalgo-run-*")` (0700); on failure, warn on stderr
     and fall back to legacy behavior (no flag) — degraded but safe.
  2. Prepend `--status-file=<dir>/backend-status.json`; `defer os.RemoveAll(dir)`.
  3. `r := runBackend(...)`; start error → `graalgo: <err>`, return 1 (unchanged).
  4. `r.sig != nil` → `signal: <name>` on stderr, return 1.
     (`syscall.SIGKILL.String()` = `"killed"` — byte-identical to `go run`;
     verified §2.)
  5. `r.code != 0` and sentinel file exists → return 1, **no** `exit status`
     line (backend's stderr already carries the message; matches `go run`).
     Sentinel honored only when `r.code != 0` (defensive).
  6. `r.code != 0` → `exit status <code>` on stderr, return 1 (unchanged,
     gc-exact for program exits incl. `os.Exit(1)`/`os.Exit(2)`/panic/deadlock).
  7. `r.code == 0` → return 0.
- `commands.go` `cmdRun` `--check` pre-pass (line ~108): on non-zero, `return 1`
  instead of leaking the raw backend code (build failure → 1, gc parity).

No bridges: no synthetic packages, no package-name-keyed cases, no import
overlays. The classification is by failure *kind* at its source (structural),
the flag is a generic driver↔backend channel (same class as
`--test-package-dir=`).

## 6. Exact code touch-points

| # | File | Change |
|---|---|---|
| 1 | `internal/backend/runtime/src/main/java/graalgo/launcher/BackendStatus.java` | NEW: `RunResult` record, flag pre-pass extract/strip, best-effort sentinel write |
| 2 | `internal/backend/runtime/src/main/java/graalgo/launcher/Main.java` | `main()` → pre-pass + `runDetailed` + sentinel; `run()` delegates; classify ~12 return sites in `runDetailed` |
| 3 | `internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3SyncMain.java` | same shape; classify ~20 return sites |
| 4 | `cmd/graalgo/main.go` | NEW `runBackend` (+`backendResult`); `graalgoRun` becomes wrapper; `graalgoRunProgram` implements §5.2 steps 1–7 |
| 5 | `cmd/graalgo/commands.go` | `cmdRun` `--check` pre-pass: non-zero → `return 1` |
| 6 | Javadoc on both mains' exit-code contracts | document `--status-file` (driver-internal, not in `--help`) |

Docs are part of the change (repo AGENTS.md §7): the exit-code contract lives
in the two mains' class Javadoc — update there, no new doc files needed.

## 7. Edge-case analysis

- **E1 — program `os.Exit(N)` for any N (incl. 1, 2, 125):** no reserved band
  exists; the guest's exit travels in-band via `System.exit`, the sentinel is
  written only on backend-failure paths. Passthrough is exact by construction.
  `os.Exit(0)` → 0, no line (unchanged).
- **E2 — backend crash (JVM signal death: SIGSEGV in compiled code, OOM-killer
  SIGKILL):** no sentinel can be written; driver prints `signal: <name>`,
  rc 1. Residual: indistinguishable from a program signal death — same as any
  process supervisor (and as `go run` itself); documented, not solved.
- **E3 — backend failure before flag parsing:** the pre-pass extracts the flag
  before the arg loop; the driver constructs the flag, so malformed-flag
  failure is impossible by construction. Defense in depth, not luck.
- **E4 — driver tempdir creation fails:** warn + legacy behavior (no flag).
  Observable behavior degrades to today's, never worse.
- **E5 — sentinel write fails (disk full):** swallowed; driver falls back to
  `exit status N`. Same degradation class as E4.
- **E6 — guest spoofing the sentinel:** the flag is stripped before `--` and
  never appears in the guest's `os.Args`; the file lives in a 0700 temp dir
  under a random name. A guest would have to guess the path — outside the
  threat model (the guest is the user's own program).
- **E7 — `--check`, batch mode, diffharness, direct launcher use:** flag
  absent → zero behavior change; launcher exit codes 0/1/2 pinned by the
  existing corpus stay green.
- **E8 — `graalgo build` artifacts:** artifacts embed wire JSON and run the
  backend without the driver; a build-time failure cannot occur at artifact
  runtime (frontend already ran). Artifact exit semantics unchanged; giving
  artifacts the same treatment is a follow-up (§10).
- **E9 — signal-name portability:** `syscall.WaitStatus` is unix-only; graalgo
  targets Linux/GraalVM (consistent with the rest of the backend). The
  differential test pins the Linux rendering.
- **E10 — concurrent `graalgo run`s:** fresh tempdir per invocation; no shared
  state.
- **E11 — sentinel present but backend exited 0:** impossible by construction
  (written only on non-zero backend-failure paths); driver ignores the
  sentinel when `code == 0` defensively.
- **E12 — `graalgo test` (`test.go:251`):** interprets raw backend codes
  itself; untouched by this design. Its own build-failure→exit-1 normalization
  (gc: `go test` exits 1 on build failure) is a follow-up (§10).

## 8. Verification plan

- **V1 — driver unit tests (fast, per-push):** new `cmd/graalgo/run_exit_test.go`
  with a **fake backend script** implementing the launcher contract
  (writes sentinel iff instructed; exits N; dies by signal on demand).
  Table-driven, each case asserting exact stderr bytes + exit code, each
  demonstrably capable of failing: backend-failure-1, backend-failure-2
  (→ silent rc 1), program-exit-{0,1,2,3,42} (→ `exit status N`, rc 1),
  panic-2-no-sentinel (→ `exit status 2`, rc 1), SIGKILL (→ `signal: killed`,
  rc 1), tempdir-uncreatable fallback (→ legacy behavior), sentinel-present-
  but-code-0 (→ ignored). No JVM needed.
- **V2 — differential vs `go run` (slower; nightly or explicit):** new
  `test/differential/run_exit_status.sh` comparing `graalgo run` against
  `go run` byte-for-byte (stderr + exit code) on: hello-ok, `os.Exit(1/2/3)`,
  panic, build-failure, missing input file, fatal deadlock, self-SIGKILL.
  Requires `make build` (real backend). This is the gc-parity pin for §2's table.
- **V3 — no corpus drift:** `make snapshot-check` must stay green — launcher
  exit codes are unchanged without the flag, and the driver is not in the
  corpus loop (corpus drives `graalgo-run`/`graalgo-sync-run` directly;
  `test/corpus/wave2/os/run_differential.sh` compares against gc-built
  *binaries*, and only pins launcher codes 0/1/2).
- **V4 — `make check`:** grep/format gates cover the new Java and Go code.
- **V5 — process gates:** independent reviewer before merge (standing order);
  no new bridges (this design adds none — verified against
  `docs/arch/bridge-inventory.md`'s grandfather list: nothing new to inventory).

## 9. Behavior-change summary (the only observable deltas)

| Case | Before | After (gc) |
|---|---|---|
| `graalgo run` backend input error (1) | `exit status 1`, rc 1 | backend message only, rc 1 |
| `graalgo run` backend build failure (2) | `exit status 2`, rc 1 | build log only, rc 1 |
| `graalgo run` backend signal death | `exit status -1`, rc 1 | `signal: <name>`, rc 1 |
| `graalgo run <pkg>` build failure (pre-pass) | rc 2 | rc 1 |
| program `os.Exit(N)` / panic / deadlock | unchanged | unchanged (already gc-exact) |
| launcher direct use / corpus / `--check` | unchanged | unchanged |

## 10. Out of scope / follow-ups (not this lane)

1. Phase-1 `catch (RuntimeException)` exit-1-vs-gc-2 fidelity (needs WS F
   panic mapping completed; tracked as a separate correctness gap).
2. `graalgo test` build-failure exit-code normalization (`test.go:251`).
3. `graalgo build` artifacts: same sentinel treatment at artifact runtime.
4. `SchedMain` (phase-2 legacy launcher): retire or align; driver never
   invokes it.

## 11. Approval gate

STOP. Awaiting coordinator approval of this design before any implementation.
On approval, the implementation lane: works in this worktree, implements §6,
runs V1–V4, then requests independent review before integration (standing order).
