# frontend/v1 — typed-AST JSON wire format

**Status:** normative for Phase 1 (PLAN.md D1). **Version:** `v1`.
**Producer:** `gofrontend` (`phases/phase1/frontend/`). **Consumer:** `graalgo.frontend.NodeFactory`.
**Validator:** `phases/phase1/frontend/tools/validate_frontend_v1.py` (enforces this document).

## 1. Overview

`gofrontend` type-checks one single-file Go program with the vendored `go/*`
packages and emits a single JSON document: the AST with per-expression type
references into a serialized type table (from `go/types`), source positions on
every node, and package metadata. `NodeFactory` parses the document and builds
Truffle nodes. The format is debuggable, versionable, and process-boundary
clean (PLAN.md D1).

Conventions: JSON objects marshal with sorted keys; arrays preserve emission
order. The exporter is deterministic — two runs on the same input are
byte-identical (`TestDeterministic`).

## 2. Document layout

```json
{
  "frontend": "v1",
  "generator": { "tool": "gofrontend", "version": "0.1.0", "goroot": "go1.27.1" },
  "package":   { "name": "main", "files": ["hello.go"], "lang": "go1.27" },
  "types":     [ ... ],
  "objects":   [ ... ],
  "funcs":     [ ... ]
}
```

| Field | Meaning |
|---|---|
| `frontend` | Wire version this document conforms to. Consumers reject anything else. |
| `generator.tool` / `version` | Producer identity. |
| `generator.goroot` | `runtime.Version()` of the toolchain the vendored `go/*` came from (the `-overlay` build in `frontend/build.sh` guarantees the compiled sources are the vendored ones; see `frontend/VENDORING.md`). |
| `package.name` / `files` | Package name; base names of input files (positions index into this array). |
| `package.lang` | Go language version assumed at type-check (`go1.27` = vendored toolchain default; per-file `-lang` gates arrive with WS G, GG-0013). |
| `types` | Type table (§4). |
| `objects` | Object table (§5). |
| `funcs` | Top-level `funcdecl` nodes, in source order (§6). |

## 3. Source positions

Every node carries `pos`:

```json
"pos": { "file": 0, "line": 11, "col": 7, "off": 412, "len": 5 }
```

`file` indexes `package.files`; `line`/`col` are 1-based (go/token convention,
except `col` is rune-based, not byte-based); `off` is the byte offset of the
node's first character; `len` is the rune length of the node's source text
(`utf8.RuneCount` over `End()`−`Pos()`). `NodeFactory` builds `SourceSection`s
as `createSection(line, col, len)` (Truffle is char-based, so the wire is too;
byte-based values crashed it with "charIndex out of range" on non-ASCII
sources — M8 fuzzer find, fixed).

## 4. Type table

`types[i]` serializes one `go/types` type exactly once; expressions reference
entries by index. Shape: `{ "kind": "<kind>", "detail": { ... } }`.
`detail` schemas per kind:

