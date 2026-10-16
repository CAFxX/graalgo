# FE-1 design: defer-correct multi-result disk transform

Fixes review finding FE-1 (P0, silent miscompilation): the disk
multi-result transform in [internal/frontend/mr_disk.go](../../internal/frontend/mr_disk.go)
broke `defer` functions that modify named results.

## 1. gc semantics (the contract)

For `func f() (x T, err error)`:

1. return operands are evaluated,
2. assigned to the result variables,
3. deferred functions run (LIFO) — they observe and may modify the results,
4. the caller receives the final values.

A deferred `func() { err = errors.New("boom") }()` changes what the
caller receives. The `defer`-sets-`err` idiom is pervasive in real stdlib
code — exactly what the disk transform compiles.

## 2. The bug

Every `return` in a transformed function was rewritten to
`return __F_pair{...}`, constructing the pair **at the return statement**,
before deferred functions run. Deferred modifications to the named-result
locals were silently discarded. Verified: gc prints `42 43`; the old
transform's output prints `1 <nil>`. Affected: bare `return`, explicit
`return a, b`, and `return f()` forwarding.

## 3. Design: conditional closure wrapper + hygienic result locals

### 3a. Wrap only when a defer is present (structural trigger)

A function needs the wrapper iff its body contains a `defer` statement at
any depth, **not** descending into nested function literals (a defer inside
a literal belongs to the literal). No-defer functions keep the existing
emitted shape byte-for-byte — minimal churn on the large no-defer corpus,
and the trigger is structural (presence of `defer`), never name-keyed.

### 3b. Wrap shape

```
// before                                  // after (wrap mode)
func __F(args) __F_pair {                   func __F(args) __F_pair {
  var x T                                     var __mr_result0 T
  ...                                           ...
  defer func() { x = 42 }()                    func() {
  return 1, nil                                  defer func() { __mr_result0 = 42 }()
}                                               __mr_result0 = 1
                                                return
                                              }()
                                              return __F_pair{__mr_result0}
                                            }
```

The body becomes an immediately-invoked function literal (IIFE):

- Named-result locals stay in `__F`'s scope, captured by reference.
- `defer` statements register on the **closure**, so they run when the
  closure returns — strictly before the single pair construction.
- Returns become assignments to the result locals + bare `return` of the
  closure: `return` → `return`; `return a, b` → `r1, r2 = a, b; return`;
  `return F(args)` → `__mr_tmp := __F(args); r1, r2 = __mr_tmp.f1,
  __mr_tmp.f2; return`. Blank results use `_` as the target and
  `*new(T)` in the final pair (zero value, as today).
- Exactly one pair construction exists, as the last statement of `__F`.

### 3c. Hygienic result locals (shadowing)

Both the old bare-return path (`return __F_pair{x}`) and the new
assignment path (`x = 1`) resolve result names lexically. An inner
`x := 5` shadowing a result would misdirect the read/write (gc always
means the result variable at a `return`). Fix, applied in **both** modes:
a scope-aware rename pass over the body — reusing `mrUseWalker`'s exact
name-resolution rule ("resolves to the function scope") — rewrites every
reference denoting a result variable to a fresh `__mr_result<i>` local
(collision-checked against all bound names). Type positions are excluded:
in valid input a type-position identifier never denotes a result variable.

Side benefit: the rename also fixes callee misresolution for
`return x()` where `x` is a result holding a func value (post-rename the
callee no longer collides with a package-level `x`).

### 3d. Interactions considered

- **panic**: propagates through the IIFE; the pair is never built.
  Matches gc.
- **recover in a deferred func**: unwinding stops at the closure
  boundary; the IIFE returns normally; the pair is built from the
  (possibly modified) locals. Matches gc.
- **`go` statements**: a goroutine writing a result after the closure
  returns races the pair construction. The original already races the
  return — no defined behavior exists to preserve; accepted and
  documented.
- **`runtime.Goexit`**: defers run, goroutine terminates, no pair.
  Matches gc.
- **methods / generics**: the receiver is a plain parameter, captured by
  the IIFE; the IIFE itself needs no type parameters. `goto`/labels keep
  working (identical block structure inside the IIFE).
- **nested function literals**: return rewriting never descended into
  them and still doesn't; their defers are their own.
- **fail-fast stubs / static Appendix-M entries / interface specs**:
  untouched — the wrapper applies only to generated `__`-helpers.

### 3e. Alternatives rejected

- **Uniform wrapper for every function**: same correctness, but rewrites
  the emitted shape of the entire corpus for no benefit.
- **Inlining deferred calls at each return**: wrong on panic paths —
  defers must also run during unwinding, which only real `defer`
  (inside the closure) provides.
- **Building the pair in a deferred closure**: the helper's return value
  is fixed before deferred functions run; a deferred closure cannot
  change it.

## 4. Tests (load-bearing, FE-58)

Structural tests in `internal/frontend/mr_disk_deferwrap_test.go`
assert the **emitted shape**, not just "type-checks":

- defer-modifies-result: helper ends with a single pair-return as its
  last statement; every return inside the IIFE is bare and preceded by
  an assignment to the result locals; no pair construction inside the
  IIFE. Variants: bare return, explicit values, `return f()` forwarding.
- no-defer control: old shape preserved (pair-returns directly in the
  helper body).
- shadowing: inner `:=` shadowing a result name does not capture the
  result references (bare return reads the renamed local).
- All new tests fail on the pre-fix transform (verified on a pristine
  base worktree).

Differential probe: original vs transformed source compiled with gc must
print identical output (defer-modify, bare return, forwarding,
panic/recover, shadowing cases), plus no drift on the existing
`snapshot-check` corpus for the affected area.
