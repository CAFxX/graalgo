package main

import (
	"fmt"
	"synthcpu"
)

// SCOPE-LIMITED + NOGC: external programs cannot import internal/* (go
// tool restriction). No synthetic adapter exists: feature detection needs
// cpuid/hwcap intrinsics no synthetic package can provide honestly, and
// there is no in-tree consumer. API-surface pin, verified by building
// against the real package in the tier1d harness.
func main() {
	fmt.Println(cpu.CacheLineSize > 0)
	// The arch records exist and are addressable; values are host-dependent
	// and are not pinned (no honest synthetic values exist).
	_ = cpu.X86
	_ = cpu.ARM64
	fmt.Println("ok")
}
