# P2-M7 pinning audit report

- Programs run: 186
- Skipped (non-zero exit, pin count still collected): 39
  - ../phase1/corpus/builtins/bi11_panic.go (exit 2)
  - ../phase1/corpus/panic/pn04_panic_string.go (exit 2)
  - ../phase1/corpus/panic/pn10_goexit.go (exit 2)
  - ../phase1/corpus/panic/pn11_goexit_not_recoverable.go (exit 2)
  - ../phase1/corpus/panic/pn12_deep_recursion.go (exit 1)
  - ../phase1/corpus/panic/pn14_oob_index.go (exit 2)
  - ../phase1/corpus/panic/pn15_slice_bounds.go (exit 2)
  - ../phase1/corpus/panic/pn16_nil_map_write.go (exit 2)
  - ../phase1/corpus/panic/pn17_type_assert.go (exit 2)
  - ../phase1/corpus/panic/pn18_nil_deref.go (exit 2)
  - ../phase1/corpus/panic/pn19_adv11_goexit_nested.go (exit 2)
  - ../phase1/corpus/panic/pn19_div_zero.go (exit 2)
  - corpus/channels/ch01_unbuffered.go (exit 2)
  - corpus/channels/ch02_buffered.go (exit 2)
  - corpus/channels/ch03_close_drain.go (exit 2)
  - corpus/channels/ch04_producer_consumer.go (exit 2)
  - corpus/channels/ch05_select_recv.go (exit 2)
  - corpus/channels/ch06_select_default.go (exit 2)
  - corpus/channels/ch07_select_send.go (exit 2)
  - corpus/channels/ch08_close_while_blocked.go (exit 2)
  - corpus/channels/ch09_send_on_closed.go (exit 2)
  - corpus/channels/ch10_close_of_closed.go (exit 2)
  - corpus/channels/ch11_select_poison_default.go (exit 2)
  - corpus/channels/ch12_select_poison_ready.go (exit 2)
  - corpus/godebug/db05_panicnil_uncaught.go (exit 2)
  - corpus/timers/tm01_sleep_order.go (exit 2)
  - corpus/timers/tm02_after_timeout.go (exit 2)
  - corpus/timers/tm03_after_value.go (exit 2)
  - corpus/timers/tm04_timer_stop.go (exit 2)
  - corpus/timers/tm05_timer_stop_after_fire.go (exit 2)
  - corpus/timers/tm06_timer_reset_active.go (exit 2)
  - corpus/timers/tm07_timer_reset_expired.go (exit 2)
  - corpus/timers/tm08_timer_reset_undrained.go (exit 2)
  - corpus/timers/tm09_ticker.go (exit 2)
  - corpus/timers/tm10_ticker_drop.go (exit 2)
  - corpus/timers/tm11_select_after_both.go (exit 2)
  - corpus/timers/tm12_sleep_zero.go (exit 2)
  - corpus/timers/tm13_ticker_stop_buffered.go (exit 2)
  - corpus/timers/tm14_ticker_reset_buffered.go (exit 2)
- Pin events total: 0 (expected 0, UNEXPECTED 0)

## Classification

Expected = matches an allowlist entry (budgeted off-carrier FFM/file-I/O per GG-0057). The default allowlist is empty: graalgo has no JNI/FFM on goroutine paths.

No `jdk.VirtualThreadPinned` events on the conformance corpus.
