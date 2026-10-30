package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSyntheticTimeCalendarR02 verifies the R0-2 fix (codebase review
// 2026-10-30): the synthetic time package's basic calendar API must be
// real implementations, not "not in M5 scope" panic stubs. The program
// exercises every fixed member — including the multi-result Date,
// Clock, Zone, and Parse call sites — and the test asserts the
// exported synthetic funcdecls for those members contain no M5-scope
// panic body. (Behavioral equivalence vs gc is covered by the
// differential probe; this test pins the export shape so the stubs
// cannot silently return.)
func TestSyntheticTimeCalendarR02(t *testing.T) {
	src := `package main

import (
	"time"
)

func main() {
	t := time.Date(2024, time.February, 29, 13, 45, 30, 123, time.UTC)
	y, m, d := t.Date()
	h, mi, se := t.Clock()
	n, _ := t.Zone()
	_ = n
	println(y, int(m), d, h, mi, se, t.Year(), int(t.Month()), t.Day(), int(t.Weekday()), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.YearDay())
	println(t.AddDate(0, 1, 0).Year())
	println(t.GoString())
	println(t.In(time.UTC).Year(), t.Location().String())
	p, err := time.Parse("2006-01-02", "2024-02-29")
	_, _ = p, err
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "cal.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(p)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	fixed := []string{
		"GoString", "AddDate", "Date", "Year", "Month", "Day",
		"Weekday", "Clock", "Hour", "Minute", "Second", "Nanosecond",
		"YearDay", "In", "Location", "Zone", "Parse",
	}
	seen := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "funcdecl" {
				if f, ok := v["fields"].(map[string]any); ok {
					name, _ := f["name"].(string)
					pkg, _ := f["pkgpath"].(string)
					if pkg == "time" {
						for _, want := range fixed {
							if name == want {
								seen[want] = true
								b, _ := json.Marshal(v)
								if strings.Contains(string(b), "not in M5 scope") {
									t.Errorf("synthetic time.%s still has an M5-scope panic body", want)
								}
							}
						}
					}
				}
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(raw)
	for _, want := range fixed {
		if !seen[want] {
			t.Errorf("synthetic time.%s funcdecl not found in export", want)
		}
	}
}
