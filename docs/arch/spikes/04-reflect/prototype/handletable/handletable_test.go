// Formulation-existence tests: every reflect unsafe-use category exercised
// through the handle table, mirroring the actual code patterns in
// src/reflect (go1.27.1). Each test names the source pattern it simulates.
package handletable

import "testing"

// TD: tt := (*abi.MapType)(unsafe.Pointer(t)); read tt.Elem.
// A descriptor is an object; the "cast" is field access at a known offset.
func TestTD_DescriptorFieldRead(t *testing.T) {
	tab := NewTable()
	desc := tab.Alloc(64, "mapdesc") // fake MapType, Elem *rtype at offset 16
	elem := tab.Alloc(32, "elemdesc")
	tab.WriteWord(tab.Add(desc, 16), uintptr(elem)) // tt.Elem = elem
	if got := Handle(tab.ReadWord(tab.Add(desc, 16))); got != elem {
		t.Fatalf("descriptor field read: got %x want %x", uint64(got), uint64(elem))
	}
}

// PA: add(p, x, whySafe) = unsafe.Pointer(uintptr(p) + x), bounds-checked.
func TestPA_Add(t *testing.T) {
	tab := NewTable()
	arr := tab.Alloc(64, "array")
	tab.WriteWord(tab.Add(arr, 24), 0xdeadbeef)
	// value.go:733 pattern: *(*unsafe.Pointer)(add(ptr, st.stkOff, "1-ptr"))
	if got := tab.ReadWord(tab.Add(arr, 24)); got != 0xdeadbeef {
		t.Fatalf("add+read: got %x", got)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected out-of-bounds Add to panic")
			}
		}()
		tab.Add(arr, 65) // past the end: the whySafe contract, enforced
	}()
}

// PA: isZero's `for uintptr(unsafe.Pointer(&b[0]))%8 != 0` answered from metadata.
func TestPA_AlignmentQuery(t *testing.T) {
	tab := NewTable()
	b := tab.Alloc(32, "bytes")
	if !tab.IsAligned8(b) {
		t.Fatal("offset 0 must be 8-aligned")
	}
	if tab.IsAligned8(tab.Add(b, 4)) {
		t.Fatal("offset 4 must not be 8-aligned")
	}
}

// IF: packEface: *(*any)(unsafe.Pointer(&EmptyInterface{typ, data})).
func TestIF_PackEface(t *testing.T) {
	tab := NewTable()
	typ := tab.Alloc(32, "rtype")
	data := tab.Alloc(16, "data")
	ef := tab.PackEface(typ, data)
	if tab.EfaceType(ef) != typ || tab.EfaceData(ef) != data {
		t.Fatal("eface round-trip failed")
	}
	// value.go:1582: (*abi.CommonInterface)(unsafe.Pointer(&out)).Data = d
	other := tab.Alloc(16, "other")
	tab.SetEfaceData(ef, other)
	if tab.EfaceData(ef) != other {
		t.Fatal("eface data word write failed")
	}
}

// IF: prototype extraction: *(**ptrType)(unsafe.Pointer(&iptr)).
func TestIF_PrototypeExtraction(t *testing.T) {
	tab := NewTable()
	proto := tab.Alloc(48, "ptrType-prototype")
	i := tab.PackEface(proto, nilHandle) // var iptr any = (*unsafe.Pointer)(nil)
	if got := tab.EfaceType(i); got != proto {
		t.Fatalf("prototype extraction: got %x want %x", uint64(got), uint64(proto))
	}
}

// RW: *(*unsafe.Pointer)(v.ptr) word read; unsafe.Slice((*byte)(p), n).
func TestRW_WordAndSlice(t *testing.T) {
	tab := NewTable()
	obj := tab.Alloc(32, "obj")
	inner := tab.Alloc(8, "inner")
	tab.WriteWord(obj, uintptr(inner)) // v.ptr -> word holding a pointer
	if got := Handle(tab.ReadWord(obj)); got != inner {
		t.Fatal("word read failed")
	}
	// value.go:1704: isZero(unsafe.Slice((*byte)(v.ptr), typ.Size()))
	h := tab.SliceFrom(obj, 32)
	if h != obj {
		t.Fatal("slice-from-handle changed identity")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected over-long SliceFrom to panic")
			}
		}()
		tab.SliceFrom(obj, 33)
	}()
	// type.go:455: copy 4 bytes between two pointers
	src := tab.Alloc(4, "nameoff")
	tab.WriteBytes(src, []byte{1, 2, 3, 4})
	dst := tab.Alloc(4, "dst")
	tab.WriteBytes(dst, tab.ReadBytes(src, 4))
	if got := tab.ReadBytes(dst, 4); string(got) != "\x01\x02\x03\x04" {
		t.Fatal("4-byte copy failed")
	}
}

// RW: float32reg bit reinterpretation *(*float32)(unsafe.Pointer(&i)).
func TestRW_BitReinterpret(t *testing.T) {
	tab := NewTable()
	cell := tab.Alloc(8, "u32")
	tab.WriteWord(cell, 0x3f800000) // bit pattern of 1.0f
	raw := tab.ReadBytes(cell, 4)
	if raw[0] != 0x00 || raw[1] != 0x00 || raw[2] != 0x80 || raw[3] != 0x3f {
		t.Fatalf("little-endian word layout wrong: %v", raw)
	}
}

