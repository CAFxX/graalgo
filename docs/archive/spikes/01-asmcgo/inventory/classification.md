# asm classification inventory — load-bearing packages (linux/amd64)

Scope: exhaustive over `runtime`, `sync`, `reflect`, `net` for the tested
configuration (linux/amd64, go1.27.1). Other architectures noted where the
file is arch-specific. Non-load-bearing packages sampled at the end.

Strategies:
- **I** = intrinsify (backend intrinsic replaces the asm)
- **E** = evaporate (not needed on the JVM; no replacement)
- **M** = emulate-then-intrinsify (portable emulation first, intrinsic later)
- **J** = JVM-covers (the JVM already provides the semantic)
- **P** = Panama FFI (cgo bridge; superseded by java.lang.foreign)

Evidence levels: **D** = differential-tested in this spike; **S** = formulation
sketch (classified, not differentially tested here).

## runtime/ (linux/amd64-relevant files)

| file | symbols | strategy | formulation sketch | ev |
|---|---|---|---|---|
| asm_amd64.s | gogo, mcall, systemstack, systemstack_switch, goexit, morestack, morestack_noctxt, mstart, setg, stackcheck, gosave_systemstack_switch | I | goroutine switch → virtual-thread park/unpark; morestack → Truffle stack-overflow-check intrinsic (JVM stack grows dynamically); setg → virtual-thread-local | S |
| asm_amd64.s | gcWriteBarrier, gcWriteBarrier1-8 | J | every JVM reference store already passes through the GC's write barrier; no explicit barrier code needed | S |
| asm_amd64.s | reflectcall | I | Truffle call node with explicit argument array (cf. Java MethodHandle.asSpreader) | S |
| asm_amd64.s | asmcgocall, asmcgocall_landingpad, asmcgocall_no_g, cgocallback, _cgo_topofstack, callRet<>, spillArgs, unspillArgs | P | Panama downcall/upcall stubs (demo in panama/) | D* |
| asm_amd64.s | sigpanic0 | J | JVM converts faulting loads/stores to exceptions natively; backend maps to Go panic | S |
| asm_amd64.s | sigtramp, sigfwd, sigreturn__sigaction, sigprofNonGoWrapper<> | E/J | JVM owns signal delivery; profiling via JFR instead of SIGPROF | S |
| asm_amd64.s | cputicks | J | System.nanoTime / JFR thread CPU time | S |
| asm_amd64.s | publicationBarrier | J | VarHandle fullFence / Unsafe.storeFence | S |
| asm_amd64.s | procyieldAsm | J | Thread.onSpinWait | S |
| asm_amd64.s | retpoline* (16 variants) | E | Spectre v2 mitigation is the JIT's job, not the language runtime's | S |
| asm_amd64.s | getfp | E | no frame pointers on the JVM | S |
| asm_amd64.s | abort, breakpoint | J | intrinsic traps (GraalVM deopt/trap nodes) | S |
| asm_amd64.s | asminit, addmoduledata | E | no first-class modules on the JVM; metadata lives in JVM structures | S |
| asm_amd64.s | debugCallV2, debugCallPanicked | E | debugger function injection replaced by Truffle Chrome Inspector (per feasibility study Delve is structurally impossible) | S |
| asm_amd64.s | _rt0_amd64, main, _rt0_amd64_lib, rt0_go, rt0_lib_go | E | process entry is provided by the JVM / Native Image | S |
| asm_amd64.s | panicBounds | J | JVM bounds checks raise ArrayIndexOutOfBounds; backend maps to Go panic | S |
| asm.s | mapinitnoop, sigpanic0 | E | no-op / covered above | S |
| memmove_amd64.s | runtime·memmove | M | no-pointer case: differential-tested pure-Go formulation (harness/mem); pointer case: backend arraycopy intrinsic emitting write barriers | D† |
| memclr_amd64.s | runtime·memclrNoHeapPointers | M | differential-tested pure-Go formulation (harness/mem) | D |
| preempt_amd64.s | asyncPreempt | E | async signal preemption unnecessary: Loom parks at blocking points, Truffle inserts back-edge safepoint polls (feasibility §1 hardest problem) | S |
| rt0_linux_amd64.s | _rt0_amd64_linux, _rt0_amd64_linux_lib | E | JVM provides process entry | S |
| sys_linux_amd64.s | write1, read, open, closefd, pipe2, connect, socket | J | java.io/java.nio; netpoller → Loom async IO | S |
| sys_linux_amd64.s | sysMmap, sysMunmap, madvise, mincore, sbrk0 | J/E | Go heap lives on the Java heap (mmap evaporates); explicit mappings → Unsafe/ByteBuffer | S |
| sys_linux_amd64.s | clone, settls, gettid, tgkill, exitThread, osyield, sched_getaffinity | J | virtual threads replace clone; Thread.yield; ThreadMXBean | S |
| sys_linux_amd64.s | futex | J | LockSupport.park/unpark | S |
| sys_linux_amd64.s | nanotime1, vgetrandom1 | J | System.nanoTime; ThreadLocalRandom/SecureRandom | S |
| sys_linux_amd64.s | rt_sigaction, rtsigprocmask, sigaltstack, raise, raiseproc, setitimer, timer_create, timer_delete, timer_settime | E/J | JVM owns signals and profiling (JFR) | S |
| sys_linux_amd64.s | exit | J | System.exit / Runtime.halt | S |
| sys_linux_amd64.s | access | J | java.nio.file.Files | S |
| sys_linux_amd64.s | usleep | J | Thread.sleep | S |
| sys_linux_amd64.s | callCgoMmap, callCgoMunmap, callCgoSigaction, cgoSigtramp, getpid | P/J | cgo path → Panama; getpid → ProcessHandle | S |
| time_linux_amd64.s | time·now | J | System.currentTimeMillis (wall clock) | S |
| test_amd64.s | testSPWrite | E | test-only stack-pointer helper | S |
| valgrind_amd64.s | valgrindClientRequest | E | tooling-only annotations | S |
| ints.s | staticuint64s data | E | data, not code; backend emits constants | S |
| asan_amd64.s, msan_amd64.s | asanread/aswrite etc. | E | sanitizer builds out of scope for the drop-in toolchain's core mission | S |
| race_amd64.s | racecall* | E | race detector is a separate tool; scoped out (re-test trigger: JVM happens-before-based detector as future work) | S |
| libfuzzer_amd64.s | libfuzzer entry | E | fuzzer harness entry; not load-bearing for the toolchain | S |
| cgo/asm_amd64.s (+10 arch siblings) | _cgo_* , crosscall2, etc. | P | entire cgo ABI bridge superseded by Panama (working demo in panama/) | D* |
| secret/asm_amd64.s | loadRegisters etc. | E | test-only scaffolding (goexperiment.runtimesecret, "used for testing only") | S |

