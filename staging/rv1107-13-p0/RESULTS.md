# RV1107-13-P0 — `graalgo build` artifacts cannot run (NEW-P0-1): RESULTS

- Lane: `rv1107-13-p0-wireinput`, chat `f7467b85-7da6-4da0-b9ec-94631a60dafe`
- Base: master `bf9b96ff5439db50024b78e9aae30310183e2748` (verified 2026-11-07)
- Source finding: independent review of rv1107-13,
  `/var/tmp/rv1107-13-review/REVIEW-2026-11-07-rv1107-13-49242715.md` (§7, VERDICT CLEAN on tip 49242715)
- Status: FIX DEVELOPED + VERIFIED IN SCRATCH. NOTHING COMMITTED — home disk quota
  EDQUOT-closed throughout (see §6). A self-gating landing runner is ARMED (see §7);
  it lands the patch when quota opens. Do NOT integrate. Do NOT push (runner does neither).

## 1. Verdict on the reviewer's proposal — INCOMPLETE, verified from disk

The reviewer proposed a one-line fix (emit `--wire-input=<file>`) plus a real-backend
test. The one line is necessary but NOT sufficient. Dynamically reproducing the full
path showed the wire-input execution path was NEVER exercised end to end (04c62c56
added it untested), and it carries FOUR compounding breaks on the single path from
launcher to running program — all from 04c62c56, all pre-existing, all part of this
one finding ("artifacts cannot run"):

1. **Arg form (Go, `internal/driver/artifact.go:191`).** Launcher emits the space form
   `--wire-input <file>`; the backend accepts ONLY the equals form
   (`P3SyncMain.java:180`, `a.startsWith("--wire-input=")`; a bare `--wire-input` token
   falls through to the `unknown flag` rejection). Reproduced: tip-built artifact exits 1
   in 0.9s with `graalgo-sync-run: unknown flag: --wire-input`.
2. **argv[0] routing (Java, `P3SyncMain.java` arg loop).** The launcher passes its argv[0]
   after `--`; the loop routes the first post-`--` token into `files` when no file was
   given, tripping `--wire-input cannot be combined with input files`. Reproduced via
   the real backend (Go red run) and via the Java test (red run).
3. **Zero Sources (Java, `P3SyncMain.java:276` + `NodeFactory.java:325-326`).** In
   wire-input mode `paths` is empty, so `new NodeFactory(sources)` throws
   `IllegalArgumentException: NodeFactory requires at least one Source`. Reproduced
   dynamically: `graalgo-sync-run --wire-input=<file>` (no argv) → uncaught exception,
   exit 1, stack trace naming `NodeFactory.<init>` / `P3SyncMain.run:290`.
4. **argv[0] dereference (Java, `P3SyncMain.java:511`).** `goArgv[0]` reads
   `files.get(0)` unconditionally — on the empty list in wire-input mode that is an
   `IndexOutOfBoundsException` (verified from source; unreachable until breaks 2–3 are
   fixed, so no separate dynamic repro — the fix removes the dereference for wire mode).

Provenance: `git log -S '--wire-input'` → both sides introduced together in `04c62c56`
("fix-render-launcher: replace bash artifact with real binary embedding frontend JSON").
`TestEmitBinaryRuns` uses a fake backend script and cannot catch any of this (it only
verifies argv shape against the fake).

## 2. The fix (5 files, +340/−28, no bridges, no special cases, no other behavior)

- `internal/driver/artifact.go` (1 line): emit `"--wire-input=" + tmpName`.
  Grep over the whole tree (`git grep --wire-input`) shows this is the ONLY producer of
  the space form; no other caller/consumer depends on it. The only other launcher-side
  flag, `--emit-wire=` in `cmd/graalgo/commands.go:227`, already uses the equals form.
- `internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3SyncMain.java`
  (4 edits, all wire-input-scoped; normal file mode untouched):
  a. `programArgv0` recorded; in the `--` branch, wire-input mode routes the first
     post-`--` token to it and ALL later tokens to `goArgs` (never to `files` — the
     first version of this fix misrouted the second user arg into `files`; the argv
     test below caught it before it shipped).
  b. Wire-input mode supplies one empty synthetic Truffle Source (`"<wire-input>"`):
     positions resolve to it and clamp to a zero-length section via `sectionFor`'s
     existing out-of-range path, so execution is unaffected; only reported positions
     lose file text (the artifact embeds no sources by design). No Phase-1 NodeFactory
     change.
  c. `goArgv[0]` comes from `programArgv0` in wire-input mode (fallback: the wire-input
     file, so `os.Args[0]` stays absolute when no `--` is given); the `files.get(0)`
     path is unchanged for normal mode.
