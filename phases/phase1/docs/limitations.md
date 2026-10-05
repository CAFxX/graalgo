# Known limitations (Phase 1)

Implementation limits that are fail-fast (clean error, no silent wrong
result), with re-test triggers for the phase that owns the fix.

## Stack depth (M10 self-attack)

Guest-language recursion burns host (JVM) stack: each Go call frame costs
several JVM frames through Truffle dispatch. With the pinned `-Xss32m`
(M5 stance), measured limit is ~10k–20k Go frames; beyond that
`graalgo-run` reports `stack overflow` on stderr and exits 2 (no JVM trace).

gc grows goroutine stacks dynamically, so very deep recursion that gc
accepts is rejected here. This is an interpreter-architecture limit, not
a semantic divergence: no program produces a wrong result, it fails fast.

- Re-test trigger: Phase 4 (JIT) owns the stack strategy (explicit stack
  / segmented stacks / deeper host stack). Re-measure the depth limit
  when the execution model changes.
- Regression: `corpus/panic/pn11_stack_overflow.go` (expects clean
  `stack overflow` on stderr, exit 2 — not a JVM trace).
