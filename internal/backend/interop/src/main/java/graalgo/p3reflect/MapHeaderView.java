package graalgo.p3reflect;

import graalgo.p3unsafe.UnsafeTable;
import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;
import java.util.ArrayList;
import java.util.List;

/**
 * The {@code internal/runtime/maps.Map} header view over a live
 * {@link GoMap} (fix-redmaster-r59): the materialization that lets real
 * guest code ({@code reflect/map.go}, {@code internal/runtime/maps.Iter})
 * read a GoMap as a maps.Map header. The guest converts a map pointer
 * with {@code (*maps.Map)(mptr)}; the conversion site (UnsafeToPtrNode,
 * layout-classified target) returns a pointer to the GoStruct built
 * here, and the guest's own field reads and unsafe arithmetic then run
 * unmodified.
 *
 * <p>Shape (faithful to the guest's own model):
 * <ul>
 *   <li>Small maps (size &le; 8): {@code dirLen = 0}, {@code dirPtr} is
 *       the handle of the single group struct itself — exactly as
 *       {@code Iter.Init} expects ({@code groupSmall.data = m.dirPtr}).
 *   <li>Large maps: a directory of length {@code dirLen} (a power of
 *       two), each entry a {@code *table}; each table holds one group
 *       (capacity 8, {@code localDepth == globalDepth == log2(dirLen)},
 *       {@code index} = its directory position). The directory is
 *       padded with empty tables when the group count is not a power
 *       of two — never with duplicate table pointers (with
 *       {@code localDepth == globalDepth} the guest's
 *       {@code nextDirIdx} advances one entry per table and would
 *       re-visit a duplicate).
 *   <li>Group images use the interleaved layout
 *       ({@code {ctrl uint64; slots [8]struct{key; elem}}}) from the
 *       shared {@link MapGroupLayout} helper — the same scalars the
 *       abi.MapType shell carries, so shell and image agree.
 *   <li>Ctrl bytes carry only the empty bit: 0x00 = full, 0x80 = empty
 *       (h2 bits are unused by iteration). Indirect slots (key/elem
 *       size &gt; 128) hold heap pointers, per gc's group-type rule.
 * </ul>
 *
 * <p>Snapshot semantics (the r53 precedent, HeaderView's N3 class): the
 * header, tables, and groups are built once per conversion from the
 * live map's entries. Writes land in the snapshot, not the live GoMap;
 * a live clear mid-iteration is not visible (acceptable — unspecified
 * under concurrent mutation). Indirect slot values share the live
 * objects (as in gc, where the iterator points into the live table).
 * Small-vs-large is decided by current size, not growth history: the
 * guest cannot observe the difference (iteration order is unspecified
 * either way).
 */
public final class MapHeaderView {
    private MapHeaderView() {}

