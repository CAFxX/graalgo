# Wave-6: SURV-070 C4 hotness verification — iface→iface assertion sites

**Question (SURVEY.md §6 wave-6 queue item 6):** quantitative hotness of iface→iface
assertion sites (fmt/error-traversal shapes); if measured hot, revisit whether the
assertion-site dominant-type guard (C4, §3 angle on SURV-070) deserves a SURV- ID.

**Method (2026-10-04, our measurements):** Go 1.27.1 (`~/tools/go/bin/go`),
linux/amd64, AMD EPYC 9D25 (2 vCPUs, shared VM — absolute ns are VM-noisy;
relative attributions are the load-bearing signal). CPU profiles via
`go test -cpuprofile` + `go tool pprof -top -cum`. Source: `/tmp/c4hot`
(scratch; benchmarks: BenchmarkFmtMixed, BenchmarkFmtStringer,
BenchmarkFmtError, BenchmarkErrorsAs, BenchmarkIfaceAssert, BenchmarkIfaceAssertVary).

## 1. fmt / errors.As workloads (site-level hotness)

Aggregate profile of the four workload benchmarks (2.36s captured samples):

- `fmt.(*pp).printArg` 33.05% cum, `errors.As` 28.39% cum, `fmt.(*pp).handleMethods`
  13.98% cum, `errors.as` 27.12% cum — the assertion-bearing sites are HOT in
  these shapes, as expected (fmt does per-operand `any`→Formatter/Stringer/error
  assertions; `errors.As` asserts per unwrap step).
- The itab-materialization residual: `runtime.getitab` 0.85% cum,
  `runtime.assertE2I (inline)` 0.85% cum. Sub-1% of the workload — the assertion
  *sites* are hot, the itab *probe path* is not.

## 2. Isolated assertion site — where exactly is the cost?

- Monomorphic site (`x.(narrowIface)` with invariant concrete type, dominant-profile
  proxy): **4.78 ns/op** — the site-cache hit path. That is the TOTAL site cost,
  assertion included.
- Three concrete types rotating through one site (cache-thrash proxy):
  **10.94 ns/op**; `runtime.typeAssert`+`getitab`+`itabTableType.find` ≈ 20% of
  captured site samples.
- Interpretation: gc's per-site inline cache (dottype1: nil check → atomic-load
  site-cache probe → `typeAssert`→`getitab` only on miss) already reduces the
  dominant-profile case to a ~few-cycle probe. The `getitab` global-table path only
  fires on site-cache miss (polymorphic thrash).

## 3. What the C4 guard would actually remove

C4's proposed guard: `dynamic-type(x) == T0` (one type-word compare) → load
precomputed constant `itab(T0, I2)`, skipping the site-cache probe. On the
dominant-profile hit path it replaces the probe (atomic load + hash&mask) with a
type-word compare + constant load — a saving of at most a couple of cycles out of
~12 total site cycles, i.e. a single-digit-% fraction of a site that is itself a
few-% slice of realistic workloads. It requires profile infra (E4) + guard-budget
coordination (SURV-055/SURV-070) to harvest that.

**Verdict: does NOT clear the bar — no new SURV- ID.**
The hotness condition holds (assertion sites are hot in fmt/error-traversal
shapes), but the *residual* condition fails: gc's site cache already minimizes the
probe; the guard's marginal saving (~cycles, not tens of cycles) on the dominant
path cannot be load-bearing. The cache-thrash case (where the probe IS hot) is
exactly the case the dominant-type guard does NOT help. C4 remains a §3 angle on
SURV-070 with hotness now measured; re-open only if a workload is found where
dominant-profile assertion sites exceed ~5% of wall time (none found here).

Caveats: sampling-based; 2-vCPU shared VM (relative shares robust, absolute ns
noisy); profile capture fraction ~20–44% of wall — relative attributions used
throughout. Numbers are ours, measured 2026-10-04, not source claims.
