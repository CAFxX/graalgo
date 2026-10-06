# P3-M3 red-green-first evidence — Java consumer half of ADR-0003 (WS B)

Date: 2026-10-06. HEAD at start: `1bc1f36` (graalgo master).
Worker: P3-M3 Java-runtime (WS B consumer); the Go producer (exporter)
is built in parallel by the sibling worker against the same ADR.

## Scope

New package `phases/phase3/src/main/java/graalgo/iface/`:
`IfaceMethodTable` (static registry), `IfaceItab` (immutable itab),
`IfaceDispatchNode` (PIC dispatch, limit 3, megamorphic fallback),
`IfaceDispatchBuilder` (the `ifacemethod` wire builder),
`IfaceMethodTables` (launcher doc→table resolution),
`DescriptorNames` (replica of the factory's descriptor naming).
Plus: `P3SyncMain` wiring (register builder, install tables),
phase-3 Makefile `test` target, 13 unit tests.

No Phase-1/Phase-2 source modified (verified: `git status` shows only
`phases/phase3/` paths).

## RED: `DescriptorNamesTest` against the ADR-literal pointer naming

The critical pin: `DescriptorNames` replicates `NodeFactory`'s private
descriptor naming so launcher-computed table keys match the runtime
`GoTypeDescriptor.name()`. ADR-0003 D5 claims pointer entries key by
`"pointer#"+typeId`; the factory actually names them
`"pointer#"+hex(structuralKey.hashCode())`.

Mutation run (temporary edit, restored afterwards): structural branch
forced to the ADR-literal `"pointer#"+typeId`. Result:

```
FAIL DescriptorNamesTest.testNamesMatchFactory:
  descriptor name for type 2 expected=pointer#12a82948 actual=pointer#2
FAIL DescriptorNamesTest.testPointerNameIsStructural:
  not the raw type id: pointer#2
13 tests, 2 failures
```

The cross-check against the real factory's `typeDescriptorFor` catches
exactly the ADR-literal error — the test is capable of failing, and the
deviation (factory-accurate naming implemented; ADR-0003 not modified,
deviation reported to the coordinator for amendment) is load-bearing:
with the literal naming, every pointer-receiver interface call would
miss table lookup.

## GREEN: full suite

After restoring the replica:

```
PASS IfaceDispatchNodeTest.testPicHit
PASS IfaceDispatchNodeTest.testPicOverflowMegamorphic
PASS IfaceDispatchNodeTest.testNilPanics
PASS IfaceDispatchNodeTest.testTypedNilProceeds
PASS IfaceDispatchNodeTest.testMissingMethodFailsFast
PASS IfaceDispatchNodeTest.testMissingTableFailsFast
PASS IfaceDispatchNodeTest.testProbesRecorded
PASS IfaceMethodTablesTest.testResolve
PASS IfaceMethodTablesTest.testMissingObjIdFailsFast
PASS IfaceMethodTablesTest.testEmptyDoc
PASS DescriptorNamesTest.testNamesMatchFactory
PASS DescriptorNamesTest.testPointerNameIsStructural
PASS DescriptorNamesTest.testAliasResolves
13 tests, 0 failures
```

Red properties (each test fails against the named breakage):
- `testPicHit`: fails if itabs are rebuilt per dispatch
  (`itabCacheSize()==1`); fails if receiver data is not params[0] or arg
  order is wrong (captured call args asserted).
- `testPicOverflowMegamorphic`: fails if the PIC limit is not 3
  (transition count != 1) or the fallback returns wrong funcs.
- `testNilPanics`: fails if nil does not panic, or the message is not
  gc's nil-dereference text (parity with `GoErrors.nilDereference()`,
  the `FuncValueCallNode` path).
- `testTypedNilProceeds`: fails if typed-nil panics or params[0] is not
  null.
- `testMissingMethodFailsFast` / `testMissingTableFailsFast`: fail if a
  missing method/table does not fail fast with `FrontendException`.
- `testProbesRecorded`: fails if a dispatch does not record its itab
  (histogram total / identity count asserted).
- `testResolve`: fails if named entries do not key by `detail.name`,
  pointer entries by the structural name, or objIDs misresolve.
- `testMissingObjIdFailsFast`: fails if a dangling method objID does
  not fail fast naming method and objID.
- `testNamesMatchFactory`: fails if the replica drifts from the factory
  on any of 14 type-table shapes (proven by the mutation run above).

