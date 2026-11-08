package graalgo.p3reflect;

import graalgo.values.GoKind;
import graalgo.values.GoMap;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.ConcurrentHashMap;

/**
 * A synthesized {@code internal/runtime/maps.Map} view over a guest
 * {@link GoMap} (fix-rv1107-10-mapaccess, R1): the disk-reflect
 * MapKeys/MapRange path converts the map Value's pointer to
 * {@code *maps.Map} and runs the maps package's Iter over it, but the
 * map Value's pointer denotes a GoMap handle, not a {@code *maps.Map}
 * struct — so the conversion synthesizes a view struct the iterator
 * can walk, and returns a GoPointer to it (same class as the existing
 * header/interface/prefix views: target-type-directed, no name-keyed
 * dispatch on the source, no synthetic package).
 *
 * <p>Layout (gc internal/runtime/maps): a small map (n &le; 8) is one
 * group; the view's dirPtr points at the group root and dirLen is 0
 * (the small path: {@code groupSmall.data = m.dirPtr}). A large map
 * has G = next-pow2(ceil(n/8)) groups in T tables (per-table group cap
 * 4096, capacity 32768, fits uint16 used/capacity); each table gets its
 * own groups-root struct; entries are distributed in order; the
 * directory is a struct of T *table pointers (dirLen = T, globalDepth
 * = log2(T)); each table's index is its directory position, so
 * {@code newTab.index != dirIdx} never fires and {@code grown} stays
 * false — the iterator never calls {@code getWithKey}, it just walks
 * every slot in order. A single table with G groups holds any n that
 * fits; the tables are never "grown", so no hash-correct placement is
 * needed (reimplementing Swiss-table insertion is avoided).
 *
 * <p>Snapshot semantics (a documented limitation, not silently claimed
 * correct): the view is synthesized fresh at each {@code (*maps.Map)}
 * conversion (once per MapKeys/MapRange call), so MapKeys is exactly
 * correct (no guest code runs during it). Delete-during-MapRange of an
 * unvisited entry is the one known divergence (the snapshot still
 * yields it; gc would not). Add-during-iteration is legal either way
 * (gc: may or may not appear; snapshot: does not). A liveness
 * follow-up (GoMap notifies live views on put/delete/clear) is
 * sketched, not implemented.
 */
public final class MapsView {
    private MapsView() {}

    /** Groups per table in the large path (capacity 32768, fits uint16). */
    private static final int GROUPS_PER_TABLE = 4096;

    /** Control word with every slot empty (ctrlEmpty 0x80 per byte). */
    private static final long CTRL_EMPTY_WORD = 0x8080808080808080L;

    /** Memo for group-root types: (keyTok, elemTok, groups). */
    private static final ConcurrentHashMap<String, GroupType> GROUP_MEMO = new ConcurrentHashMap<>();

    /** Memo for directory types: (keyTok, elemTok, tables). */
    private static final ConcurrentHashMap<String, DirType> DIR_MEMO = new ConcurrentHashMap<>();

    /** A group-root struct's TypeInfo token + GoStructDescriptor. */
    private record GroupType(long tok, GoStructDescriptor desc) {}

    /** A directory struct's TypeInfo token + GoStructDescriptor. */
    private record DirType(long tok, GoStructDescriptor desc) {}

