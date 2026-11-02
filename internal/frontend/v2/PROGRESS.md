# frontend/v2 — progress tracker

Long-lived parallel track. See [docs/arch/frontend-v2.md](../../../docs/arch/frontend-v2.md)
for the design.

## 2026-10-15 — Bootstrap

- [x] Design doc written (`docs/arch/frontend-v2.md`)
- [x] Module skeleton (`internal/frontend/v2/go.mod`)
- [x] Stage 1 (parser): `go/parser` wrapper, parses files to `*ast.Package`
- [x] Stage 2 (checker): `go/types` wrapper, real importer (no synthetics)
- [x] Stage 3 (emit): document framework + type table + object table
- [x] Node emitters: funcdecl, block, assign, vardecl, exprstmt, return,
      incdec, branch, if, for, range, ident, basiclit, binary, unary,
      paren, call, selector
- [x] Driver CLI + hello-world smoke test (passes)
- [x] End-to-end: hello-world Go → in-memory program (no JSON wire format)
- [x] Mechanical AST-node coverage matrix (`emit/coverage_test.go`):
      60 AST node types classified — 18 handled, 6 via go/types,
      8 structural, 3 non-Node (documented), 25 loudly rejected
- [x] Negative tests (`emit/negative_test.go`): 17 unhandled constructs
      proven to fail loudly with "unsupported" (no silent drops)

## 2026-10-16 — V2-12 deterministic emission

- [x] `emitDocument` no longer ranges over `pkg.Files` (a Go map): files
      are visited sorted by name, declarations sorted by source position —
      function order and type/object ids are stable run to run
- [x] Regression test (`emit/determinism_test.go`): 25 repeated emissions
      of a 3-file package are byte-identical (canonical JSON encoding);
      proven to fail with the fix reverted

## Master merge log

- 2026-10-15 21:10 CEST: merged `704932ce` (PKG-012 sync intrinsics,
  PKG-011 cell identity, bug-038/041/042 fixes). Clean merge, no conflicts.
  Standing order: merge master at least hourly.

## Next

- [ ] Differential test: programs built via v2 vs gc on the corpus
      (behavioral differentials; v2 emits no JSON, so there is no v1-JSON
      output to compare against)
- [ ] More node kinds: composite literals, struct/array/map/slice types,
      type assertions, type switches, go/defer, channels, interfaces
- [ ] Method declarations (currently only functions)
- [ ] Package-level vars and init order (globals)
- [ ] Generic instantiations
- [ ] Import resolution for multi-package programs
- [ ] `go vet` clean, more unit tests per stage

## Cutover criteria

- v2 passes the differential corpus with zero drift vs gc
- v2's own suite (coverage matrix, negative tests, driver) is green
- Independent review approves the cutover

## 2026-10-15 — JSON removal correction

- His 2026-10-15 correction: v2 must NOT emit frontend/v1 JSON. The
  emitter's JSON output was removed; `Emit` now returns the in-memory
  `*Document` directly (`encoding/json` no longer appears anywhere in v2).
