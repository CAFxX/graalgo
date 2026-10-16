# frontend/v2 — generics instantiation design

**Status:** design + MVP implementation (2026-10-16, lane `v2-generics`).
**Parent design:** [frontend-v2.md](frontend-v2.md) (§2 Stage 3, §6 open questions).
**Review finding:** V2-17 — generic declarations emitted but unusable;
`IndexExpr`/`IndexListExpr` instantiation failed loudly; the "full Go 1.27"
claim in [frontend-v2.md](frontend-v2.md) did not match reality.

## 1. Decision: monomorphization at emit time

Each distinct instantiation of a generic function with concrete type
arguments is emitted as its own ordinary (non-generic) `funcdecl`, with a
mangled name derived canonically from the function name and the type
arguments (e.g. `id` + `[int]` → `id[int]`). Generic function declarations
themselves are registered as *templates* and are **not** emitted as
`funcdecl`s — they are not callable.

The instantiation worklist runs to a fixpoint: emitting a body may request
further instantiations (e.g. a generic function calling another generic
function), which are enqueued and emitted in turn.

### Why monomorphization (and not dictionary-passing)

| Approach | Verdict |
|---|---|
| **Monomorphization at emit time** (chosen) | Backend (Truffle) has no representation for type parameters at all — every node it builds is fully concrete. Monomorphization keeps the **binary wire format unchanged**: an instantiation is just another `funcdecl` with concrete types. No new node kinds, no encoder changes, no Java changes. `go/types` already validated constraints at the check stage, so the emitter trusts the type arguments. No synthetic runtime support, no bridges. |
| Dictionary-passing (gc's hybrid: GC-shape stenciling + dictionaries) | Would require threading runtime type dictionaries through every generic call: new IR node kinds, wire-format additions, new Truffle nodes, and a dictionary ABI. Far larger blast radius for the MVP, and it invents machinery the backend does not need yet. |
| Source-level rewriting before check | Would duplicate go/types' job and forfeit its constraint checking. |

**Honest cost:** one emitted body per distinct (function, type-args) pair —
code duplication on the order of C++ templates. If this ever dominates
binary size, the documented future direction is gc-style *stenciling*
(share bodies across type arguments with identical representation), which
is a pure emitter optimization that changes neither the wire format nor
this design's external contract.

## 2. Mechanics (what the MVP implements)

- **Template registry** (`emit/generics.go`): pass 1 of `emitDocument`
  registers every `FuncDecl` whose signature has type parameters.
  Generic *methods* (receiver is an instantiated generic type) and generic
  *type declarations* (`TypeSpec` with `TypeParams`) fail loudly with
  `unsupported` — they are tracked follow-ups, not silent drops.
- **Instantiation sites** are detected via `types.Info.Instances` — the
  authoritative go/types record — never via syntax heuristics:
  - `*ast.IndexExpr` (`id[int]`) and `*ast.IndexListExpr` (`pair[int,string]`)
    with an `Instances` entry → explicit instantiation.
  - `*ast.CallExpr` whose `Fun` has an `Instances` entry keyed by the
    identifier (`id(42)`) → inferred type arguments, instantiated the same way.
  - A generic function *value* without type arguments (`f := id`) → loud
    `unsupported`: a value of generic function type has no concrete
    instantiation to monomorphize.
  - Instantiation of a generic function from another package → loud
    `unsupported`: v2 has no multi-package import resolution yet, so there
    is no source body to instantiate from (tracked follow-up).
- **Type substitution** (`instContext.substType`): while emitting an
  instantiation body, every `*types.TypeParam` is replaced by the
  corresponding concrete type argument, recursively through all structural
  type constructors (pointer, slice, array, map, chan, signature, struct,
  tuple, union). Parameters, named results, and body locals are re-declared
  as fresh `*types.Var` objects with substituted types, so the object and
  type tables contain only concrete types.
- **Name mangling:** `name + "[" + comma-joined go/types type strings
  (package-path-qualified) + "]"`. Brackets are not legal in Go identifiers,
  so a mangled name can never collide with user code; the type strings are
  canonical, so identical instantiations share one emitted function
  (dedup via the instantiation cache, populated *before* body emission so
  recursive generics terminate).
- **Safety valve:** a per-template instantiation cap (256) fails loudly on
  unbounded instantiation chains such as `f[T]` calling `f[[]T]` —
  distinct type arguments forever. This is a deliberate, documented limit,
  not a heuristic.

## 3. Interaction with the binary wire format

**None.** That is the point. The encoder (`v2/encode`) walks the
`Document` generically; instantiated functions are `funcdecl` nodes with
concrete `signature` types, indistinguishable from hand-written ones.
`NodeFactoryV2` needs no changes: it builds whatever `funcdecl`s the
document carries. Generics support is entirely a Go-side emitter concern.

## 4. What the MVP does NOT do (tracked follow-ups)

1. Generic methods (needs receiver-instantiation machinery).
2. Generic named types (`type Box[T any] struct{...}`) and their
   instantiation — includes generic types from other packages.
3. Multi-package programs (v2 general gap); cross-package generic
   instantiation rides on that.
4. Constraint-driven operations that only type-check via interfaces
   (e.g. calling constraint-interface methods on a type-parameter value)
   — go/types accepts them, but backend support is unverified; each such
   case should get a differential test before being claimed.
5. Stenciling / code sharing across instantiations (size optimization).

## 5. Scope correction for the "full Go 1.27" claim

[frontend-v2.md](frontend-v2.md) said: *"The new frontend accepts ALL Go
syntax from the start … If `go/parser` accepts it, v2 handles it."* That
was false: the mechanical coverage matrix
(`internal/frontend/v2/emit/coverage_test.go`) classifies 60 AST node
kinds, and a defined set *loudly fails* with `unsupported` (proven by
`emit/negative_test.go`). The corrected claim, now also reflected in
[frontend-v2.md](frontend-v2.md):

> v2's *design goal* is the complete Go 1.27 language. The *current*
> implementation status is exactly what the coverage matrix says:
> handled, via-`go/types`, structural, or loudly rejected. Anything not
> handled is a compile error, never a silent skip — "full Go 1.27" is the
> destination, not today's state.