    /**
     * Builds the maps.Map view struct for a guest GoMap. The map must
     * carry its type token (see GoMap.setTypeTok); an unknown type
     * fails loudly instead of building a wrong view.
     */
    public static GoStruct buildView(GoMap map) {
        long mapTok = map.typeTok();
        if (mapTok < 0) {
            throw new IllegalStateException(
                    "maps: cannot build a maps.Map view for a map with unknown type "
                            + "(no type token recorded)");
        }
        ReflectTypes.TypeInfo mt = ReflectTypes.table().info(mapTok);
        long keyTok = mt.key.tok;
        long elemTok = mt.elem.tok;
        MapsLayout.Layout layout = MapsLayout.forMap(keyTok, elemTok);
        Object[] keys = map.keysSnapshot();
        int n = keys.length;

        // Group count: next pow2 of ceil(n/8); 1 group for a small map.
        int groups = 1;
        if (n > MapsLayout.GROUP_SLOTS) {
            int need = (n + MapsLayout.GROUP_SLOTS - 1) / MapsLayout.GROUP_SLOTS;
            groups = 1;
            while (groups < need) {
                groups *= 2;
            }
        }
        int groupsPerTable = Math.min(groups, GROUPS_PER_TABLE);
        int tables = groups / groupsPerTable;

        GoStruct dirPtrTarget;
        int dirLen;
        int globalDepth;
        if (tables == 1 && n <= MapsLayout.GROUP_SLOTS) {
            // Small path: one group; dirPtr points at the group root.
            GoStruct root = buildGroupRoot(keyTok, elemTok, layout, keys, map, 0, n);
            dirPtrTarget = root;
            dirLen = 0;
            globalDepth = 0;
        } else {
            // Large path: one directory struct of T table pointers.
            GoStruct[] tableStructs = new GoStruct[tables];
            for (int t = 0; t < tables; t++) {
                int start = t * groupsPerTable * MapsLayout.GROUP_SLOTS;
                int count = Math.min(n - start, groupsPerTable * MapsLayout.GROUP_SLOTS);
                GoStruct root = buildGroupRoot(keyTok, elemTok, layout, keys, map, start,
                        Math.max(0, count));
                tableStructs[t] = buildTable(keyTok, elemTok, layout, root, groupsPerTable,
                        Math.max(0, count), t);
            }
            dirPtrTarget = buildDirectory(keyTok, elemTok, tableStructs);
            dirLen = tables;
            globalDepth = log2(tables);
        }

        GoStructDescriptor mapDesc =
                ReflectTypes.structDescriptorFor("internal/runtime/maps.Map");
        GoStruct view = GoStruct.zero(mapDesc);
        setLongField(view, "used", n);
        setLongField(view, "seed", 0);
        setObjectField(view, "dirPtr", new GoPointer(dirPtrTarget));
        setLongField(view, "dirLen", dirLen);
        setLongField(view, "globalDepth", globalDepth);
        setLongField(view, "globalShift", 64 - globalDepth);
        setLongField(view, "writing", 0);
        setLongField(view, "tombstonePossible", 0);
        setLongField(view, "clearSeq", 0);
        return view;
    }

    /**
     * A groups-root struct holding {@code count} entries starting at
     * global entry index {@code start} (entries are distributed in
     * order across tables). Slots hold the GoMap's value objects
     * directly (NO copy — copyVal copies from the view's slot via
     * typedmemmove; storing directly preserves pointer-sharing
     * semantics for pointer-containing keys/elems).
     */
    private static GoStruct buildGroupRoot(long keyTok, long elemTok, MapsLayout.Layout layout,
            Object[] keys, GoMap map, int start, int count) {
        int groups = 1;
        if (count > MapsLayout.GROUP_SLOTS) {
            int need = (count + MapsLayout.GROUP_SLOTS - 1) / MapsLayout.GROUP_SLOTS;
            groups = 1;
            while (groups < need) {
                groups *= 2;
            }
        }
        GroupType gt = groupType(keyTok, elemTok, layout, groups);
        GoStruct root = GoStruct.zero(gt.desc());
        ReflectTypes.TypeInfo kt = ReflectTypes.table().info(keyTok);
        ReflectTypes.TypeInfo et = ReflectTypes.table().info(elemTok);
        for (int g = 0; g < groups; g++) {
            long ctrls = CTRL_EMPTY_WORD;
            for (int s = 0; s < MapsLayout.GROUP_SLOTS; s++) {
                int idx = start + g * MapsLayout.GROUP_SLOTS + s;
                if (idx >= start + count) {
                    break;
                }
                Object key = keys[idx];
                Object value = map.get(key);
                // Real h2 (top 7 bits of the hash) for filled slots.
                long h2 = (MapKeyHash.hashValue(key, 0) >> 57) & 0x7FL;
                ctrls = (ctrls & ~(0xFFL << (8L * s))) | (h2 << (8L * s));
                setSlot(root, g, s, true, keyTok, kt, layout, key);
                setSlot(root, g, s, false, elemTok, et, layout, value);
            }
            setLongField(root, "g" + g + "_ctrls", ctrls);
        }
        return root;
    }

    /**
     * Stores one key/elem slot of one group. The slot field kind is
     * UNSAFE_POINTER for indirect (size &gt; 128) or direct-iface
     * (pointer/chan/func/map/unsafe.Pointer) kinds — copyVal's direct
     * path derefs the slot via {@code *(*unsafe.Pointer)(ptr)}, so the
     * field tok must equal tok(unsafe.Pointer) — else the actual
     * key/elem kind. Indirect/direct-iface slots hold a GoPointer (to
     * the value, resp. the key itself); direct slots hold the value
     * object (inLongs fields via long bits).
     */
    private static void setSlot(GoStruct root, int g, int s, boolean isKey,
            long tok, ReflectTypes.TypeInfo t, MapsLayout.Layout layout, Object value) {
        String name = "g" + g + "_" + (isKey ? "k" : "e") + s;
        boolean indirect = isKey ? layout.indirectKey() : layout.indirectElem();
        boolean directIface = MapsLayout.isDirectIface(t.kind);
        if (indirect || directIface) {
            setObjectField(root, name, slotPointer(value));
            return;
        }
        GoStructDescriptor.Field field =
                root.descriptor().field(fieldOrdinal(root.descriptor(), name));
        if (field.inLongs()) {
            setLongField(root, name, toLongBits(t.kind, value));
        } else {
            setObjectField(root, name, value);
        }
    }

