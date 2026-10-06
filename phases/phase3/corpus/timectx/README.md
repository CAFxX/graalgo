# P3-M5 `time`/`context` corpus

Go programs exercising the P3-M5 synthetic `time` and `context` packages
(ADR-0005), differential vs stock gc 1.27.1.

## Programs

| Program | Covers |
|---|---|
| `tc00_now_unix.go` | `time.Now`, `Unix`, `UnixNano`, `IsZero`, ordering |
| `tc01_duration_arith.go` | `Duration` arithmetic, `String`, `Round`, `Truncate`, `Abs` |
| `tc02_sleep.go` | `time.Sleep` |
| `tc03_after.go` | `time.After` |
| `tc04_timer_stop_reset.go` | `Timer.Stop`/`Reset` booleans |
| `tc05_ticker.go` | `Ticker` 3 ticks then `Stop` |
| `tc06_afterfunc.go` | `time.AfterFunc` |
| `tc07_timer_chan_len_cap.go` | DIV-M3-1 regression: `len`/`cap` of timer channels are 0 |
| `tc08_context_cancel.go` | `WithCancel`, `Done`, `Err` == `Canceled` |
| `tc09_context_timeout.go` | `WithTimeout`, `Err` == `DeadlineExceeded` |
| `tc10_context_deadline.go` | `WithDeadline` future/past |
| `tc11_context_value.go` | `WithValue` lookup, missing-key nil, shadowing |
| `tc12_context_parent.go` | parent cancel propagates to child/grandchild |
| `tc13_context_afterfunc.go` | `context.AfterFunc` on cancel |
| `tc14_context_cause.go` | `WithCancelCause`, `Cause` |
| `tc15_sleep_zero_negative.go` | `Sleep(0)`, `Sleep(negative)` |
| `tc16_timer_reset_expired.go` | `Reset` on expired timer |

Timing-sensitive programs print fixed strings only, never measured
durations. `run_differential.sh` follows the iface/sync harness pattern.
