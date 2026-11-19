# Known limitations (Phase 1)

Implementation limits that are fail-fast (clean error, no silent wrong
result), with re-test triggers for the phase that owns the fix.

## CPU features: none reported (`-toolexec` path)

On the `-toolexec` path (the standard library compiled from upstream's
source against the facade `runtime`, [toolexec-design.md](toolexec-design.md)
§5.4) nothing calls `internal/cpu.Initialize`, so `internal/cpu` reports no
optional CPU feature and every feature-gated path of the standard library
takes its portable branch. A program behaves as under gc with
`GODEBUG=cpu.all=off`. The only observable difference found so far is the
last bit of `math.Exp` (and what is built on it) on hosts with FMA, where
upstream's amd64 assembly has a second path; the intrinsic ports the path
without FMA. [test/m1r/run.sh](../../test/m1r/run.sh) therefore runs both
legs with `GODEBUG=cpu.all=off`.

- Re-test trigger: the parity-stance log (WP-PAR) decides whether graalgo
  reports the host's features; the intrinsic then reads `math.useFMA` as
  the assembly does.

## `sync` (`-toolexec` path)

`sync` and `internal/sync` are upstream's source on the runtime intrinsics
of [SyncIntrinsics.java](../../internal/backend/truffle/src/main/java/graalgo/go/intrinsics/SyncIntrinsics.java)
(gc's semaphore and notify-list algorithms on the scheduler). Differences:

- `testing/synctest` bubbles are not supported: no goroutine is ever in a
  bubble (`internal/synctest.IsInBubble` is false), so the bubble checks of
  `WaitGroup` never fire.
- `sync.Pool`'s pin sections serialize on one process-wide lock and use one
  per-P slot (gc: one per P, no lock); `runtime.GC` runs the pool cleanup,
  so the victim-cache behaviour across collections is gc's.
- `throw` from `sync` (a corrupted lock word, never reached by correct
  code) prints like `fatal`: the current goroutine only, where gc dumps all.
- The mutex and semaphore profiles (`runtime.SetMutexProfileFraction`,
  block profile) record nothing.

- Re-test trigger: `testing/synctest` support (M2, `testing`).

## `unsafe` on the flat value model (`-toolexec` path; SPEC §3.4, GG-0062)

Pointers never leave their referent, and every access goes through the
referent's leaf table ([ADR-0015](../dev/adr/ADR-0015-pointer-model-w13.md),
[truffle-design.md](truffle-design.md) §15). Where gc without `-d=checkptr`
runs a program that this model cannot, graalgo stops with a Go fatal error
in gc's `checkptr` wording (exit status 2) — never a wrong result:

- **A slice shares storage, so its elements must be leaves of the referent.**
  `unsafe.Slice((*byte)(unsafe.Pointer(&ints[0])), n)` — a `[]byte` over
  wider scalars — and slicing `(*[N]T)(p)[:]` when `T`'s leaves are not the
  referent's are `checkptr: unsafe.Slice result straddles multiple
  allocations` / `converted pointer straddles multiple allocations`. Scalar
  loads and stores through the same pointer (`*(*uint64)(p)`) work, and so
  does `unsafe.String` (it copies when the bytes are not byte leaves; a
  string is immutable). This is the price of one slice shape; the first std
  package that needs a byte view of words decides what replaces it.
- **gc's checkptr conditions always apply**, not only in instrumented
  builds: a converted pointer whose pointee reaches past the object, a
  misaligned pointer to a type with pointers, arithmetic that leaves the
  object (its end is allowed; a load through the end pointer is not), an
  integer that names no live object.
- **Padding has no storage**: a load reads 0, a store is dropped (gc leaves
  padding undefined — a stack variable's trailing padding holds whatever
  the stack held).
- **`uintptr(p)` is a handle**, not an address: stable for the object's
  life, ordered within an object, meaningless across objects; it does not
  keep the object alive (as in gc), and an integer whose object is gone
  converts to nothing.
- **A word addressed through a view is not lock-free**: `sync/atomic` on it
  (an `int32` operated on as `uint32`, the words of an interface header in
  `atomic.Value`) goes through the referent's monitor — atomic, but a
  monitor.
- An interface header written word by word holds its two words as they are
  until they form a value; typed code reading it in between would read
  what gc calls a corrupt interface.

- Gate: [test/m1w13/](../../test/m1w13/) (`make test-m1w13`):
  `restricted/` pins each restriction with its fatal text, `checkptr/`
  compares with gc built with `-d=checkptr`.
- Re-test trigger: W9a/W8a (descriptor images, syscall `Bytes` roles) and
  every std package admitted to `make test-m1r`.

## Stack depth (M10 self-attack)

Guest-language recursion burns host (JVM) stack: each Go call frame costs
several JVM frames through Truffle dispatch. With the pinned `-Xss32m`
(M5 stance), measured 2026-10-05: depths 1k/5k/10k/15k run clean (exit 0);
20k/30k/100k report `graalgo-run: stack overflow` on stderr, exit 2
(the original run log was research scratch and was not kept; the regression test below re-checks the 100k case). No JVM trace leaks.

gc grows goroutine stacks dynamically, so very deep recursion that gc
accepts is rejected here. This is an interpreter-architecture limit, not
a semantic divergence: no program produces a wrong result, it fails fast.

- Re-test trigger: Phase 4 (JIT) owns the stack strategy (explicit stack
  / segmented stacks / deeper host stack). Re-measure the depth limit
  when the execution model changes.
- Regression (product launcher, P3SyncMain):
  [StackDepthLimitTest](../../internal/backend/interop/src/test/java/graalgo/p3sync/launcher/StackDepthLimitTest.java)
  (`make test-java-fast`) — runs a 100k-deep recursion and asserts a
  non-zero exit, a `stack overflow` stderr line, no stdout, and no JVM
  stack trace. Measured 2026-11-18 on the product launcher: exit 1,
  `graalgo-run: fatal stack overflow in goroutine: java.lang.StackOverflowError: ...`
  (gc's own overflow is `fatal error: stack overflow`, exit 2 — a
  remaining message/exit-code divergence). The v1 graalgo-run check
  (`MainTest.testDeepRecursionReportsCleanStackOverflow`, exit 2) is
  quarantined with the rest of MainTest until M3.
