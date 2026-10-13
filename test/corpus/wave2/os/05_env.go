// 05_env: Setenv/Getenv/LookupEnv/Unsetenv/Environ/Clearenv.
package main

import (
	"fmt"
	"os"
	"strings"
)

func check(err error) {
	if err != nil {
		fmt.Println("FAIL:", err)
		os.Exit(1)
	}
}

func main() {
	const key = "GRAALGO_OS_W2_TEST"
	check(os.Setenv(key, "v1"))
	if got := os.Getenv(key); got != "v1" {
		fmt.Println("FAIL: getenv =", got)
		os.Exit(1)
	}
	v, ok := os.LookupEnv(key)
	if !ok || v != "v1" {
		fmt.Println("FAIL: lookupenv =", v, ok)
		os.Exit(1)
	}
	check(os.Unsetenv(key))
	if _, ok := os.LookupEnv(key); ok {
		fmt.Println("FAIL: unsetenv")
		os.Exit(1)
	}

	// Environ contains a key we set.
	check(os.Setenv(key, "v2"))
	found := false
	for _, kv := range os.Environ() {
		if kv == key+"=v2" {
			found = true
		}
		if strings.Contains(kv, "\x00") {
			fmt.Println("FAIL: environ entry has NUL")
			os.Exit(1)
		}
	}
	if !found {
		fmt.Println("FAIL: environ missing key")
		os.Exit(1)
	}

	// Clearenv hides everything, including the real environment.
	os.Clearenv()
	if _, ok := os.LookupEnv(key); ok {
		fmt.Println("FAIL: clearenv overlay")
		os.Exit(1)
	}
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "PATH=") {
			fmt.Println("FAIL: clearenv real env visible")
			os.Exit(1)
		}
	}
	fmt.Println("ok")
}
