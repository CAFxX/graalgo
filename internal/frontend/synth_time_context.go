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
//     __withCancelCause/__withDeadline/__withTimeout/__withDeadlineCause/
//     __withTimeoutCause/__deadlineOf single-result desugar targets behind
//     the 8 Appendix M multi-result operations (7 reachable in M5 —
//     Context.Deadline() is not callable post-afc0dfb; which keep exact gc
//     signatures with fail-fast bodies for type-checking).
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
// Reset) with panic bodies for the exporter to intercept; time.Parse keeps
// its exact gc signature with a fail-fast body while the real parser is
// __parseTime (M7 desugar table entry).
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
	p := __timeDayParts(t)
	days := p.days
	secsOfDay := p.secsOfDay
	nsec := p.nsec
	// Hinnant civil-from-days: days since 1970-01-01 -> y/m/d.
	civ := __timeCivilFromDays(days)
	y := civ.y
	m := civ.m
	d := civ.d
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

// dayParts is the single-result carrier for the unixnano decomposition
// (multi-result calls need WS D; see durationPair).
type dayParts struct {
	days      int64 // days since 1970-01-01 (floored)
	secsOfDay int64 // [0, 86399]
	nsec      int64 // [0, 999999999]
}

// __timeDayParts splits t.unixnano into days + time-of-day (UTC),
// mirroring Format's split. The zero instant is special-cased: its
// unixnano wraps (see zeroInstantNano), so the raw split would not land
// on (1,1,1); by definition (IsZero) it denotes January 1, year 1,
// 00:00:00 UTC, i.e. -719162 days since 1970-01-01.
func __timeDayParts(t Time) dayParts {
	if t.IsZero() {
		return dayParts{days: -719162}
	}
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
	return dayParts{days: days, secsOfDay: secsOfDay, nsec: nsec}
}

// civilDate is the single-result carrier for the civil-date
// decomposition (multi-result calls need WS D).
type civilDate struct {
	y, m, d int64
}

// __timeCivilFromDays converts days since 1970-01-01 to the proleptic
// Gregorian (year, month, day) via Howard Hinnant's civil_from_days —
// the inverse of the days_from_civil in Date.
func __timeCivilFromDays(days int64) civilDate {
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
		y++
	}
	return civilDate{y: y, m: m, d: d}
}

// __timeDaysFromCivil converts a proleptic Gregorian (year, 1-based
// month, day) to days since 1970-01-01 (Howard Hinnant's
// days_from_civil). The day may lie outside the month — the formula
// absorbs the offset, which is how Date normalizes out-of-range days.
func __timeDaysFromCivil(y, mo, d int64) int64 {
	yy := y
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
	doy := (153*mp+2)/5 + d - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe - 719468
}

func (t Time) GoString() string {
	p := __timeDayParts(t)
	c := __timeCivilFromDays(p.days)
	hh := p.secsOfDay / 3600
	mm := (p.secsOfDay % 3600) / 60
	ss := p.secsOfDay % 60
	// gc renders time.Date(y, time.Month, d, hh, mm, ss, nsec, time.UTC);
	// the synthetic model is UTC-only, so the location is always UTC.
	return "time.Date(" + timeIntString(c.y) + ", time." + monthName(c.m) + ", " +
		timeIntString(c.d) + ", " + timeIntString(hh) + ", " + timeIntString(mm) + ", " +
		timeIntString(ss) + ", " + timeIntString(p.nsec) + ", time.UTC)"
}
func (t Time) AddDate(years int, months int, days int) Time {
	p := __timeDayParts(t)
	c := __timeCivilFromDays(p.days)
	hh := int(p.secsOfDay / 3600)
	mm := int((p.secsOfDay % 3600) / 60)
	ss := int(p.secsOfDay % 60)
	// gc: Date(y+years, m+months, d+days, hh, mm, ss, nsec, t.Location());
	// t.Location() is UTC in the synthetic model.
	return Date(int(c.y)+years, Month(c.m)+Month(months), int(c.d)+days,
		hh, mm, ss, int(p.nsec), UTC)
}

// Date returns the civil date (UTC). Multi-result: served via the WS D
// pair-transform like any real multi-result synthetic member.
func (t Time) Date() (year int, month Month, day int) {
	p := __timeDayParts(t)
	c := __timeCivilFromDays(p.days)
	return int(c.y), Month(c.m), int(c.d)
}
func (t Time) Year() int    { return int(__timeCivilFromDays(__timeDayParts(t).days).y) }
func (t Time) Month() Month { return Month(__timeCivilFromDays(__timeDayParts(t).days).m) }
func (t Time) Day() int     { return int(__timeCivilFromDays(__timeDayParts(t).days).d) }
func (t Time) Weekday() Weekday {
	w := (__timeDayParts(t).days + 4) % 7
	if w < 0 {
		w += 7
	}
	return Weekday(w)
}

// Clock returns the time-of-day (UTC). Multi-result: WS D pair-transform.
func (t Time) Clock() (hour, min, sec int) {
	s := __timeDayParts(t).secsOfDay
	return int(s / 3600), int((s % 3600) / 60), int(s % 60)
}
func (t Time) Hour() int       { return int(__timeDayParts(t).secsOfDay / 3600) }
func (t Time) Minute() int     { return int((__timeDayParts(t).secsOfDay % 3600) / 60) }
func (t Time) Second() int     { return int(__timeDayParts(t).secsOfDay % 60) }
func (t Time) Nanosecond() int { return int(__timeDayParts(t).nsec) }
func (t Time) YearDay() int {
	p := __timeDayParts(t)
	c := __timeCivilFromDays(p.days)
	return int(p.days-__timeDaysFromCivil(c.y, 1, 1)) + 1
}
func (t Time) UTC() Time   { return t }
func (t Time) Local() Time { return t }
func (t Time) In(loc *Location) Time {
	if loc == nil {
		panic("time: missing Location in call to Time.In")
	}
	if loc.name != "" && loc.name != "UTC" {
		panic("time.Time.In: non-UTC locations not in scope (needs zoneinfo design)")
	}
	return t
}

