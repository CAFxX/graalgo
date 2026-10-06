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

## Open (pending the Go producer)

End-to-end dispatch against a real gofrontend ≥ 0.4.0 document
(`ifacemethod` nodes + `methods` on named/pointer entries) and the
ADR's differential corpus (`phases/phase3/corpus/iface/`, gc 1.27.1)
run once the sibling Go worker lands the exporter half. The Java
consumer honors the ADR wire format verbatim; the only intentional
deviation is the pointer descriptor-name keying (this file's RED
section), reported for ADR amendment.
