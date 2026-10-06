# P3-M4 slice corpus

Go programs exercising slice operations (GG-0020), differential vs
stock gc 1.27.1.

## Programs

| Program | Covers |
|---|---|
| `sl00_growth.go` | Append growth caps for ints and bytes, 1..32 appends; byte-identical to gc's `growslice` |
| `sl01_elem_kinds.go` | Append/copy of int8, int64, string, struct elements; values verified |
| `sl02_ops.go` | len/cap, slicing (low:high, full), copy, nil slice, zero-length append |

## Design notes

- gc's *observed* `cap(append(…))` for non-escaping appends uses a
  compiler stack-buffer optimization (sets cap to a fixed threshold,
  bypassing `growslice`). graalgo (like `reflect.Append` and `-N`
  builds) follows the pure `growslice` formula. The corpus builds
  slices through escaping functions (returned or passed out), which
  take the pure-growslice path on gc — the only path graalgo
  replicates. (This design note is the authoritative record of the
  divergence; it is not in SPEC.md.)
- Growth sequences are compared byte-for-byte: e.g. int caps for
  appends 1..17 are `1 2 4 4 8 8 8 16 16 32 …`.
