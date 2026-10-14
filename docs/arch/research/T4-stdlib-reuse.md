# T4 — Stdlib Reuse Analysis: runtime coupling classification (go1.27.1)

**Primary source:** `~/tools/go` (`VERSION`: go1.27.1). All numbers below are measured with `rg`/`find`/`wc` over `src/`, excluding `src/cmd/**`, `src/vendor/**`, and `*testdata*` dirs unless stated. Method notes accompany every number. [UNVERIFIED] marks claims not confirmed by a source read.

## 1. Headline numbers

| Metric | Value | Method |
|---|---|---|
| Non-test `.go` files (stdlib, excl. cmd) | 3,988 | `rg -l '^package '` with `-g '!*_test.go' -g '!cmd/**'` |
| Package dirs (same scope, excl. vendor+testdata) | 398 | distinct dirnames of the above |
| "Pure Go" packages (no `unsafe`, no `runtime` import, no `//go:linkname`, no `.s`) | 229 (57.6%) | per-dir 4-way check, §3 |
| Package dirs importing `runtime` | 70 | `^\s*"runtime"` import lines, non-test |
| Files with `//go:linkname` (non-test) | 296 in 78 dirs | `rg -l 'go:linkname'` |
| Files importing `unsafe` | 849 | `"unsafe"` matches (proxy; includes re-exports) |
| Plan 9 asm files | 485 files, 126,968 lines | `find -name '*.s'`, excl. cmd/vendor/testdata |
| Distinct `runtime.*` symbols stdlib links INTO runtime | 52 | two-arg linknames, §6 |

**Bottom line:** a majority of stdlib packages are pure Go by the strict 4-way metric, but the most load-bearing packages (`bytes`, `strings`, `sync`, `os`, `net`, `time`, `reflect`, `syscall`, `internal/bytealg`, `internal/poll`, `math`, `crypto/*`) are all coupled. "Run unchanged" still means: compile them with a Go frontend onto Truffle — the language semantics (closures, interfaces, generics, defer) must exist; only the *runtime service* calls differ.

## 2. Minimal runtime-intrinsic surface a new backend must implement

This is the compiler+stdlib contract: symbols the gc frontend emits calls to (spot-checked by grepping `src/cmd/compile/internal/` for each name; hit counts in §2.12) or that stdlib reaches via linkname. Numbered, with the defining runtime source file.

### 2.1 Memory ops
1. `memmove`, `memequal` (+`_varlen`), `memcmp`, `memhash` — `src/runtime/memmove*.go`, `memclr_*.s`; also `cmpstring` (used by `internal/bytealg` via linkname).
2. Typed variants: `typedmemmove`, `typedmemclr`, `typedslicecopy`, `memclrNoHeapPointers` — `src/runtime/mbarrier.go`, `mheap.go`.
3. `zeroVal` (linknamed by `reflect`, `internal/runtime/maps`) — `src/runtime/alg.go`.

### 2.2 Slices
4. `makeslice`, `makeslice64`, `growslice`, `growsliceNoAlias` — `src/runtime/slice.go`.

### 2.3 Maps
5. `makemap`, `makemap64`, `mapassign` (+`_fast32/64/str` and `ptr` variants), `mapaccess1/2` (+fast variants), `mapiterinit`, `mapiternext`, `mapdelete` (+fast), `mapclear` — implementations live in `src/internal/runtime/maps/*.go` and are re-exported into `runtime` via linkname (`runtime` pulls from `internal/runtime/maps`).

### 2.4 Channels and select
6. `makechan`, `chansend1`, `chanrecv1/2`, `close`, `selectnbsend/nbrecv`, `selectgo` — `src/runtime/chan.go`, `select.go`.

### 2.5 Interfaces and itabs
7. Conversions: `convT`, `convT64`, `convTstring`, `convTslice`, `convT2E`, `convT2I`, `convI2E`… — `src/runtime/iface.go`.
8. Assertions: `assertE2T/I2T/I2E/E2I…`, `panicdottype` — `src/runtime/iface.go`.
9. Equality: `efaceeq`, `ifaceeq` — `src/runtime/iface.go`.
10. itab construction: `getitab`, `itabAdd` — `src/runtime/iface.go`; type metadata layout `rtype`/`uncommonType` in `src/runtime/type.go` (+ `internal/abi` mirror). Any backend must reproduce the exact `rtype` memory layout because `reflect` reads it via `unsafe` (158 `unsafe.` uses in `reflect/value.go`, 85 in `reflect/type.go`).