D*: Panama demo shows downcall+upcall working with bit-identical results to cgo.
D†: differential evidence covers the no-pointer case; the write-barrier case is a backend-intrinsic sketch.

Out of the tested configuration (noted, not classified): all `rt0_*` /
`sys_*` / `memmove_plan9_*` / `memclr_plan9_*` files for non-linux OSes and
non-amd64 architectures; `duff_*` (no amd64 variant exists — amd64 uses the
memmove path); `tls_*` (no amd64 variant); `zcallback_windows*`.

## sync/

| file | strategy | note |
|---|---|---|
| sync/atomic/asm.s (36 trampolines) | I | stub backend: replaced by wiring.go delegating to formulations; full stdlib test suite green |
| sync/atomic/race.s | E | race build out of scope (see above) |

No other .s files under sync/.

## reflect/

| file | symbols | strategy | sketch | ev |
|---|---|---|---|---|
| asm_amd64.s | makeFuncStub, methodValueCall | I | Truffle call-target adapters: makeFuncStub → generated target unpacking []Value; methodValueCall → bound-method dispatch via method handles. The Plan-9 frame hacking evaporates; the *calling convention* is what gets intrinsified. | S |
| asm_386.s, asm_arm.s, ... (9 siblings) | same | I | same, per-arch (not in tested config) | S |
| float32reg_{ppc64x,riscv64,s390x}.s | float helpers | I | arch-specific register shims; not in tested config | S |

## net/

Zero .s files. Nothing to classify.

## internal/ packages used by the above

| package | files | strategy | evidence |
|---|---|---|---|
| internal/runtime/atomic (13 .s, amd64) | all ops | I (emulate-then-intrinsify in the spike) | D: differential vs real asm (3 seeds × 3000 cases) + Java VarHandle stress + full sync/atomic suite on stub |
| internal/bytealg (46 .s) | Index/Compare/Count SIMD kernels | I | D: differential vs real asm (3 seeds × 3000 cases, incl. empty/huge/unaligned) + full bytes+strings suites on stub |
| internal/cpu (cpu.s, cpu_x86.s) | cpuid, xgetbv, getGOAMD64level | J | JVM/Graal already knows CPU features; sketch: query via GraalVM CPUFeature API |

## Sampled: the rest (not load-bearing per the task's list)

- `crypto/internal/fips140/*` (71 .s: AES-NI, SHA-NI, PCLMULQDQ, P-256 field): **M/I** — portable Go fallbacks already exist upstream (emulate); intrinsify to GraalVM AES/SHA stubs. Spike differential evidence for portable AES-128/SHA-256 formulations (FIPS-197 vector + 500 blocks/digests ×3 seeds, zero mismatches).
- `math/bits` (no asm): **J** — validated differentially against java.lang.Long (100,013-value corpus, empty diff).
- `runtime/memmove` with pointers: see runtime table (M, D†).

## Kill assessment

No load-bearing asm file was found with NO formulation. The two highest-risk
classifications (sketch-only, no differential evidence in this spike) are:

1. **asm_amd64.s goroutine primitives** (gogo/mcall/morestack): intrinsify to
   virtual-thread operations. Re-test trigger: spike 2 (goroutines-on-Loom)
   must demonstrate a preemptible goroutine switch with Go memory-model
   semantics; if back-edge safepoint yields cannot preserve gc semantics,
   this classification fails and the verdict flips.
2. **reflect makeFuncStub/methodValueCall**: intrinsify to Truffle call
   targets. Re-test trigger: spike 4 (reflect/unsafe) must drive
   reflect.Value.Call through the formulation.

Both are *strategy* risks (the heaviest implementation work), not *absence of
a formulation*: for each, the sketch names the concrete replacement mechanism.
Per the task's kill rule ("a vague 'looks hard' is not a kill"), neither fires
the kill trigger.
