# P1-12 design: `(*runtime.Func).Name()` and the stale `panicMsg` marker

Lane: `wt/graalgo-review-20261018/p1-12-funcname-table` (review item RV22-P1-12).
Scope: [internal/frontend/export.go](../../internal/frontend/export.go) only — the `panicMsg` marker
(`resolveMethodCall`, ~:4759) and the injected-`panic` AST (`methodRefNode`,
~:9646). No backend touch.

## What the review assumed vs. what the tree contains

The review (2026-10-18 22:00 CEST, master tip `56a3dd68`) says `(*Func).Name`
"compiles to an injected `panic(...)` call site" because "no PC-to-function
table" exists, and asks to either implement the table or fail at compile
time. That premise is stale:

- Commit `84ff0434` (2026-10-14) added the `panicMsg` marker with the
  comment "no PC-to-function table".
- Commit `fbdd1aa0` ("runtime: implement stack-inspection family",
  descendant of `84ff0434`) implemented the family **for real** and did not
  remove the marker:
  - Frontend: `exportCall`'s `runtimeStackMethod` interception
    ([export.go](../../internal/frontend/export.go) ~:8704) emits the
    additive `runtimefuncname` wire node for `(*Func).Name` — the exact
    sibling of `runtimefuncentry`.
  - Backend: [RuntimeFuncNameNode.java](../../internal/backend/interop/src/main/java/graalgo/rtstack/RuntimeFuncNameNode.java)
    executes `RtStack.derefFunc(f).funcName()`.
  - The PC-to-function table exists: `graalgo.runtime.StackTrace`
    ([StackTrace.java](../../internal/backend/runtime/src/main/java/graalgo/runtime/StackTrace.java))
    interns `(funcName, file, line)` at stack-capture time
    (`RuntimeCallerNode`, `RuntimeCallersNode`); `GoFuncInfo.forPc(pc)`
    ([GoFuncInfo.java](../../internal/backend/runtime/src/main/java/graalgo/runtime/GoFuncInfo.java))
    resolves PC → handle. This is design option (a)'s second variant —
    the backend maintains the map, no frontend sidecar needed.

So the primary call path (`name := fn.Name()`, `go fn.Name()`,
`defer fn.Name()`, nested calls) already works end to end; the review's
"fail at compile time" fallback is unnecessary for it.

## Reachability audit of the marker (empirical, frontend test harness)

| Position | Path | Result today |
|---|---|---|
| `name := fn.Name()` (call) | `exportCall` → :8704 interception | `runtimefuncname` wire node ✓ |
| `go fn.Name()` | `exportGoStmt` → `exportCall` (no args) → :8704 | `runtimefuncname` in `gostmt` ✓ |
| `defer fn.Name()` | `exportDeferStmt` → `exportCall` → :8704 | `runtimefuncname` in `defer` ✓ |
| `go x.M(args)` via `hoistGoMethodRecv` | `methodRefNode` → marker → injected panic | **unreachable** — `Name()` takes no args, and `exportGoStmt` only uses the hoist path when `len(args) > 0` |
| `exportMethodCall` → `methodRefNode` | marker → injected panic | **unreachable** — sole caller is `exportCall` :8845, after the :8704 interception |
| `f := fn.Name` (method value) | `methodValueNode` → `resolveMethodCall` → marker | **EXPORTER CRASH** — `mc.decl` is nil, `mc.decl.sig` nil-dereferences (not even the injected panic) |
| instantiation enumeration (:5184, :5775) | `resolveMethodCall` → marker | benign (`needsInst` false, marks handled) |

The injected `panic(...)` AST is currently unreachable; the marker's only
live damage is the method-value crash.

## Design questions from the brief

**(a) PC-to-function table.** Already exists (see above). PCs become known
at stack-capture time in the backend; `FuncForPC` interns them and
`GoFuncInfo.forPc` resolves them. No frontend sidecar, no new interop.

**(b) Can `Name` ride `Entry`'s mechanism?** It already does — same
interception, same `f`/`type` wire shape, same `derefFunc` unwrap.

**(c) gc guarantees for edge cases.** gc's `(*Func).Name` returns `""` for
a nil `*Func` (verified against the local Go 1.27.1 runtime sources);
graalgo's `RtStack.derefFunc` raises a nil-dereference panic instead. That
is a **backend** behavioral drift, out of this lane's frontend-only scope —
flagged here for the coordinator, not fixed here. Closures/generics:
`forPc` returns null for unknown PCs; `FuncForPC` already handles that
path — again backend territory, already implemented.

## Recommendation (implemented by this lane)

The "implement the table" option is already realized; the "compile-time
scope error" fallback is unnecessary for calls. The correct fix is to
**retire the stale marker** and close the one live hole:

1. Delete the `panicMsg` field, the `resolveMethodCall` marker branch, and
   the `methodRefNode` injection branch. Their premise is false, the
   injection is unreachable, and leaving them invites the next reader to
   believe `Name` is unimplemented.
2. In `methodValueNode`, check the existing general classifier
   `runtimeStackMethod(selection)` **before** `resolveMethodCall` and fail
   with a named compile-time scope error (same mechanism as
   `runtime.Caller` at :8665): method values of the synthetic
   stack-inspection methods have no `funcdecl` for the `methodvalue` wire
   node to reference, so they are out of frontend/v1 scope. This converts
   the exporter crash into a clean diagnostic. Using `runtimeStackMethod`
   keeps it structural (whole family), not name-keyed.

Not done here (out of scope / not clearly safe): implementing real method
values for the stack family (needs a backend wire node or synthetic
funcdecl), the nil-`Func` `""`-vs-panic drift (backend + snapshot
re-baseline), anything in the sibling lanes' regions.
