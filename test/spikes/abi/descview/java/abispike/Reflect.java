package abispike;

import abispike.Mem.Addr;
import abispike.Values.Iface;
import abispike.Values.IfaceCell;
import java.nio.charset.StandardCharsets;

/**
 * go1.27.1 {@code reflect.Value} accessors (reflect/value.go),
 * transliterated like {@link Abi}. {@code Value} is the Go struct
 * {@code {typ_ *abi.Type; ptr unsafe.Pointer; flag}}.
 */
public final class Reflect {
    static final long FLAG_KIND_MASK = (1 << 5) - 1;
    static final long FLAG_STICKY_RO = 1 << 5;
    static final long FLAG_EMBED_RO = 1 << 6;
    static final long FLAG_INDIR = 1 << 7;
    static final long FLAG_ADDR = 1 << 8;
    static final long FLAG_RO = FLAG_STICKY_RO | FLAG_EMBED_RO;

    public record Value(Addr typ, Addr ptr, long flag) {
        int kind() {
            return (int) (flag & FLAG_KIND_MASK);
        }
    }

    private final Abi abi;

    public Reflect(Abi abi) {
        this.abi = abi;
    }

    // func ValueOf(i any) Value { if i == nil { return Value{} }; return unpackEface(i) }
    // func unpackEface(i any) Value {
    //     e := (*abi.EmptyInterface)(unsafe.Pointer(&i))
    //     t := e.Type
    //     if t == nil { return Value{} }
    //     f := flag(t.Kind())
    //     if !t.IsDirectIface() { f |= flagIndir }
    //     return Value{t, e.Data, f} }
    public Value valueOf(Iface i) {
        Addr e = new Addr(new IfaceCell(i), 0); // &i, viewed as *abi.EmptyInterface
        Addr t = e.ptr(0);                       // e.Type
        if (t == null) {
            return new Value(null, null, 0);
        }
        long f = abi.kind(t);
        if (!abi.isDirectIface(t)) {
            f |= FLAG_INDIR;
        }
        return new Value(t, e.ptr(8), f);        // e.Data
    }

    // func (v Value) Elem() Value {  ... case Pointer:
    //     ptr := v.ptr
    //     if v.flag&flagIndir != 0 { ...; ptr = *(*unsafe.Pointer)(ptr) }
    //     if ptr == nil { return Value{} }
    //     tt := (*ptrType)(unsafe.Pointer(v.typ()))
    //     typ := tt.Elem
    //     fl := v.flag&flagRO | flagIndir | flagAddr
    //     fl |= flag(typ.Kind())
    //     return Value{typ, ptr, fl} }
    public Value elem(Value v) {
        if (v.kind() != Images.K_POINTER) {
            throw new IllegalStateException("reflect: call of reflect.Value.Elem on non-pointer Value");
        }
        Addr ptr = v.ptr;
        if ((v.flag & FLAG_INDIR) != 0) {
            ptr = ptr.ptr(0);
        }
        if (ptr == null) {
            return new Value(null, null, 0);
        }
        Addr typ = abi.elem(v.typ);
        return new Value(typ, ptr, v.flag & FLAG_RO | FLAG_INDIR | FLAG_ADDR | abi.kind(typ));
    }

    // func (v Value) Field(i int) Value {
    //     if v.kind() != Struct { panic(&ValueError{"reflect.Value.Field", v.kind()}) }
    //     tt := (*structType)(unsafe.Pointer(v.typ()))
    //     if uint(i) >= uint(len(tt.Fields)) { panic("reflect: Field index out of range") }
    //     field := &tt.Fields[i]
    //     typ := field.Typ
    //     fl := v.flag&(flagStickyRO|flagIndir|flagAddr) | flag(typ.Kind())
    //     if !field.Name.IsExported() { if field.Embedded() { fl |= flagEmbedRO } else { fl |= flagStickyRO } }
    //     ...
    //     ptr := add(v.ptr, field.Offset, "same as non-reflect &v.field")
    //     return Value{typ, ptr, fl} }
    public Value field(Value v, int i) {
        if (v.kind() != Images.K_STRUCT) {
            throw new IllegalStateException("reflect: call of reflect.Value.Field on non-struct Value");
        }
        Addr field = abi.field(v.typ, i);
        Addr typ = abi.fieldType(field);
        long fl = v.flag & (FLAG_STICKY_RO | FLAG_INDIR | FLAG_ADDR) | abi.kind(typ);
        Addr name = abi.fieldName(field);
        if (!Abi.nameIsExported(name)) {
            fl |= Abi.nameIsEmbedded(name) ? FLAG_EMBED_RO : FLAG_STICKY_RO;
        }
        Addr ptr = v.ptr.add(abi.fieldOffset(field));
        return new Value(typ, ptr, fl);
    }

    // func (v Value) Index(i int) Value { switch v.kind() { case Array:
    //     tt := (*arrayType)(unsafe.Pointer(v.typ()))
    //     if uint(i) >= uint(tt.Len) { panic("reflect: array index out of range") }
    //     typ := tt.Elem
    //     offset := uintptr(i) * typ.Size()
    //     val := add(v.ptr, offset, "same as &v[i], i < tt.len")
    //     fl := v.flag&(flagIndir|flagAddr) | v.flag.ro() | flag(typ.Kind())
    //     return Value{typ, val, fl}
    public Value index(Value v, int i) {
        if (v.kind() != Images.K_ARRAY) {
            throw new IllegalStateException("prototype: Index transliterated for arrays only");
        }
        if (i < 0 || i >= abi.len(v.typ)) {
            throw new IndexOutOfBoundsException("reflect: array index out of range");
        }
        Addr typ = abi.elem(v.typ);
        Addr val = v.ptr.add(i * abi.size(typ));
        long ro = (v.flag & FLAG_RO) != 0 ? FLAG_STICKY_RO : 0;
        return new Value(typ, val, v.flag & (FLAG_INDIR | FLAG_ADDR) | ro | abi.kind(typ));
    }

