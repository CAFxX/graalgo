package main

// L11 (REVIEW-2026-11-18-05): log/slog — zero corpus imports before this
// program. TextHandler + JSONHandler to bytes.Buffer (time and source
// stripped via ReplaceAttr so output is deterministic), levels, groups,
// attrs, With, LogValuer.

import (
	"bytes"
	"fmt"
	"log/slog"
)

type userValuer struct{ name string }

func (u userValuer) LogValue() slog.Value {
	return slog.StringValue("user:" + u.name)
}

func stripTime(groups []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey || a.Key == slog.SourceKey {
		return slog.Attr{}
	}
	return a
}

func main() {
	var tb, jb bytes.Buffer
	opts := &slog.HandlerOptions{ReplaceAttr: stripTime, Level: slog.LevelDebug}
	text := slog.New(slog.NewTextHandler(&tb, opts))
	json := slog.New(slog.NewJSONHandler(&jb, opts))

	text.Debug("debugging", "n", 1)
	text.Info("hello", slog.String("who", "world"), slog.Int("n", 42))
	text.Warn("warned", slog.Group("g", slog.String("k", "v")))
	text.Error("failed", slog.Any("err", fmt.Errorf("boom")))
	fmt.Printf("text=%q\n", tb.String())

	json.Info("hello", slog.String("who", "world"))
	json.With("req", "abc").Info("with-attr", slog.Int("n", 7))
	json.WithGroup("http").Info("grouped", slog.String("method", "GET"))
	json.Info("valuer", "u", userValuer{"ada"})
	fmt.Printf("json=%q\n", jb.String())

	// Level filtering: a Warn-level handler drops Info.
	var fb bytes.Buffer
	warnOnly := slog.New(slog.NewTextHandler(&fb,
		&slog.HandlerOptions{ReplaceAttr: stripTime, Level: slog.LevelWarn}))
	warnOnly.Info("dropped")
	warnOnly.Warn("kept")
	fmt.Printf("filtered=%q\n", fb.String())

	// Level values and String.
	fmt.Printf("levels=%v %v %v %v\n",
		slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError)
	fmt.Printf("level-parse=%v\n", slog.Level(8))
}
