package graalgo.p3reflect;

import graalgo.values.GoKind;

/**
 * The gc group layout for a map type (fix-redmaster-r59): the single
 * computation of the {@code internal/runtime/maps} group geometry, shared
 * by the {@code abi.MapType} shell fill ({@link AbiTypes}) and the
 * header-view image builder ({@link MapHeaderView}), so shell and image
 * can never disagree.
 *
 * <p>Layout is the INTERLEAVED one (gc's {@code MapGroupType} with
 * {@code goexperiment.MapSplitGroup} off — the default for this build:
 * {@code exp_mapsplitgroup_off.go} carries
 * {@code //go:build !goexperiment.mapsplitgroup} and the frontend build
 * tags are {@code purego} + GRAALGO_BUILD_TAGS only):
 * <pre>
 *   type group struct {
 *       ctrl  uint64
 *       slots [8]struct { key keyType; elem elemType }
 *   }
 * </pre>
 * Field names are gc's ({@code ctrl}, {@code slots}, {@code key},
 * {@code elem}; see {@code cmd/compile/internal/reflectdata/map.go}).
 * For indirect kinds (size &gt; 128) the slot field type is the POINTER
 * to the key/elem type (also gc's rule: {@code MapGroupType} replaces an
 * oversized key/elem type with a pointer to it), so the effective field
 * size/align is (8, 8) and the image slots hold heap pointers.
 *
 * <p>Scalars follow gc's {@code writeMapType} for the interleaved layout:
 * keysOff = slots field offset; keyStride = elemStride = slot size;
 * elemsOff = keysOff + elem offset in slot; elemOff = elem offset in
 * slot. Flags follow the gc size rule ({@code abi.MapIndirectKey} = 4,
 * {@code abi.MapIndirectElem} = 8; {@code abi.MapMaxKeyBytes} /
 * {@code abi.MapMaxElemBytes} = 128).
 */
public final class MapGroupLayout {
    private MapGroupLayout() {}

    /** abi.MapIndirectKey. */
    public static final int FLAG_INDIRECT_KEY = 4;
    /** abi.MapIndirectElem. */
    public static final int FLAG_INDIRECT_ELEM = 8;
    /** abi.MapMaxKeyBytes / abi.MapMaxElemBytes. */
    public static final int MAX_KEY_ELEM_BYTES = 128;
    /** abi.MapGroupSlots. */
    public static final int GROUP_SLOTS = 8;

    /** The computed geometry for one map type. */
    public record Layout(long groupSize, long keysOff, long keyStride, long elemsOff,
                         long elemStride, long elemOff, int flags,
                         boolean indirectKey, boolean indirectElem) {}

    /**
     * The interleaved group layout for a map with the given key/elem
     * types, or null when either size is unknown (the caller keeps the
     * V1 zero fill; the view builder then fails loudly if that map is
     * actually iterated, rather than serving a wrong header).
     */
    public static Layout of(ReflectTypes.TypeInfo key, ReflectTypes.TypeInfo elem) {
        if (key == null || elem == null || key.size < 0 || elem.size < 0) {
            return null;
        }
        boolean indirectKey = key.size > MAX_KEY_ELEM_BYTES;
        boolean indirectElem = elem.size > MAX_KEY_ELEM_BYTES;
        // Effective slot-field geometry: a pointer (8, 8) for indirect
        // kinds, the type itself otherwise (gc's MapGroupType rule).
        long keyFieldSize = indirectKey ? 8 : key.size;
        int keyFieldAlign = indirectKey ? 8 : Math.max(key.align, 1);
        long elemFieldSize = indirectElem ? 8 : elem.size;
        int elemFieldAlign = indirectElem ? 8 : Math.max(elem.align, 1);
        int slotAlign = Math.max(keyFieldAlign, elemFieldAlign);
        long elemOffInSlot = alignUp(keyFieldSize, elemFieldAlign);
        long slotSize = alignUp(elemOffInSlot + elemFieldSize, slotAlign);
        // Group: ctrl uint64 @0, slots [8]slot @alignUp(8, slotAlign).
        int groupAlign = Math.max(8, slotAlign);
        long slotsOff = alignUp(8, slotAlign);
        long groupSize = alignUp(slotsOff + (long) GROUP_SLOTS * slotSize, groupAlign);
        int flags = (indirectKey ? FLAG_INDIRECT_KEY : 0)
                | (indirectElem ? FLAG_INDIRECT_ELEM : 0);
        return new Layout(groupSize, slotsOff, slotSize, slotsOff + elemOffInSlot,
                slotSize, elemOffInSlot, flags, indirectKey, indirectElem);
    }

    private static long alignUp(long offset, long align) {
        return (offset + align - 1) / align * align;
    }

    /** Cache of synthesized group tokens, by (keyTok, elemTok). */
    private static final java.util.concurrent.ConcurrentHashMap<String, Long> GROUP_TOKS =
            new java.util.concurrent.ConcurrentHashMap<>();

    /**
     * The token of the synthesized group type for a map with the given
     * key/elem types (slot struct + [8]slot array + group struct, via
     * {@link ReflectTypes#structOf} / {@link ReflectTypes#arrayOf}).
     * The group type is unnamed (no wire entry): its descriptor typeName
     * is {@link ReflectTypes#typeString(long)} of this token, so the r43
     * {@code infoForStructString} fallback finds its TypeInfo. Cached;
     * synthesis is idempotent (structOf/arrayOf dedup by structural key).
     */
    public static long groupTokFor(ReflectTypes.TypeInfo key, ReflectTypes.TypeInfo elem) {
        String cacheKey = key.tok + ":" + elem.tok;
        Long hit = GROUP_TOKS.get(cacheKey);
        if (hit != null) {
            return hit;
        }
        Layout layout = of(key, elem);
        if (layout == null) {
            throw new IllegalStateException(
                    "reflect: cannot synthesize a group type for a map with unknown key/elem size");
        }
        // Slot field types: pointers for indirect kinds (gc's rule).
        long keyFieldTok = layout.indirectKey() ? ReflectTypes.ptrTo(key.tok) : key.tok;
        long elemFieldTok = layout.indirectElem() ? ReflectTypes.ptrTo(elem.tok) : elem.tok;
        long slotTok = ReflectTypes.structOf(new String[]{"key", "elem"},
                new long[]{keyFieldTok, elemFieldTok},
                new String[]{"", ""}, new String[]{"", ""},
                new boolean[]{false, false});
        long slotsTok = ReflectTypes.arrayOf(slotTok, GROUP_SLOTS);
        long groupTok = ReflectTypes.structOf(new String[]{"ctrl", "slots"},
                new long[]{ctrlGroupTok(), slotsTok},
                new String[]{"", ""}, new String[]{"", ""},
                new boolean[]{false, false});
        GROUP_TOKS.put(cacheKey, groupTok);
        return groupTok;
    }

    /** Token of the {@code ctrlGroup} (uint64) field type. */
    private static long ctrlGroupTok() {
        ReflectTypes.TypeInfo ctrl =
                ReflectTypes.infoForName("internal/runtime/maps.ctrlGroup");
        if (ctrl != null) {
            return ctrl.tok;
        }
        // The maps package's ctrlGroup type is not in the wire table
        // (e.g. a minimal program): its underlying type is uint64, whose
        // word the ctrl field is read as everywhere.
        return ReflectTypes.table().tokForKind(GoKind.UINT64);
    }
}
