package main

// L11 (REVIEW-2026-11-18-05): debug/gosym — zero corpus imports before
// this program. Go symbol + line tables from the running binary
// (/proc/self/exe): function count, file count, main.main lookup with
// its line-table entry, and PCToLine for a known function entry.
// Modern gc binaries carry no .gosymtab section, so the func table is
// derived from .gopclntab alone via NewTable(nil, lt) — the canonical
// form used by the stdlib's own pclntab tests.

import (
	"debug/elf"
	"debug/gosym"
	"fmt"
	"path/filepath"
	"sort"
)

func main() {
	f, err := elf.Open("/proc/self/exe")
	if err != nil {
		fmt.Printf("elf-err=%v\n", err)
		return
	}
	defer f.Close()
	textSec := f.Section(".text")
	if textSec == nil {
		fmt.Println("no-text-section")
		return
	}
	// Modern gc binaries have no .gosymtab section; record that and
	// derive the table from .gopclntab alone.
	fmt.Printf("gosymtab-present=%v\n", f.Section(".gosymtab") != nil)
	pclnSec := f.Section(".gopclntab")
	if pclnSec == nil {
		fmt.Println("no-pclntab-section")
		return
	}
	pcln, err := pclnSec.Data()
	if err != nil {
		fmt.Printf("pcln-err=%v\n", err)
		return
	}
	lt := gosym.NewLineTable(pcln, textSec.Addr)
	tab, err := gosym.NewTable(nil, lt)
	if err != nil {
		fmt.Printf("table-err=%v\n", err)
		return
	}
	fmt.Printf("funcs=%d files=%d\n", len(tab.Funcs), len(tab.Files))
	// main.main must be present.
	fn := tab.LookupFunc("main.main")
	if fn == nil {
		fmt.Println("main-main=missing")
		return
	}
	fmt.Printf("main-main-found=true\n")
	file, line, fn2 := tab.PCToLine(fn.Entry)
	fmt.Printf("main-main-file=%s line=%d fn=%s\n",
		filepath.Base(file), line, fn2.Name)
	// Sorted first five function names (stable pins).
	names := make([]string, 0, len(tab.Funcs))
	for _, fn := range tab.Funcs {
		names = append(names, fn.Name)
	}
	sort.Strings(names)
	if len(names) > 5 {
		names = names[:5]
	}
	fmt.Printf("first-funcs=%q\n", names)
	// LineToPC round-trip on the main.main source line.
	pc, fn3, err := tab.LineToPC(file, line)
	fmt.Printf("linetopc-err=%v pc-match=%v fn=%v\n", err != nil, pc == fn.Entry, fn3 != nil && fn3.Name == "main.main")
	// Unknown function lookup returns nil.
	fmt.Printf("lookup-missing=%v\n", tab.LookupFunc("nosuchpackage.nosuchfunc") == nil)
}