// Location reports UTC: the synthetic model is UTC-only (ADR-0005:
// Local is accepted as UTC), so every Time is in UTC.
func (t Time) Location() *Location { return UTC }

// Zone reports the UTC zone. Multi-result: WS D pair-transform.
func (t Time) Zone() (name string, offset int) {
	return "UTC", 0
}

// UnixMilli/UnixMicro: gc computes sec*1e3 + nsec/1e6 (floored seconds),
// not truncated nanos — the two differ for negative instants. The zero
// instant is special-cased like Unix().
func (t Time) UnixMilli() int64 {
	if t.IsZero() {
		return -62135596800 * 1e3
	}
	sec := t.unixnano / 1e9
	nsec := t.unixnano % 1e9
	if nsec < 0 {
		nsec += 1e9
		sec--
	}
	return sec*1e3 + nsec/1e6
}
func (t Time) UnixMicro() int64 {
	if t.IsZero() {
		return -62135596800 * 1e6
	}
	sec := t.unixnano / 1e9
	nsec := t.unixnano % 1e9
	if nsec < 0 {
		nsec += 1e9
		sec--
	}
	return sec*1e6 + nsec/1e3
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

// ParseError describes a problem parsing a time string: exact gc 1.27.1
// API (fields + Error), so programs type-asserting *time.ParseError keep
// working.
type ParseError struct {
	Layout     string
	Value      string
	LayoutElem string
	ValueElem  string
	Message    string
}

func (e *ParseError) Error() string {
	if e.Message == "" {
		return "parsing time " + __parseTimeQuote(e.Value) + " as " +
			__parseTimeQuote(e.Layout) + ": cannot parse " +
			__parseTimeQuote(e.ValueElem) + " as " +
			__parseTimeQuote(e.LayoutElem)
	}
	return "parsing time " + __parseTimeQuote(e.Value) + e.Message
}

// Layout-element codes for the Parse tokenizer (clean-room numbering;
// the tokenization mirrors gc's nextStdChunk).
const (
	__ptimeStdNone                  = iota
	__ptimeStdLongMonth             // "January"
	__ptimeStdMonth                 // "Jan"
	__ptimeStdNumMonth              // "1"
	__ptimeStdZeroMonth             // "01"
	__ptimeStdLongWeekDay           // "Monday"
	__ptimeStdWeekDay               // "Mon"
	__ptimeStdDay                   // "2"
	__ptimeStdUnderDay              // "_2"
	__ptimeStdZeroDay               // "02"
	__ptimeStdUnderYearDay          // "__2"
	__ptimeStdZeroYearDay           // "002"
	__ptimeStdHour                  // "15"
	__ptimeStdHour12                // "3"
	__ptimeStdZeroHour12            // "03"
	__ptimeStdMinute                // "4"
	__ptimeStdZeroMinute            // "04"
	__ptimeStdSecond                // "5"
	__ptimeStdZeroSecond            // "05"
	__ptimeStdLongYear              // "2006"
	__ptimeStdYear                  // "06"
	__ptimeStdPM                    // "PM"
	__ptimeStdpm                    // "pm"
	__ptimeStdTZ                    // "MST"
	__ptimeStdISO8601TZ             // "Z0700"
	__ptimeStdISO8601SecondsTZ      // "Z070000"
	__ptimeStdISO8601ShortTZ        // "Z07"
	__ptimeStdISO8601ColonTZ        // "Z07:00"
	__ptimeStdISO8601ColonSecondsTZ // "Z07:00:00"
	__ptimeStdNumTZ                 // "-0700"
	__ptimeStdNumSecondsTZ          // "-070000"
	__ptimeStdNumShortTZ            // "-07"
	__ptimeStdNumColonTZ            // "-07:00"
	__ptimeStdNumColonSecondsTZ     // "-07:00:00"
	__ptimeStdFracSecond0           // ".0", ".00", ... (exact digit count)
	__ptimeStdFracSecond9           // ".9", ".99", ... (optional)
)

// __ptimeStd0x maps "01".."06" to their element codes.
var __ptimeStd0x = [...]int{
	__ptimeStdZeroMonth, __ptimeStdZeroDay, __ptimeStdZeroHour12,
	__ptimeStdZeroMinute, __ptimeStdZeroSecond, __ptimeStdYear,
}

// __ptimeChunk is one tokenized layout element.
type __ptimeChunk struct {
	prefix     string
	code       int
	fracDigits int  // fractional-second digit count (frac codes only)
	fracComma  bool // separator was ',' (frac codes only)
	suffix     string
}

func __ptimeIsDigit(s string, i int) bool {
	if len(s) <= i {
		return false
	}
	c := s[i]
	return '0' <= c && c <= '9'
}

func __ptimeStartsLower(str string) bool {
	if len(str) == 0 {
		return false
	}
	c := str[0]
	return 'a' <= c && c <= 'z'
}

// __ptimeNextChunk finds the first layout element in layout and returns
// the text before it, the element, and the text after it (mirrors gc's
// nextStdChunk, including the startsWithLowerCase guards and the
// longest-match order).
func __ptimeNextChunk(layout string) __ptimeChunk {
	for i := 0; i < len(layout); i++ {
		switch c := layout[i]; c {
		case 'J': // January, Jan
			if len(layout) >= i+3 && layout[i:i+3] == "Jan" {
				if len(layout) >= i+7 && layout[i:i+7] == "January" {
					return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdLongMonth, suffix: layout[i+7:]}
				}
				if !__ptimeStartsLower(layout[i+3:]) {
					return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdMonth, suffix: layout[i+3:]}
				}
			}
		case 'M': // Monday, Mon, MST
			if len(layout) >= i+3 {
				if layout[i:i+3] == "Mon" {
					if len(layout) >= i+6 && layout[i:i+6] == "Monday" {
						return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdLongWeekDay, suffix: layout[i+6:]}
					}
					if !__ptimeStartsLower(layout[i+3:]) {
						return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdWeekDay, suffix: layout[i+3:]}
					}
				}
				if layout[i:i+3] == "MST" {
					return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdTZ, suffix: layout[i+3:]}
				}
			}
		case '0': // 01, 02, 03, 04, 05, 06, 002
			if len(layout) >= i+2 && '1' <= layout[i+1] && layout[i+1] <= '6' {
				return __ptimeChunk{prefix: layout[0:i], code: __ptimeStd0x[layout[i+1]-'1'], suffix: layout[i+2:]}
			}
			if len(layout) >= i+3 && layout[i+1] == '0' && layout[i+2] == '2' {
				return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdZeroYearDay, suffix: layout[i+3:]}
			}
		case '1': // 15, 1
			if len(layout) >= i+2 && layout[i+1] == '5' {
				return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdHour, suffix: layout[i+2:]}
			}
			return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdNumMonth, suffix: layout[i+1:]}
		case '2': // 2006, 2
			if len(layout) >= i+4 && layout[i:i+4] == "2006" {
				return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdLongYear, suffix: layout[i+4:]}
			}
			return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdDay, suffix: layout[i+1:]}
		case '_': // _2, _2006, __2
			if len(layout) >= i+2 && layout[i+1] == '2' {
				// _2006 is really a literal _, followed by stdLongYear.
				if len(layout) >= i+5 && layout[i+1:i+5] == "2006" {
					return __ptimeChunk{prefix: layout[0 : i+1], code: __ptimeStdLongYear, suffix: layout[i+5:]}
				}
				return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdUnderDay, suffix: layout[i+2:]}
			}
			if len(layout) >= i+3 && layout[i+1] == '_' && layout[i+2] == '2' {
				return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdUnderYearDay, suffix: layout[i+3:]}
			}
		case '3':
			return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdHour12, suffix: layout[i+1:]}
		case '4':
			return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdMinute, suffix: layout[i+1:]}
		case '5':
			return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdSecond, suffix: layout[i+1:]}
		case 'P': // PM
			if len(layout) >= i+2 && layout[i+1] == 'M' {
				return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdPM, suffix: layout[i+2:]}
			}
		case 'p': // pm
			if len(layout) >= i+2 && layout[i+1] == 'm' {
				return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdpm, suffix: layout[i+2:]}
			}
		case '-': // -070000, -07:00:00, -0700, -07:00, -07
			if len(layout) >= i+7 && layout[i:i+7] == "-070000" {
				return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdNumSecondsTZ, suffix: layout[i+7:]}
			}
			if len(layout) >= i+9 && layout[i:i+9] == "-07:00:00" {
				return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdNumColonSecondsTZ, suffix: layout[i+9:]}
			}
			if len(layout) >= i+5 && layout[i:i+5] == "-0700" {
				return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdNumTZ, suffix: layout[i+5:]}
			}
			if len(layout) >= i+6 && layout[i:i+6] == "-07:00" {
				return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdNumColonTZ, suffix: layout[i+6:]}
			}
			if len(layout) >= i+3 && layout[i:i+3] == "-07" {
				return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdNumShortTZ, suffix: layout[i+3:]}
			}
		case 'Z': // Z070000, Z07:00:00, Z0700, Z07:00, Z07
			if len(layout) >= i+7 && layout[i:i+7] == "Z070000" {
				return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdISO8601SecondsTZ, suffix: layout[i+7:]}
			}
			if len(layout) >= i+9 && layout[i:i+9] == "Z07:00:00" {
				return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdISO8601ColonSecondsTZ, suffix: layout[i+9:]}
			}
			if len(layout) >= i+5 && layout[i:i+5] == "Z0700" {
				return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdISO8601TZ, suffix: layout[i+5:]}
			}
			if len(layout) >= i+6 && layout[i:i+6] == "Z07:00" {
				return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdISO8601ColonTZ, suffix: layout[i+6:]}
			}
			if len(layout) >= i+3 && layout[i:i+3] == "Z07" {
				return __ptimeChunk{prefix: layout[0:i], code: __ptimeStdISO8601ShortTZ, suffix: layout[i+3:]}
			}
		case '.', ',': // ,000 / .000 / ,999 / .999 — fractional seconds
			if i+1 < len(layout) && (layout[i+1] == '0' || layout[i+1] == '9') {
				ch := layout[i+1]
				j := i + 1
				for j < len(layout) && layout[j] == ch {
					j++
				}
				// The digit run must end here — only a fractional
				// second is all digits.
				if !__ptimeIsDigit(layout, j) {
					code := __ptimeStdFracSecond0
					if ch == '9' {
						code = __ptimeStdFracSecond9
					}
					return __ptimeChunk{prefix: layout[0:i], code: code,
						fracDigits: j - (i + 1), fracComma: c == ',', suffix: layout[j:]}
				}
			}
		}
	}
	return __ptimeChunk{prefix: layout, code: __ptimeStdNone, suffix: ""}
}