| kind | detail fields |
|---|---|
| `basic` | `name` (e.g. `"int64"`), `class` ∈ {`bool`,`int`,`uint`,`float`,`complex`,`string`,`pointer`,`untyped`,`unknown`}, `untyped` bool |
| `named` | `name` (qualified `"path.Name"`, or bare for universe scope), `underlying`: type id, `methods`: [{`name`, `obj`: funcdecl object id, `recv` ∈ {`value`,`pointer`} (declared receiver kind), `index`: receiver-adaptation path — `Selection.Index()` minus its last entry, `[]` for direct methods, `[0]` for a method promoted via embedded field 0, `null` for the unsupported value-embedded/pointer-receiver shape (itab build fails fast with a named error if dispatched), `implkey`: method-identity key — method name (unexported names package-qualified) + `\x00` + structural signature key with aliases expanded (so `Write([]uint8)` and `Write([]byte)` share a key); the runtime implements check compares `implkey`s, not bare names}] — populated since P3-M3 (ADR-0003) via `types.NewMethodSet` (covers promoted/embedded methods); interface-declared methods (no funcdecl) and template methods are skipped; a generic instantiation (e.g. `main.Box[int]`) whose method set is non-empty carries `"templateMethodsSkipped": true` — the frontend fails fast with the named P3-M3 scope error if it is boxed into a non-empty interface (R1 P1-2) |
| `pointer` | `elem`: type id, `methods`: same shape as `named` — the full `*T` method set (value + pointer receivers; P3-M3/ADR-0003) |
| `slice` | `elem`: type id |
| `array` | `len`: int, `elem`: type id |
| `map` | `key`, `elem`: type ids |
| `chan` | `dir` ∈ {`both`,`send`,`recv`}, `elem`: type id |
| `struct` | `fields`: [{`name`, `type`: id, `tag`, `embedded`: bool, `offset`: bytes via `types.SizesFor("gc","amd64")`}] |
| `signature` | `params`, `results`: [{`name`, `type`: id}], `variadic`: bool |
| `interface` | `empty`: bool, `methods`: [{`name`, `sig`: id, `implkey`: method-identity key, same computation as on `named`/`pointer` `methods` entries — the assertion implements check tests `implkey` inclusion}], `embedded`: [ids] |
| `union` | `terms`: [{`tilde`: bool, `type`: id}] |
| `typeparam` | `name`, `constraint`: type id |
| `alias` | `name` (qualified), `rhs`: type id |

Recursive types are handled (the id is registered before the entry is
described). All integer reference fields must satisfy `0 <= id < len(types)`;
the validator rejects dangling ids.

## 5. Object table

`objects[i]` = `{ "kind": "var", "name": "<name>", "type": <type id>, "param": bool }`;
since M6 also `"kind": "func"` (function/method objects) and `"kind": "inst"`
(monomorphized instantiations, with `instof` + `typeargs`). Identifiers
reference objects by
`obj` id; the id is assigned at the declaration in a single deterministic AST
walk, so ids are stable across runs.

## 5a. Globals (M7)

Package-level variables are listed in document order-independent
*initialization* order in the top-level `globals` array:

`globals[i]` = `{ "obj": <object id>, "init": <expr node|null>, "pos": <pos> }`.

- `init` is the variable's initializer expression, or `null` when the
  declaration has none (the variable takes its zero value).
- `obj: -1` marks a side-effect-only blank initializer (`var _ = ...`):
  the expression is evaluated for side effects and discarded.
- Entries appear in Go's initialization order (spec, Package
  initialization): a variable's initializer dependencies first,
  declaration order breaking ties; a dependency cycle is a build error.
- Package-level `const` declarations need no wire entry: every reference
  folds through go/types (iota included) at the use site.

## 6. Nodes

Every node is exactly `{ "kind", "pos", "fields" }` — including `funcdecl`
and `param`. `fields` schemas per kind (`type` = type-table id):

