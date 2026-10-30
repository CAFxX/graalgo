// P3-M5 (WS E, ADR-0005): synthetic `time` and `context` packages.
//
// The two packages below are REAL Go source, embedded as text and
// type-checked on demand with the overlay importer (so `context`'s
// `import "time"` resolves to the synthetic package —
// synthetic→synthetic imports; the channel-mutex design needs no
// `sync`). The resulting *types.Package instances
// are what overlayImporter.Import("time"/"context") returns, so the
// user's type-check and the synthetic sources' own type-check share
// identical *types.Func/*types.TypeName objects; the exporter then emits
// the synthetic declarations' bodies into the same wire document (shared
// type/object tables) via exportSyntheticPackages.
//
// Value model (ADR-0005 D1/D2, Appendix M):
//   - time.Duration / time.Time / time.Month / time.Weekday are plain Go
//     values; their methods are real Go, lowered like user code.
//   - time.Timer / time.Ticker are opaque Java objects (the ADR-0002
//     sync.Mutex pattern): construction is exporter-normalized to the
//     "timenew" wire node and .C/Stop/Reset are intercepted.
//   - The clock/timer entry points (Now, Sleep, After, Tick, NewTimer,
//     NewTicker, AfterFunc) are intrinsics: declared here with exact gc
//     1.27.1 signatures and panic bodies, always intercepted by the
//     exporter into the §D3 wire nodes, never exported as funcdecls.
//   - context is real Go on the Appendix M channel-mutex design (no Java
//     intrinsics at all): cancelCtx with done/cancelCh/muChan/children,
//     timerCtx via time.AfterFunc, valueCtx, and the __withCancel/
//     __withDeadline/__withTimeout/__deadlineOf single-result desugar
//     targets behind the 6 Appendix M multi-result operations (5 reachable
//     in M5 — Context.Deadline() is not callable post-afc0dfb; which keep
//     exact gc signatures with fail-fast bodies for type-checking).
//
// Out-of-scope members are declared with exact gc signatures and panic
// bodies reporting "not in M5 scope" — fail fast, never silently.
//
// The sources use only frontend/v1-supported constructs (no generics):
// they must keep passing `go vet`-level type-check AND the exporter's
// scope rules.
package main

