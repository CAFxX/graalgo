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
