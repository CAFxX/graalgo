# P2-M3 timer corpus

Differential corpus for timers/sleep (WS C; GG-SCHED-0004, GG-0018).

Each `tm*.go` is a Go program using the `time` package, run under stock
gc to produce `tm*.expected`. The Java translations in
`TimerDifferentialTest` are mechanical (see its translation convention)
and must produce byte-identical output.

Only event ORDER is significant — never wall-clock values. Sleep
margins are generous (≥50ms separations) so scheduling jitter cannot
reorder events. If a program flakes, widen the sleeps.

These .go files double as the future end-to-end corpus: once the
exporter lowers the `time` package (parent dependency), the same
programs run through `graalgo-run` and must produce identical output.

Programs:
- tm01: sleep completion order (3 goroutines)
- tm02: select with time.After → timeout branch
- tm03: <-time.After fires
- tm04: NewTimer + Stop before fire (no fire)
- tm05: Stop after fire+drain → false, value drained
- tm06: Reset on active timer → true, fires at new deadline
- tm07: Reset on expired+drained → false, fires at new deadline
- tm08: Reset on expired+undrained → true (Go 1.23+), old value dropped
- tm09: NewTicker, 3 ticks, Stop
- tm10: slow ticker receiver → ticks dropped
- tm11: select+After, value branch then timeout branch
- tm12: Sleep(0)/Sleep(-1) return immediately