- `internal/driver/artifact_test.go`: fake-backend test updated to the equals form
  (argv indices shift by one; `cp "${1#--wire-input=}"`); `TestEmitProducesBinary` now
  asserts the binary contains `--wire-input=`; NEW `TestEmitBinaryRunsRealBackend` —
  real artifact, real backend (`graalgo-sync-run --emit-wire` exactly as `graalgo build`
  does, then `Emit`, then execute), asserting exit 0, empty stdout, stderr contains
  `hello real-backend`. Program is deliberately minimal (builtin `println`, no imports):
  wire JSON ~3KB, so the test runs in ~3s; a fmt program pulls ~113MB of wire JSON and
  is covered by the differential suite. Skips in `-short` mode and when the backend /
  classes / gofrontend / java are missing (verified both skip paths).
- `internal/backend/interop/src/test/java/graalgo/p3sync/launcher/WireInputLauncherTest.java`
  (NEW): two in-JVM tests calling `P3SyncMain.run` directly (repo's launcher-test
  pattern): `testWireInputRunsProgram` (exit 0 + output) and `testWireInputForwardsArgv`
  (os.Args == [fake argv0, hello, world] exactly).
- `internal/backend/interop/src/test/java/graalgo/iface/IfaceTestRunner.java`: register
  the new test class (one line + comment).

Patch artifact: `patch.diff` (unified diff vs master, repo-relative a/ b/ headers) +
`patch/` (the 5 files). Verified: `git apply --check` + apply to a fresh master extract
reproduces `patch/` byte-identically (`apply-test/` kept as evidence).

## 3. Red/green evidence (all in scratch; GOCACHE/GOPATH/TMPDIR under /var/tmp + /tmp)

Go driver suite — RED (master code + new tests, real backend at
`GRAALGO_TEST_REAL_BACKEND_ROOT=$HOME/workspace/graalgo`):
`go test ./internal/driver/ -count=1 -vet=off -v` → FAIL, 3.1s:
- `TestEmitProducesBinary` FAIL: "artifact does not use --wire-input= (equals form)"
- `TestEmitBinaryRuns` FAIL: `args[0] = "--wire-input"`, wire copy missing, indices shifted
- `TestEmitBinaryRunsRealBackend` FAIL: "artifact run failed: exit status 1 / stderr:
  graalgo-sync-run: unknown flag: --wire-input" (+ usage text)
- other 4 tests PASS.

Go driver suite — GREEN (patched code + patched backend classes first on the
classpath via scratch root `/tmp/rv1107-13-p0-tmp/graalgo-root`): 7/7 PASS, 4.4s
(`TestEmitBinaryRunsRealBackend` 3.42s).

Java launcher tests — RED (unpatched backend classes): 0/2, both FAIL with
"--wire-input cannot be combined with input files".
Java launcher tests — GREEN (patched `P3SyncMain` first on the classpath): 2/2 PASS,
9.9s. (Intermediate: the first arg-loop fix passed 1/2 and the argv test caught the
second-arg misroute; fixed and re-verified.)