    // func (v Value) Int() int64 { k := v.kind(); p := v.ptr; switch k {
    //     case Int: return int64(*(*int)(p)); case Int8: return int64(*(*int8)(p)); case Int16: ...
    //     case Int32: ...; case Int64: return *(*int64)(p) }; panic(&ValueError{"reflect.Value.Int", v.kind()}) }
    public long intOf(Value v) {
        Addr p = v.ptr;
        return switch (v.kind()) {
            case 2, 6 -> p.u64(0);
            case 3 -> (byte) p.u8(0);
            case 4 -> (short) p.u16(0);
            case 5 -> p.i32(0);
            default -> throw new IllegalStateException("reflect: call of reflect.Value.Int on kind " + v.kind());
        };
    }

    // func (v Value) Uint() uint64 { ... case Uint: return uint64(*(*uint)(p)); case Uint8: ...; case Uintptr: ... }
    public long uintOf(Value v) {
        Addr p = v.ptr;
        return switch (v.kind()) {
            case 7, 11, 12 -> p.u64(0);
            case 8 -> p.u8(0);
            case 9 -> p.u16(0);
            case 10 -> p.u32(0);
            default -> throw new IllegalStateException("reflect: call of reflect.Value.Uint on kind " + v.kind());
        };
    }

    // func (v Value) Float() float64 { k := v.kind(); switch k {
    //     case Float32: return float64(*(*float32)(v.ptr)); case Float64: return *(*float64)(v.ptr) } ... }
    public double floatOf(Value v) {
        return switch (v.kind()) {
            case 13 -> Float.intBitsToFloat(v.ptr.i32(0));
            case 14 -> Double.longBitsToDouble(v.ptr.u64(0));
            default -> throw new IllegalStateException("reflect: call of reflect.Value.Float on kind " + v.kind());
        };
    }

    // func (v Value) Bool() bool { ...; return *(*bool)(v.ptr) }
    public boolean boolOf(Value v) {
        if (v.kind() != 1) {
            throw new IllegalStateException("reflect: call of reflect.Value.Bool on kind " + v.kind());
        }
        return v.ptr.u8(0) != 0;
    }

    // func (v Value) String() string { if v.kind() == String { return *(*string)(v.ptr) }; return v.stringNonString() }
    // The load of a string through a pointer reads the two header words.
    public String stringOf(Value v) {
        if (v.kind() != Images.K_STRING) {
            throw new IllegalStateException("prototype: stringNonString not transliterated");
        }
        Addr data = v.ptr.ptr(0);
        int len = (int) v.ptr.u64(8);
        byte[] out = new byte[len];
        for (int i = 0; i < len; i++) {
            out[i] = (byte) data.u8(i);
        }
        return new String(out, StandardCharsets.UTF_8);
    }

    // func (v Value) Len() int { if v.kind() == Slice { return (*unsafeheader.Slice)(v.ptr).Len }; return v.lenNonSlice() }
    // lenNonSlice: case String: return (*unsafeheader.String)(v.ptr).Len
    public long lenOf(Value v) {
        return switch (v.kind()) {
            case Images.K_SLICE, Images.K_STRING -> v.ptr.u64(8);
            case Images.K_ARRAY -> abi.len(v.typ);
            default -> throw new IllegalStateException("prototype: Len on kind " + v.kind());
        };
    }

    // func (f flag) mustBeAssignable() { if f&flagRO != 0 || f == 0 { f.mustBeAssignableSlow() }
    //     if f&flagAddr == 0 { panic("reflect: " + valueMethodName() + " using unaddressable value") } }
    private static void mustBeAssignable(Value v) {
        if ((v.flag & FLAG_RO) != 0) {
            throw new IllegalStateException("reflect: Set using value obtained using unexported field");
        }
        if ((v.flag & FLAG_ADDR) == 0) {
            throw new IllegalStateException("reflect: Set using unaddressable value");
        }
    }

    // func (v Value) SetInt(x int64) { v.mustBeAssignable(); switch k := v.kind(); k {
    //     case Int: *(*int)(v.ptr) = int(x); case Int8: *(*int8)(v.ptr) = int8(x); ... case Int64: *(*int64)(v.ptr) = x } }
    public void setInt(Value v, long x) {
        mustBeAssignable(v);
        int size = switch (v.kind()) {
            case 2, 6 -> 8;
            case 3 -> 1;
            case 4 -> 2;
            case 5 -> 4;
            default -> throw new IllegalStateException("reflect: call of reflect.Value.SetInt on kind " + v.kind());
        };
        v.ptr.base().storeBits(v.ptr.off(), size, x);
    }

    // func (v Value) SetFloat(x float64) { v.mustBeAssignable(); switch k := v.kind(); k {
    //     case Float32: *(*float32)(v.ptr) = float32(x); case Float64: *(*float64)(v.ptr) = x } }
    public void setFloat(Value v, double x) {
        mustBeAssignable(v);
        switch (v.kind()) {
            case 13 -> v.ptr.base().storeBits(v.ptr.off(), 4, Float.floatToRawIntBits((float) x));
            case 14 -> v.ptr.base().storeBits(v.ptr.off(), 8, Double.doubleToRawLongBits(x));
            default -> throw new IllegalStateException("reflect: call of reflect.Value.SetFloat on kind " + v.kind());
        }
    }
}
