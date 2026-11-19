//m1c:requires descriptors
//m1c:env TZ=Europe/Rome

// time's pure-Go half (upstream's source) on graalgo's clocks and the
// syscall executor: Local from $TZ through the zoneinfo files (openat, read,
// lseek, close — the W8a role-table entries), LoadLocation, Format/Parse,
// Duration, Unix conversions, monotonic Since/Until.
package main

import "time"

func main() {
	println(time.Local.String())
	d := time.Date(2026, time.March, 29, 1, 30, 0, 0, time.Local)
	println(d.Format(time.RFC3339), d.Add(time.Hour).Format(time.RFC3339), d.Weekday().String())
	ny, err := time.LoadLocation("America/New_York")
	println(err == nil, d.In(ny).Format("2006-01-02 15:04:05 MST -0700"))
	_, err = time.LoadLocation("No/Such_Zone")
	println("missing zone error:", err != nil)
	u := time.Unix(1700000000, 123456789).UTC()
	println(u.Format(time.RFC3339Nano), u.YearDay(), u.UnixMilli())
	p, err := time.Parse("2006-01-02 15:04", "2024-02-29 23:59")
	println(p.String(), err == nil, p.AddDate(1, 0, 0).Format(time.DateOnly))
	dur, _ := time.ParseDuration("1h2m3.5s")
	println(dur.String(), dur.Round(time.Minute).String(), time.Duration(1500).String())
	now := time.Now()
	println("now is after 2026:", now.Year() >= 2026, "local zone:", now.Location().String())
	println("monotonic:", time.Since(now) >= 0, time.Until(now.Add(time.Hour)) > 0)
	println(time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC).ISOWeek())
}
