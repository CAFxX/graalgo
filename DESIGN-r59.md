# r59 design note — remove the r53 mapIter bridge, make real reflect/map.go execute

Lane r59 (senior), stream fix-redmaster-fullsuite-a0df481b-r59, branch
`wt/f7467b85-7da6-4da0-b9ec-94631a60dafe/fix-redmaster-fullsuite-a0df481b-r59b`,
base a933e5bb (parent fcae54b8, grandparent a0df481b = red master).
Worktree /dev/shm/r59-senior (linked from /var/tmp/r59; both tmpfs — PUSH EARLY AND OFTEN,
a reboot wipes both).

## Why (REJECT d5c6b40f, ~23:52 CET 2026-11-16)

F1 BLOCKING: r53 adds `mapIterStart`/`mapIterNext` to `reflectIntrinsicNames`
(`internal/frontend/synth_reflect.go`), `export.go:9512` diverts calls to the REAL
reflect functions to the reflectop wire node, and `ReflectOpNode` reimplements them in
Java with guest code never executed — a package-name-keyed special case plus a Java
reimplementation of stdlib behavior, i.e. a bridge under the 2026-10-15 order,
load-bearing for the green CI (694/0 on a933e5bb, run 43955616498).
F2 (r54 delta) and F3 (r48/r51/r53 non-intercept portions) verified sound.
The reviewer's literally-stated remediation (header-view-only) was verified by r58
INSUFFICIENT: the full traversal must work, not just header reads.
Remediation: remove the intercept, fix the root cause properly so real
reflect/map.go executes, re-run CI on the new tip, re-request independent review.
Do NOT integrate, push-to-master, or merge a933e5bb.

## Guest code that must execute (Go 1.27.1, ~/tools/go/src — read, not assumed)

- `reflect/map.go:220-240`: `mapIterStart(t,m,it)` = `it.Init(t,m); it.Next()`;
  `mapIterNext(it)` = `it.Next()`. `reflect.Value.MapKeys` (246-278):
  `m := (*maps.Map)(mptr)` — an explicit unsafe.Pointer -> *maps.Map conversion,
  i.e. an `unsafetooptr` wire node -> `UnsafeToPtrNode`. **THE hook point** for the
  header view (r40 HeaderView precedent: materialize a GoStruct from the live value
  at the consumption site that knows the target type). `MapRange` uses the same
  conversion path. `maplen` already works via ReflectLinknames (r51).
- `internal/runtime/maps/table.go` `Iter` fields in order: key, elem (unsafe.Pointer),
  typ *abi.MapType, m *Map, entryOffset, dirOffset, clearSeq uint64, globalDepth uint8,
  dirIdx int, tab *table, group groupReference, entryIdx uint64.
  `Init`: typ set; nil/empty map returns early; else dirIdx 0 (or -1 + groupSmall.data
  when dirLen <= 0); m, entryOffset = rand(), dirOffset = rand(), globalDepth, dirIdx,
  group, clearSeq. `rand()` called twice in Init.
- `Map` fields: used uint64 (first), seed uintptr, dirPtr unsafe.Pointer, dirLen int,
  globalDepth uint8, globalShift uint8, writing uint8, tombstonePossible bool,
  clearSeq uint64.
- `table`: used uint16, capacity uint16, growthLeft uint16, localDepth uint8,
  index int (-1 = stale/not installed), groups groupsReference{data, lengthMask}.
- `groupReference{data}`: `ctrls()` = `(*ctrlGroup)(g.data)`;
  `key(typ,i)` = `g.data + typ.KeysOff + i*typ.KeyStride`;
  `elem(typ,i)` = `g.data + typ.ElemsOff + i*typ.ElemStride`;
  `groups.group(typ,i)` = `g.data + i*typ.GroupSize`;
  `Map.directoryAt(i)` = `*(**table)(m.dirPtr + 8*i)`.
  `ctrlGroup` is uint64; `get(i)` = `*(*ctrl)(unsafe.Add(unsafe.Pointer(g), i))`
  (little-endian byte); `matchFull()` etc. are pure uint64 bitset ops.
