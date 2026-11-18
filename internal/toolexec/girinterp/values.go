package girinterp

import (
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"

	"graalgo-toolexec/ir"
)

// Value is a Go value at run time. Representation by kind:
//
//	bool, integers, floats, complex, string: the Go value of that kind
//	pointer: *Value (the address of a variable, field or element slot)
//	struct: *Struct; array: *Array (mutable storage, copied on assignment)
//	slice: Slice; map: *Map; chan: *Chan; func: *Closure
//	interface: Iface
type Value = any

// Struct is struct storage.
type Struct struct{ F []Value }

// Array is array storage.
type Array struct{ E []Value }

// Slice is a slice header over a backing array.
type Slice struct {
	A             []Value // backing array (nil: nil slice)
	Off, Len, Cap int
}

// Iface is an interface value (T nil: nil interface).
type Iface struct {
	T *RType
	V Value
}

// Map is a map: insertion-ordered (Go leaves iteration order unspecified).
type Map struct {
	keys  []Value
	vals  []Value
	index map[string]int
	live  []bool
	n     int
}

func newMap() *Map { return &Map{index: map[string]int{}} }

// Chan is a channel (single goroutine: operations that would block are
// a deadlock).
type Chan struct {
	buf    []Value
	cap    int
	closed bool
}

func isAggregate(t *RType) bool {
	k := t.Under().Kind
	return k == KStruct || k == KArray
}

func (in *Interp) zero(t *RType) Value {
	u := t.Under()
	switch u.Kind {
	case KBool:
		return false
	case KInt:
		return int(0)
	case KInt8:
		return int8(0)
	case KInt16:
		return int16(0)
	case KInt32:
		return int32(0)
	case KInt64:
		return int64(0)
	case KUint:
		return uint(0)
	case KUint8:
		return uint8(0)
	case KUint16:
		return uint16(0)
	case KUint32:
		return uint32(0)
	case KUint64:
		return uint64(0)
	case KUintptr:
		return uintptr(0)
	case KFloat32:
		return float32(0)
	case KFloat64:
		return float64(0)
	case KComplex64:
		return complex64(0)
	case KComplex128:
		return complex128(0)
	case KString:
		return ""
	case KPointer:
		return (*Value)(nil)
	case KSlice:
		return Slice{}
	case KMap:
		return (*Map)(nil)
	case KChan:
		return (*Chan)(nil)
	case KFunc:
		return (*Closure)(nil)
	case KInterface:
		return Iface{}
	case KStruct:
		s := &Struct{F: make([]Value, len(u.Fields))}
		for i, f := range u.Fields {
			s.F[i] = in.zero(f.Type)
		}
		return s
	case KArray:
		a := &Array{E: make([]Value, u.Len)}
		for i := range a.E {
			a.E[i] = in.zero(u.Elem)
		}
		return a
	case KUnsafePointer:
		return (*Value)(nil)
	}
	panic(interpError("zero value of " + t.Str))
}

// copyVal returns an independent copy of v (aggregates are deep-copied).
func copyVal(v Value) Value {
	switch x := v.(type) {
	case *Struct:
		n := &Struct{F: make([]Value, len(x.F))}
		for i, f := range x.F {
			n.F[i] = copyVal(f)
		}
		return n
	case *Array:
		n := &Array{E: make([]Value, len(x.E))}
		for i, e := range x.E {
			n.E[i] = copyVal(e)
		}
		return n
	}
	return v
}

// store writes v into slot, copying aggregates in place so that pointers
// into the old storage keep seeing the variable.
func store(slot *Value, v Value) {
	switch x := v.(type) {
	case *Struct:
		if d, ok := (*slot).(*Struct); ok && d != x {
			src := copyVal(x).(*Struct)
			for i := range d.F {
				store(&d.F[i], src.F[i])
			}
			return
		}
		if d, ok := (*slot).(*Struct); ok && d == x {
			return
		}
	case *Array:
		if d, ok := (*slot).(*Array); ok && d != x {
			src := copyVal(x).(*Array)
			for i := range d.E {
				store(&d.E[i], src.E[i])
			}
			return
		}
		if d, ok := (*slot).(*Array); ok && d == x {
			return
		}
	}
	*slot = copyVal(v)
}