    /**
     * Builds the header GoStruct for a live map. Fails loudly (never a
     * silent wrong header) when the map is untyped, its key/elem sizes
     * are unknown, or a needed wire type is absent.
     */
    public static GoStruct build(GoMap map) {
        int wireId = map.wireTypeId();
        if (wireId < 0) {
            throw new IllegalStateException("reflect: cannot build a maps.Map header view "
                    + "for an untyped map (no wire type id; fix-redmaster-r59)");
        }
        long tok = ReflectTypes.tokForWireId(wireId);
        if (tok < 0) {
            throw new IllegalStateException("reflect: cannot build a maps.Map header view: "
                    + "wire type id " + wireId + " has no runtime token");
        }
        ReflectTypes.TypeInfo mapInfo = ReflectTypes.table().info(tok);
        if (mapInfo.kind != GoKind.MAP || mapInfo.key == null || mapInfo.elem == null) {
            throw new IllegalStateException("reflect: cannot build a maps.Map header view: "
                    + "token " + tok + " is not a map type");
        }
        MapGroupLayout.Layout layout = MapGroupLayout.of(mapInfo.key, mapInfo.elem);
        if (layout == null) {
            throw new IllegalStateException("reflect: cannot build a maps.Map header view: "
                    + "unknown key/elem size for map type " + mapInfo.name);
        }
        long groupTok = MapGroupLayout.groupTokFor(mapInfo.key, mapInfo.elem);
        ReflectTypes.TypeInfo groupInfo = ReflectTypes.table().info(groupTok);
        ReflectTypes.TypeInfo slotInfo = groupInfo.fields.get(1).type.elem;
        GoStructDescriptor groupDesc = descriptorFor(groupInfo);
        GoStructDescriptor slotDesc = descriptorFor(slotInfo);
        // Slot field tokens (pointer toks for indirect kinds, per gc).
        long keyFieldTok = layout.indirectKey()
                ? ReflectTypes.ptrTo(mapInfo.key.tok) : mapInfo.key.tok;
        long elemFieldTok = layout.indirectElem()
                ? ReflectTypes.ptrTo(mapInfo.elem.tok) : mapInfo.elem.tok;

        GoMap.Entry[] entries = map.entriesSnapshot();
        int n = entries.length;
        int nGroups = Math.max(1, (n + MapGroupLayout.GROUP_SLOTS - 1) / MapGroupLayout.GROUP_SLOTS);
        GoStruct[] groups = new GoStruct[nGroups];
        for (int g = 0; g < nGroups; g++) {
            groups[g] = buildGroup(entries, g * MapGroupLayout.GROUP_SLOTS,
                    slotDesc, groupDesc, keyFieldTok, elemFieldTok, layout);
        }

        GoStructDescriptor headerDesc =
                ReflectTypes.structDescriptorFor("internal/runtime/maps.Map");
        GoStruct header = new GoStruct(headerDesc,
                new long[headerDesc.longsCount()], new Object[headerDesc.objectsCount()]);
        if (n <= MapGroupLayout.GROUP_SLOTS) {
            // Small map: dirPtr IS the single group (Iter.Init's
            // groupSmall.data = m.dirPtr).
            setField(headerDesc, header, "used", Long.valueOf(n));
            setField(headerDesc, header, "seed", Long.valueOf(0));
            setField(headerDesc, header, "dirPtr",
                    Long.valueOf(UnsafeTable.current().intern(groups[0])));
            setField(headerDesc, header, "dirLen", Long.valueOf(0));
            setField(headerDesc, header, "globalDepth", Long.valueOf(0));
            setField(headerDesc, header, "globalShift", Long.valueOf(0));
            setField(headerDesc, header, "writing", Long.valueOf(0));
            setField(headerDesc, header, "tombstonePossible", Boolean.FALSE);
            setField(headerDesc, header, "clearSeq", Long.valueOf(0));
            return header;
        }
        // Large map: directory of dirLen (power of two) tables.
        int dirLen = 1;
        while (dirLen < nGroups) {
            dirLen *= 2;
        }
        int globalDepth = 31 - Integer.numberOfLeadingZeros(dirLen);
        Object[] dirArray = new Object[dirLen];
        GoStructDescriptor tableDesc =
                ReflectTypes.structDescriptorFor("internal/runtime/maps.table");
        GoStructDescriptor groupsRefDesc =
                ReflectTypes.structDescriptorFor("internal/runtime/maps.groupsReference");
        for (int d = 0; d < dirLen; d++) {
            GoStruct table = new GoStruct(tableDesc,
                    new long[tableDesc.longsCount()], new Object[tableDesc.objectsCount()]);
            GoStruct groupsRef = new GoStruct(groupsRefDesc,
                    new long[groupsRefDesc.longsCount()],
                    new Object[groupsRefDesc.objectsCount()]);
            if (d < nGroups) {
                int used = 0;
                for (int s = 0; s < MapGroupLayout.GROUP_SLOTS; s++) {
                    if (d * MapGroupLayout.GROUP_SLOTS + s < n) {
                        used++;
                    }
                }
                setField(groupsRefDesc, groupsRef, "data",
                        Long.valueOf(UnsafeTable.current().intern(new Object[]{groups[d]})));
                setField(groupsRefDesc, groupsRef, "lengthMask", Long.valueOf(0));
                setField(tableDesc, table, "used", Long.valueOf(used));
                setField(tableDesc, table, "capacity", Long.valueOf(8));
                setField(tableDesc, table, "growthLeft", Long.valueOf(0));
                setField(tableDesc, table, "localDepth", Long.valueOf(globalDepth));
                setField(tableDesc, table, "index", Long.valueOf(d));
                setField(tableDesc, table, "groups", groupsRef);
            } else {
                // Padding: an empty table at its own position (never a
                // duplicate pointer — see the class javadoc).
                setField(groupsRefDesc, groupsRef, "data",
                        Long.valueOf(UnsafeTable.current().intern(new Object[]{
                                buildGroup(entries, n, slotDesc, groupDesc,
                                        keyFieldTok, elemFieldTok, layout)})));
                setField(groupsRefDesc, groupsRef, "lengthMask", Long.valueOf(0));
                setField(tableDesc, table, "used", Long.valueOf(0));
                setField(tableDesc, table, "capacity", Long.valueOf(8));
                setField(tableDesc, table, "growthLeft", Long.valueOf(0));
                setField(tableDesc, table, "localDepth", Long.valueOf(globalDepth));
                setField(tableDesc, table, "index", Long.valueOf(d));
                setField(tableDesc, table, "groups", groupsRef);
            }
            dirArray[d] = new GoPointer(table);
        }
        setField(headerDesc, header, "used", Long.valueOf(n));
        setField(headerDesc, header, "seed", Long.valueOf(0));
        setField(headerDesc, header, "dirPtr",
                Long.valueOf(UnsafeTable.current().intern(dirArray)));
        setField(headerDesc, header, "dirLen", Long.valueOf(dirLen));
        setField(headerDesc, header, "globalDepth", Long.valueOf(globalDepth));
        setField(headerDesc, header, "globalShift", Long.valueOf(0));
        setField(headerDesc, header, "writing", Long.valueOf(0));
        setField(headerDesc, header, "tombstonePossible", Boolean.FALSE);
        setField(headerDesc, header, "clearSeq", Long.valueOf(0));
        return header;
    }