### 2.6 Panic / recover / defer
11. `gopanic`, `gorecover`, `panicdivide`, `panicshift`, `panicoverflow`, `panicdottype`, `panicindex`, `panicslice*`, `panicwrap`, `deferproc`, `deferreturn`, `deferconvert` — `src/runtime/panic.go`, `defer.go`.

### 2.7 Goroutine scheduler
12. `newproc`, `goexit`, `gosched`, `mcall`, `gopark`, `goready`, `entersyscall`/`exitsyscall` (linknamed by `syscall/syscall_linux.go`) — `src/runtime/proc.go`, `mgc.go` stack growth (`morestack`/`lessstack` asm per arch, `src/runtime/asm_*.s`).
13. Stack management: per-arch `asm_*.s` (gogo, morestack, stack guard checks) — part of the 8,768 (amd64) / 10,606 (arm64) runtime asm lines.

### 2.8 Netpoller (the biggest OS-coupled subsystem)
14. `netpollinit`, `netpollopen`, `netpoll`, `netpollBreak`, `netpollWaiters` — 11 `src/runtime/netpoll_*.go` files (epoll/kqueue/IOCP/event ports).
15. Runtime **pulls** 9 functions from `internal/poll` via linkname: `poll_runtime_pollServerInit/isPollServerDescriptor/pollOpen/pollClose/pollReset/pollWait/pollWaitCanceled/pollSetDeadline/pollUnblock` — `src/runtime/netpoll.go`. A new runtime must implement the poller contract end, not just the runtime end.

### 2.9 Atomics
16. `sync/atomic` is a thin wrapper: real ops live in `src/internal/runtime/atomic/` (Go + 13 `.s` files, 4,090 lines) exported via `//go:linknamestd` (`Load`, `Store`, `Swap`, `CompareAndSwap`, `Xadd`, `Xchg`, `And`, `Or`, `Loadp`…). A new backend can intrinsify these (JVM has exact equivalents) and drop the asm.

### 2.10 Timers, randomness, time
17. `nanotime`/`nanotime1` (per-OS asm/Go), `startTimer`/`stopTimer`/`resetTimer`/`modtimer` — `src/runtime/time.go`; the `time` package pushes `newTimer/stopTimer/resetTimer` into the runtime via one-arg linkname, and pulls `runtimeNow`/`runtimeNano` [UNVERIFIED: exact direction of `runtimeNow` vs `nanotime` — verify by reading `time/time.go` against `runtime/timestub.go`].
18. `fastrand`, `fastrand64`, `fastrandn`, `rand`, `randn` — `src/runtime/rand.go`; linknamed by `sync` (`Pool`), `os` (tempfile), `net` (dnsclient), `unique`.

### 2.11 GC / write barrier / allocation
19. `mallocgc`, `newobject`, `newarray`, `writeBarrier`, `gcWriteBarrier*` — `src/runtime/malloc.go`, `mbatcher.go`; write barriers are compiler-emitted (5 refs in `cmd/compile/internal`), so any backend needs a barrier model or a GC that doesn't need them (JVM GC: no explicit barriers in Go code — a genuine simplification win).

### 2.12 Traceback / symbolizer / misc intrinsics
20. `callers`, `traceback`, `symbolize` — `src/runtime/traceback.go`, `symtab.go`; `runtime.Callers` is used by `log`, `testing`, `debug/*`.
21. `goprint*`/`print*` (printlock/printunlock) — `src/runtime/print.go`.
22. `reflectcall` (linknamed by `reflect`), `countrunes`, `complex128div`, `intstring`, `staticuint64s` — `src/runtime/...`.

Compiler-reference spot check (`src/cmd/compile/internal`, distinct files referencing each name): `memmove` 13, `memequal` 10, `mapassign` 8, `growslice` 8, `makeslice` 6, `ifaceeq`/`efaceeq` 6, `writeBarrier` 5, `mallocgc` 5, `deferreturn` 5, `concatstrings` 5, `makemap`/`mapaccess1/2`/`makechan`/`gopanic`/`cmpstring`/`chanrecv1`/`countrunes` 4 each, `chansend1`/`convT`/`gorecover`/`deferproc`/`selectnbsend` 3 each, `newproc`/`panicoverflow` 1 each (other call sites reference them via `ir` node constructors rather than literal names — the contract is real regardless).

## 3. Stdlib classification: what could run unchanged?

**Method:** 398 package dirs (non-test, excl. cmd/vendor/testdata). A dir is "pure" iff no non-test file in it (a) matches `"unsafe"`, (b) has a `^\s*"runtime"` import line, (c) contains `go:linkname`, (d) has a same-dir `.s` file. This is intentionally strict: it undercounts slightly (e.g. `errors` trips on one `unsafe` match) but never overcounts.