// SH: (*unsafeheader.Slice)(unsafe.Pointer(&x)) header field access.
func TestSH_SliceHeader(t *testing.T) {
	tab := NewTable()
	backing := tab.Alloc(80, "backing")
	sh := tab.SliceHeader(backing, 10, 10)
	if tab.SliceData(sh) != backing || tab.SliceLen(sh) != 10 {
		t.Fatal("slice header field read failed")
	}
	// value.go:3796: contentEscapes((*unsafeheader.String)(unsafe.Pointer(&key)).Data)
	str := tab.SliceHeader(backing, 5, 5) // string header is a 2-word prefix
	if tab.SliceData(str) != backing {
		t.Fatal("string header data read failed")
	}
}

// CF: callReflect frame traffic: *(*unsafe.Pointer)(addr) = v.ptr etc.
func TestCF_FrameTraffic(t *testing.T) {
	tab := NewTable()
	frame := tab.Frame(8)
	rcvr := tab.Alloc(16, "rcvr")
	// value.go:534: *(*unsafe.Pointer)(addr) = v.ptr
	tab.WriteWord(tab.Add(frame, 2*8), uintptr(rcvr))
	// value.go:546: regArgs.Ptrs[st.ireg] = *(*unsafe.Pointer)(offset)
	if got := Handle(tab.ReadWord(tab.Add(frame, 16))); got != rcvr {
		t.Fatal("frame word round-trip failed")
	}
}

// FP: synthetic code pointers replace machine PCs.
func TestFP_SyntheticCodePointers(t *testing.T) {
	ft := NewFuncTable()
	// type.go:2316: unsafe.Pointer(FuncPCABIInternal(embeddedIfaceMethStub))
	tok := ft.Register("embeddedIfaceMethStub")
	// value.go:1145: pc := *(*uintptr)(unsafe.Pointer(&f)); FuncForPC(pc)
	if ft.Name(tok) != "embeddedIfaceMethStub" {
		t.Fatal("synthetic PC->function mapping failed")
	}
	// value.go:2634: methodValueCallCodePtr() stability across calls
	if tok2 := ft.Register("methodValueCall"); ft.Name(tok2) == ft.Name(tok) {
		t.Fatal("distinct functions must have distinct tokens")
	}
}

// ZV: unsafe.Pointer(&zeroVal[0]) canonical zero; handle equality.
func TestZV_CanonicalZero(t *testing.T) {
	tab := NewTable()
	z1, z2 := tab.Zero(), tab.Zero()
	if z1 != z2 {
		t.Fatal("zeroVal must be a canonical handle")
	}
	v := tab.Alloc(8, "v")
	if v == z1 {
		t.Fatal("distinct objects must have distinct handles")
	}
}

// AD: unsafe.Pointer(&x) for locals/globals.
func TestAD_AddressOf(t *testing.T) {
	tab := NewTable()
	x := tab.Alloc(8, "local-x")
	tab.WriteWord(x, 42)
	if tab.ReadWord(x) != 42 {
		t.Fatal("address-of local failed")
	}
}

// RC: race.ReadPC(unsafe.Pointer(m), callerpc, fn) with handle-aware race runtime.
func TestRC_RaceNote(t *testing.T) {
	tab := NewTable()
	m := tab.Alloc(64, "hmap")
	ft := NewFuncTable()
	pc := ft.Register("mapIterStart")
	note := RaceNote(m, pc, "mapIterStart")
	if note == "" {
		t.Fatal("race note failed")
	}
	t.Log(note)
}

// Pointer<->uintptr: UnsafeAddr()/Pointer() expose opaque tokens that
// round-trip (NewAt) but are not addresses.
func TestToken_RoundTrip(t *testing.T) {
	tab := NewTable()
	tt := NewTokenTable(tab)
	obj := tab.Alloc(24, "obj")
	tok := tt.TokenOf(obj) // uintptr(v.ptr)
	if tok == 0 {
		t.Fatal("non-nil handle must map to non-zero token")
	}
	back, ok := tt.HandleOf(tok) // NewAt(typ, unsafe.Pointer(tok))
	if !ok || back != obj {
		t.Fatal("token round-trip failed")
	}
	if tt.TokenOf(nilHandle) != 0 {
		t.Fatal("nil handle must map to token 0")
	}
}

// TD-R: module offset resolution becomes an intrinsic on descriptors.
// resolveTypeOff(rtype, off) under the new model is "descriptor slot access",
// not blob arithmetic; model it as a table lookup keyed by (desc, off).
func TestTDR_OffsetResolution(t *testing.T) {
	tab := NewTable()
	// fake module: name blob object + type descriptor referencing it by slot
	nameBlob := tab.Alloc(64, "nameblob")
	tab.WriteBytes(nameBlob, []byte("int"))
	desc := tab.Alloc(32, "rtype")
	tab.WriteWord(desc, uintptr(nameBlob)) // name slot -> blob handle
	// resolveNameOff(desc, 0): read slot, no raw arithmetic
	if got := Handle(tab.ReadWord(desc)); got != nameBlob {
		t.Fatal("descriptor slot resolution failed")
	}
	if string(tab.ReadBytes(nameBlob, 3)) != "int" {
		t.Fatal("name bytes read failed")
	}
}
