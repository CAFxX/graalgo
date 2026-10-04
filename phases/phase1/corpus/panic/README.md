# Panic corpus (M5)

Differential corpus for defer/panic/recover/Goexit (GG-0019): every program
is a single `func main` (plus `import "runtime"` for the Goexit programs,
via the frontend's Phase-1 `runtime.Goexit` overlay carve-out), exercised
both by stock gc (the oracle) and by `gofrontend` → `NodeFactory`.

## Conventions

- **gofmt-clean.** Keep files gofmt-clean so brace-finding stays trivial.
- **Two trailer kinds** (last comment line of the file):
  - `// observe: v1 v2 ...` — *recovered* programs: main returns normally
    (every panic is recovered). The listed variables' post-defer values are
    compared bit-identically (gc oracle vs graalgo frame-slot reads).
  - `// panic: <expected first stderr line>` — *fatal* programs
    (unrecovered panic, or Goexit in main). Compared: process exit code
    (graalgo-run exits 2; gc's program exit 2 surfaces as `go run`'s
    `exit status 2` trailer), the normalized panic header block
    (`panic: <value>` plus gc's optional `[signal ...]` line), and the
    presence of the `goroutine 1 [running]:` header. Frame bodies are never
    compared (paths/pcs differ legitimately).
- **`// probe` anchor** (recovered programs only): the gc-oracle injection
  replaces this anchor line with
  `defer func() { println(<kind-aware dump of observed vars>) }()`.
  It must sit after every observed variable's declaration and before
  main's first defer-affecting statement (first `defer`, or the loop that
  registers defers), so the probe is main's *first-registered* defer and
  therefore the *last* to run — observing post-defer values. Deferred
  calls run LIFO; a probe registered last would run first and observe
  pre-defer values. The anchor is a comment: zero effect on graalgo's
  execution.
- **Every declared variable must be used** — `go/types` rejects
  "declared and not used"; trailing no-op uses (`_ = x`) mark the
  observation points.
- **Determinism:** no I/O, no maps, no randomness, no wall-clock.
  The test runs everything with `GOTOOLCHAIN=local GOMAXPROCS=1`.
- **gc-oracle facts pinned by this corpus** (verified against stock go
  1.27.1):
  - `panic(nil)` renders as `runtime error: panic called with nil argument`
    (the `*PanicNilError` rendering, gc ≥ 1.21) — pn06 recovers it and
    observes `r == nil` is false.
  - `recover()` outside a deferred call during a panic returns nil —
    pn08 pins all three positions (top-level, deferred-no-panic,
    defer-argument).
  - Goexit in main is gc's fatal deadlock (`fatal error: no goroutines
    (main called runtime.Goexit) - deadlock!`, exit 2); deferred calls
    still run; `recover()` cannot stop it — pn10/pn11.
  - Deferred calls run LIFO with arguments evaluated at defer time —
    pn01/pn02.

## Edge-case coverage (each pinned against a past or plausible bug)

- `pn01_defer_lifo`: LIFO order of deferred closures capturing a variable.
- `pn02_defer_args_early`: defer arguments evaluated at defer-statement
  time, not at call time.
- `pn03_defer_named_results`: deferred closure mutates a named result
  before the return completes.
- `pn05_panic_values`: panic/recover round-trip for int/string/bool/
  float64 through `any`, incl. a type switch on the recovered value.
- `pn06_panic_nil`: `panic(nil)` → `*PanicNilError`, not a nil interface.
- `pn07_recover_stops`: recover stops the unwind; remaining (outer)
  defers still run.
- `pn08_recover_outside_defer`: recover is nil outside a deferred call,
  even during a panic unwind and as a defer argument.
- `pn09_nested_panic`: a panic inside a deferred call replaces the
  in-flight panic; remaining defers run against the new one.
- `pn10_goexit` / `pn11_goexit_not_recoverable`: Goexit runs defers,
  is not recoverable, and is gc's fatal deadlock in main.
- `pn12_deep_recursion`: panic unwind through 10000 nested closures.
- `pn13_defer_storm`: 10000 deferred closures (chain scalability).
- `pn14_oob_index` … `pn19_div_zero`: runtime panics (index/slice
  bounds, nil-map write, failed type assertion, nil dereference,
  integer divide by zero) render gc's exact `runtime error:` strings.
- `pn20_panic_slice_value`: a composite panic value round-trips through
  recover (renders as `([]int) 0xADDR`, never a real address).

## Comparison scope (fatal programs)

Documented in `PanicCorpusTest`'s javadoc; the short version: exit code +
first-panic-line + optional signal line are compared exactly after
dropping the HotSpot/Truffle `sun.misc.Unsafe` `WARNING:` lines (known M7
concern) and normalizing `0x[0-9a-f]+` → `0xADDR`; the goroutine-trace
header line must be present in both; frame bodies are not compared.

## Mutation testing (red-green)

See `../hidden_files/m5_red/README.md` for mutant evidence (SPEC §5.2):
- **Mutant 1**: `DeferChain.unwindReturning` FIFO instead of LIFO →
  pn01 fails (order=123 vs 321). Killed.
- **Mutant 2**: `Panic.recover()` always returns nil → pn07 fails
  (panic unrecovered). Killed.

## Known limitations (M5)

- **Trace text**: Panic stack traces show graalgo frames, not gc PCs.
  The header line and exit code are compared; frame bodies are known
  divergence (documented in `PanicCorpusTest`).