- `Next()` (table.go ~873-1210): small-map branch (dirIdx < 0) iterates entryIdx 0..7
  via `group.ctrls().get(k) & ctrlEmpty`, `group.key/elem`, `grown := m.dirLen > 0`
  (false for small maps — `getWithKey` NOT called); directory branch handles
  globalDepth growth, `directoryAt`, `tab.groups.group`, fast path per-slot +
  slow path via `ctrls().matchFull()` -> `bitset.first()` -> `sys.TrailingZeros64`,
  `grown := it.tab.index == -1` -> `grownKeyElem` -> `m.getWithKey` + `typ.Key.Equal`
  (only on grown/NaN path). **If image tables always have index >= 0, `getWithKey`
  is never called during iteration.** `it.m.writing` is read (image writing is
  always 0, so the `fatal` path is unreachable).
- `internal/abi/map.go` MapType fields: Key, Elem, Group *Type, Hasher, GroupSize,
  KeysOff, KeyStride, ElemsOff, ElemStride, ElemOff, Flags; flags MapNeedKeyUpdate /
  MapHashMightPanic / MapIndirectKey / MapIndirectElem; MapGroupSlots = 8,
  MapMaxKeyBytes/MapMaxElemBytes = 128.
- gc layout (`cmd/compile/internal/reflectdata/map.go` writeMapType): split layout
  (MapSplitGroup): keysOff = Group field 1 offset; keyStride = key size; elemsOff =
  Group field 2 offset; elemStride = elem size. Interleaved: keysOff = slots offset;
  keyStride = slot size; elemsOff = keysOff + elem offset in slot; elemStride = slot
  size. GroupSize = group type size. **Flags: IndirectKey iff Key.Size() > 128;
  IndirectElem iff Elem.Size() > 128** (purely size-based). Group type (split):
  `{ctrls ctrlGroup; keys [8]Key; elems [8]Elem}`; interleaved:
  `{ctrls; slots [8]struct{key; elem}}`. **Layout choice is self-consistent for this
  fix**: guest code always reads KeysOff/KeyStride/etc. from the MapType shell, never
  hardcodes layout — choose split layout, fill shell scalars for split, build the
  image descriptor for split. (MapSplitGroup default in Go 1.27 not yet confirmed —
  not blocking.)

## The MapType shell gap (must fix)

`AbiTypes.fillExtension` MAP case fills Key, Elem, Hasher (shared `MapKeyHash.func()`);
**Group/offsets/flags remain zero (V1)**, explicitly documented. `SelectorNode`
comment confirms `MapType.KeysOff` (field 6, UINT64) is "left zero by the shell fill".
So today `typ.KeysOff`/`KeyStride`/`GroupSize` are all 0 in the guest — `group.key`
would compute offset 0 for every slot. **Filling the scalars in `fillExtension`
(GroupSize, KeysOff, KeyStride, ElemsOff, ElemStride, ElemOff, Flags) from key/elem
TypeInfo sizes/alignments per gc's writeMapType rules is a required part of the
remediation.** Group (*Type) can stay nil: not read on the iteration path (only by
`typedmemmove(typ.Group,...)` in cloneGroup/rehash — document as known gap; fails
loudly if ever hit). MapType shell built by `AbiTypes.fillAbiStruct`/`fillExtension`
from `ReflectTypes.TypeInfo` (MAP TypeInfo carries key/elem toks, size 8, align 8;
ReflectTypes.java:952 and :1814 `mapOf`).

## GoMap wire-type-id stamping (required)

