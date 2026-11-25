# Straight-line corpus (M2)

Differential corpus for the frontend/v1 straight-line subset: every program
is a single `func main` with no imports, exercised both by stock gc (the
oracle) and by `gofrontend` → `NodeFactory`.

## Conventions

- **gofmt-clean.** The gc-oracle injection in `StraightLineCorpusTest` inserts a
  `println(...)` before the last `}`-alone line (which gofmt guarantees is
  `func main`'s closing brace) **and** before every bare `return` line, so
  early returns are observed too. At most one injected `println` may execute
  per oracle run; the test parses the first stderr line.
- **`// observe: v1 v2 ...`** trailer (last line, after main's closing brace)
  lists the variables whose final values are compared. Order is significant:
  the oracle parses `println`'s space-separated output positionally.
- **Every declared variable must be used** — `go/types` rejects
  "declared and not used", so the final variable(s) end with a no-op
  self-assignment (`z = z`). This is idiomatic for the corpus, not a
  language recommendation.
- **Determinism:** integer arithmetic only, no I/O, no maps, no randomness.
  The test runs the oracle with `GOTOOLCHAIN=local GOMAXPROCS=1`.
- **Edge-case coverage** (each pinned against a past or plausible bug):
  - `sl04_shifts`: shift counts ≥ 64 (Go: `<<`→0, signed `>>`→sign-extended,
    unsigned `>>`→0 — Java's count masking must never leak through).
  - `sl05_int_uint`: mixed int/uint, unsigned division/modulo/shift with the
    high bit set (Java's `/`, `%`, `>>` are sign-sensitive; Go's are not).
  - `sl06_multiassign`: multi-value assignment and the swap `a, b = b, a`
    (Go evaluates all RHS before assigning — sequential lowering
    miscompiles it).
  - `sl07_define`: `:=` forms incl. same-block redeclaration
    (`x, z := x+1, y+1` reuses `x`'s slot; go/types records it in Uses).
  - `sl10_overflow`: wraparound at int64/uint64 bounds, `minint64 / -1`
    (no trap), decimal `minint64` literal (exercises constant parsing).
  - `sl11_divmod`: negative division/modulo (truncation toward zero,
    dividend's sign — matches Java, pinned anyway).
  - `sl12_earlyreturn`: bare `return` mid-block skips the tail; nested block
    scoping.

## Files

`sl01_consts` (literal bases, negation) · `sl02_arith` (precedence, parens) ·
`sl03_bitwise` (`& | ^ &^`, complement) · `sl04_shifts` · `sl05_int_uint` ·
`sl06_multiassign` · `sl07_define` · `sl08_incdec` · `sl09_unary` ·
`sl10_overflow` · `sl11_divmod` · `sl12_earlyreturn`.
