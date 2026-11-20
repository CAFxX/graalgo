package main

// L11 (REVIEW-2026-11-18-05): debug/elf — zero corpus imports before
// this program. ELF header of the fixed system binary /bin/true: class,
// data encoding, type, machine, entry, section names (sorted) and program
// header count. Also the error path on a non-ELF file.

import (
	"debug/elf"
	"fmt"
	"sort"
)

func main() {
	f, err := elf.Open("/bin/true")
	if err != nil {
		fmt.Printf("open-err=%v\n", err)
		return
	}
	defer f.Close()
	h := f.FileHeader
	fmt.Printf("class=%v data=%v version=%v osabi=%v type=%v machine=%v entry=%#x\n",
		h.Class, h.Data, h.Version, h.OSABI, h.Type, h.Machine, h.Entry)
	fmt.Printf("sections=%d programs=%d\n", len(f.Sections), len(f.Progs))
	names := make([]string, 0, len(f.Sections))
	for _, s := range f.Sections {
		names = append(names, s.Name)
	}
	sort.Strings(names)
	fmt.Printf("section-names=%q\n", names)
	// Section sizes for the two anchor sections.
	for _, want := range []string{".text", ".symtab"} {
		s := f.Section(want)
		if s == nil {
			fmt.Printf("section-%s=missing\n", want)
			continue
		}
		fmt.Printf("section-%s size=%d addralign=%d\n", want, s.Size, s.Addralign)
	}
	// Program header types.
	types := make([]string, 0, len(f.Progs))
	for _, p := range f.Progs {
		types = append(types, p.Type.String())
	}
	fmt.Printf("prog-types=%q\n", types)

	// Error path: a text file is not ELF.
	_, err = elf.Open("/etc/hostname")
	fmt.Printf("non-elf-err=%v\n", err != nil)
	_, err = elf.Open("/nonexistent")
	fmt.Printf("missing-err=%v\n", err != nil)
}
