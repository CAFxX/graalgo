# Arithmetic corpus (M3)

Differential corpus for the WS C/D expression surface: every program is a
single `func main` with no imports, exercised both by stock gc (the oracle)
and by `gofrontend` → `NodeFactory`.

## Scope

- Every int/uint width (`int8`…`int64`, `uint8`…`uint64`, `int`, `uint`,
  `uintptr`) × `{+,-,*,/,%,<<,>>,&,|,^,&^}`, unary `-`/`^`, overflow
  wraparound, division edge cases (`minint/-1` wraps, no trap).
- `float32`/`float64`: arithmetic, `0.1+0.2` precision, `16777217`
  round-half-even, `±Inf` propagation, underflow-to-zero.
- All conversion pairs: int↔uint (all widths, sign/zero extension,
  truncation), int/uint↔float32/64 (correct rounding, `uint64→float64`
  precision loss, truncation toward zero, gc's out-of-range mirrors),
  int→string (rune conversion incl. `U+FFFD` cases), float32↔float64.
- Comparisons at every kind (signed, unsigned high-bit, float, `Inf`,
  string lexicographic, bool) and logical `&&`/`||`/`!`.
- Shift counts ≥ width at every width (Go: `<<`→0, unsigned `>>`→0,
  signed `>>`→sign extension — Java's count masking must never leak).

`string↔[]byte` conversions are NOT covered (no slices in v1 scope — WS D).
NaN cannot be produced without imports, so NaN comparison semantics are
not covered (documented gap; the nodes implement IEEE semantics, matching
gc by construction).

## Conventions

Same base conventions as `corpus/straightline/README.md`: gofmt-clean,
`// observe:` trailer (last line), every variable used (`x = x` no-op
idiom), deterministic, `GOTOOLCHAIN=local GOMAXPROCS=1`.

## Oracle encoding (type-aware)

`ArithmeticCorpusTest` builds one `println(...)` over all observed vars
(injected before `func main`'s closing brace, as in M2) and parses the
single stderr line positionally, per the variable's Go kind:

| Kind | Oracle expression | Parse |
|---|---|---|
| int widths | `println(v)` | signed decimal |
| uint widths | `println(v)` | unsigned decimal |
| float32 | `println(math.Float32bits(v))` | uint → IEEE bits |
| float64 | `println(math.Float64bits(v))` | ulong → IEEE bits |
| bool | `println(v)` | `true`/`false` |
| string | `println(hex.EncodeToString([]byte(v)))` | hex → bytes |

`import "math"` / `import "encoding/hex"` are injected into the **oracle
copy only** (the corpus files stay import-free, in frontend scope).
Bit-exact float comparison needs the real bits — Go's builtin `println`
formats floats lossily (`println(0.1+0.2)` prints `0.3`), so it cannot be
the oracle channel for floats.

The graalgo side reads frame slots per kind (`getInt`/`getLong`/
`getFloat`/`getDouble`/`getBoolean`/`getObject`→`GoString` bytes) and
asserts bit-identical values.

## Red-green / mutation story

- Red run (test+corpus written before the WS C/D implementation):
  `hidden_files/m3_red1.log` — factory rejects float/string/etc. types
  (`no TypeDecoder handles type id …`), conversions fail at export.
- Mutant A (operator mapping): float `SUB`→`ADD` in `BinaryOpNode` —
  caught by `ar10`/`ar11` (`hidden_files/m3_mutantA_red.log`).
- Mutant B (conversion): `float64→int64` uses Java's saturating cast
  instead of the gc mirror — caught by `ar13` out-of-range cases
  (`hidden_files/m3_mutantB_red.log`).
- Green: `hidden_files/m3_green.log` (55/55; `m3_green2.log` re-green
  after mutant revert).

## Edge cases pinned

- `ar13`: `w := float64(-(1 << 70))` — an untyped constant wider than 64
  bits through a non-folded unary reaches the float conversion via the
  exact-decimal protocol (`-1.1805916207174113e+21`, gc-verified). This
  caught two real bugs during M3: a `ConstNode` cast on non-folded unary
  children, and `parseGoIntConst` rejecting >64-bit magnitudes.
- `ar14`: `q := string(-(1 << 70))` — gc accepts it (→ U+FFFD); the
  conversion must not truncate the magnitude to 64 bits first.
- `ValuesTest.testGoConvertIntToFloat`: `float32(2^24+1)` rounds
  half-even to `2^24` (gc-verified); an earlier draft of the test
  asserted the wrong direction and was corrected against the gc probe.

## Files

`ar01_int8` · `ar02_int16` · `ar03_int32` · `ar04_int64` · `ar05_uint8` ·
`ar06_uint16` · `ar07_uint32` · `ar08_uint64` · `ar09_int_uint` ·
`ar10_float32` · `ar11_float64` · `ar12_conv_int` · `ar13_conv_float` ·
`ar14_conv_string` · `ar15_compare` · `ar16_bool_logic` ·
`ar17_shifts_widths`.
