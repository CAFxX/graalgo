# M10 (WS J) Self-Attack Matrix — P3-M10 closeout

Lane: `m10-self-attack` | Base: master @ `855f8b03` | Date: 2026-10-13
Scope (PHASES.md §5 + phases/phase3/PLAN.md §4): fault injection (unpaired
ioEnter, import-cycle attempts, build-tag mismatches, linkname-missing
symbols, truncated/corrupt frontend JSON, OOM during parse), signal storms
(SIGINT/SIGTERM/SIGHUP), fd exhaustion. Every finding → fix instance, hunt
error class, patch process (regression test or gate).

## Attack table

### F — Fault injection

| ID | Attack | Target | Expectation (fail-fast, clean) | Result |
|----|--------|--------|--------------------------------|--------|
| F1 | 200k-deep nested JSON arrays | `Json.parse` (phase1 frontend) | `FrontendException` (named), no SOE | **FINDING → FIXED**: raw `StackOverflowError` + 1029-line trace (e2e). Fix: `MAX_DEPTH=1000` cap in `Json.Parser` → named `FrontendException`. Regression: `FrontendRobustnessTest.testDeepNestingFailsFast`. Legit docs ≤46 deep (measured). |
| F2 | Truncated JSON (cut at 8 offsets) | `NodeFactory.buildProgram` | `FrontendException`, exit 1, no trace | PASS: all 8 cuts → path-aware `FrontendException` in ≤2ms. (Cut at len-1 = trailing `\n`: valid doc, correct.) Regression: `testCorruptJsonFailsFast`. |
| F3 | Corrupt JSON (garbage, wrong types, dup keys) | `NodeFactory.buildProgram` | `FrontendException`, no crash/hang | PASS: 14 variants → named `FrontendException`. Dup keys take last (Go parity). Regression: `testCorruptJsonFailsFast`. |
| F4 | OOM during parse (`-Xmx64m`, 300MB doc) | `Json.parse` via launcher path | `OutOfMemoryError` (Java error), no native crash/hang | PASS: clean `OutOfMemoryError: Java heap space` in `readAllBytes`, exit 1, no hs_err, 16s. (OOM is the defined clean failure; not preventable at parser level.) |
| F5 | Re-entrant `ioEnter`; `ioExit` w/o `ioEnter` | `SchedulerCore` (phase2) | `IllegalStateException` named | PASS: covered by `IoSlotsTest` (re-run 9/9 green). |
| F6 | `ioExit`/`ioEnter` from non-goroutine thread | `SchedulerCore.requireHandle` | `IllegalStateException` named | PASS (was untested): "not a scheduler goroutine". Regression: `IoSlotsTest.testIoEnterExitFromForeignThreadFails` (9/9 green). |
| F7 | Import cycles: a↔b, self-import, a→b→c→a, diamond+cycle | p3frontend loader/importer | `p3frontend: import cycle: …` fail-fast, timeout-guarded | see F7 findings below |
| F8 | Build-tag mismatch / malformed constraint | p3frontend loader tag filter | file excluded or clean error, no crash | see F8 findings below |
| F9 | `//go:linkname` to nonexistent symbol, then call it | `Linknames.dispatch` | `FrontendException` "unknown symbol" at dispatch | PASS: `linkname: no intrinsic registered for symbol "…"`, exit 1. (Dispatch-time fail-fast is the design; build-time check out of scope.) |
| F10 | Malformed `//go:linkname` directive | exporter `linknameDirective` | scope error at build, exit 2 | PASS: all 3 malformed forms → gofrontend scope error, exit 2. |
| F11 | gofrontend killed mid-run (SIGKILL) | `Main.runGofrontend` | `FrontendBuildException`, exit 1/2, no hang | **FINDING → FIXED**: exit 1 but *silent* (empty build log printed nothing). Fix: all 3 launchers now print `gofrontend exited <code> with no output` when the log is empty. |
| F12 | Fake gofrontend: truncated JSON + exit 0 | `Main.run` end-to-end | clean error exit, no JVM trace | PASS: `graalgo-run: internal error building AST: JSON parse error …`, exit 1. |

### S — Signal storms

