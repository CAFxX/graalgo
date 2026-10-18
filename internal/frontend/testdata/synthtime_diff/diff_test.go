// Package synthtime differential test against gc's time package.
//
// This file is NOT part of the repo's own test build: it lives in
// testdata and is copied by
// internal/frontend/synth_time_differential_test.go into a scratch
// module alongside the extracted synthetic time source (renamed to
// package synthtime), where `go test` runs it against the real time
// package. Being in package synthtime gives it access to the
// unexported __parseTime under test.
package synthtime

import (
	"strings"
	"testing"
	gotime "time"
)

// instantLayout renders the UTC wall clock without any zone label, so
// instants compare equal even though the synthetic model is UTC-only.
// Note: the synthetic Format renders ".0"-style (not ".9"-style)
// fractional seconds, so the layout uses zeros.
const instantLayout = "2006-01-02 15:04:05.000000000"

type parseCase struct {
	layout string
	value  string
	// checkInstant compares the parsed instant (via instantLayout);
	// false for years outside the int64-nanosecond range (1678..2262),
	// where the synthetic unixnano representation wraps — a documented
	// model limitation shared with Date/Unix. Error parity is still
	// checked for those.
	checkInstant bool
}

func TestParseDifferential(t *testing.T) {
	cases := []parseCase{
		// RFC3339 family (gc takes its fast path here; the general
		// parser must agree).
		{gotime.RFC3339, "2021-03-14T15:09:26Z", true},
		{gotime.RFC3339, "2021-03-14T15:09:26+02:00", true},
		{gotime.RFC3339, "2021-03-14T15:09:26-07:00", true},
		{gotime.RFC3339Nano, "2021-03-14T15:09:26.123456789Z", true},
		{gotime.RFC3339Nano, "2021-03-14T15:09:26.123456789012+05:30", true},
		{gotime.RFC3339, "2021-03-14 15:09:26Z", true}, // fast path fails, general path handles
		// Classic layouts.
		{gotime.ANSIC, "Sun Mar 14 15:09:26 2021", true},
		{gotime.UnixDate, "Sun Mar 14 15:09:26 UTC 2021", true},
		{gotime.UnixDate, "Sun Mar 14 15:09:26 MST 2021", true},
		{gotime.UnixDate, "Sun Mar 14 15:09:26 EST 2021", true}, // unknown abbrev: UTC instant
		{gotime.RubyDate, "Sun Mar 14 15:09:26 -0700 2021", true},
		{gotime.RFC822, "14 Mar 21 15:09 UTC", true},
		{gotime.RFC822Z, "14 Mar 21 15:09 -0700", true},
		{gotime.RFC850, "Sunday, 14-Mar-21 15:09:26 UTC", true},
		{gotime.RFC1123, "Sun, 14 Mar 2021 15:09:26 UTC", true},
		{gotime.RFC1123Z, "Sun, 14 Mar 2021 15:09:26 -0700", true},
		{gotime.Kitchen, "3:09PM", false},                 // year 0: error parity only
		{"2006-01-02 3:04PM", "2021-03-14 12:00AM", true}, // am midnight
		{"2006-01-02 3:04PM", "2021-03-14 12:00PM", true}, // pm noon
		{"2006-01-02 3:04PM", "2021-03-14 3:09PM", true},
		{"2006-01-02 3:04pm", "2021-03-14 3:09pm", true},       // lowercase pm
		{gotime.Stamp, "Mar 14 15:09:26", false},               // no year in layout -> year 0: error parity only,
		{gotime.Stamp, "Mar  4 05:09:26", false},               // no year in layout -> year 0: error parity only, // _2 blank-padded
		{gotime.StampMilli, "Mar 14 15:09:26.123", false},      // no year in layout -> year 0: error parity only,
		{gotime.StampMicro, "Mar 14 15:09:26.123456", false},   // no year in layout -> year 0: error parity only,
		{gotime.StampNano, "Mar 14 15:09:26.123456789", false}, // no year in layout -> year 0: error parity only,
		{gotime.DateTime, "2021-03-14 15:09:26", true},
		{gotime.DateOnly, "2021-03-14", true},
		{gotime.TimeOnly, "15:09:26", false}, // no year in layout -> year 0: error parity only,
		// Two-digit years and the 69 pivot.
		{"01/02/06", "03/14/21", true},
		{"01/02/06", "03/14/69", true},
		{"01/02/06", "03/14/68", true},
		{"01/02/06", "03/14/00", true},
		// Out-of-range years: error parity only (representation limit).
		{gotime.DateOnly, "0000-01-01", false},
		{gotime.DateOnly, "0001-01-01", true}, // zero instant: special-cased
		{gotime.DateOnly, "9999-12-31", false},
		{"", "", false}, // empty layout+value: zero time, no error
		// Fractional seconds: explicit, implied, comma.
		{"15:04:05", "15:09:26.123456", false},                // no year in layout -> year 0: error parity only,
		{"15:04:05", "15:09:26,123", false},                   // no year in layout -> year 0: error parity only,
		{"15:04:05.999999999", "2021-03-14 15:09:26.5", true}, // layout mismatch: error
		{"2006-01-02 15:04:05.999999999", "2021-03-14 15:09:26.5", true},
		{"2006-01-02 15:04:05.000", "2021-03-14 15:09:26.123", true},
		{"2006-01-02 15:04:05.00", "2021-03-14 15:09:26.123", true},        // wrong digit count: error
		{"2006-01-02 15:04:05,000", "2021-03-14 15:09:26,123", true},       // comma frac
		{"2006-01-02T15:04:05.999999999", "2021-03-14T15:09:26.123", true}, // implied frac
		// Month/day names, case-insensitivity.
		{"Jan 2, 2006", "Mar 14, 2021", true},
		{"January 2, 2006", "March 14, 2021", true},
		{"Jan 2, 2006", "mar 14, 2021", true},
		{"January 2, 2006", "MARCH 14, 2021", true},
		{"2 Jan 2006", "4 Mar 2021", true},
		{"_2 Jan 2006", " 4 Mar 2021", true},
		{"Monday, January 2, 2006", "Sunday, March 14, 2021", true}, // weekday ignored
		{"Monday, January 2, 2006", "Funday, March 14, 2021", true}, // bad weekday: error
		{"Mon", "Sun", false},                                       // weekday only; year 0: error parity only
		// Zone offsets: all widths, Z, ISO forms.
		{"2006-01-02T15:04:05Z0700", "2021-03-14T15:09:26+0200", true},
		{"2006-01-02T15:04:05Z0700", "2021-03-14T15:09:26Z", true},
		{"2006-01-02T15:04:05-0700", "2021-03-14T15:09:26+0000", true},
		{"15:04 -07", "15:09 +05", false},                   // no year in layout -> year 0: error parity only,
		{"15:04 -07", "15:09 -07", false},                   // no year in layout -> year 0: error parity only,
		{"15:04:05 -07:00", "15:09:26 +05:30", false},       // no year in layout -> year 0: error parity only,
		{"15:04:05 -070000", "15:09:26 +053000", false},     // no year in layout -> year 0: error parity only,
		{"15:04:05 -07:00:00", "15:09:26 +05:30:15", false}, // no year in layout -> year 0: error parity only,
		{"15:04:05Z07:00", "15:09:26+24:00", false},         // no year in layout -> year 0: error parity only, // 24h offset allowed
		{"15:04:05Z07:00", "15:09:26+25:00", true},          // range error: hour
		{"15:04:05Z07:00", "15:09:26+02:61", true},          // range error: minute
		{"15:04:05-07:00:00", "15:09:26+02:60:61", true},    // range error: second
		{"15:04 -0700", "15:09 Z", true},                    // Z not valid for -0700: error
		{"15:04:05 MST", "15:09:26 UTC", false},             // no year in layout -> year 0: error parity only,
		{"2006-01-02 15:04:05 MST", "2021-03-14 15:09:26 GMT+2", true},
		{"2006-01-02 15:04:05 MST", "2021-03-14 15:09:26 GMT", true},
		{"2006-01-02 15:04:05 MST", "2021-03-14 15:09:26 ChST", true},
		{"2006-01-02 15:04:05 MST", "2021-03-14 15:09:26 MeST", true},
		{"2006-01-02 15:04:05 MST", "2021-03-14 15:09:26 WITA", true},
		{"2006-01-02 15:04:05 MST", "2021-03-14 15:09:26 EDT", true},
		// Year days.
		{"__2 2006", "060 2021", true}, // non-leap: Mar 1
		{"__2 2006", "060 2020", true}, // leap: Feb 29
		{"002 2006", "060 2021", true},
		{"2006-01-02 __2", "2021-03-14 074", true}, // yday vs day mismatch: error
		// Range errors.
		{"15:04:05", "15:09:60", true},        // leap second: error
		{"15:04", "24:00", true},              // hour: error
		{"15:04", "12:60", true},              // minute: error
		{gotime.DateOnly, "2021-13-01", true}, // month: error
		{gotime.DateOnly, "2021-00-10", true}, // month: error
		{gotime.DateOnly, "2021-02-30", true}, // day out of range: error
		{gotime.DateOnly, "2020-02-29", true}, // leap day ok
		{gotime.DateOnly, "2021-02-29", true}, // non-leap: error
		{gotime.DateOnly, "2021-04-31", true}, // error
		// Syntax errors.
		{gotime.DateOnly, "2021-03-14abc", true},   // extra text
		{gotime.DateOnly, "2021-03-14 ", true},     // trailing space: extra text
		{gotime.DateOnly, "2021-03", true},         // short value
		{gotime.DateOnly, "2021-3-14", true},       // short month field
		{"2006", "", true},                         // short value
		{"04:05", "3:4", true},                     // fixed-width minute: error
		{"15:04:05", "15:09:26.", true},            // trailing dot: extra text
		{"2006-01-02", "2021-03-14\xc3\xa9", true}, // non-ASCII extra text (quote path)
		{"2006-01-02", "2021-03-1\xff", true},      // invalid UTF-8 (quote path)
		{"2006/01/02", "2021/03/14", true},         // literal separators
		{"2006-01-02", "  2021-03-14", true},       // leading spaces vs literal: error
		{"  2006-01-02", "  2021-03-14", true},     // space-run equivalence
		// 12-hour clock edges.
		{"2006-01-02 03:04:05 PM", "2021-03-14 12:00:00 AM", true},
		{"2006-01-02 03:04:05 PM", "2021-03-14 12:00:00 PM", true},
		{"2006-01-02 03:04:05 PM", "2021-03-14 00:00:00 AM", true}, // hour12 allows 0
		{"2006-01-02 03:04:05 PM", "2021-03-14 13:00:00 PM", true}, // hour12 > 12: error
	}
	for _, tc := range cases {
		sp := __parseTime(tc.layout, tc.value)
		gp, gerr := gotime.Parse(tc.layout, tc.value)
		if (sp.err == nil) != (gerr == nil) {
			t.Errorf("Parse(%q, %q): synth err=%v, gc err=%v", tc.layout, tc.value, sp.err, gerr)
			continue
		}
		if gerr != nil {
			if sp.err.Error() != gerr.Error() {
				t.Errorf("Parse(%q, %q) error:\n synth: %q\n gc:    %q",
					tc.layout, tc.value, sp.err.Error(), gerr.Error())
			}
			continue
		}
		if !tc.checkInstant {
			continue
		}
		want := gp.UTC().Format(instantLayout)
		got := sp.t.Format(instantLayout)
		if got != want {
			t.Errorf("Parse(%q, %q) instant:\n synth: %q\n gc:    %q",
				tc.layout, tc.value, got, want)
		}
	}
}

