# `hash/maphash` wave-3 corpus

Differential corpus for the synthetic `hash/maphash` package.

## Scope boundary — VALUE DIVERGENCE (read first)

The stdlib's maphash funnels every input through `runtime.memhash`
(a `//go:linkname` into the Go runtime; AES-based on amd64), which no
pure-Go synthetic can reproduce bit-exactly. The synthetic implements
the full API over a P3-specific seeded hash: deterministic per fixed
seed, but NOT gc-value-identical. The corpus therefore pins STRUCTURAL
properties only — never exact hash values. This is an honest scope
boundary, not a hidden bug: a program asserting exact values would be
unimplementable, not scope-limited.

Retest trigger for exact values: `memhash intrinsic lands`.

## Programs

- `00_structural.go` — chunking invariance (Write/splits/WriteByte/
  WriteString), Bytes/String/Hash consistency, Seed round-trip, Reset,
  Clone independence, seed sensitivity, MakeSeed uniqueness, Sum BE
  layout + non-mutation, zero-Hash usability, Size/BlockSize, empty
  input. All assertions are seed-agnostic booleans.
- `01_panic.go` (+`.panic`) — zero-Seed `Bytes` panics with the exact
  stdlib message `maphash: use of uninitialized Seed`.
- `02_comparable.go` — generic surface: Comparable determinism,
  ComparableHasher/WriteComparable agreement with Comparable, Equal,
  all basic kinds without panicking.
