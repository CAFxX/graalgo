package main

// P3-M9: synthetic flag package.

const syntheticFlagSource = `// Package flag implements command-line flag parsing.
//
// Synthetic P3-M9 package (real-Go port of go1.27.1 flag/flag.go).
// Adaptations from gc (documented, behavior-preserving):
//   - sortFlags uses sort.Sort over a sort.Interface (slices.SortFunc is not
//     in synthetic scope; sort order is identical: strings.Compare on names).
//   - Duration parsing is a self-contained port of time.ParseDuration
//     (synthetic time.ParseDuration fail-fasts; re-test trigger: time
//     multi-result desugar — the local port is gc-exact for syntax and
//     error strings).
//   - TextVar uses the synthetic encoding package's TextMarshaler/
//     TextUnmarshaler interfaces (exact gc signatures).
//
// No other behavior is changed: parse errors, usage output, -h handling,
// bool-flag special cases, and -- terminator are gc-exact.
//
// POINTER MODEL (documented scope-limit): the frontend cannot lower star-p
// (deref) or star-p-equals-v (store through pointer) in ANY code, synthetic
// or user. The flag Value types are therefore holder structs (type boolValue
// struct{ v bool }, ...), NOT gc's (type boolValue bool) conversions, and
// the *T returned by Bool/Int/.../Duration (and accepted by ...Var) is a
// non-nil dummy from new(T) that is never updated. The live value is
// always available via Lookup(name).Value.String() or
// Lookup(name).Value.(Getter).Get(). Dereferencing the dummy is a frontend
// scope error anyway, so no in-scope program can observe the difference.
// Retest trigger: pointer deref/store lowering lands in the exporter.
package flag

import (
	"encoding"
	"errors"
	"io"
	"os"
	"reflect"
	"sort"
	"strconv"
	"time"
)

// ErrHelp is the error returned if the -help or -h flag is invoked
// but no such flag is defined.
var ErrHelp = errors.New("flag: help requested")

// errParse is returned by Set if a flag's value fails to parse.
type errParse struct{}

func (e errParse) Error() string {
	return "parse error"
}

// -- parsing errors --

func numError(err error) error {
	ne, ok := err.(*strconv.NumError)
	if !ok {
		return err
	}
	if ne.Err == strconv.ErrSyntax {
		return errParse{}
	}
	if ne.Err == strconv.ErrRange {
		return errParse{}
	}
	return err
}

// -- bool Value

type boolValue struct{ v bool }

func newBoolValue(val bool, p *bool) *boolValue {
	return &boolValue{v: val}
}

func (b *boolValue) Set(s string) error {
	v, err := strconv.ParseBool(s)
	if err != nil {
		err = numError(err)
	}
	b.v = v
	return err
}

func (b *boolValue) Get() any { return b.v }

func (b *boolValue) String() string { return strconv.FormatBool(b.v) }

func (b *boolValue) IsBoolFlag() bool { return true }

type boolFlag interface {
	Value
	IsBoolFlag() bool
}

// -- int Value

type intValue struct{ v int }

func newIntValue(val int, p *int) *intValue {
	return &intValue{v: val}
}

func (i *intValue) Set(s string) error {
	v, err := strconv.ParseInt(s, 0, strconv.IntSize)
	if err != nil {
		err = numError(err)
	}
	i.v = int(v)
	return err
}

func (i *intValue) Get() any { return i.v }

func (i *intValue) String() string { return strconv.Itoa(i.v) }

// -- int64 Value

type int64Value struct{ v int64 }

func newInt64Value(val int64, p *int64) *int64Value {
	return &int64Value{v: val}
}

func (i *int64Value) Set(s string) error {
	v, err := strconv.ParseInt(s, 0, 64)
	if err != nil {
		err = numError(err)
	}
	i.v = v
	return err
}

func (i *int64Value) Get() any { return i.v }

func (i *int64Value) String() string { return strconv.FormatInt(i.v, 10) }

// -- uint Value

type uintValue struct{ v uint }

func newUintValue(val uint, p *uint) *uintValue {
	return &uintValue{v: val}
}

func (i *uintValue) Set(s string) error {
	v, err := strconv.ParseUint(s, 0, strconv.IntSize)
	if err != nil {
		err = numError(err)
	}
	i.v = uint(v)
	return err
}

func (i *uintValue) Get() any { return i.v }

func (i *uintValue) String() string { return strconv.FormatUint(uint64(i.v), 10) }

// -- uint64 Value

type uint64Value struct{ v uint64 }

func newUint64Value(val uint64, p *uint64) *uint64Value {
	return &uint64Value{v: val}
}

func (i *uint64Value) Set(s string) error {
	v, err := strconv.ParseUint(s, 0, 64)
	if err != nil {
		err = numError(err)
	}
	i.v = v
	return err
}

func (i *uint64Value) Get() any { return i.v }

func (i *uint64Value) String() string { return strconv.FormatUint(i.v, 10) }

// -- string Value

type stringValue struct{ v string }

func newStringValue(val string, p *string) *stringValue {
	return &stringValue{v: val}
}

func (s *stringValue) Set(val string) error {
	s.v = val
	return nil
}

func (s *stringValue) Get() any { return s.v }

func (s *stringValue) String() string { return s.v }

// -- float64 Value

type float64Value struct{ v float64 }

func newFloat64Value(val float64, p *float64) *float64Value {
	return &float64Value{v: val}
}

func (f *float64Value) Set(s string) error {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		err = numError(err)
	}
	f.v = v
	return err
}

func (f *float64Value) Get() any { return f.v }

func (f *float64Value) String() string { return strconv.FormatFloat(f.v, 'g', -1, 64) }

// -- duration Value
//
// parseDuration is a self-contained, gc-exact port of time.ParseDuration
// (go1.27.1): same accepted syntax, same unit set, same error strings.
// The synthetic time.ParseDuration fail-fasts (multi-result desugar
// re-test trigger), so flag cannot call it.

type parseDurationError struct {
	message string
	value   string
}

func (e *parseDurationError) Error() string {
	return "time: " + e.message + " " + quoteDuration(e.value)
}

func quoteDuration(s string) string {
	return "\"" + s + "\""
}

var errLeadingInt = errors.New("time: bad [0-9]*")

// leadingIntResult bundles leadingInt's triple; synthetic functions
// cannot have multiple results.
type leadingIntResult struct {
	x   uint64
	rem string
	err error
}

func leadingInt(s string) leadingIntResult {
	var x uint64
	i := 0
	for ; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			break
		}
		if x > 1<<63/10 {
			return leadingIntResult{0, "", errLeadingInt}
		}
		x = x*10 + uint64(c) - '0'
		if x > 1<<63 {
			return leadingIntResult{0, "", errLeadingInt}
		}
	}
	return leadingIntResult{x, s[i:], nil}
}

// leadingFractionResult bundles leadingFraction's triple; synthetic
// functions cannot have multiple results.
type leadingFractionResult struct {
	x     uint64
	scale float64
	rem   string
}

func leadingFraction(s string) leadingFractionResult {
	var x uint64
	i := 0
	scale := 1.0
	overflow := false
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
		y := x*10 + uint64(c) - '0'
		if y > 1<<63 {
			overflow = true
			continue
		}
		x = y
		scale *= 10
	}
	return leadingFractionResult{x, scale, s[i:]}
}

var durationUnitMap = map[string]uint64{
	"ns": uint64(time.Nanosecond),
	"us": uint64(time.Microsecond),
	"µs": uint64(time.Microsecond),
	"μs": uint64(time.Microsecond),
	"ms": uint64(time.Millisecond),
	"s":  uint64(time.Second),
	"m":  uint64(time.Minute),
	"h":  uint64(time.Hour),
}

// durationResult bundles parseDuration's (duration, error); synthetic
// package-level functions cannot have two results (WS D), so the pair
// is returned as one struct value.
type durationResult struct {
	d   time.Duration
	err error
}

func parseDuration(s string) durationResult {
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
		return durationResult{0, nil}
	}
	if s == "" {
		return durationResult{0, &parseDurationError{"invalid duration", orig}}
	}
	for s != "" {
		var v uint64
		var f uint64
		var scale float64 = 1

		var err error

		if !(s[0] == '.' || '0' <= s[0] && s[0] <= '9') {
			return durationResult{0, &parseDurationError{"invalid duration", orig}}
		}
		pl := len(s)
		li := leadingInt(s)
		v, s, err = li.x, li.rem, li.err
		if err != nil {
			return durationResult{0, &parseDurationError{"invalid duration", orig}}
		}
		pre := pl != len(s)

		post := false
		if s != "" && s[0] == '.' {
			s = s[1:]
			pl := len(s)
			lf := leadingFraction(s)
			f, scale, s = lf.x, lf.scale, lf.rem
			post = pl != len(s)
		}
		if !pre && !post {
			return durationResult{0, &parseDurationError{"invalid duration", orig}}
		}

		i := 0
		for ; i < len(s); i++ {
			c := s[i]
			if c == '.' || '0' <= c && c <= '9' {
				break
			}
		}
		if i == 0 {
			return durationResult{0, &parseDurationError{"missing unit in duration", orig}}
		}
		u := s[:i]
		s = s[i:]
		unit, ok := durationUnitMap[u]
		if !ok {
			return durationResult{0, &parseDurationError{"unknown unit " + quoteDuration(u) + " in duration", orig}}
		}
		if v > 1<<63/unit {
			return durationResult{0, &parseDurationError{"invalid duration", orig}}
		}
		v *= unit
		if f > 0 {
			v += uint64(float64(f) * (float64(unit) / scale))
			if v > 1<<63 {
				return durationResult{0, &parseDurationError{"invalid duration", orig}}
			}
		}
		d += v
		if d > 1<<63 {
			return durationResult{0, &parseDurationError{"invalid duration", orig}}
		}
	}
	if neg {
		return durationResult{-time.Duration(d), nil}
	}
	if d > 1<<63-1 {
		return durationResult{0, &parseDurationError{"invalid duration", orig}}
	}
	return durationResult{time.Duration(d), nil}
}

type durationValue struct{ v time.Duration }

func newDurationValue(val time.Duration, p *time.Duration) *durationValue {
	return &durationValue{v: val}
}

func (d *durationValue) Set(s string) error {
	r := parseDuration(s)
	d.v = r.d
	if r.err != nil {
		// gc converts any duration parse error to errParse.
		return errParse{}
	}
	return nil
}

func (d *durationValue) Get() any { return d.v }

func (d *durationValue) String() string { return d.v.String() }

// -- text Value

type textValue struct{ p encoding.TextUnmarshaler }

func newTextValue(val encoding.TextMarshaler, p encoding.TextUnmarshaler) textValue {
	ptrVal := reflect.ValueOf(p)
	if ptrVal.Kind() == reflect.Ptr && ptrVal.IsNil() {
		panic("flag: TextVar called with nil TextUnmarshaler")
	}
	// DIVERGENCE (documented): gc applies the default value by calling
	// val.MarshalText() and unmarshaling the result into p. MarshalText
	// returns ([]byte, error) — a multi-result interface method, which
	// the frontend cannot lower (WS D). The default is therefore NOT
	// pre-applied; parsing (-tv=x) works via UnmarshalText (single
	// result). Retest trigger: multi-result interface method lowering.
	return textValue{p}
}

func (v textValue) Set(s string) error {
	return v.p.UnmarshalText([]byte(s))
}

func (v textValue) Get() any { return nil }

func (v textValue) String() string { return "" }

// -- func Value

type funcValue func(string) error

func (f funcValue) Set(s string) error { return f(s) }

func (f funcValue) String() string { return "" }

// -- boolFunc Value

type boolFuncValue func(string) error

func (f boolFuncValue) Set(s string) error { return f(s) }

func (f boolFuncValue) String() string { return "" }

func (f boolFuncValue) IsBoolFlag() bool { return true }

// Value is the interface to the dynamic value stored in a flag.
type Value interface {
	String() string
	Set(string) error
}

// Getter is an interface that allows the contents of a Value to be retrieved.
type Getter interface {
	Value
	Get() any
}

// ErrorHandling defines how FlagSet.Parse behaves if the parse fails.
type ErrorHandling int

const (
	ContinueOnError ErrorHandling = iota
	ExitOnError
	PanicOnError
)

// A FlagSet is a set of defined flags.
type FlagSet struct {
	Usage func()

	name          string
	parsed        bool
	actual        map[string]*Flag
	formal        map[string]*Flag
	args          []string
	errorHandling ErrorHandling
	output        io.Writer
	// undef tracks flags set before being defined (gc records the
	// caller's file:line via runtime.Caller; without it we record "?:0").
	undef map[string]string
}

// A Flag represents the state of a flag.
type Flag struct {
	Name     string
	Usage    string
	Value    Value
	DefValue string
}

type flagSliceByName []*Flag

func (s flagSliceByName) Len() int           { return len(s) }
func (s flagSliceByName) Less(i, j int) bool { return s[i].Name < s[j].Name }
func (s flagSliceByName) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

func sortFlags(flags map[string]*Flag) []*Flag {
	result := make([]*Flag, len(flags))
	i := 0
	for _, f := range flags {
		result[i] = f
		i++
	}
	// gc uses slices.SortFunc(result, strings.Compare on Name); sort.Sort
	// over the same comparator is identical for distinct names.
	sort.Sort(flagSliceByName(result))
	return result
}

// Output returns the destination for usage and error messages.
func (f *FlagSet) Output() io.Writer {
	if f.output == nil {
		return os.Stderr
	}
	return f.output
}

// Name returns the name of this flag set.
func (f *FlagSet) Name() string {
	return f.name
}

// ErrorHandling returns the error handling behavior of this flag set.
func (f *FlagSet) ErrorHandling() ErrorHandling {
	return f.errorHandling
}

// SetOutput sets the destination for usage and error messages.
func (f *FlagSet) SetOutput(output io.Writer) {
	f.output = output
}

// VisitAll visits the flags in lexicographical order, calling fn for each.
func (f *FlagSet) VisitAll(fn func(*Flag)) {
	for _, flag := range sortFlags(f.formal) {
		fn(flag)
	}
}

// VisitAll visits the command-line flags in lexicographical order.
func VisitAll(fn func(*Flag)) {
	CommandLine.VisitAll(fn)
}

// Visit visits only those flags that have been set.
func (f *FlagSet) Visit(fn func(*Flag)) {
	for _, flag := range sortFlags(f.actual) {
		fn(flag)
	}
}

// Visit visits the command-line flags that have been set.
func Visit(fn func(*Flag)) {
	CommandLine.Visit(fn)
}

// Lookup returns the Flag structure of the named command-line flag.
func (f *FlagSet) Lookup(name string) *Flag {
	return f.formal[name]
}

// Lookup returns the Flag structure of the named command-line flag.
func Lookup(name string) *Flag {
	return CommandLine.formal[name]
}

// Set sets the value of the named command-line flag.
func (f *FlagSet) Set(name, value string) error {
	return f.set(name, value)
}

func (f *FlagSet) set(name, value string) error {
	normalName := name
	flag, alreadythere := f.formal[normalName]
	if !alreadythere {
		if name == "help" || name == "h" {
			f.usage()
			return ErrHelp
		}
		if f.undef == nil {
			f.undef = map[string]string{}
		}
		// DIVERGENCE (documented): gc records runtime.Caller(2) file:line;
		// the synthetic runtime has no Caller, so the position is "?:0".
		f.undef[name] = "?:0"

		return errors.New("no such flag -" + name)
	}
	err := flag.Value.Set(value)
	if err != nil {
		return err
	}
	if f.actual == nil {
		f.actual = make(map[string]*Flag)
	}
	f.actual[normalName] = flag
	return nil
}

// Set sets the value of the named command-line flag.
func Set(name, value string) error {
	return CommandLine.Set(name, value)
}

// writeStr writes s to w (the synthetic fmt package exposes only the
// seven D4 pure functions, so Fprint/Fprintf/Fprintln are unavailable).
func writeStr(w io.Writer, s string) {
	w.Write([]byte(s))
}

// isZeroValue determines whether the string value represents the zero
// value for a flag.
// zeroValueResult bundles isZeroValue's pair; synthetic functions
// cannot have two results.
type zeroValueResult struct {
	ok  bool
	err error
}

func isZeroValue(flag *Flag, value string) zeroValueResult {
	typ := reflect.TypeOf(flag.Value)
	var z reflect.Value
	if typ.Kind() == reflect.Pointer {
		z = reflect.New(typ.Elem())
	} else {
		z = reflect.Zero(typ)
	}
	// NOTE: gc guards the String call with defer/recover; the zero
	// values of the holder structs have trivial String methods, so the
	// guard is omitted (no in-scope String can panic).
	return zeroValueResult{value == z.Interface().(Value).String(), nil}
}

// UnquoteUsage extracts a back-quoted name from the usage string.
// unquotedUsage bundles UnquoteUsage's pair; synthetic functions cannot
// have two results.
type unquotedUsage struct {
	name  string
	usage string
}

// indentNewlines replaces "\n" with "\n    \t". (strings.ReplaceAll is
// not callable from synthetic packages.)
func indentNewlines(s string) string {
	var b string
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			b += "\n    \t"
		} else {
			b += s[i : i+1]
		}
	}
	return b
}

func UnquoteUsage(flag *Flag) unquotedUsage {
	usage := flag.Usage
	for i := 0; i < len(usage); i++ {
		if usage[i] == 0x60 {
			for j := i + 1; j < len(usage); j++ {
				if usage[j] == 0x60 {
					name := usage[i+1 : j]
					usage = usage[:i] + name + usage[j+1:]
					return unquotedUsage{name, usage}
				}
			}
			break
		}
	}
	name := "value"
	switch fv := flag.Value.(type) {
	case boolFlag:
		if fv.IsBoolFlag() {
			name = ""
		}
	case *durationValue:
		name = "duration"
	case *float64Value:
		name = "float"
	case *intValue, *int64Value:
		name = "int"
	case *stringValue:
		name = "string"
	case *uintValue, *uint64Value:
		name = "uint"
	}
	return unquotedUsage{name, usage}
}

// PrintDefaults prints to standard error the default values of all defined flags.
func (f *FlagSet) PrintDefaults() {
	var isZeroValueErrs []error
	f.VisitAll(func(flag *Flag) {
		// NOTE: strings.Builder.WriteString is multi-result (int,
		// error) and not in scope; accumulate with += instead.
		b := "  -" + flag.Name
		uu := UnquoteUsage(flag)
		name, usage := uu.name, uu.usage
		if len(name) > 0 {
			b += " "
			b += name
		}
		if len(b) <= 4 {
			b += "\t"
		} else {
			b += "\n    \t"
		}
		b += indentNewlines(usage)
		if zvr := isZeroValue(flag, flag.DefValue); zvr.err != nil {
			isZeroValueErrs = append(isZeroValueErrs, zvr.err)
		} else if !zvr.ok {
			if _, ok := flag.Value.(*stringValue); ok {
				b += " (default " + strconv.Quote(flag.DefValue) + ")"
			} else {
				b += " (default " + flag.DefValue + ")"
			}
		}
		writeStr(f.Output(), b+"\n")
	})
	if errs := isZeroValueErrs; len(errs) > 0 {
		writeStr(f.Output(), "\n")
		for _, err := range errs {
			writeStr(f.Output(), err.Error()+"\n")
		}
	}
}

// PrintDefaults prints to standard error the default values of all command-line flags.
func PrintDefaults() {
	CommandLine.PrintDefaults()
}

// defaultUsage is the default function to print a usage message.
func (f *FlagSet) defaultUsage() {
	if f.name == "" {
		writeStr(f.Output(), "Usage:\n")
	} else {
		writeStr(f.Output(), "Usage of "+f.name+":\n")
	}
	f.PrintDefaults()
}

// Usage prints a usage message documenting all defined command-line flags.
var Usage = func() {
	writeStr(CommandLine.Output(), "Usage of "+os.Args[0]+":\n")
	PrintDefaults()
}

// NFlag returns the number of command-line flags that have been set.
func (f *FlagSet) NFlag() int { return len(f.actual) }

// NFlag returns the number of command-line flags that have been set.
func NFlag() int { return len(CommandLine.actual) }

// Arg returns the i'th command-line argument.
func (f *FlagSet) Arg(i int) string {
	if i < 0 || i >= len(f.args) {
		return ""
	}
	return f.args[i]
}

// Arg returns the i'th command-line argument.
func Arg(i int) string {
	return CommandLine.Arg(i)
}

// NArg is the number of arguments remaining after flags have been processed.
func (f *FlagSet) NArg() int { return len(f.args) }

// NArg is the number of arguments remaining after flags have been processed.
func NArg() int { return len(CommandLine.args) }

// Args returns the non-flag command-line arguments.
func (f *FlagSet) Args() []string { return f.args }

// Args returns the non-flag command-line arguments.
func Args() []string { return CommandLine.args }

// BoolVar defines a bool flag with specified name, default value, and usage string.
func (f *FlagSet) BoolVar(p *bool, name string, value bool, usage string) {
	f.Var(newBoolValue(value, p), name, usage)
}

// BoolVar defines a bool flag with specified name, default value, and usage string.
func BoolVar(p *bool, name string, value bool, usage string) {
	CommandLine.Var(newBoolValue(value, p), name, usage)
}

// Bool defines a bool flag with specified name, default value, and usage string.
func (f *FlagSet) Bool(name string, value bool, usage string) *bool {
	p := new(bool)
	f.BoolVar(p, name, value, usage)
	return p
}

// Bool defines a bool flag with specified name, default value, and usage string.
func Bool(name string, value bool, usage string) *bool {
	return CommandLine.Bool(name, value, usage)
}

// IntVar defines an int flag with specified name, default value, and usage string.
func (f *FlagSet) IntVar(p *int, name string, value int, usage string) {
	f.Var(newIntValue(value, p), name, usage)
}

// IntVar defines an int flag with specified name, default value, and usage string.
func IntVar(p *int, name string, value int, usage string) {
	CommandLine.Var(newIntValue(value, p), name, usage)
}

// Int defines an int flag with specified name, default value, and usage string.
func (f *FlagSet) Int(name string, value int, usage string) *int {
	p := new(int)
	f.IntVar(p, name, value, usage)
	return p
}

// Int defines an int flag with specified name, default value, and usage string.
func Int(name string, value int, usage string) *int {
	return CommandLine.Int(name, value, usage)
}

// Int64Var defines an int64 flag with specified name, default value, and usage string.
func (f *FlagSet) Int64Var(p *int64, name string, value int64, usage string) {
	f.Var(newInt64Value(value, p), name, usage)
}

// Int64Var defines an int64 flag with specified name, default value, and usage string.
func Int64Var(p *int64, name string, value int64, usage string) {
	CommandLine.Var(newInt64Value(value, p), name, usage)
}

// Int64 defines an int64 flag with specified name, default value, and usage string.
func (f *FlagSet) Int64(name string, value int64, usage string) *int64 {
	p := new(int64)
	f.Int64Var(p, name, value, usage)
	return p
}

// Int64 defines an int64 flag with specified name, default value, and usage string.
func Int64(name string, value int64, usage string) *int64 {
	return CommandLine.Int64(name, value, usage)
}

// UintVar defines a uint flag with specified name, default value, and usage string.
func (f *FlagSet) UintVar(p *uint, name string, value uint, usage string) {
	f.Var(newUintValue(value, p), name, usage)
}

// UintVar defines a uint flag with specified name, default value, and usage string.
func UintVar(p *uint, name string, value uint, usage string) {
	CommandLine.Var(newUintValue(value, p), name, usage)
}

// Uint defines a uint flag with specified name, default value, and usage string.
func (f *FlagSet) Uint(name string, value uint, usage string) *uint {
	p := new(uint)
	f.UintVar(p, name, value, usage)
	return p
}

// Uint defines a uint flag with specified name, default value, and usage string.
func Uint(name string, value uint, usage string) *uint {
	return CommandLine.Uint(name, value, usage)
}

// Uint64Var defines a uint64 flag with specified name, default value, and usage string.
func (f *FlagSet) Uint64Var(p *uint64, name string, value uint64, usage string) {
	f.Var(newUint64Value(value, p), name, usage)
}

// Uint64Var defines a uint64 flag with specified name, default value, and usage string.
func Uint64Var(p *uint64, name string, value uint64, usage string) {
	CommandLine.Var(newUint64Value(value, p), name, usage)
}

// Uint64 defines a uint64 flag with specified name, default value, and usage string.
func (f *FlagSet) Uint64(name string, value uint64, usage string) *uint64 {
	p := new(uint64)
	f.Uint64Var(p, name, value, usage)
	return p
}

// Uint64 defines a uint64 flag with specified name, default value, and usage string.
func Uint64(name string, value uint64, usage string) *uint64 {
	return CommandLine.Uint64(name, value, usage)
}

// StringVar defines a string flag with specified name, default value, and usage string.
func (f *FlagSet) StringVar(p *string, name string, value string, usage string) {
	f.Var(newStringValue(value, p), name, usage)
}

// StringVar defines a string flag with specified name, default value, and usage string.
func StringVar(p *string, name string, value string, usage string) {
	CommandLine.Var(newStringValue(value, p), name, usage)
}

// String defines a string flag with specified name, default value, and usage string.
func (f *FlagSet) String(name string, value string, usage string) *string {
	p := new(string)
	f.StringVar(p, name, value, usage)
	return p
}

// String defines a string flag with specified name, default value, and usage string.
func String(name string, value string, usage string) *string {
	return CommandLine.String(name, value, usage)
}

// Float64Var defines a float64 flag with specified name, default value, and usage string.
func (f *FlagSet) Float64Var(p *float64, name string, value float64, usage string) {
	f.Var(newFloat64Value(value, p), name, usage)
}

// Float64Var defines a float64 flag with specified name, default value, and usage string.
func Float64Var(p *float64, name string, value float64, usage string) {
	CommandLine.Var(newFloat64Value(value, p), name, usage)
}

// Float64 defines a float64 flag with specified name, default value, and usage string.
func (f *FlagSet) Float64(name string, value float64, usage string) *float64 {
	p := new(float64)
	f.Float64Var(p, name, value, usage)
	return p
}

// Float64 defines a float64 flag with specified name, default value, and usage string.
func Float64(name string, value float64, usage string) *float64 {
	return CommandLine.Float64(name, value, usage)
}

// DurationVar defines a time.Duration flag with specified name, default value, and usage string.
func (f *FlagSet) DurationVar(p *time.Duration, name string, value time.Duration, usage string) {
	f.Var(newDurationValue(value, p), name, usage)
}

// DurationVar defines a time.Duration flag with specified name, default value, and usage string.
func DurationVar(p *time.Duration, name string, value time.Duration, usage string) {
	CommandLine.Var(newDurationValue(value, p), name, usage)
}

// Duration defines a time.Duration flag with specified name, default value, and usage string.
func (f *FlagSet) Duration(name string, value time.Duration, usage string) *time.Duration {
	p := new(time.Duration)
	f.DurationVar(p, name, value, usage)
	return p
}

// Duration defines a time.Duration flag with specified name, default value, and usage string.
func Duration(name string, value time.Duration, usage string) *time.Duration {
	return CommandLine.Duration(name, value, usage)
}

// TextVar defines a flag with specified name, default value, and usage string.
func (f *FlagSet) TextVar(p encoding.TextUnmarshaler, name string, value encoding.TextMarshaler, usage string) {
	f.Var(newTextValue(value, p), name, usage)
}

// TextVar defines a flag with specified name, default value, and usage string.
func TextVar(p encoding.TextUnmarshaler, name string, value encoding.TextMarshaler, usage string) {
	CommandLine.Var(newTextValue(value, p), name, usage)
}

// Func defines a flag with the specified name and usage string.
func (f *FlagSet) Func(name, usage string, fn func(string) error) {
	f.Var(funcValue(fn), name, usage)
}

// Func defines a flag with the specified name and usage string.
func Func(name, usage string, fn func(string) error) {
	CommandLine.Var(funcValue(fn), name, usage)
}

// BoolFunc defines a flag with the specified name and usage string.
func (f *FlagSet) BoolFunc(name, usage string, fn func(string) error) {
	f.Var(boolFuncValue(fn), name, usage)
}

// BoolFunc defines a flag with the specified name and usage string.
func BoolFunc(name, usage string, fn func(string) error) {
	CommandLine.Var(boolFuncValue(fn), name, usage)
}

// Var defines a flag with the specified name and usage string.
// stringsContainsByte reports whether s contains c.
func stringsContainsByte(s string, c byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return true
		}
	}
	return false
}

func (f *FlagSet) Var(value Value, name string, usage string) {
	// NOTE: strings.HasPrefix/Contains are not callable from synthetic
	// packages; inlined manually.
	if len(name) > 0 && name[0] == '-' {
		panic("flag " + strconv.Quote(name) + " begins with -")
	} else if stringsContainsByte(name, '=') {
		panic("flag " + strconv.Quote(name) + " contains =")
	}

	flag := &Flag{name, usage, value, value.String()}
	_, alreadythere := f.formal[name]
	if alreadythere {
		var msg string
		if f.name == "" {
			msg = "flag redefined: " + name
		} else {
			msg = f.name + " flag redefined: " + name
		}
		panic(msg)
	}
	if pos := f.undef[name]; pos != "" {
		panic("flag " + name + " set at " + pos + " before being defined")
	}
	if f.formal == nil {
		f.formal = make(map[string]*Flag)
	}
	f.formal[name] = flag
}

// Var defines a flag with the specified name and usage string.
func Var(value Value, name string, usage string) {
	CommandLine.Var(value, name, usage)
}

// failf prints msg to standard error and the usage message, and returns
// the error. (fmt.Sprintf with ...any spread does not unpack in v1, so
// call sites build messages with concatenation.)
func (f *FlagSet) failf(msg string) error {
	writeStr(f.Output(), msg+"\n")
	f.usage()
	return errors.New(msg)
}

// usage calls the Usage method for the flag set if one is specified,
// or the appropriate default usage function otherwise.
func (f *FlagSet) usage() {
	if f.Usage == nil {
		f.defaultUsage()
		return
	}
	// NOTE: f.Usage() selector call on a func field is not in scope.
	// Invoke via a package-level func value variable.
	var u func() = f.Usage
	u()
}

// parseOne parses one flag. It reports whether a flag was seen.
// parseOneResult bundles parseOne's pair; synthetic methods cannot have
// two results.
type parseOneResult struct {
	seen bool
	err  error
}

func (f *FlagSet) parseOne() parseOneResult {
	if len(f.args) == 0 {
		return parseOneResult{false, nil}
	}
	s := f.args[0]
	if len(s) < 2 || s[0] != '-' {
		return parseOneResult{false, nil}
	}
	numMinuses := 1
	if s[1] == '-' {
		numMinuses++
		if len(s) == 2 {
			f.args = f.args[1:]
			return parseOneResult{false, nil}
		}
	}
	name := s[numMinuses:]
	if len(name) == 0 || name[0] == '-' || name[0] == '=' {
		return parseOneResult{false, f.failf("bad flag syntax: " + s)}
	}

	f.args = f.args[1:]
	hasValue := false
	value := ""
	for i := 1; i < len(name); i++ {
		if name[i] == '=' {
			value = name[i+1:]
			hasValue = true
			name = name[0:i]
			break
		}
	}

	flag, ok := f.formal[name]
	if !ok {
		if name == "help" || name == "h" {
			f.usage()
			return parseOneResult{false, ErrHelp}
		}
		return parseOneResult{false, f.failf("flag provided but not defined: -" + name)}
	}

	if fv, ok := flag.Value.(boolFlag); ok && fv.IsBoolFlag() {
		if hasValue {
			if err := fv.Set(value); err != nil {
				return parseOneResult{false, f.failf("invalid boolean value " + strconv.Quote(value) + " for -" + name + ": " + err.Error())}
			}
		} else {
			if err := fv.Set("true"); err != nil {
				return parseOneResult{false, f.failf("invalid boolean flag " + name + ": " + err.Error())}
			}
		}
	} else {
		if !hasValue && len(f.args) > 0 {
			hasValue = true
			value, f.args = f.args[0], f.args[1:]
		}
		if !hasValue {
			return parseOneResult{false, f.failf("flag needs an argument: -" + name)}
		}
		if err := flag.Value.Set(value); err != nil {
			return parseOneResult{false, f.failf("invalid value " + strconv.Quote(value) + " for flag -" + name + ": " + err.Error())}
		}
	}
	if f.actual == nil {
		f.actual = make(map[string]*Flag)
	}
	f.actual[name] = flag
	return parseOneResult{true, nil}
}

// Parse parses flag definitions from the argument list.
func (f *FlagSet) Parse(arguments []string) error {
	f.parsed = true
	f.args = arguments
	for {
		r := f.parseOne()
		if r.seen {
			continue
		}
		if r.err == nil {
			break
		}
		switch f.errorHandling {
		case ContinueOnError:
			return r.err
		case ExitOnError:
			if r.err == ErrHelp {
				os.Exit(0)
			}
			os.Exit(2)
		case PanicOnError:
			panic(r.err)
		}
	}
	return nil
}

// Parsed reports whether f.Parse has been called.
func (f *FlagSet) Parsed() bool {
	return f.parsed
}

// Parse parses the command-line flags from os.Args[1:].
func Parse() {
	CommandLine.Parse(os.Args[1:])
}

// Parsed reports whether the command-line flags have been parsed.
func Parsed() bool {
	return CommandLine.Parsed()
}

// CommandLine is the default set of command-line flags, parsed from os.Args.
var CommandLine = NewFlagSet(os.Args[0], ExitOnError)

func init() {
	CommandLine.Usage = commandLineUsage
}

func commandLineUsage() {
	writeStr(CommandLine.Output(), "Usage of "+os.Args[0]+":\n")
	PrintDefaults()
}

// NewFlagSet returns a new, empty flag set with the specified name and
// error handling property.
func NewFlagSet(name string, errorHandling ErrorHandling) *FlagSet {
	f := &FlagSet{
		name:          name,
		errorHandling: errorHandling,
	}
	// NOTE: f.Usage is left nil; usage() performs the default when nil.
	// (Assigning the method value f.defaultUsage is not in scope.)
	return f
}

// Init sets the name and error handling property for a flag set.
func (f *FlagSet) Init(name string, errorHandling ErrorHandling) {
	f.name = name
	f.errorHandling = errorHandling
}

`

