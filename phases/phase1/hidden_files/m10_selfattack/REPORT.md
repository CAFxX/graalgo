# M10 self-attack report (2026-10-05)

Adversarial programs run against `graalgo-run`, each compared to stock gc.
Any crash/hang/divergence = P0. Programs in this directory (`atk*.go`).

## Attacks and outcomes

| # | Attack | gc | graalgo | Verdict |
|---|--------|----|---------|---------|
| 1 | Deep recursion (100k frames) | ok (100000, 75025) | **CRASH**: `NoClassDefFoundError` cascade from Truffle profiler (root: `StackOverflowError`) | **P0 → FIXED** |
| 2 | Defer storm (10k defers in loop + deferred closures) | ok | byte-identical | PASS |
| 3 | Nested generic structs (`Box[Box[Box[Box[int]]]]`) | ok | **CRASH**: `StackOverflowError` in frontend | **P0 → FIXED** |
| 4 | Hostile fmt (`%p %T %q`, precisions, slicing, map order) | ok | identical except `%p` → `0x0` (documented address erasure) | PASS |
| 5 | Defer/panic interplay (panic in defer, recover) | ok | byte-identical | PASS |
| 6 | Adversarial typeparams (constraints, multi-param, Pair swap) | ok | byte-identical | PASS |

| 3b | (first draft of atk03 had an instantiation cycle — gc correctly
|    | rejected it; not a graalgo bug; rewritten to the valid finite nesting) | — | — | n/a — attack-authoring error |

## P0-1: exporter named-type dedup dropped nested type args

- **Symptom**: 4-deep `Box[Box[Box[Box[int]]]]` → `StackOverflowError` in
  `NodeFactory.structZeroTemplate` (510 recursion levels for 4 type levels).
- **Root cause**: `frontend/export.go:qualifiedNameForType` rendered
  `*types.Named` type arguments as just the qualified name, dropping their
  own type arguments. `Box[Box[int]]`, `Box[Box[Box[int]]]`,
  `Box[Box[Box[Box[int]]]]` all collapsed to dedup key `"main.Box[main.Box]"`.
  The winner's underlying struct referenced the winner itself → cyclic type
  graph → infinite recursion. Second symptom: `%T` printed the wrong
  (truncated) name for nested instantiations.
- **Fix** (commit `ea4f6e9`): render named type args recursively via
  `namedTypeName`. Terminates: type arguments form a finite tree.
- **Class hunt**: `qualifiedNameForType` has one caller (`namedTypeName`);
  other dedup maps (`typeIDs`, `substTypeIDs`) key on object identity —
  not affected. `writeSubstString` (subst keys) already renders args.
- **Regression**: `corpus/generics/gn11_nested_struct.go` + 
  `GenericsCorpusTest.testGn11NestedStruct` — verified FAIL (StackOverflowError)
  on pre-fix exporter, PASS post-fix. `%T` now matches gc.

## P0-2: deep recursion crashed with JVM cascade

- **Symptom**: 100k-deep Go recursion → `NoClassDefFoundError: Could not
  initialize class ...OptimizedCallTarget$ReturnProfile` (Truffle's
  exception profiler blowing the exhausted stack while handling the real
  `StackOverflowError`).
- **Measured limit**: ~10k–20k Go frames at `-Xss32m` (M5 stance).
- **Fix** (commit `51e281e`): catch `StackOverflowError` at the launcher top
  level; unwrap the profiler cascade (`NoClassDefFoundError` /
  `ExceptionInInitializerError` caused by `StackOverflowError`). Report
  `graalgo-run: stack overflow: ...` on stderr, exit 2. Anything else still
  rethrows.
- **Documented**: `docs/limitations.md` (fail-fast limit, Phase-4 re-test
  trigger).
- **Regression**: `MainTest.testDeepRecursionReportsCleanStackOverflow` —
  asserts exit 2, stderr contains "stack overflow", no `StackOverflowError`
  / `\tat ` JVM trace leakage.

## Not bugs (documented behavior)

- `%p` prints `0x0`: address erasure for determinism (gc addresses are
  non-deterministic anyway). Covered by `FmtVerbMatrixTest.testAddressErasure`.
- JVM `WARNING: ... sun.misc.Unsafe ...` lines on stderr: HotSpot/Truffle
  terminal-deprecation notices; the test harness normalizes them (M7).
