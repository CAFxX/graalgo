package abispike;

import abispike.Mem.Addr;
import java.io.ByteArrayOutputStream;
import java.nio.charset.StandardCharsets;

/**
 * go1.27.1 {@code internal/abi} and {@code reflect} type accessors,
 * transliterated statement by statement into operations on the pointer
 * model — the code a node factory would build from the verbatim source.
 * Each method quotes the Go it stands for. The only functions that are NOT
 * transliterations are the intrinsics gc also implements in the runtime:
 * {@code resolveNameOff} and {@code resolveTypeOff}.
 */
public final class Abi {
    private final TypeTab tab;
    private final Images images;
    private final long oTFlag, oKind, oSize, oAlign, oFieldAlign, oStr;

    public Abi(TypeTab tab, Images images) {
        this.tab = tab;
        this.images = images;
        oTFlag = tab.off("Type.TFlag");
        oKind = tab.off("Type.Kind_");
        oSize = tab.off("Type.Size_");
        oAlign = tab.off("Type.Align_");
        oFieldAlign = tab.off("Type.FieldAlign_");
        oStr = tab.off("Type.Str");
    }

    // func (t *Type) Kind() Kind { return t.Kind_ }
    public int kind(Addr t) {
        return (int) t.u8(oKind);
    }

    // func (t *Type) Size() uintptr { return t.Size_ }
    public long size(Addr t) {
        return t.u64(oSize);
    }

    // func (t *Type) Align() int { return int(t.Align_) }
    public int align(Addr t) {
        return (int) t.u8(oAlign);
    }

    // func (t *Type) FieldAlign() int { return int(t.FieldAlign_) }
    public int fieldAlign(Addr t) {
        return (int) t.u8(oFieldAlign);
    }

    // func (t *Type) IsDirectIface() bool { return t.TFlag&TFlagDirectIface != 0 }
    public boolean isDirectIface(Addr t) {
        return (t.u8(oTFlag) & Images.TFLAG_DIRECT_IFACE) != 0;
    }

    // func (t *Type) HasName() bool { return t.TFlag&TFlagNamed != 0 }
    public boolean hasName(Addr t) {
        return (t.u8(oTFlag) & Images.TFLAG_NAMED) != 0;
    }

    // func (n Name) DataChecked(off int, whySafe string) *byte {
    //     return (*byte)(addChecked(unsafe.Pointer(n.Bytes), uintptr(off), whySafe)) }
    private static Addr nameData(Addr bytes, int off) {
        return bytes.add(off);
    }

    // func (n Name) ReadVarint(off int) (int, int) {
    //     v := 0
    //     for i := 0; ; i++ {
    //         x := *n.DataChecked(off+i, "read varint")
    //         v += int(x&0x7f) << (7 * i)
    //         if x&0x80 == 0 { return i + 1, v } } }
    private static int[] readVarint(Addr n, int off) {
        int v = 0;
        for (int i = 0;; i++) {
            long x = nameData(n, off + i).u8(0);
            v += (int) (x & 0x7f) << (7 * i);
            if ((x & 0x80) == 0) {
                return new int[] {i + 1, v};
            }
        }
    }

    /** {@code unsafe.String(p, n)}: n bytes starting at p, as a Go string (kept as raw bytes). */
    private static byte[] unsafeString(Addr p, int n) {
        ByteArrayOutputStream out = new ByteArrayOutputStream(n);
        for (int i = 0; i < n; i++) {
            out.write((int) p.u8(i));
        }
        return out.toByteArray();
    }

    // func (n Name) Name() string {
    //     if n.Bytes == nil { return "" }
    //     i, l := n.ReadVarint(1)
    //     return unsafe.String(n.DataChecked(1+i, "non-empty string"), l) }
    public static byte[] nameName(Addr n) {
        if (n == null) {
            return new byte[0];
        }
        int[] il = readVarint(n, 1);
        return unsafeString(nameData(n, 1 + il[0]), il[1]);
    }

    // func (n Name) Tag() string {
    //     if !n.HasTag() { return "" }
    //     i, l := n.ReadVarint(1)
    //     i2, l2 := n.ReadVarint(1 + i + l)
    //     return unsafe.String(n.DataChecked(1+i+l+i2, "non-empty string"), l2) }
    public static byte[] nameTag(Addr n) {
        if ((n.u8(0) & 2) == 0) {
            return new byte[0];
        }
        int[] il = readVarint(n, 1);
        int[] il2 = readVarint(n, 1 + il[0] + il[1]);
        return unsafeString(nameData(n, 1 + il[0] + il[1] + il2[0]), il2[1]);
    }

    // func (n Name) IsExported() bool { return (*n.Bytes)&(1<<0) != 0 }
    public static boolean nameIsExported(Addr n) {
        return (n.u8(0) & 1) != 0;
    }