## Gates

- `make main`: clean under `-Xlint:deprecation -Werror`.
- `make test`: 13/13 green (above).
- `make check`: placeholder + debug-leftover gates clean.
- Launcher smoke: `make run ARGS="--check corpus/sync/sy01_mutex_exclusion.go"`
  exit 0; full run prints `counter: 32000`, exit 0 — the P3SyncMain
  wiring (builder registration + table install on every build) does not
  disturb existing programs.

## Resolved (2026-10-06)

End-to-end is done: the exporter half landed in this workstream
(commits `01ac1c5`, `3cec0fc`), the iface corpus
(`phases/phase3/corpus/iface/`, if00–if10) is committed, and the
differential vs gc 1.27.1 is 11/11 byte-identical. The pointer
descriptor-name keying deviation was amended into ADR-0003
(`417c3cc`).

## End-to-end red-green: interface dispatch mutations (2026-10-06)

Method: isolated copy of the tree at `~/workspace/mut` (never the live
tree — AGENTS.md mutation lesson); copy verified clean first
(`grep` for mutation markers: none; `if00` baseline byte-identical on
the copy). Each mutation rebuilt the affected layer, ran the named
corpus program(s), then was reverted before the next. All three went
red — the PIC, the method tables, and the itab func mapping are all
load-bearing; no dead code, no silent wrong-method divergence.