GoMap creation sites (production): `MakeNode.java:65` (make builtin),
`CompositeLitNode.java:128` (map literals), `ReflectOpNode.java:1488` `makeMap(tok)`
(reflect.MakeMap, tok in scope), `ReflectLinknames.java:153` `reflect.makemap`
(tok in scope via `tokForTypeArg(args[0])`). `GoSyncMap.java:28` (sync.Map internals
— encapsulated, never reflect-iterated; leave untyped, document). `ReflectOpNode.java:2029`
(nil-map placeholder in the r53 intercept — removed with the intercept). Tests only:
`ValuesTest.java`.
- **Correction to r58**: the wire "make" node DOES carry `"type"`
  (`buildMakeBuiltin` reads `f.intValue("type", path)`) but the map branch
  **discards it** (`return new MakeNode(-1)`). Fix: thread the typeId through —
  `setTypeId(typeId)` on the MakeNode (same pattern as composite literals), read it
  in `executeObject`, stamp the GoMap.
- **Correction to r58**: `CompositeLitNode` does NOT need a constructor/factory change
  — `NodeFactory` already calls `lit.setTypeId(typeId)` after construction, so
  `buildMap` can read `typeId()` directly.
- The stamp is the **wire type-table id** (int); the view builder converts via
  `ReflectTypes.tokForWireId(wireId)`. Untyped maps (wire id -1) must fail LOUDLY
  at view-build time.

## Existing machinery verified (reuse as-is)

- `UnsafeToPtrNode` (1294 lines, read): `structFieldAt` (byte offset landing exactly
  on a direct field -> GoFieldCell; r51 descent into struct-typed fields with
  remainder; uses `ReflectTypes.infoForName`/`infoForStructString` + field offsets;
  requires target tok != referent tok); `continuedFieldPointer` (tries
  `arrayElemPointer` then `arrayFieldPointer` at each prefix-parent level, depth 8);
  `arrayElemPointer` (offset into an ARRAY field -> `new GoPointer(arr, idx)`,
  idx = rel/elemSize, one-past-end allowed); `arrayFieldPointer` (offset exactly at
  an ARRAY field -> pointer to the array); `derivedPointer` (array roots only:
  `goArrayElemSize` + nested-array walk); header views (r40: `captureHeaderTarget`
  classifies by field TYPE alone — 2-3 fields at word offsets 0/8/16, field 0 data
  word, rest int words; `HeaderView.build` materializes from string/slice; read
  snapshot, N3 class); prefix views; interface views; ptrToPtr type-word cell.
  `build()` captures view/header/prefix/iface targets from the wire type table.
- `UnsafeTable` (471 lines, read): `intern` (identity; scalar boxes normalized to
  carrier arrays), `internEqual`, `handleForPointer`, `pointerFor`, `resolve`,
  `resolveView` (unwraps ByteRef layers -> (root, totalOffset)), `derive` (offset 0
  -> base handle unchanged; nil base + nonzero offset traps; interns
  `ByteRef(root, total)` by value equality). `ByteRef` base is never itself a ByteRef.
- `unsafe.Add` -> `UnsafeAddNode` -> `UnsafeTable.derive` (since r39, NOT a trap).
  `uintptr->unsafe.Pointer` with proven provenance -> `UnsafeDeriveNode` -> derive.
  Frontend prover `unsafe_derive.go` (read): `flattenAdditive` (+/- only; products
  are single terms), `uintptrConversionOfPointer`, `valuePointerCall`
  (reflect.Value.Pointer), offset terms must be pure integers with no hidden
  unsafe.Pointer. `exportUintptrToPointer` emits `unsafederive {base, offset, type}`.
- `DerefNode` (284 lines, read): GoPointer deref, offset-0 struct descent
  (`descendOffsetZero`), array-first-element peel for scalar kinds, interface
  reconstruction from eface-layout structs, Long decode via `Boxing.fromLongBits`,
  r53 word-read (`*(*unsafe.Pointer)(p)` on GoMap/GoChan/GoFunc/GoPointer ->
  `GoPointer.internHandle(o)`, nil -> 0). **No sub-word byte-read path** (gap (a)).