func (in *Interp) comparable(t *RType) bool {
	u := t.Under()
	switch u.Kind {
	case KSlice, KMap, KFunc:
		return false
	case KStruct:
		for _, f := range u.Fields {
			if !in.comparable(f.Type) {
				return false
			}
		}
	case KArray:
		return in.comparable(u.Elem)
	}
	return true
}

// equal implements == on values of type t.
func (in *Interp) equal(a, b Value, t *RType) bool {
	u := t.Under()
	switch u.Kind {
	case KStruct:
		x, y := a.(*Struct), b.(*Struct)
		for i, f := range u.Fields {
			if !in.equal(x.F[i], y.F[i], f.Type) {
				return false
			}
		}
		return true
	case KArray:
		x, y := a.(*Array), b.(*Array)
		for i := range x.E {
			if !in.equal(x.E[i], y.E[i], u.Elem) {
				return false
			}
		}
		return true
	case KInterface:
		x, y := a.(Iface), b.(Iface)
		if x.T == nil || y.T == nil {
			return x.T == y.T
		}
		if x.T != y.T {
			return false
		}
		if !in.comparable(x.T) {
			in.runtimePanic("comparing uncomparable type " + x.T.Str)
		}
		return in.equal(x.V, y.V, x.T)
	case KSlice:
		return a.(Slice).A == nil && b.(Slice).A == nil
	case KMap:
		return a.(*Map) == b.(*Map)
	case KFunc:
		return a.(*Closure) == b.(*Closure)
	}
	return a == b
}

// hashKey is a canonical string for map key v of type t.
func (in *Interp) hashKey(v Value, t *RType) string {
	var b strings.Builder
	in.writeKey(&b, v, t)
	return b.String()
}

func (in *Interp) writeKey(b *strings.Builder, v Value, t *RType) {
	u := t.Under()
	switch u.Kind {
	case KStruct:
		b.WriteByte('{')
		for i, f := range u.Fields {
			in.writeKey(b, v.(*Struct).F[i], f.Type)
			b.WriteByte(',')
		}
		b.WriteByte('}')
	case KArray:
		b.WriteByte('[')
		for _, e := range v.(*Array).E {
			in.writeKey(b, e, u.Elem)
			b.WriteByte(',')
		}
		b.WriteByte(']')
	case KInterface:
		x := v.(Iface)
		if x.T == nil {
			b.WriteString("nil")
			return
		}
		if !in.comparable(x.T) {
			in.runtimePanic("hash of unhashable type " + x.T.Str)
		}
		b.WriteString(x.T.Key + ":")
		in.writeKey(b, x.V, x.T)
	case KFloat32, KFloat64, KComplex64, KComplex128:
		var c complex128
		switch f := v.(type) {
		case float32:
			c = complex(float64(f), 0)
		case float64:
			c = complex(f, 0)
		case complex64:
			c = complex128(f)
		case complex128:
			c = f
		}
		if real(c) != real(c) || imag(c) != imag(c) {
			in.nanKeys++
			b.WriteString("NaN#" + strconv.Itoa(in.nanKeys)) // NaN != NaN: every insert is new
			return
		}
		re, im := real(c), imag(c)
		if re == 0 {
			re = 0 // -0 == +0
		}
		if im == 0 {
			im = 0
		}
		b.WriteString(strconv.FormatFloat(re, 'g', -1, 64) + "," + strconv.FormatFloat(im, 'g', -1, 64))
	case KString:
		s := v.(string)
		b.WriteString(strconv.Itoa(len(s)) + ":" + s)
	case KPointer:
		b.WriteString("p" + strconv.Itoa(in.ptrID(v.(*Value))))
	case KChan:
		b.WriteString("c" + strconv.Itoa(in.chanID(v.(*Chan))))
	default:
		b.WriteString(strconv.Quote(formatScalar(v)))
	}
}

