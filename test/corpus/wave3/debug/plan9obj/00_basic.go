package main

// L11 (REVIEW-2026-11-18-05): debug/plan9obj — zero corpus imports
// before this program. A minimal Plan 9 386 a.out file built in-memory
// (big-endian header + text/data sections): header fields, section
// names/sizes/offsets, section data. Also the error path on a bad magic.

import (
	"bytes"
	"debug/plan9obj"
	"encoding/binary"
	"fmt"
)

func buildPlan9() []byte {
	buf := make([]byte, 0, 64)
	put32 := func(v uint32) {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], v)
		buf = append(buf, b[:]...)
	}
	// Header (32 bytes, big-endian): magic, text, data, bss, syms, entry, spsz, pcsz.
	put32(491) // Magic386 = (4*11+0)*11+7
	put32(16)  // text size
	put32(8)   // data size
	put32(0x324)
	put32(0)                                         // syms size
	put32(0x14)                                      // entry
	put32(0)                                         // spsz
	put32(0)                                         // pcsz
	buf = append(buf, []byte("0123456789abcdef")...) // text (16)
	buf = append(buf, []byte("data-data!")[:8]...)   // data (8)
	return buf
}

func main() {
	data := buildPlan9()
	f, err := plan9obj.NewFile(bytes.NewReader(data))
	if err != nil {
		fmt.Printf("new-err=%v\n", err)
		return
	}
	h := f.FileHeader
	fmt.Printf("magic=%d bss=%#x entry=%#x ptrsize=%d loadaddr=%#x hdrsize=%d\n",
		h.Magic, h.Bss, h.Entry, h.PtrSize, h.LoadAddress, h.HdrSize)
	fmt.Printf("sections=%d\n", len(f.Sections))
	for _, s := range f.Sections {
		fmt.Printf("section=%s size=%d offset=%d\n", s.Name, s.Size, s.Offset)
	}
	text := f.Section("text")
	if text != nil {
		d, err := text.Data()
		fmt.Printf("text-data=%q err=%v\n", string(d), err)
	}
	dataSec := f.Section("data")
	if dataSec != nil {
		d, err := dataSec.Data()
		fmt.Printf("data-data=%q err=%v\n", string(d), err)
	}
	fmt.Printf("section-missing=%v\n", f.Section("nosuch") == nil)

	// Error path: bad magic.
	bad := buildPlan9()
	bad[0], bad[1], bad[2], bad[3] = 0, 0, 0, 0
	_, err = plan9obj.NewFile(bytes.NewReader(bad))
	fmt.Printf("badmagic-err=%v\n", err != nil)
}
