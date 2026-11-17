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

## Files and comparison

- [regen.sh](regen.sh) records `<prog>.expected` from gc: stdout, the
  stderr normalized by [normalize.py](normalize.py), and the exit code.
- [run_differential.sh](run_differential.sh) runs each program under
  `internal/backend/graalgo-sync-run` (override with `GRAALGO_SYNC_RUN`)
  with `GOMAXPROCS=1` and stdin at `/dev/null`, one JVM at a time, and
  compares against `.expected`. A hang fails via the per-program timeout
  (`DH_TIMEOUT`, default 120 s).

The normalization keeps what the lifecycle owns — fatal/panic message
lines, the panic chain, and `goroutine N [reason]:` headers — and drops
frame lines and `created by` trailers: stack-trace fidelity (frame names,
paths, inlining, `+0x` offsets) is the [traceback corpus](../traceback/)'s
subject and a known divergence there.
