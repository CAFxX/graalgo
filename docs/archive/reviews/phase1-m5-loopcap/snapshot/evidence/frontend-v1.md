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

`file` indexes `package.files`; `line`/`col` are 1-based (go/token convention);
`off` is the byte offset of the node's first character; `len` is the byte
length of the node's source text (`End()`−`Pos()`). `NodeFactory` builds
`SourceSection`s as `createSection(line, col, len)` (lengths are byte-based;
identical to char lengths for ASCII sources — WS E refines for non-ASCII).

## 4. Type table

`types[i]` serializes one `go/types` type exactly once; expressions reference
entries by index. Shape: `{ "kind": "<kind>", "detail": { ... } }`.
`detail` schemas per kind:

| kind | detail fields |
|---|---|
| `basic` | `name` (e.g. `"int64"`), `class` ∈ {`bool`,`int`,`uint`,`float`,`complex`,`string`,`pointer`,`untyped`,`unknown`}, `untyped` bool |
| `named` | `name` (qualified `"path.Name"`, or bare for universe scope), `underlying`: type id, `methods`: [] (populated by WS D; a named type *with* methods is an exporter scope error today) |
| `pointer` | `elem`: type id |
| `slice` | `elem`: type id |
| `array` | `len`: int, `elem`: type id |
| `map` | `key`, `elem`: type ids |
| `chan` | `dir` ∈ {`both`,`send`,`recv`}, `elem`: type id |
| `struct` | `fields`: [{`name`, `type`: id, `tag`, `embedded`: bool, `offset`: bytes via `types.SizesFor("gc","amd64")`}] |
| `signature` | `params`, `results`: [{`name`, `type`: id}], `variadic`: bool |
| `interface` | `empty`: bool, `methods`: [{`name`, `sig`: id}], `embedded`: [ids] |
| `union` | `terms`: [{`tilde`: bool, `type`: id}] |
| `typeparam` | `name`, `constraint`: type id |
| `alias` | `name` (qualified), `rhs`: type id |

Recursive types are handled (the id is registered before the entry is
described). All integer reference fields must satisfy `0 <= id < len(types)`;
the validator rejects dangling ids.

## 5. Object table

`objects[i]` = `{ "kind": "var", "name": "<name>", "type": <type id>, "param": bool }`.
v1 has variables only (parameters flagged). Identifiers reference objects by
`obj` id; the id is assigned at the declaration in a single deterministic AST
walk, so ids are stable across runs.

## 6. Nodes

Every node is exactly `{ "kind", "pos", "fields" }` — including `funcdecl`
and `param`. `fields` schemas per kind (`type` = type-table id):

| kind | fields |
|---|---|
| `funcdecl` | `name`, `sig`: type id, `params`: [param nodes], `body`: block node |
| `param` | `name`, `obj`: object id, `type`: type id |
| `block` | `stmts`: [stmt nodes] |
| `vardecl` | `names`: [str], `objs`: [object ids] (parallel to `names`), `define`: bool (`true` for `:=`), `type`: type id or null, `values`: [expr nodes] |
| `assign` | `tok` ∈ {`=`,`+=`,`-=`,`*=`,`/=`,`%=`,`&=`,`\|=`,`^=`,`&^=`,`<<=`,`>>=`}, `lhs`: [expr nodes], `rhs`: [expr nodes] |
| `exprstmt` | `x`: expr node |
| `return` | `results`: [expr nodes] |
| `incdec` | `tok` ∈ {`++`,`--`}, `x`: expr node |
| `ident` | `name`, `obj`: object id, `type`: type id; *or* builtin form: `name`, `builtin`: str, `type`: type id |
| `basiclit` | `value`: source text, `const`: `go/constant` `ExactString()` (present when the literal is a constant), `type`: type id; string constants additionally carry `sbytes`: hex of the exact UTF-8 bytes (byte-exact — `ExactString` is a quoted literal and JSON cannot carry invalid UTF-8); float constants additionally carry `f32bits`: int and `f64bits`: long, the signed-reinterpreted IEEE-754 bits (correctly rounded by `go/constant` itself; `ExactString` renders rationals as `"a/b"`, which no float parser reads) |
| `binary` | `op` ∈ {`+`,`-`,`*`,`/`,`%`,`&`,`\|`,`^`,`&^`,`<<`,`>>`,`==`,`!=`,`<`,`<=`,`>`,`>=`,`&&`,`\|\|`}, `x`, `y`, `type`: type id |
| `unary` | `op` ∈ {`+`,`-`,`!`,`^`}, `x`, `type`: type id |
| `paren` | `x`, `type`: type id |
| `convert` | `to`: type id, `x`: expr node — a Go conversion `T(x)` where `T` denotes a type (M3; builtin calls stay `call`) |
| `call` | `fun`: expr node, `args`: [expr nodes], `builtin`: str or null, `type`: type id or null |
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
| `blank` | (no fields) — the blank identifier `_` in assignment lhs |
| `complit` | `type`: type id, `keyed`: bool, `keys`: [expr nodes] (absent unless keyed map/array/slice), `fieldkeys`: [str] (absent unless keyed struct), `values`: [expr nodes] |
| `index` | `x`, `index`: expr nodes, `type`: type id (element type; map elem type for comma-ok), `commaOk`: bool |
| `sliceexpr` | `x`: expr node, `lo`/`hi`/`max`: expr nodes or null, `type`: type id |
| `selector` | `x`: expr node, `field`: str, `selpath`: [int] (field indices through embedding), `type`: type id |
| `typeassert` | `x`: expr node, `type`: type id, `commaOk`: bool |
| `nil` | `type`: type id (the switch tag's type for `case nil:`) |

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

## 7. Exporter scope (v1, M4 control-flow + composite subset)

The exporter fails fast — non-zero exit, message on stderr, no JSON on
stdout — outside this scope. Every rejection names the workstream that owns
the extension:

- one input file; **no imports** except `fmt` via the Phase-1 import
  overlay (WS G): the overlay synthesizes a `fmt` package exposing only
  the seven D4 symbols (`Print`, `Printf`, `println`, `Sprint`, `Sprintf`,
  `Sprintln`, `Errorf`) with exact variadic signatures; any other `fmt.X`
  is a scope error naming Phase 3. Calls to the D4 symbols are emitted as
  `fmtcall` wire nodes.
- no package-level declarations except `func main` (WS G/H);
- `func main`: no receiver (WS D); parameters/results are rejected by
  `go/types` itself ("func main must have no arguments and no return
  values");
- statements: block, `var`/`:=`, `=`/op-assign (lhs: identifier, index,
  selector, or blank), expression statements, `return`, `++`/`--`
  (identifier/index/selector), `if`/`else` (with init), `for` (3-clause,
  condition-only, infinite), `range` over slice/array/string/map (not
  channels — Phase 2), `switch` (expression + type switch), `break`/
  `continue` (labeled and unlabeled), `goto`/labels, `fallthrough`
  (go/defer → WS F; `select` → WS E Phase 2);
- expressions: identifiers, basic literals, binary/unary, parentheses,
  conversions, composite literals (array/slice/struct/map, incl. keyed and
  `&T{}`), indexing (array/slice/string/map, incl. comma-ok), slicing
  (2- and 3-index), selectors (incl. embedded/promoted), type assertions
  (incl. comma-ok), `nil`;
- calls must be builtins; `NodeFactory` v1 rejects every builtin call with a
  precise error (builtin surface: WS G);
- named types with methods → scope error (WS D).

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
