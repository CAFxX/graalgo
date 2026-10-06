// P3-M5 (WS E, ADR-0005): synthetic `time` and `context` packages.
//
// The two packages below are REAL Go source, embedded as text and
// type-checked on demand with the overlay importer (so `context`'s
// `import ("sync"; "time")` resolves to the synthetic packages —
// synthetic→synthetic imports). The resulting *types.Package instances
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
//     targets behind the 6 multi-result operations (which keep exact gc
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
// unixnano == 0 (IsZero). Monotonic readings are out of M5 scope.
type Time struct {
	unixnano int64
}

func (t Time) IsZero() bool { return t.unixnano == 0 }

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
	return Time{unixnano: t.unixnano + int64(d)}
}

func (t Time) Sub(u Time) Duration {
	return Duration(t.unixnano - u.unixnano)
}

func (t Time) Unix() int64     { return t.unixnano / 1e9 }
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
			return Time{unixnano: n - r}
		}
		return Time{unixnano: n - dd - r}
	}
	if r+r < dd {
		return Time{unixnano: n - r}
	}
	return Time{unixnano: n + dd - r}
}

func (t Time) Truncate(d Duration) Time {
	if d <= 0 {
		return t
	}
	dd := int64(d)
	return Time{unixnano: t.unixnano - t.unixnano%dd}
}

// Out-of-M5-scope members: exact gc signatures, fail fast.
func (t Time) String() string { panic("time.Time.String: not in M5 scope") }
func (t Time) Format(layout string) string {
	panic("time.Time.Format: not in M5 scope")
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

func Parse(layout, value string) (Time, error) {
	panic("time.Parse: not in M5 scope")
}

func Date(year int, month Month, day, hour, min, sec, nsec int, loc *Location) Time {
	panic("time.Date: not in M5 scope")
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
// (ADR-0005 D2): REAL Go composing the synthetic sync.Mutex, channel
// receive, synthetic time, and P3-M3 interface dispatch (a combination
// test of M2+M3+M5). Exact gc 1.27.1 signatures for the in-scope API.
//
// Two deliberate simplifications vs the stdlib (documented in
// TIME_CONTEXT_DESIGN.md):
//   - No embedding: timerCtx/valueCtx/withoutCancelCtx delegate
//     explicitly (the exporter resolves promoted methods, but explicit
//     delegation keeps the method tables obvious).
//   - Parent cancellation propagates via a children registry on
//     *cancelCtx (no goroutine per child); exotic (user-defined) parent
//     Contexts fall back to a watcher goroutine, which is reaped when
//     the parent fires.
//   - emptyCtx.Done() returns a package-level never-closed channel, not
//     nil: the runtime's channel receive throws on nil channels (spike),
//     so nil would not block forever (ADR-0005 fallback).
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
//   - The 6 multi-result operations keep exact gc signatures with
//     fail-fast bodies (for type-checking); the exporter desugars the
//     2-target call sites to the __withCancel/__withCancelCause/
//     __withDeadline/__withTimeout/__deadlineOf single-result helpers
//     plus cancelFunc/cancelCauseFunc methods (Appendix M). The concrete
//     Deadline() methods are declared for go/types interface
//     satisfaction only — every call site desugars, so their fail-fast
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
		// Won the claim: publish err/cause, broadcast, then
		// propagate without holding the semaphore.
		c.err = err
		c.cause = cause
		close(c.done)
		<-c.muChan
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
		panic("context: nil parent")
	}
	c := newCancelCtx(parent)
	ctxPropagateCancel(parent, c)
	return c
}

func __withCancelCause(parent Context) *cancelCtx {
	if parent == nil {
		panic("context: nil parent")
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
		panic("context: nil parent")
	}
	c := &timerCtx{cancel: newCancelCtx(parent), deadline: d}
	ctxPropagateCancel(parent, c.cancel)
	if time.Until(d) <= 0 {
		c.cancelTimer(DeadlineExceeded, DeadlineExceeded)
	} else {
		c.timer = time.AfterFunc(time.Until(d), func() {
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
	p := __deadlineOf(c.parent)
	return p.d, p.ok
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
		panic("context: nil parent")
	}
	if key == nil {
		panic("context: nil key")
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
		// parked watcher goroutine. Documented M5 limitation.
		return func() bool { return false }
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
		panic("context: nil parent")
	}
	if parent.Done() == nil {
		return parent
	}
	return &withoutCancelCtx{parent: parent}
}
`
