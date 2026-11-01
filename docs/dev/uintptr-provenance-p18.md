# Retiring the `uintptr` → `unsafe.Pointer` trap (review P1-8)

Status: design + first extension landed in this lane (branch
`wt/main/rem-review-20261031-p1-8`, awaiting independent review).
The trap itself (`UnsafeOps.fromUintTrap`, `unsafefromuint` wire node)
is NOT removed: it still fires for every shape this document lists as
trapped. Nothing in this lane returns a bogus pointer to silence it.

## 1. Why the trap exists, in one paragraph

In the D5 handle model an `unsafe.Pointer` is a `long` handle minted by
`UnsafeTable` interning, and `unsafe.Pointer → uintptr` yields that
handle as an integer identity token (PKG-007). Handles are dense
sequential integers, so a bare integer cannot be mapped back to a
referent: integer 7 might be the handle of some live object, or it
might be a length, a hash, or an address computed by the program — the
value model cannot tell. Materializing a handle from an integer by
numeric lookup would silently alias whatever referent happens to hold
that number. The trap is therefore honest, and any retirement must
carry provenance (which live pointer this integer came from), never
guess it from the number.

## 2. What the r39 prover already covered

`internal/frontend/unsafe_derive.go` (`exportUintptrToPointer`) lowers
`unsafe.Pointer(E)` to the `unsafederive` wire node when `E` is an
additive tree with exactly one pointer-derived term plus pure-integer
terms. The backend (`UnsafeDeriveNode` + `UnsafeTable.derive`) derives
from the LIVE base handle by the byte offset — the summed integer is
never materialized. Covered base terms: `uintptr(P)` for an
`unsafe.Pointer`-typed `P` (any expression form, since only `P`'s type
is checked), and `reflect.Value.Pointer()` (via the `UnsafePointer`
twin call). `unsafe.Add` derives the same way. Everything else traps.

Note on legality, verified against gc while scoping this lane: a
`*T` value cannot be converted to `uintptr` directly — gc rejects
`uintptr(&x)` ("cannot convert"). The only legal routes from a pointer
to a `uintptr` are via `unsafe.Pointer` or via `Value.Pointer()`
(plus `SliceData`/`StringData`, which return `*T`, not `uintptr`).
The prover's restriction to `unsafe.Pointer`-typed bases therefore
matches the language, not just the implementation.

## 3. This lane's extension: single-assignment stored uintptrs (V1)

### 3.1 The shape

```go
p := unsafe.Pointer(&arr[0])
u := uintptr(p)              // or: var u uintptr = uintptr(p)
q := unsafe.Pointer(u + 16)  // lowers to unsafederive{base: p, offset: 16}
```

plus chain copies (`v := u`, where `u` is proven — `v` inherits `u`'s
base). No backend change: the derive node, the `ByteRef` view, and the
typed consumption sites (`UnsafeToPtrNode`, element-size validation,
bounds/alignment traps) are reused unchanged.

### 3.2 The proof (why re-evaluating the base at the conversion site is sound)

`computeStoredUintptr` (same file) runs once per function body
(installed around `exportBlock` by `exportFuncDecl`,
`exportInstantiation`, and `exportFuncLit`, saved/restored exactly like
`currentMR`, so a nested literal sees only its own definitions) and
keeps a definition only if ALL of these hold:

1. The definition is a top-level statement of the function body:
   single-LHS `:=` or single-name `var` spec with a single value, of
   `uintptr` type, whose RHS is `uintptr(P)` with `P` a bare
   `unsafe.Pointer` identifier (or a bare-identifier chain copy of an
   earlier proven definition).
2. The stored variable has exactly one assignment in the whole
   function (the defining `:=` itself, or none besides the definition
   for a `var` spec). Assignment counting covers `=`, `:=` (including
   mixed new/existing), `++`/`--`, and range-clause targets.
3. Neither the stored variable nor the base variable has its address
   taken anywhere in the function (no alias can rewrite them).
4. No function literal in the body mentions either variable (no
   closure can rewrite them after the definition).
5. The base variable is never assigned after the definition position
   (assignments before it merely establish the defined value).
6. If the base is a named result, no value-`return` after the
   definition reassigns it (a bare `return` leaves results untouched).
