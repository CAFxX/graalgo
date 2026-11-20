package main

// L11 (REVIEW-2026-11-18-05): debug/macho — zero corpus imports before
// this program. A minimal 64-bit Mach-O file built in-memory (header +
// one LC_SEGMENT_64 command): header fields, load-command decoding,
// segment name/flags. Also the error path on a non-Mach-O file.

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"fmt"
)

func buildMachO64() []byte {
	buf := make([]byte, 0, 256)
	put := func(v uint32) {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], v)
		buf = append(buf, b[:]...)
	}
	// FileHeader (32 bytes): magic, cputype, subcpu, type, ncmd, cmdsz, flags, reserved.
	put(0xfeedfacf) // MH_MAGIC_64
	put(0x01000007) // CPU_TYPE_X86_64
	put(3)          // CPU_SUBTYPE_X86_64_ALL
	put(2)          // MH_EXECUTE
	put(1)          // Ncmd
	put(72)         // Cmdsz: one LC_SEGMENT_64
	put(0)          // Flags
	put(0)          // Reserved
	// LC_SEGMENT_64 (72 bytes).
	put(0x19) // LC_SEGMENT_64
	put(72)   // Cmdsize
	segname := make([]byte, 16)
	copy(segname, "__TEXT")
	buf = append(buf, segname...)
	put64 := func(v uint64) {
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], v)
		buf = append(buf, b[:]...)
	}
	put64(0x100000000) // Vmaddr
	put64(0x1000)      // Vmsize
	put64(0)           // Fileoff
	put64(0)           // Filesize
	put(7)             // Maxprot
	put(5)             // Initprot
	put(0)             // Nsects
	put(0)             // Flags
	return buf
}

func main() {
	data := buildMachO64()
	f, err := macho.NewFile(bytes.NewReader(data))
	if err != nil {
		fmt.Printf("new-err=%v\n", err)
		return
	}
	h := f.FileHeader
	fmt.Printf("magic=%#x cpu=%v subcpu=%d type=%v ncmd=%d cmdsz=%d flags=%#x\n",
		h.Magic, h.Cpu, h.SubCpu, h.Type, h.Ncmd, h.Cmdsz, h.Flags)
	fmt.Printf("sections=%d\n", len(f.Sections))
	for _, s := range f.Sections {
		fmt.Printf("section=%s\n", s.Name)
	}
	fmt.Printf("loads=%d\n", len(f.Loads))
	for _, l := range f.Loads {
		if seg, ok := l.(*macho.Segment); ok {
			fmt.Printf("segment=%s addr=%#x memsz=%#x filesz=%#x nsect=%d\n",
				seg.Name, seg.Addr, seg.Memsz, seg.Filesz, seg.Nsect)
		} else {
			fmt.Printf("load=%T\n", l)
		}
	}
	seg := f.Segment("__TEXT")
	fmt.Printf("segment-lookup=%v\n", seg != nil)

	// Error path: an ELF file is not Mach-O.
	_, err = macho.Open("/bin/true")
	fmt.Printf("elf-err=%v\n", err != nil)
	_, err = macho.Open("/nonexistent")
	fmt.Printf("missing-err=%v\n", err != nil)
}