**Result: 229/398 ≈ 58% of package dirs are pure by this metric.**

Spot-checked pure examples (all four checks clean): `strconv`, `fmt`, `container/list`, `sort`, `cmp`, `io`, `path`, `encoding/json` has a `runtime` import (uses `runtime` for error stack info? — flagged, not pure), `net/http` has linknames. Cautionary near-misses that LOOK pure but aren't:
1. `bytes`, `strings` — import `unsafe`; `bytes` also has a linkname file.
2. `slices`, `maps` — import `unsafe`; `maps` has a linkname file.
3. `encoding/json` — imports `runtime`.
4. `math` — 54 asm files, 7 linkname files (arch float bits).
5. `reflect` — the poster child of coupling: 158 `unsafe.` uses in `value.go`, linknames `reflectcall`/`zeroVal`/`getStaticuint64s` from runtime, and the runtime linknames ~30 `reflect.*` symbols back in.

**Packages importing `runtime` (70 dirs, selected load-bearing ones):** `os`, `net`, `net/http`, `time`, `reflect`, `sync`, `syscall`, `internal/poll`, `internal/reflectlite`, `testing`, `flag`, `log`, `log/slog`, `expvar`, `database/sql`, `crypto/tls`, `crypto/x509`, `os/exec`, `os/signal` (via subpkgs), `path/filepath`, `os/user`, `iter`, `weak`, `unique`, `arena`. (Full list in the research notes: 70 dirs.)

**Packages with `//go:linkname` (78 dirs / 296 files):** heaviest by file count — `syscall` 24, `internal/syscall/unix` 15, `os` 7, `net/http` 7, `net` 6, `internal/cpu` 6, `internal/runtime/maps` 5, `time` 4, `reflect` 3, `internal/poll` 3, `internal/bytealg` 3, `crypto/tls` 3, `sync` 2, `math/bits` 2, `internal/runtime/atomic` 2, `go/types` 2. (Note: `internal/cpu`, `go/types`, `go/build` are toolchain-facing, not end-user stdlib.)

**What "unchanged" really requires:** (1) a Go frontend that lowers generics/closures/defer/interfaces to Truffle nodes; (2) the intrinsic surface of §2 implemented as Truffle builtins; (3) `internal/bytealg`, `internal/cpu`, `internal/runtime/atomic|maps` reimplemented as intrinsics (their linkname targets must exist). Everything else in the 229 pure packages compiles as ordinary Go. Transitive closure caveat: a pure package that imports a coupled one (e.g. `net/http` ← `internal/poll`) inherits the coupling — the 58% is a per-package, not per-program, number.

## 4. The `.s` (Plan 9 assembly) surface

**Method:** `find src -name '*.s'`, excluding `src/cmd/**` (32 vendored `golang.org/x/sys/unix` files), `src/vendor/**` (17), `*testdata*` (74). Result: **485 files, 126,968 lines** across 49 package dirs.

### 4.1 Per-GOARCH volume (filename suffix `*_<arch>.s`)

| GOARCH | files | lines | runtime pkg lines | crypto+math lines |
|---|---|---|---|---|
| amd64 | 97 | 39,937 | 8,768 | 17,664 |
| arm64 | 78 | 20,227 | 10,606 | 4,655 |
| riscv64 | 42 | 8,588 | 3,966 | 2,155 |
| 386 | 49 | 9,957 | — | — |
| s390x | 66 | (not counted) | — | — |

(amd64/arm64/riscv64/386 counted precisely; s390x/arm/loong64/ppc64/mips/wasm omitted from line counts — same method applies. 70 files carry no arch suffix; most are shared stubs or nonstandard names.)

Top `.s` holders: `runtime` 182 files, `internal/bytealg` 46, `math` 42, `syscall` 39, `reflect` 14, `internal/runtime/atomic` 13, `math/big` 12, `runtime/cgo` 11, `internal/runtime/syscall/linux` 10.

### 4.2 Strategy per category

