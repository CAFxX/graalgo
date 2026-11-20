# Recovery plan (started 2026-11-17)

[SPEC.md](SPEC.md) is authoritative. This plan brings the implementation back
to it. Baseline audit: three independent audits of every `GG-*` requirement
against the code at `a0df481b` (findings summarized below; each claim was
verified by running programs, not by reading docs).

## Baseline (a0df481b)

| Area | State |
|---|---|
| Scheduler core (quantum park, drainer, wake lanes, fairness) | Good; unit-tested and measured |
| Value model (`GoStruct`/`GoSlice`/`GoString`/`GoMap`/…) | Mostly matches SPEC §3.4 |
| Vendored `go/*` | Byte-identical to go1.27.1 |
| Process lifecycle | Broken: hangs when main returns with live goroutines or a goroutine panics; no deadlock detection |
| Panic model | Runtime errors are not `error`; recovered `panic(nil)` crashes; panic print skips `Error()`/`String()` |
| JIT | Never compiles: `PermanentBailoutException: Too deep inlining` on every function (no `@TruffleBoundary`); ~50–500× slower than gc |
| Frontend v1 | Single-file JSON IR; cannot build multi-package modules; 140 hand-written `synth_*.go` stdlib replacements (violates GG-0009) |
| Frontend v2 | ~5% prototype |
| Driver | Custom 5-command reimplementation (SPEC GG-0011 requires a `-toolexec` wrapper) |
| `TruffleLanguage`/polyglot `Context` | Absent (blocks interop, debugging, native-image) |
| AOT / hybrid modes, isolates, cgo→Panama | Absent |
| Test gates | `snapshot-check` records graalgo output (SPEC §5.9: gc); `%`→`floorMod` mutant survives every per-push gate; layer 3/4 scripts reference the deleted `phases/` tree |
| Upgrade tooling (GG-0035, 0039–0043, 0056) | Absent |

## State (2026-11-18)

Integration branch: `master` in `~/dev/graalgo` (fast-forward only; lanes
work in worktrees, [AGENTS.md](../../AGENTS.md) §4). The `wt/recovery/*`
branches below are history.

