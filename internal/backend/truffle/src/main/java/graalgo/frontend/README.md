# graalgo.frontend

Frontend service (WS B, PLAN.md D1): `gofrontend` (Go; vendored `go/*`
verbatim — see [internal/frontend/](../../../../../../../../internal/frontend/)) type-checks one single-file package
and emits `frontend/v1` JSON (wire spec: [phases/phase1/docs/frontend-v1.md](../../../../../../../../phases/phase1/docs/frontend-v1.md)).
This package parses that JSON and builds Truffle nodes.

## Contents

| File | Role |
|---|---|
| `Json.java` | Hand-rolled JSON reader (no third-party deps; choice documented in-file). Path-aware errors. |
| `FrontendException.java` | Parse/build failures; also the deliberate extension seam (unknown kinds fail naming their workstream). |
| `NodeFactory.java` | v1 JSON → Truffle nodes. Extension hooks: `registerExprKind` (WS D), `registerStmtKind` (WS E), `registerTypeDecoder` (WS C/D). |
| `GoExprNode` / `GoStmtNode` | Node base classes (unboxed `long` / `void` execute). |
| `ConstNode`, `BinaryOpNode`, `UnaryOpNode`, `VarReadNode`, `VarDeclNode`, `AssignNode`, `IncDecNode`, `BlockNode`, `ExprStmtNode`, `ReturnNode` | v1 straight-line node set (constants, integer arithmetic, assignment). |
| `GoReturn` | M2 return control-flow signal → WS F `ControlFlowException`. |
| `GoRootNode` | Root node for `func main`; catches `GoReturn`. |
| `TypeTable.java` | Bounds-checked view over the type table. |
| `DeferNode`, `FuncLitNode`, `CapReadNode`, `CapWriteNode`, `CapIncDecNode` | M5: `defer` (call-time arg evaluation, LIFO chain), function literals with closure capture over materialized parent frames. |
| `PanicBuiltinNode`, `RecoverBuiltinNode`, `PanicNode`, `GoexitCallNode` | M5: `panic`/`recover` builtins (wired to the frozen `Panic.throwPanic`/`recover`), the `panic(...)` statement form, `runtime.Goexit`. |

## v1 scope and stand-ins

Straight-line programs over Go integer kinds (all → `long`, D5). Deliberate
M2 stand-ins, each owned by its workstream: `GoReturn` → WS F; divide-by-zero
/ negative-shift `ArithmeticException` → WS F Go panic; bare-`return`/fall-off
→ 0 (real observability is the WS G println intrinsic). Shift semantics are
Go-exact (`<<`/`>>` with count ≥ 64 do **not** follow Java's masking).
Source sections: Truffle 25.x removed `Node.setSourceSection` — sections are
stored on the node and exposed via `getSourceSection()` override.

## Semantic lowerings (M3)

- **Kind-typed values (D5).** Every expression node carries a `GoKind`;
  `NodeFactory.kindOf` resolves named types to their underlying type and
  maps the type table through `GoKind.fromBasic`/`fromCompositeKind`.
  Narrow int/uint kinds live in `Int` slots holding sign/zero-extended
  values (the invariant: the carrier always contains the extended value,
  so parents never need wrapper nodes); `int64`/`uint64`/`int`/`uint`/
  `uintptr` use `Long`; `float32`→`Float`, `float64`→`Double`,
  `bool`→`Boolean`, strings/slices/maps/structs/interfaces/funcs→`Object`.
  Untyped constants are transparent (`GoKind.transparent`) — no conversion
  node is built; they assume the context kind.
- **Go-exact arithmetic.** `BinaryOpNode` implements the full operator
  surface at every width: signedness comes from the operand kind (unsigned
  `/`/`%` use `Long.divideUnsigned`/`remainderUnsigned`, unsigned `>>` is
  logical `>>>`); shift counts ≥ width yield 0 (unsigned/`<<`) or
  sign-extension (signed `>>`) — never Java's masking. Float `==` uses
  `==` (NaN ≠ NaN); `&&`/`||` short-circuit; string `+` concatenates
  `GoString`s. `ConvertNode` implements the full conversion matrix with
  gc-mirrored float→int (out-of-range → `0x8000...`, via `GoConvert`) and
  correctly-rounded int→float.
- **Two-phase multi-assignment.** Go evaluates every right-hand side (and
  every old left-hand value for op-assignments) before any assignment runs.
  `NodeFactory.assignTwoPhase` lowers `n > 1` assignments through fresh
  temporary slots typed by the value's kind, so `a, b = b, a` swaps; single
  assignments lower directly. The same rule covers multi-value `var`/`:=`,
  and `:=` redeclaration reuses the existing variable's slot (go/types
  records redeclared names in Uses; the exporter maps them to the existing
  object id).
- **Integer constant parsing** (`NodeFactory.parseGoIntConst`) returns the
  low 64 bits via `BigInteger` (untyped constants can exceed 64 bits, e.g.
  `1<<70`); typed constants are guaranteed representable by go/types, so
  extension is exact. Untyped constants wider than 64 bits are observed
  through the `exactDecimal()` node protocol (implemented by `ConstNode`
  and `UnaryOpNode`), which conversions use for exact float rounding and
  rune range checks — never the 64-bit slot value.
