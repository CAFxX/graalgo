# test/bench — benchmarks vs gc (status: **mixed**; not a gate)

- [suite/](suite/) + [suite.sh](suite.sh) / [suite.py](suite.py) — **new
  path**: the benchmark suite of [performance.md](../../docs/arch/performance.md),
  run under gc, gc+PGO and graalgo with outputs checked against gc, warmup
  curves, steady-state medians and JSON. `make bench-suite BENCH_ARGS=…`.
- `fib/`, `loop/`, `slicesum/`, `maps/`, `structs/`, `iface/`, `strbuild/`,
  `chanpingpong/` — micro-benchmarks (`main.go` each). [run.sh](run.sh) runs
  them on **v1** ([graalgo-sync-run](../../internal/backend/graalgo-sync-run))
  as `make bench`; [test/m1a/bench.sh](../m1a/bench.sh) runs the same
  workloads on the new path (`make bench-m1a`) and as the PE gate
  (`make pe-check-m1a`, per-push).

**Add a benchmark:** a directory under [suite/](suite/) using the shared
harness [suite/h/](suite/h/). Results and method: [performance.md](../../docs/arch/performance.md).
