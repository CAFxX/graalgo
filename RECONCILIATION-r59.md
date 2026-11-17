# r59 reconciliation — r58 design vs watch-r59 DESIGN.md

Date: 2026-11-17 ~01:45 UTC. Lane r59 (senior, coordinator-dispatched).
Base: r54 tip a933e5bb (CI-green, REJECT d5c6b40f F1). Working branch:
`wt/f7467b85-7da6-4da0-b9ec-94631a60dafe/fix-redmaster-fullsuite-a0df481b-r59`
in /var/tmp/r59. The r59b branch (`...-r59b`, DESIGN-r59.md, commit 3ea5348d,
pushed) is SUPERSEDED for implementation; its content is merged below.
The watch-r59 DESIGN.md lives at `r59-evidence/DESIGN.md` (6,372 B).

Two designs were derived independently from disk truth and agree on the
architecture (real reflect/map.go executes; GoMap header view at the
`(*maps.Map)` conversion; MapType shell fill; maps.rand binding; intercept
removed). They differ per piece; the better-verified mechanism wins below.
"r58" = the senior design (DESIGN-r59.md on r59b). "D2" = the watch-r59
design (r59-evidence/DESIGN.md).

## Decisions

1. **Group layout: INTERLEAVED (D2).** Verified from disk truth:
   `~/tools/go/src/internal/goexperiment/exp_mapsplitgroup_off.go` carries
   `//go:build !goexperiment.mapsplitgroup` and `const MapSplitGroup = false`;
   the frontend build tags are `purego` + GRAALGO_BUILD_TAGS only
   (diskimport.go:294) — no mapsplitgroup tag. So gc generates the
   interleaved layout (slots `[8]struct{key; elem}`) for this build.
   The layout choice is self-consistent either way (guest code reads
   layout only from the MapType shell, never hardcodes it), but matching
   the gc default removes a whole class of reviewer doubt. Group struct:
   `{ctrls ctrlGroup; slots [8]slot}`; slot struct: `{key K; elem V}`.
   Shell scalars (interleaved, gc writeMapType): keysOff = slots field
   offset (= 8); keyStride = slotSize; elemsOff = keysOff + elem offset
   in slot; elemStride = slotSize; elemOff = elem offset in slot.
2. **One shared layout helper (D2 step 2).** A single computation from
   (key TypeInfo, elem TypeInfo) producing {groupSize, keysOff, keyStride,
   elemsOff, elemStride, elemOff, flags, indirectKey, indirectElem}, used
   by BOTH AbiTypes.fillExtension AND the group-image builder, so shell
   and image can never disagree.
3. **GoMap stamp: wire type-table id (int) (r58).** A plain field
   (`wireTypeId`, -1 = untyped), stamped at creation (MakeNode via the
   wire "type" threaded through `setTypeId`; CompositeLitNode via its
   existing `typeId()`; ReflectOpNode.makeMap / ReflectLinknames.makemap
   via a new `ReflectTypes.wireIdForTok` reverse lookup). Rejects D2's
   values-package hook (GoMap.mapToksForWireId registered by ReflectTypes):
   the int field has no registration-ordering hazard (stamped at
   construction, always) and fewer moving parts. No version/clearSeq on
   GoMap (rejects D2 step 1): the view is a FRESH snapshot per conversion
   (one conversion per MapKeys/MapRange call, so caching saves nothing),
   and clearSeq is never read on the iteration path (the grown path, the
   only clearSeq reader, never fires for images: small maps have dirLen 0,
   image tables always have index >= 0). Header clearSeq = 0, documented.
4. **Header-view firing: LAYOUT-classified target (D2 step 3).** At the
   `(*maps.Map)` conversion (UnsafeToPtrNode), the target fires iff it is
   a named struct with exactly 9 fields at offsets 0,8,16,24,32,33,34,35,40
   with classes uint,uint,data,int,uint,uint,uint,bool,uint — type-directed
   only (r40 HeaderView precedent), no package-name predicate. Rejects
   r58's qualified-name capture as less general. The materialized header
   GoStruct's descriptor typeName is the target's qualified name, so
   SelectorNode resolves fields via the real maps.Map TypeInfo. Nil
   referent -> zero header (Init early-returns on nil m).
5. **Group TypeInfo: SYNTHESIZED via structOf/arrayOf (D2 step 4).**
   slotTok = structOf([key, elem]); slotsTok = arrayOf(slotTok, 8);
   groupTok = structOf([ctrls, slots]). structOf computes gc-exact field
   offsets (alignUp walk). Descriptor typeName = typeString(groupTok) so
   the r43 infoForStructString fallback finds the TypeInfo. This lets the
   EXISTING TypeInfo-based machinery (structFieldAt, arrayElemPointer,
   readStructFieldAt) work on group images. Rejects r58's "no TypeInfo,
   descriptor-based offsets" (more new code for the same result). The
   MapType shell's Group field is FILLED from the synthesized group
   TypeInfo (D2 step 2) — not left nil as r58 planned.
6. **Indirect keys/elems: SUPPORTED (r58).** Flags by the gc size rule
   (> 128 bytes); image slots hold GoPointers to heap values for indirect
   kinds; Next's `*(*unsafe.Pointer)(key)` deref works via DerefNode.
   Rejects D2's direct-only V1 limitation as an unnecessary scope cut:
   the mechanism is identical, and a >128-byte-key test would fail loudly
   under the cut but pass under support. Test if time permits.