var __ptimeShortMonths = []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
var __ptimeLongMonths = []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
var __ptimeShortDays = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
var __ptimeLongDays = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

// __ptimeMatch is gc's case-insensitive ASCII match for month/weekday names.
func __ptimeMatch(s1, s2 string) bool {
	for i := 0; i < len(s1); i++ {
		c1 := s1[i]
		c2 := s2[i]
		if c1 != c2 {
			// Switch to lower-case; 'a'-'A' is known to be a single bit.
			c1 |= 'a' - 'A'
			c2 |= 'a' - 'A'
			if c1 != c2 || c1 < 'a' || c1 > 'z' {
				return false
			}
		}
	}
	return true
}

func __ptimeLookup(tab []string, val string) (int, string, bool) {
	for i, v := range tab {
		if len(val) >= len(v) && __ptimeMatch(val[:len(v)], v) {
			return i, val[len(v):], true
		}
	}
	return -1, val, false
}

// __ptimeGetnum parses s[0:1] or s[0:2] (fixed forces s[0:2]) as a
// decimal integer (mirrors gc's getnum).
func __ptimeGetnum(s string, fixed bool) (int, string, bool) {
	if !__ptimeIsDigit(s, 0) {
		return 0, s, false
	}
	if !__ptimeIsDigit(s, 1) {
		if fixed {
			return 0, s, false
		}
		return int(s[0] - '0'), s[1:], true
	}
	return int(s[0]-'0')*10 + int(s[1]-'0'), s[2:], true
}