| kind | fields |
|---|---|
| `funcdecl` | `name`, `sig`: type id, `params`: [param nodes], `body`: block node — or, for a `//go:linkname`-bound function (P3-M4, WS C, ADR-0004; first emitted by gofrontend 0.5.0): `name`, `sig`, `params`, `results` (optional), `linkname`: str (the target symbol) and NO `body` field; the factory builds a `LinknameRootNode` dispatching by symbol name instead of a Go body |
| `param` | `name`, `obj`: object id, `type`: type id |
| `block` | `stmts`: [stmt nodes] |
| `vardecl` | `names`: [str], `objs`: [object ids] (parallel to `names`), `define`: bool (`true` for `:=`), `type`: type id or null, `values`: [expr nodes] |
| `assign` | `tok` ∈ {`=`,`+=`,`-=`,`*=`,`/=`,`%=`,`&=`,`\|=`,`^=`,`&^=`,`<<=`,`>>=`}, `lhs`: [expr nodes], `rhs`: [expr nodes] |
| `exprstmt` | `x`: expr node |
| `return` | `results`: [expr nodes] |
| `incdec` | `tok` ∈ {`++`,`--`}, `x`: expr node |
| `ident` | `name`, `obj`: object id, `type`: type id; *or* builtin form: `name`, `builtin`: str, `type`: type id |
| `methodref` | `obj`: object id (method funcdecl/instantiation), `recv`: expr node (evaluated first, passed as the method's params[0]), `type`: signature type id with the receiver as params[0] — a method-call target (M6, WS H) |
| `ifacemethod` | `recv`: expr node (evaluates to a GoInterface), `method`: str, `iface`: type id (the receiver's static interface type — named or anonymous; the method-name list is the `methods` list of the `interface` type-table entry, following `underlying` for `named` entries), `args`: [expr nodes] (the receiver is NOT included), `sig`: type id (the method signature, no receiver — for call-site argument adaptation, the `methodref` precedent), `type`: type id or null (null when the call has no results) — an interface method call `w.M(args)` with dynamic dispatch through the itab (P3-M3, WS D, ADR-0003; additive node kind, first emitted by gofrontend 0.4.0) |
| `basiclit` | `value`: source text, `const`: `go/constant` `ExactString()` (present when the literal is a constant), `type`: type id; string constants additionally carry `sbytes`: hex of the exact UTF-8 bytes (byte-exact — `ExactString` is a quoted literal and JSON cannot carry invalid UTF-8); float constants additionally carry `f32bits`: int and `f64bits`: long, the signed-reinterpreted IEEE-754 bits (correctly rounded by `go/constant` itself; `ExactString` renders rationals as `"a/b"`, which no float parser reads); complex constants additionally carry `creal32`/`creal64`/`cimag32`/`cimag64`: the IEEE-754 bits of the real and imaginary parts (M7) |
| `binary` | `op` ∈ {`+`,`-`,`*`,`/`,`%`,`&`,`\|`,`^`,`&^`,`<<`,`>>`,`==`,`!=`,`<`,`<=`,`>`,`>=`,`&&`,`\|\|`}, `x`, `y`, `type`: type id |
| `unary` | `op` ∈ {`+`,`-`,`!`,`^`}, `x`, `type`: type id |
| `paren` | `x`, `type`: type id |
| `convert` | `to`: type id, `from`: source type id (M7; needed for string<->slice to get the element kind), `x`: expr node — a Go conversion `T(x)` where `T` denotes a type (M3; builtin calls stay `call`) |
| `call` | `fun`: expr node, `args`: [expr nodes], `builtin`: str or null, `type`: type id or null, `ellipsis`: bool (M7; true for `append(s, x...)` — string spread *and* slice spread since `007b42f`; only `append` carries it, and only for `...` calls) |
| `fmtcall` | `func`: str (one of `Print`, `Printf`, `Println`, `Sprint`, `Sprintf`, `Sprintln`, `Errorf`), `args`: [expr nodes], `type`: type id or null — a call to the D4 `fmt` stub via the Phase-1 import overlay (WS G). Only the seven D4 symbols resolve; any other `fmt.X` is a scope error naming Phase 3. |
| `if` | `init`: stmt node or null, `cond`: expr node, `then`: block node, `else`: stmt node or null (block or `if` for else-if) |
| `for` | `init`: stmt node or null, `cond`: expr node or null, `post`: stmt node or null, `body`: block node |
| `range` | `key`, `value`: object ids or null (absent/blank), `define`: bool, `x`: expr node, `body`: block node |
| `switch` | `init`: stmt node or null, `tag`: expr node or null (null = `switch {}`), `clauses`: [`caseclause` nodes] |
| `caseclause` | `cases`: [expr nodes], `default`: bool, `body`: block node |
| `typeswitch` | `init`: stmt node or null, `tag`: expr node, `clauses`: [`typecase` nodes] |
| `typecase` | `types`: [type ids] or null (`case nil`), `nilcase`: bool, `default`: bool, `bindobj`: object id or null, `bindtype`: type id or null, `body`: block node |
| `branch` | `op` ∈ {`break`,`continue`,`goto`,`fallthrough`}, `label`: str or null |
| `labeled` | `label`: str, `stmt`: stmt node |
| `defer` | `call`: expr node — a deferred call (M5, WS F) |
| `funclit` | `params`: [param nodes], `results`: [param nodes], `body`: block node — a function literal (M5, WS F) |
| `blank` | (no fields) — the blank identifier `_` in assignment lhs |
| `complit` | `type`: type id, `keyed`: bool, `keys`: [expr nodes] (absent unless keyed map/array/slice), `fieldkeys`: [str] (absent unless keyed struct), `values`: [expr nodes] |
| `index` | `x`, `index`: expr nodes, `type`: type id (element type; map elem type for comma-ok), `commaOk`: bool |
| `sliceexpr` | `x`: expr node, `lo`/`hi`/`max`: expr nodes or null, `type`: type id |
| `selector` | `x`: expr node, `field`: str, `selpath`: [int] (field indices through embedding), `type`: type id |
| `typeassert` | `x`: expr node, `type`: type id, `commaOk`: bool |
| `nil` | `type`: type id (the switch tag's type for `case nil:`) |
| `makechan` | `elem`: type id (channel element type), `size`: expr node or absent (buffer capacity; absent = unbuffered), `type`: type id (the channel type) — `make(chan T)` / `make(chan T, n)` (P3-M5, WS E, ADR-0005 App C; additive node kind, first emitted by gofrontend 0.7.0). Emitted instead of the generic `make` call node: the frozen builtin make path rejects chan types |
| `send` | `chan`, `value`: expr nodes — `ch <- v` statement (P3-M5, WS E, ADR-0005 App C; additive node kind, first emitted by gofrontend 0.7.0) |
| `recv` | `x`: expr node (the channel), `type`: type id (element type) — `<-ch` (P3-M5, WS E, ADR-0005 App C; additive node kind, first emitted by gofrontend 0.7.0). Replaces the old `unary` `<-` emission (the frozen UnaryOpNode never implemented it). The `v, ok := <-ch` shape desugars to a single-case, no-default `select` (the frozen factory's `isCommaOkValue` only recognizes `index`/`typeassert`) |
| `chanclose` | `x`: expr node — `close(ch)` statement (P3-M5, WS E, ADR-0005 App C; additive node kind, first emitted by gofrontend 0.7.0). The builtin registry is untouched: intercepted by ident in the exporter |
| `select` | `cases`: [`selectcase` nodes] — a `select` statement (P3-M5, WS E, ADR-0005 App C; additive node kind, first emitted by gofrontend 0.7.0) |
| `selectcase` | `kind` ∈ {`recv`,`send`,`default`}; recv: `chan`: expr node, `targets`: [object ids] or absent (case-scoped `v := <-ch` / `v, ok := <-ch` targets, parallel to the lhs); send: `chan`, `value`: expr nodes; `body`: block node — one `select` case. Channel/value expressions are evaluated once up front (gc semantics) |
| `chanlen` | `x`: expr node (the channel), `type`: type id (int result type) — `len(ch)` (P3-M5, WS E, ADR-0005 App C; additive node kind, first emitted by gofrontend 0.7.0). The frozen `LenNode` rejects chan (WS G); `TimerChan` (hence `TimeChan`) reports 0 via the DIV-M3-1 override |
| `chancap` | `x`: expr node (the channel), `type`: type id (int result type) — `cap(ch)` (P3-M5, WS E, ADR-0005 App C; additive node kind, first emitted by gofrontend 0.7.0). Same builder path as `chanlen` |
| `timenew` | `kind` ∈ {`timer`,`ticker`}, `duration`: expr node (or JSON number: nanoseconds — the Java builders accept both), `type`: type id (`*time.Timer` / `*time.Ticker`) — `time.NewTimer(d)` / `time.NewTicker(d)` (P3-M5, WS E, ADR-0005 App W; additive node kind, first emitted by gofrontend 0.7.0) |
| `timeop` | `op` ∈ {`Stop`,`Reset`}, `recv`: expr node (the timer/ticker), `duration`: expr node or absent (required for `Reset`, absent for `Stop`), `type`: type id (bool) or absent — `(*Timer).Stop/Reset`, `(*Ticker).Stop/Reset` (P3-M5, WS E, ADR-0005 App W; additive node kind, first emitted by gofrontend 0.7.0). `type` is absent for void `(*Ticker).Stop()` |
| `timec` | `recv`: expr node (the timer/ticker), `type`: type id (channel) — `t.C` (P3-M5, WS E, ADR-0005 App W; additive node kind, first emitted by gofrontend 0.7.0) |
| `timenow` | `type`: type id (the named `time.Time`) — `time.Now()` (P3-M5, WS E, ADR-0005 App W; additive node kind, first emitted by gofrontend 0.7.0) |
| `timesleep` | `duration`: expr node (or JSON number: nanoseconds) — `time.Sleep(d)`, void: no `type` field (P3-M5, WS E, ADR-0005 App W; additive node kind, first emitted by gofrontend 0.7.0) |
| `timeafter` | `duration`: expr node (or JSON number: nanoseconds), `type`: type id (channel) — `time.After(d)` (P3-M5, WS E, ADR-0005 App W; additive node kind, first emitted by gofrontend 0.7.0) |
| `timetick` | `duration`: expr node (or JSON number: nanoseconds), `type`: type id (channel) — `time.Tick(d)` (P3-M5, WS E, ADR-0005 App W; additive node kind, first emitted by gofrontend 0.7.0) |
| `timeafterfunc` | `duration`: expr node (or JSON number: nanoseconds), `func`: expr node, `type`: type id (`*time.Timer`) — `time.AfterFunc(d, f)` (P3-M5, WS E, ADR-0005 App W; additive node kind, first emitted by gofrontend 0.7.0) |

Normalizations (applied by the exporter, part of the contract):
- `*ast.EmptyStmt` is dropped (empty statements carry no semantics).
- `:=` is emitted as `vardecl` with `define: true` (one node kind for both
  declaration forms; `NodeFactory` treats them identically).
- Constant folding (M3): a `binary`/`paren` expression whose `go/types`
  value is a non-nil constant is emitted as a single folded `basiclit`
  (with the folded value's `const`/`sbytes`/`f32bits`/`f64bits` and type).
  Unary expressions are *not* folded (unary ops are exact on low 64 bits;
  folding them churned the golden for no benefit) — but an untyped
  constant wider than 64 bits can still sit under a unary node, so
  consumers must read such subtrees via exact decimals, not 64-bit slots.
- Universe identifiers (M3): `true`/`false` (a `*types.Const`) are emitted
  as `basiclit` nodes; `nil` (a `*types.Nil`) is a scope error naming
  WS D.

Monomorphization (M6, WS H): generics are erased on the wire — the
factory never sees a type parameter. The exporter computes the full
instantiation fixpoint (`enumerateInstantiations` in `frontend/export.go`):
roots come from `go/types` `info.Instances`/`info.Selections` in
non-generic code; nested instantiations inside generic bodies are found by
walking each template with its type substitution. Every needed
(generic function, type args) / (method, receiver args, method args) pair
is emitted as its own `funcdecl` in the top-level `funcs` array, with
`name` = the display name (e.g. `Min[int]`, from `instNameForFunc`).
Dedup is by instantiation key (`F:<name>|<targs>` for functions,
`M:<recvkey>.<mname>|<mtargs>` for methods): one wire funcdecl per
distinct instantiation, no matter how many call sites. Method-level type
arguments bind positionally (`bind[mtps.At(i)] = methodTargs[i]`) with loud
arity/concreteness errors; the path is covered differentially by
`corpus/generics/gn04_generic_method_targs.go` (`s.MapTo[string](...)`,
`s.MapTo[int](...)` vs stock gc). The generic
template itself is *not* emitted — only reachable instantiations are.
Termination is guaranteed by `go/types` (instantiation cycles are type
errors, so the graph is finite); a defensive cap guards against
enumerator bugs as an internal error, never silent. The cap is
unreachable for accepted programs except pathological-but-legal ones
with more than 100000 distinct instantiations, where it fails loudly
rather than hanging.

Type identity across instantiations (M6, WS H): a `*types.Named` with
substituted type arguments (e.g. `Some[T]` under `T=int`) denotes the same
type as the `Some[int]` from non-generic code. The exporter substitutes
bound type parameters before the type-table memo lookup (`substType` in
`frontend/export.go`) and deduplicates named types by qualified name
(including type arguments), so both sides share one wire type id — without
this, a value boxed in a monomorphized body could never satisfy a type
assertion in the caller (issue52026).

Call sites resolve through the same map: a call to a generic
function/method is a `call` node whose `fun` is an `ident` (functions)
or `methodref` (methods, carrying `obj`, `recv`, and the signature
`type` id) with `obj` = the instantiation's object id and `type` = the
instantiation's signature id — statically dispatched, exactly like a
non-generic call. The `methodref` `type` id has the receiver as params[0]
(matching the callee funcdecl's `sig`) so the factory can box concrete
arguments passed to interface-typed parameters at the call site (P1-1).
Generic function *values* in single-`IndexExpr` form (`f := Min[int]`)
resolve the same way; multi-type-argument values (`f := Show[int,
float64]`, an `*ast.IndexListExpr`) and parenthesized values
(`(Min)[int]`) are loud scope errors (WS D).
Generic structs carry field offsets of `-1` ("unknown") whenever a field
type could contain a bare type parameter (`go/types` `gcSizes` asserts on
those; offsets are instantiation-dependent anyway); the factory indexes
fields by position, never by byte offset.

## 7. Exporter scope (v1, M4 control-flow + composite subset, M5 defer/panic/funclit, WS G builtins, M6 typeparams/monomorphization, M7 methods)

The exporter fails fast — non-zero exit, message on stderr, no JSON on
stdout — outside this scope. Every rejection names the workstream that owns
the extension:

- one input file; **no imports** except `fmt` via the Phase-1 import
  overlay (WS G) and `runtime` for `runtime.Goexit` only (M5, WS F):
  the overlay synthesizes a `fmt` package exposing only
  the seven D4 symbols (`Print`, `Printf`, `Println`, `Sprint`, `Sprintf`,
  `Sprintln`, `Errorf`) with exact variadic signatures; any other `fmt.X`
  is a scope error naming Phase 3. Calls to the D4 symbols are emitted as
  `fmtcall` wire nodes.
- package-level declarations: `func main` plus any number of named
  functions, generic or not (M6, WS H); package-level `const` (references
  fold through go/types, iota included) and `var` (M7 — initialized in
  dependency order before `main` runs; see §5a). Package-level `var` with
  a multi-valued initializer (`var a, b = f()`) is a v1 scope error
  (WS D), as is ranging with `=` into a package-level variable (M7). A
  generic function's template is
  never emitted as such — only its reachable instantiations appear, one
  `funcdecl` per instantiation (see §6, Monomorphization). An
  instantiation whose type arguments still contain an unbound type
  parameter is an internal error, never silent.
- `func main`: no receiver (WS D); parameters/results are rejected by
  `go/types` itself ("func main must have no arguments and no return
  values");
- named types may have methods (M6, WS H): methods are emitted as
  `funcdecl`s / instantiations and dispatch resolves by object id. The
  `methods` list on `named` type-table entries is populated since P3-M3
  (ADR-0003: one `{name, obj, recv}` per method via `types.NewMethodSet`);
  `pointer` entries carry the same field with the full `*T` method set.
  Generic structs carry field offsets of `-1`
  (instantiation-dependent; see §6).
- statements: block, `var`/`:=`, `=`/op-assign (lhs: identifier, index,
  selector, or blank), expression statements, `return`, `++`/`--`
  (identifier/index/selector), `if`/`else` (with init), `for` (3-clause,
  condition-only, infinite), `range` over slice/array/string/map (not
  channels — Phase 2), `switch` (expression + type switch), `break`/
  `continue` (labeled and unlabeled), `goto`/labels, `fallthrough`,
  `defer` (M5, WS F; `go` → WS F Phase 2; `select` → WS E Phase 2);
- expressions: identifiers, basic literals, binary/unary, parentheses,
  conversions, composite literals (array/slice/struct/map, incl. keyed and
  `&T{}`), indexing (array/slice/string/map, incl. comma-ok), slicing
  (2- and 3-index), selectors (incl. embedded/promoted), type assertions
  (incl. comma-ok), `nil`, function literals (M5, WS F);
- calls: builtins per the WS G builtin surface (see freeze-stub.md
  "Builtin nodes"); `panic`/`recover` are builtins (M5, WS F/G);
  user-defined function calls (M6, WS H) and method calls (M6, WS H):
  methods dispatch by object id via `methodref` fun targets
  (receiver-based dispatch only — implicit address-of/deref on the
  receiver, method values, and interface dispatch on type parameters are
  scope errors naming WS D/WS H); interface method calls (P3-M3, WS D)
  dispatch dynamically via `ifacemethod` nodes (ADR-0003) — method values
  and method expressions on interface receivers, and generic (template)
  methods via interfaces, are named P3-M3 scope errors;
- named types with methods beyond the M6 subset → scope error (WS D):
  the M6 subset is receiver-based dispatch on named-type receivers with
  an exact receiver-type match (see the calls bullet).

There are no silently dropped or defaulted nodes. `go/types` errors (parse
and type errors) abort before any JSON is emitted.

## 8. Versioning rule

**Additive-only within v1.** These changes keep `"frontend": "v1"`:

- new node kinds, new type kinds, new object kinds;
- new optional `fields`/`detail` entries (consumers must ignore unknown fields);
- new enum values in `tok`/`op`/`dir`/`class` sets (consumers reject unknown
  values with a precise error, as today).

These changes bump to **`v2`** (and require a new/updated factory):

- renaming, removing, or retyping any existing field;
- changing the meaning of any existing field or enum value;
- changing position semantics.

`NodeFactory.buildProgram` rejects any document whose `frontend` is not
`"v1"` with a `FrontendException` quoting this rule. The exporter stamps
`frontendVersion` (`frontend/export.go`); the golden tests pin the wire bytes.

## 9. Extending NodeFactory — WS D/E hook points

`src/main/java/graalgo/frontend/NodeFactory.java` is structured for
extension, not modification of the v1 core:

1. **New expression kinds** (WS D: unary/call/composite/selector/… full
   coverage): `registerExprKind(String kind, ExprBuilder b)`. The builder
   receives the node object and the `BuildCtx` and returns a `GoExprNode`.
   Unknown kinds already fail with `FrontendException` naming this hook.
2. **New statement kinds** (WS E: if/for/switch/branch/…):
   `registerStmtKind(String kind, StmtBuilder b)`, same pattern.
3. **New type-table kinds** (WS C/D: floats, strings, slices, structs,
   maps, interfaces…): `registerTypeDecoder(TypeDecoder d)`. A decoder maps
   `(typeId, entry)` to a `FrameSlotKind`, or returns `null` to abstain; the
   factory tries decoders in registration order and throws naming this hook
   if none handles the entry. The v1 default handles `basic` int/uint →
   `FrameSlotKind.Long` (D5).
4. **Build state** (`BuildCtx`, public): `source`, `frames`
   (`FrameDescriptor.Builder`), `types` (`TypeTable`), `objects` (raw
   array), `objSlots` (object id → slot index), `slotsByName` (name → slot
   index; test/introspection aid, last-wins on shadowing).
5. **Duplicate registration is an error** — two workstreams claiming the
   same kind fails fast at registration, never silently.
6. **Node classes** (`GoExprNode`/`GoStmtNode` hierarchies) live in
   `graalgo.frontend` for WS B; WS D/E may relocate them to `graalgo.nodes`
   (or keep them) — either way the `NodeFactory` hook signatures are the
   stable seam.

Deliberate M2 stand-ins, each replaced by its owning workstream:
- `GoReturn` (private control-flow signal) → WS F `ControlFlowException`.
- `ArithmeticException` on divide-by-zero / negative shift → WS F Go panic.
- `return` with no value / falling off `main` → 0 (main returns nothing in
  Go; the value is the M2 test-observability channel — real programs observe
  via the WS G println intrinsic).

## 10. Worked example

`frontend/testdata/straight01.go`:
```go
package main

func main() {
	x := 6
	y := x + 1
	z := x * y
	z = z
}
```
exports to `frontend/testdata/straight01.json` (checked in; golden test).
`frontend/javatest/` replays it: `NodeFactoryTest` builds the program from
the JSON, executes it on the Truffle runtime, and asserts `z == 42`,
including a position cross-check (`SourceSection` char index/length vs the
exporter's `off`/`len`) and gc-oracle value checks for `straight02`.
