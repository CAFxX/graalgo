package main

// L11 (REVIEW-2026-11-18-05): debug/pe — zero corpus imports before
// this program. A minimal PE32+ file built in-memory (DOS stub + COFF
// header + optional header + one .text section): header fields, section
// listing, imported-symbol handling on an empty import table. Also the
// error path on a non-PE file.

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"fmt"
)

func buildPE() []byte {
	buf := make([]byte, 0, 512)
	// DOS header: "MZ" + e_lfanew at 0x3c pointing at 0x40.
	dos := make([]byte, 0x40)
	dos[0], dos[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(dos[0x3c:], 0x40)
	buf = append(buf, dos...)
	// PE signature.
	buf = append(buf, 'P', 'E', 0, 0)
	// COFF FileHeader (20 bytes).
	coff := make([]byte, 20)
	binary.LittleEndian.PutUint16(coff[0:], 0x8664)  // Machine AMD64
	binary.LittleEndian.PutUint16(coff[2:], 1)       // NumberOfSections
	binary.LittleEndian.PutUint32(coff[4:], 0)       // TimeDateStamp
	binary.LittleEndian.PutUint32(coff[8:], 0)       // PointerToSymbolTable
	binary.LittleEndian.PutUint32(coff[12:], 0)      // NumberOfSymbols
	binary.LittleEndian.PutUint16(coff[16:], 240)    // SizeOfOptionalHeader
	binary.LittleEndian.PutUint16(coff[18:], 0x2022) // Characteristics
	buf = append(buf, coff...)
	// OptionalHeader PE32+ (240 bytes): Magic 0x20b, rest zero.
	opt := make([]byte, 240)
	binary.LittleEndian.PutUint16(opt[0:], 0x20b)
	buf = append(buf, opt...)
	// One section header (40 bytes): .text.
	sect := make([]byte, 40)
	copy(sect[0:8], ".text")
	binary.LittleEndian.PutUint32(sect[8:], 0x10) // VirtualSize
	binary.LittleEndian.PutUint32(sect[12:], 0x1000)
	binary.LittleEndian.PutUint32(sect[16:], 0x10)  // SizeOfRawData
	binary.LittleEndian.PutUint32(sect[20:], 0x200) // PointerToRawData
	buf = append(buf, sect...)
	// Raw section data at 0x200 (pad).
	for len(buf) < 0x200 {
		buf = append(buf, 0)
	}
	buf = append(buf, []byte("pe-text-payload!")...)
	return buf
}

func main() {
	data := buildPE()
	f, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		fmt.Printf("new-err=%v\n", err)
		return
	}
	h := f.FileHeader
	fmt.Printf("machine=%#x sections=%d timestamp=%d symbols=%d optsize=%d chars=%#x\n",
		h.Machine, h.NumberOfSections, h.TimeDateStamp, h.NumberOfSymbols,
		h.SizeOfOptionalHeader, h.Characteristics)
	oh, ok := f.OptionalHeader.(*pe.OptionalHeader64)
	fmt.Printf("optional64=%v magic=%#x\n", ok, oh.Magic)
	fmt.Printf("sections=%d\n", len(f.Sections))
	for _, s := range f.Sections {
		fmt.Printf("section=%s vsize=%d vaddr=%#x rawsize=%d\n",
			s.Name, s.VirtualSize, s.VirtualAddress, s.Size)
		d, err := s.Data()
		fmt.Printf("section-data=%q err=%v\n", string(d), err)
	}
	syms, err := f.ImportedSymbols()
	fmt.Printf("imported-symbols=%v err=%v\n", syms, err)

	// Error path: an ELF file is not PE.
	_, err = pe.Open("/bin/true")
	fmt.Printf("elf-err=%v\n", err != nil)
	_, err = pe.Open("/nonexistent")
	fmt.Printf("missing-err=%v\n", err != nil)
}
