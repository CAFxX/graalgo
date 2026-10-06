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

## Re-test triggers (P3-M4 remediation, methodology P2-4)

Each known traceback divergence below gets an explicit re-test
trigger — the condition under which the divergence must be
re-examined (the corpus + `normalize.py` are the re-test vehicle in
every case):

1. **No `+0x` pc offsets.** graalgo frames carry no machine code, so
   there is no pc to print. Trigger: if a future backend records a
   per-frame pc/offset, render it and drop the `+0x` strip rule from
   `normalize.py`.
2. **No argument words.** graalgo frames carry no arg words; gc prints
   `(...)`/`(0x...)`. Trigger: if frames ever record arguments, render
   them and drop the arg-list normalization.
3. **No inlined frames.** graalgo does not inline; gc prints inlined
   frames. Trigger: if the Truffle tier ever inlines Go calls,
   re-derive the expected traces with inlined frames (the current
   `.expected` files would fail loudly — that failure IS the trigger).
4. **No `created by` line.** The scheduler does not record goroutine
   creators. Trigger: when it does, render the line and drop the
   `created by` drop rule from `normalize.py`.
5. **Single-goroutine only.** Only the panicking goroutine renders.
   Trigger: when multi-goroutine dumps exist, extend the corpus with a
   two-goroutine panic program and compare the full dump.

(Absence of a trigger firing is not evidence: the `.expected` files
and `normalize.py` rules encode the current divergences, so any
behavior change fails the corpus loudly.)