- **Mutation A — break the PIC identity check** (Java,
  `IfaceDispatchNode.lookupPic`: `s0 != null && s0.itab == itab` →
  `s0 != null`, so slot 0 always hits and every dynamic type at the
  site dispatches the first-seen type's method). Corpus `if05`
  (4 types, 1 site): RED — `ClassCastException: Long cannot be cast
  to GoStruct`, exit 1, output `8` vs expected `8/8/0/16`. The itab
  identity comparison is load-bearing: without it, cross-type dispatch
  is a crash, not a slowdown.
- **Mutation B — drop pointer-receiver methods from `*T` sets** (Go
  exporter, `methodTableEntries`: skip `recv == "pointer"` entries for
  pointer types). Corpus `if00`, `if08`: RED —
  `if00`: `interface dispatch: no method table installed for dynamic
  type "pointer#…"` (the table went empty and was never installed);
  `if08`: `interface dispatch: method "Write" missing for dynamic type
  "pointer#…" (unreachable: go/types verified assignability)` — the
  "unreachable" guard fired exactly as designed. Pointer-receiver
  methods in `*T` sets are load-bearing.
- **Mutation C — swap two methods' GoFuncs in one itab**
  (`IfaceMethodTable.buildItab`: exchange the `GoFunc`s of the first
  two installed methods, keeping names/paths). Corpus `if09`
  (`Writer`+`Closer`, 2 methods): RED — `ArrayIndexOutOfBoundsException:
  Index 2 out of bounds for length 2` (Close's func invoked with
  Write's argument frame), empty stdout vs expected `3` / `3 true`.
  What C proves, scoped honestly (R2 F3): the itab's name→`GoFunc`
  mapping is load-bearing, and a wrong-method dispatch *with mismatched
  arity* crashes loudly at the call boundary. C does NOT prove "a
  wrong-method dispatch cannot be silent" in general — nothing at the
  invoke boundary checks method identity (`GoCalls.invoke` receives a
  `GoFunc`; the compiled `RootCallTarget` carries no method-name
  check), so a same-arity swap would dispatch the wrong method with a
  plausible frame and no crash. See Mutation C2.
- **Mutation C2 — swap two SAME-ARITY methods' GoFuncs in one itab**
  (R2 F3; `IfaceMethodTable.buildItab`: exchange the `GoFunc`s of
  `Inc`/`Dec`, both `() int`, on a probe program with
  `Inc()→11, Dec()→10`). RED via **output mismatch**, not a crash:
  `9` / `10` vs expected `11` / `10` (Inc dispatched Dec's func).
  This is the honest demonstration the methodology review asked for:
  the mapping is load-bearing through wrong output, and a same-arity
  wrong-method dispatch at the mechanism level IS silent (no crash,
  no fail-fast — just wrong results). What keeps it correct in
  production is the construction path: the mapping is built straight
  from go/types method names (`methodTableEntries` → obj ids →
  `Program.functions()`), guarded by the itab-build fail-fasts
  (missing-method / missing-table), and any corruption of it would
  surface as output divergence in the differential corpus (which
  exercises multi-method interfaces: if02, if09, if11, if12).

Baseline restored after each mutation (revert + rebuild + `if00`
byte-identical before the next mutation); the live tree never carried
a mutation — all mutation edits and rebuilds happened under
`~/workspace/mut`, and the live tree's `git diff` was verified to
contain no `MUTATION_` markers at the end of the exercise.

## P3-M3 remediation red-green (2026-10-06)

Review: `reviews/phase3-m3/reviewer1-code.md` (R1), `reviewer2-methodology.md` (R2).
Each P0 has true red-green: the repro was run against the pre-fix build
(RED) and the post-fix build (GREEN), via `git stash` of the fix with
rebuilds on both sides. gc is 1.27.1 throughout.

- **R1 P0-1 — signature-aware implements check.** Repro: `a.(Writer)` where
  the dynamic type declares `Write(string)` instead of `Write([]byte)`.
  RED (pre-fix): graalgo prints `assert ok`, gc prints `assert failed` —
  the silent divergence. GREEN (post-fix): graalgo prints `assert failed`
  (gc parity). Positive controls, all gc-identical post-fix: correct
  signature with a differently-named parameter (`Write(p []byte)`) still
  asserts ok; `byte`/`uint8`/alias (`type B = []byte`) cross-spellings
  assert ok (the key expands aliases and keys basics by `BasicKind`).
  Pinned by corpus `if11_assert_signature.go` (differential 13/13).
- **R1 P0-2 — canonical descriptors from the builder.** Repro (reviewer's):
  `i.M(&T{v:2})` with `x.(*T)` comma-ok inside, plus a type-switch variant.
  RED (pre-fix): graalgo prints `no` / `no`, gc prints `yes-*T` / `yes-*T`.
  GREEN (post-fix): `yes-*T` / `yes-*T` (gc parity), via the new public
  `NodeFactory.canonicalDescriptorFor` accessor (additive; ADR-0003
  amendment). Pinned by corpus `if12_callee_type_identity.go`.
- **R1 P1-1 — alias-of-interface receiver.** Repro: `type W = Writer;
  var w W = &Buf{}; w.Write(...)`. RED (pre-fix): gofrontend exits 2,
  `receiver static type main.W is not an interface (exporter bug)`.
  GREEN (post-fix): exports cleanly, graalgo prints `5` (gc parity).
  Pinned by `TestIfaceMethodCallAliasReceiver`.
- **R1 P1-2 — generic instantiation via non-empty interface.** Repro:
  `var g Getter = Box[int]{v: 7}; println(g.Get())`. RED (pre-fix): dies
  at first dispatch with the misleading `no method table installed for
  dynamic type "main.Box[int]" (needs gofrontend >= 0.4.0 WS D method
  tables)` — the document IS 0.4.0. GREEN (post-fix): fails fast at
  frontend build time with the named scope error `cannot use generic
  instantiation main.Box[int] as a non-empty interface: method tables
  for instantiated methods are not supported (P3-M3 scope)` — at all
  three boxing sites (`maybeConvert`, `adaptArgsForInterfaceParams`,
  the P3 builder's `boxArg`) and, for the `any`-then-assert shape, at
  assertion runtime via the `TEMPLATE_METHODS_SKIPPED` sentinel (a bare
  `false` there would be a silent divergence vs gc, which decides by the
  real method set). `Box[int]` boxed as `any` stays legal (gc parity).
  Pinned by `TestIfaceInstantiationSkippedMarker` (wire marker).
- **R2 P2-7 — narrowed unsupported-promotion fail-fast.** Repro: interface
  with a supported method `Val` and an unsupported promotion `Ptr`
  (pointer receiver via value-embedded field). RED (pre-fix): dispatching
  only `Val` dies at itab build — gc prints `15`. GREEN (post-fix):
  `Val` dispatches (gc parity `15`); dispatching `Ptr` still fails fast
  with the named `(P3-M3 scope)` error. Pinned by
  `testSupportedMethodBesideUnsupportedDispatches` (fails pre-fix) and
  `testUnsupportedPromotionFailsFastAtDispatch`.
- **Go unit red-greens** (each verified FAIL pre-fix / PASS post-fix via
  stash): `TestFreshSigIDKeepsTypeListParallel` (R2 P2-3),
  `TestMentionsTypeParamAliasUnion` (R2 P2-4).
