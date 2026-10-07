# P3-M7 math: Red-Green Evidence

Non-tautological evidence that the math synthetic package tests can fail
and do fail for the right reasons.

## 1. Differential corpus (5/5 pass)

`phases/phase3/corpus/wave1/math/run_differential.sh` compares graalgo
vs gc 1.27.1 byte-for-byte on stdout (programs print with `%x` / `%.17g`).

| Test | Coverage | Result |
|------|----------|--------|
| m00_bits | Float64bits/frombits, Float32bits/frombits, Sqrt, FMA (incl. NaN payloads, ±Inf) | PASS |
| m01_transcendentals | Sin/Cos/Tan/Exp/Log/Asin/Atan/Sinh/Cosh/Tanh/Cbrt/Expm1/Log10/Log2/Log1p on adversarial values (subnormals, MaxFloat64, 1e±300, overflow boundaries) + NaN payload propagation + ±Inf | PASS |
| m02_binary | Pow/Atan2/Hypot/Mod/Remainder/Max/Min/Dim/Copysign on pairs incl. Inf/NaN/±0; Ldexp extreme exponents; Nextafter | PASS |
| m03_special | Floor/Ceil/Trunc/Round/RoundToEven (incl. sNaN quieting); Abs/Signbit/IsNaN/IsInf; constants | PASS |
| m04_bessel | J0/J1/Y0/Y1/Gamma/Erf/Erfc/Jn/Yn/Erfinv/Erfcinv | PASS |

## 2. Red-green demonstrations (non-tautological)

### R1: mathbitsLen64 bug (caught by m01)
During development, my hand-rolled `mathbitsLen64` (replacing gc's
`len8tab` string table, which WS E cannot index) computed wrong bit
lengths: for x=0xFF it returned 10 instead of 8. This broke
`mathbitsLeadingZeros64`, which broke `trigReduce` (Payne-Hanek), which
made `Sin(1e300)` return -0.9758 instead of gc's -0.8178.

**Red**: `m01_transcendentals` FAILED with stdout diff on `sin 1e300`.
**Green**: After fixing `mathbitsLen64` to use explicit range checks,
all 5 tests pass. The test is non-tautological: it failed with the
wrong implementation and passes with the right one.

### R2: RoundToEven shift underflow (caught by m03)
`RoundToEven(1e300)` panicked with "negative shift amount". The gc
code does `bits>>(shift-e)` where `e` is uint; for large e, `shift-e`
wraps to a huge uint (bits>>huge==0). The graalgo exporter computed the
shift as signed, causing a panic.

**Red**: `m03_special` FAILED with `panic: runtime error: negative shift amount` in `RoundToEven`.
**Green**: After adding a guard (`if e < shift`), the test passes and
matches gc's output (`1.0000000000000001e+300`). Non-tautological: the
panic was real, the fix is verified by byte-identical output.

### R3: Missing overlayImporter enable (caught by import)
When `math` was added to the import allowlist but not to the
`overlayImporter` enable check in `export.go`, any program importing
`math` failed with `could not import math`.

**Red**: `gofrontend /tmp/tmath.go` → `type error: could not import math`.
**Green**: After adding `|| p == "math"` to the enable check, imports
work. Non-tautological: the error was observed, the fix verified.

### R4: Unqualified intrinsic calls (caught by m00)
The synthetic math source calls `Float64bits`/`Float64frombits`
internally (e.g., in `Abs`, `NaN`). The exporter skipped intrinsics in
`funcByObj`, so these internal calls failed with `call to unknown
function Float64frombits`.

**Red**: `gofrontend` → `call to unknown function Float64frombits`.
**Green**: After adding `isMathIntrinsicFunc` interception in
`exportFuncCall`, internal calls lower to wire nodes correctly.
Non-tautological: the failure was observed before the fix.

## 3. Bit-exactness decisions (per function)

| Function | Decision | Evidence |
|----------|----------|----------|
| Sqrt, FMA | JVM intrinsic (StrictMath) | 0 mismatches / 50,071 adversarial inputs vs gc 1.27.1 |
| Float64bits/frombits, Float32bits/frombits | JVM intrinsic (bit ops) | Exact by construction |
| Exp | Pure Go (transliterated amd64 FMA path) | 0 mismatches / 50,071 |
| Log | Pure Go (transliterated amd64) | 0 mismatches, bit-exact |
| Sin/Cos/Tan | Pure Go (gc trig_reduce.go + kernel) | 0 mismatches on corpus; MaxFloat64 verified |
| All others | Pure Go (verbatim gc) | Differential corpus 5/5 pass |

**Why not StrictMath for transcendentals**: Differential testing showed
StrictMath is NOT bit-identical to gc 1.27.1 for Sin/Cos/Tan/Exp/Log
etc. (11k-35k mismatches on 50k inputs). gc preserves NaN payloads;
StrictMath does not. gc's amd64 impls are <1-ulp, not correctly rounded.
Only Sqrt and FMA matched (JVM intrinsics).
