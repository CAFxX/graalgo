# Known limitations (Phase 1)

Implementation limits that are fail-fast (clean error, no silent wrong
result), with re-test triggers for the phase that owns the fix.

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
