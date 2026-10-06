# ADR-0001 addendum — P3-D1 re-examination (P3-M6, 2026-10-07)

## Trigger
PLAN.md §3 (P3-M6): re-measure the coupled-package set on go1.27.1; drift
>15% vs SPEC §2.8's ~78 re-opens P3-D1 (fork-vs-overlay boundary).

## Measurement
`tools/stdlib-excl measure` (strict pure-Go check, documented in
`phases/phase3/stdlib-fork/coupled-packages-go1.27.1.md`): **142 coupled
of 399 package dirs (36%)**, vs SPEC's ~78. Drift = 82% — trigger fires.

## Decomposition (from coupled-packages-go1.27.1.json)
| reasons | count | fork implication |
|---|---|---|
| build-tags only | 47 | mechanical: P3-M1 build-tag filtering already handles; no source adaptation |
| linkname only | 26 | needs GG-0021 symbol surface (waves M7–M9); sources compile unchanged once provided |
| build-tags + linkname | 25 | as above |
| asm (+build-tags ± linkname) | 32 | real adaptation: asm strategy (ADR-0007) |
| cgo (±others) | 12 | real adaptation: Panama FFI strategy (P3-M8) |

Substantive adaptation set (asm/cgo): ~44 packages — within the spirit of
the original ~78. The 47 build-tags-only packages were never "coupled" in
the adaptation sense; the original ~78 came from an imprecise, undocumented
check, while the new 142 uses a strict documented one. Part of the drift is
definitional, part is real (new `arena`, `crypto/boring`, `simd/*` packages;
linkname in 69 dirs).

## Verdict: P3-D1 STANDS
The fork model (real, lightly adapted sources for coupled packages; never
re-synthesized) holds. What changes is precision, not the boundary:
- The coupled set is now a versioned, per-dir-reasoned list instead of a
  hand-wavy ~78.
- The 133 build-tagged packages are explicitly NOT fork-adaptation work —
  they are importer-filtering work, already built.
- The linkname skip-list (525, from the linkname-diff trial) is the
  measured long tail; waves M7–M9 drive it down. SPEC §3.6's kill trigger
  (skip-list growth >15 twice per release) watches it.
- Per-release re-measurement re-fires this examination on >15% moves.

## Consequences
- M7–M9 wave planning uses the 142-list with per-dir reasons, not the ~78.
- No change to overlay graduation: synthetic overlays remain only for
  `runtime` (Goexit/SetFinalizer/KeepAlive/GC) and `fmt` (seven D4 symbols).