func TestParseErrorAPI(t *testing.T) {
	// The exported ParseError type keeps gc's API: fields + Error.
	sp := __parseTime("2006-01-02", "2021-13-01")
	if sp.err == nil {
		t.Fatal("expected error")
	}
	pe, ok := sp.err.(*ParseError)
	if !ok {
		t.Fatalf("err is %T, want *ParseError", sp.err)
	}
	if pe.Layout != "2006-01-02" || pe.Value != "2021-13-01" {
		t.Errorf("Layout/Value = %q/%q", pe.Layout, pe.Value)
	}
	if !strings.Contains(pe.Error(), "month out of range") {
		t.Errorf("Error() = %q", pe.Error())
	}
}

// accessorCase constructs the same instant in both packages.
type accessorCase struct {
	name string
	sy   Time
	gc   gotime.Time
	// skipAddDate: the AddDate results fall outside the int64-nanosecond
	// range (representation limit shared with Date/Unix), so only
	// error-parity-style construction is compared for these.
	skipAddDate bool
}

func dateCases() []accessorCase {
	mk := func(name string, y int, mo gotime.Month, d, hh, mm, ss, nsec int) accessorCase {
		return accessorCase{
			name: name,
			sy:   Date(y, Month(mo), d, hh, mm, ss, nsec, UTC),
			gc:   gotime.Date(y, mo, d, hh, mm, ss, nsec, gotime.UTC),
		}
	}
	ux := func(name string, sec, nsec int64) accessorCase {
		return accessorCase{
			name: name,
			sy:   Unix(sec, nsec),
			// gc's Unix returns Local; the synthetic model is UTC-only.
			gc: gotime.Unix(sec, nsec).UTC(),
		}
	}
	return []accessorCase{
		mk("epoch", 1970, 1, 1, 0, 0, 0, 0),
		mk("y2k1", 2021, 3, 14, 15, 9, 26, 123456789),
		mk("pre-epoch", 1969, 12, 31, 23, 59, 59, 999999999),
		mk("leap-day", 2000, 2, 29, 12, 0, 0, 0),
		// AddDate on the zero instant lands outside the int64-nanosecond
		// range (years 2 / -3): not representable in the unixnano model.
		{name: "zero-instant", sy: Date(1, 1, 1, 0, 0, 0, 0, UTC),
			gc:          gotime.Date(1, 1, 1, 0, 0, 0, 0, gotime.UTC),
			skipAddDate: true},
		mk("year-end", 2021, 12, 31, 23, 59, 59, 999999999),
		mk("neg-nsec-norm", 2021, 3, 14, 15, 9, 26, -1),
		mk("big-nsec-norm", 2021, 3, 14, 15, 9, 26, 2000000001),
		mk("far-past", 1800, 6, 15, 8, 30, 0, 0),
		mk("far-future", 2200, 6, 15, 8, 30, 0, 0),
		mk("month-overflow", 2021, 14, 40, 25, 61, 61, 0),
		ux("unix-zero", 0, 0),
		ux("unix-neg", -1, 500000000),
		ux("unix-pos", 1234567890, 123),
		{name: "go-zero-value", sy: Time{}, gc: gotime.Time{}, skipAddDate: true},
	}
}

