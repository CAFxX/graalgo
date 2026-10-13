# graalgo.runtime

Runtime entry points callable from generated Truffle ASTs
(`Runtime.java`).

M1 scope: the print path only (`Runtime.println(String)` — writes the string
plus a newline to stdout), exercised by the hand-built hello AST.

WS G grows this package into the intrinsic surface (builtins + runtime
stubs) under the frozen signature set (GG-0020/GG-0021, PLAN.md D4/D7).
Parity stances for the real builtins (e.g. Go's print/println write to
stderr, not stdout) are recorded in GG-0045 when they land — the M1 hello
fixture's stdout routing is deliberate and documented in `Runtime.java`,
not a parity claim.

## WS G contents (M4)

**Real implementations** (differential vs gc):
- `Intrinsics`: memmove/memequal, makeslice/growslice (gc's growth policy
  ported exactly), slicecopy, map ops, fastrand (wyrand port).
- `BuiltinPrint`: `print`/`println` builtins → **stderr** (gc parity).
- `Fmt`: the D4 `fmt` stub (7 pure functions) with verb-conformance matrix
  (`corpus/fmt/matrix.tsv`, 122 rows vs gc). Replaceable-in-Phase-3.
- `GoFloatFmt`, `GoQuote`, `GoIsPrint`: ported gc formatting logic.
- `Atomics`: JVM-backed atomics. `Timers.nanotime()`: `System.nanoTime()`.

**Honest stubs** (throw `UnsupportedOperationException`, Phase 2 owns):
- `Intrinsics.itabFor`, `Intrinsics.traceback`.
- `Timers.sleep/startTimer/stopTimer`, `Scheduler` (all), `Netpoll` (all),
  `ChanOps` (all).

**Worker-F** (panic/defer/goexit): `Panic`, `PanicException`, `DeferChain`,
`GoroutineState`, etc. — the frozen `throwPanic`/`recover` interface that
WS G's `PanicNode` wires to.

See [docs/freeze-stub.md](../../../../../docs/freeze-stub.md) (freeze v2; v2 delta: ADR-0008 `elemDesc` on
`makeslice`/`growslice`) for the full 58-function catalog.