// syntheticTimeSource is the P3-M5 synthetic `time` package (ADR-0005
// D1). Exact gc 1.27.1 signatures for the in-scope API; pure-Go value
// types (Duration, Time, Month, Weekday) with real methods; intrinsics
// (Now/Sleep/After/Tick/NewTimer/NewTicker/AfterFunc, Timer/ Ticker.Stop/
// Reset) with panic bodies for the exporter to intercept.
const syntheticTimeSource = `package time

// Duration is an int64 nanosecond count, as in the real package.
type Duration int64

const (
	Nanosecond  Duration = 1
	Microsecond          = 1000 * Nanosecond
	Millisecond          = 1000 * Microsecond
	Second               = 1000 * Millisecond
	Minute               = 60 * Second
	Hour                 = 60 * Minute
)

// Reference layout constants (gc 1.27.1 time/format.go exact).
// P4 (driver-harden): were absent; real-world packages (e.g. pflag's
// timeValue) reference them.
const (
	Layout      = "01/02 03:04:05PM '06 -0700"
	ANSIC       = "Mon Jan _2 15:04:05 2006"
	UnixDate    = "Mon Jan _2 15:04:05 MST 2006"
	RubyDate    = "Mon Jan 02 15:04:05 -0700 2006"
	RFC822      = "02 Jan 06 15:04 MST"
	RFC822Z     = "02 Jan 06 15:04 -0700"
	RFC850      = "Monday, 02-Jan-06 15:04:05 MST"
	RFC1123     = "Mon, 02 Jan 2006 15:04:05 MST"
	RFC1123Z    = "Mon, 02 Jan 2006 15:04:05 -0700"
	RFC3339     = "2006-01-02T15:04:05Z07:00"
	RFC3339Nano = "2006-01-02T15:04:05.999999999Z07:00"
	Kitchen     = "3:04PM"
	Stamp       = "Jan _2 15:04:05"
	StampMilli  = "Jan _2 15:04:05.000"
	StampMicro  = "Jan _2 15:04:05.000000"
	StampNano   = "Jan _2 15:04:05.000000000"
	DateTime    = "2006-01-02 15:04:05"
	DateOnly    = "2006-01-02"
	TimeOnly    = "15:04:05"
)

const maxDuration Duration = 1<<63 - 1
const minDuration Duration = -1 << 63

func (d Duration) String() string {
	buf := make([]byte, 32)
	n := timeFormatDuration(buf, d)
	return string(buf[n:])
}

// timeFormatDuration formats d into the tail of buf, returning the offset
// of the first character. Clean-room implementation matching gc 1.27.1's
// output exactly ("72h3m0.5s" form; sub-second values use ms/us/ns so the
// leading digit is non-zero; zero formats as "0s").
func timeFormatDuration(buf []byte, d Duration) int {
	w := len(buf)
	u := uint64(d)
	neg := d < 0
	if neg {
		u = -u
	}
	if u < uint64(Second) {
		var prec int
		w--
		buf[w] = 's'
		w--
		switch {
		case u == 0:
			buf[w] = '0'
			return w
		case u < uint64(Microsecond):
			prec = 0
			buf[w] = 'n'
		case u < uint64(Millisecond):
			prec = 3
			w--
			copy(buf[w:], "µ")
		default:
			prec = 6
			buf[w] = 'm'
		}
		w = timeFormatFrac(buf[:w], u, prec)
		u = timeDivPow10(u, prec)
		w = timeFormatInt(buf[:w], u)
	} else {
		w--
		buf[w] = 's'
		w = timeFormatFrac(buf[:w], u, 9)
		u = timeDivPow10(u, 9)
		w = timeFormatInt(buf[:w], u%60)
		u /= 60
		if u > 0 {
			w--
			buf[w] = 'm'
			w = timeFormatInt(buf[:w], u%60)
			u /= 60
			if u > 0 {
				w--
				buf[w] = 'h'
				w = timeFormatInt(buf[:w], u)
			}
		}
	}
	if neg {
		w--
		buf[w] = '-'
	}
	return w
}

func timeFormatFrac(buf []byte, v uint64, prec int) int {
	w := len(buf)
	print := false
	for i := 0; i < prec; i++ {
		digit := v % 10
		print = print || digit != 0
		if print {
			w--
			buf[w] = byte(digit) + '0'
		}
		v /= 10
	}
	if print {
		w--
		buf[w] = '.'
	}
	return w
}

func timeDivPow10(v uint64, prec int) uint64 {
	for i := 0; i < prec; i++ {
		v /= 10
	}
	return v
}

func timeFormatInt(buf []byte, v uint64) int {
	w := len(buf)
	if v == 0 {
		w--
		buf[w] = '0'
	} else {
		for v > 0 {
			w--
			buf[w] = byte(v%10) + '0'
			v /= 10
		}
	}
	return w
}

func (d Duration) Nanoseconds() int64  { return int64(d) }
func (d Duration) Microseconds() int64 { return int64(d) / 1e3 }
func (d Duration) Milliseconds() int64 { return int64(d) / 1e6 }

func (d Duration) Seconds() float64 { return float64(d) / float64(Second) }
func (d Duration) Minutes() float64 { return float64(d) / float64(Minute) }
func (d Duration) Hours() float64   { return float64(d) / float64(Hour) }

func (d Duration) Truncate(m Duration) Duration {
	if m <= 0 {
		return d
	}
	return d - d%m
}

func timeLessThanHalf(x, y Duration) bool {
	return uint64(x)+uint64(x) < uint64(y)
}

func (d Duration) Round(m Duration) Duration {
	if m <= 0 {
		return d
	}
	r := d % m
	if d < 0 {
		r = -r
		if timeLessThanHalf(r, m) {
			return d + r
		}
		if d1 := d - m + r; d1 < d {
			return d1
		}
		return minDuration
	}
	if timeLessThanHalf(r, m) {
		return d - r
	}
	if d1 := d + m - r; d1 > d {
		return d1
	}
	return maxDuration
}

func (d Duration) Abs() Duration {
	switch {
	case d >= 0:
		return d
	case d == minDuration:
		return maxDuration
	default:
		return -d
	}
}

type timeParseError struct {
	message string
	value   string
}

func (e *timeParseError) Error() string {
	return "time: " + e.message + " " + timeQuote(e.value)
}

func timeQuote(s string) string {
	b := make([]byte, 0, len(s)+2)
	b = append(b, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c >= 0x7f {
			b = append(b, '\\', 'x')
			b = append(b, "0123456789abcdef"[c>>4], "0123456789abcdef"[c&0xf])
		} else {
			b = append(b, c)
		}
	}
	b = append(b, '"')
	return string(b)
}

// __timeLeadingInt/__timeLeadingFraction are single-result variants of
// gc's helpers (multi-result calls need WS D; these are only called from
// __parseDuration, so they return pairs directly).
type leadingIntPair struct {
	x  uint64
	s  string
	ok bool
}

func __timeLeadingInt(s string) leadingIntPair {
	var x uint64
	i := 0
	for ; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			break
		}
		if x > (1<<63)/10 {
			return leadingIntPair{}
		}
		x = x*10 + uint64(c-'0')
		if x > 1<<63 {
			return leadingIntPair{}
		}
	}
	return leadingIntPair{x: x, s: s[i:], ok: true}
}

type leadingFractionTriple struct {
	x     uint64
	scale float64
	s     string
}

func __timeLeadingFraction(s string) leadingFractionTriple {
	var x uint64
	scale := float64(1)
	overflow := false
	i := 0
	for ; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			break
		}
		if overflow {
			continue
		}
		if x > (1<<63-1)/10 {
			overflow = true
			continue
		}
		y := x*10 + uint64(c-'0')
		if y > 1<<63 {
			overflow = true
			continue
		}
		x = y
		scale *= 10
	}
	return leadingFractionTriple{x: x, scale: scale, s: s[i:]}
}

// __timeUnitValue is the single-result variant of gc's unit lookup
// (multi-result calls need WS D; only called from __parseDuration).
type unitPair struct {
	unit uint64
	ok   bool
}

func __timeUnitValue(u string) unitPair {
	switch u {
	case "ns":
		return unitPair{unit: uint64(Nanosecond), ok: true}
	case "us", "µs", "μs":
		return unitPair{unit: uint64(Microsecond), ok: true}
	case "ms":
		return unitPair{unit: uint64(Millisecond), ok: true}
	case "s":
		return unitPair{unit: uint64(Second), ok: true}
	case "m":
		return unitPair{unit: uint64(Minute), ok: true}
	case "h":
		return unitPair{unit: uint64(Hour), ok: true}
	}
	return unitPair{}
}

// durationPair is the Appendix M single-result carrier for ParseDuration.
type durationPair struct {
	d   Duration
	err error
}

// __parseDuration is the Appendix M desugar target: the real
// ParseDuration logic (gc 1.27.1 syntax and error strings: "300ms",
// "-1.5h", "2h45m"; "0" is zero) returning a single durationPair. It
// cannot call ParseDuration: the exporter desugars 2-target
// multi-result calls, so "d, err := ParseDuration(s)" here would
// recurse.
func __parseDuration(s string) durationPair {
	orig := s
	var d uint64
	neg := false
	if s != "" {
		c := s[0]
		if c == '-' || c == '+' {
			neg = c == '-'
			s = s[1:]
		}
	}
	if s == "0" {
		return durationPair{}
	}
	if s == "" {
		return durationPair{err: &timeParseError{"invalid duration", orig}}
	}
	for s != "" {
		var v, f uint64
		scale := float64(1)
		// NB: written without !(...) — go/types infers untyped bool for
		// !(paren-expr) in this position, which the v1 wire cannot express
		// (P3-M5: BOOL -> UNTYPED_BOOL rejection).
		if s[0] != '.' && (s[0] < '0' || s[0] > '9') {
			return durationPair{err: &timeParseError{"invalid duration", orig}}
		}
		pl := len(s)
		li := __timeLeadingInt(s)
		if !li.ok {
			return durationPair{err: &timeParseError{"invalid duration", orig}}
		}
		v = li.x
		s = li.s
		pre := pl != len(s)
		post := false
		if s != "" && s[0] == '.' {
			s = s[1:]
			pl = len(s)
			lf := __timeLeadingFraction(s)
			f, scale, s = lf.x, lf.scale, lf.s
			post = pl != len(s)
		}
		if !pre && !post {
			return durationPair{err: &timeParseError{"invalid duration", orig}}
		}
		i := 0
		for ; i < len(s); i++ {
			c := s[i]
			if c == '.' || ('0' <= c && c <= '9') {
				break
			}
		}
		if i == 0 {
			return durationPair{err: &timeParseError{"missing unit in duration", orig}}
		}
		u := s[:i]
		s = s[i:]
		up := __timeUnitValue(u)
		if !up.ok {
			return durationPair{err: &timeParseError{"unknown unit " + timeQuote(u) + " in duration", orig}}
		}
		unit := up.unit
		if v > (1<<63)/unit {
			return durationPair{err: &timeParseError{"invalid duration", orig}}
		}
		v *= unit
		if f > 0 {
			v += uint64(float64(f) * (float64(unit) / scale))
			if v > 1<<63 {
				return durationPair{err: &timeParseError{"invalid duration", orig}}
			}
		}
		d += v
		if d > 1<<63 {
			return durationPair{err: &timeParseError{"invalid duration", orig}}
		}
	}
	if neg {
		return durationPair{d: -Duration(d)}
	}
	if d > 1<<63-1 {
		return durationPair{err: &timeParseError{"invalid duration", orig}}
	}
	return durationPair{d: Duration(d)}
}

// ParseDuration keeps the exact gc 1.27.1 signature for type-checking;
// the exporter desugars 2-target call sites (Appendix M), every other
// position fails fast. Body never runs.
func ParseDuration(s string) (Duration, error) {
	panic("time.ParseDuration: not in M5 scope (multi-result calls need WS D; re-test when it lands)")
}

// Time is a wall-clock instant as Unix nanoseconds. The zero Time has
// isSet == false (IsZero); every constructor (Unix, Date, Now, ...)
// sets isSet, so time.Unix(0,0) (1970-01-01, unixnano == 0) is correctly
// NOT the zero Time (0001-01-01). Monotonic readings are out of M5 scope.
type Time struct {
	unixnano int64
	isSet    bool
}

// zeroInstantNano is the Unix-nanosecond reading of the zero time instant
// (0001-01-01 00:00:00 UTC). -62135596800*1e9 is not representable in an
// int64, so every synthetic constructor that targets the zero instant
// (Date, Unix) wraps to this same value — -62135596800*1e9 mod 2^64 as
// int64 (signed multiplication wraps, two's complement, both under gc and
// under the graalgo frontend) — making it the canonical in-representation
// marker of the zero instant. (Written as a literal: the product is not a
// valid Go constant expression, it overflows int64.)
const zeroInstantNano = int64(-6795364578871345152)

// IsZero reports whether t represents the zero time instant, January 1,
// year 1, 00:00:00 UTC. Like gc, this is value-based:
// time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC).IsZero() is true.
// The Go zero value Time{} (!isSet) is the zero instant; a constructed
// Time (isSet) is the zero instant exactly when its unixnano carries the
// wrapped zero-instant marker above.
func (t Time) IsZero() bool { return !t.isSet || t.unixnano == zeroInstantNano }

func (t Time) Before(u Time) bool { return t.unixnano < u.unixnano }
func (t Time) After(u Time) bool  { return t.unixnano > u.unixnano }
func (t Time) Equal(u Time) bool  { return t.unixnano == u.unixnano }

func (t Time) Compare(u Time) int {
	switch {
	case t.unixnano < u.unixnano:
		return -1
	case t.unixnano > u.unixnano:
		return 1
	}
	return 0
}

func (t Time) Add(d Duration) Time {
	return Time{unixnano: t.unixnano + int64(d), isSet: t.isSet || d != 0}
}

func (t Time) Sub(u Time) Duration {
	return Duration(t.unixnano - u.unixnano)
}

func (t Time) Unix() int64 {
	// P3-M9: gc parity — the zero Time (0001-01-01) has Unix() == -62135596800,
	// not 0. The zero instant's nanos overflow int64, so the wrapped
	// unixnano cannot simply be divided; the IsZero test above already
	// identifies the zero instant however it is spelled (Go zero value,
	// Date(1,1,1), Unix(-62135596800, 0)).
	if t.IsZero() {
		return -62135596800
	}
	// Floored division, like gc: pre-epoch times with a nonzero
	// nanosecond remainder round toward -inf (Unix(-1, 1) == -1, not 0).
	return timeFloorDiv(t.unixnano, 1e9)
}
func (t Time) UnixNano() int64 {
	if t.IsZero() {
		// gc's UnixNano for the zero instant is the wrapped product
		// -62135596800*1e9 (== zeroInstantNano); the Go zero value
		// stores unixnano == 0, so it needs the same special case
		// as Unix().
		return zeroInstantNano
	}
	return t.unixnano
}

func (t Time) Round(d Duration) Time {
	if d <= 0 {
		return t
	}
	dd := int64(d)
	n := t.unixnano
	r := n % dd
	if r < 0 {
		if r+r > -dd {
			return Time{unixnano: n - r, isSet: t.isSet}
		}
		return Time{unixnano: n - dd - r, isSet: t.isSet}
	}
	if r+r < dd {
		return Time{unixnano: n - r, isSet: t.isSet}
	}
	return Time{unixnano: n + dd - r, isSet: t.isSet}
}

func (t Time) Truncate(d Duration) Time {
	if d <= 0 {
		return t
	}
	dd := int64(d)
	return Time{unixnano: t.unixnano - t.unixnano%dd, isSet: t.isSet}
}

// timeCivil is the UTC civil decomposition of a Time: the single
// implementation shared by Format and the Date/Year/Month/Day/Weekday/
// Clock/Hour/Minute/Second/Nanosecond/YearDay accessors (R0-2), so the
// accessors can never drift from Format.
type timeCivil struct {
	year    int64
	month   int64 // 1..12
	day     int64 // 1..31
	weekday int64 // 0 = Sunday
	hour    int64
	min     int64
	sec     int64
	nsec    int64
	yearday int64 // 1..366
	days    int64 // days since 1970-01-01 (UTC)
}

// timeIsLeapYear reports whether y is a leap year (proleptic Gregorian).
func timeIsLeapYear(y int64) bool {
	return y%4 == 0 && (y%100 != 0 || y%400 == 0)
}

// timeDaysInMonth returns the number of days in month m of year y
// (m 1-based; 0 for an out-of-range month).
func timeDaysInMonth(y, m int64) int64 {
	switch m {
	case 1, 3, 5, 7, 8, 10, 12:
		return 31
	case 4, 6, 9, 11:
		return 30
	case 2:
		if timeIsLeapYear(y) {
			return 29
		}
		return 28
	}
	return 0
}

// timeMonthDayFromYearDay converts a 1-based day-of-year into its
// month/day in year y (caller validates the range first).
func timeMonthDayFromYearDay(y, yday int64) timeCivil {
	month := int64(1)
	day := yday
	for month < 12 {
		dim := timeDaysInMonth(y, month)
		if day <= dim {
			break
		}
		day -= dim
		month++
	}
	return timeCivil{month: month, day: day}
}

// timeDecompose splits t into UTC civil fields. The zero instant (the
// Go zero value, or Date(1, January, 1, ...) — both spellings) is
// special-cased: its unixnano is 0 (!isSet) or the wrapped
// zero-instant marker, neither of which decomposes to year 1 by
// arithmetic, so it maps to 0001-01-01 00:00:00 UTC (Monday, yearday 1),
// exactly like gc. (Before this fix, Format rendered the zero value as
// 1970-01-01; the accessors and Format now agree on 0001-01-01.)
func timeDecompose(t Time) timeCivil {
	if t.IsZero() {
		return timeCivil{year: 1, month: 1, day: 1, weekday: 1, yearday: 1, days: -719162}
	}
	// Split unixnano into days + time-of-day (UTC).
	sec := t.unixnano / 1e9
	nsec := t.unixnano % 1e9
	if nsec < 0 {
		nsec += 1e9
		sec--
	}
	days := sec / 86400
	secsOfDay := sec % 86400
	if secsOfDay < 0 {
		secsOfDay += 86400
		days--
	}
	// Hinnant civil-from-days: days since 1970-01-01 -> y/m/d.
	z := days + 719468
	var era int64
	if z >= 0 {
		era = z / 146097
	} else {
		era = (z - 146096) / 146097
	}
	doe := z - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	y := yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	d := doy - (153*mp+2)/5 + 1
	var m int64
	if mp < 10 {
		m = mp + 3
	} else {
		m = mp - 9
	}
	if m <= 2 {
		y++
	}
	hour := secsOfDay / 3600
	min := (secsOfDay % 3600) / 60
	sec2 := secsOfDay % 60
	// Weekday: 1970-01-01 was a Thursday (weekday 4).
	weekday := (days + 4) % 7
	if weekday < 0 {
		weekday += 7
	}
	// Yearday: days elapsed in the year before this month, plus day.
	yearday := d
	for mm := int64(1); mm < m; mm++ {
		yearday += timeDaysInMonth(y, mm)
	}
	return timeCivil{year: y, month: m, day: d, weekday: weekday, hour: hour, min: min, sec: sec2, nsec: nsec, yearday: yearday, days: days}
}

// Format renders t per the Go reference layout (M9: civil-date
// implementation; synthetic Time is UTC-only).
func (t Time) Format(layout string) string {
	// Civil decomposition is shared with the calendar accessors
	// (timeDecompose); Format renders UTC, as before.
	c := timeDecompose(t)
	y := c.year
	m := c.month
	d := c.day
	days := c.days
	hour := c.hour
	min := c.min
	sec2 := c.sec
	nsec := c.nsec

	var b []byte
	i := 0
	for i < len(layout) {
		c := layout[i]
		switch c {
		case 'J': // January, Jan
			if hasPrefix(layout[i:], "January") {
				b = append(b, monthName(m)...)
				i += len("January")
			} else if hasPrefix(layout[i:], "Jan") {
				b = append(b, monthName(m)[:3]...)
				i += len("Jan")
			} else {
				b = append(b, c)
				i++
			}
		case 'M': // Mon, Monday
			if hasPrefix(layout[i:], "Monday") {
				b = append(b, weekdayName(days)...)
				i += len("Monday")
			} else if hasPrefix(layout[i:], "Mon") {
				b = append(b, weekdayName(days)[:3]...)
				i += len("Mon")
			} else {
				b = append(b, c)
				i++
			}
		case '0': // 01, 02, 06, 15, 04, 05
			switch {
			case hasPrefix(layout[i:], "01"):
				b = append(b, byte('0'+m/10), byte('0'+m%10))
				i += 2
			case hasPrefix(layout[i:], "02"):
				b = append(b, byte('0'+d/10), byte('0'+d%10))
				i += 2
			case hasPrefix(layout[i:], "06"):
				yy := y % 100
				b = append(b, byte('0'+yy/10), byte('0'+yy%10))
				i += 2
			case hasPrefix(layout[i:], "03"):
				h12 := hour % 12
				if h12 == 0 {
					h12 = 12
				}
				b = append(b, byte('0'+h12/10), byte('0'+h12%10))
				i += 2
			case hasPrefix(layout[i:], "04"):
				b = append(b, byte('0'+min/10), byte('0'+min%10))
				i += 2
			case hasPrefix(layout[i:], "05"):
				b = append(b, byte('0'+sec2/10), byte('0'+sec2%10))
				i += 2
			default:
				b = append(b, c)
				i++
			}
		case '1': // 15, 1, 01 handled above
			if hasPrefix(layout[i:], "15") {
				b = append(b, byte('0'+hour/10), byte('0'+hour%10))
				i += 2
			} else {
				b = append(b, byte('0'+m))
				i++
			}
		case '2': // 2006, 2, _2, 02 handled above
			switch {
			case hasPrefix(layout[i:], "2006"):
				ys := itoa4(y)
				b = append(b, ys...)
				i += 4
			case hasPrefix(layout[i:], "_2"):
				if d < 10 {
					b = append(b, ' ', byte('0'+d))
				} else {
					b = append(b, byte('0'+d/10), byte('0'+d%10))
				}
				i += 2
			default:
				b = append(b, byte('0'+d))
				i++
			}
		case '_': // _2, _2006
			if hasPrefix(layout[i:], "_2") {
				if d < 10 {
					b = append(b, ' ', byte('0'+d))
				} else {
					b = append(b, byte('0'+d/10), byte('0'+d%10))
				}
				i += 2
			} else {
				b = append(b, c)
				i++
			}
		case '3': // 3 (12h)
			h12 := hour % 12
			if h12 == 0 {
				h12 = 12
			}
			b = append(b, byte('0'+h12))
			i++
		case '4': // 4 (minute)
			b = append(b, byte('0'+min))
			i++
		case '5': // 5 (second)
			b = append(b, byte('0'+sec2))
			i++
		case 'P': // PM
			if hour < 12 {
				b = append(b, "AM"...)
			} else {
				b = append(b, "PM"...)
			}
			i += 2
		case 'p': // pm
			if hour < 12 {
				b = append(b, "am"...)
			} else {
				b = append(b, "pm"...)
			}
			i += 2
		case '.': // fractional seconds .0, .00, ...
			j := i + 1
			for j < len(layout) && layout[j] == '0' {
				j++
			}
			if j > i+1 {
				prec := j - (i + 1)
				frac := nsec
				// scale to prec digits
				div := int64(1)
				for k := 0; k < 9-prec; k++ {
					div *= 10
				}
				v := frac / div
				mult := int64(1)
				for k := 0; k < prec; k++ {
					mult *= 10
				}
				// zero-pad to prec
				tmp := v
				digits := make([]byte, prec)
				for k := prec - 1; k >= 0; k-- {
					digits[k] = byte('0' + tmp%10)
					tmp /= 10
				}
				_ = mult
				b = append(b, '.')
				b = append(b, digits...)
				i = j
			} else {
				b = append(b, c)
				i++
			}
		case 'Z': // Z0700, Z07:00, Z07 -> Z (UTC-only)
			switch {
			case hasPrefix(layout[i:], "Z0700"):
				b = append(b, 'Z')
				i += 5
			case hasPrefix(layout[i:], "Z07:00"):
				b = append(b, 'Z')
				i += 6
			case hasPrefix(layout[i:], "Z07"):
				b = append(b, 'Z')
				i += 3
			default:
				b = append(b, c)
				i++
			}
		case '-': // -0700, -07:00, -07 -> +0000 etc (UTC-only)
			switch {
			case hasPrefix(layout[i:], "-0700"):
				b = append(b, "+0000"...)
				i += 5
			case hasPrefix(layout[i:], "-07:00"):
				b = append(b, "+00:00"...)
				i += 6
			case hasPrefix(layout[i:], "-07"):
				b = append(b, "+00"...)
				i += 3
			default:
				b = append(b, c)
				i++
			}
		default:
			b = append(b, c)
			i++
		}
	}
	return string(b)
}

// P3-driver-harden (jsonlib lane): AppendFormat (gc 1.27.1 semantics).
func (t Time) AppendFormat(b []byte, layout string) []byte {
	return append(b, t.Format(layout)...)
}

// monthName returns the full English month name (1-based).
func monthName(m int64) string {
	switch m {
	case 1:
		return "January"
	case 2:
		return "February"
	case 3:
		return "March"
	case 4:
		return "April"
	case 5:
		return "May"
	case 6:
		return "June"
	case 7:
		return "July"
	case 8:
		return "August"
	case 9:
		return "September"
	case 10:
		return "October"
	case 11:
		return "November"
	case 12:
		return "December"
	}
	return ""
}

// weekdayName returns the full English weekday name for days since
// 1970-01-01 (which was a Thursday).
func weekdayName(days int64) string {
	w := (days + 4) % 7
	if w < 0 {
		w += 7
	}
	switch w {
	case 0:
		return "Sunday"
	case 1:
		return "Monday"
	case 2:
		return "Tuesday"
	case 3:
		return "Wednesday"
	case 4:
		return "Thursday"
	case 5:
		return "Friday"
	case 6:
		return "Saturday"
	}
	return ""
}

// itoa4 renders a year as 4+ digits (no truncation for y > 9999).
func itoa4(y int64) []byte {
	if y < 0 {
		return append([]byte{'-'}, itoa4(-y)...)
	}
	var rev []byte
	if y == 0 {
		rev = []byte{'0'}
	} else {
		for y > 0 {
			rev = append(rev, byte('0'+y%10))
			y /= 10
		}
	}
	for len(rev) < 4 {
		rev = append(rev, '0')
	}
	out := make([]byte, len(rev))
	for i, c := range rev {
		out[len(rev)-1-i] = c
	}
	return out
}

func hasPrefix(s, p string) bool {
	return len(s) >= len(p) && s[:len(p)] == p
}
// Calendar decomposition (R0-2): the Date() constructor already stored
// the instant as unixnano; these accessors decompose it through
// timeDecompose, so a Date() result round-trips instead of panicking.
// The synthetic Time is UTC-only (the same documented divergence class
// as Local-as-UTC): In returns t for any non-nil location, Location
// returns UTC, Zone returns ("UTC", 0). AddDate normalizes through the
// Date() constructor, exactly like gc (Jan 31 + 1 month = Mar 2/3).
func (t Time) GoString() string {
	c := timeDecompose(t)
	return "time.Date(" + timeIntString(c.year) + ", time." + monthName(c.month) + ", " + timeIntString(c.day) + ", " + timeIntString(c.hour) + ", " + timeIntString(c.min) + ", " + timeIntString(c.sec) + ", " + timeIntString(c.nsec) + ", time.UTC)"
}
func (t Time) AddDate(years int, months int, days int) Time {
	c := timeDecompose(t)
	return Date(int(c.year)+years, Month(c.month)+Month(months), int(c.day)+days, int(c.hour), int(c.min), int(c.sec), int(c.nsec), UTC)
}
func (t Time) Date() (year int, month Month, day int) {
	c := timeDecompose(t)
	return int(c.year), Month(c.month), int(c.day)
}
func (t Time) Year() int        { return int(timeDecompose(t).year) }
func (t Time) Month() Month     { return Month(timeDecompose(t).month) }
func (t Time) Day() int         { return int(timeDecompose(t).day) }
func (t Time) Weekday() Weekday { return Weekday(timeDecompose(t).weekday) }
func (t Time) Clock() (int, int, int) {
	c := timeDecompose(t)
	return int(c.hour), int(c.min), int(c.sec)
}
func (t Time) Hour() int       { return int(timeDecompose(t).hour) }
func (t Time) Minute() int     { return int(timeDecompose(t).min) }
func (t Time) Second() int     { return int(timeDecompose(t).sec) }
func (t Time) Nanosecond() int { return int(timeDecompose(t).nsec) }
func (t Time) YearDay() int    { return int(timeDecompose(t).yearday) }
func (t Time) UTC() Time       { return t }
func (t Time) Local() Time     { return t }
func (t Time) In(loc *Location) Time {
	if loc == nil {
		panic("time: missing Location in call to Time.In")
	}
	return t
}
func (t Time) Location() *Location { return UTC }
func (t Time) Zone() (string, int) { return "UTC", 0 }
func (t Time) UnixMilli() int64 {
	if t.IsZero() {
		return -62135596800000
	}
	return timeFloorDiv(t.unixnano, 1e6)
}
func (t Time) UnixMicro() int64 {
	if t.IsZero() {
		return -62135596800000000
	}
	return timeFloorDiv(t.unixnano, 1e3)
}

// Location exists so *Location-typed signatures type-check. Local is
// accepted as UTC (ADR-0005: documented divergence until P3-M7).
type Location struct {
	name string
}

var UTC *Location = &Location{name: "UTC"}
var Local *Location = &Location{name: "UTC"}

func (l *Location) String() string {
	if l == nil || l.name == "" {
		return "UTC"
	}
	return l.name
}

// timeParseQuote quotes s the way gc's ParseError does: '"' and '\'
// escaped, other bytes below 0x20 or at/above 0x7f as \xNN.
func timeParseQuote(s string) string {
	b := make([]byte, 0, len(s)+2)
	b = append(b, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' {
			b = append(b, '\\', '"')
		} else if c == '\\' {
			b = append(b, '\\', '\\')
		} else if c < 0x20 || c >= 0x7f {
			b = append(b, '\\', 'x')
			b = append(b, "0123456789abcdef"[c>>4], "0123456789abcdef"[c&0xf])
		} else {
			b = append(b, c)
		}
	}
	b = append(b, '"')
	return string(b)
}

// timeLayoutError is gc's time.ParseError for the synthetic parser:
// mismatch errors carry the full layout ("as" form); range and
// extra-text errors do not.
type timeLayoutError struct {
	layout string
	value  string
	elem   string
	rest   string
	msg    string
}

func (e *timeLayoutError) Error() string {
	if e.elem != "" {
		return "parsing time " + timeParseQuote(e.value) + " as " + timeParseQuote(e.layout) + ": cannot parse " + timeParseQuote(e.rest) + " as " + timeParseQuote(e.elem)
	}
	return "parsing time " + timeParseQuote(e.value) + ": " + e.msg
}

func timeParseMismatch(layout, value, rest, elem string) error {
	return &timeLayoutError{layout: layout, value: value, elem: elem, rest: rest}
}

func timeParseRangeErr(value, msg string) error {
	return &timeLayoutError{value: value, msg: msg}
}

func timeParseExtraErr(value, rest string) error {
	return &timeLayoutError{value: value, msg: "extra text: " + timeParseQuote(rest)}
}

// timeParsedNum is the single-result carrier for the numeric parsers
// (synthetic sources avoid multi-result helpers, cf. __parseDuration).
type timeParsedNum struct {
	v    int64
	rest string
	ok   bool
}

// timeParseFixedNum parses exactly n digits (gc getnum, fixed width).
func timeParseFixedNum(value string, n int) timeParsedNum {
	if len(value) < n {
		return timeParsedNum{}
	}
	var v int64
	for i := 0; i < n; i++ {
		c := value[i]
		if c < '0' || c > '9' {
			return timeParsedNum{}
		}
		v = v*10 + int64(c-'0')
	}
	return timeParsedNum{v: v, rest: value[n:], ok: true}
}

// timeParseVarNum parses 1..maxDigits digits (gc getnum, variable width).
func timeParseVarNum(value string, maxDigits int) timeParsedNum {
	var v int64
	i := 0
	for i < len(value) && i < maxDigits {
		c := value[i]
		if c < '0' || c > '9' {
			break
		}
		v = v*10 + int64(c-'0')
		i++
	}
	if i == 0 {
		return timeParsedNum{}
	}
	return timeParsedNum{v: v, rest: value[i:], ok: true}
}

func timeLowerByte(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// timeMatchFold reports whether value starts with name, case-insensitively
// (name is already lowercase).
func timeMatchFold(value, name string) bool {
	if len(value) < len(name) {
		return false
	}
	for i := 0; i < len(name); i++ {
		if timeLowerByte(value[i]) != name[i] {
			return false
		}
	}
	return true
}

// timeNameMatch is the single-result carrier for the name parsers.
type timeNameMatch struct {
	v    int64
	rest string
	ok   bool
}

func timeParseMonthAbbrev(value string) timeNameMatch {
	if len(value) < 3 {
		return timeNameMatch{}
	}
	abbr := string([]byte{timeLowerByte(value[0]), timeLowerByte(value[1]), timeLowerByte(value[2])})
	switch abbr {
	case "jan":
		return timeNameMatch{v: 1, rest: value[3:], ok: true}
	case "feb":
		return timeNameMatch{v: 2, rest: value[3:], ok: true}
	case "mar":
		return timeNameMatch{v: 3, rest: value[3:], ok: true}
	case "apr":
		return timeNameMatch{v: 4, rest: value[3:], ok: true}
	case "may":
		return timeNameMatch{v: 5, rest: value[3:], ok: true}
	case "jun":
		return timeNameMatch{v: 6, rest: value[3:], ok: true}
	case "jul":
		return timeNameMatch{v: 7, rest: value[3:], ok: true}
	case "aug":
		return timeNameMatch{v: 8, rest: value[3:], ok: true}
	case "sep":
		return timeNameMatch{v: 9, rest: value[3:], ok: true}
	case "oct":
		return timeNameMatch{v: 10, rest: value[3:], ok: true}
	case "nov":
		return timeNameMatch{v: 11, rest: value[3:], ok: true}
	case "dec":
		return timeNameMatch{v: 12, rest: value[3:], ok: true}
	}
	return timeNameMatch{}
}

func timeParseMonthLong(value string) timeNameMatch {
	if timeMatchFold(value, "january") {
		return timeNameMatch{v: 1, rest: value[7:], ok: true}
	}
	if timeMatchFold(value, "february") {
		return timeNameMatch{v: 2, rest: value[8:], ok: true}
	}
	if timeMatchFold(value, "march") {
		return timeNameMatch{v: 3, rest: value[5:], ok: true}
	}
	if timeMatchFold(value, "april") {
		return timeNameMatch{v: 4, rest: value[5:], ok: true}
	}
	if timeMatchFold(value, "may") {
		return timeNameMatch{v: 5, rest: value[3:], ok: true}
	}
	if timeMatchFold(value, "june") {
		return timeNameMatch{v: 6, rest: value[4:], ok: true}
	}
	if timeMatchFold(value, "july") {
		return timeNameMatch{v: 7, rest: value[4:], ok: true}
	}
	if timeMatchFold(value, "august") {
		return timeNameMatch{v: 8, rest: value[6:], ok: true}
	}
	if timeMatchFold(value, "september") {
		return timeNameMatch{v: 9, rest: value[9:], ok: true}
	}
	if timeMatchFold(value, "october") {
		return timeNameMatch{v: 10, rest: value[7:], ok: true}
	}
	if timeMatchFold(value, "november") {
		return timeNameMatch{v: 11, rest: value[8:], ok: true}
	}
	if timeMatchFold(value, "december") {
		return timeNameMatch{v: 12, rest: value[8:], ok: true}
	}
	return timeNameMatch{}
}

func timeParseWeekdayAbbrev(value string) timeNameMatch {
	if len(value) < 3 {
		return timeNameMatch{}
	}
	abbr := string([]byte{timeLowerByte(value[0]), timeLowerByte(value[1]), timeLowerByte(value[2])})
	switch abbr {
	case "sun":
		return timeNameMatch{v: 0, rest: value[3:], ok: true}
	case "mon":
		return timeNameMatch{v: 1, rest: value[3:], ok: true}
	case "tue":
		return timeNameMatch{v: 2, rest: value[3:], ok: true}
	case "wed":
		return timeNameMatch{v: 3, rest: value[3:], ok: true}
	case "thu":
		return timeNameMatch{v: 4, rest: value[3:], ok: true}
	case "fri":
		return timeNameMatch{v: 5, rest: value[3:], ok: true}
	case "sat":
		return timeNameMatch{v: 6, rest: value[3:], ok: true}
	}
	return timeNameMatch{}
}

func timeParseWeekdayLong(value string) timeNameMatch {
	if timeMatchFold(value, "sunday") {
		return timeNameMatch{v: 0, rest: value[6:], ok: true}
	}
	if timeMatchFold(value, "monday") {
		return timeNameMatch{v: 1, rest: value[6:], ok: true}
	}
	if timeMatchFold(value, "tuesday") {
		return timeNameMatch{v: 2, rest: value[7:], ok: true}
	}
	if timeMatchFold(value, "wednesday") {
		return timeNameMatch{v: 3, rest: value[9:], ok: true}
	}
	if timeMatchFold(value, "thursday") {
		return timeNameMatch{v: 4, rest: value[8:], ok: true}
	}
	if timeMatchFold(value, "friday") {
		return timeNameMatch{v: 5, rest: value[6:], ok: true}
	}
	if timeMatchFold(value, "saturday") {
		return timeNameMatch{v: 6, rest: value[8:], ok: true}
	}
	return timeNameMatch{}
}

// timeParsedFrac is the single-result carrier for the fraction parsers.
type timeParsedFrac struct {
	nsec int64
	rest string
	ok   bool
}

// timeParseFixedFrac parses a fixed-width fraction: separator ('.' or
// ',') plus exactly digits digits. ok == false means mismatch.
func timeParseFixedFrac(value string, digits int) timeParsedFrac {
	if len(value) < 1+digits {
		return timeParsedFrac{}
	}
	sep := value[0]
	if sep != '.' && sep != ',' {
		return timeParsedFrac{}
	}
	var v int64
	for i := 0; i < digits; i++ {
		c := value[1+i]
		if c < '0' || c > '9' {
			return timeParsedFrac{}
		}
		if i < 9 {
			v = v*10 + int64(c-'0')
		}
	}
	if digits <= 9 {
		mult := int64(1)
		for k := 0; k < 9-digits; k++ {
			mult *= 10
		}
		v *= mult
	}
	return timeParsedFrac{nsec: v, rest: value[1+digits:], ok: true}
}

// timeParseVarFrac parses a variable-width fraction: separator plus one
// or more digits, truncated to 9 digits. ok == false means "no fraction
// present" (no separator, or no digit after it): the caller skips the
// layout fraction element and leaves value untouched, like gc.
func timeParseVarFrac(value string) timeParsedFrac {
	if len(value) < 2 {
		return timeParsedFrac{rest: value}
	}
	sep := value[0]
	if sep != '.' && sep != ',' {
		return timeParsedFrac{rest: value}
	}
	c := value[1]
	if c < '0' || c > '9' {
		return timeParsedFrac{rest: value}
	}
	var v int64
	ndigits := 0
	i := 1
	for i < len(value) {
		c = value[i]
		if c < '0' || c > '9' {
			break
		}
		if ndigits < 9 {
			v = v*10 + int64(c-'0')
		}
		ndigits++
		i++
	}
	if ndigits <= 9 {
		mult := int64(1)
		for k := 0; k < 9-ndigits; k++ {
			mult *= 10
		}
		v *= mult
	}
	return timeParsedFrac{nsec: v, rest: value[i:], ok: true}
}

// timeParsedZone is the single-result carrier for the zone-offset parser.
type timeParsedZone struct {
	offset int64
	hour   int64
	sec    int64
	hasSec bool
	rest   string
	ok     bool
}

// timeParseZoneOffset parses a zone offset of the given shape after an
// optional 'Z' (zForm only). shape is one of "07", "0700", "070000",
// "07:00", "07:00:00". Minutes are not range-checked, like gc; hour and
// seconds are validated by the caller.
func timeParseZoneOffset(value string, zForm bool, shape string) timeParsedZone {
	if zForm && len(value) > 0 && value[0] == 'Z' {
		return timeParsedZone{rest: value[1:], ok: true}
	}
	if len(value) == 0 {
		return timeParsedZone{}
	}
	sign := value[0]
	if sign != '+' && sign != '-' {
		return timeParsedZone{}
	}
	rest := value[1:]
	var hh, mm, ss int64
	hasSec := false
	switch shape {
	case "07":
		num := timeParseFixedNum(rest, 2)
		if !num.ok {
			return timeParsedZone{}
		}
		hh = num.v
		rest = num.rest
	case "0700":
		num := timeParseFixedNum(rest, 4)
		if !num.ok {
			return timeParsedZone{}
		}
		hh = num.v / 100
		mm = num.v % 100
		rest = num.rest
	case "070000":
		num := timeParseFixedNum(rest, 6)
		if !num.ok {
			return timeParsedZone{}
		}
		hh = num.v / 10000
		mm = (num.v / 100) % 100
		ss = num.v % 100
		hasSec = true
		rest = num.rest
	case "07:00":
		num := timeParseFixedNum(rest, 2)
		if !num.ok {
			return timeParsedZone{}
		}
		hh = num.v
		rest = num.rest
		if len(rest) == 0 || rest[0] != ':' {
			return timeParsedZone{}
		}
		num = timeParseFixedNum(rest[1:], 2)
		if !num.ok {
			return timeParsedZone{}
		}
		mm = num.v
		rest = num.rest
	case "07:00:00":
		num := timeParseFixedNum(rest, 2)
		if !num.ok {
			return timeParsedZone{}
		}
		hh = num.v
		rest = num.rest
		if len(rest) == 0 || rest[0] != ':' {
			return timeParsedZone{}
		}
		num = timeParseFixedNum(rest[1:], 2)
		if !num.ok {
			return timeParsedZone{}
		}
		mm = num.v
		rest = num.rest
		if len(rest) == 0 || rest[0] != ':' {
			return timeParsedZone{}
		}
		num = timeParseFixedNum(rest[1:], 2)
		if !num.ok {
			return timeParsedZone{}
		}
		ss = num.v
		hasSec = true
		rest = num.rest
	default:
		return timeParsedZone{}
	}
	offset := hh*3600 + mm*60 + ss
	if sign == '-' {
		offset = -offset
	}
	return timeParsedZone{offset: offset, hour: hh, sec: ss, hasSec: hasSec, rest: rest, ok: true}
}

// timeParseTokenAt reports the layout token starting at layout[0]
// (longest match wins), or "" when layout[0] does not start a token.
func timeParseTokenAt(layout string) string {
	if layout == "" {
		return ""
	}
	switch layout[0] {
	case 'J':
		if hasPrefix(layout, "January") {
			return "January"
		}
		if hasPrefix(layout, "Jan") {
			return "Jan"
		}
	case 'M':
		if hasPrefix(layout, "Monday") {
			return "Monday"
		}
		if hasPrefix(layout, "Mon") {
			return "Mon"
		}
		if hasPrefix(layout, "MST") {
			return "MST"
		}
	case '2':
		if hasPrefix(layout, "2006") {
			return "2006"
		}
		return "2"
	case '0':
		if hasPrefix(layout, "002") {
			return "002"
		}
		if hasPrefix(layout, "01") {
			return "01"
		}
		if hasPrefix(layout, "02") {
			return "02"
		}
		if hasPrefix(layout, "03") {
			return "03"
		}
		if hasPrefix(layout, "04") {
			return "04"
		}
		if hasPrefix(layout, "05") {
			return "05"
		}
		if hasPrefix(layout, "06") {
			return "06"
		}
	case '1':
		if hasPrefix(layout, "15") {
			return "15"
		}
		return "1"
	case '3':
		if hasPrefix(layout, "03") {
			return "03"
		}
		return "3"
	case '4':
		if hasPrefix(layout, "04") {
			return "04"
		}
		return "4"
	case '5':
		if hasPrefix(layout, "05") {
			return "05"
		}
		return "5"
	case 'P':
		if hasPrefix(layout, "PM") {
			return "PM"
		}
	case 'p':
		if hasPrefix(layout, "pm") {
			return "pm"
		}
	case 'Z':
		if hasPrefix(layout, "Z07:00:00") {
			return "Z07:00:00"
		}
		if hasPrefix(layout, "Z07:00") {
			return "Z07:00"
		}
		if hasPrefix(layout, "Z0700") {
			return "Z0700"
		}
		if hasPrefix(layout, "Z07") {
			return "Z07"
		}
	case '-':
		if hasPrefix(layout, "-07:00:00") {
			return "-07:00:00"
		}
		if hasPrefix(layout, "-07:00") {
			return "-07:00"
		}
		if hasPrefix(layout, "-070000") {
			return "-070000"
		}
		if hasPrefix(layout, "-0700") {
			return "-0700"
		}
		if hasPrefix(layout, "-07") {
			return "-07"
		}
	case '_':
		if hasPrefix(layout, "__2") {
			return "__2"
		}
		if hasPrefix(layout, "_2") {
			return "_2"
		}
	case '.', ',':
		if len(layout) > 1 && (layout[1] == '0' || layout[1] == '9') {
			i := 1
			for i < len(layout) && (layout[i] == '0' || layout[i] == '9') {
				i++
			}
			return layout[:i]
		}
	}
	return ""
}

// timeParseChunk is the single-result carrier for timeParseNext.
type timeParseChunk struct {
	lit string
	tok string
}

// timeParseNext splits layout into the literal prefix before the next
// token and that token ("" when no token remains).
func timeParseNext(layout string) timeParseChunk {
	for i := 0; i < len(layout); i++ {
		if tok := timeParseTokenAt(layout[i:]); tok != "" {
			return timeParseChunk{lit: layout[:i], tok: tok}
		}
	}
	return timeParseChunk{lit: layout}
}

// timeParseAutoFracOK reports whether a fractional second in value may
// be consumed right after a seconds element: only when the layout is
// exhausted, or the next layout element is a non-fraction token. When
// the next element IS a fraction element, that element parses it
// instead (a fixed-width fraction must see the digits to reject a
// short fraction, rather than having them auto-consumed).
func timeParseAutoFracOK(layout string) bool {
	if layout == "" {
		return true
	}
	chunk := timeParseNext(layout)
	if chunk.tok == "" {
		return false
	}
	if chunk.tok[0] == '.' || chunk.tok[0] == ',' {
		return false
	}
	return true
}

// Parse parses a formatted string and returns the time value it
// represents (gc 1.27.1 semantics for the standard layout elements).
// UTC-only model, like the rest of the synthetic package: a parsed zone
// offset shifts the instant (wall clock minus offset) but the zone
// itself is not retained — decomposition always reads UTC, and Zone()
// returns ("UTC", 0). A zone abbreviation (MST element) parses exactly
// three uppercase letters with offset 0, like gc's unknown-abbreviation
// rule; the name is discarded.
func Parse(layout, value string) (Time, error) {
	origLayout := layout
	origValue := value
	year := int64(0)
	month := int64(1)
	day := int64(1)
	hour := int64(0)
	min := int64(0)
	sec := int64(0)
	nsec := int64(0)
	pmSet := false
	pm := false
	hourIs12 := false
	monthSet := false
	daySet := false
	yday := int64(0)
	ydaySet := false
	zoneOffset := int64(0)
	zoneHour := int64(0)
	zoneSec := int64(0)
	zoneHasSec := false
	for layout != "" {
		chunk := timeParseNext(layout)
		if chunk.lit != "" {
			if !hasPrefix(value, chunk.lit) {
				return Time{}, timeParseMismatch(origLayout, origValue, value, chunk.lit)
			}
			value = value[len(chunk.lit):]
			layout = layout[len(chunk.lit):]
		}
		if chunk.tok == "" {
			break
		}
		tok := chunk.tok
		layout = layout[len(tok):]
		switch tok {
		case "2006":
			num := timeParseFixedNum(value, 4)
			if !num.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			year = num.v
			value = num.rest
		case "06":
			num := timeParseFixedNum(value, 2)
			if !num.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			if num.v >= 69 {
				year = 1900 + num.v
			} else {
				year = 2000 + num.v
			}
			value = num.rest
		case "01":
			num := timeParseFixedNum(value, 2)
			if !num.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			month = num.v
			monthSet = true
			value = num.rest
			// gc checks the month range immediately, so a bad month
			// beats any later token failure (e.g. "2006-13" / "2024-13").
			if month < 1 || month > 12 {
				return Time{}, timeParseRangeErr(origValue, "month out of range")
			}
		case "1":
			num := timeParseVarNum(value, 2)
			if !num.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			month = num.v
			monthSet = true
			value = num.rest
			if month < 1 || month > 12 {
				return Time{}, timeParseRangeErr(origValue, "month out of range")
			}
		case "02":
			num := timeParseFixedNum(value, 2)
			if !num.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			day = num.v
			daySet = true
			value = num.rest
		case "2":
			num := timeParseVarNum(value, 2)
			if !num.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			day = num.v
			daySet = true
			value = num.rest
		case "_2":
			v := value
			if len(v) > 0 && v[0] == ' ' {
				v = v[1:]
			}
			num := timeParseVarNum(v, 2)
			if !num.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			day = num.v
			daySet = true
			value = num.rest
		case "002":
			num := timeParseFixedNum(value, 3)
			if !num.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			yday = num.v
			ydaySet = true
			value = num.rest
		case "__2":
			v := value
			spaces := 0
			for spaces < 2 && len(v) > 0 && v[0] == ' ' {
				v = v[1:]
				spaces++
			}
			num := timeParseVarNum(v, 3)
			if !num.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			yday = num.v
			ydaySet = true
			value = num.rest
		case "15":
			num := timeParseFixedNum(value, 2)
			if !num.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			hour = num.v
			hourIs12 = false
			value = num.rest
			// Hour range is checked immediately, pre-PM-adjustment
			// (a 12-hour hour is 0..12 even with PM: "3:04PM" /
			// "13:45PM" is an hour error, and "25:x" beats the
			// later minute mismatch).
			if hour < 0 || hour > 23 {
				return Time{}, timeParseRangeErr(origValue, "hour out of range")
			}
		case "03":
			num := timeParseFixedNum(value, 2)
			if !num.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			hour = num.v
			hourIs12 = true
			value = num.rest
			if hour < 0 || hour > 12 {
				return Time{}, timeParseRangeErr(origValue, "hour out of range")
			}
		case "3":
			num := timeParseVarNum(value, 2)
			if !num.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			hour = num.v
			hourIs12 = true
			value = num.rest
			if hour < 0 || hour > 12 {
				return Time{}, timeParseRangeErr(origValue, "hour out of range")
			}
		case "04":
			num := timeParseFixedNum(value, 2)
			if !num.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			min = num.v
			value = num.rest
			if min < 0 || min > 59 {
				return Time{}, timeParseRangeErr(origValue, "minute out of range")
			}
		case "4":
			num := timeParseVarNum(value, 2)
			if !num.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			min = num.v
			value = num.rest
			if min < 0 || min > 59 {
				return Time{}, timeParseRangeErr(origValue, "minute out of range")
			}
		case "05":
			num := timeParseFixedNum(value, 2)
			if !num.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			sec = num.v
			value = num.rest
			if sec < 0 || sec > 59 {
				return Time{}, timeParseRangeErr(origValue, "second out of range")
			}
			if timeParseAutoFracOK(layout) {
				fr := timeParseVarFrac(value)
				if fr.ok {
					nsec = fr.nsec
					value = fr.rest
				}
			}
		case "5":
			num := timeParseVarNum(value, 2)
			if !num.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			sec = num.v
			value = num.rest
			if sec < 0 || sec > 59 {
				return Time{}, timeParseRangeErr(origValue, "second out of range")
			}
			if timeParseAutoFracOK(layout) {
				fr := timeParseVarFrac(value)
				if fr.ok {
					nsec = fr.nsec
					value = fr.rest
				}
			}
		case "PM":
			if hasPrefix(value, "PM") {
				pm = true
				pmSet = true
				value = value[2:]
			} else if hasPrefix(value, "AM") {
				pm = false
				pmSet = true
				value = value[2:]
			} else {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
		case "pm":
			if hasPrefix(value, "pm") {
				pm = true
				pmSet = true
				value = value[2:]
			} else if hasPrefix(value, "am") {
				pm = false
				pmSet = true
				value = value[2:]
			} else {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
		case "Monday":
			nm := timeParseWeekdayLong(value)
			if !nm.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			value = nm.rest
		case "Mon":
			nm := timeParseWeekdayAbbrev(value)
			if !nm.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			value = nm.rest
		case "January":
			nm := timeParseMonthLong(value)
			if !nm.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			month = nm.v
			monthSet = true
			value = nm.rest
		case "Jan":
			nm := timeParseMonthAbbrev(value)
			if !nm.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			month = nm.v
			monthSet = true
			value = nm.rest
		case "MST":
			if len(value) < 3 || value[0] < 'A' || value[0] > 'Z' || value[1] < 'A' || value[1] > 'Z' || value[2] < 'A' || value[2] > 'Z' {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			zoneOffset = 0
			zoneHour = 0
			zoneSec = 0
			zoneHasSec = false
			value = value[3:]
		case "Z07:00:00":
			zn := timeParseZoneOffset(value, true, "07:00:00")
			if !zn.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			zoneOffset = zn.offset
			zoneHour = zn.hour
			zoneSec = zn.sec
			zoneHasSec = zn.hasSec
			value = zn.rest
		case "Z07:00":
			zn := timeParseZoneOffset(value, true, "07:00")
			if !zn.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			zoneOffset = zn.offset
			zoneHour = zn.hour
			zoneSec = zn.sec
			zoneHasSec = zn.hasSec
			value = zn.rest
		case "Z0700":
			zn := timeParseZoneOffset(value, true, "0700")
			if !zn.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			zoneOffset = zn.offset
			zoneHour = zn.hour
			zoneSec = zn.sec
			zoneHasSec = zn.hasSec
			value = zn.rest
		case "Z07":
			zn := timeParseZoneOffset(value, true, "07")
			if !zn.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			zoneOffset = zn.offset
			zoneHour = zn.hour
			zoneSec = zn.sec
			zoneHasSec = zn.hasSec
			value = zn.rest
		case "-07:00:00":
			zn := timeParseZoneOffset(value, false, "07:00:00")
			if !zn.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			zoneOffset = zn.offset
			zoneHour = zn.hour
			zoneSec = zn.sec
			zoneHasSec = zn.hasSec
			value = zn.rest
		case "-07:00":
			zn := timeParseZoneOffset(value, false, "07:00")
			if !zn.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			zoneOffset = zn.offset
			zoneHour = zn.hour
			zoneSec = zn.sec
			zoneHasSec = zn.hasSec
			value = zn.rest
		case "-070000":
			zn := timeParseZoneOffset(value, false, "070000")
			if !zn.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			zoneOffset = zn.offset
			zoneHour = zn.hour
			zoneSec = zn.sec
			zoneHasSec = zn.hasSec
			value = zn.rest
		case "-0700":
			zn := timeParseZoneOffset(value, false, "0700")
			if !zn.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			zoneOffset = zn.offset
			zoneHour = zn.hour
			zoneSec = zn.sec
			zoneHasSec = zn.hasSec
			value = zn.rest
		case "-07":
			zn := timeParseZoneOffset(value, false, "07")
			if !zn.ok {
				return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
			}
			zoneOffset = zn.offset
			zoneHour = zn.hour
			zoneSec = zn.sec
			zoneHasSec = zn.hasSec
			value = zn.rest
		default:
			// Fractional-second element (separator + '0'/'9' run).
			run := tok[1:]
			if len(run) > 0 && run[0] == '0' {
				fr := timeParseFixedFrac(value, len(run))
				if !fr.ok {
					return Time{}, timeParseMismatch(origLayout, origValue, value, tok)
				}
				nsec = fr.nsec
				value = fr.rest
			} else {
				fr := timeParseVarFrac(value)
				if fr.ok {
					nsec = fr.nsec
					value = fr.rest
				}
			}
		}
		// Zone offset ranges are checked immediately too, so a bad
		// offset beats any later token failure or extra text
		// ("+25x" reports the offset hour, not extra text). This
		// runs after every element; non-zone elements leave the
		// last zone's values untouched, and a bad zone already
		// returned on its own iteration.
		if zoneHour > 24 {
			return Time{}, timeParseRangeErr(origValue, "time zone offset hour out of range")
		}
		if zoneHasSec && zoneSec > 59 {
			return Time{}, timeParseRangeErr(origValue, "time zone offset second out of range")
		}
	}
	if value != "" {
		return Time{}, timeParseExtraErr(origValue, value)
	}
	// 12-hour adjustment, after all elements are parsed: the hour
	// range was already checked pre-adjustment (0..12), so the
	// adjusted hour is always 0..23 here. A 24-hour hour is never
	// adjusted, even with a PM element ("15:04PM" / "13:45PM" == 13:45).
	if hourIs12 && pmSet {
		if pm && hour < 12 {
			hour += 12
		} else if !pm && hour == 12 {
			hour = 0
		}
	}
	// Remaining validation, in gc's order: day-of-year range/match,
	// then day. (Month, hour, minute, second, and zone offset ranges
	// were already checked immediately, during the loop; extra text
	// was checked just above, so it beats day-of-year/day errors.)
	if ydaySet {
		maxYday := int64(365)
		if timeIsLeapYear(year) {
			maxYday = 366
		}
		if yday < 1 || yday > maxYday {
			return Time{}, timeParseRangeErr(origValue, "day-of-year out of range")
		}
		md := timeMonthDayFromYearDay(year, yday)
		if monthSet && month != md.month {
			return Time{}, timeParseRangeErr(origValue, "day-of-year does not match month")
		}
		if daySet && day != md.day {
			return Time{}, timeParseRangeErr(origValue, "day-of-year does not match day")
		}
		month = md.month
		day = md.day
	} else {
		if day < 1 || day > timeDaysInMonth(year, month) {
			return Time{}, timeParseRangeErr(origValue, "day out of range")
		}
	}
	base := Date(int(year), Month(month), int(day), int(hour), int(min), int(sec), int(nsec), UTC)
	return Time{unixnano: base.unixnano - zoneOffset*1e9, isSet: true}, nil
}

func Date(year int, month Month, day, hour, min, sec, nsec int, loc *Location) Time {
	if loc == nil {
		panic("time: missing Location in call to Date")
	}
	if loc.name != "" && loc.name != "UTC" {
		panic("time.Date: non-UTC locations not in scope (re-test: synthetic zoneinfo)")
	}
	// Normalize nsec→sec→min→hour→day with floored division, like gc.
	sec64 := int64(sec) + timeFloorDiv(int64(nsec), 1e9)
	nsec64 := timeFloorMod(int64(nsec), 1e9)
	min64 := int64(min) + timeFloorDiv(sec64, 60)
	sec64 = timeFloorMod(sec64, 60)
	hour64 := int64(hour) + timeFloorDiv(min64, 60)
	min64 = timeFloorMod(min64, 60)
	day64 := int64(day) + timeFloorDiv(hour64, 24)
	hour64 = timeFloorMod(hour64, 24)
	// Normalize month (1-based) into the year.
	m0 := int64(month) - 1
	y64 := int64(year) + timeFloorDiv(m0, 12)
	m0 = timeFloorMod(m0, 12)
	// days_from_civil (Howard Hinnant's algorithm), proleptic Gregorian;
	// day64 may lie outside the month — the formula absorbs the offset.
	yy := y64
	mo := m0 + 1 // 1-based, in [1,12]
	if mo <= 2 {
		yy--
	}
	era := yy / 400
	if yy < 0 && yy%400 != 0 {
		era--
	}
	yoe := yy - era*400
	mp := mo - 3
	if mp < 0 {
		mp += 12
	}
	doy := (153*mp+2)/5 + day64 - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	days := era*146097 + doe - 719468 // days since 1970-01-01
	unixSec := days*86400 + hour64*3600 + min64*60 + sec64
	return Time{unixnano: unixSec*1e9 + nsec64, isSet: true}
}

// Unix returns the Time corresponding to the given Unix time, sec seconds
// and nsec nanoseconds since January 1, 1970 UTC. nsec outside
// [0, 999999999] is normalized, like gc.
func Unix(sec int64, nsec int64) Time {
	sec += timeFloorDiv(nsec, 1e9)
	nsec = timeFloorMod(nsec, 1e9)
	return Time{unixnano: sec*1e9 + nsec, isSet: true}
}

// timeFloorDiv/timeFloorMod are Euclidean division/modulo (Go's / and %
// truncate toward zero; Date/Unix normalization needs floored semantics
// for negative operands).
func timeFloorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

func timeFloorMod(a, b int64) int64 {
	r := a % b
	if r != 0 && (r < 0) != (b < 0) {
		r += b
	}
	return r
}

// Month is a 1-based month number with gc's String rendering.
type Month int

const (
	January Month = iota + 1
	February
	March
	April
	May
	June
	July
	August
	September
	October
	November
	December
)

func (m Month) String() string {
	switch m {
	case January:
		return "January"
	case February:
		return "February"
	case March:
		return "March"
	case April:
		return "April"
	case May:
		return "May"
	case June:
		return "June"
	case July:
		return "July"
	case August:
		return "August"
	case September:
		return "September"
	case October:
		return "October"
	case November:
		return "November"
	case December:
		return "December"
	}
	return "%!Month(" + timeIntString(int64(m)) + ")"
}

// Weekday is a 0-based day of the week (Sunday = 0) with gc's String.
type Weekday int

const (
	Sunday Weekday = iota
	Monday
	Tuesday
	Wednesday
	Thursday
	Friday
	Saturday
)

func (d Weekday) String() string {
	switch d {
	case Sunday:
		return "Sunday"
	case Monday:
		return "Monday"
	case Tuesday:
		return "Tuesday"
	case Wednesday:
		return "Wednesday"
	case Thursday:
		return "Thursday"
	case Friday:
		return "Friday"
	case Saturday:
		return "Saturday"
	}
	return "%!Weekday(" + timeIntString(int64(d)) + ")"
}

func timeIntString(v int64) string {
	neg := v < 0
	var u uint64
	if neg {
		u = uint64(-(v + 1)) + 1
	} else {
		u = uint64(v)
	}
	var arr [24]byte
	w := len(arr)
	if u == 0 {
		w--
		arr[w] = '0'
	} else {
		for u > 0 {
			w--
			arr[w] = byte(u%10) + '0'
			u /= 10
		}
	}
	if neg {
		w--
		arr[w] = '-'
	}
	return string(arr[w:])
}

// Since returns the time elapsed since t: Now().Sub(t).
func Since(t Time) Duration { return Now().Sub(t) }

// Until returns the duration until t: t.Sub(Now()).
func Until(t Time) Duration { return t.Sub(Now()) }

// Timer is opaque: the Go value IS the Java timer object (ADR-0002
// sync.Mutex pattern). The C field exists for type-checking; the
// exporter intercepts .C selections into the "timec" wire node.
type Timer struct {
	C <-chan Time
}

// Ticker is opaque, like Timer.
type Ticker struct {
	C <-chan Time
}

// Clock/timer intrinsics: exact gc 1.27.1 signatures; the bodies never
// run (the exporter intercepts every call into the §D3 wire nodes).
func Now() Time                          { panic("time: intrinsic Now") }
func Sleep(d Duration)                   { panic("time: intrinsic Sleep") }
func After(d Duration) <-chan Time       { panic("time: intrinsic After") }
func Tick(d Duration) <-chan Time        { panic("time: intrinsic Tick") }
func NewTimer(d Duration) *Timer         { panic("time: intrinsic NewTimer") }
func NewTicker(d Duration) *Ticker       { panic("time: intrinsic NewTicker") }
func AfterFunc(d Duration, f func()) *Timer {
	panic("time: intrinsic AfterFunc")
}

func (t *Timer) Stop() bool {
	panic("time: intrinsic Timer.Stop")
}

func (t *Timer) Reset(d Duration) bool {
	panic("time: intrinsic Timer.Reset")
}

func (t *Ticker) Stop() { panic("time: intrinsic Ticker.Stop") }

func (t *Ticker) Reset(d Duration) { panic("time: intrinsic Ticker.Reset") }
`