- `SelectorNode.readFieldPath`: per-step `deref` + `(GoStruct)` cast — the failure
  site for GoMap referents; kind-aware overload (r53) for MapType shells.
  `GoStructDescriptor.layout(...)` factory exists for building descriptors from field
  lists; `Field` record carries name/kind/inLongs/index/arrayLen/arrayElemKind/
  arrayElemDesc/structDesc (no byte offsets — offsets come from
  `ReflectTypes.FieldInfo`).
- `ReflectLinknames.loadPointerValue` (912+): Number handle -> `resolveView`;
  byteOffset 0 -> `loadOffsetZero` (denoted value + offset-0 prefix descent);
  byteOffset != 0 -> `ReflectValues.arrayGet(arrayRoot(view), t.kind,
  elementIndex(view, t, symbol))` where `arrayRoot` accepts ONLY ArrayRef/GoSlice/
  Java array and `elementIndex` = base + byteOffset/t.size. `storePointerValue`
  mirrors this. `typedmemmove(tok, dst, src)` = load + store. `mapLen` (r51)
  already handles GoMap.
- `Linknames` dispatch: Java intrinsic registered under a symbol wins, else a Go func
  bound under the qualified name, else fail-fast FrontendException.
  `LinknameIntrinsics.java` registers `runtime.fastrand`/`runtime.fastrandn`/
  `runtime.rand` -> `Intrinsics.fastrand64()` etc. `Intrinsics.fastrand64()` exists
  (runtime/Intrinsics.java:631).
- `ReflectTypes.structDescriptorFor(qname)` (2272) scans the cached wire TypeTable for
  a "named" entry — synthesized types have no wire entry, so the group image
  descriptor must be built in Java (via `GoStructDescriptor.layout`) and its typeName
  must resolve via `infoForName`/`infoForStructString` (ReflectTypes.java:247/273;
  the r43 precedent: unnamed structs resolve by structural rendering).
- `sys.TrailingZeros64`/`sys.OnesCount64` are pure Go (`internal/runtime/sys/intrinsics.go`)
  — should compile; verify empirically (math/bits backend). `goarch.*` are constants.
  `abi.MapGroupSlots` (=8) constant.
- `GoStruct.zero(descriptor)`, `GoStructDescriptor.layout(typeName, names, kinds, ...)`
  exist. `GoPointer` constructors: referent / GoCell / (array, index); `referent()`,
  `setReferent`, `isCellPointer`, `isElemPointer`, `internHandle`, `resolveHandle`,
  `unwrapStorage` hooks exist.

## Machinery gaps (the implementation work)

- **(a) Ctrl-byte read — NEW machinery required.** `ctrlGroup.get(k)` =
  `*(*ctrl)(unsafe.Add(unsafe.Pointer(g), k))`: `(*ctrlGroup)(g.data)` resolves via
  `structFieldAt(groupImage, 0)` to a GoFieldCell over the ctrls uint64 field;
  `unsafe.Add` derives ByteRef(cell, k); `(*ctrl)` -> UnsafeToPtrNode: `structFieldAt`
  unwraps the cell to a Long (not a GoStruct) -> null; `continuedFieldPointer` ->
  null; `derivedPointer` -> not an array -> TRAP. Needed: a GoPointer form carrying
  (longs-field cell, byte offset) + DerefNode support reading byte k of the long
  (shift/mask by kind, little-endian). DerefWriteNode: read-only for iteration
  (image is a snapshot; nothing writes ctrls during Next) — the new form must fail
  LOUDLY on write, never silently corrupt.
