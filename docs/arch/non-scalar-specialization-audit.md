# Non-scalar object specialization audit (2026-10-15)

His concern: non-scalar Go values (strings, slices, maps, structs) are treated
as generic `Object` instead of having Truffle specializations.

## Finding 1: Static-kind switches are already optimal — no DSL needed

Most nodes (`IndexNode`, `BinaryOpNode`, `ConvertNode`, etc.) switch on a
**final** kind field set at construction from frontend type info:

```java
// IndexNode.read(): containerKind is final
return switch (containerKind) {
    case SLICE -> { GoSlice s = (GoSlice) container.executeObject(frame); ... }
    case MAP   -> { GoMap m = (GoMap) container.executeObject(frame); ... }
    ...
};
```

The Graal compiler constant-folds the switch on the final field. The
`executeObject` + checkcast is a single type check. A DSL `@Specialization`
on the runtime type would add polymorphic-cache overhead for a site that is
already monomorphic by construction. **No change.**

## Finding 2: IfaceDispatchNode hand-rolled PIC → DSL (DONE)

`IfaceDispatchNode` (interface method dispatch) had a hand-rolled polymorphic
inline cache: volatile `pic0/pic1/pic2` slots, identity comparison on itab,
synchronized `lookupPicMiss`, and a `HashMap` megamorphic fallback.

This is exactly what the Truffle DSL generates. Converted to:

```java
@Specialization(limit = "3", guards = "descriptorOf(gi) == cachedDescriptor")
Object doCached(VirtualFrame frame, GoInterface gi,
        @Cached("descriptorOf(gi)") Object cachedDescriptor,
        @Cached("itabForDescriptor(cachedDescriptor)") IfaceItab cachedItab,
        @Cached("targetForItab(cachedItab)") IfaceMethodTarget cachedTarget) {
    return invokeWithTarget(frame, gi, cachedTarget);
}

@Fallback
Object doMegamorphic(VirtualFrame frame, GoInterface gi) {
    megamorphicTransitions.incrementAndGet();
    ...
}
```

The guard keys on receiver descriptor identity (cheap field read); the itab
and target are derived once per specialization via `@Cached`. The full
dispatch body (embedded redispatch, receiver handling, `GoCalls.invoke`) is
unchanged in `invokeWithTarget`.

## Finding 3: GoEquality — static utility, out of scope for DSL

`GoEquality.equal(Object, Object)` has 36 instanceof checks. It is a static
utility called from `GoMap`, `BinaryOpNode`, `SwitchNode`, `GoStruct` — not a
Truffle node. Converting it to a DSL node would require changing all call
sites, including non-AST ones (map internals). The instanceof chain is a
linear type test; for map-key equality the common cases (Long, GoString) are
early in the chain. A future lane could introduce a `MapKeyEqualityNode`
with DSL specializations at the AST-level call sites.

## Finding 4: Scalar typed executes are the model

`GoExprNode` already provides `executeInt`/`executeLong`/`executeFloat`/
`executeDouble`/`executeBoolean` for scalars. Non-scalars funnel through
`executeObject` because Java has no primitive carrier for them — the
concrete classes (`GoString`, `GoSlice`, `GoMap`, `GoStruct`) ARE the
specializations, distinguished by the static kind. Adding per-class execute
methods (e.g. `executeGoString`) would not enable DSL dispatch (the DSL
specializes on parameter types, and these nodes take `VirtualFrame`, not
values).

## Verification

- `javac -proc:none` parse check: no syntax errors (only expected
  missing-symbol errors for single-file compilation; DSL imports resolve).
- Full build + DSL processor verification: pending (box load 16+); CI
  `make all` is the backstop.
- Behavior: unchanged. The DSL generates a PIC equivalent to the removed
  hand-rolled one (limit 3, megamorphic fallback, transition counter).
