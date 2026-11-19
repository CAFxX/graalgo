// Targets time.Format / time.Parse / AppendFormat with fixed layouts in UTC:
// every std layout constant, fractional seconds (.000/.999/,000), padded
// and unpadded fields, numeric zones, year 0 and negative years, plus
// ParseDuration and Parse error messages for malformed inputs.
package main

import (
	"time"
)

func errs(err error) string {
	if err == nil {
		return "nil"
	}
	return err.Error()
}

var layouts = []string{
	time.Layout, time.ANSIC, time.UnixDate, time.RubyDate, time.RFC822, time.RFC822Z, time.RFC850,
	time.RFC1123, time.RFC1123Z, time.RFC3339, time.RFC3339Nano, time.Kitchen, time.Stamp,
	time.StampMilli, time.StampMicro, time.StampNano, time.DateTime, time.DateOnly, time.TimeOnly,
	"2006-01-02T15:04:05.000Z07:00", "02/01/06 3:4:5 pm", "Jan _2 __2 002", ".999999999 ,000 .9",
	"Monday Mon January Jan 1 01 2 02 _2", "15 3 03 4 04 5 05 PM pm", "-0700 -07:00 -07 Z0700 Z07:00 Z07 MST",
	"2006 06 literal \\ text", "20060102150405", "060102 1504",
}

func main() {
	times := []time.Time{
		time.Date(2006, 1, 2, 15, 4, 5, 999999999, time.UTC),
		time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC),
		time.Date(1999, 12, 31, 23, 59, 59, 120000000, time.UTC),
		time.Date(0, 1, 1, 12, 0, 0, 1, time.UTC),
		time.Date(-44, 3, 15, 9, 9, 9, 50000, time.UTC),
		time.Date(12345, 10, 5, 1, 2, 3, 4, time.UTC),
		time.Date(2030, 7, 4, 12, 30, 0, 100, time.FixedZone("X", -(3*3600+30*60))),
		time.Date(2030, 7, 4, 12, 30, 0, 0, time.FixedZone("", 5*3600+45*60+30)),
	}
	for _, t := range times {
		for _, l := range layouts {
			println(t.Format(l))
		}
		b := t.AppendFormat([]byte("@"), time.RFC3339Nano)
		println(string(b), t.String())
	}
	parses := [][2]string{
		{time.RFC3339, "2024-02-29T12:34:56Z"},
		{time.RFC3339, "2024-02-30T12:34:56Z"},
		{time.RFC3339, "2024-02-29T12:34:56+05:30"},
		{time.RFC3339Nano, "2024-02-29T12:34:56.123456789Z"},
		{time.RFC3339, "2024-02-29T12:34:56.5Z"},
		{time.RFC3339, "2024-02-29 12:34:56Z"},
		{time.DateOnly, "2023-13-01"},
		{time.DateOnly, "0000-01-01"},
		{time.DateTime, "2023-06-31 00:00:00"},
		{time.DateTime, "2023-06-30 24:00:00"},
		{time.Kitchen, "3:04PM"},
		{time.Kitchen, "13:04PM"},
		{time.RFC1123, "Mon, 02 Jan 2006 15:04:05 UTC"},
		{time.RFC1123Z, "Mon, 02 Jan 2006 15:04:05 -0130"},
		{time.ANSIC, "Tue Jan  2 15:04:05 2006"},
		{"2006-01-02 .000", "2020-05-05 .123"},
		{"2006-01-02 .000", "2020-05-05 .12"},
		{"Jan _2 2006", "Feb  3 2021"},
		{"January 2, 2006", "Septembr 2, 2006"},
		{"02 Jan 06 15:04 -0700", "31 Dec 99 23:59 +2400"},
		{"2006-002", "2021-366"},
		{"2006-002", "2020-366"},
		{"15:04:05", "25:00:00"},
		{"", ""},
		{"x", "y"},
		{time.RFC3339, "2024-02-29T12:34:56Zjunk"},
		{"2006-01-02T15:04:05,000Z", "2020-01-01T00:00:00,999Z"},
	}
	for _, p := range parses {
		t, err := time.Parse(p[0], p[1])
		_, off := t.Zone()
		println(p[1], "|", t.UTC().Format(time.RFC3339Nano), off, t.Unix(), errs(err))
	}
	t, err := time.ParseInLocation(time.DateTime, "2010-10-10 10:10:10", time.UTC)
	println(t.Unix(), errs(err))
	for _, s := range []string{"0", "1h", "-1.5h", "1h2m3.5s", "1.000000001s", "300ms", "2us", "2\u00b5s", "2\u03bcs", "7ns", ".5m", "1d", "", "-", "+3s",
		"9223372036854775807ns", "9223372036854775808ns", "2562047h47m16.854775807s", "2562048h", "1e3s", "3m-2s", "0.0000000001s"} {
		d, err := time.ParseDuration(s)
		println(s, int64(d), d.String(), errs(err))
	}
	// round trip sweep through RFC3339Nano
	var h uint64 = 14695981039346656037
	bad := 0
	tt := time.Date(1950, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 600; i++ {
		tt = tt.Add(time.Duration(i)*97*time.Hour + time.Duration(i*i)*time.Millisecond + 13)
		s := tt.Format(time.RFC3339Nano)
		for j := 0; j < len(s); j++ {
			h = (h ^ uint64(s[j])) * 1099511628211
		}
		back, err := time.Parse(time.RFC3339Nano, s)
		if err != nil || !back.Equal(tt) {
			bad++
		}
		s2 := tt.Format(time.RFC1123Z)
		for j := 0; j < len(s2); j++ {
			h = (h ^ uint64(s2[j])) * 1099511628211
		}
	}
	println("roundtrip", h, bad)
	mt, _ := times[0].MarshalText()
	println(string(mt))
	var u time.Time
	println(errs(u.UnmarshalText([]byte("2001-02-03T04:05:06.7+01:00"))), u.Unix(), u.Nanosecond())
	println(errs(u.UnmarshalText([]byte("bogus"))))
	big := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err = big.MarshalText()
	println(errs(err))
}
