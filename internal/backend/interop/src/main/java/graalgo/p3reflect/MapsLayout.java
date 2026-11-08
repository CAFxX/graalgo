package graalgo.p3reflect;

import graalgo.values.GoKind;
import java.util.List;
import java.util.concurrent.ConcurrentHashMap;

/**
 * The gc Swiss-table group layout for a map[K]V (fix-rv1107-10-mapaccess,
 * R1): the split layout the maps package's group struct uses, computed
 * from the map's key/elem TypeInfos so the MapsView builder and the
 * abi MapType shell agree by construction (the shell is the single
 * source of truth — MapsView reads the shell's fields).
 *
 * <p>Rules (gc cmd/compile/internal/reflectdata/map.go,
 * internal/abi/map.go): split layout — KeysOff = alignUp(8, keyAlign),
 * KeyStride = keySize, ElemsOff = alignUp(KeysOff + 8*keySize,
 * elemAlign), ElemStride = elemSize, ElemOff = 0 (split only),
 * GroupSize = alignUp(ElemsOff + 8*elemSize, groupAlign), groupAlign =
 * max(8, keyAlign, elemAlign). MapGroupSlots = 8. IndirectKey iff
 * Key.Size() &gt; 128 (MapMaxKeyBytes), IndirectElem iff Elem.Size()
 * &gt; 128. Flags (iota order): NeedKeyUpdate=1, HashMightPanic=2,
 * IndirectKey=4, IndirectElem=8.
 */
public final class MapsLayout {
    private MapsLayout() {}

    /** Slots per group (gc internal/abi/map.go MapGroupSlots). */
    public static final int GROUP_SLOTS = 8;

    /** Max direct key/elem size in bytes (gc MapMaxKeyBytes/ElemBytes). */
    public static final long MAX_DIRECT_BYTES = 128;

    /** MapType.Flags bit: the key needs updating on insert. */
    public static final int FLAG_NEED_KEY_UPDATE = 1;

    /** MapType.Flags bit: hashing the key might panic. */
    public static final int FLAG_HASH_MIGHT_PANIC = 2;

    /** MapType.Flags bit: keys are stored indirectly. */
    public static final int FLAG_INDIRECT_KEY = 4;

    /** MapType.Flags bit: elems are stored indirectly. */
    public static final int FLAG_INDIRECT_ELEM = 8;

    /**
     * The computed layout for one map[K]V, plus the token of the
     * synthetic group struct TypeInfo (created by {@link #groupTypeTok}).
     */
    public record Layout(long keysOff, long keyStride, long elemsOff, long elemStride,
            long elemOff, long groupSize, long groupAlign,
            boolean indirectKey, boolean indirectElem, int flags, long groupTok) {}

    private static final ConcurrentHashMap<String, Layout> MEMO = new ConcurrentHashMap<>();

    /** The layout for map[K]V (memoized by (keyTok, elemTok)). */
    public static Layout forMap(long keyTok, long elemTok) {
        String key = keyTok + ":" + elemTok;
        Layout layout = MEMO.get(key);
        if (layout != null) {
            return layout;
        }
        ReflectTypes.TypeInfo kt = ReflectTypes.table().info(keyTok);
        ReflectTypes.TypeInfo et = ReflectTypes.table().info(elemTok);
        long keyAlign = kt.align <= 0 ? 1 : kt.align;
        long elemAlign = et.align <= 0 ? 1 : et.align;
        long keysOff = alignUp(8, keyAlign);
        long keyStride = kt.size;
        long elemsOff = alignUp(keysOff + GROUP_SLOTS * kt.size, elemAlign);
        long elemStride = et.size;
        long groupAlign = Math.max(8, Math.max(keyAlign, elemAlign));
        long groupSize = alignUp(elemsOff + GROUP_SLOTS * et.size, groupAlign);
        boolean indirectKey = kt.size > MAX_DIRECT_BYTES;
        boolean indirectElem = et.size > MAX_DIRECT_BYTES;
        int flags = 0;
        if (needKeyUpdate(kt)) {
            flags |= FLAG_NEED_KEY_UPDATE;
        }
        if (hashMightPanic(kt)) {
            flags |= FLAG_HASH_MIGHT_PANIC;
        }
        if (indirectKey) {
            flags |= FLAG_INDIRECT_KEY;
        }
        if (indirectElem) {
            flags |= FLAG_INDIRECT_ELEM;
        }
        Layout computed = new Layout(keysOff, keyStride, elemsOff, elemStride,
                0, groupSize, groupAlign, indirectKey, indirectElem, flags,
                groupTypeTok(keyTok, elemTok));
        Layout prev = MEMO.putIfAbsent(key, computed);
        return prev != null ? prev : computed;
    }

