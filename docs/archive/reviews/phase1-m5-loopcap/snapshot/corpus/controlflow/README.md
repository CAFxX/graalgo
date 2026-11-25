# Control-flow corpus (M4, WS E)

Differential tests for Go control flow, bit-identical vs stock gc 1.27.1
(`ControlFlowCorpusTest`; `// verifies: GG-0014`).

## Programs

| File | Covers |
|------|--------|
| cf01_ifelse.go | if/else-if/else, init statement (`if x := 15; ...`) |
| cf02_for.go | 3-clause for (sum, product, decrementing) |
| cf03_while.go | condition-only for, for with init but no post, continue |
| cf04_infinite.go | infinite `for {}` with break (incl. nested-if break) |
| cf05_labeled.go | nested loops, labeled break/continue (`break outer`, `continue outer/inner`) |
| cf06_range_slice.go | range over slice (index+value, value-only, index-only, blank) |
| cf07_range_array_string.go | range over array (copy semantics), range over string (byte offsets + runes), non-define `=` range |
| cf08_range_map.go | range over map (key+value, key-only, value-only); order-independent reductions |
| cf09_switch.go | expression switch (init, multi-case, fallthrough, default, no-match, break-in-case), tagless switch |
| cf10_typeswitch.go | type switch (binding, named-type distinction, `case nil`, multi-type clause, default) |
| cf11_goto.go | forward goto, backward goto as loop, goto out of nested block/for |

## Conventions

- Every file is gofmt-clean, has no imports and no builtins
  (`len`/`cap`/`append`/… are WS G's; tests avoid them).
- The `// observe:` trailer lists scalar (`int`) variables whose final
  values are compared bit-identical (`long[]`) against the gc oracle.
  The oracle injects `println(vars...)`; graalgo reads frame slots.
- Range-over-map programs reduce to **order-independent** sums/counts, so
  gc's randomized iteration order can never cause a false mismatch. The
  sums prove the visited multiset, which is the specified semantics.

## Mutation story (red-green)

- **Mutant A** (control-flow): dropped the `!` in `ForRepeatingNode`'s
  condition check → loops exit immediately. Killed by 5 tests
  (`hidden_files/m4_red/mutantA_red.log`): cf02, cf03, cf05, cf11, co09.
- **Mutant B** (composite): off-by-one in `SliceExprNode` length
  (`h - l + 1`). Killed by 2 tests
  (`hidden_files/m4_red/mutantB_red.log`): co02, co03.