    // func (n Name) IsEmbedded() bool { return (*n.Bytes)&(1<<3) != 0 }
    public static boolean nameIsEmbedded(Addr n) {
        return (n.u8(0) & 8) != 0;
    }

    // func (t *rtype) nameOff(off aNameOff) abi.Name {
    //     return abi.Name{Bytes: (*byte)(resolveNameOff(unsafe.Pointer(t), int32(off)))} }
    private Addr nameOff(Addr t, int off) {
        return images.resolveOff(off); // intrinsic
    }

    // func (t *rtype) String() string {
    //     s := t.nameOff(t.t.Str).Name()
    //     if t.t.TFlag&abi.TFlagExtraStar != 0 { return s[1:] }
    //     return s }
    public String string(Addr t) {
        byte[] s = nameName(nameOff(t, t.i32(oStr)));
        if ((t.u8(oTFlag) & Images.TFLAG_EXTRA_STAR) != 0) {
            return new String(s, 1, s.length - 1, StandardCharsets.UTF_8);
        }
        return new String(s, StandardCharsets.UTF_8);
    }

    // func (t *rtype) Name() string {
    //     if !t.t.HasName() { return "" }
    //     s := t.String()
    //     i := len(s) - 1
    //     sqBrackets := 0
    //     for i >= 0 && (s[i] != '.' || sqBrackets != 0) {
    //         switch s[i] { case ']': sqBrackets++; case '[': sqBrackets-- }
    //         i-- }
    //     return s[i+1:] }
    public String name(Addr t) {
        if (!hasName(t)) {
            return "";
        }
        String s = string(t);
        int i = s.length() - 1;
        int sq = 0;
        while (i >= 0 && (s.charAt(i) != '.' || sq != 0)) {
            if (s.charAt(i) == ']') {
                sq++;
            } else if (s.charAt(i) == '[') {
                sq--;
            }
            i--;
        }
        return s.substring(i + 1);
    }

    // func (t *Type) Uncommon() *UncommonType {
    //     if t.TFlag&TFlagUncommon == 0 { return nil }
    //     switch t.Kind() {
    //     case Struct: return &(*structTypeUncommon)(unsafe.Pointer(t)).u
    //     case Pointer: type u struct { PtrType; u UncommonType }; return &(*u)(unsafe.Pointer(t)).u
    //     ... default: type u struct { Type; u UncommonType }; return &(*u)(unsafe.Pointer(t)).u } }
    // The field offset of u in each local struct is sizeof(the kind struct): a frontend constant.
    public Addr uncommon(Addr t) {
        if ((t.u8(oTFlag) & Images.TFLAG_UNCOMMON) == 0) {
            return null;
        }
        return t.add(tab.sizeof(images.kindStruct(kind(t))));
    }

    // func (t *rtype) PkgPath() string {
    //     if t.t.TFlag&abi.TFlagNamed == 0 { return "" }
    //     ut := t.uncommon()
    //     if ut == nil { return "" }
    //     return t.nameOff(ut.PkgPath).Name() }
    public String pkgPath(Addr t) {
        if (!hasName(t)) {
            return "";
        }
        Addr ut = uncommon(t);
        if (ut == null) {
            return "";
        }
        return new String(nameName(nameOff(t, ut.i32(tab.off("UncommonType.PkgPath")))), StandardCharsets.UTF_8);
    }

    // func (t *UncommonType) ExportedMethods() []Method {
    //     if t.Xcount == 0 { return nil }
    //     return (*[1 << 16]Method)(addChecked(unsafe.Pointer(t), uintptr(t.Moff), "t.xcount > 0"))[:t.Xcount:t.Xcount] }
    // Returned as (pointer to element 0, length); element i is at i*sizeof(Method).
    public Addr exportedMethods(Addr ut) {
        return ut.add(ut.u32(tab.off("UncommonType.Moff")));
    }

    public int xcount(Addr ut) {
        return ut == null ? 0 : (int) ut.u16(tab.off("UncommonType.Xcount"));
    }

    // func (t *rtype) NumMethod() int {
    //     if t.Kind() == Interface { tt := (*interfaceType)(unsafe.Pointer(t)); return tt.NumMethod() }
    //     return len(t.exportedMethods()) }
    public int numMethod(Addr t) {
        if (kind(t) == Images.K_INTERFACE) {
            return (int) t.u64(tab.off("InterfaceType.Methods") + 8);
        }
        return xcount(uncommon(t));
    }

    /** {@code t.Method(i).Name}: nameOff of ExportedMethods()[i].Name or of InterfaceType.Methods[i].Name. */
    public String methodName(Addr t, int i) {
        int off;
        if (kind(t) == Images.K_INTERFACE) {
            Addr ms = t.ptr(tab.off("InterfaceType.Methods"));
            off = ms.add((long) i * tab.sizeof("Imethod")).i32(tab.off("Imethod.Name"));
        } else {
            Addr ut = uncommon(t);
            off = exportedMethods(ut).add((long) i * tab.sizeof("Method")).i32(tab.off("Method.Name"));
        }
        return new String(nameName(nameOff(t, off)), StandardCharsets.UTF_8);
    }

