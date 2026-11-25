# ADR-0021 — Go strings stay `Str`; TruffleString only as kernels and views

**Date:** 2026-11-25
**Status:** accepted 2026-11-25 by the coordinator (lane research-tsbc,
WP-TR-AUDIT-12 of [truffle-audit.md](../../arch/truffle-audit.md), finding
F-9.1). Follow-up: the cached hash in `Str` and the TruffleString search
kernels over `Str` views go to the string/map perf work (WP-TR-AUDIT-4)
**Scope:** the string representation of the new path
(`graalgo.go.values.Str`, `graalgo.go.runtime.Strs`, the bytealg
intrinsics). Evidence: [tstring-bytecode-study.md](../../arch/tstring-bytecode-study.md) §A.

## Context

F-9.1 proposed prototyping `TruffleString` for `string`: cached hash, lazy
concatenation, vectorized compare/search stubs, zero-copy interop. The new
path compiles the Go standard library from upstream source, so the
representation is touched only by the runtime's own operations (`==`, `<`,
`+`, map hashing, conversions, slicing, `range`, and the `internal/bytealg`
intrinsics); everything in `strings`/`bytes`/`strconv` is Go code over bytes.

## Decision (proposed)

1. **Keep `Str { byte[], off, len }`** as the representation (SPEC §3.4,
   ADR-0015 decision 3 unchanged).
2. **Cache the hash in `Str`** (an `int` field; it fits the object's
   padding: 32 bytes before and after). Measured in the product: `mapstr`
   1.27 → 0.62 ms (2.0×; 1.5× gc → 0.73× gc); no change elsewhere.
3. **Run TruffleString's search kernels over zero-copy views** in the
   `IndexByteString`/`IndexString` (and the `[]byte` `IndexByte`/`Index`)
   intrinsics: nodes in the intrinsic roots instead of `@TruffleBoundary`
   SWAR loops (in isolation 3.4× and 7× faster). Optionally the same for
   `range` decoding, after the per-rune node overhead
   ([performance.md](../../arch/performance.md) §5 item 4) is fixed.
4. **Interop** (WP-TR-AUDIT-13) produces a `TruffleString` view of a `Str`
   window at the boundary (`FromByteArrayNode`, `copy=false`) instead of
   changing the representation.

## Consequences

- Avoided: +8 B per string, 2–20× slower string operations before JIT, a
  code-range scan on every `UTF_8` creation, and an L migration through
  `Flat`/`unsafe`/`reflect`/syscall byte roles and descriptor images.
- Lost: lazy concatenation (2.4× on a `+=` chain; idiomatic Go builds with
  `strings.Builder`, which is unaffected).
- The cached hash (like any TS hash cache) is stale if a program mutates the
  bytes under an `unsafe.String`; Go makes that undefined behavior.
- Re-open when a polyglot-interop milestone needs strings that are
  `TruffleString`s end to end, with the study's gate
  (`strbuild`, `striter`, `mapstr`, `textproc`, `regexscan` at ≥ parity).