    /** One group image: 8 slots from entries[start..start+8), empty past the end. */
    private static GoStruct buildGroup(GoMap.Entry[] entries, int start,
            GoStructDescriptor slotDesc, GoStructDescriptor groupDesc,
            long keyFieldTok, long elemFieldTok, MapGroupLayout.Layout layout) {
        Object[] slots = new Object[MapGroupLayout.GROUP_SLOTS];
        long ctrls = 0;
        for (int s = 0; s < MapGroupLayout.GROUP_SLOTS; s++) {
            int idx = start + s;
            GoStruct slot = new GoStruct(slotDesc,
                    new long[slotDesc.longsCount()], new Object[slotDesc.objectsCount()]);
            if (idx < entries.length) {
                GoMap.Entry e = entries[idx];
                setField(slotDesc, slot, "key", indirectValue(e.key(), layout.indirectKey()));
                setField(slotDesc, slot, "elem", indirectValue(e.value(), layout.indirectElem()));
            } else {
                // Empty slot: zero values (never read — ctrls says
                // empty — but the image must be well-formed).
                setField(slotDesc, slot, "key", ReflectOpNode.zeroFor(keyFieldTok));
                setField(slotDesc, slot, "elem", ReflectOpNode.zeroFor(elemFieldTok));
                ctrls |= (0x80L << (8 * s));
            }
            slots[s] = slot;
        }
        GoStruct group = new GoStruct(groupDesc,
                new long[groupDesc.longsCount()], new Object[groupDesc.objectsCount()]);
        setField(groupDesc, group, "ctrl", Long.valueOf(ctrls));
        setField(groupDesc, group, "slots", slots);
        return group;
    }

    /** Wraps an indirect slot value in a heap pointer; direct values pass through. */
    private static Object indirectValue(Object v, boolean indirect) {
        return indirect ? new GoPointer(v) : v;
    }

