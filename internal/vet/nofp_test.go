package vet

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNoFalsePositives(t *testing.T) {
	src := `package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

func main() {
	fmt.Printf("%d %s\n", 42, "hello")
	fmt.Printf("value: %v\n", 3.14)
	s := string(rune('a'))
	_ = s
	t := time.Now()
	fmt.Println(t.Format("2006-01-02"))
}

func goodCancel() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	<-ctx.Done()
}

var mu sync.Mutex

func goodLock() {
	mu.Lock()
	defer mu.Unlock()
}

type T struct{ A, B int }

func keyed() {
	_ = T{A: 1, B: 2}
}

func TestGood(t *testing.T) {
	t.Log("ok")
}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "good.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	ds, err := Run([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) > 0 {
		for _, d := range ds {
			t.Errorf("false positive: %s: %s", d.Pos, d.Message)
		}
	}
}
