# test/bench — benchmarks vs gc (status: **new path**; not a gate)

- [suite/](suite/) + [suite.sh](suite.sh) / [suite.py](suite.py) — **new
  path**: the benchmark suite of [performance.md](../../docs/arch/performance.md),
  run under gc, gc+PGO and graalgo with outputs checked against gc, warmup
  curves, steady-state medians and JSON. `make bench-suite BENCH_ARGS=…`.
- [realistic/](realistic/) — whole-workload programs (stdlib pipelines,
  allocation and tail latency, goroutine scaling) for
  [north-star.md](../../docs/arch/north-star.md), on the same runner
  (`--dir`): `make bench-realistic`.
- [scaling/spawn_flat.go](scaling/spawn_flat.go) — goroutine spawn cost flat
  in the live count (ADR-0018), a wall-clock ratio compared with gc's output
  by the m1r runner: `make bench-spawn-flat`, weekly in ci.yml's full-suite
  job (the one benchmark that asserts; never per-push).
- `fib/`, `loop/`, `slicesum/`, `maps/`, `structs/`, `iface/`, `strbuild/`,
  `chanpingpong/` — micro-benchmarks (`main.go` each).
  [test/m1a/bench.sh](../m1a/bench.sh) runs them on the new path (`make
  bench-m1a`) and as the PE gate (`make pe-check-m1a`, per-push). v1's
  `run.sh` runner was deleted at m3-plan D3; `make bench` is `make
  bench-suite`.

**Add a benchmark:** a directory under [suite/](suite/) using the shared
harness [suite/h/](suite/h/). Results and method: [performance.md](../../docs/arch/performance.md).