func (in *Interp) ptrID(p *Value) int {
	if p == nil {
		return 0
	}
	if id, ok := in.ptrIDs[p]; ok {
		return id
	}
	id := len(in.ptrIDs) + 1
	in.ptrIDs[p] = id
	return id
}

func (in *Interp) chanID(c *Chan) int {
	if c == nil {
		return 0
	}
	if id, ok := in.chanIDs[c]; ok {
		return id
	}
	id := len(in.chanIDs) + 1
	in.chanIDs[c] = id
	return id
}

func (m *Map) lookup(in *Interp, k Value, kt *RType) (Value, bool) {
	if m == nil {
		return nil, false
	}
	if i, ok := m.index[in.hashKey(k, kt)]; ok {
		return m.vals[i], true
	}
	return nil, false
}

func (m *Map) set(in *Interp, k Value, kt *RType, v Value) {
	h := in.hashKey(k, kt)
	if i, ok := m.index[h]; ok {
		store(&m.vals[i], v)
		return
	}
	m.index[h] = len(m.keys)
	m.keys = append(m.keys, copyVal(k))
	m.vals = append(m.vals, copyVal(v))
	m.live = append(m.live, true)
	m.n++
}

func (m *Map) del(in *Interp, k Value, kt *RType) {
	if m == nil {
		return
	}
	h := in.hashKey(k, kt)
	if i, ok := m.index[h]; ok {
		delete(m.index, h)
		m.live[i] = false
		m.keys[i], m.vals[i] = nil, nil
		m.n--
	}
}

func (m *Map) clear() {
	if m == nil {
		return
	}
	for h := range m.index {
		delete(m.index, h)
	}
	for i := range m.live {
		m.live[i] = false
		m.keys[i], m.vals[i] = nil, nil
	}
	m.n = 0
}

// ---------------------------------------------------------------- scalars

func formatScalar(v Value) string {
	switch x := v.(type) {
	case bool:
		return strconv.FormatBool(x)
	case string:
		return x
	case float32:
		return strconv.FormatFloat(float64(x), 'g', -1, 32)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case complex64:
		return strconv.FormatComplex(complex128(x), 'g', -1, 64)
	case complex128:
		return strconv.FormatComplex(x, 'g', -1, 128)
	}
	if i, ok := asInt64(v); ok {
		if isUnsignedVal(v) {
			return strconv.FormatUint(uint64(i), 10)
		}
		return strconv.FormatInt(i, 10)
	}
	panic(interpError("formatScalar of non-scalar"))
}

func isUnsignedVal(v Value) bool {
	switch v.(type) {
	case uint, uint8, uint16, uint32, uint64, uintptr:
		return true
	}
	return false
}

// asInt64 returns an integer value's bits as int64.
func asInt64(v Value) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int8:
		return int64(x), true
	case int16:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case uint:
		return int64(x), true
	case uint8:
		return int64(x), true
	case uint16:
		return int64(x), true
	case uint32:
		return int64(x), true
	case uint64:
		return int64(x), true
	case uintptr:
		return int64(x), true
	}
	return 0, false
}

// fromInt64 converts integer bits to kind k with Go's truncation.
func fromInt64(i int64, unsigned bool, k Kind) Value {
	switch k {
	case KInt:
		return int(i)
	case KInt8:
		return int8(i)
	case KInt16:
		return int16(i)
	case KInt32:
		return int32(i)
	case KInt64:
		return i
	case KUint:
		return uint(i)
	case KUint8:
		return uint8(i)
	case KUint16:
		return uint16(i)
	case KUint32:
		return uint32(i)
	case KUint64:
		return uint64(i)
	case KUintptr:
		return uintptr(i)
	case KFloat32:
		if unsigned {
			return float32(uint64(i))
		}
		return float32(i)
	case KFloat64:
		if unsigned {
			return float64(uint64(i))
		}
		return float64(i)
	case KComplex64:
		if unsigned {
			return complex(float32(uint64(i)), 0)
		}
		return complex(float32(i), 0)
	case KComplex128:
		if unsigned {
			return complex(float64(uint64(i)), 0)
		}
		return complex(float64(i), 0)
	}
	panic(interpError("integer conversion"))
}