// syntheticContextSource is the P3-M5 synthetic `context` package
// (ADR-0005 D2, Appendix M): REAL Go on the channel-mutex design — no
// Java intrinsics, no goroutine leaks. It composes channel operations
// (make/send/recv/close/select, Appendix C), synthetic time, and P3-M3
// interface dispatch (a combination test of M2+M3+M5). Exact gc 1.27.1
// signatures for the in-scope API.
//
// Design (Appendix M):
//   - cancelCtx carries done (closed on cancel, broadcast), cancelCh
//     (buffer 1; occupancy = "cancel claimed"), err/cause, muChan (binary
//     semaphore, buffer 1, pre-filled), children, afterFuncs.
//   - cancel() claims via select { case cancelCh <- err: ...;
//     case <-done: }: exactly one winner; losers wait for done.
//     Parent/child propagation is synchronous (no watcher goroutine —
//     the launcher's awaitQuiescence hangs on parked goroutines, unlike
//     gc which exits with main).
//   - Lock discipline: snapshot children/hooks under muChan, recurse and
//     spawn hook goroutines WITHOUT holding the lock (the context tree
//     is a DAG — no cycles).
//   - The 6 Appendix M multi-result operations keep exact gc signatures
//     with fail-fast bodies (for type-checking); the exporter desugars
//     the 5 reachable ones' 2-target call sites to the __withCancel/
//     __withCancelCause/__withDeadline/__withTimeout/__deadlineOf
//     single-result helpers plus cancelFunc/cancelCauseFunc methods
//     (Appendix M; Deadline() is not callable in M5). The concrete
//     Deadline() methods are declared for go/types interface
//     satisfaction only — no call site type-checks (the synthetic
//     interface omits Deadline post-afc0dfb), so their fail-fast
//     bodies are unreachable.
//   - AfterFunc registers a hook run at cancel time (go f() per hook,
//     spawned by the canceler — never a parked watcher goroutine).
//
// Documented M5 limitations (Appendix M): user-defined Context
// implementations get no async parent-cancel propagation, no AfterFunc
// hook, and no deadline (their Deadline method cannot be called —
// multi-value dispatch needs WS D).
const syntheticContextSource = `package context

import "time"

type ctxErrorString struct {
	s string
}

func (e *ctxErrorString) Error() string { return e.s }

var Canceled error = &ctxErrorString{s: "context canceled"}
var DeadlineExceeded error = &ctxErrorString{s: "context deadline exceeded"}

// Context is the gc 1.27.1 interface (method set identical).
type Context interface {
	// Deadline() restored (P4): the general interface multi-result mechanism
	// (046afc86) now supports multi-result interface methods. This removes
	// the P3-M5 deliberate divergence.
	Deadline() (deadline time.Time, ok bool)
	Done() <-chan struct{}
	Err() error
	Value(key any) any
}

type emptyCtx struct{}

// Deadline is declared for go/types interface satisfaction only; the
// exporter desugars every Deadline() call site (Appendix M), so this
// fail-fast body is unreachable.
func (emptyCtx) Deadline() (deadline time.Time, ok bool) {
	panic("context: emptyCtx.Deadline: not in M5 scope (multi-result calls need WS D; re-test when it lands)")
}
func (emptyCtx) Done() <-chan struct{} { return ctxNeverFire }
func (emptyCtx) Err() error            { return nil }
func (emptyCtx) Value(key any) any     { return nil }

// ctxNeverFire is the ADR-0005 fallback for emptyCtx.Done: a
// package-level never-closed channel. (Nil would be gc-exact, but the
// runtime's channel receive throws on nil channels instead of blocking.)
var ctxNeverFire = make(chan struct{})

var ctxBackground = &emptyCtx{}
var ctxTODO = &emptyCtx{}

func Background() Context { return ctxBackground }
func TODO() Context       { return ctxTODO }

// CancelFunc is gc's func().
type CancelFunc func()

// CancelCauseFunc is gc's func(cause error).
type CancelCauseFunc func(cause error)

// the multi-result Deadline method.

// cancelCtx is the channel-mutex context (ADR-0005 Appendix M).
type cancelCtx struct {
	parent Context
	done   chan struct{} // closed on cancel (broadcast)
	// cancelCh has buffer 1; occupancy = "cancel claimed". Exactly one
	// cancel() call wins the send; losers take the <-done branch.
	cancelCh chan error
	err      error
	cause    error
	// muChan is a binary semaphore (buffer 1, pre-filled at
	// construction) guarding children/afterFuncs/err/cause.
	muChan     chan struct{}
	children   map[*cancelCtx]struct{}
	afterFuncs []*afterFuncEntry
}

type afterFuncEntry struct {
	f       func()
	stopped bool
	ran     bool
}

func newCancelCtx(parent Context) *cancelCtx {
	c := &cancelCtx{
		parent:   parent,
		done:     make(chan struct{}),
		cancelCh: make(chan error, 1),
		muChan:   make(chan struct{}, 1),
	}
	c.muChan <- struct{}{} // pre-fill the binary semaphore
	return c
}

// Deadline is declared for go/types interface satisfaction only (see
// emptyCtx.Deadline).
func (c *cancelCtx) Deadline() (deadline time.Time, ok bool) {
	panic("context: cancelCtx.Deadline: not in M5 scope (multi-result calls need WS D; re-test when it lands)")
}

func (c *cancelCtx) Done() <-chan struct{} { return c.done }

func (c *cancelCtx) Err() error {
	select {
	case <-c.done:
		// done is closed after err is set (program order in the
		// winner); receiving the close synchronizes-with it, so no
		// lock is needed for the read.
		return c.err
	default:
		return nil
	}
}

func (c *cancelCtx) Value(key any) any { return c.parent.Value(key) }

// cancel claims cancellation via cancelCh and propagates synchronously.
// No goroutine is spawned or parked here (the launcher's
// awaitQuiescence hangs on parked goroutines, unlike gc).
func (c *cancelCtx) cancel(err, cause error) {
	if err == nil {
		panic("context: internal error: missing cancel error")
	}
	select {
	case c.cancelCh <- err:
		// Won the claim. Acquire muChan BEFORE publishing err/cause:
		// readers (Cause(), ctxPropagateCancel, AfterFunc) access them
		// under muChan, so the write must hold it for the happens-before
		// edge (P1-3). The cancelCh claim synchronizes the winner election
		// only, not the payload.
		<-c.muChan
		c.err = err
		c.cause = cause
		close(c.done)
		children := c.children
		c.children = nil
		hooks := c.afterFuncs
		c.afterFuncs = nil
		for _, h := range hooks {
			h.ran = true
		}
		parent := c.parent
		c.muChan <- struct{}{}
		ctxRemoveChild(parent, c)
		for child := range children {
			child.cancel(err, cause)
		}
		for _, h := range hooks {
			if !h.stopped {
				f := h.f
				go f()
			}
		}
	case <-c.done:
		// Already canceled: the winner is closing/closed done.
	}
}

// cancelFunc/cancelCauseFunc are the Appendix M desugar targets: the
// second result of WithCancel/WithCancelCause.
func (c *cancelCtx) cancelFunc() CancelFunc {
	return func() {
		c.cancel(Canceled, Canceled)
		ctxRemoveChild(c.parent, c)
	}
}

func (c *cancelCtx) cancelCauseFunc() CancelCauseFunc {
	return func(cause error) {
		c.cancel(Canceled, cause)
		ctxRemoveChild(c.parent, c)
	}
}

// __withCancel/__withCancelCause are the Appendix M desugar targets:
// the single-result constructors behind WithCancel/WithCancelCause.
func __withCancel(parent Context) *cancelCtx {
	if parent == nil {
		panic("cannot create context from nil parent")
	}
	c := newCancelCtx(parent)
	ctxPropagateCancel(parent, c)
	return c
}

func __withCancelCause(parent Context) *cancelCtx {
	if parent == nil {
		panic("cannot create context from nil parent")
	}
	c := newCancelCtx(parent)
	ctxPropagateCancel(parent, c)
	return c
}

// WithCancel/WithCancelCause keep exact gc 1.27.1 signatures for
// type-checking. The exporter desugars 2-target call sites (Appendix M);
// every other position fails fast. Bodies never run.
func WithCancel(parent Context) (Context, CancelFunc) {
	panic("context.WithCancel: not in M5 scope (multi-result calls need WS D; re-test when it lands)")
}

func WithCancelCause(parent Context) (Context, CancelCauseFunc) {
	panic("context.WithCancelCause: not in M5 scope (multi-result calls need WS D; re-test when it lands)")
}

// Cause returns the cause of c's cancellation: the cause for synthetic
// contexts, else c.Err() (gc parity).
func Cause(c Context) error {
	if cc := ctxUnwrapCancel(c); cc != nil {
		<-cc.muChan
		cause := cc.cause
		cc.muChan <- struct{}{}
		return cause
	}
	return c.Err()
}

// ctxUnwrapCancel finds the *cancelCtx driving parent, through valueCtx
// and timerCtx wrappers. Nil for exotic (user-defined) Contexts.
func ctxUnwrapCancel(parent Context) *cancelCtx {
	switch p := parent.(type) {
	case *cancelCtx:
		return p
	case *timerCtx:
		return p.cancel
	case *valueCtx:
		return ctxUnwrapCancel(p.parent)
	}
	return nil
}

// ctxPropagateCancel arranges synchronous child cancellation: if the
// parent is already canceled the child cancels immediately; otherwise
// the child registers in the parent's children set (no watcher
// goroutine — see cancel). Exotic parents get no async propagation
// (documented M5 limitation).
func ctxPropagateCancel(parent Context, child *cancelCtx) {
	if parent.Done() == nil {
		return
	}
	if pc := ctxUnwrapCancel(parent); pc != nil {
		<-pc.muChan
		if pc.err != nil {
			err, cause := pc.err, pc.cause
			pc.muChan <- struct{}{}
			child.cancel(err, cause)
			return
		}
		if pc.children == nil {
			pc.children = make(map[*cancelCtx]struct{})
		}
		pc.children[child] = struct{}{}
		pc.muChan <- struct{}{}
		return
	}
}

func ctxRemoveChild(parent Context, child *cancelCtx) {
	if pc := ctxUnwrapCancel(parent); pc != nil {
		<-pc.muChan
		delete(pc.children, child)
		pc.muChan <- struct{}{}
	}
}

type timerCtx struct {
	cancel   *cancelCtx
	timer    *time.Timer
	deadline time.Time
}

// Deadline is declared for go/types interface satisfaction only (see
// emptyCtx.Deadline).
func (c *timerCtx) Deadline() (deadline time.Time, ok bool) {
	panic("context: timerCtx.Deadline: not in M5 scope (multi-result calls need WS D; re-test when it lands)")
}

func (c *timerCtx) Done() <-chan struct{} { return c.cancel.Done() }
func (c *timerCtx) Err() error             { return c.cancel.Err() }
func (c *timerCtx) Value(key any) any      { return c.cancel.Value(key) }

func (c *timerCtx) cancelTimer(err, cause error) {
	c.cancel.cancel(err, cause)
	if c.timer != nil {
		c.timer.Stop()
	}
}

// cancelFunc is the Appendix M desugar target for the WithDeadline/
// WithTimeout cancel function (stops the timer too).
func (c *timerCtx) cancelFunc() CancelFunc {
	return func() { c.cancelTimer(Canceled, Canceled) }
}

// __withDeadline/__withTimeout are the Appendix M desugar targets.
func __withDeadline(parent Context, d time.Time) *timerCtx {
	if parent == nil {
		panic("cannot create context from nil parent")
	}
	c := &timerCtx{cancel: newCancelCtx(parent), deadline: d}
	ctxPropagateCancel(parent, c.cancel)
	until := time.Until(d)
	if until <= 0 {
		c.cancelTimer(DeadlineExceeded, DeadlineExceeded)
	} else if c.cancel.Err() == nil {
		// The parent was not already canceled (gc checks c.err under the
		// lock before arming; Err() observes the same state via the
		// done-close edge). Arming after a parent cancel would leave a
		// timer that can only no-op fire later (reviewer1 P2-8: gc's
		// timerCtx.cancel stops the timer; here the timer is unreachable
		// from the propagation path, so the fire would be a wasted
		// goroutine). Residual race (parent cancels after the check but
		// before AfterFunc arms) still no-op fires — benign, as before.
		c.timer = time.AfterFunc(until, func() {
			c.cancelTimer(DeadlineExceeded, DeadlineExceeded)
		})
	}
	return c
}

func __withTimeout(parent Context, timeout time.Duration) *timerCtx {
	return __withDeadline(parent, time.Now().Add(timeout))
}

// WithDeadline/WithTimeout keep exact gc signatures for type-checking;
// the exporter desugars 2-target call sites (Appendix M), every other
// position fails fast. Bodies never run.
func WithDeadline(parent Context, d time.Time) (Context, CancelFunc) {
	panic("context.WithDeadline: not in M5 scope (multi-result calls need WS D; re-test when it lands)")
}

func WithTimeout(parent Context, timeout time.Duration) (Context, CancelFunc) {
	panic("context.WithTimeout: not in M5 scope (multi-result calls need WS D; re-test when it lands)")
}

func WithDeadlineCause(parent Context, d time.Time, cause error) (Context, CancelFunc) {
	panic("context.WithDeadlineCause: not in M5 scope")
}

func WithTimeoutCause(parent Context, timeout time.Duration, cause error) (Context, CancelFunc) {
	panic("context.WithTimeoutCause: not in M5 scope")
}

// deadlinePair is the Appendix M single-result carrier for Deadline().
type deadlinePair struct {
	d  time.Time
	ok bool
}

// __deadlineOf is the Appendix M desugar target behind ctx.Deadline().
// User-defined Contexts report no deadline (their Deadline method cannot
// be called — multi-value dispatch needs WS D; documented M5
// limitation).
func __deadlineOf(c Context) deadlinePair {
	switch c := c.(type) {
	case *timerCtx:
		return deadlinePair{d: c.deadline, ok: true}
	case *valueCtx:
		return __deadlineOf(c.parent)
	case *cancelCtx:
		return deadlinePair{}
	case emptyCtx, *emptyCtx:
		return deadlinePair{}
	case *withoutCancelCtx:
		return deadlinePair{}
	default:
		return deadlinePair{}
	}
}

type valueCtx struct {
	parent Context
	key    any
	val    any
}

func (c *valueCtx) Deadline() (deadline time.Time, ok bool) {
	// Declared for go/types interface satisfaction only (see
	// emptyCtx.Deadline). The pre-amendment real body (delegating to
	// __deadlineOf(c.parent)) is removed: it is unreachable (the
	// synthetic Context interface omits Deadline, so no call site
	// type-checks, and nothing inside the synthetic package calls it —
	// __deadlineOf handles *valueCtx via its own type-switch), and a
	// silently-succeeding outlier among fail-fast siblings would mask a
	// desugar escape (reviewer1 P2-5).
	panic("context: valueCtx.Deadline: not in M5 scope (multi-result calls need WS D; re-test when it lands)")
}

func (c *valueCtx) Done() <-chan struct{} { return c.parent.Done() }
func (c *valueCtx) Err() error             { return c.parent.Err() }

func (c *valueCtx) Value(key any) any {
	if c.key == key {
		return c.val
	}
	return c.parent.Value(key)
}

func WithValue(parent Context, key, val any) Context {
	if parent == nil {
		panic("cannot create context from nil parent")
	}
	if key == nil {
		panic("nil key")
	}
	return &valueCtx{parent: parent, key: key, val: val}
}

// AfterFunc arranges f to run in its own goroutine when ctx is done,
// returning a stop function (gc signature). The hook is registered on
// the driving cancelCtx and spawned by the canceler — no parked watcher
// goroutine (which would hang the launcher's awaitQuiescence).
func AfterFunc(ctx Context, f func()) (stop func() bool) {
	if ctx.Err() != nil {
		go f()
		return func() bool { return false }
	}
	cc := ctxUnwrapCancel(ctx)
	if cc == nil {
		// Exotic (user-defined) parent: no hook possible without a
		// parked watcher goroutine (documented M5 limitation), so f never
		// runs. stop() reports true on the first call (gc parity: the
		// function was stopped before it started), false thereafter.
		// (Reviewer1 P2-6: the old code returned false unconditionally.)
		mu := make(chan struct{}, 1)
		mu <- struct{}{}
		stopped := false
		return func() bool {
			<-mu
			defer func() { mu <- struct{}{} }()
			if stopped {
				return false
			}
			stopped = true
			return true
		}
	}
	e := &afterFuncEntry{f: f}
	<-cc.muChan
	if cc.err != nil {
		cc.muChan <- struct{}{}
		go f()
		return func() bool { return false }
	}
	cc.afterFuncs = append(cc.afterFuncs, e)
	cc.muChan <- struct{}{}
	return func() bool {
		<-cc.muChan
		if e.stopped || e.ran {
			cc.muChan <- struct{}{}
			return false
		}
		e.stopped = true
		cc.muChan <- struct{}{}
		return true
	}
}

type withoutCancelCtx struct {
	parent Context
}

// Deadline is declared for go/types interface satisfaction only (see
// emptyCtx.Deadline).
func (c *withoutCancelCtx) Deadline() (deadline time.Time, ok bool) {
	panic("context: withoutCancelCtx.Deadline: not in M5 scope (multi-result calls need WS D; re-test when it lands)")
}
func (c *withoutCancelCtx) Done() <-chan struct{} { return ctxNeverFire }
func (c *withoutCancelCtx) Err() error             { return nil }
func (c *withoutCancelCtx) Value(key any) any {
	return c.parent.Value(key)
}

func WithoutCancel(parent Context) Context {
	if parent == nil {
		panic("cannot create context from nil parent")
	}
	// gc 1.27.1 always wraps (no Done()==nil shortcut — verified
	// against the gc source and empirically: WithoutCancel(Background())
	// != Background()). The old "if parent.Done() == nil { return parent }"
	// check was dead for synthetic parents (emptyCtx.Done() is never nil)
	// and wrong for exotic ones, so it is removed. (Reviewer1 P2-7 —
	// the reviewer's gc premise was stale; corrected per gc 1.27.1.)
	return &withoutCancelCtx{parent: parent}
}
`
