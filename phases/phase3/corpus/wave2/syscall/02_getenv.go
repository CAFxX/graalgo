package main

import (
	"fmt"
	"syscall"
)

// sc02: env family — Getenv/Setenv/Unsetenv/Clearenv/Environ round-trip
// on a unique key, plus the gc validation rules (empty/'='/NUL key and
// NUL in value -> EINVAL).
func main() {
	v, ok := syscall.Getenv("PATH")
	fmt.Println(ok, len(v) > 0)
	_, ok = syscall.Getenv("GRAALGO_M8_DEFINITELY_ABSENT_VAR")
	fmt.Println(ok)
	_, ok = syscall.Getenv("")
	fmt.Println(ok)
	fmt.Println(syscall.Setenv("GRAALGO_M8_SYS_TEST", "hello") == nil)
	v, ok = syscall.Getenv("GRAALGO_M8_SYS_TEST")
	fmt.Println(ok, v == "hello")
	// Overwrite keeps one entry.
	fmt.Println(syscall.Setenv("GRAALGO_M8_SYS_TEST", "world") == nil)
	v, ok = syscall.Getenv("GRAALGO_M8_SYS_TEST")
	fmt.Println(ok, v == "world")
	fmt.Println(syscall.Unsetenv("GRAALGO_M8_SYS_TEST") == nil)
	_, ok = syscall.Getenv("GRAALGO_M8_SYS_TEST")
	fmt.Println(ok)
	// Validation mirrors gc exactly.
	fmt.Println(syscall.Setenv("", "x") == syscall.EINVAL)
	fmt.Println(syscall.Setenv("GRAALGO_M8_SYS_TEST", "a\x00b") == syscall.EINVAL)
	fmt.Println(syscall.Setenv("A=B", "x") == syscall.EINVAL)
	fmt.Println(syscall.Setenv("A\x00B", "x") == syscall.EINVAL)
	// Environ contains PATH= and the round-tripped key.
	fmt.Println(syscall.Setenv("GRAALGO_M8_SYS_TEST2", "v2") == nil)
	foundPath, foundKey := false, false
	for _, kv := range syscall.Environ() {
		if len(kv) > 5 && kv[:5] == "PATH=" {
			foundPath = true
		}
		if kv == "GRAALGO_M8_SYS_TEST2=v2" {
			foundKey = true
		}
	}
	fmt.Println(foundPath, foundKey)
	fmt.Println(syscall.Unsetenv("GRAALGO_M8_SYS_TEST2") == nil)
}
