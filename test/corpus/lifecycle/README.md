# Lifecycle corpus (T0.A)

Go programs pinning graalgo's process lifecycle and panic model against
stock gc (go1.27.1), per [docs/arch/RECOVERY.md](../../../docs/arch/RECOVERY.md)
lane T0.A and SPEC §3.5 ([docs/arch/SPEC.md](../../../docs/arch/SPEC.md)).

| Programs | Covers |
|---|---|
| `lc00` | main returning exits 0 while other goroutines block or spin |
| `lc01` | an unrecovered panic in a non-main goroutine is fatal at once (exit 2) |
| `lc02`–`lc06` | deadlock detection: `<-c`, `wg.Wait()`, double `mu.Lock()`, `select {}`, every goroutine blocked (gc wait reasons in the dump headers) |
| `lc07`, `lc08` | no false deadlock while a timer/AfterFunc/ticker/sleep is pending; deadlock once the last timer retires |
| `lc09`–`lc11` | `runtime.Goexit` on main ("no goroutines"), panics around Goexit |
| `lc12` | recovered runtime errors implement `error` and `runtime.Error`; `Error()` text and `%T` match gc; recovered `panic(nil)` is `*runtime.PanicNilError` |
| `lc13`–`lc19` | crash header: `Error()`/`String()` values, `[recovered]` and `[recovered, repanicked]` chains, nested panics, named-string values |
| `lc20` | `runtime.GOMAXPROCS` honors the environment and returns the previous value |
| `lc21` | `os.Exit` with live goroutines and pending defers |
| `lc22` | a goroutine blocked reading a pipe releases its scheduler slot (GOMAXPROCS=1) |
| `lc23` | an expression switch evaluates its tag once, also against interface-typed cases |
| `lc24` | a typed nil pointer in an interface selects its type's case in a type switch, not `case nil` |
| `lc25` | a method call on a nil interface evaluates its arguments before panicking |
| `lc26` | a backward `goto` loop is preempted like any loop (GOMAXPROCS=1) |
| `lc27`–`lc30` | the panic value's `Error()`/`String()` panics (gc's `panic while printing panic value` throw), sleeps, or needs other goroutines while the report renders (world still running) |
| `lc31` | main returns while a goroutine's panic report is still rendering: exit 0, no partial report |
| `lc32`–`lc34` | finalizers run on a goroutine: a queued finalizer is a waker (no false deadlock) and may block; a panicking finalizer is fatal; a live finalizable object does not mask a deadlock |

## Files and comparison

- [regen.sh](regen.sh) records `<prog>.expected` from gc: stdout, the
  stderr normalized by [normalize.py](../../lib/normalize.py), and the exit code.
- The import-free programs run on the new path in `make test-m1a` (gc vs graalgo,
  `GOMAXPROCS=1`, stdin at `/dev/null`); the v1 runner that compared them
  with `.expected` was deleted (m3-plan D0).

The normalization keeps what the lifecycle owns — fatal/panic message
lines, the panic chain, and `goroutine N [reason]:` headers (also in the
`gp=… m=…` form a gc `fatal error` throw prints) — and drops frame lines,
`created by` trailers, and the gc runtime's own goroutines (`created by
runtime.*`) that a throw dumps: stack-trace fidelity (frame names,
paths, inlining, `+0x` offsets) is the [traceback corpus](../traceback/)'s
subject and a known divergence there.