    /** Token of the synthetic group struct TypeInfo for map[K]V. */
    public static long groupTypeTok(long keyTok, long elemTok) {
        ReflectTypes.Table table = ReflectTypes.table();
        String key = "maps.group:" + keyTok + ":" + elemTok;
        Long existing = table.keyToTok.get(key);
        if (existing != null) {
            return existing;
        }
        synchronized (table) {
            existing = table.keyToTok.get(key);
            if (existing != null) {
                return existing;
            }
            Layout layout = groupLayoutOnly(keyTok, elemTok);
            ReflectTypes.TypeInfo kt = table.info(keyTok);
            ReflectTypes.TypeInfo et = table.info(elemTok);
            long keysTok = ReflectTypes.arrayOf(keyTok, GROUP_SLOTS);
            long elemsTok = ReflectTypes.arrayOf(elemTok, GROUP_SLOTS);
            ReflectTypes.TypeInfo ctrlInfo = ctrlGroupInfo();
            ReflectTypes.TypeInfo keysInfo = table.info(keysTok);
            ReflectTypes.TypeInfo elemsInfo = table.info(elemsTok);
            List<ReflectTypes.FieldInfo> fields = List.of(
                    new ReflectTypes.FieldInfo("ctrls", "", ctrlInfo, "", 0, new int[]{0}, false),
                    new ReflectTypes.FieldInfo("keys", "", keysInfo, "", layout.keysOff(),
                            new int[]{1}, false),
                    new ReflectTypes.FieldInfo("elems", "", elemsInfo, "", layout.elemsOff(),
                            new int[]{2}, false));
            long newTok = table.byTok.size();
            table.keyToTok.put(key, newTok);
            ReflectTypes.TypeInfo info = new ReflectTypes.TypeInfo(newTok, GoKind.STRUCT,
                    key, "", layout.groupSize(), (int) layout.groupAlign(),
                    null, null, 0, null, fields, List.of(),
                    List.of(), List.of(), false, List.of(), "", -1);
            table.byTok.add(info);
            return newTok;
        }
    }

    /**
     * The group layout numbers without creating the group TypeInfo
     * (groupTypeTok's inner computation; forMap calls groupTypeTok,
     * which calls this — the memo in forMap prevents re-entry loops).
     */
    private static Layout groupLayoutOnly(long keyTok, long elemTok) {
        ReflectTypes.TypeInfo kt = ReflectTypes.table().info(keyTok);
        ReflectTypes.TypeInfo et = ReflectTypes.table().info(elemTok);
        long keyAlign = kt.align <= 0 ? 1 : kt.align;
        long elemAlign = et.align <= 0 ? 1 : et.align;
        long keysOff = alignUp(8, keyAlign);
        long elemsOff = alignUp(keysOff + GROUP_SLOTS * kt.size, elemAlign);
        long groupAlign = Math.max(8, Math.max(keyAlign, elemAlign));
        long groupSize = alignUp(elemsOff + GROUP_SLOTS * et.size, groupAlign);
        return new Layout(keysOff, kt.size, elemsOff, et.size,
                0, groupSize, groupAlign, kt.size > MAX_DIRECT_BYTES,
                et.size > MAX_DIRECT_BYTES, 0, -1);
    }

    /** The ctrlGroup TypeInfo (named uint64), or uint64 as fallback. */
    public static ReflectTypes.TypeInfo ctrlGroupInfo() {
        ReflectTypes.TypeInfo info =
                ReflectTypes.infoForName("internal/runtime/maps.ctrlGroup");
        if (info != null) {
            return info;
        }
        ReflectTypes.Table table = ReflectTypes.table();
        for (int i = 1; i < table.byTok.size(); i++) {
            ReflectTypes.TypeInfo candidate = table.byTok.get(i);
            if (candidate != null && candidate.kind == GoKind.UINT64 && candidate.size == 8) {
                return candidate;
            }
        }
        throw new IllegalStateException("maps: no uint64 type in the table (ctrlGroup fallback)");
    }

    /** Whether the key type needs updating on insert (gc needkeyupdate). */
    public static boolean needKeyUpdate(ReflectTypes.TypeInfo t) {
        return switch (t.kind) {
            case BOOL, INT8, INT16, INT32, INT64, UINT8, UINT16, UINT32, UINT64,
                 POINTER, UNSAFE_POINTER, CHAN -> false;
            case FLOAT32, FLOAT64, COMPLEX64, COMPLEX128, INTERFACE, STRING -> true;
            case ARRAY -> needKeyUpdate(t.elem);
            case STRUCT -> t.fields.stream().anyMatch(f -> needKeyUpdate(f.type));
            default -> true;
        };
    }

    /** Whether hashing the key might panic (gc hashMightPanic). */
    public static boolean hashMightPanic(ReflectTypes.TypeInfo t) {
        return switch (t.kind) {
            case INTERFACE -> true;
            case ARRAY -> hashMightPanic(t.elem);
            case STRUCT -> t.fields.stream().anyMatch(f -> hashMightPanic(f.type));
            default -> false;
        };
    }

    /**
     * Whether copyVal takes the direct-iface path for this type (gc
     * IsDirectIface): pointer, chan, func, map, and unsafe.Pointer
     * values live in the Value's ptr word directly.
     */
    public static boolean isDirectIface(GoKind kind) {
        return kind == GoKind.POINTER || kind == GoKind.CHAN || kind == GoKind.FUNC
                || kind == GoKind.MAP || kind == GoKind.UNSAFE_POINTER;
    }

    private static long alignUp(long x, long align) {
        return (x + align - 1) / align * align;
    }
}