- Route: circular compilation of go/* remains primary (his 2026-10-15
  order: "go is ideal (to reuse go/*) if we can circularly compile it
  for inclusion in graalgo"); Java rewrite only if that proves infeasible.

## 2026-10-16 — Go→Java binary interface (lane restaff)

- **Interface design**: The v2 `Document` is encoded to a compact binary
  format (NOT JSON) for transfer to the Java backend. Format: type-tagged
  values (null/bool/int/string/list/map), varint integers, length-prefixed
  strings/collections. General: encodes the Document structure generically,
  not hardcoded per node kind. See `encode/encode.go` for the spec.
- **Go encoder** (`encode/`): `Encode(*Document) ([]byte, error)` walks the
  `map[string]any` structure. Tested: hello-world encodes to 915 bytes,
  round-trip type coverage. `encode/cmd/encode-demo` CLI for manual testing.
- **Java decoder** (`internal/backend/truffle/.../graalgo/frontend/v2/`):
  `BinaryDecoder.java` decodes binary to `Map<String,Object>`/`List`/`String`/
  `Long`. The cross-language contract is pinned by automated tests, not a
  manual demo: `BinaryDecoderTest.java` decodes golden vectors byte-identical
  to the Go encoder's `TestEncodeGoldenDoc` output (2026-10-29: the former
  manual `V2DecodeDemo.java` print-demo was removed — demo mains with
  `println` output cannot live in `src/main` under the debug-leftover gate;
  the full-document hello-world decode is currently proven on the Go side
  only, and re-proving it on the Java side is open work).
- **NodeFactoryV2** (`graalgo/frontend/v2/NodeFactoryV2.java`): Takes binary
  bytes, decodes via `BinaryDecoder`, validates frontend=="v2", and builds
  Truffle nodes. MVP implements: document validation, funcdecl extraction,
  block/exprstmt/basiclit node construction (ConstNode, BlockNode,
  ExprStmtNode). Complex kinds (assign, call, ident, etc.) fail loudly with
  FrontendException — full implementation is next.
- **End-to-end proven**: `go run ./encode/cmd/encode-demo hello.go hello.bin`
  produces the binary the Java `BinaryDecoder` consumes; the shared wire
  format is pinned by the Go `TestEncodeGoldenDoc` / Java `BinaryDecoderTest`
  golden pair. The binary interface works; no JSON involved.
- **Next**: Full Truffle node construction in NodeFactoryV2 (assign with
  frame slots, calls, idents, control flow). Then circular compilation:
  graalgo compiles the v2 Go code (which uses go/parser, go/ast, go/types)
  to JVM bytecode, so it runs in-process and calls NodeFactoryV2 directly
  (no inter-process binary transfer).

## 2026-11-02 — Node-kind contract for the v2 Document (veto fix-up)

This section is the live contract for what `emit.Emit` puts in
`*Document` (`internal/frontend/v2/emit/emit.go`). The design doc
(`docs/arch/frontend-v2.md`) does not enumerate node kinds — it states
the pipeline shape and disclaims the v1 JSON wire format — so this
section, not the design doc, is what the next backend lane builds
against. The Document is in-memory (`map[string]any` nodes, `Types` /
`Objects` tables, `Funcs`, `Globals`); for transfer to Java it is
encoded by `encode/` (see `encode/encode.go` for the binary spec), never
as JSON.

### Statement / declaration node kinds (`node.kind` → fields)

- `funcdecl` {name, sig, recv, params, results, body, pkgpath}.
  `recv` is nil for plain functions; for methods it is
  {name, obj, type} where `obj` is always an object id — an unnamed
  receiver has name `""` but still has a `*types.Var` object, so
  `recv.obj` is never nil-for-unnamed.
- `go` {call}, `defer` {call} — the call node, not the keyword alone.
- `send` {chan, value}.
- `switch` {init, tag, body}: `init`/`tag` are nil when absent
  (`switch {` has tag nil); `body` is a list of `case` nodes.
- `typeswitch` {init, guard, body}: `guard` is a `typeswitchguard`
  node, never an assign — go/types records no single object for a
  type-switch guard variable (it has a different type per clause).
- `typeswitchguard` {var, x}: `var` is the guard variable name, or nil
  for an unguarded switch (`switch x.(type)`).
- `select` {body}: a list of `comm` nodes.
- `comm` {comm, default, body}: `comm` is the send / receive-assign /
  bare-receive statement node, or nil for the default clause
  (`default` true marks it).
- `case` {list, default, body}: `default` is true exactly when the
  clause list is empty. `case nil:` is NOT default — its list holds
  one `ident` node resolving to the nil object. In a type switch, a
  type in the list is a `typeexpr` node, not a value expression.
- `labeled` {label, stmt}, `branch` {tok, label}: `tok` is
  break/continue/goto/fallthrough; `label` is the label string, or nil
  for an unlabelled branch.
- `typeexpr` {type}: a type-table id for a type appearing in type
  position (type-switch case, type assertion target).
- `globalvar` {names, objs, type, values}, `globalconst` (same, plus
  `constvals`: one go/types `ExactString` per name, so repeated const
  specs and iota chains carry values the AST does not),
  `globaltype` {name, obj, type, alias} (`alias` true for `type A = T`).

### Label-nil rule

Nil in a label/var/init/tag/comm slot means "absent by syntax"
(unlabelled branch, unguarded switch, no init/tag, default select
clause) — never "unknown" or "missing data". A consumer must not invent
a label, a guard object, or a default case from nil; the explicit
`default` flags exist so it does not have to infer them.

### Type-table kinds (`Types` entry kind → detail)

- `basic` {name, class, untyped}, `pointer` {elem}, `slice` {elem},
  `array` {len, elem}, `map` {key, elem}, `chan` {dir, elem}
  (dir: both/send/recv), `signature` {params, results, variadic}.
- `named` {name, underlying, methods, ptrMethods}: `methods` is the
  value method set, `ptrMethods` the pointer-only remainder (methods
  callable via `*T` but not `T`; value-receiver methods appear once,
  under `methods`).
- `struct` {fields}: each {name, type, tag, embedded, offset}
  (`offset` is currently always 0 — a known placeholder, not a real
  layout).
- `interface` {empty, methods, embedded, complete}: `methods` are the
  explicit methods only; `embedded` holds embedded interfaces and
  type-set elements (unions) by type id. `complete` is
  `IsMethodSet()`: true exactly when the method list is the whole
  interface. It is false for any interface carrying type-set
  restrictions (a union term, or an embedded constraint interface),
  even after successful checking — false never means "still under
  construction". Backend rule for `complete: false`: the `methods`
  list is NOT the full contract; consult `embedded` (unions) to decide
  which types satisfy the interface. Method-set-only dispatch is wrong
  for it.
- `union` {terms}: each term {type, tilde} — `tilde` true admits
  underlying types (`~int`), false admits only the exact type.
  A union is never `basic`.
- `typeparam` {name, constraint, index}: a generic type parameter;
  `constraint` is a type-table id (usually an interface).
- `alias` {name, rhs, args}: an alias declaration's own identity plus
  its right-hand-side type id (`args` empty for non-generic aliases).
- `unknown` {name, class: "unknown"}: a loud marker for a go/types
  kind with no case yet — a backend must reject it explicitly, never
  treat it as a scalar. No current go/types kind produces it.

### Java-side status (do not assume)

`NodeFactoryV2` implements a subset of these kinds and fails loudly
(`FrontendException`) on the rest; it currently discards branch labels
(builds `BranchNode(op, null)`) — Java label handling is a separate
follow-up lane, not done by this diff. A backend built on this contract
must not assume labels reach execution yet.

### Loudly rejected (compile error, not silent)

`EmptyStmt`, `StarExpr` (including ordinary pointer dereference `*p` —
the largest remaining emitter gap, a follow-up candidate),
`FuncLit`, `Ellipsis`, generic `IndexListExpr`. Pinned in
`emit/negative_test.go`. Generic type/function type parameters are not
yet in the type table (pre-existing gap, out of scope here).