| ID | Attack | Expectation | Result |
|----|--------|-------------|--------|
| S1 | SIGINT during execution | JVM exits 130, no zombies, no state corruption | PASS: executing JVM (CPU advancing) dies on SIGINT; 0 zombies. (First attempt invalid — 3s sleeper finished before the signal; redo with infinite sleeper + pgrep PID capture.) |
| S2 | SIGTERM during execution | JVM exits 143, no zombies | PASS: exit=143, zombies=0. |
| S3 | SIGHUP during execution | JVM exits 129, no zombies | PASS: exit=129, zombies=0. |
| S4 | Storm: 30× mixed SIGINT/SIGTERM/SIGHUP in 1s during sleep | single death, no zombies, no partial state | PASS: exit=129, zombies=0. |
| S5 | Storm during `--dump-polymorphism=path` final write | no partial/corrupt snapshot file | **FINDING → FIXED**: `Files.writeString` is not atomic — a kill mid-write leaves a truncated snapshot. Fix: write temp + `ATOMIC_MOVE`. Existing `testDumpPolymorphism` exercises the path. Sibling `PinAudit` writeString reviewed: diagnostic archive also on stdout, no mid-run consumer — left as-is (documented). |
| S6 | SIGTERM during gofrontend phase (child alive) | child reparented+reaped, no zombie | |

### D — FD exhaustion

| ID | Attack | Expectation | Result |
|----|--------|-------------|--------|
| D1 | `ulimit -n 32`, run hello.go | clean JVM or launcher error, no hang | PASS: exit 0, "hello fd" printed. JVM boots and runs with 32 fds. |
| D2 | `ulimit -n 64`, run hello.go | runs or clean error | PASS: exit 0. |
| D3 | Program opening 400 files under `ulimit -n 256` | Go-level errors, no hang/crash | **3 FINDINGS → ALL FIXED**: (1) `GoOs.errno()` mapped fd-exhaustion to `EIO` not `EMFILE` — fixed with `EMFILE=24`/`ENFILE=23` + message mapping (note: `FileChannel.open` throws `FileSystemException`, not `IOException` — the check lives in both branches); (2) lazy class loading died with `NoClassDefFoundError` under exhaustion — fixed by `ClassPreloader` (eager `graalgo.*` load at launcher startup, no-init); (3) crash reported as misleading "internal error" — fixed with honest "fatal fd exhaustion … (too many open files)" via fd probe. **Result**: exit 0, 158/158 errors are `EMFILE` (`errors.Is` verified), gc-identical message, gc parity (gc: 149 EMFILE, same message; counts differ only in JVM fd headroom). Regression: `GoOsErrnoTest.testErrnoTooManyOpenFiles` (both exception branches). |
| D4 | `ulimit -n 32` with `--check` (frontend-only) | clean error, no hang | PASS: exit 0. |

## Findings log

### FINDING-1 (F1): `Json.parse` StackOverflowError on deeply-nested input
- **Instance**: 200k-deep `[` nesting → raw `java.lang.StackOverflowError` + 1029-line trace, exit 1 (e2e via `GRAALGO_GOFRONTEND`).
- **Root cause**: recursive-descent `Parser.value/object/array` with no depth bound.
- **Fix**: `MAX_DEPTH=1000` in `Json.Parser`; exceed → `FrontendException("nesting too deep (max 1000)")`. Legit docs measured ≤46 deep (corpus + 2000-term chains + 500 parens).
- **Error-class hunt**: `GoDebug`'s private `Json` only parses the trusted classpath registry (GODEBUG env is `k=v`, not JSON) — not the same class. No other recursive parsers on untrusted input.
- **Process patch**: regression test `FrontendRobustnessTest.testDeepNestingFailsFast` (+ `testCorruptJsonFailsFast` battery for F2/F3).

### FINDING-2 (F11): silent exit-1 when gofrontend is killed
- **Instance**: fake gofrontend SIGKILLed mid-run → `FrontendBuildException(137, "")` → `System.err.print("")` → exit 1 with zero output.
- **Fix**: all three launchers (`Main`, `SchedMain`, `P3SyncMain`) now print `gofrontend exited <code> with no output` when the build log is empty.
- **Error-class hunt**: the pattern was triplicated across the three launchers — all three fixed. The sibling `readAll` swallowing `IOException` → empty bytes is intentional (partial output + nonzero exit still fails fast via the exit-code check).
- **Process patch**: covered by re-running the F11 attack (needs rebuilt classes — pending thaw).

### FINDING-3 (S5): non-atomic `--dump-polymorphism` snapshot write
- **Instance**: `Files.writeString(target, …)` — a signal mid-write leaves a truncated snapshot file.
- **Fix**: write temp file in target dir + `ATOMIC_MOVE` + `REPLACE_EXISTING`; `deleteIfExists` in finally.
- **Error-class hunt**: `PinAudit`'s `writeString` reviewed — diagnostic archive, also printed to stdout, no mid-run consumer; deliberately unchanged (documented here).
- **Process patch**: existing `MainTest.testDumpPolymorphism` exercises the new path (asserts snapshot content).

