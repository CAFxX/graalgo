# North star: long-running Go programs, significantly faster than gc

Status: strategy + measurement record, first written 2026-11-20 (lane
`north-star`). The goal is the owner's statement of 2026-11-20, recorded as
`[DECIDED]` in [SPEC.md](SPEC.md) §1.1; this document says what it means
operationally, how it is measured, what blocks it today, and how the
roadmap ([RECOVERY.md](RECOVERY.md)) is re-ordered around it. Measurement
method and micro-benchmark numbers stay in [performance.md](performance.md);
the realistic programs are [test/bench/realistic/](../../test/bench/realistic/).

> **Primary goal (owner, 2026-11-20):** run long-running programs
> significantly faster than the vanilla `go` compiler does — any
> long-running program, from local CPU-bound utilities to large I/O-bound
> servers (Kubernetes, databases, …). Start-up and short runs are secondary.

## 1. What "significantly faster" means

A claim is made per workload, against gc **and** gc+PGO (SPEC §2.6: every
win must beat gc with PGO), on the same machine, at steady state, with the
same CPU budget (gc at `GOMAXPROCS=N` against the JVM at
`-XX:ActiveProcessorCount=N`). One of the following must hold, and none of
the guard rails may be broken:

| Metric | Definition | "Significantly faster" | Guard rail (must not be worse than) |
|---|---|---|---|
| Throughput | ops/s (requests/s, MB/s, rounds/s) at steady state: after time to peak, median of ≥ 5 interleaved reps | **≥ 1.3×** gc+PGO | — |
| Tail latency | p99 and p99.9 of a unit of work (request, transaction) at a fixed offered load ≤ 70 % of gc's saturation, GC pauses included; open-loop load (no coordinated omission) | **≥ 2× lower** p99.9 | p50 ≤ 1.1× gc |
| CPU per op | (user + system CPU of the whole process, JIT and GC threads included) / ops at equal offered load | **≥ 30 % less** | — |
| Memory | peak RSS at steady state | — | ≤ 2× gc's RSS for heaps ≥ 1 GB; ≤ gc + 300 MB below that |
| Time to peak | process start → first round within 1.10× of the steady median | — | amortized: ≤ 1 % of the run's wall time for a server benchmark (a 30 min run tolerates 18 s), ≤ 10 % for a utility run |

"Long-running" is operational: the program runs long enough for the
warmup budget above. A 2 s CLI invocation is not in scope (secondary goal:
AOT/hybrid modes, §5.9); a 2 min `hugo` build or a 30 min batch job is.
Ratios under 1.2× on the shared development host are noise
([performance.md](performance.md) §2); a claim is made on a quiet,
dedicated host and reported with its interval (§3.4).

## 2. Workload tiers

Each tier: the runtime properties that dominate it, what blocks graalgo
**today** (capability: the program does not link or run), and what is a
**performance** question once it runs. Capability state is master
`a366807f` (M1d landed; [toolexec-design.md](toolexec-design.md) §13),
probed with `go build -toolexec graalgo-tool` of a blank import per package
(no `-tags=purego`, unlike the launcher's ADR-0010 deviation, which hides
some assembly paths) and small programs that open a socket, run a
finalizer and call `runtime.Callers`.

### T1 — CPU-bound utilities

`compress/gzip`, `regexp`, `encoding/json`, `sort`, crypto hashes,
AES-GCM, TLS handshakes, `math/big`, image codecs, `text/template`;
Hugo-, ripgrep-, scc-class tools.

- **Dominated by:** straight-line and loop code over `[]byte`/`string`,
  reflection (json, template), interface calls through `io.Reader`/
  `io.Writer`/`hash.Hash`, allocation of short-lived values; in gc,
  assembly kernels (crc32, sha256, AES-NI, `math/big`, `bytealg`).