// __ptimeGetnum3 parses s[0:1], s[0:2], or s[0:3] (fixed forces s[0:3])
// as a decimal integer (mirrors gc's getnum3).
func __ptimeGetnum3(s string, fixed bool) (int, string, bool) {
	var n, i int
	for i < 3 && __ptimeIsDigit(s, i) {
		n = n*10 + int(s[i]-'0')
		i++
	}
	if i == 0 || (fixed && i != 3) {
		return 0, s, false
	}
	return n, s[i:], true
}

func __ptimeCutspace(s string) string {
	for len(s) > 0 && s[0] == ' ' {
		s = s[1:]
	}
	return s
}

// __ptimeSkip removes the given literal prefix from value, treating runs
// of space characters as equivalent (mirrors gc's skip).
func __ptimeSkip(value, prefix string) (string, bool) {
	for len(prefix) > 0 {
		if prefix[0] == ' ' {
			if len(value) > 0 && value[0] != ' ' {
				return value, false
			}
			prefix = __ptimeCutspace(prefix)
			value = __ptimeCutspace(value)
			continue
		}
		if len(value) == 0 || value[0] != prefix[0] {
			return value, false
		}
		prefix = prefix[1:]
		value = value[1:]
	}
	return value, true
}

// __ptimeAtoi parses an all-digit string (callers pass fixed-width digit
// slices, so no sign handling is needed).
func __ptimeAtoi(s string) (int, bool) {
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

func __ptimeCommaOrPeriod(b byte) bool {
	return b == '.' || b == ','
}

// __ptimeParseNanoseconds parses a fractional second: value[0] is '.'
// or ',', followed by nbytes digits (mirrors gc's parseNanoseconds).
// The rangeErr result is "fractional second" when the value is negative,
// which cannot happen for digit input.
func __ptimeParseNanoseconds(value string, nbytes int) (ns int, rangeErr string, ok bool) {
	if len(value) == 0 || !__ptimeCommaOrPeriod(value[0]) {
		return 0, "", false
	}
	v := value
	nb := nbytes
	if nb > 10 {
		v = value[:10]
		nb = 10
	}
	ns, ok = __ptimeAtoi(v[1:nb])
	if !ok {
		return 0, "", false
	}
	if ns < 0 {
		return 0, "fractional second", false
	}
	// Scale to nanoseconds.
	for i := 0; i < 10-nb; i++ {
		ns *= 10
	}
	return ns, "", true
}

// __ptimeNumOffset is the single-result carrier for numeric zone-offset
// parsing (multi-result calls need WS D).
type __ptimeNumOffset struct {
	offset   int // seconds east of UTC
	rest     string
	rangeErr string
	ok       bool
}

// __ptimeParseNumOffset parses a numeric zone offset per the layout
// element code (mirrors the numeric branch of gc's parse).
func __ptimeParseNumOffset(value string, code int) __ptimeNumOffset {
	var r __ptimeNumOffset
	var sign, hour, min, secs string
	switch code {
	case __ptimeStdISO8601ColonTZ, __ptimeStdNumColonTZ:
		if len(value) < 6 || value[3] != ':' {
			return r
		}
		sign, hour, min, secs, value = value[0:1], value[1:3], value[4:6], "00", value[6:]
	case __ptimeStdNumShortTZ, __ptimeStdISO8601ShortTZ:
		if len(value) < 3 {
			return r
		}
		sign, hour, min, secs, value = value[0:1], value[1:3], "00", "00", value[3:]
	case __ptimeStdISO8601ColonSecondsTZ, __ptimeStdNumColonSecondsTZ:
		if len(value) < 9 || value[3] != ':' || value[6] != ':' {
			return r
		}
		sign, hour, min, secs, value = value[0:1], value[1:3], value[4:6], value[7:9], value[9:]
	case __ptimeStdISO8601SecondsTZ, __ptimeStdNumSecondsTZ:
		if len(value) < 7 {
			return r
		}
		sign, hour, min, secs, value = value[0:1], value[1:3], value[3:5], value[5:7], value[7:]
	default: // __ptimeStdNumTZ, __ptimeStdISO8601TZ
		if len(value) < 5 {
			return r
		}
		sign, hour, min, secs, value = value[0:1], value[1:3], value[3:5], "00", value[5:]
	}
	// The range tests use > rather than >=, as some people do write
	// offsets of 24 hours or 60 minutes or 60 seconds. They apply even
	// when a field fails to parse (gc checks them unconditionally), and
	// take precedence over the syntax error.
	hr, okHr := __ptimeAtoi(hour)
	mm, okMm := __ptimeAtoi(min)
	ss, okSs := __ptimeAtoi(secs)
	if hr > 24 {
		r.rangeErr = "time zone offset hour"
	}
	if mm > 60 {
		r.rangeErr = "time zone offset minute"
	}
	if ss > 60 {
		r.rangeErr = "time zone offset second"
	}
	if !okHr || !okMm || !okSs {
		return r
	}
	off := (hr*60+mm)*60 + ss // offset is in seconds
	switch sign[0] {
	case '+':
	case '-':
		off = -off
	default:
		return r
	}
	r.offset = off
	r.rest = value
	r.ok = true
	return r
}

// __ptimeParseTimeZone parses a zone abbreviation (mirrors gc's
// parseTimeZone): 3-5 upper-case letters (5 or 4 must end in T, with the
// WITA exception), the ChST/MeST mixed-case specials, GMT with an
// optional signed offset, or a bare signed offset.
func __ptimeParseTimeZone(value string) (int, bool) {
	if len(value) < 3 {
		return 0, false
	}
	// Special case 1: ChST and MeST are the only zones with a lower-case letter.
	if len(value) >= 4 && (value[:4] == "ChST" || value[:4] == "MeST") {
		return 4, true
	}
	// Special case 2: GMT may have an hour offset; treat it specially.
	if value[:3] == "GMT" {
		return __ptimeParseGMT(value), true
	}
	// Special case 3: some zones are not named, but have +/-00 format.
	if value[0] == '+' || value[0] == '-' {
		n := __ptimeParseSignedOffset(value)
		return n, n > 0
	}
	// How many upper-case letters are there? Need at least three, at most five.
	nUpper := 0
	for nUpper < 6 && nUpper < len(value) {
		if c := value[nUpper]; c < 'A' || 'Z' < c {
			break
		}
		nUpper++
	}
	switch nUpper {
	case 0, 1, 2, 6:
		return 0, false
	case 5: // Must end in T to match.
		if value[4] == 'T' {
			return 5, true
		}
	case 4:
		// Must end in T, except one special case.
		if value[3] == 'T' || value[:4] == "WITA" {
			return 4, true
		}
	case 3:
		return 3, true
	}
	return 0, false
}

// __ptimeParseGMT parses a GMT zone with optional hour offset.
func __ptimeParseGMT(value string) int {
	v := value[3:]
	if len(v) == 0 {
		return 3
	}
	return 3 + __ptimeParseSignedOffset(v)
}

// __ptimeParseSignedOffset parses a signed offset like "+03" or "-04"
// (mirrors gc's parseSignedOffset).
func __ptimeParseSignedOffset(value string) int {
	if len(value) == 0 || (value[0] != '-' && value[0] != '+') {
		return 0
	}
	i := 1
	x := 0
	ndig := 0
	for i < len(value) && '0' <= value[i] && value[i] <= '9' {
		if ndig < 18 {
			x = x*10 + int(value[i]-'0')
		}
		ndig++
		i++
	}
	// Fail if nothing consumed by the digits, or the value exceeds the
	// -23..+23 range (any 19+-digit number does).
	if ndig == 0 || ndig > 18 || x > 23 {
		return 0
	}
	return i
}

// __ptimeRuneLen returns the UTF-8 encoding length of the rune at s[i],
// or 1 if s[i] does not start a valid encoding (mirroring Go's range,
// which yields (RuneError, 1) for invalid bytes).
func __ptimeRuneLen(s string, i int) int {
	c := s[i]
	if c < 0x80 {
		return 1
	}
	var width int
	var lo, hi byte
	switch {
	case c >= 0xC2 && c <= 0xDF:
		width = 2
		lo, hi = 0x80, 0xBF
	case c >= 0xE0 && c <= 0xEF:
		width = 3
		lo, hi = 0x80, 0xBF
		if c == 0xE0 {
			lo = 0xA0 // reject overlongs
		}
		if c == 0xED {
			hi = 0x9F // reject surrogates
		}
	case c >= 0xF0 && c <= 0xF4:
		width = 4
		lo, hi = 0x80, 0xBF
		if c == 0xF0 {
			lo = 0x90 // reject overlongs
		}
		if c == 0xF4 {
			hi = 0x8F // reject > U+10FFFF
		}
	default:
		return 1
	}
	if i+width > len(s) {
		return 1
	}
	for j := 1; j < width; j++ {
		b := s[i+j]
		l, h := byte(0x80), byte(0xBF)
		if j == 1 {
			l, h = lo, hi
		}
		if b < l || b > h {
			return 1
		}
	}
	return width
}

// __parseTimeQuote mirrors gc's time.quote: printable ASCII passes
// through ('"' and '\\' backslash-escaped); anything else is \x-escaped
// per byte of its UTF-8 encoding.
func __parseTimeQuote(s string) string {
	const lowerhex = "0123456789abcdef"
	buf := make([]byte, 1, len(s)+2) // slice will be at least len(s) + quotes
	buf[0] = '"'
	for i := 0; i < len(s); {
		c := s[i]
		if c >= 0x80 || c < ' ' {
			w := __ptimeRuneLen(s, i)
			for j := 0; j < w; j++ {
				buf = append(buf, '\\', 'x')
				buf = append(buf, lowerhex[s[i+j]>>4], lowerhex[s[i+j]&0xF])
			}
			i += w
			continue
		}
		if c == '"' || c == '\\' {
			buf = append(buf, '\\')
		}
		buf = append(buf, c)
		i++
	}
	buf = append(buf, '"')
	return string(buf)
}

// __ptimeIsLeap reports whether year is a leap year (proleptic Gregorian).
func __ptimeIsLeap(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

// __ptimeDaysBeforeTable[m-1] counts the days in a non-leap year before
// month m begins (m = 13 holds the year length).
var __ptimeDaysBeforeTable = [...]int{0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334, 365}

func __ptimeDaysBefore(m int) int {
	return __ptimeDaysBeforeTable[m-1]
}

func __ptimeDaysIn(month, year int) int {
	if month == 2 && __ptimeIsLeap(year) {
		return 29
	}
	return __ptimeDaysBefore(month+1) - __ptimeDaysBefore(month)
}

// timePair is the Appendix M single-result carrier for Parse.
type timePair struct {
	t   Time
	err error
}

// __parseTime is the real time.Parse implementation behind the M7
// desugar table entry: exact gc 1.27.1 parse semantics, clean-room
// implementation. Zone handling is instant-exact: the synthetic model is
// UTC-only (ADR-0005), so a parsed zone offset is applied to the instant
// (as gc does internally) and zone abbreviations collapse to their UTC
// wall time — the instant always matches gc; only the rendered zone
// label differs (UTC), consistent with the existing Format behavior.
func __parseTime(layout, value string) timePair {
	fail := func(layoutElem, valueElem, message string, alayout, avalue string) timePair {
		return timePair{err: &ParseError{Layout: alayout, Value: avalue,
			LayoutElem: layoutElem, ValueElem: valueElem, Message: message}}
	}
	alayout, avalue := layout, value
	rangeErr := "" // set if a value is out of range
	amSet := false // do we need to subtract 12 from the hour for midnight?
	pmSet := false // do we need to add 12 to the hour?

	// Time being constructed.
	year := 0
	month := -1
	day := -1
	yday := -1
	hour := 0
	min := 0
	sec := 0
	nsec := 0
	var z *Location
	zoneOffset := -1
	zoneName := ""

	ok := true
	// Each iteration processes one layout element.
	for {
		chunk := __ptimeNextChunk(layout)
		stdstr := layout[len(chunk.prefix) : len(layout)-len(chunk.suffix)]
		value, ok = __ptimeSkip(value, chunk.prefix)
		if !ok {
			return fail(chunk.prefix, value, "", alayout, avalue)
		}
		if chunk.code == __ptimeStdNone {
			if len(value) != 0 {
				return fail("", value, ": extra text: "+__parseTimeQuote(value), alayout, avalue)
			}
			break
		}
		layout = chunk.suffix
		hold := value
		switch chunk.code {
		case __ptimeStdYear:
			if len(value) < 2 {
				ok = false
				break
			}
			var y int
			y, ok = __ptimeAtoi(value[0:2])
			value = value[2:]
			if !ok {
				break
			}
			if y >= 69 { // Unix time starts Dec 31 1969 in some time zones
				y += 1900
			} else {
				y += 2000
			}
			year = y
		case __ptimeStdLongYear:
			if len(value) < 4 || !__ptimeIsDigit(value, 0) {
				ok = false
				break
			}
			var y int
			y, ok = __ptimeAtoi(value[0:4])
			value = value[4:]
			if !ok {
				break
			}
			year = y
		case __ptimeStdMonth:
			var m int
			var rest string
			m, rest, ok = __ptimeLookup(__ptimeShortMonths, value)
			if !ok {
				break
			}
			month = m + 1
			value = rest
		case __ptimeStdLongMonth:
			var m int
			var rest string
			m, rest, ok = __ptimeLookup(__ptimeLongMonths, value)
			if !ok {
				break
			}
			month = m + 1
			value = rest
		case __ptimeStdNumMonth, __ptimeStdZeroMonth:
			var m int
			var rest string
			m, rest, ok = __ptimeGetnum(value, chunk.code == __ptimeStdZeroMonth)
			if !ok {
				break
			}
			month = m
			value = rest
			if month <= 0 || 12 < month {
				rangeErr = "month"
			}
		case __ptimeStdWeekDay:
			// Weekday is checked for syntax but otherwise ignored.
			var rest string
			_, rest, ok = __ptimeLookup(__ptimeShortDays, value)
			value = rest
		case __ptimeStdLongWeekDay:
			var rest string
			_, rest, ok = __ptimeLookup(__ptimeLongDays, value)
			value = rest
		case __ptimeStdDay, __ptimeStdUnderDay, __ptimeStdZeroDay:
			if chunk.code == __ptimeStdUnderDay && len(value) > 0 && value[0] == ' ' {
				value = value[1:]
			}
			var d int
			var rest string
			d, rest, ok = __ptimeGetnum(value, chunk.code == __ptimeStdZeroDay)
			if !ok {
				break
			}
			day = d
			value = rest
			// Note that we allow any one- or two-digit day here.
			// The month, day, year combination is validated after parsing.
		case __ptimeStdUnderYearDay, __ptimeStdZeroYearDay:
			for i := 0; i < 2; i++ {
				if chunk.code == __ptimeStdUnderYearDay && len(value) > 0 && value[0] == ' ' {
					value = value[1:]
				}
			}
			var yd int
			var rest string
			yd, rest, ok = __ptimeGetnum3(value, chunk.code == __ptimeStdZeroYearDay)
			if !ok {
				break
			}
			yday = yd
			value = rest
			// Note that we allow any one-, two-, or three-digit year-day here.
			// The year-day, year combination is validated after parsing.
		case __ptimeStdHour:
			var h int
			var rest string
			h, rest, ok = __ptimeGetnum(value, false)
			if !ok {
				break
			}
			hour = h
			value = rest
			if hour < 0 || 24 <= hour {
				rangeErr = "hour"
			}
		case __ptimeStdHour12, __ptimeStdZeroHour12:
			var h int
			var rest string
			h, rest, ok = __ptimeGetnum(value, chunk.code == __ptimeStdZeroHour12)
			if !ok {
				break
			}
			hour = h
			value = rest
			if hour < 0 || 12 < hour {
				rangeErr = "hour"
			}
		case __ptimeStdMinute, __ptimeStdZeroMinute:
			var m int
			var rest string
			m, rest, ok = __ptimeGetnum(value, chunk.code == __ptimeStdZeroMinute)
			if !ok {
				break
			}
			min = m
			value = rest
			if min < 0 || 60 <= min {
				rangeErr = "minute"
			}
		case __ptimeStdSecond, __ptimeStdZeroSecond:
			var s int
			var rest string
			s, rest, ok = __ptimeGetnum(value, chunk.code == __ptimeStdZeroSecond)
			if !ok {
				break
			}
			sec = s
			value = rest
			if sec < 0 || 60 <= sec {
				rangeErr = "second"
				break
			}
			// Special case: do we have a fractional second but no
			// fractional second in the format?
			if len(value) >= 2 && __ptimeCommaOrPeriod(value[0]) && __ptimeIsDigit(value, 1) {
				peek := __ptimeNextChunk(layout)
				if peek.code == __ptimeStdFracSecond0 || peek.code == __ptimeStdFracSecond9 {
					// Fractional second in the layout; proceed normally.
					break
				}
				// No fractional second in the layout but we have one in the input.
				n := 2
				for n < len(value) && __ptimeIsDigit(value, n) {
					n++
				}
				nsec, rangeErr, ok = __ptimeParseNanoseconds(value, n)
				value = value[n:]
			}
		case __ptimeStdPM:
			if len(value) < 2 {
				ok = false
				break
			}
			p := value[0:2]
			value = value[2:]
			switch p {
			case "PM":
				pmSet = true
			case "AM":
				amSet = true
			default:
				ok = false
			}
		case __ptimeStdpm:
			if len(value) < 2 {
				ok = false
				break
			}
			p := value[0:2]
			value = value[2:]
			switch p {
			case "pm":
				pmSet = true
			case "am":
				amSet = true
			default:
				ok = false
			}
		case __ptimeStdISO8601TZ, __ptimeStdISO8601SecondsTZ, __ptimeStdISO8601ShortTZ,
			__ptimeStdISO8601ColonTZ, __ptimeStdISO8601ColonSecondsTZ:
			if len(value) >= 1 && value[0] == 'Z' {
				value = value[1:]
				z = UTC
				break
			}
			r := __ptimeParseNumOffset(value, chunk.code)
			if r.rangeErr != "" {
				rangeErr = r.rangeErr
			}
			if !r.ok {
				ok = false
				break
			}
			zoneOffset = r.offset
			value = r.rest
		case __ptimeStdNumTZ, __ptimeStdNumSecondsTZ, __ptimeStdNumShortTZ,
			__ptimeStdNumColonTZ, __ptimeStdNumColonSecondsTZ:
			r := __ptimeParseNumOffset(value, chunk.code)
			if r.rangeErr != "" {
				rangeErr = r.rangeErr
			}
			if !r.ok {
				ok = false
				break
			}
			zoneOffset = r.offset
			value = r.rest
		case __ptimeStdTZ:
			// Does it look like a time zone?
			if len(value) >= 3 && value[0:3] == "UTC" {
				z = UTC
				value = value[3:]
				break
			}
			n, ok2 := __ptimeParseTimeZone(value)
			if !ok2 {
				ok = false
				break
			}
			zoneName, value = value[:n], value[n:]
		case __ptimeStdFracSecond0:
			// stdFracSecond0 requires the exact number of digits as
			// specified in the layout.
			ndigit := 1 + chunk.fracDigits
			if len(value) < ndigit {
				ok = false
				break
			}
			nsec, rangeErr, ok = __ptimeParseNanoseconds(value, ndigit)
			value = value[ndigit:]
		case __ptimeStdFracSecond9:
			if len(value) < 2 || !__ptimeCommaOrPeriod(value[0]) || value[1] < '0' || '9' < value[1] {
				// Fractional second omitted.
				break
			}
			// Take any number of digits, even more than asked for,
			// because it is what the stdSecond case would do.
			i := 0
			for i+1 < len(value) && '0' <= value[i+1] && value[i+1] <= '9' {
				i++
			}
			nsec, rangeErr, ok = __ptimeParseNanoseconds(value, 1+i)
			value = value[1+i:]
		}
		if rangeErr != "" {
			return fail(stdstr, value, ": "+rangeErr+" out of range", alayout, avalue)
		}
		if !ok {
			return fail(stdstr, hold, "", alayout, avalue)
		}
	}
	if pmSet && hour < 12 {
		hour += 12
	} else if amSet && hour == 12 {
		hour = 0
	}

	// Convert yday to day, month.
	if yday >= 0 {
		var d int
		var m int
		if __ptimeIsLeap(year) {
			if yday == 31+29 {
				m = 2
				d = 29
			} else if yday > 31+29 {
				yday--
			}
		}
		if yday < 1 || yday > 365 {
			return fail("", value, ": day-of-year out of range", alayout, avalue)
		}
		if m == 0 {
			m = (yday-1)/31 + 1
			if __ptimeDaysBefore(m+1) < yday {
				m++
			}
			d = yday - __ptimeDaysBefore(m)
		}
		// If month, day already seen, yday's m, d must match.
		// Otherwise, set them from m, d.
		if month >= 0 && month != m {
			return fail("", value, ": day-of-year does not match month", alayout, avalue)
		}
		month = m
		if day >= 0 && day != d {
			return fail("", value, ": day-of-year does not match day", alayout, avalue)
		}
		day = d
	} else {
		if month < 0 {
			month = 1
		}
		if day < 0 {
			day = 1
		}
	}

	// Validate the day of the month.
	if day < 1 || day > __ptimeDaysIn(month, year) {
		return fail("", value, ": day out of range", alayout, avalue)
	}

	if z != nil {
		return timePair{t: Date(year, Month(month), day, hour, min, sec, nsec, UTC)}
	}

	if zoneOffset != -1 {
		t := Date(year, Month(month), day, hour, min, sec, nsec, UTC)
		return timePair{t: t.Add(-Duration(zoneOffset) * Second)}
	}

	if zoneName != "" {
		// gc records a fabricated zone with the abbreviation (zero
		// offset, or the GMT hour offset) — the instant is the UTC wall
		// time either way, which is what the UTC-only model keeps.
		return timePair{t: Date(year, Month(month), day, hour, min, sec, nsec, UTC)}
	}

	// Otherwise, fall back to UTC (gc's defaultLocation for Parse).
	return timePair{t: Date(year, Month(month), day, hour, min, sec, nsec, UTC)}
}

// Parse keeps the exact gc 1.27.1 signature for type-checking. The real
// implementation is __parseTime: 2-target call sites desugar via the M7
// table (m7MultiResultFuncs); every other position fails fast. The body
// never runs.
func Parse(layout, value string) (Time, error) {
	panic("time.Parse: not in M5 scope (multi-result calls need WS D; re-test when it lands)")
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
	days := __timeDaysFromCivil(y64, m0+1, day64) // days since 1970-01-01
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
func Now() Time                    { panic("time: intrinsic Now") }
func Sleep(d Duration)             { panic("time: intrinsic Sleep") }
func After(d Duration) <-chan Time { panic("time: intrinsic After") }
func Tick(d Duration) <-chan Time  { panic("time: intrinsic Tick") }
func NewTimer(d Duration) *Timer   { panic("time: intrinsic NewTimer") }
func NewTicker(d Duration) *Ticker { panic("time: intrinsic NewTicker") }
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
	// cause is the construction cause for WithDeadlineCause/
	// WithTimeoutCause (nil for the plain variants): the manual cancel
	// and the deadline-fire cancel both report it (gc parity).
	cause error
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

// cancelCauseFunc is the Appendix M desugar target for the
// WithDeadlineCause/WithTimeoutCause cancel function (stops the timer
// too; the manual-cancel cause is the construction cause).
func (c *timerCtx) cancelCauseFunc() CancelFunc {
	return func() { c.cancelTimer(Canceled, c.cause) }
}

// __withDeadline/__withTimeout/__withDeadlineCause/__withTimeoutCause are
// the Appendix M desugar targets.
func __withDeadline(parent Context, d time.Time) *timerCtx {
	return __withDeadlineCause(parent, d, nil)
}

// __withDeadlineCause implements WithDeadlineCause (and, via
// __withDeadline, the plain WithDeadline): the construction cause is
// remembered on the timerCtx and used for both the manual cancel and
// the deadline-fire cancel (gc parity: gc's timer fires with the
// construction cause). A nil cause keeps the historical
// (DeadlineExceeded, DeadlineExceeded) behavior of the plain variants.
func __withDeadlineCause(parent Context, d time.Time, cause error) *timerCtx {
	if parent == nil {
		panic("cannot create context from nil parent")
	}
	c := &timerCtx{cancel: newCancelCtx(parent), deadline: d, cause: cause}
	ctxPropagateCancel(parent, c.cancel)
	fireCause := cause
	if fireCause == nil {
		fireCause = DeadlineExceeded
	}
	until := time.Until(d)
	if until <= 0 {
		c.cancelTimer(DeadlineExceeded, fireCause)
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
			c.cancelTimer(DeadlineExceeded, fireCause)
		})
	}
	return c
}

func __withTimeoutCause(parent Context, timeout time.Duration, cause error) *timerCtx {
	return __withDeadlineCause(parent, time.Now().Add(timeout), cause)
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
	panic("context.WithDeadlineCause: not in M5 scope (multi-result calls need WS D; re-test when it lands)")
}

func WithTimeoutCause(parent Context, timeout time.Duration, cause error) (Context, CancelFunc) {
	panic("context.WithTimeoutCause: not in M5 scope (multi-result calls need WS D; re-test when it lands)")
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
