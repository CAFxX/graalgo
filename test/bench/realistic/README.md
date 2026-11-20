# test/bench/realistic — whole-workload benchmarks (not a gate)

Programs shaped like the workloads of [north-star.md](../../../docs/arch/north-star.md)
(tier T1/T2 stand-ins that run on the new path today), on the
[suite](../suite/) harness and runner: `make bench-realistic BENCH_ARGS=…`
(gc legs at `GOMAXPROCS=4`, like the JVM's `-XX:ActiveProcessorCount=4`;
peak RSS and CPU time per leg; `STAT` lines collected).

| Program | Workload |
|---|---|
| `jsonrt` | `encoding/json` Marshal/Unmarshal of API-shaped structs |
| `regexscan` | `regexp` match/submatch over a generated log corpus |
| `textproc` | word frequencies: `strings`, maps, `sort.Slice`, `fmt.Sprintf` |
| `tmplrender` | `text/template` page rendering |
| `reqpipe` | handler-shaped: JSON decode, regexp check, map under a mutex, JSON encode through `bufio`/`io.Writer` |
| `deflate` | `compress/flate` round trip of 1 MiB of text |
| `allocmix` | live heap ~100 MB plus allocation churn; unit-latency p50/p99/p99.9/max |
| `goroutines` | `GOR_N` goroutines: spawn, idle RSS per goroutine, lifecycle rounds (`make bench-goroutines`: 10k/100k/1M, graalgo in carrier mode, ADR-0018) |

GC matrix: `BENCH_ARGS='--gg-variant par=-XX:+UseParallelGC --gg-variant
zgc=-XX:+UseZGC allocmix'`. They need the M1d stdlib (`fmt`, `reflect`,
`os`); on a tree without it the graalgo leg fails at link. Add a program:
a directory with `main.go` calling `h.Run` from `graalgo/bench/h`.