func fromFloat(f float64, k Kind) Value {
	switch k {
	case KInt:
		return int(f)
	case KInt8:
		return int8(f)
	case KInt16:
		return int16(f)
	case KInt32:
		return int32(f)
	case KInt64:
		return int64(f)
	case KUint:
		return uint(f)
	case KUint8:
		return uint8(f)
	case KUint16:
		return uint16(f)
	case KUint32:
		return uint32(f)
	case KUint64:
		return uint64(f)
	case KUintptr:
		return uintptr(f)
	case KFloat32:
		return float32(f)
	case KFloat64:
		return f
	case KComplex64:
		return complex(float32(f), 0)
	case KComplex128:
		return complex(f, 0)
	}
	panic(interpError("float conversion"))
}

// constValue materializes a GIR constant as a value of type t.
func (in *Interp) constValue(c *ir.ConstValue, t *RType) Value {
	u := t.Under()
	switch c.Kind {
	case ir.ConstKindBool:
		return c.Text == "true"
	case ir.ConstKindString:
		if u.Kind == KInterface {
			return Iface{T: in.basic(KString), V: c.Text}
		}
		return c.Text
	}
	if u.Kind == KInterface {
		panic(interpError("non-string constant of interface type " + t.Str))
	}
	re := parseRat(c.Text)
	switch u.Kind {
	case KFloat32, KFloat64:
		f, _ := re.Float64()
		if u.Kind == KFloat32 {
			f32, _ := re.Float32()
			return f32
		}
		return f
	case KComplex64, KComplex128:
		f, _ := re.Float64()
		im := 0.0
		if c.Imag != "" {
			im, _ = parseRat(c.Imag).Float64()
		}
		if u.Kind == KComplex64 {
			return complex64(complex(f, im))
		}
		return complex(f, im)
	}
	if !re.IsInt() {
		panic(interpError("non-integer constant " + c.Text + " of type " + t.Str))
	}
	n := re.Num()
	if !n.IsInt64() && !n.IsUint64() {
		panic(interpError("constant " + c.Text + " overflows " + t.Str))
	}
	if n.IsInt64() {
		return fromInt64(n.Int64(), false, u.Kind)
	}
	return fromInt64(int64(n.Uint64()), true, u.Kind)
}

func parseRat(s string) *big.Rat {
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		panic(interpError("bad constant " + s))
	}
	return r
}

// ---------------------------------------------------------------- operators

type integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

type ordered interface {
	integer | ~float32 | ~float64 | ~string
}

func intArith[T integer](in *Interp, op ir.BinOp, x, y T) Value {
	switch op {
	case ir.BinOpAdd:
		return x + y
	case ir.BinOpSub:
		return x - y
	case ir.BinOpMul:
		return x * y
	case ir.BinOpQuo:
		if y == 0 {
			in.runtimePanic("integer divide by zero")
		}
		return x / y
	case ir.BinOpRem:
		if y == 0 {
			in.runtimePanic("integer divide by zero")
		}
		return x % y
	case ir.BinOpAnd:
		return x & y
	case ir.BinOpOr:
		return x | y
	case ir.BinOpXor:
		return x ^ y
	case ir.BinOpAndNot:
		return x &^ y
	}
	return cmpOp(op, x, y)
}

func floatArith[T ~float32 | ~float64](op ir.BinOp, x, y T) Value {
	switch op {
	case ir.BinOpAdd:
		return x + y
	case ir.BinOpSub:
		return x - y
	case ir.BinOpMul:
		return x * y
	case ir.BinOpQuo:
		return x / y
	}
	return cmpOp(op, x, y)
}

func complexArith[T ~complex64 | ~complex128](op ir.BinOp, x, y T) Value {
	switch op {
	case ir.BinOpAdd:
		return x + y
	case ir.BinOpSub:
		return x - y
	case ir.BinOpMul:
		return x * y
	case ir.BinOpQuo:
		return x / y
	case ir.BinOpEql:
		return x == y
	case ir.BinOpNeq:
		return x != y
	}
	panic(interpError("complex operator"))
}