7. The base variable is declared within the function itself (a local,
   parameter, or result — checked by declaration position against the
   enclosing declaration's range). A package variable, a dot-imported
   variable, or an outer function's local seen from a literal could
   be reassigned by code this body cannot see.
8. The conversion site is textually after the definition.

(8) plus (1) gives dominance: a direct statement of the body block
executes before every statement textually after it. The one apparent
hole — a `goto` jumping over the definition — is closed by go/types
itself, which rejects "goto jumps over declaration" before export runs.
Together, (2)–(7) mean the stored variable still holds exactly the
defined value at the conversion site, and the base identifier still
denotes the same pointer — so exporting the base identifier as the
derive node's base child reads the same pointer the definition stored.
Exporting the definition's own identifier node (not the name) is what
makes this robust against shadowing at the conversion site: the wire
refers to the variable by go/types object id.
Side-effecting base expressions are excluded by the bare-identifier
rule: re-evaluation is only sound because an identifier read has no
effects and, under (3)–(6), a stable value.

### 3.3 The offset-term hardening (a hole this lane closes)

The r39 prover trusted every non-base additive term as a byte count.
That is unsound for terms the prover KNOWS are pointer-derived: adding
a handle number to a base pointer yields a byte offset that can land
in bounds and silently alias the wrong element. This lane rejects
offset terms containing a known pointer-derived subexpression
(`exprHasPointerDerived`): a `uintptr(P)` conversion, a
`reflect.Value.Pointer()` call, or a proven stored-uintptr variable.
Two notes on scope:

- A `Pointer()` call used directly as an offset term was ALREADY
  rejected by r39 — not by the offset checks, but by the two-base
  rule (the term loop recognizes it as a second base term). The new
  check's genuine coverage is the shapes the old checks miss: a
  `Pointer()` call NESTED inside a larger offset term (e.g.
  `v.Pointer()*8`, a single multiplicative term the term loop never
  sees as a call), and a proven stored variable used as an offset.
  The pre-existing `exprContainsUnsafePointer` check already rejected
  offset terms containing an `unsafe.Pointer`-typed subexpression; a
  `Pointer()` call's subexpressions are `reflect.Value`-typed, and a
  stored variable is `uintptr`-typed, so neither old check catches
  them.
- Terms the prover does NOT know are pointer-derived (plain `uintptr`
  variables, function returns) remain trusted as byte counts, exactly
  as in r39. That is a documented limitation, not a hole introduced
  here: the prover cannot know their provenance, and gc treats their
  values as byte offsets too. Closing it needs value-level provenance
  (§5), not a syntactic check.

### 3.4 Tests

`internal/frontend/unsafe_derive_export_test.go`, following the
existing r39 pattern (wire-kind assertions over `ExportFile` output):

- Positive: stored local with offset, stored round trip, `var`-spec
  definition, chain copy, parameter base (also covering a non-`main`
  function).
- Negative (trap preserved): reassigned stored var, address-taken
  stored var, closure mentioning it, nested-block definition,
  reassigned base, address-taken base, package-level base,
  multi-assignment definition, non-identifier base, stored var as
  offset term.
- Negative (offset hardening): `Pointer()` call nested inside an
  offset term (`v.Pointer()*8`) — asserted as a count delta against
  a control program, because importing `reflect` pulls real reflect
  bodies (which legitimately contain `unsafederive` nodes) into the
  export; the two programs differ only in the offset term, so a
  lowering conversion would add exactly one `unsafederive` node. A
  top-level `Pointer()` offset term is kept as a pin of the
  pre-existing two-base rejection, not as hardening evidence.

### 3.5 Differential program (gc == graalgo, to be run at review time)

```go
package main

import (
	"fmt"
	"unsafe"
)

func main() {
	arr := [4]int64{10, 20, 30, 40}
	p := unsafe.Pointer(&arr[0])
	u := uintptr(p)
	q := unsafe.Pointer(u + 16)
	fmt.Println(*(*int64)(q)) // 30
	*(*int64)(q) = 99
	fmt.Println(arr[2]) // 99
	fmt.Println(*(*int64)(unsafe.Pointer(u))) // 10
}
```

Run under gc with `go run` — verified by this lane: `30`, `99`, `10`
in order. The graalgo leg was NOT run by this lane (open verification
for the reviewer/integrator): it needs `make build` at the repo root
followed by `internal/backend/graalgo-sync-run <program>`, and the
lane judged the marginal value low against a contested box because no
backend code changed — the wire node this shape emits (`unsafederive`
plus the `elemSize`-carrying `unsafetooptr`) is byte-identical to the
r39 path, and the exporter tests verify the emission exactly. The
reviewer should still run the graalgo leg before merge, and should
note that the launcher script's `gofrontend` path comment still
references the removed `phases/` tree (the launcher's ToolPaths
walk-up is what finds the binary in the current layout). The wave-2
runner `test/corpus/wave2/run_wave.sh` still points at the removed
`phases/` tree and was deliberately NOT extended — see §6.

Lane verification actually run (all in the lane worktree, all under
`nice`, all gated on `load-shed.sh backoff` CLEAR):

- `go test -run TestUnsafeDerive` in `internal/frontend`: 23 tests,
  PASS (76.9 s) — the 6 pre-existing r39 tests plus the 17 new ones
  (5 positive, 11 negative, 1 count-delta pin).
- Failing-then-passing: with only the two implementation files
  stashed (tests kept), the 5 new positives, the chain-copy positive,
  and the stored-as-offset negative FAIL, and the nested
  `Pointer()`-offset test FAILS; with the implementation restored
  they all PASS. The top-level `Pointer()`-offset pin passes on both
  sides, as designed (it pins pre-existing two-base behavior).
- Full `internal/frontend` package suite: PASS, exit 0, 1144 s
  (the first attempt hit the default 10-minute timeout while the
  test binary was frozen by the load shedder — a T-state freeze, not
  a hang; the rerun with `-timeout 60m` completed green).
- `gofmt -l` on all three changed files: clean.

## 4. Shapes that remain trapped, and exactly why

| Shape | Example | Why it still traps |
|---|---|---|
| Stored through a parameter | `func f(u uintptr) { unsafe.Pointer(u+8) }` | Callers are not visible from one function body; a parameter can be called with any integer. Needs whole-program call-graph analysis, and even that fails for function values / interface calls. |
| Stored in a struct field | `s.u` where `u uintptr` was `uintptr(p)` | Field writes happen anywhere (other functions, other packages); no intraprocedural proof. Same for array elements, map values, globals. |
| Stored through a return value | `u := f()` where `f` returns `uintptr(p)` | Interprocedural; the exporter's per-function proof cannot see into `f`. |
| Stored through a function value / interface | — | As above, plus dynamic dispatch. |
| Multi-assignment definition | `u, v := uintptr(p), 0` | V1 scope only (single-LHS). Mechanical to add; not yet done. |
| Non-identifier base | `u := uintptr(unsafe.Pointer(&x))`, `u := uintptr(s.p)` | Re-evaluating the base at the conversion site is not proven stable without a free-identifier stability analysis (does every variable in the expression keep its value? is every index/field write-free?). The V2 generalization is: allow any base expression whose free identifiers satisfy the §3.2 stability rules (no assignment after the definition, no address-taken, no closure mention). That also covers `&arr[i]` with stable `i`. |
| Pointer() stored | `u := v.Pointer()` | Same V1 scope restriction as non-identifier bases: the receiver expression would need the stability analysis (receiver is a struct value, not an identifier). The `Pointer()` call itself is pure, but its receiver's stability is what needs proving. |
| Named uintptr types | `type addr uintptr; unsafe.Pointer(addr(p)+8)` | `isUintptr` requires the basic type; named integer types take a different conversion path and currently scope-error rather than trap. Needs the general named-integer-type conversion treatment, not a uintptr special case. |
| Double / wrapped conversions | `uintptr(uintptr(p))`, `+uintptr(p)` | The term recognizer does not unwrap them. Trivial to add (recurse through `uintptr` conversions / unary `+`); real-world frequency is ~zero, so deferred, not forgotten. |
| Bitwise / masking ops | `uintptr(p) &^ 7`, `uintptr(p) \| flags` (tagged pointers) | These need the pointer's NUMERIC address: alignment rounding and tag bits are meaningless on dense handle numbers. Only a fake-address model (§5.2) can support them, and even then tag bits need a defined masking semantics at consumption. |
| Scaled / multiplied pointer terms | `uintptr(p) * 2` | Not a derivation at all (scaling an address is not addressing). Correctly trapped forever, unless the multiplier is the constant 1 — a degenerate case not worth a rule. |
| Two pointer terms | `uintptr(p) + uintptr(q)` | Not a derivation (sum of two addresses). Correctly trapped forever. |
| `StringData` + nonzero offset, then `*byte` deref | `(*byte)(unsafe.Pointer(unsafe.StringData(s) + off))` — via the proven route `uintptr(unsafe.Pointer(unsafe.StringData(s))) + off` | The derivation itself would work (StringData handle → `ByteRef` over the `GoString`), but consumption traps: `UnsafeToPtrNode.derivedPointer` only handles array roots, and there is no `GoString`-root byte-pointer consumption (a `*byte` over a string's bytes). Offset 0 is different: `derive` returns the base handle unchanged, so the round trip itself is representable — but dereferencing the resulting `*byte` over a `GoString` referent is likewise unproven and must be checked before anyone claims the shape. |
| Arbitrary integers | `unsafe.Pointer(42)`, `unsafe.Pointer(n)` for unknown `n` | No provenance exists, by definition. Only §5 retires these. |

## 5. Retiring the trap entirely: the two real designs

Both are Phase-4-scale work, not follow-up lanes. The choice between
them is the decision this design puts to the coordinator.

### 5.1 Fat uintptr: carry provenance in the value

Change the representation of `uintptr` values that originate from a
pointer to a pair `(number, provenance?)`, where provenance is
`(base handle, byte offset)` or empty. Arithmetic on provenance-carrying
values propagates it (`+`/`-` integer adjusts the offset; any other
operation — multiply, divide, bitwise ops, shifts — drops it to a bare
integer, except `&^` with a mask that provably preserves alignment,
which needs care). A conversion materializes iff provenance is present
(and the offset validates at consumption, as today); otherwise it
traps exactly as now. Stored/field/parameter/return flows then work
automatically, because the value carries its own proof.

- Pros: no global address space; provenance is exact (no numeric
  collisions, ever); the trap's loud-failure contract is preserved for
  genuinely bare integers; incremental (only `uintptr`-typed values
  need the new representation, and only the pointer-derived ones box).
- Cons: pervasive. Every node that produces, consumes, or stores a
  `uintptr` must handle two representations (bare `long` vs boxed
  provenance-carrying value): arithmetic, comparison, conversion,
  frame slots, interface boxing, map keys, channel sends, struct
  fields, and the `unsafe.Pointer → uintptr` direction. Truffle
  specialization pressure on every integer path. The boxed form must
  also survive serialization boundaries the backend currently assumes
  are `long` (wire format, snapshots). This is a backend-wide refactor
  with a long tail of "forgot one path" bugs, each of which is either a
  trap (safe) or a dropped provenance (loud trap, safe) — the failure
  mode is safe, but the surface is large.

### 5.2 Fake-address (byte-addressed) value model

Assign every allocation a numeric address range in a global fake
address space; `uintptr` IS the fake address; `unsafe.Pointer` is the
same number; conversion is a range-map lookup (address → owning
allocation + offset), trapping when unmapped. Pointer arithmetic is
ordinary integer arithmetic. This is the model the bridge inventory's
Phase 4 names ("byte-addressed value model").

- Pros: gc-faithful semantics for ALL of §4's trapped shapes at once,
  including bitwise/tagged pointers and field offsets (given real
  struct layouts, which the frontend already knows from go/types
  `Sizes` and the reflect tables). No per-value boxing; integers stay
  integers everywhere.
- Cons: it is a new value model, not a feature. Allocations need
  address assignment and liveness (a fake address must stop resolving
  when its object dies, or reuse must be proven safe — the current
  "handles are never reused" guarantee exists precisely to avoid this).
  Interior pointers, one-past-the-end pointers, and zero-sized
  allocations need address-space rules. And it still does NOT solve
  memory reinterpretation (`*T` over bytes of a different type) — that
  needs byte-level storage for values, a further, larger rewrite. So
  §5.2 retires the trap but not D5's reinterpretation limit; the
  consumption sites' type validation stays.
- Recommendation: §5.2 is the honest end state (it matches gc's
  semantics, where addresses ARE numbers), but it must land as its own
  program with a migration plan for the handle model, not as a lane.
  §5.1 is a smaller, safer intermediate step that retires the
  parameter/field/return shapes without re-architecting storage — at
  the cost of touching every `uintptr` path. If the coordinator wants
  one next step after V1, it is the V2 syntactic generalization (§4,
  non-identifier bases via free-identifier stability) followed by §5.1,
  with §5.2 reserved for the frontend-v2 rewrite, where the value
  model is being redesigned anyway.

## 6. Deliberately not done in this lane

- No corpus addition: `test/corpus/wave2/run_wave.sh` still builds
  `phases/phase3` etc., removed 2026-10-14. Adding a program to a
  stale runner would be debris, not coverage. The differential program
  in §3.5 is the artifact; reviving the runner is a separate lane.
- No backend change: the entire extension is frontend proof + wire
  reuse. The backend's `derive`/`ByteRef`/consumption semantics are
  untouched, so no existing differential behavior can drift.
- No change to `fromUintTrap`'s message or contract: the trap still
  names the byte-addressed value model as its re-test trigger.
- No `check_no_bridges` impact: no new wire node, no new
  package-name-keyed branch, no synthetic package. The stored-uintptr
  recognizer is keyed on go/types objects (variable identity), never
  on package or symbol names.