func TestAccessorsDifferential(t *testing.T) {
	for _, tc := range dateCases() {
		sy, gc := tc.sy, tc.gc
		if got := sy.Year(); got != gc.Year() {
			t.Errorf("%s: Year() = %d, want %d", tc.name, got, gc.Year())
		}
		if got := sy.Month(); got != Month(gc.Month()) {
			t.Errorf("%s: Month() = %v, want %v", tc.name, got, gc.Month())
		}
		if got := sy.Day(); got != gc.Day() {
			t.Errorf("%s: Day() = %d, want %d", tc.name, got, gc.Day())
		}
		if got := sy.Weekday(); got != Weekday(gc.Weekday()) {
			t.Errorf("%s: Weekday() = %v, want %v", tc.name, got, gc.Weekday())
		}
		if got := sy.Hour(); got != gc.Hour() {
			t.Errorf("%s: Hour() = %d, want %d", tc.name, got, gc.Hour())
		}
		if got := sy.Minute(); got != gc.Minute() {
			t.Errorf("%s: Minute() = %d, want %d", tc.name, got, gc.Minute())
		}
		if got := sy.Second(); got != gc.Second() {
			t.Errorf("%s: Second() = %d, want %d", tc.name, got, gc.Second())
		}
		if got := sy.Nanosecond(); got != gc.Nanosecond() {
			t.Errorf("%s: Nanosecond() = %d, want %d", tc.name, got, gc.Nanosecond())
		}
		if got := sy.YearDay(); got != gc.YearDay() {
			t.Errorf("%s: YearDay() = %d, want %d", tc.name, got, gc.YearDay())
		}
		if got := sy.GoString(); got != gc.GoString() {
			t.Errorf("%s: GoString() = %q, want %q", tc.name, got, gc.GoString())
		}
		if got := sy.UnixMilli(); got != gc.UnixMilli() {
			t.Errorf("%s: UnixMilli() = %d, want %d", tc.name, got, gc.UnixMilli())
		}
		if got := sy.UnixMicro(); got != gc.UnixMicro() {
			t.Errorf("%s: UnixMicro() = %d, want %d", tc.name, got, gc.UnixMicro())
		}
		if got := sy.Format(instantLayout); got != gc.UTC().Format(instantLayout) {
			t.Errorf("%s: Format = %q, want %q", tc.name, got, gc.UTC().Format(instantLayout))
		}
		// Multi-result methods (served via the pair-transform in the
		// exporter; direct calls here).
		y1, m1, d1 := sy.Date()
		y2, m2, d2 := gc.Date()
		if y1 != y2 || m1 != Month(m2) || d1 != d2 {
			t.Errorf("%s: Date() = (%d,%v,%d), want (%d,%v,%d)",
				tc.name, y1, m1, d1, y2, m2, d2)
		}
		h1, mi1, s1 := sy.Clock()
		h2, mi2, s2 := gc.Clock()
		if h1 != h2 || mi1 != mi2 || s1 != s2 {
			t.Errorf("%s: Clock() = (%d,%d,%d), want (%d,%d,%d)",
				tc.name, h1, mi1, s1, h2, mi2, s2)
		}
		zn1, zo1 := sy.Zone()
		zn2, zo2 := gc.Zone()
		if zn1 != zn2 || zo1 != zo2 {
			t.Errorf("%s: Zone() = (%q,%d), want (%q,%d)", tc.name, zn1, zo1, zn2, zo2)
		}
		if got := sy.Location().String(); got != gc.Location().String() {
			t.Errorf("%s: Location() = %q, want %q", tc.name, got, gc.Location().String())
		}
		// AddDate round-trip via instant comparison (skipped where the
		// result leaves the int64-nanosecond range).
		if !tc.skipAddDate {
			ad1 := sy.AddDate(1, 2, 3)
			ad2 := gc.AddDate(1, 2, 3)
			if ad1.Format(instantLayout) != ad2.UTC().Format(instantLayout) {
				t.Errorf("%s: AddDate(1,2,3) = %q, want %q",
					tc.name, ad1.Format(instantLayout), ad2.UTC().Format(instantLayout))
			}
			ad1 = sy.AddDate(-2, -14, -40)
			ad2 = gc.AddDate(-2, -14, -40)
			if ad1.Format(instantLayout) != ad2.UTC().Format(instantLayout) {
				t.Errorf("%s: AddDate(-2,-14,-40) = %q, want %q",
					tc.name, ad1.Format(instantLayout), ad2.UTC().Format(instantLayout))
			}
		}
		// In(UTC) is the identity on the instant.
		if got := sy.In(UTC).Format(instantLayout); got != gc.In(gotime.UTC).UTC().Format(instantLayout) {
			t.Errorf("%s: In(UTC) = %q, want %q", tc.name, got, gc.In(gotime.UTC).UTC().Format(instantLayout))
		}
	}
}

func TestInRejectsNonUTC(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("In(non-UTC) did not panic")
		}
		if !strings.Contains(r.(string), "non-UTC locations not in scope") {
			t.Fatalf("panic = %q", r)
		}
	}()
	Date(2021, 3, 14, 0, 0, 0, 0, UTC).In(&Location{name: "EST"})
}

func TestInRejectsNil(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("In(nil) did not panic")
		}
		if !strings.Contains(r.(string), "missing Location") {
			t.Fatalf("panic = %q", r)
		}
	}()
	Date(2021, 3, 14, 0, 0, 0, 0, UTC).In(nil)
}