func cmpOp[T ordered](op ir.BinOp, x, y T) Value {
	switch op {
	case ir.BinOpEql:
		return x == y
	case ir.BinOpNeq:
		return x != y
	case ir.BinOpLss:
		return x < y
	case ir.BinOpLeq:
		return x <= y
	case ir.BinOpGtr:
		return x > y
	case ir.BinOpGeq:
		return x >= y
	}
	panic(interpError("operator " + op.String()))
}

// arith applies a non-shift, non-logical binary operator to two scalars
// of the same type.
func (in *Interp) arith(op ir.BinOp, a, b Value) Value {
	switch x := a.(type) {
	case int:
		return intArith(in, op, x, b.(int))
	case int8:
		return intArith(in, op, x, b.(int8))
	case int16:
		return intArith(in, op, x, b.(int16))
	case int32:
		return intArith(in, op, x, b.(int32))
	case int64:
		return intArith(in, op, x, b.(int64))
	case uint:
		return intArith(in, op, x, b.(uint))
	case uint8:
		return intArith(in, op, x, b.(uint8))
	case uint16:
		return intArith(in, op, x, b.(uint16))
	case uint32:
		return intArith(in, op, x, b.(uint32))
	case uint64:
		return intArith(in, op, x, b.(uint64))
	case uintptr:
		return intArith(in, op, x, b.(uintptr))
	case float32:
		return floatArith(op, x, b.(float32))
	case float64:
		return floatArith(op, x, b.(float64))
	case complex64:
		return complexArith(op, x, b.(complex64))
	case complex128:
		return complexArith(op, x, b.(complex128))
	case string:
		if op == ir.BinOpAdd {
			return x + b.(string)
		}
		return cmpOp(op, x, b.(string))
	case bool:
		switch op {
		case ir.BinOpEql:
			return x == b.(bool)
		case ir.BinOpNeq:
			return x != b.(bool)
		}
	}
	panic(interpError("operator " + op.String() + " on non-scalar"))
}

func shiftOf[T integer](x T, n uint64, left bool) Value {
	if left {
		return x << n // Go's own shift semantics: counts >= width give 0 / sign
	}
	return x >> n
}

// shift implements x << n and x >> n; n is any integer value.
func (in *Interp) shift(x, n Value, left bool) Value {
	c, _ := asInt64(n)
	if !isUnsignedVal(n) && c < 0 {
		in.runtimePanic("negative shift amount")
	}
	s := uint64(c)
	switch v := x.(type) {
	case int:
		return shiftOf(v, s, left)
	case int8:
		return shiftOf(v, s, left)
	case int16:
		return shiftOf(v, s, left)
	case int32:
		return shiftOf(v, s, left)
	case int64:
		return shiftOf(v, s, left)
	case uint:
		return shiftOf(v, s, left)
	case uint8:
		return shiftOf(v, s, left)
	case uint16:
		return shiftOf(v, s, left)
	case uint32:
		return shiftOf(v, s, left)
	case uint64:
		return shiftOf(v, s, left)
	case uintptr:
		return shiftOf(v, s, left)
	}
	panic(interpError("shift of non-integer"))
}

func negate[T integer | ~float32 | ~float64 | ~complex64 | ~complex128](x T) T { return -x }

func (in *Interp) unary(op ir.UnOp, v Value) Value {
	switch op {
	case ir.UnOpPos:
		return v
	case ir.UnOpNot:
		return !v.(bool)
	case ir.UnOpNeg:
		switch x := v.(type) {
		case int:
			return negate(x)
		case int8:
			return negate(x)
		case int16:
			return negate(x)
		case int32:
			return negate(x)
		case int64:
			return negate(x)
		case uint:
			return negate(x)
		case uint8:
			return negate(x)
		case uint16:
			return negate(x)
		case uint32:
			return negate(x)
		case uint64:
			return negate(x)
		case uintptr:
			return negate(x)
		case float32:
			return negate(x)
		case float64:
			return negate(x)
		case complex64:
			return negate(x)
		case complex128:
			return negate(x)
		}
	case ir.UnOpBitNot:
		switch x := v.(type) {
		case int:
			return ^x
		case int8:
			return ^x
		case int16:
			return ^x
		case int32:
			return ^x
		case int64:
			return ^x
		case uint:
			return ^x
		case uint8:
			return ^x
		case uint16:
			return ^x
		case uint32:
			return ^x
		case uint64:
			return ^x
		case uintptr:
			return ^x
		}
	}
	panic(interpError("unary " + op.String()))
}