- **Blocks today:** `hash/crc32` (3 undefined assembly symbols; blocks
  `compress/gzip`, `image/png`, `archive/zip`), `crypto/*` (`crypto/sha256`
  24 symbols: FIPS-140 indicators and assembly; `crypto/aes`, `crypto/md5`
  likewise), `math/big` (6, assembly); `runtime.Callers` returns no frames
  (BUG-0047: `log` with `Lshortfile`, `testing`'s helpers); finalizers
  never run. Links and runs: `encoding/json`, `regexp`, `text/template`,
  `html/template`, `encoding/csv`/`xml`, `compress/flate`, `hash/fnv`,
  `bufio`, `fmt`, `os` files.
- **Performance questions:** compiled code quality on byte loops and
  `goto`-heavy state machines (§4.1: today's PE failures), reflection speed,
  assembly kernels as intrinsics vs gc's assembly, the devirtualization
  cascade on `io.*` interfaces (SPEC §2.6.1).

### T2 — `net/http` servers

JSON APIs, static files, reverse proxies, gRPC-like RPC.

- **Dominated by:** goroutine per connection (10k–100k live, churn per
  request), netpoller wakeups and `read`/`write`/`accept4`/`epoll_wait`
  system calls, `bufio`, `encoding/json` or protobuf (reflection or
  generated code), allocation per request, `sync.Pool`, TLS (AES-GCM,
  ECDHE, SHA-256), `time` timers per connection, `context`.
- **Blocks today:** `net` links but no socket call is in the role table
  (`socket(2)`, number 41, is a fatal error on first use); `net/http`
  does not link (imports `crypto/tls`: the crypto symbols above); the
  poller exists ([Poller.java](../../internal/backend/truffle/src/main/java/graalgo/syscall/Poller.java))
  but is exercised only by pipes; `writev`/`sendmsg` (pointers to values
  holding pointers) are not expressible in the role table yet; **goroutine
  creation is O(live goroutines)** (§4.3) — with 10k live goroutines a
  `go` statement costs ~0.4 ms, with 100k ~3 ms (gc: ~1 µs).
- **Performance questions:** cost per goroutine (spawn, park, wake,
  memory), syscall downcall overhead per request, the scheduler hop
  (`chanpingpong` 3× gc), JSON/reflect speed, GC tail latency under a
  request-scoped allocation pattern, TLS crypto speed.

### T3 — embedded databases and KV stores

bbolt, badger, pebble, `modernc.org/sqlite` (pure Go), etcd's raft.

- **Dominated by:** `mmap`'d files and `unsafe` over them (bbolt),
  `pread`/`pwrite`/`fsync`, large byte slices and slab allocators, skip
  lists and B-trees (pointer chasing), CRC32 (Castagnoli) and xxhash
  (assembly in gc), `sync/atomic`, compression (snappy/zstd), big heaps.
- **Blocks today:** `mmap` (W8b foreign memory: not started; an `mmap`'d
  `[]byte` must be a view over off-heap memory, GG-0072's pointer model),
  `hash/crc32`, assembly in third-party modules (xxhash, zstd) — each needs
  its `purego`/`noasm` tag or an intrinsic, `os/signal`, finalizers.
- **Performance questions:** `unsafe` and the flat value model's cost
  (checked conversions, view pointers), off-heap buffers vs `byte[]`,
  heap sizing for multi-GB live sets, `fsync`-bound paths where nothing can
  be won (parity is the goal).

### T4 — large systems

etcd, Prometheus, Caddy, CockroachDB, Dolt, kube-apiserver, kubelet,
controller-manager.

- **Dominated by:** everything in T2 + T3, plus gRPC and protobuf
  (reflection on descriptors, generated code), 1–30 GB heaps with
  millions of small objects (watch caches, informers, TSDB head blocks),
  10k–1M goroutines, `os/exec` (kubelet), signals, `runtime/pprof`,
  `runtime.Callers` (logging, tracing), `plugin` (rare), cgo
  (CockroachDB: GEOS, jemalloc-less builds are possible; kubelet with
  cgo-backed DNS resolver is optional).
- **Blocks today:** all of T2/T3; `os/exec` (`fork`: `runtime_BeforeFork`/
  `AfterFork` undefined; the `posix_spawn` helper, GG-0064, not started);
  `os/signal`; cgo (GG-0022, WP-CGO1–3, not started); `runtime/pprof`
  (gc's profiler has nothing to sample; JFR is the substitute, GG-0024);
  the 2 GB heap cap of the lane rules (a host rule, not a product limit).
- **Performance questions:** GC pause and footprint at 10–30 GB live,
  goroutine memory at 1M, warmup of a 100 MB binary's hot set, megamorphic
  call sites in generic informer/handler code.

## 3. The benchmark portfolio

### 3.1 Rules

- **Real programs, not micro-benchmarks.** Sources are vendored under
  [test/bench/realistic/](../../test/bench/realistic/) (stand-ins written for
  graalgo, stdlib only) or fetched as published modules at a pinned version
  ([test/e2e/](../../test/e2e/) style: a `go.mod` with exact versions and
  `go.sum`; never patched; build tags such as `purego`/`noasm` allowed and
  recorded).
- **Legs:** gc (`GOGC` default), gc+PGO (profile of a representative run
  of the same binary), gc tuned (the best of `GOGC` ∈ {100, 200, 400} and a
  `GOMEMLIMIT` equal to graalgo's heap cap), graalgo (the default JVM flags
  of the runner) and graalgo tuned (best GC flag set of §5.1). A claim names
  the leg it beats; "significantly faster" is against the best gc leg.
- **Same CPU budget:** gc `GOMAXPROCS=N`, graalgo
  `-XX:ActiveProcessorCount=N`; on servers the load generator runs on
  disjoint cores (`taskset`), built with gc.
- **Correctness first:** each run's output is checked against gc's (the
  `RESULT` line of the suite harness; for servers, a response digest).

### 3.2 Statistical method

Interleaved legs (gc, gc+PGO, graalgo, gc, …), ≥ 5 reps on a dedicated
host (3 on the shared one), medians of per-rep steady medians, and a
bootstrap 95 % interval of the ratio. A claim needs the interval's
lower bound above the threshold of §1 (e.g. 1.3×). Latency percentiles come
from open-loop load at fixed rates (vegeta-style constant arrival), with
HDR histograms, 3 runs of ≥ 5 min after a warmup phase that is reported
separately (time to peak, CPU spent in warmup).

### 3.3 Portfolio

| Tier | Program | Source | Load / input | Metrics | Runs on graalgo |
|---|---|---|---|---|---|
| T1 | `jsonrt`, `regexscan`, `textproc`, `tmplrender`, `deflate`, `reqpipe` | [test/bench/realistic/](../../test/bench/realistic/) | generated, deterministic | throughput, time to peak, RSS, CPU | **now** (§4.1) |
| T1 | `allocmix` (service-shaped heap) | same | live heap ~100 MB + churn | throughput, unit p50/p99/p99.9/max, RSS | **now** (§4.2) |
| T1 | `goroutines` (10k–1M) | same | — | spawn time, idle bytes/goroutine, lifecycle time | **now** (§4.3) |
| T1 | gzip/gunzip of a corpus; sha256sum; AES-GCM seal/open | stdlib programs (planned, `test/bench/realistic/`) | Silesia-like corpus | MB/s, CPU | after crc32/crypto intrinsics |
| T1 | goldmark (Markdown → HTML), gjson/encoding-json-heavy CLI, scc (line counter) | `github.com/yuin/goldmark`, `github.com/tidwall/gjson`, `github.com/boyter/scc` at pinned versions | a docs tree, a JSON dump, the Go source tree | wall, MB/s | after the module path (test/e2e on the new path) |
| T1 | Hugo site build | `github.com/gohugoio/hugo` (non-extended, pinned) | a 5k-page generated site, `--renderToMemory` | wall for repeated builds in one process (server mode) | T4-scale capability (os/exec optional, Callers) |
| T2 | JSON API: decode, validate, map lookup, encode | own server, stdlib `net/http` (`test/bench/servers/` (planned)) | open-loop HTTP/1.1 keep-alive, 1k–10k connections | req/s at saturation, p99/p99.9 at 50 % and 70 % load, CPU/req, RSS | after net + crypto link floor (§6.2) |
| T2 | static files, reverse proxy | stdlib `http.FileServer`, `httputil.ReverseProxy` | same | same + MB/s | same |
| T2 | gRPC unary + streaming | `google.golang.org/grpc` + protobuf, pinned | ghz-style client built with gc | same | after T2 floor + reflect speed |
| T3 | bbolt fill/read, pebble ycsb-A/C, badger, modernc sqlite | pinned modules | the projects' own bench drivers | ops/s, p99, RSS | after mmap/pread/fsync roles, crc32, asm tags |
| T4 | etcd put/range | `go.etcd.io/etcd` pinned + its benchmark tool (etcd repo, tools/benchmark) | 3-node on one host, then a real cluster | req/s, p99, RSS | after T2 + T3 + signals |
| T4 | Prometheus ingest + PromQL | pinned; `avalanche` as the load | 1M series | samples/s, query p99, RSS | after T3 |
| T4 | Caddy reverse proxy / file server | pinned | same as T2 | same as T2 | after T2 + TLS |
| T4 | kube-apiserver + etcd (kwok-simulated nodes) | pinned Kubernetes release | kwok 1k nodes, 30k pods churn | API p99, RSS, CPU | after T4 floor (exec, signals, Callers) |

### 3.4 Pass criteria and reporting

A program **passes** when one §1 metric clears its threshold against the
best gc leg with the interval's lower bound and no guard rail is broken.
It **regresses** when graalgo is slower than gc beyond 1.2×; regressions
are tracked as work items, not hidden. Results go to
[performance.md](performance.md) (one table per tier, dated, host and load
stated); the run's `--json` summary goes in the commit message.

## 4. Measurements (2026-11-20)

> **Update 2026-11-23 (lane perf-pe):** every failed compilation below is
> fixed (zero on all six pipelines, `make pe-report`), and the uintptr and
> checked-conversion paths of `reflect` and `strings.Builder` are cheaper:
> the pipelines' geomean is **3.7× gc at long-running steady state** (20 s
> of rounds, was 21.8× on the same host) and 10.5× in the default 4 s
> window (was 31.9×; warmup dominates). Tables, method and the remaining
> gaps: [performance.md](performance.md) §8.1. The numbers below are the
> 2026-11-20 baseline.

Host: the shared 16-core x86-64 WSL2 box ([performance.md](performance.md)
§2); graalgo legs under `nice -n 19` with `-Xmx2g
-XX:ActiveProcessorCount=4`, gc legs at `GOMAXPROCS=4`. Tree: master
`a366807f` (M1d: real `fmt`/`os`/`reflect`; §4.2–§4.3 on the
`wt/fmt/path` tree just before it merged, same backend code). Programs:
[test/bench/realistic/](../../test/bench/realistic/), runner `make
bench-realistic`. These are first numbers on a shared host — the ratios
are large enough that the conclusions do not depend on the noise.

### 4.1 Stdlib pipelines (T1 stand-ins) vs gc and gc+PGO

`make bench-realistic BENCH_ARGS="--pgo --reps 3 --gg-variant
coh=-XX:+UseCompactObjectHeaders …"`: interleaved, 3 reps, medians of the
per-rep steady medians (ms); load average 7 at the start, 34 at the end
(other lanes started). "coh" is graalgo with compact object headers.
CPU is user + system seconds of the whole ~5 s process.

| Program | gc | gc+PGO | graalgo best | graalgo steady | ×gc | ×gc+PGO | coh ×gc | time to peak | RSS MB gc / graalgo | CPU s gc / graalgo |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| `jsonrt` | 2.62 | 2.69 | 30.7 | 37.5 | 14× | 14× | 17× | 2.5 s | 18 / 932 | 4.4 / 16.3 |
| `regexscan` | 2.43 | 2.37 | 231.6 | 262.9 | 108× | 111× | 105× | 1.1 s | 20 / 1,001 | 4.3 / 11.9 |
| `textproc` | 5.67 | 5.20 | 18.6 | 96.0 | 17× | 18× | 10× | 0.7 s | 18 / 854 | 5.1 / 12.7 |
| `tmplrender` | 2.46 | 2.44 | 41.5 | 75.7 | 31× | 31× | 27× | 1.6 s | 18 / 794 | 4.1 / 15.4 |
| `reqpipe` | 1.58 | 1.53 | 38.5 | 46.8 | 30× | 31× | 30× | 1.8 s | 19 / 847 | 3.7 / 17.5 |
| `deflate` | 11.46 | 10.92 | 102.9 | 198.4 | 17× | 18× | 15× | 1.9 s | 19 / 845 | 4.2 / 15.8 |

Geometric mean graalgo/gc: **27× on steady medians, 19× on best rounds**
(the micro-benchmark suite of [performance.md](performance.md) §4 is at
1.4×). PGO moves gc by ≤ 8 % here. A 1-rep run on the pre-merge tree an
hour earlier (load 15) gave the same picture (14–130×).

Why — compilation evidence from `-Dpolyglot.engine.TraceCompilation`
(one run each, same tree):

| Program | compiled | **failed** | deopts | the failures |
|---|---:|---:|---:|---|
| `regexscan` | 85 | 1 | 1 | `regexp.(*Regexp).tryBacktrack` — "Partial evaluation did not reduce value to a constant … ValuePhi" |
| `jsonrt` | 393 | 1 | 3 | `encoding/json/internal/jsonwire.ConsumeNumberResumable` — same |
| `reqpipe` | 448 | 2 | 12 | `tryBacktrack`, `ConsumeNumberResumable` — same |
| `deflate` | 213 | 3 | 15 | `(*decompressor).huffmanBlock` — same; `(*huffmanEncoder).generate`, `(*tokens).Slice` — "Too deep inlining" |
| `tmplrender` | 316 | 1 | 102 | `text/template.(*state).evalFieldChain` — "Deopt taken too many times" |
| `textproc` | 164 | 0 | 16 | — |
| `allocmix` | 23 | 0 | 1 | — |

A failed compilation leaves the function in the AST interpreter for the
rest of the run. The three "did not reduce to a constant" functions all
contain `goto` (`backtrack.go` 13, `inflate.go` 5, jsonwire's `decode.go`
3): [Ctl.java](../../internal/backend/truffle/src/main/java/graalgo/go/nodes/Ctl.java)
`GotoBlock` merge-explodes over a program counter and asserts it
PE-constant, which fails when the label target comes out of a `Jump`
caught from a nested statement (`[VERIFIED]` for the failure messages,
the `goto` link is `[ESTIMATE]` pending the fix). `regexscan` spends its
time in exactly that function, hence 108×. The rest of the gap (`textproc`
has no failure and is still 10–17×; its best round is 4×, its steady
median 17×: GC or deopt churn between rounds) is unprofiled: reflection
paths (§5.6), strings as `Str` windows, map hashing of strings, and the
value model are the suspects — lane `perf-pe` (§6.3) owns the profile.

### 4.2 Allocation-heavy heap and the collector (`allocmix`)

Live heap ~100 MB (gc) plus churn: short-lived trees, map record
replacement, value-struct appends; the latency of each of 400 units per
round is recorded and the percentiles are over the last half of the
rounds. 60 s per leg (`ALLOCMIX_ROUNDS=1200`, load average 2), one rep
each; a 4 s run with 3 interleaved reps (load 4–8) gave the same ranking.
CPU/round is the process's user + system CPU over 1,200 rounds (JIT and
GC threads, start-up included).

| Leg | median round (ms) | ×gc | unit p50 / p99 / p99.9 / max (µs) | peak RSS (MB) | CPU/round (ms) |
|---|---:|---:|---|---:|---:|
| gc (`GOGC=100`) | 31.0 | 1.00 | 49 / 225 / 370 / 7,212 | 140 | 68 |
| gc `GOGC=400` | 18.8 | 0.61 | 35 / 133 / 237 / 7,674 | 338 | 30 |
| graalgo G1 (runner default) | 46.8 | 1.51 | 41 / 91 / 238 / 6,566 | 913 | 59 |
| graalgo G1 + `-XX:+UseCompactObjectHeaders` | 42.0 | 1.35 | 38 / 90 / 259 / 6,391 | 906 | 52 |
| graalgo ParallelGC | 49.4 | 1.59 | 39 / 86 / 172 / 5,193 | 1,128 | 59 |
| graalgo ZGC (generational) | 61.8 | 1.99 | 49 / 124 / 199 / 3,795 | 2,234 | 85 |

Reading: throughput is 1.35–2× *slower* than default gc and 2.2× slower
than gc at `GOGC=400` (gc's best leg here). The unit p99 is 2.5× lower
than default gc's (gc's concurrent mark assists land on the mutator; the
JVM's young collections are short), but against gc at `GOGC=400` it is
1.5× and p99.9 is level: no tier-pass on latency either. CPU per round is
13–23 % below default gc and 2× above `GOGC=400`. Shenandoah cannot run
under the Graal JIT (§5.1).

### 4.3 Goroutine scaling (`goroutines`)

`GOR_N` goroutines spawned and parked on a channel; idle RSS after a GC;
a round is one spawn → park → broadcast wake → exit lifecycle.

| N | gc spawn | gc bytes/goroutine idle | gc round | graalgo spawn | graalgo bytes/goroutine idle | graalgo round |
|---:|---:|---:|---:|---:|---:|---:|
| 1,000 | — | — | — | 0.13 s | (below noise) | 16 ms |
| 5,000 | — | — | — | 0.98 s | — | 0.53 s |
| 10,000 | 10 ms | 2,770 | 2.3 ms | 2.3 s | 13,300 | 1.46 s |
| 20,000 | — | — | — | 6.9 s | 10,300 | 5.7 s |
| 100,000 | 0.16 s | 2,740 | 45–59 ms | 162 s | 4,680 | 383 s |
| 200,000 | 0.22 s | 2,737 (524 MB RSS) | 68 ms | not done spawning after 25 min (load 30, `nice -n 19`): no idle RSS | — | — |
| 1,000,000 | 1.36 s | 2,760 (2.6 GB RSS) | 0.35 s | not run: quadratic (§5.3), extrapolated > 1 h | — | — |

Spawn time grows ~N² (10k → 100k: 70×); the JFR profile puts 60 % of the
samples in `PolyglotContextImpl.checkAllThreadAccesses` under
`TruffleThreadBuilder.build()` — Truffle walks every registered thread
when a new one is created (§5.3). The idle memory per goroutine (4.7 KB
at 100k, heap-growth noise below that) is 1.7× gc's, so memory is not the
first problem; creation is. 1M goroutines under graalgo is blocked on
that fix, not on the 2 GB heap (the explicit heap-scaling run was
therefore not attempted).

### 4.4 Footprint baselines

| Program | gc | graalgo |
|---|---|---|
| hello world (`fmt.Println`), peak RSS / wall | 2 MB / < 10 ms | 245 MB / 1.1 s |
| `net` or `os/exec` imported, hello world | — | 250–262 MB / 1.1–1.2 s |
| any §4.1 pipeline, peak RSS | 11–19 MB | 725–906 MB (`-Xmx2g`, G1 sizing) |
| `allocmix` (~100 MB live) | 140 MB | 906–2,234 MB by collector |

The JVM floor (~250 MB) and G1's heap headroom up to `-Xmx` dominate; §5.1
and §5.2 hold the levers.

### 4.5 What the numbers say

1. **Nothing realistic is faster than gc today.** The stdlib pipelines are
   10–110× slower (geomean 27×), the allocation-heavy heap 1.35–2×, and
   goroutine creation is quadratic. The micro-benchmark suite's 1.4×
   geomean does not transfer to real code: the gap is in code the suite
   never exercised (`goto` state machines, reflection, large functions).
2. **The first blockers are bugs, not architecture:** PE failures (a
   handful of functions run interpreted) and O(N) goroutine creation are
   each a bounded fix; both are on the critical path (§6).
3. **Where a JVM is already better:** unit tail latency under allocation
   churn (p99 2.5× lower than gc's default) and CPU per unit of work
   (13–23 % lower) — but only against default gc; gc at `GOGC=400` beats
   both. A latency claim must be made against tuned gc (§3.1).
4. **Footprint is the structural cost:** ~250 MB floor and 0.8–1 GB at
   `-Xmx2g` for programs gc runs in 20 MB. Acceptable for T4 servers with
   GB heaps; it rules out the "small utility" end of T1 in JIT mode.
5. **Collector:** G1 stays the default; compact object headers are a
   cheap 10 % on allocation-heavy code; ZGC does not pay at 100 MB live.

## 5. JVM-side design questions

Each question names the decision, what decides it, and the current
evidence. Labels as in SPEC §1.3.

### 5.1 Garbage collector and heap sizing

- **Decision:** the runner's default collector and heap policy, and how
  `GOGC`/`GOMEMLIMIT` map onto it (SPEC §6.7's parity stance, WP-PAR).
- **Evidence (§4.2):** on `allocmix`, G1 (the runner's default) is the best
  throughput/footprint compromise; compact object headers take 10 % off
  the round time and 12 % off CPU at the same RSS; ParallelGC gives the
  best p99.9 (172 µs) for 5 % less throughput and 24 % more RSS;
  generational ZGC costs 32 % throughput, 45 % more CPU and commits the
  whole 2 GB heap for a ~100 MB live set, buying the lowest max pause
  (3.8 ms vs 6.6). **Recommendation:** keep G1 as the default, add
  `-XX:+UseCompactObjectHeaders` once `make all` passes with it (lane
  `perf-mem`, §6.3), revisit ZGC only for multi-GB heaps with a pause SLO (T4) —
  measured on a T3/T4 benchmark, not on this one. No collector closes the
  1.35× gap: allocation rate (the flat value model) is the lever.
- **Shenandoah is not an option** on the pinned Oracle GraalVM 25: the
  Graal JIT refuses it ("Shenandoah garbage collector is not supported by
  Graal") `[VERIFIED]`, so a Truffle language cannot run compiled under it.
- **Heap sizing vs `GOGC`/`GOMEMLIMIT`:** gc sizes the heap as live × (1 +
  GOGC/100) and honours a soft limit; the JVM sizes from `-Xmx` and the
  collector's ergonomics, and its RSS is dominated by the committed heap
  (§4.4: ~250 MB for hello-world, ~900 MB under G1 and 2 GB under ZGC for a
  ~100 MB live set at `-Xmx2g`). Proposal `[PROPOSAL]`: the runner derives
  `-Xmx` from `GOMEMLIMIT` when set (else from the container/cgroup limit,
  as gc's `GOMEMLIMIT` users expect), maps `GOGC` to G1's
  `-XX:G1PeriodicGCInterval`/`MaxHeapFreeRatio`-style ergonomics only
  loosely (documented in the parity-stance log), and enables
  `-XX:+UseCompactObjectHeaders` (JEP 519, product in JDK 25) by default
  once the corpus passes with it. The memory guard rail of §1 is the
  acceptance gate, not parity of knobs.
- **Off-heap for large buffers:** not before a benchmark shows `byte[]`
  copying or GC scanning of large buffers in a profile; `mmap` (T3) needs
  off-heap views regardless (W8b, GG-0072).
- **Flat value model and allocation rate:** a Go struct value is a `Flat`
  object with `long[]`/`Object[]` regions ([performance.md](performance.md)
  §5 item 2); a `&rec{}` that gc allocates as one 80-byte object is up to
  three JVM objects. Allocation rate, not collector choice, is the lever on
  `allocmix`: single-region layouts for all-scalar structs and inline
  arrays are the work item (WP-PERF).

### 5.2 Memory footprint

JVM object headers (12 bytes, 8 with compact headers) vs gc's headerless
objects with size classes; the JVM's committed heap headroom; Truffle's
per-call-target metadata and compiled code; the JIT compiler's own heap
(libgraal: outside the Java heap). Measured baselines in §4.4. The guard
rail of §1 (≤ 2× gc for heaps ≥ 1 GB) is reachable only with compact
headers, a GOMEMLIMIT-derived `-Xmx`, and a flat layout without the extra
region objects; below 1 GB the fixed ~250 MB JVM floor dominates and only
the AOT/hybrid modes (Native Image's SubstrateVM heap) can approach gc.

### 5.3 Goroutine scalability

- **Today (§4.3):** goroutine creation costs O(live goroutines) because
  every goroutine is a polyglot thread: `TruffleThreadBuilder.build()` →
  `PolyglotContextImpl.checkAllThreadAccesses` iterates a `WeakHashMap` of
  all the context's threads `[VERIFIED]` (JFR, 40k goroutines: 2,045 of
  ~3,400 samples). 100k goroutines take 162 s to spawn (gc: 0.16 s), 1M is
  out of reach. Idle cost: ~4.7 KB per parked goroutine at 100k (gc
  2.7 KB).
- **Options:** (a) one polyglot thread per carrier, goroutine virtual
  threads entering the context through a cheaper path — needs Truffle's
  support for many short-lived threads (ask upstream; check whether
  `checkAllThreadAccesses` can be skipped when the language allows
  multi-threading from the start); (b) a pool of context-entered virtual
  threads reused across goroutines (amortizes churn, the server pattern;
  does not fix 1M live); (c) goroutines as continuations on a fixed set of
  entered threads (`jdk.internal.vm.Continuation`: internal API, rejected
  for now). (a) is the structural fix and the first item of lane L1 (§6.3).
- **Park/unpark:** the scheduler hop (`chanpingpong` 3× gc after the
  WP-SCHED handoff) matters to servers only per request (≈ 2–4 hops); the
  creation cost above dominates first.

### 5.4 I/O and the network stack

- **ADR-0009 (decided):** real `net`/`internal/poll` from upstream's
  source over the Panama syscall executor and the runtime epoll poller
  (GG-0063); no NIO fork. Cost model: the executor adds 45–215 ns per call
  over gc's raw syscall (abi-spike §2.4: `write` 242 vs 194 ns). A
  keep-alive HTTP/1.1 request is ~3–5 system calls (read, write, the
  occasional epoll_wait), so the overhead is ≤ 1 µs per request against a
  gc budget of ~20–50 µs per request: ≤ 5 % `[ESTIMATE]`. The poller's
  wake path (epoll thread → scheduler token → virtual-thread unpark) is the
  bigger unknown; it costs at least one scheduler hop per readiness event.
- **Viability check (planned, lane L2):** a loopback echo benchmark
  (1k connections, 64-byte messages) gc vs graalgo with the socket roles in
  place, measuring req/s and CPU/req, before `net/http`; kill criterion: if
  the per-request overhead after the goroutine-creation fix exceeds 2× gc
  at equal load, the poller design (one epoll thread, edge-triggered,
  one waiter per direction) is revisited before T2 work continues.

### 5.5 Crypto

gc uses assembly (AES-NI + PCLMULQDQ for GCM, SHA-NI, AVX2 for
ChaCha20/Poly1305, BMI2 for P-256). With `internal/cpu` reporting no
features ([limitations.md](limitations.md)) and `purego`, graalgo would
run the generic Go code: typically 5–20× slower than the assembly
`[ESTIMATE]`. The JDK has the same instructions as compiler intrinsics
(`com.sun.crypto.provider.AESCrypt`, `GHASH.processBlocks`,
`sun.security.provider.SHA2.implCompress`) and the Graal compiler
implements them as stubs. Plan `[PROPOSAL]`: implement gc's assembly
entry points (the body-less functions of `crypto/internal/fips140/...`)
as intrinsics whose Java bodies are JDK-intrinsified loops over the same
state layout (expanded AES key schedule, SHA state words) — beating gc is
not the goal (both run the same instructions), parity is: TLS-heavy T2/T4
servers must not lose 10× here. Panama to the host's libcrypto is the
fallback (not a pure-JVM deployment). The FIPS-140 indicator and
`fatal` hooks are runtime symbols to provide in the facade (link floor).

### 5.6 Reflection-heavy code

`encoding/json`, `text/template`, protobuf and gRPC reflect on type
descriptors (W9a/W9b: descriptor images, reflect's patched call path).
§4.1 measures json and templates 20–30× slower than gc. Descriptor reads
are memory loads in gc; here they go through images and checked
`unsafe.Pointer` views; method calls by name go through
`callMethodByName`. The JIT can fold descriptor reads when the type is a
compile-time constant (a profiled monomorphic `reflect.Type` at a site):
this is where a JIT *can* beat gc (gc never specializes reflection), but
only after the baseline is within ~2×. Work: profile json/template,
PE-constant descriptor reads, intrinsics for the hottest reflect paths.

### 5.7 cgo-using dependencies

SQLite (`mattn/go-sqlite3`), GEOS, RocksDB bindings, `net`'s cgo resolver
(avoidable with `netgo`). GG-0022's Panama mapping (WP-CGO1–3) is 31–63
agent-days and blocks only programs that cannot drop cgo; T1–T3 have pure-Go
alternatives (`modernc.org/sqlite`, pebble). It stays after the T2 floor.

### 5.8 Speculation a JIT has and gc does not

Ranked by expected value for the north star (each needs its falsifying
benchmark, SPEC §2.6):

1. **Devirtualization through `io.Reader`/`io.Writer`/`http.Handler`/
   `hash.Hash`/`sort.Interface`** with splitting (SPEC §2.6.1; `ifacesplit`
   reaches gc, [performance.md](performance.md) §3.1). Falsifier: `reqpipe`
   and the T2 JSON API.
2. **Inlining across package boundaries and generic instantiations**
   (gc's budget and GC shape stenciling limit it). Falsifier: generic
   containers in T3/T4 code.
3. **Partial escape analysis after inlining** (allocation sinking): the
   per-request allocations of T2 handlers.
4. **Profile-driven bounds-check elimination and loop vectorization**
   (`loop` is 5× faster than gc today): byte-scanning in T1.
5. **Reflection specialization** (§5.6) — unique to a JIT.
6. **Lock elision** (WP-TMELIDE): hardware-gated, exploratory.

### 5.9 Warmup

Time to peak today is 0.2–2.3 s per program (§4.1) with the JIT on 4
cores. For servers that is amortized inside the §1 budget; for utilities
it decides whether a 10 s run counts. Mitigations in order of cost: Truffle
`engine.Mode=throughput` and compilation thresholds (flags, measure now);
the JDK AOT cache / CDS for the runtime's own classes (JEP 483/514, start-up
only); Truffle's auxiliary engine cache (persisted compiled code; GraalVM
feature, availability on the pin to be checked); the hybrid mode (SPEC
§2.5 (c), WP-MODE) for the runtime and stdlib hot set. None is on the
critical path of the north star; all are on the secondary goal's.

## 6. Roadmap re-prioritization

### 6.1 What moves

Work package ids as in [RECOVERY.md](RECOVERY.md) and
[toolexec-design.md](toolexec-design.md) §8–§9. Effort in agent-days,
`[EST]`.

| Item | Move | Why (north star) |
|---|---|---|
| Land M1d (`wt/fmt/path`) | **first** | every realistic benchmark needs `fmt`/`reflect`/`os` |
| Goroutine creation O(1) (new, §5.3) | **up: top** | T2–T4 are impossible at 10k+ goroutines; measured quadratic |
| PE failures on `goto` blocks and deep inlining (new, §4.1) | **up: top** | `regexp`, `encoding/json`'s scanner, `compress/flate` run interpreted: 20–130× gc |
| W8b sockets + poller on sockets (M4 `net`) | **up** (before the rest of M4) | the T2 floor |
| crypto link floor + intrinsics, `hash/crc32`, `math/big` (M4 `crypto/*`) | **up**, split: link floor first, speed second | `net/http` does not link without `crypto/tls`; gzip/png/T3 need crc32 |
| `net/http` server/client differential (M4) | **up** | the first T2 benchmark |
| WP-PERF (reflect, value structs, strings, megamorphic) | **up**, re-aimed at §4.1's programs | throughput on T1/T2 hot paths; micro-benchmarks become secondary |
| GC/heap defaults, compact headers, `GOMEMLIMIT` → `-Xmx` (new, §5.1) | **up** (small) | memory guard rail; measured −10 % round time with compact headers on `allocmix` |
| W8b `os/exec` spawn helper (GG-0064), `os/signal` | middle | T4 (kubelet, controllers), not T1/T2 |
| `mmap` off-heap views, `pread`/`fsync` roles | middle | T3 |
| M2 (`testing`, `go test`), M3 cutover, M5 | unchanged | correctness/drop-in track; not on the performance critical path but M3's deletions free maintenance |
| WP-JFR, `runtime/pprof` substitute, `runtime.Callers` (BUG-0047) | middle | T4 observability; `Callers` also blocks `log` file:line |
| WP-SCHED remaining, WP-POLLCOST follow-ups | middle | per-request hops matter after creation cost is fixed |
| WP-TMELIDE | **down** | hardware-gated, narrow; after a T2 server runs |
| WP-AOT / WP-MODE / WP-OSR (modes), GG-0050 | **down** | start-up is the secondary goal; peak is the JIT's |
| Isolates (WP-ISO1/2), interop GG-0003/GG-0005 | **down** | not on any tier's path |
| cgo (WP-CGO1–3, GG-0022) | **down** (after T3) | pure-Go alternatives exist for T1–T3 |
| Upgrade tooling (WP-UPG1/2, WP-PAR) | unchanged | needed for the go1.28 intake regardless; WP-PAR absorbs the GC stance |

### 6.2 Critical path: first real server benchmarked against gc

```
M1d lands ─┬─> L1 goroutine creation O(1) ─────────────┐
           ├─> L2 socket roles + poller + echo bench ──┼─> L4 net/http differential ─> L5 T2 JSON API benchmark
           └─> L3 crypto + crc32 link floor ───────────┘
           └─> L6 PE fixes + reflect/json speed (parallel; decides whether L5 can pass)
```

| Step | Work | Gate (must be able to fail) | Effort `[EST]` |
|---|---|---|---|
| L1 | goroutine creation independent of live count (§5.3 option a, upstream issue if needed) | `goroutines` at 100k: spawn ≤ 10× gc and linear (10k→100k time ratio ≤ 12); idle bytes/goroutine ≤ 2× gc | 4–10 |
| L2 | W8b socket family (`socket`, `bind`, `listen`, `accept4`, `connect`, `getsockopt`/`setsockopt`, `getsockname`/`getpeername`, `shutdown`), `writev`/`sendmsg` with pointer-holding iovecs, poller on sockets | `net` loopback differential (TCP/UDP/Unix); echo benchmark at 1k connections with req/s and CPU/req recorded vs gc; R15 table-vs-source gate | 5–8 |
| L3 | facade FIPS-140 hooks; `crypto/*` and `hash/crc32` and `math/big` assembly entry points as intrinsics (§5.5) | `net/http`, `crypto/tls`, `compress/gzip`, `math/big` link; crypto test vectors vs gc; sha256/AES-GCM MB/s ≥ 0.5× gc | 6–12 |
| L4 | `net/http` server + client (M4 subset), `context` cancellation through the poller | differential: a client/server corpus vs gc (headers, chunked, keep-alive, timeouts) | 4–8 |
| L5 | the T2 JSON API benchmark with an open-loop load generator built with gc, on disjoint cores | the §3 protocol report: req/s, p99/p99.9 at 50/70 % load, CPU/req, RSS vs gc, gc+PGO, gc tuned | 3–5 |
| L6 | PE failures (`GotoBlock` pc not PE-constant, deep inlining bailouts), then reflect/json/template throughput | `make bench-realistic`: no `opt failed` on any program (`--trace`); `jsonrt`, `tmplrender`, `regexscan` ≤ 3× gc | 6–12 |

Sum: 28–55 agent-days; with L1/L2/L3/L6 parallel, ~3 calendar weeks to
the first measured server after M1d lands. The first T2 number will very
likely be a regression (§4: every stdlib pipeline is ≥ 8× gc today); its
value is the profile it gives, ranked against §5.8's speculation list.

### 6.3 The next six lanes

1. **`sched-spawn` (T2 runtime lane):** L1 — goroutine creation O(1) in
   live goroutines; idle memory per goroutine; re-run §4.3 incl. 1M.
2. **`m4-net` (T4 syscall lane):** L2 — socket roles, poller on sockets,
   loopback echo benchmark (the viability check of §5.4).
3. **`m4-crypto` (T1 intrinsics lane):** L3 — link floor for `crypto/*`,
   `hash/crc32`, `math/big`; JDK-intrinsified kernels; sha256/AES-GCM/crc32
   in `test/bench/realistic/`.
4. **`perf-pe` (T2 performance lane):** L6 — zero PE failures on the
   realistic programs (GotoBlock, inlining bailouts, the template deopt
   loop), then reflect/json/template speed; owns the realistic results
   table in performance.md.
5. **`m4-http` (T1 + T4 lane), after 2 and 3:** L4 + L5 — `net/http`
   differential and the first T2 benchmark with the load harness
   (`test/bench/servers/` (planned)).
6. **`perf-mem` (T2 runtime lane):** GC/heap defaults (§5.1: compact
   headers, `GOMEMLIMIT`-derived `-Xmx`, collector), single-region flat
   layouts for all-scalar structs, footprint vs gc on `allocmix` and
   `goroutines`; WP-PAR's GC stance entry.

Gates for every lane: `make all`; numbers through `make bench-realistic`
(interleaved, reps stated) recorded in performance.md; a claim of
"faster than gc" only under §1 and §3.4.
