package main

// P3-M9: synthetic testing/quick package.

const syntheticTestingQuickSource = `// Package quick implements utility functions to help with black box testing.
//
// Synthetic P3-M9 package: faithful port of go1.27.1 testing/quick/quick.go,
// adapted to the synthetic scope:
//
//   - Randomness comes from an internal deterministic xorshift64* PRNG
//     (qrng), NOT math/rand: Config.Rand, Config.Values, the Generator
//     interface, and Value(t, rand) all name *math/rand.Rand, which has not
//     landed (tier1c owns math/rand; re-test trigger: synthetic math/rand).
//     DIVERGENCE (documented, unobservable in differential output): gc seeds
//     from time.Now (nondeterministic sequences); this port uses a fixed
//     seed, so generated inputs are deterministic. Corpus programs print
//     pass/fail only, never generated values.
//   - The omitted Config.Rand/Config.Values fields, Generator interface,
//     and Value function are compile-time scope boundaries: user code naming
//     them fails loudly at compile time, never silently.
//   - Check, CheckEqual, SetupError, CheckError, CheckEqualError, and the
//     -quickchecks flag are gc-exact.
package quick

import (
	"flag"
	"math"
	"reflect"
)

// anyToString converts a single value to string.
func anyToString(v any) string {
	return toString([]any{v})
}

// formatString is a minimal Sprintf for %d, %s, %v (no spread unpacking issues).
func formatString(format string, args []any) string {
	var b string
	argIdx := 0
	for i := 0; i < len(format); i++ {
		if format[i] == '%' && i+1 < len(format) {
			i++
			var arg any
			if argIdx < len(args) {
				arg = args[argIdx]
				argIdx++
			}
			switch format[i] {
			case 'd', 'v', 's':
				b += anyToString(arg)
			case '%':
				b += "%"
			default:
				b += "%" + string(format[i])
			}
		} else {
			b += string(format[i])
		}
	}
	return b
}

var defaultMaxCount *int = flag.Int("quickchecks", 100, "The default number of iterations for each check")

// qrng is the internal deterministic PRNG (xorshift64*) backing value
// generation. See the package doc for why math/rand is not used.
type qrng struct {
	s uint64
}

func newQrng(seed uint64) *qrng {
	if seed == 0 {
		seed = 0x9E3779B97F4A7C15
	}
	return &qrng{s: seed}
}

func (r *qrng) uint64() uint64 {
	x := r.s
	x ^= x >> 12
	x ^= x << 25
	x ^= x >> 27
	r.s = x
	return x * 0x2545F4914F6CDD1D
}

func (r *qrng) int63() int64 {
	return int64(r.uint64() >> 1)
}

func (r *qrng) intn(n int) int {
	if n <= 0 {
		panic("quick: invalid Intn argument")
	}
	return int(r.uint64() % uint64(n))
}

func (r *qrng) int() int {
	return int(r.int63())
}

func (r *qrng) float64() float64 {
	return float64(r.uint64()>>11) / (1 << 53)
}

func randFloat32(rand *qrng) float32 {
	f := rand.float64() * math.MaxFloat32
	if rand.int()&1 == 1 {
		f = -f
	}
	return float32(f)
}

func randFloat64(rand *qrng) float64 {
	f := rand.float64() * math.MaxFloat64
	if rand.int()&1 == 1 {
		f = -f
	}
	return f
}

func randInt64(rand *qrng) int64 {
	return int64(rand.uint64())
}

const complexSize = 50

// sizedValueResult bundles sizedValue's pair; synthetic functions cannot
// have two results.
type sizedValueResult struct {
	value reflect.Value
	ok    bool
}

func sizedValue(t reflect.Type, rand *qrng, size int) sizedValueResult {
	v := reflect.New(t).Elem()
	switch concrete := t; concrete.Kind() {
	case reflect.Bool:
		v.SetBool(rand.int()&1 == 0)
	case reflect.Float32:
		v.SetFloat(float64(randFloat32(rand)))
	case reflect.Float64:
		v.SetFloat(randFloat64(rand))
	case reflect.Complex64:
		v.SetComplex(complex(float64(randFloat32(rand)), float64(randFloat32(rand))))
	case reflect.Complex128:
		v.SetComplex(complex(randFloat64(rand), randFloat64(rand)))
	case reflect.Int16:
		v.SetInt(randInt64(rand))
	case reflect.Int32:
		v.SetInt(randInt64(rand))
	case reflect.Int64:
		v.SetInt(randInt64(rand))
	case reflect.Int8:
		v.SetInt(randInt64(rand))
	case reflect.Int:
		v.SetInt(randInt64(rand))
	case reflect.Uint16:
		v.SetUint(uint64(randInt64(rand)))
	case reflect.Uint32:
		v.SetUint(uint64(randInt64(rand)))
	case reflect.Uint64:
		v.SetUint(uint64(randInt64(rand)))
	case reflect.Uint8:
		v.SetUint(uint64(randInt64(rand)))
	case reflect.Uint:
		v.SetUint(uint64(randInt64(rand)))
	case reflect.Uintptr:
		v.SetUint(uint64(randInt64(rand)))
	case reflect.Map:
		numElems := rand.intn(size)
		v.Set(reflect.MakeMap(concrete))
		for i := 0; i < numElems; i++ {
			kr := sizedValue(concrete.Key(), rand, size)
			key, ok1 := kr.value, kr.ok
			vr := sizedValue(concrete.Elem(), rand, size)
			value, ok2 := vr.value, vr.ok
			if !ok1 || !ok2 {
				return sizedValueResult{}
			}
			v.SetMapIndex(key, value)
		}
	case reflect.Pointer:
		if rand.intn(size) == 0 {
			v.SetZero()
		} else {
			er := sizedValue(concrete.Elem(), rand, size)
			elem, ok := er.value, er.ok
			if !ok {
				return sizedValueResult{}
			}
			v.Set(reflect.New(concrete.Elem()))
			v.Elem().Set(elem)
		}
	case reflect.Slice:
		numElems := rand.intn(size)
		sizeLeft := size - numElems
		v.Set(reflect.MakeSlice(concrete, numElems, numElems))
		for i := 0; i < numElems; i++ {
			er := sizedValue(concrete.Elem(), rand, sizeLeft)
			elem, ok := er.value, er.ok
			if !ok {
				return sizedValueResult{}
			}
			v.Index(i).Set(elem)
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			er := sizedValue(concrete.Elem(), rand, size)
			elem, ok := er.value, er.ok
			if !ok {
				return sizedValueResult{}
			}
			v.Index(i).Set(elem)
		}
	case reflect.String:
		numChars := rand.intn(complexSize)
		codePoints := make([]rune, numChars)
		for i := 0; i < numChars; i++ {
			codePoints[i] = rune(rand.intn(0x10ffff))
		}
		v.SetString(string(codePoints))
	case reflect.Struct:
		n := v.NumField()
		sizeLeft := size
		if n > sizeLeft {
			sizeLeft = 1
		} else if n > 0 {
			sizeLeft /= n
		}
		for i := 0; i < n; i++ {
			er := sizedValue(concrete.Field(i).Type, rand, sizeLeft)
			elem, ok := er.value, er.ok
			if !ok {
				return sizedValueResult{}
			}
			v.Field(i).Set(elem)
		}
	default:
		return sizedValueResult{}
	}

	return sizedValueResult{v, true}
}

// A Config structure contains options for running a test.
type Config struct {
	// MaxCount sets the maximum number of iterations.
	// If zero, MaxCountScale is used.
	MaxCount int
	// MaxCountScale is a non-negative scale factor applied to the
	// default maximum.
	// A count of zero implies the default, which is usually 100
	// but can be set by the -quickchecks flag.
	MaxCountScale float64
	// DIVERGENCE (documented): gc's Config also has Rand *rand.Rand and
	// Values func([]reflect.Value, *rand.Rand). Both name *math/rand.Rand,
	// which has not landed (tier1c owns math/rand). They are omitted here
	// as a compile-time scope boundary; re-test when synthetic math/rand
	// lands.
}

var defaultConfig Config

func (c *Config) getRand() *qrng {
	return newQrng(0x9E3779B97F4A7C15)
}

func (c *Config) getMaxCount() (maxCount int) {
	maxCount = c.MaxCount
	if maxCount == 0 {
		if c.MaxCountScale != 0 {
			maxCount = int(c.MaxCountScale * float64(*defaultMaxCount))
		} else {
			maxCount = *defaultMaxCount
		}
	}

	return
}

// A SetupError is the result of an error in the way that check is being
// used, independent of the functions being tested.
type SetupError string

func (s SetupError) Error() string { return string(s) }

// A CheckError is the result of Check finding an error.
type CheckError struct {
	Count int
	In    []any
}

func (s *CheckError) Error() string {
	return formatString("#%d: failed on input %s", []any{s.Count, toString(s.In)})
}

// A CheckEqualError is the result [CheckEqual] finding an error.
type CheckEqualError struct {
	CheckError
	Out1 []any
	Out2 []any
}

func (s *CheckEqualError) Error() string {
	return formatString("#%d: failed on input %s. Output 1: %s. Output 2: %s", []any{s.Count, toString(s.In), toString(s.Out1), toString(s.Out2)})
}

// Check looks for an input to f, any function that returns bool,
// such that f returns false.
func Check(f any, config *Config) error {
	if config == nil {
		config = &defaultConfig
	}

	ftr := functionAndType(f)
	fVal, fType, ok := ftr.v, ftr.t, ftr.ok
	if !ok {
		return SetupError("argument is not a function")
	}

	if fType.NumOut() != 1 {
		return SetupError("function does not return one value")
	}
	if fType.Out(0).Kind() != reflect.Bool {
		return SetupError("function does not return a bool")
	}

	arguments := make([]reflect.Value, fType.NumIn())
	rand := config.getRand()
	maxCount := config.getMaxCount()

	for i := 0; i < maxCount; i++ {
		err := arbitraryValues(arguments, fType, rand)
		if err != nil {
			return err
		}

		if !fVal.Call(arguments)[0].Bool() {
			return &CheckError{i + 1, toInterfaces(arguments)}
		}
	}

	return nil
}

// CheckEqual looks for an input on which f and g return different results.
func CheckEqual(f, g any, config *Config) error {
	if config == nil {
		config = &defaultConfig
	}

	fxr := functionAndType(f)
	x, xType, ok := fxr.v, fxr.t, fxr.ok
	if !ok {
		return SetupError("f is not a function")
	}
	fyr := functionAndType(g)
	y, yType, ok := fyr.v, fyr.t, fyr.ok
	if !ok {
		return SetupError("g is not a function")
	}

	if xType != yType {
		return SetupError("functions have different types")
	}

	arguments := make([]reflect.Value, xType.NumIn())
	rand := config.getRand()
	maxCount := config.getMaxCount()

	for i := 0; i < maxCount; i++ {
		err := arbitraryValues(arguments, xType, rand)
		if err != nil {
			return err
		}

		xOut := toInterfaces(x.Call(arguments))
		yOut := toInterfaces(y.Call(arguments))

		if !reflect.DeepEqual(xOut, yOut) {
			return &CheckEqualError{CheckError{i + 1, toInterfaces(arguments)}, xOut, yOut}
		}
	}

	return nil
}

func arbitraryValues(args []reflect.Value, f reflect.Type, rand *qrng) (err error) {
	for j := 0; j < len(args); j++ {
		var ok bool
		svr := sizedValue(f.In(j), rand, complexSize)
		args[j], ok = svr.value, svr.ok
		if !ok {
			err = SetupError(formatString("cannot create arbitrary value of type %s for argument %d", []any{f.In(j), j}))
			return
		}
	}

	return
}

// functionAndTypeResult bundles functionAndType's triple; synthetic
// functions cannot have three results.
type functionAndTypeResult struct {
	v  reflect.Value
	t  reflect.Type
	ok bool
}

func functionAndType(f any) functionAndTypeResult {
	v := reflect.ValueOf(f)
	if v.Kind() != reflect.Func {
		return functionAndTypeResult{}
	}
	return functionAndTypeResult{v, v.Type(), true}
}

func toInterfaces(values []reflect.Value) []any {
	ret := make([]any, len(values))
	for i, v := range values {
		ret[i] = v.Interface()
	}
	return ret
}

func toString(interfaces []any) string {
	s := make([]string, len(interfaces))
	for i, v := range interfaces {
		s[i] = formatString("%v", []any{v})
	}
	// Manual join (strings.Join not callable from synthetic).
	var b string
	for i, part := range s {
		if i > 0 {
			b += ", "
		}
		b += part
	}
	return b
}

`