    /** The GoPointer for an indirect/direct-iface slot (GoPointer as-is, else wrapped). */
    private static GoPointer slotPointer(Object value) {
        if (value instanceof GoPointer gp) {
            return gp;
        }
        // A nil value becomes a nil GoPointer, not Java null.
        return new GoPointer(value);
    }

    /** A table struct for one groups-root (used/capacity/growthLeft/localDepth/index/groups). */
    private static GoStruct buildTable(long keyTok, long elemTok, MapsLayout.Layout layout,
            GoStruct groupsRoot, int groupsPerTable, int used, int index) {
        GoStructDescriptor tableDesc =
                ReflectTypes.structDescriptorFor("internal/runtime/maps.table");
        GoStruct table = GoStruct.zero(tableDesc);
        int capacity = groupsPerTable * MapsLayout.GROUP_SLOTS;
        setLongField(table, "used", used);
        setLongField(table, "capacity", capacity);
        setLongField(table, "growthLeft", capacity - used);
        setLongField(table, "localDepth", 0);
        setLongField(table, "index", index);
        GoStructDescriptor groupsDesc =
                ReflectTypes.structDescriptorFor("internal/runtime/maps.groupsReference");
        GoStruct groups = GoStruct.zero(groupsDesc);
        setObjectField(groups, "data", new GoPointer(groupsRoot));
        setLongField(groups, "lengthMask", groupsPerTable - 1);
        setObjectField(table, "groups", groups);
        return table;
    }

    /** A directory struct of T *table pointers (fields d0..d_{T-1}, offsets 8*i). */
    private static GoStruct buildDirectory(long keyTok, long elemTok, GoStruct[] tables) {
        DirType dt = dirType(keyTok, elemTok, tables.length);
        GoStruct dir = GoStruct.zero(dt.desc());
        for (int t = 0; t < tables.length; t++) {
            setObjectField(dir, "d" + t, new GoPointer(tables[t]));
        }
        return dir;
    }

    /**
     * The group-root TypeInfo + descriptor for (keyTok, elemTok,
     * groups): TypeInfo and descriptor are built from ONE field list
     * (same order/names) so structFieldAt/readStructFieldAt resolve
     * byte offsets to fields. The TypeInfo name is tok-based so two
     * different unnamed types never collide; the descriptor typeName
     * is the same string so infoForName(descriptor.typeName()) finds
     * the TypeInfo.
     */
    private static GroupType groupType(long keyTok, long elemTok, MapsLayout.Layout layout,
            int groups) {
        String memoKey = keyTok + ":" + elemTok + "#" + groups;
        GroupType gt = GROUP_MEMO.get(memoKey);
        if (gt != null) {
            return gt;
        }
        ReflectTypes.Table table = ReflectTypes.table();
        synchronized (table) {
            gt = GROUP_MEMO.get(memoKey);
            if (gt != null) {
                return gt;
            }
            ReflectTypes.TypeInfo kt = table.info(keyTok);
            ReflectTypes.TypeInfo et = table.info(elemTok);
            ReflectTypes.TypeInfo ctrlInfo = MapsLayout.ctrlGroupInfo();
            ReflectTypes.TypeInfo unsafePtrInfo = unsafePointerInfo();
            String typeName = "internal/runtime/maps.viewGroup:" + keyTok + ":" + elemTok
                    + "#" + groups;
            List<String> names = new ArrayList<>();
            List<GoKind> kinds = new ArrayList<>();
            List<Integer> arrayLens = new ArrayList<>();
            List<GoKind> arrayElemKinds = new ArrayList<>();
            List<GoStructDescriptor> arrayElemDescs = new ArrayList<>();
            List<GoStructDescriptor> structDescs = new ArrayList<>();
            List<ReflectTypes.FieldInfo> fields = new ArrayList<>();
            int ordinal = 0;
            for (int g = 0; g < groups; g++) {
                long groupBase = (long) g * layout.groupSize();
                names.add("g" + g + "_ctrls");
                kinds.add(GoKind.UINT64);
                arrayLens.add(-1);
                arrayElemKinds.add(null);
                arrayElemDescs.add(null);
                structDescs.add(null);
                fields.add(new ReflectTypes.FieldInfo("g" + g + "_ctrls", "", ctrlInfo, "",
                        groupBase, new int[]{ordinal++}, false));
                for (int s = 0; s < MapsLayout.GROUP_SLOTS; s++) {
                    addSlotField(names, kinds, arrayLens, arrayElemKinds, arrayElemDescs,
                            structDescs, fields, "g" + g + "_k" + s,
                            groupBase + layout.keysOff() + (long) s * layout.keyStride(),
                            ordinal++, kt, layout.indirectKey(), unsafePtrInfo);
                    addSlotField(names, kinds, arrayLens, arrayElemKinds, arrayElemDescs,
                            structDescs, fields, "g" + g + "_e" + s,
                            groupBase + layout.elemsOff() + (long) s * layout.elemStride(),
                            ordinal++, et, layout.indirectElem(), unsafePtrInfo);
                }
            }
            GoStructDescriptor desc = GoStructDescriptor.layout(typeName, names, kinds,
                    arrayLens, arrayElemKinds, arrayElemDescs, structDescs);
            long newTok = table.byTok.size();
            table.keyToTok.put(typeName, newTok);
            ReflectTypes.TypeInfo info = new ReflectTypes.TypeInfo(newTok, GoKind.STRUCT,
                    typeName, "", (long) groups * layout.groupSize(), (int) layout.groupAlign(),
                    null, null, 0, null, fields, List.of(),
                    List.of(), List.of(), false, List.of(), "", -1);
            table.byTok.add(info);
            gt = new GroupType(newTok, desc);
            GROUP_MEMO.put(memoKey, gt);
            return gt;
        }
    }

