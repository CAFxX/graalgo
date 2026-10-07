package time

type Duration int64
type Month int
type Time struct{ sec, nsec int64 }
var UTC = &Location{}
type Location struct{}
func Now() Time { return Time{} }
func Unix(sec, nsec int64) Time { return Time{sec, nsec} }
func Date(y int, m Month, d, h, mi, s, nsec int, l *Location) Time { return Time{} }
func (t Time) UnixNano() int64 { return t.sec*1e9 + t.nsec }