- **(b) `loadPointerValue`/`storePointerValue` GoStruct-root extension — required.**
  copyVal's typedmemmove reads key/elem pointers that are ByteRef handles into the
  group image (a GoStruct root); `arrayRoot` currently throws for non-array roots.
  Needed: in the byteOffset != 0 branch, when the root is a GoStruct, resolve via a
  new static resolver in UnsafeToPtrNode (extract `structFieldAt`/
  `arrayElemPointer`/`continuedFieldPointer` logic parameterized by targetTok/
  elemSize) -> GoPointer -> return `p.referent()`. If resolution returns null, fall
  through to the existing array path (preserving the loud failure). Direct-iface
  keys (e.g. int) go through `*(*unsafe.Pointer)(ptr)` -> UnsafeToPtrNode ->
  `arrayElemPointer` (works as-is, no new machinery). Non-direct keys (e.g. string,
  2 words) go through typedmemmove -> this extension.
- **(c) Directory path — `derivedPointer` struct-array extension — required.**
  `groups.group(typ, i)` yields ByteRef(groupsArray, i*GroupSize); re-derivation in
  `group.key/elem` yields ByteRef(groupsArray, i*GroupSize + within). Consumption
  must: divide by GroupSize -> group index, take remainder -> resolve within the
  group struct. Needed: `goArrayElemSize` returns the struct size for uniform
  GoStruct-element arrays (via `ReflectTypes.infoForName(descriptor.typeName()).size`
  or descriptor-derived size), and `derivedPointer`'s nested walk descends into a
  GoStruct element with the remainder via struct-field/array resolution (currently
  it traps on non-array elements). Alternative considered and rejected: a GoStruct
  container with one field per group (descriptor explosion per group count).
- **(d) `maps.rand` linkname — registration required.** `internal/runtime/maps/runtime.go`
  declares `//go:linkname rand` (single-field) -> exporter symbol is
  `pkgPath + "." + local` = `"internal/runtime/maps.rand"` (export.go:6166
  `linknameDirective`, verified). `Linknames` has `"runtime.rand"` registered — a
  DIFFERENT symbol. `maps.rand()` is called twice in `Init` (entryOffset,
  dirOffset), so dispatch would fail fast without registration. Fix: register
  `"internal/runtime/maps.rand"` -> `Intrinsics.fastrand64()` in LinknameIntrinsics.
  Other maps linknames (`fatal`, `bootstrapRand`, `typedmemmove`, `typedmemclr`,
  `newarray`, `newobject`) are NOT on the iteration path (fatal only when writing != 0;
  typedmemmove/clr/newarray/newobject only in clone/rehash/growth/Clear) — register
  only rand; note fatal as follow-up if a path needs it.
