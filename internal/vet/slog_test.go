package vet

import (
	"strings"
	"testing"
)

// RV18-P1-21(4): runSlog used start=1 for every slog call, but
// slog.Log(ctx, level, msg, args...) has two leading non-key args, so the
// level was flagged as a non-string key.
func TestSlogLogSkipsLevel(t *testing.T) {
	src := `package main

import (
	"context"
	"log/slog"
)

func f(ctx context.Context) {
	slog.Log(ctx, slog.LevelInfo, "hello", "k", "v")
	slog.Info("hello", "k", "v")
	slog.InfoContext(ctx, "hello", "k", "v")
}
`
	joined := runVetOnSrc(t, src)
	if strings.Contains(joined, "key should be a string") {
		t.Errorf("false positive: slog.Log level treated as a key:\n%s", joined)
	}
	if strings.Contains(joined, "odd number") {
		t.Errorf("false positive on well-formed slog calls:\n%s", joined)
	}
}

func TestSlogLogOddArgs(t *testing.T) {
	src := `package main

import (
	"context"
	"log/slog"
)

func f(ctx context.Context) {
	slog.Log(ctx, slog.LevelInfo, "hello", "k")
	slog.Info("hello", "k")
}
`
	joined := runVetOnSrc(t, src)
	if n := strings.Count(joined, "odd number of key-value arguments"); n != 2 {
		t.Errorf("expected 2 odd-args diagnostics, got %d:\n%s", n, joined)
	}
}

// A malformed slog call must never crash the analyzer.
func TestSlogMalformedNoCrash(t *testing.T) {
	src := `package main

import "log/slog"

func f() {
	slog.Info()
	slog.Log()
}
`
	_ = runVetOnSrc(t, src)
}