    /** One slot field (key or elem) of a group-root struct. */
    private static void addSlotField(List<String> names, List<GoKind> kinds,
            List<Integer> arrayLens, List<GoKind> arrayElemKinds,
            List<GoStructDescriptor> arrayElemDescs, List<GoStructDescriptor> structDescs,
            List<ReflectTypes.FieldInfo> fields, String name, long offset, int ordinal,
            ReflectTypes.TypeInfo t, boolean indirect, ReflectTypes.TypeInfo unsafePtrInfo) {
        GoKind slotKind;
        ReflectTypes.TypeInfo slotType;
        if (indirect || MapsLayout.isDirectIface(t.kind)) {
            slotKind = GoKind.UNSAFE_POINTER;
            slotType = unsafePtrInfo;
        } else {
            slotKind = t.kind;
            slotType = t;
        }
        names.add(name);
        kinds.add(slotKind);
        if (slotKind == GoKind.ARRAY) {
            arrayLens.add(t.arrayLen);
            arrayElemKinds.add(t.elem.kind);
            arrayElemDescs.add(t.elem.kind == GoKind.STRUCT
                    ? ReflectOpNode.structDescFor(t.elem) : null);
        } else {
            arrayLens.add(-1);
            arrayElemKinds.add(null);
            arrayElemDescs.add(null);
        }
        structDescs.add(slotKind == GoKind.STRUCT ? ReflectOpNode.structDescFor(t) : null);
        fields.add(new ReflectTypes.FieldInfo(name, "", slotType, "", offset,
                new int[]{ordinal}, false));
    }