7. **Ctrl-byte read: GoByteCell (D2 5e).** A GoCell over a GoFieldCell +
   byte index (get extracts the byte, set merges it back), minted by
   UnsafeToPtrNode when a ByteRef names a byte inside a Long-valued field
   cell. Read+write flow through the EXISTING GoCell machinery — NO
   DerefNode/DerefWriteNode change. Plus the same extraction in
   readStructFieldAt's non-aggregate-field branch (today throws). Replaces
   r58 gap (a) (new GoPointer form + DerefNode branch + DerefWriteNode
   loud-fail): D2 is less invasive and supports writes.
8. **Offset machinery: D2 5a–5f PLUS r58 gap (c).** Adopt D2 5a
   (structFieldAt array-of-struct descent), 5b (same in
   readStructFieldAt), 5c (denotedValue ByteRef-over-GoStruct for ALL
   kinds), 5d (loadPointerValue ByteRef-over-GoStruct via denotedValue),
   5f (readStructFieldAt infoForStructString fallback). D2's 5a–5f cover
   the SMALL-map path (root = group struct). The DIRECTORY path needs one
   more piece D2 does not list: r58 gap (c) — derivedPointer's walk must
   divide by the GROUP size for Object[]-of-GoStruct roots (goArrayElemSize
   struct case via Intrinsics.structSizeBytes) and descend into a GoStruct
   element with the remainder. Framed as ONE shared resolver used by both
   derivedPointer and loadPointerValue (r58 gap (b) formulation). dirPtr
   shape is UNIFORM (r58): always the groups array, for small and large
   maps alike — one mechanism; rejects D2's small-map direct-group dirPtr
   (gap (c) is needed for large maps regardless).
9. **Shell fill (AbiTypes.fillExtension MAP):** GroupSize, KeysOff,
   KeyStride, ElemsOff, ElemStride, ElemOff, Flags (IndirectKey/
   IndirectElem by size rule) + Group (from 5), via the shared helper (2).
   NeedKeyUpdate/HashMightPanic left zero, documented: not read on any
   path graalgo executes through the shell (clone/rehash never run on
   images; PutSlot runs on live GoMaps via GoMap.put, not maps.PutSlot).
10. **maps.rand: register `internal/runtime/maps.rand` ->
    Intrinsics.fastrand64() in LinknameIntrinsics (both designs agree).**
    Only rand: other maps linknames are not on the iteration path.
11. **Intercept removal: DONE by watch-r59 (3 files, +10/-241).**
    Verified complete: export.go's diversion is gated on
    isReflectIntrinsicFunc (now false for mapIterStart/mapIterNext); no
    capital-I mapIter refs remain; the lowercase `__reflect_mapiter*`
    range-over-map path is untouched and stays. DerefNode's r53 word-read
    stays (F3 sound). Repro confirmed by watch-r59: bi21 fails
    `GoMap cannot be cast to GoStruct`, rc=1.

## Implementation order

1. GoMap stamp + wireIdForTok + shared layout helper + AbiTypes MAP fill
   (incl. Group) + maps.rand registration + synthesized group TypeInfo.
2. MapHeaderView + layout-classified hook + GoByteCell.
3. Offset machinery 5a-5f + gap (c) shared resolver.
4. Unit tests (image/header/ctrl-byte) + bi21 (small maps) + a large-map
   test (>8 entries via fmt, directory path) as the end-to-end signals.
5. Push new tip via the Git Data API route (push_repo.py, warm state
   file in .git/ — the cold-start full-closure push takes hours; see
   step 1 of the r59b lane), trigger CI workflow_dispatch on the exact
   new SHA, verify both legs + per-test counts from the job log.
6. Final report: REJECT findings quoted, change per finding, new hash,
   CI evidence on the exact SHA, independent-review request. Then
   `wt.py integrate` or `wt.py abandon` per the Charter if quota allows;
   mark the registry entry done; append the outcome to
   ~/memory/2026-11-17.md (owed).

## Notes

- bi21 (`test/corpus/phase1/builtins/bi21_nested_maps.go`): fmt map
  printing -> fmtsort.Sort -> reflect.Value.MapKeys -> mapIterStart/Next.
  All bi21 maps are small (1-3 entries); the directory path is required
  for full-suite green, not just small maps. Range-over-map uses the
  separate `mapiterinit`/`mapiternext` Intrinsics path (already working).
- Snapshot semantics (r53 precedent, HeaderView N3 class): writes land
  in the snapshot, not the live GoMap; a live clear mid-iteration is not
  visible (acceptable — unspecified under concurrent mutation).
- Image ctrl bytes need only the empty bit: 0x00 = full, 0x80 = empty;
  h2 bits unused by iteration (only by matchH2/lookups, never called).
- `sys.TrailingZeros64`/`sys.OnesCount64` are pure Go
  (internal/runtime/sys/intrinsics.go) — verify empirically via tests.
- `Key.Equal` is called ONLY on the grown path (table.go ~913), which
  never fires for images; AbiTypes fills a real Equal func value anyway.
