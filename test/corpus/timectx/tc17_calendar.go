package main

import (
	"fmt"
	"time"
)

// tc17: time.Time calendar accessors + Parse (P4 R1). All inputs are
// fixed values (no Now — differential-safe). Prints accessors, Date /
// Clock triples, Zone / Location / GoString, AddDate results, Parse
// results (via accessors, never via a .999 Format layout — the
// synthetic Format renders .999 literally today, a separate gap),
// and Parse error strings (gc-exact on the UTC path).
//
// Deliberately NOT covered here (documented divergences / representation
// limits, see synth_time_context.go): year-0 results (year-less Parse
// layouts — unixnano cannot hold year 0), non-zero zone offsets and
// non-UTC zone abbreviations in Parse (panic), Format of the zero
// Time (decomposes the wrapped marker), and Location/GoString of
// Unix-constructed times (gc returns Local; the synthetic package
// documents Local as UTC, so those columns are printed only for
// Date-constructed times).
func show(label string, t time.Time) {
	y, m, d := t.Date()
	h, mi, s := t.Clock()
	zn, off := t.Zone()
	fmt.Printf("%s: %d %s %d %s %02d:%02d:%02d.%d yd=%d | date=%d %s %d clock=%d %d %d | zone=%q %d loc=%s | go=%s | zero=%v unix=%d\n",
		label, t.Year(), t.Month(), t.Day(), t.Weekday(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.YearDay(),
		y, m, d, h, mi, s, zn, off, t.Location(), t.GoString(), t.IsZero(), t.Unix())
}

// showUnix prints accessors for Unix-constructed times. It omits
// Location/GoString on purpose: gc's Unix returns Local, while the
// synthetic package documents Local as UTC (ADR-0005 divergence), so
// those two columns differ by design (loc=Local/time.Local under gc,
// loc=UTC/time.UTC here) even where every accessor matches.
func showUnix(label string, t time.Time) {
	y, m, d := t.Date()
	h, mi, s := t.Clock()
	zn, off := t.Zone()
	fmt.Printf("%s: %d %s %d %s %02d:%02d:%02d.%d yd=%d | date=%d %s %d clock=%d %d %d | zone=%q %d | zero=%v unix=%d\n",
		label, t.Year(), t.Month(), t.Day(), t.Weekday(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.YearDay(),
		y, m, d, h, mi, s, zn, off, t.IsZero(), t.Unix())
}

func parseShow(layout, value string) {
	p, err := time.Parse(layout, value)
	if err != nil {
		fmt.Printf("parse %q %q: err=%v\n", layout, value, err)
		return
	}
	fmt.Printf("parse %q %q: %s %02d:%02d:%02d.%d yd=%d unix=%d\n",
		layout, value, p.Format("2006-01-02"), p.Hour(), p.Minute(), p.Second(), p.Nanosecond(), p.YearDay(), p.Unix())
}

func main() {
	// Accessors on fixed instants.
	show("leap", time.Date(2024, time.February, 29, 23, 59, 58, 999999999, time.UTC))
	show("pre1970", time.Date(1969, time.December, 31, 23, 59, 59, 0, time.UTC))
	show("epoch", time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC))
	show("zero", time.Time{})
	show("date1", time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC))
	show("century-leap", time.Date(2000, time.February, 29, 12, 0, 0, 1, time.UTC))
	show("century-nonleap", time.Date(1900, time.March, 1, 0, 0, 0, 0, time.UTC))
	show("year-end", time.Date(2023, time.December, 31, 23, 59, 59, 999999999, time.UTC))
	showUnix("unix-neg", time.Unix(-1, 0))
	showUnix("unix-nanos", time.Unix(0, 999999999))
	fmt.Printf("in-utc: %s\n", time.Date(2024, time.February, 29, 1, 2, 3, 0, time.UTC).In(time.UTC).Format("2006-01-02 15:04:05"))

	// AddDate (gc normalization: Jan 31 + 1 month is Mar 2 in a leap year).
	fmt.Printf("adddate jan31+1mo: %s\n", time.Date(2024, time.January, 31, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0).Format("2006-01-02"))
	fmt.Printf("adddate mar31-1mo: %s\n", time.Date(2024, time.March, 31, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0).Format("2006-01-02"))
	fmt.Printf("adddate leap+1y: %s\n", time.Date(2024, time.February, 29, 0, 0, 0, 0, time.UTC).AddDate(1, 0, 0).Format("2006-01-02"))
	fmt.Printf("adddate dec31+1d: %s\n", time.Date(2024, time.December, 31, 10, 20, 30, 5, time.UTC).AddDate(0, 0, 1).Format("2006-01-02 15:04:05"))

	// Parse: success cases.
	parseShow(time.RFC3339, "2024-02-29T23:59:58.999999999Z")
	parseShow(time.RFC3339, "2024-02-29T23:59:58+00:00")
	parseShow(time.RFC3339Nano, "2024-02-29T10:20:30.123456789Z")
	parseShow("2006-01-02", "2024-02-29")
	parseShow("2006-01-02", "1969-12-31")
	parseShow("2006-01-02 15:04:05", "1969-12-31 23:59:59")
	parseShow("2006-01-02 15:04:05.999999999", "2024-02-29 10:20:30.5")
	parseShow("2006-01-02 15:04:05.000", "2024-02-29 10:20:30.123")
	parseShow("02 Jan 06 15:04 MST", "29 Feb 24 23:59 UTC")
	parseShow("2006-01-02T15:04:05Z0700", "2024-02-29T10:20:30Z")
	parseShow("2006-01-02T15:04:05Z07", "2024-02-29T10:20:30+00")
	parseShow("06-01-02", "24-02-29")
	parseShow("06-01-02", "69-02-01")
	parseShow("06-01-02", "68-02-01")
	parseShow("Jan _2, 2006", "Feb  9, 2024")
	parseShow("January 2, 2006", "february 29, 2024")
	parseShow("Mon 2006-01-02", "Thu 2024-02-29")
	parseShow(time.ANSIC, "Thu Feb 29 23:59:58 2024")
	parseShow("2006-002", "2024-060")
	parseShow("2006-002", "2023-060")
	parseShow("2006-01-002", "2024-02-060")

	// Parse: error cases (strings are compared exactly against gc).
	parseShow("2006-01-02", "2024-02-30")
	parseShow("2006-01-02", "2024-13-01")
	parseShow("2006-01-02", "2024-1-1")
	parseShow("2006-01-02", "abcd-01-01")
	parseShow("2006-01-02", "2024-02-29extra")
	parseShow("15:04:05", "24:00:00")
	parseShow("15:04:05", "23:60:00")
	parseShow("15:04:05", "23:59:60")
	parseShow("2006-01-02 15:04:05.000", "2024-02-29 10:20:30.5")
	parseShow("2006-01-02 15:04:05.000", "2024-02-29 10:20:30")
	parseShow("2006-01-02 15:04:05.999", "2024-02-29 10:20:30.")
	parseShow("2006-01-02", "2024-02")

	// ParseDuration (multi-result desugar; proves the existing helper
	// still works alongside the new Parse).
	d, err := time.ParseDuration("1h30m")
	fmt.Printf("parseduration 1h30m: %s err=%v\n", d, err)
	d, err = time.ParseDuration("2h45m")
	fmt.Printf("parseduration 2h45m: %s err=%v\n", d, err)
	_, err = time.ParseDuration("1x")
	fmt.Printf("parseduration 1x: err=%v\n", err != nil)
}
