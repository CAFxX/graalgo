# P3-M4 traceback corpus

Go programs exercising panic stack rendering (GG-0020), differential
vs stock gc 1.27.1.

## Programs

| Program | Covers |
|---|---|
| `tb00_simple.go` | Single-frame panic: message, `goroutine 1 [running]`, `main.main()`, file:line |
| `tb01_nested.go` | 4-deep call chain: every frame at gc's line numbers (call sites) |
| `tb02_values.go` | Panic with a formatted struct value: `panic: code=42 msg=broken` |
| `tb03_defer.go` | Deferred call runs before the panic trace; trace still complete |

## Normalization (`normalize.py`)

gc and graalgo tracebacks are normalized before comparison:

- goroutine IDs → `N`
- `main.` prefix stripped from frame names (`main.main()` → `main()`)
- argument lists → `()` (graalgo doesn't render arg words)
- `+0x…` offsets stripped
- absolute file paths → basename
- `created by …` lines dropped (graalgo doesn't record goroutine creators)

## Known divergences (documented, not silent)

- gc qualifies nested functions as `main.main.func1`; graalgo's
  closure names differ (not covered by this corpus).
- gc shows inlined frames with `main.inner(...)`; graalgo does not
  inline (not covered).
- Multi-goroutine traces are single-goroutine only in graalgo.
