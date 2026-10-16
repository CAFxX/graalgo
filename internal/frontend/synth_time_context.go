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
	return t.unixnano / 1e9
}
func (t Time) UnixNano() int64 { return t.unixnano }

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

// Format renders t per the Go reference layout (M9: civil-date
// implementation; synthetic Time is UTC-only).
func (t Time) Format(layout string) string {
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
func (t Time) GoString() string { panic("time.Time.GoString: not in M5 scope") }
func (t Time) AddDate(years int, months int, days int) Time {
	panic("time.Time.AddDate: not in M5 scope")
}
func (t Time) Date() (year int, month Month, day int) {
	panic("time.Time.Date: not in M5 scope")
}
func (t Time) Year() int          { panic("time.Time.Year: not in M5 scope") }
func (t Time) Month() Month       { panic("time.Time.Month: not in M5 scope") }
func (t Time) Day() int           { panic("time.Time.Day: not in M5 scope") }
func (t Time) Weekday() Weekday   { panic("time.Time.Weekday: not in M5 scope") }
func (t Time) Clock() (int, int, int) {
	panic("time.Time.Clock: not in M5 scope")
}
func (t Time) Hour() int        { panic("time.Time.Hour: not in M5 scope") }
func (t Time) Minute() int      { panic("time.Time.Minute: not in M5 scope") }
func (t Time) Second() int      { panic("time.Time.Second: not in M5 scope") }
func (t Time) Nanosecond() int  { panic("time.Time.Nanosecond: not in M5 scope") }
func (t Time) YearDay() int     { panic("time.Time.YearDay: not in M5 scope") }
func (t Time) UTC() Time        { return t }
func (t Time) Local() Time      { return t }
func (t Time) In(loc *Location) Time {
	panic("time.Time.In: not in M5 scope")
}
func (t Time) Location() *Location { panic("time.Time.Location: not in M5 scope") }
func (t Time) Zone() (string, int) {
	panic("time.Time.Zone: not in M5 scope")
}
func (t Time) UnixMilli() int64 { return t.unixnano / 1e6 }
func (t Time) UnixMicro() int64 { return t.unixnano / 1e3 }

// Location exists so *Location-typed signatures type-check. Local is
// accepted as UTC (ADR-0005: documented divergence until P3-M7).
// offset records seconds east of UTC for zones built by FixedZone
// (gc 1.27.1 semantics); zone arithmetic (Time.In/Zone/Location) stays
// out of scope and fails fast there.
type Location struct {
	name   string
	offset int
}

var UTC *Location = &Location{name: "UTC"}
var Local *Location = &Location{name: "UTC"}

func (l *Location) String() string {
	if l == nil || l.name == "" {
		return "UTC"
	}
	return l.name
}

// unnamedFixedZones mirrors gc 1.27.1's cache (time/zoneinfo.go):
// FixedZone("", k*3600) returns the same *Location for whole hours
// k in -12..+14 (pointer identity is observable via ==).
var unnamedFixedZones [27]*Location

func init() {
	for hr := -12; hr <= 14; hr++ {
		unnamedFixedZones[hr+12] = &Location{offset: hr * 60 * 60}
	}
}

// FixedZone returns a [Location] that always uses the given zone name
// and offset (seconds east of UTC), matching gc 1.27.1 (time/zoneinfo.go)
// including its unnamed whole-hour cache. (P0 fix 2026-10-16:
// archive/zip's timeZone calls this; the missing declaration made the
// synthetic archive/zip package fail type-check, panicking any user
// import of archive/zip.)
func FixedZone(name string, offset int) *Location {
	if hour := offset / 60 / 60; name == "" && -12 <= hour && hour <= 14 && hour*60*60 == offset {
		return unnamedFixedZones[hour+12]
	}
	return &Location{name: name, offset: offset}
}

func Parse(layout, value string) (Time, error) {
	panic("time.Parse: not in M5 scope")
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
	// NB: Deadline() omitted (P3-M5, ADR-0005): v1 cannot emit multi-result
	// methods (WS D), and M5 scope does not need Context.Deadline() — all
	// call sites are desugared via __deadlineOf (Appendix M) or out of scope.
	// This is a deliberate, documented divergence from gc's interface.
	// Re-test when WS D lands.
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
