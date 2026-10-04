# Composite-types corpus (M4, WS E)

Differential tests for Go composite types, bit-identical vs stock gc 1.27.1
(`CompositeCorpusTest`; `// verifies: GG-0014`).

## Programs

| File | Covers |
|------|--------|
| co01_array.go | array literal, indexing, index assignment/incdec, keyed literal, array copy-on-assign |
| co02_slice.go | slice literal, 2-index slicing, aliasing (write through sub-slice), open-ended slices |
| co03_slice3.go | 3-index slicing (values + length via range), full slice expr, slice of array |
| co04_struct.go | struct literals (keyed + unkeyed), field access/assign, struct copy-on-assign, zero struct, `&T{}` |
| co05_embed.go | embedded struct field promotion (read + write), explicit path, zero outer |
| co06_map.go | map literal → index → assign → range, overwrite, int-keyed map, map shares on assign |
| co07_commaok.go | comma-ok map index (`:=` and `=`, present/missing key, blank value) |
| co08_multi.go | multi-dimensional: `[][]int`, `[]rec`, `map[string][]int`, `[]map[string]int`, `[2][3]int` (incl. elided literals) |
| co09_string.go | string indexing (bytes), string slicing, byte-sum loop |
| co10_assert.go | type assertion (single-valued, comma-ok present/wrong-type, named type) |

## Conventions

- Every file is gofmt-clean, has no imports and no builtins.
- Composite values are **reduced to scalar observables** inside the
  programs (sums, counts, field projections via range loops), so the
  comparison is the same bit-identical `long[]` idiom as the other corpora.
  Map reductions are order-independent by construction.
- Panics (out-of-bounds, nil deref) are stubbed as internal errors until
  WS F gives them real semantics; the corpus stays in the valid-bounds
  subspace.

## Mutation story (red-green)

- **Mutant B** (composite): off-by-one in `SliceExprNode` length
  (`h - l + 1`). Killed by 2 tests
  (`hidden_files/m4_red/mutantB_red.log`): co02, co03.
- **Mutant A** (control-flow): dropped the `!` in `ForRepeatingNode`.
  Killed by 5 tests including co09
  (`hidden_files/m4_red/mutantA_red.log`).
