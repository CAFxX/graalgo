# ADR-0023 — Closure environments are `Object[]` capture arrays (amendment A9)

**Date:** 2026-11-25
**Status:** accepted by evidence (lane perf-frames, WP-TR-AUDIT-5 of
[truffle-audit.md](../../arch/truffle-audit.md), finding F-8.1); implemented
in full; the coordinator confirms. Decides SPEC amendment **A9**
([truffle-design.md](../../arch/truffle-design.md) T7), which the owner
deferred pending benchmarks on 2026-11-18.
**Scope:** the closure representation of the new path
(`graalgo.go.values.Closure`, `graalgo.go.values.Env`, `Calls.MakeClosure`,
`Calls.Bind1`, `Calls.EnvRead`, the intrinsics that build func values).
SPEC §3.4 prose only; no requirement text changes.

## Context

SPEC §3.4 described a func value as `{RootCallTarget, MaterializedFrame|null}`.
A9 proposed capturing typed cells instead of a `MaterializedFrame`. F-8.1
found that the as-built code already never materializes the creating
function's frame (captured variables live in `Flat` boxes, so a closure
holds the boxes by reference; the one `materialize()` is range-over-func's
yield body, unrelated). What remained was the environment's own cost: a
fresh `MaterializedFrame` per literal evaluation — the frame object, an
`Object[]` and a `long[]` slot array and a `byte[]` tag array — to hold N
references that are never written after creation.

## Decision

`Closure(RootCallTarget target, Object env)`, where `env` has the layout of
the capture count `size`, which every creator and reader knows statically
(`Env` is the only code that knows the layout):

| captures | `env` | allocation per evaluation |
|---|---|---|
| 0 | `null`; the literal is **one constant func value** (gc's static closure: func values are not comparable, so no program can tell evaluations apart) | none |
| 1 | the captured box (or bound receiver, or an intrinsic's state) itself | the `Closure` only |
| ≥ 2 | `Object[size]` | `Closure` + one array |

`EnvRead(index, size)` reads all three layouts with `size` a constant;
method values (`Bind1`), range-over-func's yield closure and the reflect /
reflectlite func values use the same layouts. Typed per-closure cell arrays
(a `long[]` beside the `Object[]`) were not adopted: a captured variable is
captured by reference, so what the environment holds is always a reference
to its box — there is no primitive to unbox.

## Evidence

Interleaved A/B (master `04e68ec5` vs the lane, 3 reps each, medians,
graalgo `-Xmx2g -XX:ActiveProcessorCount=4` under `nice -n 19`, host load
8–12; ratio = graalgo steady median / gc):

| Program | A ×gc | B ×gc |
|---|---:|---:|
| `closurealloc` (escaping closures of 0–3 captures; scratch program) | 3.24× | **2.04×** |
| `sortslice` (`sort.Slice` with a capturing less, `slices.All` range-over-func; scratch program) | 1.35× | **1.22×** |
| `closures` (suite: non-escaping literals, `counter()`) | 1.38× | 1.39× |

- **Escape analysis.** `closures` does not move: its literals are passed to
  callees that inline, and partial escape analysis already scalar-replaced
  the old `MaterializedFrame` environment there as well as the new one. The
  gain is where closures escape (stored, returned, sent, handed to a
  non-inlined callee): `closurealloc` 1.6× faster.
- **Memory per closure** (JDK 25 layout, compressed oops, 12-byte headers):
  before `Closure` 24 B + frame 32 B + `Object[n]` + `long[n]` + `byte[n]`
  (≥ 24 B each) = 128 B for one capture, 136 B for two; after 0 B (no
  capture), 24 B (one), 48 B (two), 56 B (three).
- Suite and realistic geomeans move within the host's noise
  ([performance.md](../../arch/performance.md) §8.3).

## Consequences

- SPEC §3.4 states closures as `{RootCallTarget, env}` with the layouts
  above; truffle-design T7's A9 row is decided.
- A capture-free literal's identity is shared across evaluations; only
  `reflect.Value.Pointer`-style inspection could observe it, and gc shares
  it the same way.
- `Env.get` takes the layout size: a caller that passes the wrong size reads
  a wrong value (`FrameEnvTest` pins every layout).
- Re-open if a future value model captures variables by value (no box): then
  typed cells become worth measuring again.
