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
- [x] End-to-end: hello-world Go → valid frontend/v1 JSON

## Next

- [ ] Differential test: v2 output vs v1 output on the corpus
- [ ] More node kinds: composite literals, struct/array/map/slice types,
      type assertions, type switches, go/defer, channels, interfaces
- [ ] Method declarations (currently only functions)
- [ ] Package-level vars and init order (globals)
- [ ] Generic instantiations
- [ ] Import resolution for multi-package programs
- [ ] `go vet` clean, more unit tests per stage

## Cutover criteria

- v2 passes the differential corpus with zero drift vs v1
- All v1 tests pass against v2 output
- Independent review approves the cutover