Regression checks (patched backend): normal file-mode execution
`graalgo-sync-run hello.go` (println program) → exit 0, prints "hello real-backend",
2.1s. `go vet ./internal/driver/` clean. `gofmt -l`: only `artifact.go` listed, with
the IDENTICAL pre-existing hunk as master (backtick-concatenation comment ~line 186;
the lane's changed lines are gofmt-clean; `artifact_test.go` clean).

NOT re-verified at CLI level in scratch: a full `graalgo build` binary rebuild was not
done (would need a larger export + rebuild). It adds nothing: `driver.Emit` is the
single artifact-production path (only caller `cmd/graalgo/commands.go:242`, verified),
`--emit-wire` capture is unchanged and was proven working by the rv1107-13 review
(§3b), and the Go test exercises emit-wire → Emit → artifact run end to end. The
landing runner re-verifies in the worktree (see §7).

## 4. What the reviewer asked the lane to determine (first-run execution: slow or hung?)

Neither — for the fmt hello it is KILLED. Direct backend run of the fmt hello
(`graalgo-sync-run hello.go`, TMPDIR on /tmp): SIGKILL, exit 137 ("Killed") at 53s,
after the `--check` (build-only) variant of the same program succeeds in 19s. dmesg
is unreadable from the sandbox, so the killer is not directly observable; the box
showed real memory pressure at the time (MemFree 530MB of 7.9GB, swap 432MB used, a
sibling lane's gofrontend holding ~1GB RSS, two frozen javacs). Most likely the kernel
OOM killer under memory pressure — NOT a graalgo hang (the same program's build phase
completes, and the minimal program executes in 1.5s). The fmt program's wire JSON is
112,792,259 bytes (~108MiB); the minimal program's is 3,352 bytes. No graalgo change
follows from this; it is an environment-capacity note for the coordinator.

## 5. Environment incidents (methodology, for the coordinator)

- `/var/tmp` hit 100% mid-verification (184KB free). First symptom: `--emit-wire` wrote
  a 56,238,080-byte TRUNCATED wire file (true size 112,792,259) and the backend then
  failed with "JSON parse error at line 1516426, col 30" — a disk-full artifact, NOT a
  frontend/backend bug. A direct gofrontend run redirected to /var/tmp also produced a
  0-byte file the same way. Resolved by moving JVM/tmp work to /tmp (140MB free at the
  time) and deleting only my own scratch files. Lesson restated: on this box, a
  truncated-output failure must be checked against tmpfs free space before being
  believed. No other lane's scratch was touched.
- Home quota EDQUOT-closed for the whole run (no home writes made; see §6).

## 6. Home writes: NONE. Registry heartbeat: FAILED (quota), logged.

- No branch, worktree, commit, registry entry, or file was written to home.
- `workreg.py heartbeat --id rv1107-13-p0-wireinput` was not attempted as a write
  (registry lives on home; it would fail with EDQUOT like every other home write
  this run). The landing runner registers + heartbeats once quota opens.
- Daily-log entry for 2026-11-07 is staged at `daily-log-2026-11-07.md` in this dir;
  the runner appends it to `~/memory/2026-11-07.md` at landing.

## 7. Landing runner (ARMED, waiting on quota)

- Script: `/var/tmp/rv1107-13-p0/landing-runner.sh` (PID recorded in `landing.pid`
  once launched; progress in `landing.log`).
- Behavior: loops every 120s on a REAL home-quota probe (write 64KB unique content to
  home, verify byte count, remove). When the probe passes: claims worktree
  (`wt.py new graalgo rv1107-13-p0-wireinput --chat f7467b85-7da6-4da0-b9ec-94631a60dafe`,
  `--recover` fallback), `git apply patch.diff`, re-runs the full verification in the
  worktree (Go suite with real backend after `make generate build`; Java compile +
  wire-input tests; normal-path regression; gofmt + go vet), commits with explicit
  pathspecs (5 files), writes `TIP_SHA` + landing section of RESULTS.md here AND to
  `~/workspace/goals/graalgo-go-toolchain-on-graalvm-truffle/hidden_files/rv1107-13-p0-wireinput/`
  if writable, appends the staged daily-log entry, registers + heartbeats the registry.
  It does NOT integrate and does NOT push. On any mid-landing failure it records state
  and exits (resume is a fresh runner invocation, never a blind retry of a half-done step).

## 8. Scope / bridges

Exactly this finding (artifacts cannot run). No new `synth_*.go`, no import overlays, no
package-name-keyed conditionals, no Java reimplementations of stdlib behavior, no other
behavior changes. The Java edits are general mechanisms on the launcher/backend
contract (wire-input argv + Source provisioning), not special cases for any program.
Normal file mode (`graalgo run`, diffharness, `--check`) is byte-identical in behavior
by construction (all new branches are gated on `wireInput != null`) and was regression-
checked above.

## 9. Residual risks (for the coordinator)

- The patch, runner, and this record live ONLY in `/var/tmp` (tmpfs) plus the lane's
  final report — a VM reboot wipes them (reboot #88 class). Home quota blocks any
  persistent copy right now. If the VM reboots before landing, re-dispatch from the
  final report (which carries the full patch text).
- If master moves past `bf9b96ff` before landing, `git apply` may fail on context
  drift (especially `P3SyncMain.java`); the runner then fails loudly with state
  recorded — it does NOT adapt the patch. That needs a re-dispatch with re-verification.
- The runner waits indefinitely on the quota probe (120s cycle, logged). It is not in
  the work registry yet (registry is a home write); a reboot before quota opens loses
  the runner itself — re-arm from this record.
