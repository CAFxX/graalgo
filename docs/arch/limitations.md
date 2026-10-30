# Known limitations (Phase 1)

Implementation limits that are fail-fast (clean error, no silent wrong
result), with re-test triggers for the phase that owns the fix.

## Stack depth (M10 self-attack)

Guest-language recursion burns host (JVM) stack: each Go call frame costs
several JVM frames through Truffle dispatch. With the pinned `-Xss32m`
(M5 stance), measured 2026-10-05: depths 1k/5k/10k/15k run clean (exit 0);
20k/30k/100k report `graalgo-run: stack overflow` on stderr, exit 2
(log: `hidden_files/m10_selfattack/DEPTH_LIMIT.log`). No JVM trace leaks.

gc grows goroutine stacks dynamically, so very deep recursion that gc
accepts is rejected here. This is an interpreter-architecture limit, not
a semantic divergence: no program produces a wrong result, it fails fast.

- Re-test trigger: Phase 4 (JIT) owns the stack strategy (explicit stack
  / segmented stacks / deeper host stack). Re-measure the depth limit
  when the execution model changes.
- Regression: `MainTest.testDeepRecursionReportsCleanStackOverflow`
  (`src/test/java/graalgo/launcher/MainTest.java`) — generates a 100k-deep
  recursion program in a temp dir, asserts exit 2 and a clean
  `stack overflow` stderr line (no JVM trace).

## synctest bubbles (codebase review 2026-10-30, finding R1-8)

`internal/synctest` bubbles are not implemented. Any program using
`internal/synctest` directly, or `testing/synctest` (which wraps
it), cannot run: `synctest.Run` and `synctest.inBubble` raise a Go
panic ("synctest.Run: not implemented by graalgo" /
"synctest.inBubble: not implemented by graalgo") instead of running
the function, because running it without a bubble would silently
drop the fake-time and deadlock-detection semantics. This is
fail-fast, not silent: no program gets a wrong result, it fails
loud. The no-bubble answers around the panics are gc-faithful
outside a bubble: `IsInBubble` is false, `associate` returns
`Unbubbled` (0), `isAssociated` is false, `acquire` yields nil,
and `Wait` outside a bubble panics with gc's exact message
("goroutine is not in a bubble").

One deliberate divergence, valid only while bubbles do not exist:
`release` of the nil `acquire` yields is a no-op, where gc's
`synctest_release` type-asserts and panics on nil. It is reachable
only because `acquire` can never return a real bubble today.

- Re-test trigger: the design in
  [synctest-bubble-design.md](synctest-bubble-design.md) is
  implemented (fake clock, durable-block classification, bubble
  driver, association enforcement — in that dependency order, not
  a shortcut that runs the function unbubbled) and the differential
  synctest corpus in that design's §5 is green against gc 1.27.1.
  When that lands, this section is removed in the same change.
- Regression: `SynctestContractTest`
  (`internal/backend/interop/src/test/java/graalgo/p3link/SynctestContractTest.java`)
  — pins the panic messages and the no-bubble answers, and is the
  tripwire for the real implementation: its `Run`/`inBubble`
  panic tests must fail once bubbles genuinely exist, at which
  point the class is replaced by bubble semantics tests.