| Lane | State |
|---|---|
| T0.A runtime lifecycle (`wt/recovery/runtime`) | landed |
| T0.B JIT enablement (`wt/recovery/jit`) | landed |
| T0.C test infrastructure (`wt/recovery/testinfra`) | landed |
| T1 design ([toolexec-design.md](toolexec-design.md)), T2 design ([truffle-design.md](truffle-design.md)) | landed |
| ABI spike ([abi-spike.md](abi-spike.md): P1 verbatim reflect, P2 syscall boundary) | completed, both **GO**; amendments applied |
| SPEC amendments (`wt/recovery/spec-amend`) | landed ([ADR-0009](../dev/adr/ADR-0009-recovery-spec-amendments.md)) |
| toolexec W1 launcher + W2 GIR (`wt/recovery/gir-launcher`) | landed |
| T0.D + TR-0 + TR-1 backend structure (`wt/recovery/backend-structure`) | landed |
| toolexec W3 emitter (all GIR node kinds; girinterp reference interpreter) | landed 2026-11-18 (`wt/m1a/nodes-fix2`); the M1a close-out added GIR `FuncLit.wrapper` |
| toolexec W4 new node package ([graalgo/go/](../../internal/backend/truffle/src/main/java/graalgo/go/), [truffle-design.md](truffle-design.md) §15) | landed 2026-11-18; M1a close-out (`wt/m1a/closeout`): Goexit, recover through wrappers, range-over-func, the open-coding limit pinned; `make test-m1a` 173/173 programs vs gc (+ [defer_path.sh](../../test/m1a/defer_path.sh)), per-push in `make all`; per-node T1–T4 tests waived ([ADR-0014](../dev/adr/ADR-0014-node-tests-waived-m1a.md), WP-NODETEST) |
| srctest v4 (`wt/ci/green`): conformance triage of every GOROOT/test result on the product launcher | landed 2026-11-18 (`c14b63d0`): adapter honors action lines, stable divergence texts, every entry bug-id'd and signature-pinned ([srctest-exclusions.yaml](../../test/differential/srctest-exclusions.yaml) v4; v5 adds gc2.go, BUG-0032) |
| Debt paydown round 2 (`wt/ci/green2`) | 2026-11-18: StackDepthLimit flake fixed at the root (call-path holder warm-up); layering, forward-only (ratchet, locked-text, statuses), exclusion-register, quarantine owner/expiry, superseded-id, per-tree log-size gates; no-bridges gate over whole pushes; typeparam adapter un-broken (un-quarantined); fuzz-soak fits the job limit; stdlib-gate manual-only (BUG-0033); M3 delete inventory (toolexec-design §9.1) |
| Spec reconciliation (`wt/spec/reconcile`): global spec-conformance review items | done 2026-11-18 ([ADR-0010](../dev/adr/ADR-0010-launcher-deviations.md), [ADR-0011](../dev/adr/ADR-0011-spec-reconciliation.md), coverage below) |
| toolexec M1r, first slice (`wt/m1r/facade`): W5 fork tooling + facade `runtime`, W6 intrinsic registry + link-time resolution and reachability | landed 2026-11-18: upstream's std source runs on the node package for 18 importable packages — those that at run time need no `internal/reflectlite` descriptor read, no semaphore and no system call (`make test-m1r` 11/11; [toolexec-design.md](toolexec-design.md) §13); all of std compiles against the facade. **Open in M1r:** W13 pointer model ([ADR-0015](../dev/adr/ADR-0015-pointer-model-w13.md), accepted by the owner 2026-11-18, **ACTIVE: owner lane `w13`** — also a soundness gate: `unsafe.Pointer` conversions are an unchecked identity until it lands), W9a descriptor images + `reflectlite` intrinsics (gate for `errors` and everything above it), W8a syscalls; then `sync`'s semaphores (11 symbols) |
| toolexec W13, the pointer model (`wt/w13/pointers`; [ADR-0015](../dev/adr/ADR-0015-pointer-model-w13.md)) | landed 2026-11-19 on the branch: gc layout + leaf tables on every type (checked against the frontend's record), byte region in `Flat` with one slice shape, checked `unsafe.Pointer` conversions with view pointers and header views, `unsafe` builtins as node kinds, range-allocating `uintptr` handles, gc's `checkptr` fatal errors; gc's order of evaluation; `&T{}` for slice/map types; the startup hang traced to Truffle's mount hook re-entering `MethodHandle.customize` (runner flag). Gates: `make test-m1w13` 14/14 (per-push), `test-m1a` 176, `test-m1r` 12, `LeafTableTest`; [toolexec-design.md](toolexec-design.md) §13, [truffle-design.md](truffle-design.md) §15, [limitations.md](limitations.md). **Open in M1r:** W9a (gate for `errors` and everything above it), W8a, then `sync`'s semaphores |
| toolexec M1c slice (`wt/m1c/sync-time`): `sync` semaphores, `internal/bytealg`, W8a executor, `time` | 2026-11-19 on the branch: `sync`/`internal/sync` from upstream's source on scheduler-aware semaphore and notify-list intrinsics (gc wait reasons in deadlock reports); `internal/bytealg`'s assembly as intrinsics over the byte region; [graalgo/syscall/](../../internal/backend/truffle/src/main/java/graalgo/syscall/) executes `Syscall6` by Panama through the role table [table.txt](../../internal/toolexec/syscalls/table.txt), checked against std's call sites (R15 gate, `go test ./syscalls`); `time`'s runtime half on the timer heap. Gates: `make test-m1c` 11 pass / 4 pending (`sync.Map`, `strings`/`bytes`, two `time` programs — all on W9a's descriptor images), [mutants.sh](../../test/m1c/mutants.sh) 14/14; [toolexec-design.md](toolexec-design.md) §13. **Open:** W9a (then the pending programs run by themselves), W8b (struct/array roles, `os`'s numbers) |
| toolexec W9a, descriptor images (`wt/w9a/descriptors`) | landed 2026-11-19 on the branch: gc-layout type descriptor images as addressable objects built from the type table and the bundle's own `internal/abi` (no descriptor offset in the backend), the type word of an interface header, the seven `internal/reflectlite` intrinsics; `internal/abi` and `internal/reflectlite` run from upstream's source. `errors` (New/Is/As/Unwrap/Join), `strconv` and `sort.Slice` run. Gates: `make test-imgcheck` (per-push: 1,802 descriptors, 10,409 facts identical to gc's, the walker checked against gc's `reflect`, 5/5 seeded errors detected), `test-m1r` 15, `test-m1w13` 16, `test-m1a` 178; three frontend fixes (typed nil, generic method sets, gc's local type names); [toolexec-design.md](toolexec-design.md) §13, [truffle-design.md](truffle-design.md) §15, [limitations.md](limitations.md). `reflectlite.Swapper`/`pkgPath` (byte/word views of other leaves) run as temporary registry intrinsics by owner decision (2026-11-19, BUG-0037; removal is WP-REFLECTLITE-VIEW). **Open in M1r:** W8a, `sync`'s semaphores and `internal/bytealg` (lane m1c; `strings`/`bytes` 16 undefined symbols each) |
| toolexec M1c/M1d, hello world with `fmt` (`wt/fmt/path`): W8b for `os`, the runtime poller, W9b, `fmt` | 2026-11-20 on the branch: `fmt`, `os`, `internal/poll`, `reflect` (call path and type constructors as [stdlib/patches/reflect](../../stdlib/patches/reflect/), 5 patched files), `encoding/json`, `regexp`, `text/template`, `bufio`, `flag`, `log`, `context`, `math/rand` run from upstream's source; epoll poller (GG-0063) behind `os.Pipe`; 47 role-table entries with `sel` and `rd`/`wr` readiness; BUG-0029 fixed. Gates: `make test-m1d` 6/6 (per-push, ~15 s), [mutants.sh](../../test/m1d/mutants.sh) 7/7; [toolexec-design.md](toolexec-design.md) §13, [limitations.md](limitations.md). **Open:** BUG-0046 (runtime error types as facade source), BUG-0047 (WP-CALLERS), sockets/`writev`/`ioctl`/`getrandom` roles, GG-0064 spawn helper, `hash/crc32`/`math/big`/crypto assembly, `os/signal`, the fmt-importing corpus leg of M1d |

## Decision log

| Date | Decision | Where recorded |
|---|---|---|
| 2026-11-18 | W4 builds a new idiomatic Truffle node package (DSL specializations, typed families, `TruffleLanguage`-native); v1 node classes retired at M3; +6–10 agent-days on M1a | toolexec-design D12, §14; truffle-design T13 |
| 2026-11-18 | Goroutines in image modes (A1) and GG-0050's "performant without the JIT" (A2): **measure first** — gating work items P-D (`engine.Compilation=false`, 0.5 day) and P-F (native-image thread behaviour, 1 day) before TR-12/T3; SPEC carries both as `[OPEN]` | SPEC §3.5, §3.9; truffle-design §13.2, §14 |
| 2026-11-18 | Host access for `graalgo/java` (A4): embedder's `HostAccess` when embedded; allow-listed public access when standalone and the package is imported. A3, A5, A6 stay pending | SPEC §2.4, §4.3; truffle-design §5.6, §14 |
| 2026-11-18 | Netpoller: runtime epoll poller through the Panama syscall executor, goroutines park via the slot-releasing path; `GG-0018` superseded by GG-0063 | SPEC §3.5, §7.4 |
| 2026-11-18 | Value model flat with per-type leaf tables; `unsafe.Pointer` = (object, gc byte offset), in-object arithmetic defined, escapes are gc `checkptr` fatal errors; `GG-0015`/`GG-0016` superseded by `GG-0061`/GG-0062 (`GG-0061` was re-issued as GG-0072 the next day, see the ADR-0015 row) | SPEC §3.4, §7.3 |
| 2026-11-18 | Process creation: `posix_spawn` helper binary shipped in the distribution (GG-0064) | SPEC §3.8 |
| 2026-11-18 | abi-spike §3's ten toolexec-design amendments applied; R1/R2 resolved GO with residual risks; new R15 (memory-unsafe syscall table) | toolexec-design §10, §14 |
| 2026-11-18 | Unknown GODEBUG keys accepted silently like gc; `GG-0055` superseded by `GG-0065` (re-issued below) | SPEC §4.5, ADR-0011 |
| 2026-11-18 | Frontend = `go/*` of the toolchain that builds graalgo-tool, with a tracked-release assertion; SPEC §3.2/§3.3 describe GIR; `GG-0012` superseded by `GG-0066` (re-issued below) | SPEC §3.2, ADR-0011 |
| 2026-11-18 | Slot-releasing wrapper restated for syscall downcalls; `GG-SCHED-0004` superseded by `GG-SCHED-0005` (re-issued below); goroutine state in `G`, not thread-scoped holders | SPEC §3.5, ADR-0011 |
| 2026-11-18 | The W1 launcher's `go version` suffix, `GOTOOLCHAIN=local`, `CGO_ENABLED=0`, `-tags=purego` and visible `-toolexec` are temporary deviations with retirement triggers; requirements unchanged | ADR-0010; SPEC `[DEVIATION, ADR-0010]` |
| 2026-11-18 | M1 re-sequenced: new milestone M1r (registry, pointer model, descriptor images, reflectlite intrinsics, four syscalls) before M1b | toolexec-design §8.1 |
| 2026-11-18 | A8 **accepted**: open-coded defers (statically lowered for top-level defers not in loops under the limit; explicit per-goroutine chain as fallback and as panic/recover/Goexit unwinding path; `recover`/`Goexit`/`runtime.Caller` match gc); `GG-0019` superseded by `GG-0067` (re-issued below); SPEC §3.5 and Appendix B amended | [ADR-0012](../dev/adr/ADR-0012-open-coded-defers.md), SPEC §3.5, truffle-design §4.5, §14 |
| 2026-11-18 | A7 (safepoint preemption) **accepted conditionally on P-B** `[OPEN]`: the owner switches only if prototype P-B validates; SPEC, GG-SCHED-0001 and GG-0017 unchanged and `Safepoint.poll()` stays until then | ADR-0012, truffle-design §13.2, §14 |
| 2026-11-18 | A7 **REJECTED** (supersedes the row above): prototype P-B failed — pinned virtual-thread parks (94 `jdk.VirtualThreadPinned` events vs 0), plain programs hang (3,000 CPU-bound goroutines; F1 with 10,000 spinners), Truffle mount-hook recursion, counted loops lose the safepoint under the community compiler configuration, a compiled `for {}` never preempted in the unwind-and-resume variant; by the owner's rule (switch only if P-B validates) the poll stays; SPEC and requirements unchanged. Real finding kept as backlog: the explicit poll costs 18.3 ms vs 1.0 ms without on `loop` (gc 4.4 ms) — new work package WP-POLLCOST | [ADR-0013](../dev/adr/ADR-0013-a7-rejected.md), truffle-design §2.7, §14 |
| 2026-11-18 | M1a close-out: the per-node unit/PE tests of truffle-design §11 (T1–T4) are waived for M1a — the gc-differential corpus gate, `defer_path.sh` and `pe-check-m1a` stand in — with work package WP-NODETEST to build them; graalgo-tool's refusal of `-race`/`-msan`/`-asan`/`-cover`/fuzzing recorded as deviation DV6 | [ADR-0014](../dev/adr/ADR-0014-node-tests-waived-m1a.md), [ADR-0010](../dev/adr/ADR-0010-launcher-deviations.md), truffle-design §15 |
| 2026-11-18 | **ADR-0015 accepted (all four decisions); W13 ACTIVE, owner lane `w13`:** checked `unsafe.Pointer` conversions with view pointers (gc byte offset) as the fallback and `checkptr` fatal errors; headers viewed through the slot that holds them, replaced on store; one-byte leaves in a typed `byte[]` region of each flat object, one slice shape `{base, prim, ref, byt, len, cap}` (**amends SPEC §3.4's "typed Java arrays" wording**; other narrow kinds stay `long`/`double` slots, wider typed arrays are a post-M2 benchmark question); `uintptr(p)` via the range-allocating handle table. `GG-0061` is superseded by the draft GG-0072 (its text said one `long[]` plus one `Object[]` and typed-array slices, false under this ADR; awaiting owner sign-off), GG-0062 text unchanged | ADR-0015, SPEC §3.4, truffle-design §15, toolexec-design §9 |
| 2026-11-18 | A9 (closures over typed cells) **deferred pending benchmarks**: SPEC §3.4 unchanged (`MaterializedFrame` closures) until the owner decides after the closure benchmarks | ADR-0012, truffle-design §14 |
| 2026-11-18 | **Re-issue (owner decision on the independent Fable review):** the four requirements locked that day without owner sign-off are superseded by requirements with corrected wording — `GG-0065` → GG-0068 (GODEBUG: what gc ignores is ignored; fail only on an unsupported knob's effective value from any source), `GG-0066` → GG-0069 (frontend unmodified; graalgo-tool refuses to run when built by another release), `GG-SCHED-0005` → GG-SCHED-0006 (slot *and* carrier accounting around every may-block host call; semaphore and poller parks; fail-fast bracket misuse), `GG-0067` split into GG-0070 (observable semantics incl. `panicnil` and caller frames, with [traceback-normalization.md](traceback-normalization.md)) and GG-0071 (open-coded/chain mechanism, limit *L* pinned by a boundary test). **The owner signed off and locked all five on 2026-11-18** (`req lock`, after reading the independent Fable review); `GG-0063` (its only defect is the `GG-SCHED-0004` citation, resolved by the `superseded_by` chain) and `GG-0013` (cosmetic "vendored stdlib" wording) are left as is, not re-issued. `req lint` gains rule (e), a locked-text hash check | [ADR-0012](../dev/adr/ADR-0012-open-coded-defers.md) (re-issue section), ADR-0011 (status corrected), SPEC §3.2, §3.5, §4.5, §7.15, Appendix B |
| 2026-11-19 | **Re-issue (owner decision):** `GG-0061` is superseded by GG-0072 (owner locked it 2026-11-19) because its locked text is false under ADR-0015 (one `long[]` plus one `Object[]` per struct, typed-Java-array-backed slices `{array, offset, len, cap}`): GG-0072 states the three regions (`long[]`, `Object[]`, `byte[]` for one-byte leaves), the one slice shape `{base, prim, ref, byt, len, cap}`, string windows into a `byte[]`, checked conversions with view pointers and header views, and keeps every other obligation; history ends "drafted for ADR-0015; awaiting owner sign-off" and it is **not locked** (GG-0062 is unchanged) | [ADR-0015](../dev/adr/ADR-0015-pointer-model-w13.md), SPEC §3.4, Appendix B |
| 2026-11-20 | Direct handoff to the woken goroutine (gc's runnext) with a bounded handoff spin: a goroutine's wake is deferred and, if it parks within 20 µs, it hands the woken goroutine its slot and quantum through one HANDOFF token; requirements unchanged (WP-SCHED, lane sched-handoff) | SPEC §3.5, [ADR-0016](../dev/adr/ADR-0016-direct-handoff.md) |

## Tracks

**T0 — make what exists honest** (no architecture dependency; start now)
- T0.A runtime lifecycle: exit when main returns, fatal on goroutine panic,
  deadlock detection, panic value model, GOMAXPROCS wiring.
- T0.B JIT enablement: PE boundaries, PE-friendly `Safepoint.poll`, measured
  speedup, regression guard.
- T0.C test infrastructure: snapshots under gc, working mutation gate,
  per-push gate that catches the `floorMod` mutant, remove `phases/` paths,
  widen layer-2 triggers, prune stale exclusions.
- T0.D flatten the same-FQN stub overlay (truffle+runtime → one module).
  Done 2026-11-18: [internal/backend/truffle/](../../internal/backend/truffle/)
  is the one core module (nodes, values, runtime, scheduler); the 8 stubs,
  the `isPhase2()` classpath-order guards and the overlay tests are
  deleted; interop builds downstream.

**T1 — architecture (SPEC §3.1–3.3)**: design, then build, the per-package
`-toolexec` compile/link path: `compile` emits typed IR into the package
archive, `link` bundles it into a launcher; the real stdlib compiles through
it; delete `synth_*.go` and the v1 JSON frontend. Design doc first
(`toolexec-design.md`, written by lane T1).

**T2 — language integration**: `TruffleLanguage` registration + polyglot
`Context` (GG-0005/0024), then Go↔Java interop and debugging. TR-0 (tool/TCK
jars, `make pe-check`) and TR-1 (language shell; the product launcher runs
programs through `Context.eval`) landed 2026-11-18 — see
[truffle-design.md §13.4](truffle-design.md).

**T3 — modes** (gated on the P-D and P-F measurements, truffle-design
§13.2): native-image AOT, hybrid, isolates, mode selection
(GG-0006/0049/0050/0060/0025/0026), reachability metadata (GG-0052) —
work packages WP-AOT, WP-MODE, WP-OSR, WP-ISO1, WP-ISO2 below.

**T4 — remaining pillars**: the syscall executor's poller (GG-0063) and
spawn helper (GG-0064) land with toolexec W8/M4; cgo→Panama (GG-0022,
WP-CGO1–3), pinning (WP-PIN), plugin (WP-PLG), the upgrade toolchain
(WP-UPG1/2, WP-PAR), retiring the launcher deviations and faithful
GOTOOLCHAIN selection (WP-DEV); `go env`/vet reuse fall out of T1's wrapper
driver.

## Roadmap: work packages and requirement coverage

Every live requirement has at least one work package that delivers it and an
acceptance gate that can fail. Work packages already defined elsewhere are
referenced by their IDs: toolexec **W1–W13** and milestones **M0–M5**
([toolexec-design.md](toolexec-design.md) §8–§9), Truffle **TR-0–TR-13** and
measurements **P-A–P-F** ([truffle-design.md](truffle-design.md) §10.2,
§13.2), and the landed **T0.A–T0.D** lanes above. The packages below are the
ones the 2026-11-18 spec-conformance review found missing. Owners are tracks
(a lane is opened per package when it starts). Effort is focused
agent-days, `[EST]`, as ranges; none is measured yet.

### New work packages

| WP | Owner | Requirements | Scope | Depends on | Acceptance gate (each must be able to fail) | Effort |
|---|---|---|---|---|---|---|
| WP-GODEBUG | T1 (W5 `internal/godebug` patch; v1 launcher until M3) | GG-0068 | the GODEBUG validator follows GG-0068: unknown keys, bare fields, unparsable values accepted silently as gc does, at startup and on later updates; startup failure only for an unsupported knob whose effective value (environment, `//go:debug`, `go.mod` `godebug`, go-version default) is not the default; the six v1 tests that assert the old rule inverted | — (v1 now), W5 (new path) | differential vs gc of a program run with `GODEBUG=bogusknob=1,schedtace=100,schedtrace,schedtrace=abc`: same output and exit status; an unsupported non-default value still denied; the inverted tests fail on today's validator first | 1–2 |
| WP-DEV | T1, T4 | GG-0001, GG-0002, GG-0011, GG-0046 | retire ADR-0010's DV1–DV6 at their triggers; faithful `GOTOOLCHAIN` selection over graalgo distributions | M5 (DV1, DV5), W6 intrinsics (DV3), WP-CGO2 (DV4), graalgo release packaging (DV2), M2 coverage / WP-MUT fuzzing / owner decision on race and sanitizers (DV6) | per deviation: the txtar matrix and the CLI-surface diff (`flagdiff` of `--help`, `go env`, `go version`) show no difference attributable to it; GG-0046's toolchain-switch scenario tests pass | 6–15 |
| WP-CGO1 | T4 | GG-0022 | the C ABI struct-layout engine (offsets, alignment, `_Bool`, bitfields, unions, packed and flexible members) for linux/amd64, then linux/arm64 | — | layout differential vs the host gcc on a generated corpus of ≥ 1,000 C types (`sizeof`/`alignof`/`offsetof` probes): zero mismatches; a seeded bitfield error turns it red | 10–20 |
| WP-CGO2 | T4 | GG-0022, GG-0057 | execute cgo's generated stubs through Panama: downcall handles, upcall stubs for exported Go functions, `Arena` scoping for `C.CString`/`C.GoBytes`, C calls on an off-pool platform-thread pool | WP-CGO1, W8a (executor), M2 | the Phase-4 cgo corpus (cmd/cgo test programs + a `misc/cgo` subset) differential vs gc on linux/amd64; no `jdk.VirtualThreadPinned` on scheduler carriers during cgo calls; ADR-0010 DV4 retired | 20–40 |
| WP-CGO3 | T4 with momo | GG-0058 | MM-0017 verifier rule: `import "C"` and the Panama mapping rejected in momo module code | WP-CGO2 | the verifier rejects a module importing `"C"` and accepts the same code as host code (red-green) | 1–3 |
| WP-POLLCOST | T2 | GG-SCHED-0001, GG-0017 (unchanged; replaces TR-6 and A7, which P-B rejected, ADR-0013) | **LANDED 2026-11-19 (lane perf-peak; [performance.md](performance.md) §3): neither prototype — the poll's fast path is one volatile load of a shared poll epoch that a ticker thread bumps every ≤ 1 ms (and every exit), compared with the thread's last-seen epoch; no store, no countdown, no hidden argument. `loop` 14.8 → 6.1 ms best (gc 4.5), `slicesum` 11.9 → 6.5, `fib` 5.0 → 3.3; torture F1/F3/F4 green (F6 hangs intermittently on master too: BUG-0038); seeded gate: [preempt_spin.go](../../test/m1r/programs/preempt_spin.go) times out without the back-edge poll.** Original scope: make `Safepoint.poll()` cheaper on tight loops without Truffle safepoints: prototype the flag fallback (a per-goroutine `preempt` flag set by the scheduler's monitor, poll = one load and one branch the compiler must repeat — the read must not be hoistable) reusing the P-B branch's `Preemptor` (branch `wt/pb/safepoint`), and/or strip the per-back-edge thread-slot store and the volatile `exitEpoch` read (formerly `exitPending`) from today's poll (fold the exit check into the strided full poll, count in a frame local); keep the winner, delete the loser | TR-1 (landed), TR-3 | `loop`, `slicesum` and `fib` no worse than today's poll (target: `loop` toward the 1.0 ms of a poll-free loop, 18.3 ms today, gc 4.4 ms) in `make bench`; torture legs F1, F3, F4 and F6 green; GG-FAIR/GG-SCHED-0001 observables unchanged; a seeded variant whose back-edge has no poll starves the fairness probe, so the gate can fail | 3–6 |
| WP-NODETEST | T2 | GG-0014, GG-0071 (compiled-graph clause), GG-0010 | the per-node tests of truffle-design §11 waived for M1a by ADR-0014: T4 for the dynamic sites first (`Calls.Dispatch`, `Calls.IfaceDispatch`, DSL arithmetic/conversion nodes: monomorphic, polymorphic at the limit, megamorphic, nil; specialization asserted by DSL introspection); GG-0071's compiled-graph check (no `G` access and no allocation beyond the argument array on an open-coded function's non-panic path); T1's PE half for every node reachable from a hot loop | W4 (landed) | a seeded missing `@TruffleBoundary`, a seeded extra allocation on the open-coded path and a site forced past its cache limit each turn the suite red; the suite runs in `make test-java-fast` | 6–10 |
| WP-PIN | T4 | GG-0057, GG-SCHED-0006 | pinning audit of W8's downcalls and blocking file I/O: each may-block role-table entry is bracketed and either off-pool or counted in the +k carrier budget (mechanically, by R15's table-vs-source gate extended to the may-block bit); Phase-2 pinning matrix | W8b | audit record listing every may-block entry with its disposition; the `CapExhaustion` falsifier ported to downcalls green at cap = GOMAXPROCS; JFR `jdk.VirtualThreadPinned` audit over the corpus | 3–6 |
| WP-SCHED | T2 | GG-0048, GG-SCHED-0001–0003, GG-WAKE-0001–0003, GG-FAIR-0001–0003 | **Progress 2026-11-20 (lane sched-handoff): [BUG-0038](../dev/bugs.md) fixed — the F6 lost wake-up was a waker classifying a select waiter by a self-claim peek before its claiming CAS (the selector self-claims under another channel's monitor in between); the claim is now classified by the CAS alone (`ChanWaiter.tryClaim`); deterministic, stress and gc-differential regressions ([select_send_races.go](../../test/m1r/programs/select_send_races.go)); F6 10/10. Direct handoff landed ([ADR-0016](../dev/adr/ADR-0016-direct-handoff.md), [performance.md](performance.md) §3.1): `chanpingpong` 10.5 → 1.13 ms (gc 0.31), the handoff in the model check (`WakeupModelTest`: HANDOFF scripts, a slot-accounting check after every action, seeded bugs found) and a starvation probe (`testHandoffPairDoesNotStarveSpinner`). Still open: server-hardware re-measurement, SPEC §7.2 observables.** Scope: re-validate the scheduler on the production path: torture and fairness batteries through the new node package (A7 is decided: rejected, ADR-0013), server-hardware re-measurement, model check of the park/unpark protocol | TR-3, WP-POLLCOST | SPEC §7.2's five observables equivalent vs gc; per-hop overhead ≤ 1 % at 10 ms on server hardware; a TLA+/JPF model of the drainer protocol covering the R1 interleaving list finds no counterexample (and finds the seeded ack-after-unpark bug) | 8–15 |
| WP-AOT | T3 | GG-0006, GG-0050, GG-0052 | pure-AOT images: generic runtime image and per-program images; tracing-agent reachability metadata for all of std with the hand-authored tax measured | TR-12, P-D, P-F | hello-world, the benchmark set and an std-test subset gc-identical in mode (b); 0 hand-authored metadata entries, or a measured tax recorded and accepted; GG-0050's target fixed from P-D (A2) and met | 15–30 |
| WP-MODE | T3 | GG-0049, GG-0060, GG-0006 | hybrid images (mode (c)); mode selection by build flag/environment with documented defaults; SPEC §8 rows 8–13 as the meta-gate | WP-AOT, TR-12 | meta-gate report with rows 8–13 measured; default = hybrid iff it beats both single modes on ≥ 1 workload class; the corpus is output-identical across the three modes | 12–25 |
| WP-OSR | T3 | GG-0054, GG-0027 | verify OSR inside a native image and `SpawnIsolate` maturity on the pinned GraalVM | TR-12 | dated results appended to truffle-design §13: a long-loop OSR test in mode (c); isolate create/teardown and cross-isolate call latency on the pin; each outcome selects SPEC §7.12/§7.9's main path or fallback | 2–5 |
| WP-ISO1 | T3 | GG-0025, GG-0027 | isolate per momo module generation: create/teardown, crash-only restart, `MaxHeapSize` + `isResourceExhausted` → Go panic → restart, cross-isolate calls under `SCOPED`; CPU-quota gap documented | WP-OSR, WP-MODE | a fault injected in one generation leaves the others running; a heap-quota breach is a Go panic in that module; latencies compared with momo's kill+join baseline | 15–30 |
| WP-ISO2 | T3 | GG-0026 | JIT-mode fallback: one OS process per generation (spawn helper, GG-0064) with a documented weaker containment | W8b, TR-5 | WP-ISO1's fault-injection test passes in mode (a); process-per-generation overhead measured against the dev-loop budget | 8–15 |
| WP-PLG | T4 | GG-0051 | `plugin.Open` in mode (a) (a `-buildmode=plugin` bundle loaded into the running context); documented error plus exclusion entry in (b)/(c) | M5 (build modes), TR-5 | std `plugin` tests green in mode (a); `plugin.Open` returns the documented error in (b)/(c); exclusion entry with reason code | 5–10 |
| WP-PERF | T2 | GG-0007 | **IN PROGRESS (lane perf-peak, 2026-11-20): the suite, method, current table vs gc and gc+PGO and the ranked gap analysis are [performance.md](performance.md) (`make bench-suite`). Lane perf-calls (2026-11-20): strip-mined polls in counted loops, no entry poll in call-free functions, splitting by GIR re-decode (`cloneUninitialized`, benchmark `ifacesplit`), read-only value receivers share the interface data word, word-at-a-time bytealg kernels — suite geomean vs gc 2.26× → 1.56× (1.65× vs gc+PGO, performance.md §4); `loop` and `arraymat` beat gc+PGO (vectorization), recorded as measurements, not yet as SPEC §2.6 claims.** beyond-gc benchmarks against gc **with PGO**: vectorization, always-on profiling, partial escape analysis, the devirtualization cascade (SPEC §2.6.1 falsifying suite) | TR-4 | per claimed win, a named benchmark that beats gc+PGO at steady state, or the claim is struck from the docs; results table per GraalVM pin | 8–15 |
| WP-JFR | T2 | GG-0008, GG-0024 | JFR events `graalgo.GoroutineStart/End`, `GoroutinePark` (rate-controlled), `ChannelBlock`, `ModuleCrash`; GC mapping; heap dumps; VisualVM in mode (a) | TR-5, TR-10 | a recording of `chanpingpong` contains every event type with correct goroutine ids and the park sampling rates of SPEC §3.10 (`jfr print` test, red without the events) | 6–10 |
| WP-UPG1 | T4 | GG-0035, GG-0021, GG-0068 (`godebug-diff`) | `apidiff`, `go/types` signature diff, `rtlink`, `flagdiff`, `godebug-diff`, `respdiff` as maintained tools (from the spike-3 prototypes and toolexec E2) | — | N-vs-N output empty; N-vs-N+1 equals `api/go1.N+1.txt`'s content; `respdiff` flags the 1.26→1.27 response-file change | 8–15 |
| WP-UPG2 | T4 | GG-0004, GG-0038–GG-0043, GG-0044, GG-0056, GG-0069 | the nine-phase runbook as scripts (including the toolchain bump of GG-0069 and the fork-size audit of GG-0044), patch-release light path, release-watch agent (prepares, never merges), toil accounting with mechanical bound checks, breach procedure, agent-leverage metric | WP-UPG1, W5, M5 | a light-path dry run go1.27.0 → go1.27.1 and an rc intake report produced by the agent; the upgrade report computes every GG-0042 bound; a waived gate without a recorded reason fails the report | 10–20 |
| WP-PAR | T4 | GG-0045 | the parity-stance log (architecture log): one entry per runtime-internal behaviour without a JVM analog (GC, scheduler, malloc size classes, GC/scheduler GODEBUG knobs) | — | the log exists and SPEC §6.7 links it; every `no-op`/`unsupported` GODEBUG registry entry cites a stance (checked by a script) | 1–3, then per release |
| WP-FRZ | T1 | GG-0059 | interface freeze: the versioned intrinsic registry (generated from annotated Java methods) and the `-toolexec` argv contract; CI check that GIR call sites and the node factory use only frozen signatures | W6 | the CI check turns red on a seeded call to an unregistered intrinsic and on an argv-contract change without a version bump | 3–6 |
| WP-MUT | T0.C, T1 | GG-0034, GG-0037, GG-0029 (layer 4) | the mutation gate on the new path (mutants in the new node package); `testing.F` differential fuzzing of std functions under gc and graalgo; a program generator (a verified gosmith-class tool, else template-based) with minimization | M2, W10 | the `%`→`floorMod` mutant and the nine spike-7 mutation classes detected on the new path; a nightly fuzz job whose minimized divergences become committed regressions; overnight soak per release | 8–15 |
| WP-REFLECTLITE-VIEW | T1 (lane opened when the owner decides) | GG-0072, GG-0009 | delete the two `TEMPORARY (BUG-0037)` registry `replace` intrinsics (`internal/reflectlite.Swapper`, `pkgPath`) and run `internal/reflectlite` verbatim: needs checked views over wider scalars (a `[]int32` over `float32`/`uint32` leaves or several leaves, a `[4]byte` over an `int32`). Options: a word-view slice shape (revisit GG-0072 / [ADR-0015](../dev/adr/ADR-0015-pointer-model-w13.md) decision 3), or per-kind typed views; **owner decision pending** | W13, W9a | [reflectlite_views.go](../../test/m1r/programs/reflectlite_views.go) still matches gc under `make test-m1r` with both registry entries deleted; `TestTemporaryEntries` (`internal/toolexec/intrinsics`) fails while an entry remains and BUG-0037 is marked fixed | 2–5 |
| WP-CALLERS | T2 | GG-0070 (caller frames), GG-0009 | call positions in the node package's frames so that `runtime.Caller`, `Callers`, `CallersFrames` and `FuncForPC` answer as gc ([BUG-0047](../dev/bugs.md)): reflect's method names in panics, `log`'s file:line, `testing`'s helpers | W4 (landed), source sections (truffle-design §6) | the `panic.unexported-set` check returns to [reflect_probe.go](../../test/m1d/programs/reflect_probe.go) and a `log.Lshortfile` program matches gc; a seeded off-by-one frame skip turns both red | 3–5 |
| WP-TMELIDE | T2 (exploration; owner TBD, lane opened when TSX hardware is available) | GG-0007 | exploratory hardware transactional lock elision for `sync.Mutex`/`RWMutex` ([PHASES.md](PHASES.md) §7, SPEC §2.6 `[PROPOSAL]`, [htm-lock-elision.md](../../research/phase5-opt-survey/htm-lock-elision.md)): step 0 acquire a TSX-enabled Xeon (this dev host is AMD Zen 5: no `rtm`/`hle` flags; a cloud or bare-metal Sapphire/Emerald/Granite Rapids whose `/proc/cpuinfo` shows `rtm`); step 1 the ceiling with gc plus a Go-assembly TLE wrapper (no JVM); step 2 a JVM emission spike (Panama critical downcalls; GraalVM upstream asked about an RTM intrinsic); step 3 an adaptive graalgo prototype | TSX hardware access; WP-PERF suite | a dated go/no-go note with measurements on real RTM hardware: B1–B6 of the note across 1–32+ threads, abort causes and fallback rates; kill on < 10% ceiling win at ≥ 8 threads, no abort-safe emission path, < 10% over gc+PGO plain locks on a realistic workload, > 2% uncontended regression, or any differential/deadlock/fairness divergence; no hardware → the note records that and the item parks | 3–6 to go/no-go on step 1; 25–45 through step 3 |

### Requirement → work package coverage

"landed" means implemented and verified on the shared runtime or the
toolexec module; it still needs the listed packages on the new path.
`GG-0047` is the only requirement without a work package: it is a decision
rule (Sulong vs direct AST) whose re-test triggers are watched, not built.

| Requirement | Delivered by |
|---|---|
| GG-0001 Drop-in CLI surface | W1 (landed), M5 txtar matrix, WP-DEV |
| GG-0002 Drop-in build/test/module behaviour | W1, W7, M2, M5, WP-DEV |
| GG-0003 Extra JVM APIs | TR-9 |
| GG-0004 Bounded version tracking | WP-UPG1, WP-UPG2 |
| GG-0005 Go↔Java interop | TR-1 (landed), TR-8, TR-9 |
| GG-0006 Three execution modes | TR-12, WP-AOT, WP-MODE |
| GG-0007 Beyond-gc optimization bar | TR-4, WP-PERF, WP-TMELIDE (exploratory) |
| GG-0008 Modern JVM integration | TR-1 (landed), W8b (poller), WP-JFR, WP-ISO1 |
| GG-0009 Stdlib reuse | W5, M1r–M4, M3 (`synth_*.go` deleted) |
| GG-0010 Testing built early | T0.C (landed), W10 |
| GG-0011 Driver wrapper via `-toolexec` | W1 (landed), WP-DEV (DV5) |
| GG-0013 No language pinning | W3, M1a gate |
| GG-0014 One Truffle AST | W4/TR-3, TR-4, TR-13, WP-NODETEST |
| GG-0017 Goroutines on virtual threads | shared scheduler (landed), TR-1, TR-5, WP-POLLCOST (A7 rejected, ADR-0013), WP-SCHED |
| GG-0070 Panic/defer/recover/Goexit semantics and caller frames (locked 2026-11-18; with GG-0071 supersedes `GG-0067`) | T0.A (landed, v1), TR-3, TR-7, W10 (traceback normalizer rules T4–T6) |
| GG-0071 Defer mechanism: open-coded up to *L*, chain otherwise (locked 2026-11-18) | TR-3 (A8 accepted; landed in W4 with the *L* boundary test, M1a close-out), WP-NODETEST (compiled-graph check) |
| GG-0020 Intrinsic surface | W5, W6, W8, W9, M5 coverage audit |
| GG-0021 Linkname by symbol name | W6, M5 linkname-diff, WP-UPG1 (`rtlink`) |
| GG-0022 cgo via Panama | WP-CGO1, WP-CGO2 |
| GG-0023 Linux-first | W1 (`GOOS`/`GOARCH` check), W8 |
| GG-0024 Debugging and profiling | TR-7, TR-10, TR-11, WP-JFR |
| GG-0025 Isolates for momo | WP-ISO1 |
| GG-0026 JIT-mode isolation fallback | WP-ISO2 |
| GG-0027 Pinned GraalVM | pin recorded (SPEC Appendix C), WP-OSR |
| GG-0028 Build-cache compatibility | W1 (landed `-V=full`), M2 |
| GG-0029 Four test layers | T0.C (landed), W10, WP-MUT |
| GG-0030 Differential oracle | T0.C (landed: snapshots under gc), W10 |
| GG-0031 Nondeterminism controls | T0.C (landed), W10 |
| GG-0032 Conformance corpora | W10, M2 (std tests), M5 (txtar) |
| GG-0033 Exclusion list | W10, M2 |
| GG-0034 Mutation-validated oracles | T0.C (landed, v1 path), WP-MUT |
| GG-0035 API/ABI diff tooling | WP-UPG1 |
| GG-0036 Behaviour snapshots | T0.C (landed) |
| GG-0037 Generative differential fuzzing | WP-MUT |
| GG-0038 Gate integration and triage | WP-UPG2 |
| GG-0039 Nine-phase runbook | WP-UPG2 |
| GG-0040 Patch-release light path | WP-UPG2 |
| GG-0041 Release-watch agent | WP-UPG2 |
| GG-0042 Toil accounting | WP-UPG2 |
| GG-0043 Breach handling | WP-UPG2 |
| GG-0044 Patch-stack discipline | W5 (fork-size audit), WP-UPG2 |
| GG-0045 Parity stances | WP-PAR |
| GG-0046 GOTOOLCHAIN fidelity | WP-DEV |
| GG-0047 Sulong decision rule | — (decision rule; SPEC §7.1 re-test triggers) |
| GG-0048 Preemption-equivalence bar | WP-POLLCOST (P-B failed, A7 rejected, ADR-0013), WP-SCHED |
| GG-0049 Hybrid meta-gate | WP-MODE |
| GG-0050 AOT baseline quality | P-D, WP-AOT |
| GG-0051 plugin.Open scope | WP-PLG |
| GG-0052 Reachability metadata | WP-AOT |
| GG-0053 go vet reuse | W1 (landed pass-through), M5 (vet scripts) |
| GG-0054 OSR verification | WP-OSR |
| GG-0056 No agent-leverage discount | WP-UPG2 |
| GG-0057 Pinning audit | WP-PIN, WP-CGO2 |
| GG-0058 MM-0017 gate on FFI | WP-CGO3 |
| GG-0059 Interface freeze | WP-FRZ |
| GG-0060 Mode selection | WP-MODE |
| GG-0072 Flat value model with a byte region (locked 2026-11-19; supersedes `GG-0061`) | W4, W13 (ACTIVE, lane `w13`; [ADR-0015](../dev/adr/ADR-0015-pointer-model-w13.md)) |
| GG-0062 unsafe.Pointer/uintptr | W13 (ACTIVE, lane `w13`; [ADR-0015](../dev/adr/ADR-0015-pointer-model-w13.md)) |
| GG-0063 Runtime epoll poller | W8b |
| GG-0064 Spawn helper | W8b |
| GG-0068 GODEBUG registry, gc-compatible parsing (locked 2026-11-18) | WP-GODEBUG, W5, WP-UPG1 |
| GG-0069 Frontend of the tracked release, unmodified (locked 2026-11-18) | W1/W2 (landed: tracked-release test, `-goversion`/`-V=full` checks), graalgo-tool refuse-to-run check and the toolexec module's `toolchain` line (landed at the M1a close-out), WP-UPG2 |
| GG-SCHED-0001–0003 | shared scheduler (landed), WP-POLLCOST (A7 rejected, ADR-0013), WP-SCHED |
| GG-SCHED-0006 (locked 2026-11-18) | W8a, W8b, WP-PIN |
| GG-WAKE-0001–0003 | shared scheduler (landed), WP-SCHED |
| GG-FAIR-0001–0003 | shared scheduler (landed), WP-SCHED |

Totals of the new packages: 156–310 agent-days `[EST]`, on top of
toolexec M0–M5 and TR-0–TR-13. Superseded requirements (`GG-0012`,
`GG-0015`, `GG-0016`, `GG-0018`, `GG-0055`, `GG-SCHED-0004`, and the
re-issued `GG-0065`, `GG-0066`, `GG-0067`, `GG-SCHED-0005`) are covered
through their successors.

## Layering debt

The new path (`graalgo.go.*`, and `internal/toolexec`) must not depend on
v1 code, which is deleted at M3. [check_layering.py](../../.github/scripts/check_layering.py)
(`make check`) follows every reference transitively from `graalgo.go.*`
through the shared code and fails on any edge into `graalgo.frontend`,
`graalgo.values`, `graalgo.p3*`, `graalgo.iface`, `graalgo.rtstack` or
`graalgo.rtmemstats`, and on any `internal/toolexec` import of
`internal/{frontend,driver,vet}`. The 38 edges present when the gate landed
(2026-11-18) are grandfathered, shrink-only, in
[layering_allowlist.txt](../../.github/scripts/layering_allowlist.txt);
`PanicNilError` was split off then (its v1 descriptor moved to
`RuntimeErrorTypes`). The rest is not a mechanical split and is left to the
owning lanes, in this order:

1. `graalgo.language` (`GoLanguage`, `GoContext`, `GoProgramBuilder`,
   `GoProgramRootNode` → `frontend.NodeFactory`, `WireKindRegistrar`,
   `FrontendException`): the language shell serves both paths; the v1 wire
   build moves behind a v1-only builder interface that the bundle path never
   loads (T2, with the m1a-closeout/m1r-facade lanes, which edit this code
   now). Deleted at M3 otherwise.
2. `graalgo.runtime.Intrinsics` (→ `values.GoMap/GoSlice/GoKind/
   GoTypeDescriptor/GoStructDescriptor/GoComplex*`, `frontend.Boxing`,
   `FrontendException`): the v1 intrinsic surface; the new path has
   `graalgo.go.runtime`. Split the entry points the new path reaches, the
   rest goes at M3.
3. `graalgo.runtime` helpers (`Panic`, `PanicRendering`, `RuntimeErrorTypes`,
   `BuiltinPrint`, `DeferredCall`, `GoCalls`, `GoStrings`) and
   `graalgo.sched.SchedulerCore` (→ `values.GoFunc`, `GoString`,
   `GoTypeDescriptor`, `GoKind`): typed on the v1 value model; each splits
   into a v1-free core and a v1 adapter deleted at M3 (T2 runtime/scheduler).

## Working rules

[AGENTS.md](../../AGENTS.md) is the canonical working contract — worktrees
and integration, build and test gates, correctness and generality rules,
`nice` and load-shedding. The debt-paydown lane is updating it for this box;
once that lands it is the only source, and this plan states no rules of its
own. Toolchain paths are in [ENV.md](../dev/ENV.md).
