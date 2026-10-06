# M9 polymorphism re-measurement — P3-M3 interface dispatch

Programs: 21 total (11 iface corpus + 10 interface-heavy probes); all executed with probing; 30s-equivalent per-program budget (120s cap, none hit).
Methodology: `--dump-polymorphism` on P3SyncMain enables the WS D `InterfaceDispatchProbes` hooks; each `ifacemethod` node records the itab *identity* per dispatch into a per-site histogram. Site names are `iface:<funcdecl-objId>@<file>:<line>:<col>#<method>` (per call site — two textual sites never merge). Deferred calls are probed (same node).
Call sites measured (full traces): 31; total probed dispatches: 1227.

## Call-site itab cardinality — full traces only
  (distinct dynamic types per interface call site)

| itabs | sites | % |
|---|---|---|
| 1 (monomorphic) | 13 | 41.9% |
| 2 (bimorphic) | 11 | 35.5% |
| ≥3 (megamorphic) | 7 | 22.6% |

Monomorphic: 13 / 31 (41.9%).
Megamorphic (≥3 itabs): 7 / 31 (22.6%).

## Per-site data (full traces)

- [bi] if00_basic_dispatch.go :: iface:3@if00_basic_dispatch.go:19:44#Write: itabs=2 dispatches=3
- [mega] if01_multi_writer.go :: iface:3@if01_multi_writer.go:27:9#Write: itabs=3 dispatches=3
- [bi] if02_sort_iface.go :: iface:0@if02_sort_iface.go:17:7#Len: itabs=2 dispatches=2
- [bi] if02_sort_iface.go :: iface:0@if02_sort_iface.go:19:24#Less: itabs=2 dispatches=21
- [bi] if02_sort_iface.go :: iface:0@if02_sort_iface.go:20:4#Swap: itabs=2 dispatches=15
- [bi] if03_error_iface.go :: iface:3@if03_error_iface.go:32:16#Error: itabs=2 dispatches=3
- [mono] if04_assert_iface.go :: iface:1@if04_assert_iface.go:19:14#Write: itabs=1 dispatches=1
- [mega] if05_megamorphic.go :: iface:4@if05_megamorphic.go:28:12#Write: itabs=4 dispatches=16
- [mono] if07_typed_nil.go :: iface:1@if07_typed_nil.go:21:14#Write: itabs=1 dispatches=1
- [mono] if08_embedded_promotion.go :: iface:2@if08_embedded_promotion.go:26:14#Write: itabs=1 dispatches=1
- [mono] if08_embedded_promotion.go :: iface:2@if08_embedded_promotion.go:32:14#Size: itabs=1 dispatches=1
- [mono] if09_iface_embedding.go :: iface:2@if09_iface_embedding.go:28:2#Write: itabs=1 dispatches=1
- [mono] if09_iface_embedding.go :: iface:2@if09_iface_embedding.go:29:9#Close: itabs=1 dispatches=1
- [mono] if10_iface_param.go :: iface:1@if10_iface_param.go:15:7#Write: itabs=1 dispatches=1
- [mono] if10_iface_param.go :: iface:1@if10_iface_param.go:16:7#Write: itabs=1 dispatches=1
- [mono] if10_iface_param.go :: iface:2@if10_iface_param.go:28:7#Write: itabs=1 dispatches=1
- [mono] if10_iface_param.go :: iface:3@if10_iface_param.go:38:14#CopyFrom: itabs=1 dispatches=1
- [mono] mp01_hot_mono.go :: iface:1@mp01_hot_mono.go:15:10#Add: itabs=1 dispatches=1000
- [bi] mp02_bimorphic.go :: iface:2@mp02_bimorphic.go:24:12#Size: itabs=2 dispatches=100
- [mega] mp03_many_types.go :: iface:6@mp03_many_types.go:35:10#ID: itabs=6 dispatches=6
- [mega] mp04_errors.go :: iface:4@mp04_errors.go:32:16#Error: itabs=3 dispatches=9
- [bi] mp05_sorters.go :: iface:4@mp05_sorters.go:47:15#Name: itabs=2 dispatches=4
- [bi] mp05_sorters.go :: iface:4@mp05_sorters.go:47:25#Sort: itabs=2 dispatches=4
- [mono] mp06_nested_iface.go :: iface:2@mp06_nested_iface.go:14:9#Value: itabs=1 dispatches=1
- [mono] mp06_nested_iface.go :: iface:3@mp06_nested_iface.go:20:7#Double: itabs=1 dispatches=5
- [bi] mp07_value_pointer_mix.go :: iface:2@mp07_value_pointer_mix.go:24:12#Next: itabs=2 dispatches=10
- [bi] mp08_recursive.go :: iface:1@mp08_recursive.go:15:36#Sum: itabs=2 dispatches=3
- [bi] mp08_recursive.go :: iface:1@mp08_recursive.go:15:51#Sum: itabs=2 dispatches=3
- [mega] mp09_embedded_3impl.go :: iface:6@mp09_embedded_3impl.go:28:7#Write: itabs=3 dispatches=3
- [mega] mp09_embedded_3impl.go :: iface:6@mp09_embedded_3impl.go:29:8#Close: itabs=3 dispatches=3
- [mega] mp10_nonstrukt.go :: iface:3@mp10_nonstrukt.go:22:15#String: itabs=3 dispatches=3