    // func (t *Type) Elem() *Type {
    //     switch t.Kind() {
    //     case Array: tt := (*ArrayType)(unsafe.Pointer(t)); return tt.Elem
    //     case Chan: ...; case Map: ...; case Pointer: ...; case Slice: ... }
    //     return nil }
    public Addr elem(Addr t) {
        return switch (kind(t)) {
            case Images.K_ARRAY -> t.ptr(tab.off("ArrayType.Elem"));
            case Images.K_CHAN -> t.ptr(tab.off("ChanType.Elem"));
            case Images.K_MAP -> t.ptr(tab.off("MapType.Elem"));
            case Images.K_POINTER -> t.ptr(tab.off("PtrType.Elem"));
            case Images.K_SLICE -> t.ptr(tab.off("SliceType.Elem"));
            default -> null;
        };
    }

    // func (t *Type) Key() *Type { if t.Kind() == Map { return (*MapType)(unsafe.Pointer(t)).Key }; return nil }
    public Addr key(Addr t) {
        return kind(t) == Images.K_MAP ? t.ptr(tab.off("MapType.Key")) : null;
    }

    // func (t *Type) Len() int { if t.Kind() == Array { return int((*ArrayType)(unsafe.Pointer(t)).Len) }; return 0 }
    public long len(Addr t) {
        return kind(t) == Images.K_ARRAY ? t.u64(tab.off("ArrayType.Len")) : 0;
    }

    // func (t *Type) ChanDir() ChanDir { if t.Kind() == Chan { ch := (*ChanType)(unsafe.Pointer(t)); return ch.Dir }; return InvalidDir }
    public long chanDir(Addr t) {
        return kind(t) == Images.K_CHAN ? t.u64(tab.off("ChanType.Dir")) : 0;
    }

    // func (t *FuncType) NumIn() int { return int(t.InCount) }
    public int numIn(Addr t) {
        return (int) t.u16(tab.off("FuncType.InCount"));
    }

    // func (t *FuncType) NumOut() int { return int(t.OutCount & (1<<15 - 1)) }
    public int numOut(Addr t) {
        return (int) (t.u16(tab.off("FuncType.OutCount")) & ((1 << 15) - 1));
    }

    // func (t *FuncType) IsVariadic() bool { return t.OutCount&(1<<15) != 0 }
    public boolean isVariadic(Addr t) {
        return (t.u16(tab.off("FuncType.OutCount")) & (1 << 15)) != 0;
    }

    // func (t *FuncType) InSlice() []*Type {
    //     uadd := unsafe.Sizeof(*t)
    //     if t.TFlag&TFlagUncommon != 0 { uadd += unsafe.Sizeof(UncommonType{}) }
    //     if t.InCount == 0 { return nil }
    //     return (*[1 << 16]*Type)(addChecked(unsafe.Pointer(t), uadd, "t.inCount > 0"))[:t.InCount:t.InCount] }
    private Addr params(Addr t) {
        long uadd = tab.sizeof("FuncType");
        if ((t.u8(oTFlag) & Images.TFLAG_UNCOMMON) != 0) {
            uadd += tab.sizeof("UncommonType");
        }
        return t.add(uadd);
    }

    // func (t *FuncType) In(i int) *Type { return t.InSlice()[i] }
    public Addr in(Addr t, int i) {
        return params(t).ptr(8L * i);
    }

    // func (t *FuncType) Out(i int) *Type { return (t.OutSlice()[i]) }   // OutSlice = same array [InCount : InCount+outCount]
    public Addr out(Addr t, int i) {
        return params(t).ptr(8L * (numIn(t) + i));
    }

    // tt := (*structType)(unsafe.Pointer(t)); len(tt.Fields)
    public int numField(Addr t) {
        return (int) t.u64(tab.off("StructType.Fields") + 8);
    }

    // field := &tt.Fields[i]
    public Addr field(Addr t, int i) {
        int n = numField(t);
        if (i < 0 || i >= n) {
            throw new IndexOutOfBoundsException("reflect: Field index out of range");
        }
        return t.ptr(tab.off("StructType.Fields")).add((long) i * tab.sizeof("StructField"));
    }

    public Addr fieldName(Addr f) {
        return f.ptr(tab.off("StructField.Name"));
    }

    public Addr fieldType(Addr f) {
        return f.ptr(tab.off("StructField.Typ"));
    }

    public long fieldOffset(Addr f) {
        return f.u64(tab.off("StructField.Offset"));
    }
}
