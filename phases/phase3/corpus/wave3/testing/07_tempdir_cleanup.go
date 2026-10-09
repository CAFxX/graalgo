// 07_tempdir_cleanup: TempDir creates unique dirs (existence checked via
// os.Stat, paths never printed), parent TempDir removed at cleanup,
// Setenv restores, Cleanup LIFO via subtests.
package main

import (
	"fmt"
	"os"
	"testing"
)

func TestTempDir(t *testing.T) {
	d1 := t.TempDir()
	d2 := t.TempDir()
	if d1 == d2 {
		t.Error([]any{"TempDir not unique"}...)
	}
	for i, d := range []string{d1, d2} {
		fi, err := os.Stat(d)
		if err != nil || !fi.IsDir() {
			t.Errorf("TempDir %d missing: %v", []any{i, err}...)
		}
	}
	// Write a file; it must vanish with cleanup.
	if err := os.WriteFile(d1+"/f.txt", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fmt.Println("TEMPDIR-OK")
	t.Cleanup(func() {
		if _, err := os.Stat(d1); !os.IsNotExist(err) {
			fmt.Println("BAD: tempdir survived cleanup")
		} else {
			fmt.Println("TEMPDIR-CLEANED")
		}
	})
}

func TestSetenv(t *testing.T) {
	os.Setenv("CORPUS_TEST_VAR", "orig")
	t.Setenv("CORPUS_TEST_VAR", "changed")
	if got := os.Getenv("CORPUS_TEST_VAR"); got != "changed" {
		t.Errorf("Setenv: got %q", []any{got}...)
	}
	fmt.Println("SETENV-OK")
	t.Cleanup(func() {
		if got := os.Getenv("CORPUS_TEST_VAR"); got != "orig" {
			fmt.Println("BAD: setenv not restored:", got)
		} else {
			fmt.Println("SETENV-RESTORED")
		}
	})
}

func TestHelper(t *testing.T) {
	helper(t)
	fmt.Println("HELPER-OK")
}

func helper(t *testing.T) {
	t.Helper()
	t.Log([]any{"from helper"}...)
}

func main() {
	tests := []testing.InternalTest{
		{"TestTempDir", TestTempDir},
		{"TestSetenv", TestSetenv},
		{"TestHelper", TestHelper},
	}
	testing.Main(nil, tests, nil, nil)
}