### FINDING-4 (F7-prep): p3frontend Go module did not build at base
- **Instance**: `go build ./...` in `phases/phase3/src/p3frontend` failed at master `855f8b03`: (a) `synth_enc_core.go:120-122` — `:=` redeclaring closure named results `(writer, reader, writeCloser)`; (b) `enccore_test.go:130` — `imp := importerFunc(func…{ … imp … })` references `imp` inside its own `:=` initializer (out of scope per Go spec).
- **Introduced by**: M7→M8 rebase `7e53780` (31 staged patches); no Go build gate covers p3frontend, so it escaped.
- **Fix**: (a) `:=` → `=` (assign named results); (b) `var imp importerFunc` + separate assignment.
- **Error-class hunt**: `go vet ./...` clean after fix; full `go test` then unmasked 5 drift-test failures (FINDING-5).
- **Process patch**: TODO — add a Go build/vet gate for p3frontend (see Process patches section).

### FINDING-5 (F7-prep follow-up): 5 p3frontend drift-test failures unmasked by the build fix
- **Instances**: (a) `TestBytesSyntheticSignatures`/`TestBytesOmissionsPinned`: importer lacked "io" (M8 added io import to synthetic bytes); (b) `TestStringsSyntheticSignatures`/`TestStringsOmissionSet`: `rejectImports{}` broke when synthetic strings gained "errors" import; (c) `TestJsonSyntheticSignatures`: mirror missing `UnsupportedTypeError` (phase-1 gained it in M8).
- **Fix**: (a) recursive `imp` importer with "io" branch via `phase1SyntheticPackageForTest` (same pattern as `enccore_test.go`); (b) new `stringsPhase1Importer(t)` resolving "errors"; (c) added `UnsupportedTypeError` (unexported `typ string` field + pointer `Error()` method) to the mirror.
- **Error-class hunt**: all drift tests re-run; remaining failures (if any) to be triaged.
- **Process patch**: same as FINDING-4 — the Go gate would have caught every one of these at the introducing commit.

### FINDING-6 (D3): fd exhaustion — three sub-findings, all fixed
- **6a. EIO instead of EMFILE.** `GoOs.errno()` had no EMFILE/ENFILE case — exhaustion fell through to generic `EIO`. gc returns `EMFILE`; Go programs branch on it (the synthetic `syscall` package itself has an `e == EMFILE` retry predicate and defines `EMFILE = Errno(0x18)`). Fix: `ENFILE=23`/`EMFILE=24` constants; message-based mapping ("too many open files" → EMFILE, "…in system" → ENFILE). **Subtlety caught by the attack**: `FileChannel.open` throws `FileSystemException`, not plain `IOException` — the check is needed in *both* branches (first attempt only covered `IOException` and still produced EIO; the D3 rerun caught it).
- **6b. Lazy class loading died under exhaustion.** `NoClassDefFoundError: graalgo/frontend/UnaryOpNode$1` (a compiler-generated switch-map) when the first error-reporting path executed under exhaustion — the guest died even though it handled EMFILE correctly. Fix: new `graalgo.launcher.ClassPreloader` — eagerly loads all `graalgo.*` classes at launcher startup (no initialization, best-effort), wired into all three launchers (`Main`, `SchedMain`, `P3SyncMain`) before guest execution.
- **6c. Misleading "internal error" report.** The scheduler's `recordCrash` labeled the exhaustion-induced `NoClassDefFoundError` as "internal error". Fix: `recordCrash` now probes fds directly (`fdExhausted()`) on class-loading failures and reports `graalgo-run: fatal fd exhaustion in goroutine: … (too many open files)`.
- **Error-class hunt**: `GoOs.errno` is the single errno mapper (net layer delegates) — no siblings. ClassPreloader covers all present and future `graalgo.*` classes automatically.
- **Process patch**: `GoOsErrnoTest.testErrnoTooManyOpenFiles` covers both exception branches; D3 attack itself is the behavioral regression (documented here).
- **Evidence**: `ulimit -n 256`, 400 opens → exit 0, `opened: 242 errors: 158 emfile: 158`, `first error: open /tmp/selfattack-target.txt: too many open files`, `done`. gc oracle: `opened: 251 errors: 149 emfile: 149`, identical message — parity (counts differ only in JVM fd headroom).