    /**
     * The directory TypeInfo + descriptor for (keyTok, elemTok, T):
     * fields d0..d_{T-1} (POINTER kind, GoPointers to tables), offsets
     * 8*i. The *table TypeInfo is found by scanning byTok for
     * kind==POINTER && elem.tok == tableTok (the real *maps.table tok
     * — required so FieldHit.fieldTok == targetTok in
     * UnsafeToPtrNode.structFieldAt).
     */
    private static DirType dirType(long keyTok, long elemTok, int tables) {
        String memoKey = keyTok + ":" + elemTok + "#" + tables;
        DirType dt = DIR_MEMO.get(memoKey);
        if (dt != null) {
            return dt;
        }
        ReflectTypes.Table table = ReflectTypes.table();
        synchronized (table) {
            dt = DIR_MEMO.get(memoKey);
            if (dt != null) {
                return dt;
            }
            ReflectTypes.TypeInfo tableInfo =
                    ReflectTypes.infoForName("internal/runtime/maps.table");
            if (tableInfo == null) {
                throw new IllegalStateException("maps: no internal/runtime/maps.table type");
            }
            ReflectTypes.TypeInfo ptrToTableInfo = null;
            for (int i = 1; i < table.byTok.size(); i++) {
                ReflectTypes.TypeInfo candidate = table.byTok.get(i);
                if (candidate != null && candidate.kind == GoKind.POINTER
                        && candidate.elem != null && candidate.elem.tok == tableInfo.tok) {
                    ptrToTableInfo = candidate;
                    break;
                }
            }
            if (ptrToTableInfo == null) {
                throw new IllegalStateException(
                        "maps: no *internal/runtime/maps.table type in the table");
            }
            String typeName = "internal/runtime/maps.viewDir:" + keyTok + ":" + elemTok
                    + "#" + tables;
            List<String> names = new ArrayList<>();
            List<GoKind> kinds = new ArrayList<>();
            List<Integer> arrayLens = new ArrayList<>();
            List<GoKind> arrayElemKinds = new ArrayList<>();
            List<GoStructDescriptor> arrayElemDescs = new ArrayList<>();
            List<GoStructDescriptor> structDescs = new ArrayList<>();
            List<ReflectTypes.FieldInfo> fields = new ArrayList<>();
            for (int t = 0; t < tables; t++) {
                names.add("d" + t);
                kinds.add(GoKind.POINTER);
                arrayLens.add(-1);
                arrayElemKinds.add(null);
                arrayElemDescs.add(null);
                structDescs.add(null);
                fields.add(new ReflectTypes.FieldInfo("d" + t, "", ptrToTableInfo, "",
                        8L * t, new int[]{t}, false));
            }
            GoStructDescriptor desc = GoStructDescriptor.layout(typeName, names, kinds,
                    arrayLens, arrayElemKinds, arrayElemDescs, structDescs);
            long newTok = table.byTok.size();
            table.keyToTok.put(typeName, newTok);
            ReflectTypes.TypeInfo info = new ReflectTypes.TypeInfo(newTok, GoKind.STRUCT,
                    typeName, "", 8L * tables, 8,
                    null, null, 0, null, fields, List.of(),
                    List.of(), List.of(), false, List.of(), "", -1);
            table.byTok.add(info);
            dt = new DirType(newTok, desc);
            DIR_MEMO.put(memoKey, dt);
            return dt;
        }
    }

    /** The unsafe.Pointer TypeInfo (the wire name is bare "Pointer", never "unsafe.Pointer"). */
    private static ReflectTypes.TypeInfo unsafePointerInfo() {
        ReflectTypes.Table table = ReflectTypes.table();
        for (int i = 1; i < table.byTok.size(); i++) {
            ReflectTypes.TypeInfo candidate = table.byTok.get(i);
            if (candidate != null && candidate.kind == GoKind.UNSAFE_POINTER) {
                return candidate;
            }
        }
        throw new IllegalStateException("maps: no unsafe.Pointer type in the table");
    }

    /** Exact log2 of a power of two. */
    private static int log2(int pow2) {
        int log = 0;
        while ((1 << log) < pow2) {
            log++;
        }
        return log;
    }

    /** The field ordinal of a named field in a descriptor. */
    private static int fieldOrdinal(GoStructDescriptor desc, String name) {
        for (int i = 0; i < desc.fields().size(); i++) {
            if (desc.field(i).name().equals(name)) {
                return i;
            }
        }
        throw new IllegalStateException("maps: no field " + name + " in "
                + desc.typeName());
    }

    /** Sets a Long-carried field by name (loud on carrier mismatch). */
    private static void setLongField(GoStruct s, String name, long value) {
        int ordinal = fieldOrdinal(s.descriptor(), name);
        if (!s.descriptor().field(ordinal).inLongs()) {
            throw new IllegalStateException("maps: field " + name + " of "
                    + s.descriptor().typeName() + " is not Long-carried");
        }
        s.setLong(ordinal, value);
    }

    /** Sets an object-carried field by name (loud on carrier mismatch). */
    private static void setObjectField(GoStruct s, String name, Object value) {
        int ordinal = fieldOrdinal(s.descriptor(), name);
        if (s.descriptor().field(ordinal).inLongs()) {
            throw new IllegalStateException("maps: field " + name + " of "
                    + s.descriptor().typeName() + " is Long-carried");
        }
        s.setObject(ordinal, value);
    }

    /**
     * Long bits for a direct slot value (mirrors Boxing.toLongBits,
     * which is package-private to graalgo.frontend and not callable
     * from p3reflect).
     */
    private static long toLongBits(GoKind kind, Object value) {
        return switch (kind) {
            case FLOAT32 -> (long) Float.floatToIntBits((Float) value);
            case FLOAT64 -> Double.doubleToLongBits((Double) value);
            case BOOL, UNTYPED_BOOL -> ((Boolean) value) ? 1L : 0L;
            default -> ((Number) value).longValue();
        };
    }
}
