package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSyntheticTimeAccessorsExport verifies NEW-P1-3 (review
// 2026-11-04-02): user code calling the single-result time accessors
// exports cleanly, and the synthetic Time method funcdecls in the wire
// doc carry real bodies — not the old panic("... not in M5 scope")
// stubs that compiled cleanly and then panicked at runtime. The
// funcdecl check is scoped to each method's own subtree: other stubs
// (GoString, Parse) legitimately still carry M5-scope panics.
//
// Date/Clock/Zone call sites are included in the program below: they
// export through the general multi-result pair-transform path (verified
// empirically), so their new bodies are reachable from user code, not
// merely present as funcdecls. Values are pinned in
// TestSyntheticTimeAccessorsValues.
func TestSyntheticTimeAccessorsExport(t *testing.T) {
	src := `package main

import "time"

func main() {
	t := time.Date(2024, time.March, 5, 6, 7, 8, 9, time.UTC)
	println(t.Year(), int(t.Month()), t.Day())
	println(int(t.Weekday()), t.YearDay())
	println(t.Hour(), t.Minute(), t.Second(), t.Nanosecond())
	println(t.AddDate(1, 2, 3).Year())
	y, mo, d := t.Date()
	println(y, int(mo), d)
	h, mi, s := t.Clock()
	println(h, mi, s)
	zn, off := t.Zone()
	println(zn, off)
	println(t.In(time.UTC).Year(), t.In(time.Local).Year())
	println(t.Location().String())
	println(t.Format(time.DateTime))
	println(time.Time{}.Year())
}
`
	doc := decodeExport(t, mustExport(t, src))

	// Every accessor must be emitted as a funcdecl.
	want := []string{
		"Year", "Month", "Day", "Weekday", "Hour", "Minute", "Second",
		"Nanosecond", "YearDay", "Date", "Clock", "AddDate", "In",
		"Location", "Zone",
	}
	byName := map[string][]map[string]any{}
	for _, f := range findAllKind(doc, "funcdecl") {
		fl, _ := f["fields"].(map[string]any)
		if name, _ := fl["name"].(string); name != "" {
			byName[name] = append(byName[name], f)
		}
	}
	for _, name := range want {
		decls := byName[name]
		if len(decls) == 0 {
			t.Errorf("wire doc missing funcdecl %q", name)
			continue
		}
		// At least one funcdecl of this name must be a real body:
		// its subtree must not contain the M5-scope panic string.
		real := false
		for _, d := range decls {
			if !subtreeHasString(d, "not in M5 scope") {
				real = true
				break
			}
		}
		if !real {
			t.Errorf("funcdecl %q still carries an M5-scope panic body", name)
		}
	}
}

// subtreeHasString reports whether any string in the decoded subtree
// contains substr.
func subtreeHasString(n any, substr string) bool {
	switch v := n.(type) {
	case string:
		return strings.Contains(v, substr)
	case map[string]any:
		for _, c := range v {
			if subtreeHasString(c, substr) {
				return true
			}
		}
	case []any:
		for _, c := range v {
			if subtreeHasString(c, substr) {
				return true
			}
		}
	}
	return false
}

// accessorGoTool locates a gc toolchain for running the synthetic
// source, following the bisect regression test's precedent (GOROOT
// env, then ~/tools/go*); it skips (rather than failing) when none is
// found, since the export test above still pins the lowering.
func accessorGoTool(t *testing.T) string {
	t.Helper()
	if gr := os.Getenv("GOROOT"); gr != "" {
		if p := filepath.Join(gr, "bin", "go"); fileExists(p) {
			return p
		}
	}
	home, err := os.UserHomeDir()
	if err == nil {
		for _, cand := range []string{
			filepath.Join(home, "tools", "go", "bin", "go"),
			filepath.Join(home, "tools", "go-1.27.1", "bin", "go"),
		} {
			if fileExists(cand) {
				return cand
			}
		}
	}
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	t.Skip("no Go toolchain found for the synthetic time accessor value test")
	return ""
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// TestSyntheticTimeAccessorsValues runs the exact synthetic time
// source (syntheticTimeSource, package clause rewritten to main) under
// gc and pins every accessor's value against gc expectations —
// including the zero Time, pre-epoch and leap-day cases, AddDate's
// Date-style normalization, and the UTC-only In/Location/Zone
// decision. Expected values are literals verified against gc's real
// time package, not computed by the code under test, so a wrong
// decomposition fails here.
func TestSyntheticTimeAccessorsValues(t *testing.T) {
	goTool := accessorGoTool(t)
	src := strings.Replace(syntheticTimeSource, "package time",
		"package main\n\nimport \"fmt\"", 1)
	if src == syntheticTimeSource {
		t.Fatal("syntheticTimeSource has no \"package time\" clause to rewrite")
	}
	src += `
func main() {
	base := Date(2024, March, 5, 6, 7, 8, 123456789, UTC)
	fmt.Printf("base-year=%d\n", base.Year())
	fmt.Printf("base-month=%d\n", base.Month())
	fmt.Printf("base-month-name=%s\n", base.Month())
	fmt.Printf("base-day=%d\n", base.Day())
	fmt.Printf("base-weekday=%s\n", base.Weekday())
	fmt.Printf("base-yearday=%d\n", base.YearDay())
	fmt.Printf("base-hour=%d\n", base.Hour())
	fmt.Printf("base-minute=%d\n", base.Minute())
	fmt.Printf("base-second=%d\n", base.Second())
	fmt.Printf("base-nanosecond=%d\n", base.Nanosecond())
	y, mo, d := base.Date()
	fmt.Printf("base-date=%d %s %d\n", y, mo, d)
	h, mi, s := base.Clock()
	fmt.Printf("base-clock=%d %d %d\n", h, mi, s)
	zn, off := base.Zone()
	fmt.Printf("base-zone=%s %d\n", zn, off)
	fmt.Printf("base-location=%s\n", base.Location())
	fmt.Printf("base-in-utc-eq=%v\n", base.In(UTC).UnixNano() == base.UnixNano())
	fmt.Printf("base-in-local-eq=%v\n", base.In(Local).UnixNano() == base.UnixNano())
	fmt.Printf("base-format=%s\n", base.Format(DateTime))
	epoch := Unix(0, 0)
	fmt.Printf("epoch=%d %s %d %s %d %d %d %d\n", epoch.Year(), epoch.Month(), epoch.Day(), epoch.Weekday(), epoch.YearDay(), epoch.Hour(), epoch.Minute(), epoch.Second())
	pre := Date(1969, December, 31, 23, 59, 59, 999999999, UTC)
	fmt.Printf("pre=%d %s %d %s %d %d %d %d %d\n", pre.Year(), pre.Month(), pre.Day(), pre.Weekday(), pre.YearDay(), pre.Hour(), pre.Minute(), pre.Second(), pre.Nanosecond())
	leap := Date(2024, February, 29, 12, 0, 0, 0, UTC)
	fmt.Printf("leap=%s %d\n", leap.Weekday(), leap.YearDay())
	nonleap := Date(2023, March, 1, 0, 0, 0, 0, UTC)
	fmt.Printf("nonleap=%s %d\n", nonleap.Weekday(), nonleap.YearDay())
	sunday := Date(2024, March, 3, 0, 0, 0, 0, UTC)
	fmt.Printf("sunday=%s %d\n", sunday.Weekday(), sunday.YearDay())
	uneg := Unix(-1, 500000000)
	fmt.Printf("uneg=%d %s %d %s %d %d %d %d\n", uneg.Year(), uneg.Month(), uneg.Day(), uneg.Weekday(), uneg.Hour(), uneg.Minute(), uneg.Second(), uneg.Nanosecond())
	zero := Time{}
	fmt.Printf("zero=%d %s %d %s %d %d %d %d %d %v %s\n", zero.Year(), zero.Month(), zero.Day(), zero.Weekday(), zero.YearDay(), zero.Hour(), zero.Minute(), zero.Second(), zero.Nanosecond(), zero.IsZero(), zero.Location())
	zzn, zoff := zero.Zone()
	fmt.Printf("zero-zone=%s %d\n", zzn, zoff)
	fmt.Printf("add-oct31=%s\n", Date(2024, October, 31, 0, 0, 0, 0, UTC).AddDate(0, 1, 0).Format(DateTime))
	fmt.Printf("add-jan31=%s\n", Date(2024, January, 31, 0, 0, 0, 0, UTC).AddDate(0, 1, 0).Format(DateTime))
	fmt.Printf("add-feb28=%s\n", Date(2023, February, 28, 23, 59, 59, 0, UTC).AddDate(0, 0, 1).Format(DateTime))
	fmt.Printf("add-feb29=%s\n", Date(2024, February, 29, 12, 0, 0, 0, UTC).AddDate(1, 0, 0).Format(DateTime))
	fmt.Printf("add-negmo=%s\n", Date(2020, January, 1, 0, 0, 0, 0, UTC).AddDate(0, -1, 0).Format(DateTime))
	fmt.Printf("add-base=%s\n", base.AddDate(1, 2, 3).Format(DateTime))
	fmt.Printf("add-identity=%v\n", base.AddDate(0, 0, 0).UnixNano() == base.UnixNano())
	fmt.Printf("add-negday=%s\n", Date(2024, January, 1, 0, 0, 0, 0, UTC).AddDate(0, 0, -1).Format(DateTime))
	func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("in-nil=%v\n", r)
			}
		}()
		_ = base.In(nil)
	}()
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "main.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(goTool, "run", p)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run synthetic time source: %v\n%s", err, out)
	}
	want := `base-year=2024
base-month=3
base-month-name=March
base-day=5
base-weekday=Tuesday
base-yearday=65
base-hour=6
base-minute=7
base-second=8
base-nanosecond=123456789
base-date=2024 March 5
base-clock=6 7 8
base-zone=UTC 0
base-location=UTC
base-in-utc-eq=true
base-in-local-eq=true
base-format=2024-03-05 06:07:08
epoch=1970 January 1 Thursday 1 0 0 0
pre=1969 December 31 Wednesday 365 23 59 59 999999999
leap=Thursday 60
nonleap=Wednesday 60
sunday=Sunday 63
uneg=1969 December 31 Wednesday 23 59 59 500000000
zero=1 January 1 Monday 1 0 0 0 0 true UTC
zero-zone=UTC 0
add-oct31=2024-12-01 00:00:00
add-jan31=2024-03-02 00:00:00
add-feb28=2023-03-01 23:59:59
add-feb29=2025-03-01 12:00:00
add-negmo=2019-12-01 00:00:00
add-base=2025-05-08 06:07:08
add-identity=true
add-negday=2023-12-31 00:00:00
in-nil=time: missing Location in call to Time.In
`
	if got := string(out); got != want {
		t.Fatalf("accessor values mismatch.\n got:\n%s\nwant:\n%s", got, want)
	}
}
