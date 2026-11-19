// Targets pure time computations in UTC: Duration arithmetic, String,
// Round/Truncate/Abs/Hours/Minutes; time.Date normalization (month 13,
// day 0, negative fields, leap years), Add/Sub/AddDate saturation,
// Weekday/YearDay/ISOWeek, Unix*/UnixMilli round trips, Truncate/Round.
package main

import (
	"math"
	"time"
)

func dur(d time.Duration) {
	println(int64(d), d.String(), int64(d.Round(time.Millisecond)), int64(d.Truncate(time.Second)), int64(d.Round(7*time.Minute)), int64(d.Abs()), math.Float64bits(d.Hours()), math.Float64bits(d.Minutes()), math.Float64bits(d.Seconds()), d.Milliseconds(), d.Microseconds())
}

func pt(t time.Time) {
	y, w := t.ISOWeek()
	println(t.Year(), int(t.Month()), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Weekday().String(), t.YearDay(), y, w, t.Unix(), t.Month().String())
}

func main() {
	ds := []time.Duration{0, 1, 999, 1000, 1500, time.Microsecond * 1001, time.Millisecond, 1500 * time.Millisecond, time.Second, 90 * time.Second,
		time.Hour + 2*time.Minute + 3*time.Second + 4*time.Millisecond, -time.Nanosecond, -90 * time.Minute, math.MaxInt64, math.MinInt64,
		math.MinInt64 + 1, 100 * time.Hour, 2*time.Hour + 30*time.Minute + 1, 59*time.Second + 999999999}
	for _, d := range ds {
		dur(d)
	}
	println(int64(time.Duration(math.MaxInt64).Round(time.Hour)), int64(time.Duration(math.MinInt64).Round(time.Hour)), int64(time.Second.Round(0)), int64(time.Second.Truncate(-1)))
	e18 := time.Duration(1e18)
	println(int64(e18*10), int64(time.Hour*time.Duration(1<<20)), (3 * time.Second / 2).String())

	cases := [][6]int{
		{2024, 2, 29, 12, 0, 0}, {2023, 2, 29, 0, 0, 0}, {2024, 13, 1, 0, 0, 0}, {2024, 0, 0, 0, 0, 0},
		{2024, 1, -1, 0, 0, 0}, {2024, -1, 15, 0, 0, 0}, {2000, 12, 31, 23, 59, 60}, {1999, 12, 31, 24, 0, 0},
		{2024, 3, 10, -1, -1, -1}, {1, 1, 1, 0, 0, 0}, {0, 1, 1, 0, 0, 0}, {-1, 12, 31, 0, 0, 0},
		{1970, 1, 1, 0, 0, 0}, {1969, 12, 31, 23, 59, 59}, {9999, 12, 31, 23, 59, 59}, {10000, 1, 1, 0, 0, 0},
		{2100, 2, 29, 0, 0, 0}, {1600, 2, 29, 0, 0, 0}, {2021, 1, 3, 0, 0, 0}, {2021, 1, 4, 0, 0, 0},
		{2020, 12, 31, 0, 0, 0}, {2026, 12, 28, 0, 0, 0}, {-4713, 11, 24, 12, 0, 0}, {292277026596, 12, 4, 15, 30, 7},
		{2024, 100, 400, 1000, 10000, 100000},
	}
	for _, c := range cases {
		t := time.Date(c[0], time.Month(c[1]), c[2], c[3], c[4], c[5], 123456789, time.UTC)
		pt(t)
	}
	base := time.Date(2024, 1, 31, 10, 20, 30, 400, time.UTC)
	for _, ad := range [][3]int{{0, 1, 0}, {0, -1, 0}, {1, 1, 0}, {0, 0, 29}, {0, 13, -31}, {-2024, 0, 0}, {-2025, -1, 0}, {100, 0, -36525}} {
		pt(base.AddDate(ad[0], ad[1], ad[2]))
	}
	far := base.Add(math.MaxInt64)
	pt(far)
	println(int64(far.Sub(base)), int64(base.Sub(far)), int64(base.Add(math.MinInt64).Sub(base)))
	a := time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)
	b := time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC)
	println(int64(b.Sub(a)), int64(a.Sub(b)), b.After(a), a.Before(b), a.Equal(a.Add(0)), a.Compare(b), b.Compare(a), a.Compare(a))
	var zero time.Time
	println(zero.IsZero(), zero.Unix(), zero.Year(), zero.Weekday().String(), zero.Equal(a), zero.UnixNano())
	pt(zero)

	// Unix round trips
	var h uint64 = 14695981039346656037
	for _, s := range []int64{0, 1, -1, 86399, -86401, 951782400, 4102444800, -62135596800, -62135596801, 253402300799, 1 << 40, -(1 << 40)} {
		t := time.Unix(s, 999999999).UTC()
		pt(t)
		t2 := time.UnixMilli(t.UnixMilli()).UTC()
		t3 := time.UnixMicro(t.UnixMicro()).UTC()
		println(t.UnixMilli(), t.UnixMicro(), t2.Equal(t), t3.Nanosecond(), t.UnixNano())
	}
	pt(time.Unix(0, -1).UTC())
	pt(time.Unix(-1, 2e9).UTC())
	pt(time.Unix(5, math.MinInt64).UTC())

	// sweep: day-by-day over 3 years, checksum date fields
	d := time.Date(2019, 11, 15, 6, 7, 8, 9, time.UTC)
	for i := 0; i < 1200; i++ {
		y, w := d.ISOWeek()
		h = (h ^ uint64(d.Year()*10000+int(d.Month())*100+d.Day())) * 1099511628211
		h = (h ^ uint64(int(d.Weekday())+d.YearDay()<<3+w<<12+y<<20)) * 1099511628211
		d = d.Add(24*time.Hour + 37*time.Minute)
	}
	println("sweep", h)

	// Truncate / Round on times
	t := time.Date(2024, 5, 17, 13, 47, 31, 555555555, time.UTC)
	for _, q := range []time.Duration{0, -1, time.Nanosecond, time.Second, time.Minute, 15 * time.Minute, time.Hour, 24 * time.Hour, 7 * 24 * time.Hour, 1000 * time.Hour, 333 * time.Millisecond} {
		tr := t.Truncate(q)
		ro := t.Round(q)
		println(int64(q), tr.Unix(), tr.Nanosecond(), ro.Unix(), ro.Nanosecond())
	}
	for m := time.Month(0); m <= 13; m++ {
		print(m.String(), " ")
	}
	println()
	for w := time.Weekday(-1); w <= 7; w++ {
		print(w.String(), " ")
	}
	println()
}