## Program outcomes

- if00_basic_dispatch.go (corpus): exit=0 sites=1
- if01_multi_writer.go (corpus): exit=0 sites=1
- if02_sort_iface.go (corpus): exit=0 sites=3
- if03_error_iface.go (corpus): exit=0 sites=1
- if04_assert_iface.go (corpus): exit=0 sites=1
- if05_megamorphic.go (corpus): exit=0 sites=1
- if06_nil_panic.go (corpus): exit=2 sites=0 TRUNCATED
- if07_typed_nil.go (corpus): exit=0 sites=1
- if08_embedded_promotion.go (corpus): exit=0 sites=2
- if09_iface_embedding.go (corpus): exit=0 sites=2
- if10_iface_param.go (corpus): exit=0 sites=4
- mp01_hot_mono.go (probe): exit=0 sites=1
- mp02_bimorphic.go (probe): exit=0 sites=1
- mp03_many_types.go (probe): exit=0 sites=1
- mp04_errors.go (probe): exit=0 sites=1
- mp05_sorters.go (probe): exit=0 sites=2
- mp06_nested_iface.go (probe): exit=0 sites=2
- mp07_value_pointer_mix.go (probe): exit=0 sites=1
- mp08_recursive.go (probe): exit=0 sites=2
- mp09_embedded_3impl.go (probe): exit=0 sites=2
- mp10_nonstrukt.go (probe): exit=0 sites=1

## Comparison with Phase-1 M9

Phase 1 measured 170 static call sites over 263 executed programs: 169 monomorphic (99.4%), 1 bimorphic (0.6%), 0 megamorphic. Those were *statically-resolved* method calls (`methodref`) — receiver cardinality was 1 by construction; the histogram was scaffolding awaiting WS D.

This re-measurement covers what Phase 1 could not: *interface* dispatch
sites, where the dynamic type genuinely varies at runtime. The honest
reading of the numbers:

- The distribution (41.9% mono / 35.5% bi / 22.6% mega) does NOT
  describe real Go workloads -- it describes this probe set, which was
  deliberately built to exercise the polymorphism spectrum (if05 feeds
  4 types through one site; mp03 feeds 6; mp02/mp05/mp07/mp09 are
  bimorphic by construction). An adversarial exercise set measures
  mechanism coverage, not workload shape.
- What the re-measurement does establish: the dispatch machinery is
  correct across the whole spectrum -- every site above dispatched
  correctly (the corpus differential is 11/11 byte-identical, the
  probes 10/10 vs gc), the PIC serves the mono/bi sites, and the
  megamorphic hash fallback serves if05/mp03/mp04/mp09/mp10.
- The Phase-2 plan's re-measure trigger asked whether the 99.4%
  monomorphic finding survives interface dispatch. That question needs
  a workload-representative program set (real interface-heavy Go code,
  not micro-probes) -- deferred to Phase 4, when the frontend covers
  enough of the language to run one. This measurement is the mechanism
  check, not the workload verdict.

Per-site machine data: `polymorphism_report.json` (this directory).
