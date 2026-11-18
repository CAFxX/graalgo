# graalgo bug register

Known graalgo bugs that an expected-divergence entry, a quarantine line or a
test comment cites by id. An id is never reused; a fixed bug keeps its row
with the fixing commit. Every [corpus-exclusions.yaml](../../test/differential/corpus-exclusions.yaml)
and [srctest-exclusions.yaml](../../test/differential/srctest-exclusions.yaml)
entry with reason `deferred-parity` names one of these ids (`backend-specific`
is reserved for behavior gc leaves to the implementation, SPEC §5.6 — a
graalgo bug is never backend-specific).

| Id | Bug | Evidence | Retired by |
|---|---|---|---|
| BUG-0001 | v1 frontend scope gaps / type errors: constructs `gofrontend` rejects or mis-types (calls through index expressions, method expressions, `&` on parenthesized exprs, multi-result interface methods, range over `*ast.StarExpr`, synthetic `sort`/`encoding/json` without reflect, …) | 25 srctest entries; corpus 10, 19, 40, 42 | the toolexec emitter at M3 (toolexec-design D11) |
| BUG-0002 | named result assignment references an unknown object id (srctest F-2) | `235.go` | M3 |
| BUG-0003 | an unsigned narrow element with the high bit set, read out of an **array** through fmt/reflect, prints sign-extended through uint32 (`[2]uint8{5, 250}` → `[5 4294967290]`); slices are fine. The `[I cannot be cast to [B` crash on the same path was fixed 2026-11-18 (narrow-array-fmt regression test) | [test/regression/narrow-array-fmt/](../../test/regression/narrow-array-fmt/) comment | p3reflect deletion at M3, or a fix in the reflect element-read path |
| BUG-0004 | variadic interface forwarding: `fmt.Sprint(v...)` gives `[9][8]` not `98` (srctest F-3) | `defer.go`, `defernil.go` | M3 |
| BUG-0005 | `new(x)` with a non-constant `x` returns the zero value (srctest F-4) | `newexpr.go` | M3 |
| BUG-0006 | complex comparison: `(0+1.8e308i) != itself` (srctest F-5) | `literal2.go` | M3 |
| BUG-0007 | peg-solitaire search finds no solution (srctest F-6; root cause unknown) | `solitaire.go` | triage |
| BUG-0008 | map keys of kind ARRAY are unsupported | `bigmap.go` | M3 (flat value model) |
| BUG-0009 | `min`/`max` on signed zeros depend on operand order (`max(-0.0, 0.0)` = -0) | corpus 58 | min/max lowering |
| BUG-0010 | a function that recovers returns zero values instead of the named results set by deferred calls; still diverging after T0.A | corpus 59 | panic model |
| BUG-0011 | goroutine traces print frames as `level3()` without package qualifier, args and absolute path; still diverging after T0.A | corpus 27 | panic/trace rendering |
| BUG-0012 | `gofrontend` intermittently fails with `JSON encoding failed: invalid character '\x00' after object key` (seen in RangeConcurrentRegressionTest rep 2, 2026-11-18; snapshot-check used to retry it away — now counted by `-retry-budget`, global review G6) | `make test-java-slow` log | v1 deletion at M3, or a fix in the exporter |
| BUG-0013 | a method value (`r.Multistream` on a `*gzip.Reader`) prints as `<nil>` instead of a func address: the method value is not materialized | wave3 `compress/gzip/00_roundtrip_levels.go`; the `.normalize.sed` rule that hid it was removed 2026-11-18 (global review G7) | method values in the new node package (W4) |
