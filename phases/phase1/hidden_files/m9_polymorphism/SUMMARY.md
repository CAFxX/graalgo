# M9 polymorphism measurement — summary

Programs: 289 total; 24 rejected at build (out of WS scope); 0 frontend timeouts; 263 executed with probing; 2 produced no snapshot (exit-1); 12 panic-truncated (partial traces, excluded from the headline histogram).
Methodology: 30s per-program probed-run timeout; site names are `<funcdecl-objId>@<file>:<line>:<col>` (per-instantiation sites do not merge); deferred calls probed.
Call sites measured (full traces): 170 (method sites with receiver data: 13).

## Call-site target cardinality — full traces only
  (distinct compiled targets per site)

| targets | sites | % |
|---|---|---|
| 1 (monomorphic) | 169 | 99.4% |
| 2 (bimorphic) | 1 | 0.6% |

Monomorphic: 169 / 170 (99.4%).
Megamorphic (>5 targets): 0 / 170 (0.0%).

Truncated traces (excluded above): 9 sites, histogram {1: 9}.

## Method receiver-type cardinality

In Phase 1, method calls resolve statically (`methodref`), so the
receiver's static type is fixed per site: receiver cardinality is 1
for every executed method site. This histogram is scaffolding for
WS D (interface dispatch), not signal yet.
Method sites with receiver data: 13; histogram: {'1': 13}.

## Generic instantiations (static, from frontend/v1 documents)

Generic origins: 17; total instantiations: 25.
Per-origin instantiation-count histogram: {'1': 9, '2': 8}.

Full per-site data: `report.json`.