- **(e) Group image descriptor — synthesized.** The group type (`typ.Group`) has no
  wire-table entry (go/types has no Group; it is compiler-synthesized in gc's
  reflectdata). The image must be a GoStruct whose descriptor mirrors the gc split
  layout (fields ctrls uint64 @0, keys [8]K @8, elems [8]V @8+8*keySize) built via
  `GoStructDescriptor.layout`, with a typeName resolvable by `infoForName`/
  `infoForStructString` (structural rendering precedent, r43) and a synthesized
  TypeInfo registered in ReflectTypes.table(). Image ctrl bytes need only the empty
  bit: 0x00 = full, 0x80 = empty; h2 bits unused by iteration (only by matchH2/
  lookups, never called). Snapshot semantics (r53 precedent, documented like
  HeaderView's N3): writes land in the snapshot, not the live GoMap; a live clear
  mid-iteration is not visible (acceptable — unspecified under concurrent mutation).
- **(f) Indirect keys/elems**: Flags by gc rule (size > 128). Image keys/elems arrays
  then hold GoPointers to heap values; `it.typ.IndirectKey()` reads Flags from the
  shell; `key = *((*unsafe.Pointer)(key))` derefs via DerefNode (works). Zero-size
  keys: KeyStride 0, all slots resolve to element 0 — correct (all zero values
  identical).

## Layering (r58 decision, kept)

Truffle frontend cannot import interop (build order runtime -> truffle -> interop;
`ReflectTypes.install` called only by interop launchers `P3SyncMain`/
`P3KindRegistrations`). The `SelectorNode` hook needs a provider-registry pattern:
a small interface in `graalgo.values`, implementation in interop (p3reflect already
imports p3unsafe, no new cross-package dep), registered at the `ReflectTypes.install`
choke point so every launcher path covers it. SelectorNode asks the provider when
the referent is a GoMap; untyped maps fail loudly.

## Implementation order

1. GoMap wire-type-id field + stamp at MakeNode (thread wire "type" via
   `setTypeId`), CompositeLitNode (existing `typeId()`), ReflectOpNode.makeMap,
   ReflectLinknames makemap. AbiTypes.fillExtension MAP case: fill GroupSize/
   KeysOff/KeyStride/ElemsOff/ElemStride/ElemOff/Flags per gc rules (split layout).
   Register `internal/runtime/maps.rand` -> `Intrinsics.fastrand64()`.
2. MapHeaderView builder (new, interop) + UnsafeToPtrNode hook for target
   `*maps.Map` with a GoMap referent (capture target from wire by qualified name
   `internal/runtime/maps.Map`; build snapshot image: header GoStruct with
   maps.Map descriptor, directory image, group images).
3. Gap (a) byte-of-long read; gap (b) GoStruct-root load/store; gap (c)
   derivedPointer struct-array extension. Unit tests (image/header/ctrl-word) +
   bi21 as end-to-end signal.
4. Remove the r53 intercept ONLY after the replacement keeps bi21 green: names out
   of `reflectIntrinsicNames`, `isReflectIntrinsicFunc` back to `__reflect_*` only,
   ReflectOpNode mapIter ops + RealIterStates removed (ReflectOpNode.java ~1780-2050:
   `reflectMapIterStart`/`reflectMapIterNext`/`iterStruct`/`iterMap`/`iterAdvance`/
   `RealIterStates`, dispatch cases at :468/:476-482). Run `check_no_bridges.py` after
   .go commits.
5. Push new tip via Git Data API route (helpers in ~/workspace/system/github-sync/graalgo/;
   `custom.github-pat` connector doc is the reference; plain `git push github` FAILS),
   trigger CI `workflow_dispatch` on the exact new SHA, verify both legs + per-test
   counts from the job log (r57 `gh_r57.py` 302-without-auth pattern; job-log downloads
   via gh-api-get FAIL 401 on signed redirect).
6. Final report: REJECT findings quoted, change per finding, new hash, CI evidence on
   the exact SHA, independent-review request. Then `wt.py integrate` or `wt.py abandon`
   per the Charter if quota allows; mark the registry entry done; append the outcome
   to `~/memory/2026-11-17.md` (owed).

## bi21 (end-to-end signal)

`test/corpus/phase1/builtins/bi21_nested_maps.go` — `fmt.Println` of nested maps
(2- and 3-level, mixed types, deferred print). Path: fmt map printing -> fmtsort.Sort
-> `reflect.Value.MapKeys` -> mapIterStart/Next. All bi21 maps are small (1-3 entries)
but the full suite also prints larger maps via fmt, so the directory path is required
for full-suite green, not just small maps. Range-over-map uses a separate
`mapiterinit`/`mapiternext` Intrinsics path (already working) — NOT affected by
intercept removal.

## Open checks (verify empirically, do not assume)

- `sys.TrailingZeros64`/`sys.OnesCount64` resolve in graalgo (math/bits backend).
- `MapSplitGroup` default in Go 1.27 (layout choice is self-consistent regardless).
- `MapRange` conversion path (same as MapKeys — confirm when reading it).
- `v.pointer()` shape (for the conversion hook).
- Whether the maps package's types are in the wire table of the bi21 program
  (structDescriptorFor("internal/runtime/maps.Map") resolves).
