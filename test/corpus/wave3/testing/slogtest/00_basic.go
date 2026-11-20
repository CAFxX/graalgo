package main

// L11 (REVIEW-2026-11-18-05): testing/slogtest — zero corpus imports
// before this program. Testing a slog.Handler against the package's
// case suite: a recording handler stores each record as a nested
// map[string]any (groups become nested maps, values resolved, empty
// groups omitted), and TestHandler checks the whole suite. Only the
// verdict and the record count are printed (maps carry live times
// and source PCs).

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"testing/slogtest"
)

// preAttr is one WithAttrs attribute together with the group path
// that was active when it was added.
type preAttr struct {
	groups []string
	attr   slog.Attr
}

type recordingHandler struct {
	// Shared pointer: WithAttrs copies the handler, but all copies
	// must append to the same record list.
	records *[]map[string]any
	pre     []preAttr
	groups  []string
}

func (h *recordingHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelInfo
}

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	m := map[string]any{}
	if !r.Time.IsZero() {
		m[slog.TimeKey] = r.Time
	}
	m[slog.LevelKey] = r.Level.String()
	m[slog.MessageKey] = r.Message
	if r.PC != 0 {
		if frames := runtime.CallersFrames([]uintptr{r.PC}); frames != nil {
			fr, _ := frames.Next()
			m[slog.SourceKey] = fr.Function
		}
	}
	for _, p := range h.pre {
		addAttr(m, p.groups, p.attr)
	}
	r.Attrs(func(a slog.Attr) bool {
		addAttr(m, h.groups, a)
		return true
	})
	*h.records = append(*h.records, m)
	return nil
}

// addAttr stores one attr into m under groups; group attrs nest,
// empty-key groups inline, empty attrs and empty groups are skipped,
// values are resolved.
func addAttr(m map[string]any, groups []string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		g := a.Value.Group()
		if len(g) == 0 {
			return
		}
		if a.Key == "" {
			for _, ga := range g {
				addAttr(m, groups, ga)
			}
			return
		}
		sub := map[string]any{}
		for _, ga := range g {
			addAttr(sub, nil, ga)
		}
		putGrouped(m, groups, a.Key, sub)
		return
	}
	if a.Key == "" {
		return
	}
	putGrouped(m, groups, a.Key, a.Value.Any())
}

func putGrouped(m map[string]any, groups []string, key string, val any) {
	for _, g := range groups {
		next, ok := m[g].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[g] = next
		}
		m = next
	}
	m[key] = val
}

func (h *recordingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	nh := *h
	nh.pre = append([]preAttr(nil), h.pre...)
	for _, a := range attrs {
		nh.pre = append(nh.pre, preAttr{groups: append([]string(nil), h.groups...), attr: a})
	}
	return &nh
}

func (h *recordingHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	nh := *h
	nh.groups = append(append([]string(nil), h.groups...), name)
	return &nh
}

func main() {
	var records []map[string]any
	h := &recordingHandler{records: &records}
	err := slogtest.TestHandler(h, func() []map[string]any { return records })
	fmt.Printf("testhandler-err=%v records=%d\n", err != nil, len(records))
	if err != nil {
		fmt.Printf("testhandler-detail=%v\n", err)
	}
}
