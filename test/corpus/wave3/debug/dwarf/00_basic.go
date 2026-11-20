package main

// L11 (REVIEW-2026-11-18-05): debug/dwarf — zero corpus imports before
// this program. DWARF from the running binary (/proc/self/exe via
// debug/elf): entry count by tag, plus the error path on a stripped
// system binary (/bin/true has no DWARF).

import (
	"debug/dwarf"
	"debug/elf"
	"fmt"
	"sort"
)

func main() {
	f, err := elf.Open("/proc/self/exe")
	if err != nil {
		fmt.Printf("elf-err=%v\n", err)
		return
	}
	defer f.Close()
	dw, err := f.DWARF()
	if err != nil {
		fmt.Printf("dwarf-err=%v\n", err)
		return
	}
	r := dw.Reader()
	byTag := map[dwarf.Tag]int{}
	total := 0
	for {
		e, err := r.Next()
		if err != nil {
			fmt.Printf("reader-err=%v\n", err)
			return
		}
		if e == nil {
			break
		}
		byTag[e.Tag]++
		total++
	}
	fmt.Printf("entries=%d\n", total)
	tags := make([]string, 0, len(byTag))
	for t, n := range byTag {
		tags = append(tags, fmt.Sprintf("%s=%d", t, n))
	}
	sort.Strings(tags)
	fmt.Printf("by-tag=%q\n", tags)
	// A named compile unit exists.
	r.Seek(0)
	e, _ := r.Next()
	fmt.Printf("first-tag=%v offset=%d\n", e.Tag, e.Offset)

	// Error path: stripped binary has no DWARF.
	f2, err := elf.Open("/bin/true")
	if err != nil {
		fmt.Printf("true-elf-err=%v\n", err)
		return
	}
	defer f2.Close()
	_, err = f2.DWARF()
	fmt.Printf("stripped-dwarf-err=%v\n", err != nil)
}