    /**
     * Sets a struct field by name, through its carrier (longs vs objects).
     * The accessors take FIELD ordinals and translate to carrier indices
     * internally (fix-redmaster-r59): passing {@code f.index()} — the
     * carrier index — addresses the wrong field whenever a longs field
     * and an objects field share a carrier index (e.g. the group
     * struct's {@code ctrl} and {@code slots} are both carrier index 0,
     * so the slots write landed on ctrl and died with "field ctrl is
     * not an objects-carried field").
     */
    private static void setField(GoStructDescriptor desc, GoStruct gs, String name, Object value) {
        int ord = fieldOrdinal(desc, name);
        GoStructDescriptor.Field f = desc.field(ord);
        if (f.inLongs()) {
            gs.setLong(ord, toLongBits(f.kind(), value));
        } else {
            gs.setObject(ord, value);
        }
    }

    /**
     * Encodes a canonical boxed scalar as the long bits a longs-carried
     * field holds (mirrors {@code Boxing.toLongBits}, package-private
     * to {@code graalgo.frontend}): floats by bit pattern, bools by
     * 0/1, everything else by {@link Number#longValue}.
     */
    private static long toLongBits(GoKind kind, Object boxed) {
        return switch (kind) {
            case FLOAT32 -> (long) Float.floatToIntBits((Float) boxed);
            case FLOAT64 -> Double.doubleToLongBits((Double) boxed);
            case BOOL, UNTYPED_BOOL -> ((Boolean) boxed) ? 1L : 0L;
            default -> ((Number) boxed).longValue();
        };
    }

    /** Field ordinal by name (fails loudly on a missing field). */
    private static int fieldOrdinal(GoStructDescriptor desc, String name) {
        for (int i = 0; i < desc.fields().size(); i++) {
            if (desc.field(i).name().equals(name)) {
                return i;
            }
        }
        throw new IllegalStateException(
                "reflect: struct " + desc.typeName() + " has no field " + name);
    }

    /**
     * The GoStructDescriptor for a TypeInfo, built from its field list
     * (recursively for nested structs and struct array elements). The
     * typeName is the qualified name for named types, the structural
     * rendering for synthesized (unnamed) ones — so
     * {@code infoForStructString} finds the latter. Mirrors
     * ReflectOpNode.structDescFor but carries array lengths and element
     * descriptors, which structSizeBytes needs for the slots array.
     */
    private static GoStructDescriptor descriptorFor(ReflectTypes.TypeInfo info) {
        if (info.kind != GoKind.STRUCT || info.fields == null) {
            throw new IllegalStateException(
                    "reflect: cannot build a struct descriptor for non-struct type " + info.name);
        }
        String typeName = (info.name != null && !info.name.isEmpty())
                ? info.name : ReflectTypes.typeString(info.tok);
        List<String> names = new ArrayList<>();
        List<GoKind> kinds = new ArrayList<>();
        List<Integer> lens = new ArrayList<>();
        List<GoKind> elemKinds = new ArrayList<>();
        List<GoStructDescriptor> elemDescs = new ArrayList<>();
        List<GoStructDescriptor> structDescs = new ArrayList<>();
        for (ReflectTypes.FieldInfo f : info.fields) {
            names.add(f.name);
            kinds.add(f.type.kind);
            if (f.type.kind == GoKind.ARRAY) {
                lens.add(f.type.arrayLen);
                elemKinds.add(f.type.elem.kind);
                elemDescs.add(arrayElemDesc(f.type));
            } else {
                lens.add(-1);
                elemKinds.add(null);
                elemDescs.add(null);
            }
            structDescs.add(f.type.kind == GoKind.STRUCT ? descriptorFor(f.type) : null);
        }
        return GoStructDescriptor.layout(typeName, names, kinds, lens, elemKinds, elemDescs,
                structDescs);
    }

    /**
     * The struct descriptor for an array type's ultimate struct element,
     * or null when the element chain does not end in a struct. (Nested
     * arrays of structs are not supported by structSizeBytes today — a
     * map keyed by one fails loudly at first iteration, never silently.)
     */
    private static GoStructDescriptor arrayElemDesc(ReflectTypes.TypeInfo arrayInfo) {
        ReflectTypes.TypeInfo t = arrayInfo.elem;
        while (t != null && t.kind == GoKind.ARRAY) {
            t = t.elem;
        }
        if (t != null && t.kind == GoKind.STRUCT) {
            return descriptorFor(t);
        }
        return null;
    }
}
