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

## Unimplemented stubs reachable only through interface call arguments or reflect (P1-3)

Since P1-3, an unimplemented stub (a bodyless method, a multi-result
declaration under gc's default symbol, or a multi-result `//go:linkname`
declaration with no compiled provider) that is referenced from reachable
code fails the export — the equivalent of gc's link error for a missing
implementation — instead of building cleanly and panicking at runtime.
Direct references (calls, method calls, function and method values,
package initializers, `init` functions) and interface boxings (explicit
conversions, assignments and declarations into interface-typed variables,
interface-typed globals, and interface-typed returns) are all covered.

Two shapes are NOT covered by the reachability gate, and still panic at
runtime if the stub is actually invoked (with its accurate reason, never
the old assembly blame):

- A stub method reached only by passing a concrete value as an argument
  to an interface-typed parameter, and then dispatched through that
  interface inside the callee. The wire format does not mark that
  implicit conversion, and resolving callee signatures (including
  methodref receiver offsets and variadic packing) is not reliable
  enough to fail builds on.
- A stub reached only via reflect (`MethodByName` / `Value.Call`),
  which carries method names, not object ids.

A blank-identifier interface assignment (`var _ I = T{}`) is deliberately
not flagged: the exporter elides the conversion entirely, so no itab is
built and the stub is genuinely unreachable in the emitted program.

- Re-test trigger: extend the gate's boxing analysis to call arguments
  when a reliable callee-signature resolution exists on the wire format.
- Regression: `TestUnimplementedReachableFails`,
  `TestUnimplementedUnreferencedBuilds`,
  `TestUnimplementedInterfaceConversionFails`
  ([internal/frontend/stub_reachability_test.go](../../internal/frontend/stub_reachability_test.go)).
