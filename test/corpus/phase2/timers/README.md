# P2-M3 timer corpus

Differential corpus for timers/sleep (WS C; GG-SCHED-0004, GG-0018).

Each `tm*.go` is a Go program using the `time` package, run under stock
gc to produce `tm*.expected`. The Java translations in
`TimerDifferentialTest` are mechanical (see its translation convention)
and must produce byte-identical output.

Only event ORDER is significant — never wall-clock values — EXCEPT tm16
(RV-P1-22 carve-out, 2026-10-17): tm16 is the first value-sensitive case,
asserting the `time.Time` VALUE delivered on timer channels is the fire
instant. Margins follow the P1-2 rule (≥300ms separations; 50ms tolerance,
sat inside the measured fire-lateness noise band). If a program flakes,
widen the sleeps.

These .go files double as the future end-to-end corpus: once the
exporter lowers the `time` package (parent dependency), the same
programs run through `graalgo-run` and must produce identical output.

**Resolved divergence (P3-M2a; was P2-1/DIV-M3-1):** gc presents
timer channels as unbuffered (`len(t.C)` and `cap(t.C)` always 0); the
implementation now uses the `TimerChan` flavor of `GoChan` (real 1-slot
buffer, observation probes masked to 0). tm15 is the differential proof.

Programs:
- tm01: sleep completion order (3 goroutines; 300/600/900ms)
- tm02: select with time.After → timeout branch
- tm03: <-time.After fires
- tm04: NewTimer + Stop before fire (no fire)
- tm05: Stop on expired-undrained → true (Go 1.23+), pending value drained
- tm06: Reset on active timer → true, fires at new deadline
- tm07: Reset on expired+drained → false, fires at new deadline
- tm08: Reset on expired+undrained → true (Go 1.23+), old value dropped
- tm09: NewTicker, 3 ticks, Stop
- tm10: slow ticker receiver → ticks dropped
- tm11: select+After, value branch then timeout branch
- tm12: Sleep(0)/Sleep(-1) return immediately
- tm13: Ticker Stop with buffered tick → drained, 0 remaining (Go 1.23+)
- tm14: Ticker Reset with buffered tick → stale drained, fresh tick arrives
- tm15: len/cap of timer/ticker channels are 0 in every state (DIV-M3-1)
- tm16: fire-value is the fire instant (RV-P1-22): `<-time.After(300ms)` lands
  in `[before+300ms-50ms, after+50ms]`; 3 ticker ticks non-decreasing
