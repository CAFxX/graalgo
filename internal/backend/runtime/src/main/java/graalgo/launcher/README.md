# graalgo.launcher

The `graalgo-run` command-line interface (PLAN.md §5 CLI contract).

## Contract

```
graalgo-run file.go              frontend → AST → execute main; exit 0
graalgo-run --check file.go      frontend only (harness build step; build-failure parity)
graalgo-run --dump-vars file.go  execute, then print name=value per frame slot (debug aid)
graalgo-run --dump-polymorphism[=path] file.go
                             execute with call-site polymorphism recording,
                             then print (bare flag) or write (path) the JSON snapshot (M9, WS L)
graalgo-run --help
```

Exit codes: 0 = success; 1 = usage error, unreadable input file, gofrontend
unlaunchable, or a launcher-internal error; 2 = build failure (gofrontend
rejected the program — parse, type, or scope error; gofrontend's stderr is
passed through verbatim as the deterministic build log the diffharness
records) **or an uncaught Go panic / Goexit in main** (M5: gc exits 2 on
fatal panics; `Main.reportPanic` prints gc-comparable output — the
`panic: <value>` line with `printpanicval`-exact rendering, the optional
`[signal SIGSEGV: ...]` line, and the `goroutine 1 [running]:` header with
graalgo-shaped Go frames; Goexit in main prints gc's fatal-deadlock line,
exit 2, per the GG-0045 stance).

`--check` is the diffharness build step: it succeeds exactly when the
frontend accepts the program and fails (exit 2, deterministic stderr) when
it rejects it, so build failures compare 1:1 against gc.

## M2 state (2026-10-04)

The frontend path is real: `graalgo-run` shells out to `gofrontend`
(resolved by `ToolPaths`: `GRAALGO_GOFRONTEND`, else
`<phase1>/frontend/gofrontend` relative to the launcher's own classes, else
`PATH`), feeds the frontend/v1 JSON to `NodeFactory`, and executes
`func main` on the Truffle AST. The M1 `--handbuilt-hello` scaffolding and
`Handbuilt.java` were removed at M2 per the recorded decision — there is no
second execution mode.

The program's exit code is 0 after `func main` completes (Go's `main`
returns nothing; `os.Exit`/panics arrive with WS F/G). Computed values are
observable in tests via frame-slot reads (`StraightLineCorpusTest`
differential vs stock gc); `--dump-vars` prints them for manual runs. The
WS G `println` intrinsic becomes the program-observable channel.

## M5 state (2026-10-04)

`defer`/`panic`/`recover`/`Goexit` are live (GG-0019, `PanicCorpusTest`
differential vs stock gc on `corpus/panic/` — 20 programs). The fatal-path
contract above (exit 2, gc-comparable stderr) is pinned by the test's
`// panic:` cases with an explicit comparison scope: normalized panic
header block compared exactly, goroutine-trace header asserted present,
frame bodies not compared (paths/pcs), `0xADDR` address normalization —
see the test's javadoc.

Host-stack stance: guest-language recursion burns host stack, so the JVM
runs with `-Xss32m` (Makefile `JAVA_FLAGS` and the `graalgo-run`
wrapper). Measured 2026-10-04: the corpus's 10000-deep panic unwind
(pn12) needs ~13–16m (~1.3–1.6KB per nested call through Truffle's
exception profiling); 32m is ~2x headroom. A stackless/trampolined call
path is Phase-2+ work; re-measure at M10 self-attack if deeper
adversarial recursion lands.

## Known M7 concern

The HotSpot Truffle runtime prints `sun.misc.Unsafe` terminal-deprecation
warnings to stderr on every JVM launch (they come from Truffle itself, not
graalgo code). The diffharness compares stderr, so the stderr normalizer
needs an allowlist entry for the graalgo backend before full-corpus
differential runs (tracked here so it is not discovered at M7). Verified
2026-10-04: gc-a vs graalgo on `sl06_multiassign` diverges ONLY on this
warning line — build, exit code, and stdout are identical.
`PanicCorpusTest` filters these exact `WARNING: ...sun.misc.Unsafe...`
lines before comparing fatal-program stderr (documented in the test).
