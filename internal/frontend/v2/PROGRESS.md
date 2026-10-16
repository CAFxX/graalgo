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
  `Long`. `V2DecodeDemo.java` proves end-to-end: Go encodes, Java decodes
  and prints structure. Tested: 915-byte hello-world binary decodes to
  frontend=v2, package=main, 1 func, 3 types, 1 object.
- **NodeFactoryV2** (`graalgo/frontend/v2/NodeFactoryV2.java`): Takes binary
  bytes, decodes via `BinaryDecoder`, validates frontend=="v2", and builds
  Truffle nodes. MVP implements: document validation, funcdecl extraction,
  block/exprstmt/basiclit node construction (ConstNode, BlockNode,
  ExprStmtNode). Complex kinds (assign, call, ident, etc.) fail loudly with
  FrontendException — full implementation is next.
- **End-to-end proven**: `go run ./encode/cmd/encode-demo hello.go hello.bin`
  → `java graalgo.frontend.v2.V2DecodeDemo hello.bin` prints the decoded
  structure. The binary interface works; no JSON involved.
- **Next**: Full Truffle node construction in NodeFactoryV2 (assign with
  frame slots, calls, idents, control flow). Then circular compilation:
  graalgo compiles the v2 Go code (which uses go/parser, go/ast, go/types)
  to JVM bytecode, so it runs in-process and calls NodeFactoryV2 directly
  (no inter-process binary transfer).
