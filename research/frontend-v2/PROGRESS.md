# frontend/v2 — progress tracker

Research track (since 2026-11-01: moved out of the product tree by
lane D, review P1-4 — see the decision record in
[docs/REPO_STRUCTURE.md](../../docs/REPO_STRUCTURE.md)). See
[docs/arch/frontend-v2.md](../../docs/arch/frontend-v2.md) for the design.

## 2026-10-15 — Bootstrap

- [x] Design doc written (`docs/arch/frontend-v2.md`)
- [x] Module skeleton (`research/frontend-v2/go.mod`)
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

## 2026-11-01 — Research-track decision (lane D, review P1-4)

- v2 moved from `internal/frontend/v2/` to `research/frontend-v2/`:
  at 18/60 handled AST node kinds, no method declarations, no generics,
  no multi-package import resolution, and no product caller on either
  side (the Go driver is standalone; `NodeFactoryV2` is uncalled), v2
  is a research program, not a cutover candidate. It leaves the
  per-push gate (`make all` no longer runs `test-v2`; the target
  remains for manual runs) and re-enters the product tree only through
  the parity gate in `docs/arch/frontend-v2.md` §4: differential parity
  on the corpus, plus independent review of the cutover.
- P2-1 fixed: emitter positions are now rune-based. `parser.Result`
  carries source texts through to `emit.Emit`, which counts runes in
  the source line before each position (`emit/emit.go` runeCol);
  pinned by `emit/positions_test.go` (multi-byte runes before a node).
  The old byte-column approximation and its TODO are gone.
- Java side: the uncalled MVP `NodeFactoryV2` was deleted from the
  product tree (it built only funcdecl/block/basiclit; everything else
  failed loudly; no caller, no test). `BinaryDecoder` and its golden
  test stay: they are the pinned Go→Java wire contract the research
  track still produces, and a future NodeFactoryV2 is rebuilt against
  them at cutover time, not carried as dead code until then.
