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

## Method values of the runtime stack-inspection methods (review-2026-10-31 P1-3)

Direct calls work for real: `fn.Name()`, `fn.Entry()`,
`fn.FileLine(pc)`, `frames.Next()` lower to the `graalgo.rtstack`
intrinsic wire nodes, backed by the PC-to-function table (PC interning
in `StackTrace`, resolved via `GoFuncInfo.forPc`). But the synthetic
`runtime` overlay declares no funcdecl for these methods, so there is
no methodref/methodvalue lowering: using one as a method value
(`f := fn.Name`, `take(fn.Name)`, `f := frames.Next`) fails at
frontend compile time with a named scope error ("has no
methodref/methodvalue lowering ... in frontend/v1 scope (WS D)").
That is deliberate and honest — before this fix, `fn.Name` as a value
crashed the exporter (nil method declaration), and the old code path
would have emitted a runtime panic for a program that had built
cleanly. A funclit desugar is not a fix: the receiver must be
evaluated once at value-creation time, and expression position has no
statement slot to hoist it into.

- Re-test trigger: a backend design for bound-intrinsic function
  values (a new wire node carrying the intrinsic plus the bound
  receiver, or an expression-position hoist mechanism). Until then,
  the workaround is a direct call (`fn.Name()`), not a method value.
- Regression: `TestRuntimeFuncMethodValueFailFast`
  (`internal/frontend/runtime_export_test.go`) — pins the
  compile-time error for every stack-inspection method value and the
  still-working direct call.

## Bodyless multi-result assembly stubs: panic-body `__`-helpers (mr_disk audit, review-2026-10-31 P1-3)

Audited, not changed. When a disk-imported multi-result function has
no Go body and no `//go:linkname` directive (typically a `//go:noescape`
assembly stub graalgo cannot implement — the assembly gap, same class
as the P2-6 entry), its generated `__`-helper gets a single-statement
panic body ("`__<helper> not implemented: no Go body (backend
intrinsic)`"), and the original declaration is replaced by a stub
that panics "unreachable". A program that calls such a function
therefore builds and then panics at runtime, where gc would fail at
link time. This was left as-is on purpose: the call-site rewrites
that would need to fail instead are error-less pre-type-check AST
transforms, so a call-site scope error needs new error plumbing
through that transform, and it must not disturb the linkname-bound
helpers (which are real backend intrinsics, not stubs). The failure
is loud, never a wrong result.

- Re-test trigger: assembly support, or linkname/intrinsic coverage
  for the affected symbols — at which point the panic-body helper
  for a covered symbol is replaced by the linkname-bound helper and
  this entry shrinks to the still-uncovered set.
