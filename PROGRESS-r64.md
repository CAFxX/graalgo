# r64 — C1+C2 condition discharge for r63 tip 1c4ff136 (CONDITIONAL APPROVE)

Branch `fix-redmaster-fullsuite-a0df481b-r64` from r63 tip
`1c4ff136` (branch CI fully green on 266bb9d0: 705/0; reviewer caf30134,
CONDITIONAL APPROVE with conditions C1+C2, discharge by landing-check).

## C1 — GoByteCell Long-only storage (reviewer condition)

- `GoByteCell.get()/set()` now require the wrapped cell to hold a `Long`
  (was any `Number`); `set()`'s byte value accepts integral Numbers
  (`Long`/`Integer`/`Byte`/`Short`) and rejects `Double`/`Float` loudly.
  The integral-accepting split is required: guest uint8 boxes to
  `Integer` (`Boxing.java`), so Long-only values would break every real
  byte write (`DerefWriteNode.java:43`).
- `UnsafeToPtrNode.byteCellPointer` mints only over a `Long`-carrying cell.
- Tests (`GoByteCellTest`, +4): `testIntegerCellFailsLoud`,
  `testDoubleCellFailsLoud`, `testDoubleValueFailsLoud`,
  `testIntegerValueStoresByte` (positive pin). Local: **9/9 PASS**.
  Falsification (pre-fix GoByteCell): exactly the 3 negative tests fail.

## C2 — large-map directory-path probe

- New `test/differential/corpus/49_map_large/main.go` (+ README row) and
  `test/corpus/phase1/builtins/bi23_map_large.go` (+ `testBi23MapLarge`).
  Two maps (9 entries, just past one 8-slot bucket; 20 entries, past 17
  forcing 2+ tables / dirLen 4), each via native `range`, reflect
  `MapRange`, `maps.Keys`/`maps.Values`, `maps.Clone`/`Equal` (true, then
  false after mutation). All output sorted/order-independent.
- Local: **bi23 PASS via Differential** (gc vs graalgo byte-identical,
  12 lines); **49_map_large diff-identical** (12 lines); minimal probes
  p3/p8/p9/p9native/p20 all byte-identical to gc.

## The probe found a real defect (fixed here, not just papered over)

bi23 failed pre-fix: the large-map directory path was broken (threshold
pinned at exactly 9 entries; native range fine). Root cause, proven from
the wire JSON + fresh stack traces + the guest source
(`internal/runtime/maps`): `MapHeaderView` wraps each table's single
group in a 1-element `Object[]` (`groups.data`) and the directory in an
`Object[]` of table pointers (`dirPtr`), but three backend paths treated
those interned arrays as the denoted value instead of element 0:

1. `UnsafeToPtrNode` offset-0 conversions: `(*ctrlGroup)(g.data)`
   returned a pointer to the `Object[]` itself, so every ctrl read died
   (`GoStruct cannot be cast to Number` / `Object[] cannot be cast to
   GoStruct`). Fix: `arrayElementZero` (struct-element arrays resolve
   to the element's field-0 cell, the same rule as for struct roots)
   plus an `ArrayRef(array, 0)` fallback for pointer-element arrays
   (fixes `directoryAt(dirIdx == 0)`), both guarded to never fire for
   array-typed targets.
2. `ReflectLinknames.loadPointerValue`: `group.key(i)`/`group.elem(i)`
   pointers (ByteRef over the groups array) took the flat carrier-array
   path and died casting `Object[]` to `long[]`. Fix: route
   struct-array roots through `denotedValue` like struct roots.
3. `ReflectRefs.denotedValue`: ByteRef into an array holding a struct
   now resolves the byte offset as a struct offset into the addressed
   element (`readStructFieldAt(element, offset % structSize)`).

All three fixes are general (no package-name keying, no synthetic
stdlib, no Java reimplementation of stdlib behavior) and each fails
closed to the pre-existing loud behavior on unresolvable shapes.
`check_no_bridges.py` passes on the full branch delta.

## Regression evidence at this tip (local, fresh JVM per test)

- `BuiltinsCorpusTest` bi01–bi23: **23/23 PASS** (singles, -Xmx1024m),
  plus `testCorpusHasEnoughPrograms` PASS.
- `GoByteCellTest` 9/9, `MapGroupLayoutTest` 6/6.
- Falsification: p9/bi23 fail pre-fix (CCE, reproduced fresh) and pass
  post-fix; C1 negatives fail on the pre-fix `GoByteCell`.

## Follow-ups (not blockers for this tip)

- `reflect.mapaccess_faststr` is unbound: `reflect.MapIndex` on string
  keys fails identically on a 3-entry map (pre-existing gap, not a
  directory-path defect). `MapIndex` was trimmed from the C2 probe to
  the condition's stated surface (MapRange + Keys/Values + Clone/Equal);
  the linkname needs its own lane.
- Reviewer P2-1 (`DESCRIPTOR_WIRE_IDS` per-run-id defect) still needs
  its own design decision; it does not block this tip.

## Still owed

- CI: `workflow_dispatch` on the final tip SHA, verified from the API +
  downloaded job log (expect truffle 270/0 with bi23, runtime 234/0,
  interop 202/0; build-and-check SUCCESS, incl. the run.sh green run
  picking up 49_map_large).
- Landing-check: confirm CI green on the final SHA (new commits void the
  r63 tip's CI). No fresh full review needed per the CONDITIONAL APPROVE.