// ---------------------------------------------------------------- conversions

// convert implements T(x) for a value of type from to type to (both
// resolved, neither a type parameter).
func (in *Interp) convert(v Value, from, to *RType) Value {
	tu, fu := to.Under(), from.Under()
	if tu.Kind == KInterface {
		if fu.Kind == KInterface {
			return v // dynamic type unchanged (assignability checked statically)
		}
		return Iface{T: from, V: copyVal(v)}
	}
	switch {
	case isNumeric(tu.Kind) && isNumeric(fu.Kind):
		if i, ok := asInt64(v); ok {
			return fromInt64(i, isUnsignedVal(v), tu.Kind)
		}
		switch x := v.(type) {
		case float32:
			return fromFloat(float64(x), tu.Kind)
		case float64:
			return fromFloat(x, tu.Kind)
		case complex64:
			if tu.Kind == KComplex128 {
				return complex128(x)
			}
			return x
		case complex128:
			if tu.Kind == KComplex64 {
				return complex64(x)
			}
			return x
		}
	case tu.Kind == KString && isInteger(fu.Kind):
		i, _ := asInt64(v)
		if isUnsignedVal(v) && uint64(i) > utf8.MaxRune || !isUnsignedVal(v) && (i < 0 || i > utf8.MaxRune) {
			return "�"
		}
		return string(rune(i))
	case tu.Kind == KString && fu.Kind == KSlice:
		s := v.(Slice)
		if fu.Elem.Under().Kind == KUint8 {
			b := make([]byte, s.Len)
			for i := range b {
				b[i] = s.A[s.Off+i].(uint8)
			}
			return string(b)
		}
		r := make([]rune, s.Len)
		for i := range r {
			r[i] = s.A[s.Off+i].(int32)
		}
		return string(r)
	case tu.Kind == KSlice && fu.Kind == KString:
		s := v.(string)
		var out []Value
		if tu.Elem.Under().Kind == KUint8 {
			out = make([]Value, len(s))
			for i := 0; i < len(s); i++ {
				out[i] = s[i]
			}
		} else {
			for _, r := range s {
				out = append(out, int32(r))
			}
			if out == nil {
				out = []Value{}
			}
		}
		return Slice{A: out, Len: len(out), Cap: len(out)}
	case tu.Kind == KArray && fu.Kind == KSlice:
		s := v.(Slice)
		if int64(s.Len) < tu.Len {
			in.runtimePanic("cannot convert slice with length " + strconv.Itoa(s.Len) + " to array or pointer to array with length " + strconv.FormatInt(tu.Len, 10))
		}
		a := &Array{E: make([]Value, tu.Len)}
		for i := range a.E {
			a.E[i] = copyVal(s.A[s.Off+i])
		}
		return a
	case tu.Kind == KPointer && fu.Kind == KSlice:
		s := v.(Slice)
		n := tu.Elem.Under().Len
		if int64(s.Len) < n {
			in.runtimePanic("cannot convert slice with length " + strconv.Itoa(s.Len) + " to array or pointer to array with length " + strconv.FormatInt(n, 10))
		}
		if s.A == nil {
			return (*Value)(nil)
		}
		panic(interpError("slice to array pointer aliasing is not modelled"))
	}
	return v // identical underlying types (or pointer/chan/func of such)
}

func isInteger(k Kind) bool  { return k >= KInt && k <= KUintptr }
func isNumeric(k Kind) bool  { return k >= KInt && k <= KComplex128 }
func isFloatish(k Kind) bool { return k >= KFloat32 && k <= KComplex128 }

var _ = math.Inf