- **Zero values.** `var x T` (no initializer) lowers to the kind's zero
  via `NodeFactory.zeroNode` (numeric 0, `false`, `""`, nil for
  reference kinds, zero struct) — the factory supplies the initializer, so
  `VarDeclNode` never sees a null init.

## Defer / panic / recover / Goexit (M5)

- **Deferred calls** (`DeferNode` → `graalgo.runtime.DeferredCall`): the
  function value and arguments are evaluated when the `defer` statement
  executes; the call is pushed on the goroutine's `DeferChain` and runs
  LIFO at function return, panic unwind, or goexit. A per-function
  watermark gives nested calls per-function defer semantics from the
  single per-goroutine chain.
- **Function literals** (`FuncLitNode`, new `funclit` wire kind): built in
  a child `BuildCtx` whose frame descriptor starts with the `$closure`
  slot; variables resolving to an ancestor become `CapReadNode`/
  `CapWriteNode` walking the materialized parent frame. The literal
  captures the parent frame iff the body references an ancestor variable.
- **Panic** (`PanicBuiltinNode` → `Panic.throwPanic`): throws
  `PanicException` (an `AbstractTruffleException`, lazy traces) carrying
  the Go value plus its dynamic-type descriptor and Go-syntax type name
  for `printpanicval`-exact rendering (`PanicRendering`: bare
  ints/uints/floats/bools, `Name(v)` for named basics, `(type) 0xADDR`
  for composites, `*PanicNilError` for `panic(nil)`). `GoRootNode`
  unwinds the chain on panic; an unrecovered panic reaches the launcher
  (exit 2, gc-comparable stderr).
- **Recover** (`RecoverBuiltinNode` → `Panic.recover`): non-nil only when
  called *directly* by a deferred function while a panic is in flight
  (the `DeferChain` arms the flag around each deferred call during panic
  unwinding and clears it for nested calls and deferred builtins). After
  recovery the remaining deferred calls still run (their `recover()`
  calls see nil) — gc-verified.
- **Goexit** (`GoexitCallNode` → `Goexit`, a `ControlFlowException`):
  runs the deferred calls (not recoverable) and rethrows; Goexit in main
  is gc's fatal deadlock (exit 2) per the GG-0045 stance.

## Tests

- `src/test/java/graalgo/frontend/StraightLineCorpusTest.java`
  (`// verifies: GG-0012`): 12-program corpus
  (`phases/phase1/corpus/straightline/`, conventions in its README.md),
  differential vs stock gc — the oracle injects `println(vars...)` and the
  test asserts frame-slot reads are bit-identical. Run via `make test`.
- `src/test/java/graalgo/frontend/ArithmeticCorpusTest.java`
  (`// verifies: GG-0015`): 17-program corpus
  (`phases/phase1/corpus/arithmetic/`, conventions in its README.md),
  differential vs stock gc with a type-aware oracle (ints decimal, floats
  by `math.Float32/64bits`, strings hex) and per-kind frame-slot reads.
- `src/test/java/graalgo/values/ValuesTest.java`
  (`// verifies: GG-0015`): unit tests for the D5 value model and the
  `GoConvert` matrices, including gc-probed edge cases.
- `src/test/java/graalgo/frontend/PanicCorpusTest.java`
  (`// verifies: GG-0019`): 20-program corpus
  (`phases/phase1/corpus/panic/`, conventions in its README.md),
  differential vs stock gc — recovered programs assert post-defer values
  bit-identical (gc oracle via a `// probe`-anchored deferred `println`);
  fatal programs assert exit code 2, the normalized panic header block,
  and the goroutine-trace header (explicit comparison scope in the
  test's javadoc).
- [internal/frontend/javatest/NodeFactoryTest.java](../../../../../../../../internal/frontend/javatest/NodeFactoryTest.java) (WS B, compiled to
  `/tmp` scratch): golden JSON → build → execute, asserting gc-oracle
  values, node shapes, and `SourceSection` ↔ exporter-position agreement,
  plus negative cases.

## GG status

**GG-0012 active** (2026-10-04, M2): `gofrontend` emits typed AST and
`NodeFactory` builds straight-line programs end-to-end via `graalgo-run`,
verified by `StraightLineCorpusTest` (`// verifies: GG-0012`, differential vs
stock gc). GG-0013/GG-0044 stay **locked** per PLAN.md.

**GG-0015 active** (2026-10-04, M3): the D5 value model
(`graalgo.values`) and the complete expression surface
(`BinaryOpNode`/`UnaryOpNode`/`ConstNode`/`ConvertNode` at every Go kind)
are differential-green on the arithmetic corpus, verified by
`ArithmeticCorpusTest` + `ValuesTest` (`// verifies: GG-0015`). The D8
`Safepoint.poll()` seam is in place at function entries (no-op in
Phase 1; signature frozen in `docs/freeze-stub.md`).

**GG-0019 active** (2026-10-04, M5): defer/panic/recover/Goexit are
differential-green on the 20-program panic corpus, verified by
`PanicCorpusTest` (`// verifies: GG-0019`) — recovered programs assert
every observed post-defer value bit-identical to stock gc; fatal programs
assert exit code 2, the normalized panic header block, and the
goroutine-trace header. `panic(nil)` renders as `*PanicNilError`;
`recover()` outside a deferred call is nil; Goexit in main is gc's fatal
deadlock (GG-0045 stance).