1. **Runtime core (`runtime/*_*.s`, ~8.8k amd64 lines): scheduler entry, stack growth (`morestack`), signal trampolines, `nanotime1`, memmove/memclr.** → (a) intrinsify/replace: a Truffle runtime doesn't need Go-stack-switching asm at all; signal handling and nanotime map to JVM/OS facilities. This whole category evaporates by design — biggest win.
2. **`internal/runtime/atomic` (13 files, 4,090 lines): CAS/load/store per arch.** → (a) intrinsify: one Truffle/JVM intrinsic per op; delete all asm.
3. **`internal/bytealg` (46 files): memequal/index/strcmp per arch, incl. AVX2/NEON variants.** → (a) intrinsify hot paths (memchr/memcmp map to `Unsafe`/vector APIs), (b) Go fallback for the rest. Note its only linkname imports are `memequal`, `memequal_varlen`, `cmpstring` from runtime.
4. **`internal/runtime/syscall/linux` (10 files): raw syscall stubs.** → (a) reimplement per OS as JNI/JVM calls; Linux-first only (§5).
5. **Crypto (`crypto/*`, `crypto/internal/fips140/*`, ~17.7k amd64 lines): AES/SHA/chacha asm.** → (a) intrinsify via JVM crypto provider where semantics match (AES-NI, SHA-NI), (c) emulate the rest in Go initially — correctness first, performance later. This is the largest asm category on amd64 and the most version-churny (FIPS 140 code moves every release).
6. **`math`/`math/big` (54 files): arch float bits, big-int kernels.** → (a) intrinsify float ops (JVM gives IEEE754 exact), (b) `math/big` kernels in Go initially (they're already mostly Go; asm is a speedup).
7. **`reflect` (14 files): call frame / `reflectcall` trampolines, `makeFunc` stubs.** → (a) must be reimplemented as Truffle frame machinery; deep coupling to the calling convention — treat as part of the runtime, not "stdlib asm".
8. **`syscall` (39 files): `rawsyscall`, `syscall6/10`, arch entry.** → (a) per-OS reimplementation, Linux-first.
9. **`runtime/cgo` (11 files): cgo thread entry.** → defer with cgo itself (see §5).

## 5. Build-tagged packages: Linux-first scope

**Method:** counted `*_<goos>.go` / `*_<goos>_<arch>.go` non-test files per package.

| Package | linux | darwin | windows | note |
|---|---|---|---|---|
| `syscall` | 72 files, ~63k lines | 15 files, ~9.5k lines | 13 files, ~6.7k lines | windows `zsyscall_windows.go` alone 1,487 lines of DLL thunks |
| `net` | 7 | 3 | 13 | windows winsock/file-poll surface |
| `os` | 6 | 3 | 10 | windows path/file semantics |
| `runtime` OS files | `os_linux.go`, `sys_linux_*.go`… | `os_darwin.go`… | `os_windows.go`, `mem_windows.go`… | 51 `os_*/sys_*` files total |
| runtime netpoll | `netpoll_epoll.go`, `netpoll_linux.go` | `netpoll_kqueue.go` | `netpoll_windows.go` (IOCP) | 11 netpoll files |

**Linux-first plan:**
1. Implement: `syscall` linux set (72 files, but mostly generated `zsyscall_linux_*.go` tables — regenerate, don't hand-port), `runtime` linux+epoll netpoller, `internal/poll` fd poller, `os/signal` linux, `net` linux socket files, `os/user` cgo-disabled path.
2. Defer: darwin (kqueue netpoll, libSystem quirks, `os` darwin specifics), windows (IOCP netpoller is a different async model — completion ports vs readiness; winsock; the 6.7k-line windows syscall layer; registry in `internal/syscall/windows/registry`). Windows is easily 2–3× the porting cost of darwin.
3. Defer entirely: `runtime/cgo` + `plugin` (cgo needs the full foreign-call ABI; `plugin` needs dynamic loading — both are poor fits for a first Truffle backend and can be stubbed as "unsupported" with clear errors).
4. `syscall/js`+`wasm`: only if the wasm target is in scope — otherwise drop.

## 6. The `//go:linkname` contract — the real coupling cost

Four distinct linkname forms exist in go1.27.1, each a different coupling direction:

1. **Stdlib → runtime** (`//go:linkname local runtime.sym`): **52 distinct runtime symbols** pulled by stdlib (list measured: `mapaccess1/2`, `mapassign` + fast variants, `mapdelete_fast*`, `memequal`, `memequal_varlen`, `cmpstring`, `nanotime`, `rand`/`randn`, `reflectcall`, `zeroVal`, `getStaticuint64s`, `entersyscall`/`exitsyscall`, `fcntl`, `write`, `divideError`/`overflowError`, `vgetrandom`, `msan*`/`asan*` sanitizer hooks, libfuzzer hooks…).
2. **Runtime → stdlib** (`//go:linkname sym pkg.path` inside `src/runtime/*.go`): runtime pulls from `internal/poll` (9 poller fns), `internal/runtime/maps`, `time` (`newTimer/stopTimer/resetTimer`…), `sync` (`runtime_Semacquire` etc.), `os` (`sigpipe`, `runtime_args`), `reflect` (~30 symbols: `mapiterinit/next`, `typedmemmove`, `typelinks`…), `weak` (`runtime_registerWeakPointer`), `unique` (`runtime_blockUntilEmptyCleanupQueue`), `arena`, `crypto/*` (boring/fips hooks).
3. **One-arg push markers** (`//go:linkname localname`, no target): 77 files outside runtime export symbols for runtime's use (e.g. `time`'s `newTimer`, `weak`'s `runtime_registerWeakPointer`, `sync`'s `poolCleanup`).
4. **`//go:linknamestd`**: `internal/runtime/atomic` → `sync/atomic` (Load/Store/Swap/CAS/Xadd/Xchg/And/Or/Loadp…).

**What this implies for a reimplemented runtime:**
1. **Symbol-name compatibility is mandatory, ABI compatibility is negotiable.** Linknames resolve by *symbol name* at link time; there is no version or signature check. A new runtime must export the same 52 names (form 1) — but "export" here means the Truffle backend must *provide a callable with that exact name and Go-callable signature*. Since graalgo compiles stdlib from source with its own frontend, the ABI (register/stack layout) can differ from gc's — **as long as every caller and callee is compiled by graalgo**. The ABI is only observable through `unsafe` and asm, which are being replaced anyway.
2. **Direction 2 is the hard constraint.** The runtime *calls into* stdlib packages (`time`, `internal/poll`, `reflect`, `weak`, `unique`, `sync`, `os`). So the new runtime cannot be written in isolation and "linked against stock stdlib": it must be co-designed with those packages, or those packages must be forked/adapted (their linkname push markers assume a gc-shaped runtime on the other side).
3. **Per-release churn is the tax.** Linkname targets are *internal* symbols — explicitly not covered by the Go 1 compatibility promise. They move between releases (e.g. `growsliceNoAlias` appeared recently; `internal/runtime/maps` was carved out of `runtime` in Go 1.24-ish). Reuse strategy must include a per-release linkname-diff step (see T2 version tracking): a mechanical check that all 52+ names still exist with compatible signatures.
4. **The pragmatic escape hatch:** fork the ~78 linkname-using packages into the graalgo tree as "adapted stdlib" (mechanical renames where linknames changed), and compile the 229 pure packages verbatim from upstream. This bounds the per-release toil to the coupled set instead of the whole tree.

## 7. Open / unverified items

1. [UNVERIFIED] Exact direction/semantics of `time`'s `runtimeNow`/`runtimeNano` linknames — read `time/time.go` against `runtime/timestub.go` before designing the timer bridge.
2. [UNVERIFIED] `errors` matched `"unsafe"` once — confirm whether it's a real import or a comment/string; the 229 count treats it as coupled (conservative).
3. [UNVERIFIED] s390x/arm/loong64/ppc64/mips/wasm asm line counts — same method as §4.1 applies; only amd64/arm64/riscv64/386 were counted.
4. [UNVERIFIED] The 70 `"runtime"`-importing dirs include `internal/trace/testdata` and `go/doc/testdata` (test fixtures, not real coupling) — the load-bearing count is slightly lower.
5. [UNVERIFIED] Whether `plugin` and `runtime/cgo` can be stubbed without breaking `go test` of the adapted stdlib — depends on which tests reference them.

## T4 key takeaways

1. **229 of 398 stdlib package dirs (58%) are pure Go by a strict 4-way check** (no unsafe, no runtime import, no linkname, no asm) and can compile unchanged once the frontend and the intrinsic surface exist — but the load-bearing packages are all in the coupled 42%.
2. **The minimal intrinsic surface is ~60–70 named runtime functions** (§2): memory ops, slice/map/chan/select, interface/itab machinery, panic/defer, scheduler, netpoller contract, atomics, timers, fastrand, write barrier, traceback — plus exact `rtype`/itab memory layouts for `reflect`'s 243 `unsafe` uses.
3. **485 asm files / 127k lines** are dominated by crypto (emulate-then-intrinsify), runtime core (evaporates — no Go stacks on Truffle), and atomics/bytealg (intrinsify); only amd64+arm64 merit full intrinsic coverage, riscv64 can start with Go fallbacks.
4. **Linux-first is well-bounded** (72 syscall files, epoll netpoller); darwin is a moderate deferral, windows is 2–3× the cost (IOCP async model + 6.7k-line syscall layer); cgo/`plugin` should be stubbed as unsupported initially.
5. **Linkname compatibility means symbol names, not ABI**: 52 symbols stdlib→runtime plus ~100 runtime→stdlib call edges; the coupling tax is per-release churn of internal symbols, payable via a linkname-diff step and a forked "adapted stdlib" limited to the ~78 coupled packages.
