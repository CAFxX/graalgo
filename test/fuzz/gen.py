#!/usr/bin/env python3
"""test/fuzz/gen.py — seeded generator of random Go programs for the
differential fuzzer of the -toolexec path (test/fuzz/run.py).

Derived from v1's tools/fuzz-soak generator (deleted at m3-plan D3),
retargeted to what the new path supports today:
  - output through the builtin println only, so every program's
    observable output is its stderr (fmt links since toolexec M1d — the
    hand-written corpus has fmt programs, test/fuzz/corpus/stdlib/fmt_*.go
    — but the generator's printing is not converted yet);
  - imports limited to packages that link: strconv, strings, slices, sort,
    math;
  - the language beyond gen-1: generics (a fixed prelude of generic
    functions and types, instantiated at random types), interfaces and
    type switches, struct embedding, closures capturing locals, defers that
    mutate named results, labeled break/continue, range over int, runtime
    panics.

Every program is deterministic: maps are dumped in sorted key order, no
goroutines, no time, no addresses printed. Runtime panics are allowed by
construction (division by zero, index out of range, nil map write, failed
assertion...): each generated test function runs under a recover that
prints the panic message, so the panic text is compared too, and the
program continues with the next function. A float-to-integer conversion
goes through a clamping helper (out-of-range conversion is
implementation-defined in Go).

Usage: gen.py SEED [> prog.go]   (generate(seed) for library use)
"""
import random
import sys

INT_TYPES = ['int', 'int8', 'int16', 'int32', 'int64', 'uint', 'uint8',
             'uint16', 'uint32', 'uint64', 'uintptr']
FLOAT_TYPES = ['float32', 'float64']
SCALARS = INT_TYPES + FLOAT_TYPES + ['bool', 'string']
BITS = {'int': 64, 'int8': 8, 'int16': 16, 'int32': 32, 'int64': 64,
        'uint': 64, 'uint8': 8, 'uint16': 16, 'uint32': 32, 'uint64': 64,
        'uintptr': 64}
EDGE = [0, 1, 2, 3, 7, 8, 15, 16, 31, 32, 63, 64, 100, 127, 128, 255, 256,
        1000, 32767, 32768, 65535, 65536, 1 << 31, (1 << 31) - 1, 1 << 32,
        (1 << 32) - 1, (1 << 63) - 1, 1 << 62, 0x5555555555555555, 12345678901]
FLOATS = ['0.0', '1.0', '-1.0', '0.5', '0.1', '1e10', '-1e-10', '3.4028234663852886e38',
          '1.401298464324817e-45', '4.9e-324', '1.7976931348623157e308', '2.5',
          '1e300', '-0.75', '123456.789', '16777217.0', '9007199254740993.0']
STRINGS = ['""', '"a"', '"hello"', '"héllo"', '"日本語"', '"\\xff\\xfe"', '"a\\x00b"',
           '"Go"', '"🙂x"', '"\\u2028"', '"ab,cd,,ef"', '"  sp  "', '"ǅǆ"', '"ẞß"']

PRELUDE = r'''
type Number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr | ~float32 | ~float64
}

func gSum[T Number](xs ...T) T {
	var s T
	for _, x := range xs {
		s += x
	}
	return s
}

func gMax[T cmp.Ordered](a, b T) T {
	if a > b {
		return a
	}
	return b
}

func gMap[T, U any](xs []T, f func(T) U) []U {
	out := make([]U, 0, len(xs))
	for _, x := range xs {
		out = append(out, f(x))
	}
	return out
}

func gFilter[T any](xs []T, keep func(T) bool) []T {
	var out []T
	for _, x := range xs {
		if keep(x) {
			out = append(out, x)
		}
	}
	return out
}

type Pair[K comparable, V any] struct {
	Key K
	Val V
}

func (p Pair[K, V]) Swap() Pair[K, V] { return p }

type Stack[T any] struct{ items []T }

func (s *Stack[T]) Push(x T) { s.items = append(s.items, x) }
func (s *Stack[T]) Pop() (T, bool) {
	var zero T
	if len(s.items) == 0 {
		return zero, false
	}
	x := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return x, true
}
func (s *Stack[T]) Len() int { return len(s.items) }

type Shape interface {
	Area() float64
	Name() string
}

type Rect struct{ W, H float64 }
type Circle struct{ R float64 }
type Named struct {
	Rect
	Label string
}

func (r Rect) Area() float64    { return r.W * r.H }
func (r Rect) Name() string      { return "rect" }
func (c *Circle) Area() float64 { return 3 * c.R * c.R }
func (c *Circle) Name() string   { return "circle" }
func (n Named) Name() string     { return "named:" + n.Label }

type MyInt int

func (m MyInt) Double() MyInt { return m * 2 }

func describe(v any) string {
	switch x := v.(type) {
	case nil:
		return "nil"
	case int:
		return "int:" + strconv.Itoa(x)
	case int8, int16, int32, int64:
		return "intN"
	case uint8:
		return "byte:" + strconv.Itoa(int(x))
	case string:
		return "string:" + strconv.Quote(x)
	case float64:
		return "f64:" + strconv.FormatFloat(x, 'g', -1, 64)
	case float32:
		return "f32:" + strconv.FormatFloat(float64(x), 'g', -1, 32)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case MyInt:
		return "MyInt:" + strconv.Itoa(int(x.Double()))
	case Shape:
		return "shape:" + x.Name() + ":" + strconv.FormatFloat(x.Area(), 'g', 6, 64)
	case error:
		return "error:" + x.Error()
	}
	return "other"
}

func id[T any](x T) T { return x }

func nz[T Number](x T) T {
	if x == 0 {
		return 1
	}
	return x
}

func ix(i, n int) int {
	if n == 0 {
		return 0
	}
	i %= n
	if i < 0 {
		i += n
	}
	return i
}

func clip(s string) string {
	if len(s) > 256 {
		return s[:256]
	}
	return s
}

func f2i(x float64) int64 {
	if x != x || x > 1e15 || x < -1e15 {
		return -7
	}
	return int64(x)
}

func run(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			switch e := r.(type) {
			case interface{ Error() string }:
				println(name, "panic:", e.Error())
			case string:
				println(name, "panic:", e)
			default:
				println(name, "panic:", describe(e))
			}
		}
	}()
	f()
}

var _ = math.MaxInt8
var _ = clip
var _ = strings.ToUpper
var _ = slices.Sort[[]int]
var _ = sort.Ints
'''


def lit_int(r, t, canon=False):
    v = r.choice(EDGE) if r.random() < 0.7 else r.randint(0, 50)
    bits = BITS[t]
    if t.startswith('u'):
        v &= (1 << bits) - 1
    else:
        v &= (1 << bits) - 1
        if v >= 1 << (bits - 1):
            v -= 1 << bits
        if r.random() < 0.3 and v != -(1 << (bits - 1)):
            v = -v
    if v < 0:
        return '%s(%d)' % (t, v)
    if not canon and r.random() < 0.3:
        return '%s(%s)' % (t, hex(v))
    return '%s(%d)' % (t, v)


class Gen:
    def __init__(self, seed):
        self.r = random.Random(seed)
        self.scopes = []
        self.nvar = 0
        self.readonly = set()

    # --- scopes ---------------------------------------------------------------
    def push(self):
        self.scopes.append([])

    def pop(self):
        self.scopes.pop()

    def vars(self, pred=lambda _: True, writable=False):
        out = []
        for s in self.scopes:
            for n, t in s:
                if pred(t) and not (writable and n in self.readonly):
                    out.append((n, t))
        return out

    def new(self, t):
        self.nvar += 1
        n = 'v%d' % self.nvar
        self.scopes[-1].append((n, t))
        return n

    # --- types ---------------------------------------------------------------
    def scalar(self):
        return self.r.choice(SCALARS)

    def any_type(self):
        k = self.r.random()
        if k < 0.55:
            return self.scalar()
        if k < 0.65:
            return '[%d]%s' % (self.r.randint(1, 4), self.r.choice(INT_TYPES + ['string', 'float64']))
        if k < 0.8:
            return '[]' + self.r.choice(INT_TYPES + ['string', 'float32', 'float64', 'bool'])
        if k < 0.9:
            return 'map[%s]%s' % (self.r.choice(['int', 'string', 'int8', 'uint16', 'float64']),
                                  self.r.choice(['int', 'string', 'uint8', 'float64']))
        return self.r.choice(['Rect', 'Pair[string, int]', 'Pair[int8, float64]', '*Circle', 'MyInt', 'Named'])

    # --- expressions -----------------------------------------------------------
    def expr(self, t, d=0):
        r = self.r
        cands = self.vars(lambda x: x == t)
        if cands and (d > 2 or r.random() < 0.4):
            return r.choice(cands)[0]
        if d > 3:
            return self.opaque(t)
        if t in INT_TYPES:
            return self.int_expr(t, d)
        if t in FLOAT_TYPES:
            return self.float_expr(t, d)
        if t == 'bool':
            return self.bool_expr(d)
        if t == 'string':
            return self.string_expr(d)
        if t == 'MyInt':
            return r.choice(['MyInt(%s)' % self.expr('int', d + 1), '%s.Double()' % self.expr('MyInt', d + 1)])
        return self.literal(t)

    def opaque(self, t):
        """A literal hidden from the constant folder (mostly): constant
        operands would make overflow and division by zero compile errors."""
        if self.r.random() < 0.0:
            return self.literal(t)
        return 'id(%s)' % self.literal(t)

    def literal(self, t, canon=False):
        r = self.r
        if t in INT_TYPES:
            return lit_int(r, t, canon)
        if t in FLOAT_TYPES:
            f = r.choice(FLOATS if t == 'float64' else [x for x in FLOATS if abs(float(x)) < 3.4e38])
            return '%s(%s)' % (t, f) if not f.startswith('-') else '%s(%s)' % (t, f)
        if t == 'bool':
            return r.choice(['true', 'false'])
        if t == 'string':
            return r.choice(STRINGS)
        if t.startswith('['):
            if t.startswith('[]'):
                el = t[2:]
                if r.random() < 0.15:
                    return t + '(nil)' if r.random() < 0.5 else t + '{}'
                return t + '{' + ', '.join(self.literal(el) for _ in range(r.randint(0, 5))) + '}'
            n = int(t[1:t.index(']')])
            el = t[t.index(']') + 1:]
            return t + '{' + ', '.join(self.literal(el) for _ in range(r.randint(0, n))) + '}'
        if t.startswith('map['):
            k, v = t[4:].split(']', 1)
            if r.random() < 0.1:
                return 'map[%s]%s(nil)' % (k, v)
            keys = []
            for _ in range(r.randint(0, 4)):
                kk = self.literal(k, canon=True)
                if kk not in keys:
                    keys.append(kk)
            return t + '{' + ', '.join('%s: %s' % (kk, self.literal(v)) for kk in keys) + '}'
        if t == 'Rect':
            return 'Rect{W: %s, H: %s}' % (self.literal('float64'), self.literal('float64'))
        if t == 'Named':
            return 'Named{Rect: %s, Label: %s}' % (self.literal('Rect'), self.literal('string'))
        if t == '*Circle':
            return '&Circle{R: %s}' % self.literal('float64') if r.random() < 0.85 else '(*Circle)(nil)'
        if t == 'MyInt':
            return 'MyInt(%s)' % lit_int(r, 'int')
        if t.startswith('Pair['):
            k, v = t[5:-1].split(', ')
            return '%s{Key: %s, Val: %s}' % (t, self.literal(k), self.literal(v))
        raise ValueError(t)

    def int_expr(self, t, d):
        r = self.r
        k = r.random()
        if k < 0.15:
            return self.opaque(t)
        if k < 0.5:
            op = r.choice(['+', '-', '*', '/', '%', '&', '|', '^', '&^', '+', '-', '*'])
            if op in '/%' and r.random() < 0.85:
                return '(%s %s nz(%s))' % (self.expr(t, d + 1), op, self.expr(t, d + 1))
            return '(%s %s %s)' % (self.expr(t, d + 1), op, self.expr(t, d + 1))
        if k < 0.6:
            op = r.choice(['<<', '>>'])
            st = r.choice(['uint8', 'uint', 'int', 'uint64'])
            return '(%s %s (%s & 127))' % (self.expr(t, d + 1), op, self.expr(st, d + 1))
        if k < 0.72:
            src = r.choice(INT_TYPES)
            return '%s(%s)' % (t, self.expr(src, d + 1))
        if k < 0.77:
            return '%s(f2i(float64(%s)))' % (t, self.expr(r.choice(FLOAT_TYPES), d + 1))
        if k < 0.82:
            return '%s(%s)' % (r.choice(['-', '^', '+']), self.expr(t, d + 1))
        if k < 0.88:
            sl = self.vars(lambda x: x.startswith('[') or x == 'string')
            if sl:
                return '%s(id(len(%s)))' % (t, r.choice(sl)[0])
            return self.opaque(t)
        if k < 0.92:
            return 'gSum(%s)' % ', '.join(self.expr(t, d + 1) for _ in range(r.randint(1, 3))) \
                if r.random() < 0.5 else 'gSum[%s]()' % t
        if k < 0.96:
            return 'gMax(%s, %s)' % (self.expr(t, d + 1), self.expr(t, d + 1))
        # index into a slice/array of this element type
        sl = self.vars(lambda x: x.startswith('[') and x.endswith(']' + t))
        if sl:
            seq = r.choice(sl)[0]
            return '%s[%s]' % (seq, self.index_expr(d, seq))
        return self.opaque(t)

    def index_expr(self, d, seq=None):
        r = self.r
        if seq and r.random() < 0.8:
            return 'ix(%s, len(%s))' % (self.expr('int', d + 1), seq)
        if r.random() < 0.8:
            return 'id(%d)' % r.randint(0, 3)
        return 'int(%s & 7)' % self.expr('uint8', d + 1)

    def float_expr(self, t, d):
        r = self.r
        k = r.random()
        if k < 0.2:
            return self.opaque(t)
        if k < 0.6:
            op = r.choice(['+', '-', '*', '/'])
            return '(%s %s %s)' % (self.expr(t, d + 1), op, self.expr(t, d + 1))
        if k < 0.75:
            return '%s(%s)' % (t, self.expr(r.choice(INT_TYPES + FLOAT_TYPES), d + 1))
        if k < 0.85:
            fn = r.choice(['math.Floor', 'math.Sqrt', 'math.Abs', 'math.Trunc', 'math.Round'])
            return '%s(math.%s(float64(%s)))' % (t, fn.split('.')[1], self.expr(t, d + 1))
        if k < 0.9:
            return '-(%s)' % self.expr(t, d + 1)
        if k < 0.95:
            return 'gMax(%s, %s)' % (self.expr(t, d + 1), self.expr(t, d + 1))
        return 'gSum(%s, %s)' % (self.expr(t, d + 1), self.expr(t, d + 1))

    def bool_expr(self, d):
        r = self.r
        k = r.random()
        if k < 0.1:
            return self.opaque('bool')
        if k < 0.55:
            t = r.choice(SCALARS[:-1] + ['string'])
            if t == 'bool':
                t = 'int'
            op = r.choice(['==', '!=', '<', '<=', '>', '>='])
            return '(%s %s %s)' % (self.expr(t, d + 1), op, self.expr(t, d + 1))
        if k < 0.75:
            return '(%s %s %s)' % (self.expr('bool', d + 1), r.choice(['&&', '||']), self.expr('bool', d + 1))
        if k < 0.85:
            return '!' + self.expr('bool', d + 1)
        if k < 0.92:
            return 'strings.Contains(%s, %s)' % (self.expr('string', d + 1), self.expr('string', d + 1))
        return '(%s == %s)' % (self.literal('Rect'), self.expr('Rect', d + 1)) if r.random() < 0.5 \
            else '(any(%s) == any(%s))' % (self.expr('int', d + 1), self.expr('int64', d + 1))

    def string_expr(self, d):
        r = self.r
        k = r.random()
        if k < 0.2:
            return self.opaque('string')
        if k < 0.4:
            return '(%s + %s)' % (self.expr('string', d + 1), self.expr('string', d + 1))
        if k < 0.5:
            return 'strconv.Itoa(int(%s))' % self.expr(r.choice(INT_TYPES), d + 1)
        if k < 0.55:
            return 'strconv.FormatFloat(float64(%s), %s, %d, %d)' % (
                self.expr(r.choice(FLOAT_TYPES), d + 1), r.choice(["'g'", "'e'", "'f'", "'x'"]),
                r.randint(-1, 8), r.choice([32, 64]))
        if k < 0.62:
            return 'string(rune(%s))' % self.expr(r.choice(['int32', 'int', 'uint16', 'int64']), d + 1)
        if k < 0.7:
            return '%s[%d:]' % (self.expr('string', d + 1), r.randint(0, 3))
        if k < 0.8:
            fn = r.choice(['strings.ToUpper', 'strings.ToLower', 'strings.TrimSpace', 'strconv.Quote',
                           'strings.Title', 'strconv.QuoteToASCII'])
            if fn == 'strings.Title':
                fn = 'strings.ToTitle'
            return '%s(%s)' % (fn, self.expr('string', d + 1))
        if k < 0.85:
            return 'strings.Repeat(%s, %d)' % (self.expr('string', d + 1), r.randint(0, 3))
        if k < 0.9:
            return 'describe(%s)' % self.expr(r.choice(['int', 'string', 'float64', 'MyInt', 'Rect', '*Circle', 'bool', 'uint8']), d + 1)
        if k < 0.95:
            return 'strings.Join(%s, %s)' % (self.expr('[]string', d + 1), self.literal('string'))
        return 'string(%s)' % self.expr('[]byte' if False else '[]uint8', d + 1) if self.vars(lambda x: x == '[]uint8') \
            else self.opaque('string')

    # --- printing ----------------------------------------------------------------
    def dump(self, n, t, ind):
        p = '\t' * ind
        if t in SCALARS or t == 'MyInt':
            if t == 'string':
                return [p + 'println("%s", strconv.Quote(%s))' % (n, n)]
            if t == 'uintptr':
                return [p + 'println("%s", uint64(%s))' % (n, n)]
            return [p + 'println("%s", %s)' % (n, n)]
        if t.startswith('[]') or t.startswith('['):
            el = t[t.index(']') + 1:]
            # len only: cap after append depends on escape analysis under gc (a non-escaping
            # append gets a 32-byte stack buffer, BUG-0027), which the spec leaves open.
            out = [p + 'println("%s len", len(%s), %s == nil)' % (n, n, n)] if t.startswith('[]') else []
            elp = 'strconv.Quote(x)' if el == 'string' else ('uint64(x)' if el == 'uintptr' else 'x')
            out.append(p + 'for i, x := range %s {' % n)
            out.append(p + '\tprintln("%s", i, %s)' % (n, elp))
            out.append(p + '}')
            return out
        if t.startswith('map['):
            k, v = t[4:].split(']', 1)
            kp = 'strconv.Quote(k)' if k == 'string' else 'k'
            vp = 'strconv.Quote(%s[k])' % n if v == 'string' else '%s[k]' % n
            return [p + 'println("%s len", len(%s), %s == nil)' % (n, n, n),
                    p + '{',
                    p + '\tkeys := make([]%s, 0, len(%s))' % (k, n),
                    p + '\tfor k := range %s {' % n,
                    p + '\t\tkeys = append(keys, k)',
                    p + '\t}',
                    p + '\tslices.Sort(keys)',
                    p + '\tfor _, k := range keys {',
                    p + '\t\tprintln("%s", %s, %s)' % (n, kp, vp),
                    p + '\t}',
                    p + '}']
        if t == 'Rect':
            return [p + 'println("%s", %s.W, %s.H, %s.Area())' % (n, n, n, n)]
        if t == 'Named':
            return [p + 'println("%s", %s.W, %s.H, strconv.Quote(%s.Label), %s.Name(), %s.Area())' % ((n,) * 6)]
        if t == '*Circle':
            return [p + 'if %s != nil {' % n, p + '\tprintln("%s", %s.R)' % (n, n), p + '} else {',
                    p + '\tprintln("%s nil")' % n, p + '}']
        if t.startswith('Pair['):
            k, v = t[5:-1].split(', ')
            kp = 'strconv.Quote(%s.Key)' % n if k == 'string' else '%s.Key' % n
            return [p + 'println("%s", %s, %s.Val)' % (n, kp, n)]
        raise ValueError(t)

    # --- statements ----------------------------------------------------------------
    def stmt(self, ind, d):
        r = self.r
        k = r.random()
        if k < 0.22:
            return self.decl(ind)
        if k < 0.42:
            return self.assign(ind)
        if d < 3 and k < 0.52:
            return self.if_stmt(ind, d)
        if d < 3 and k < 0.62:
            return self.for_stmt(ind, d)
        if d < 3 and k < 0.67:
            return self.switch_stmt(ind, d)
        if k < 0.76:
            return self.container_stmt(ind)
        if k < 0.81:
            return self.closure_stmt(ind)
        if k < 0.86:
            return self.iface_stmt(ind)
        if k < 0.9:
            return self.generic_stmt(ind)
        if k < 0.93:
            return self.print_stmt(ind)
        return self.extra_stmt(ind)

    def extra_stmt(self, ind):
        """Self-contained blocks for areas the expression grammar does not reach:
        complex arithmetic, channels and goroutines (deterministic hand-offs),
        defers on named results, float/interface map keys, slice-to-array
        conversions, struct embedding. Each prints what it computed."""
        r = self.r
        p = '\t' * ind
        k = r.randrange(6)
        if k == 0:
            ct = r.choice(['complex64', 'complex128'])
            a, b = self.expr('float64'), self.expr('float64')
            c, d = self.expr('float64'), self.expr('float64')
            op = r.choice(['*', '/', '+', '-'])
            body = ['x := %s(complex(%s, %s))' % (ct, a, b), 'y := %s(complex(%s, %s))' % (ct, c, d),
                    'z := x %s y' % op, 'println("cplx", z, real(z), imag(z), z == x, x*x %s y)' % op]
        elif k == 1:
            t = r.choice(['int', 'int8', 'uint32', 'string', 'float64'])
            vals = [self.expr(t) for _ in range(r.randint(1, 4))]
            pr = 'strconv.Quote(v)' if t == 'string' else 'v'
            # The values are computed before the goroutine starts: a panicking operand
            # must not race the receiver's output.
            body = ['vs := []%s{%s}' % (t, ', '.join(vals)),
                    'ch := make(chan %s, %d)' % (t, r.randint(0, 3)), 'done := make(chan int)',
                    'go func() {', '\tn := 0', '\tfor v := range ch {', '\t\tprintln("recv", %s)' % pr,
                    '\t\tn++', '\t}', '\tdone <- n', '}()']
            body += ['for _, v := range vs {', '\tch <- v', '}']
            body += ['close(ch)', 'println("received", <-done)', 'v, ok := <-ch',
                     'println("closed", %s, ok)' % pr]
        elif k == 2:
            t = r.choice(['int', 'int16', 'uint64', 'string'])
            e1, e2 = self.expr(t), self.expr(t)
            pr = 'strconv.Quote(%s)' if t == 'string' else '%s'
            body = ['f := func(a %s) (res %s, n int) {' % (t, t),
                    '\tdefer func() {', '\t\tif r := recover(); r != nil {', '\t\t\tn = -1', '\t\t}',
                    '\t\tres += a', '\t\tn++', '\t}()',
                    '\tfor i := 0; i < 2; i++ {', '\t\tdefer func() { n += i }()', '\t}',
                    '\tif a == %s {' % e2, '\t\tpanic("eq")', '\t}', '\treturn a, 10', '}',
                    'r1, n1 := f(%s)' % e1, 'println("named", %s, n1)' % (pr % 'r1')]
        elif k == 3:
            xs = [self.expr('float64') for _ in range(r.randint(1, 4))]
            body = ['m := map[float64]int{}', 'mi := map[any]int{}']
            for i, x in enumerate(xs):
                body += ['m[%s] += %d' % (x, i + 1), 'mi[%s] = %d' % (x, i), 'mi[int64(%d)] = %d' % (i, i)]
            body += ['m[math.Copysign(0, -1)] += 100', 'm[0] += 1000', 'm[math.NaN()] = 7',
                     'keys := make([]float64, 0, len(m))', 'nans := 0', 'for k := range m {',
                     '\tif k != k {', '\t\tnans++', '\t\tcontinue', '\t}', '\tkeys = append(keys, k)',
                     '}', 'println("nans", nans)', 'slices.Sort(keys)', 'for _, k := range keys {',
                     '\tprintln("fkey", k, math.Signbit(k), m[k])', '}', 'println("ikeys", len(mi), len(m))']
        elif k == 4:
            t = r.choice(['int', 'uint8', 'string', 'float32'])
            vals = ', '.join(self.expr(t) for _ in range(r.randint(0, 5)))
            n = r.randint(0, 4)
            pr = 'strconv.Quote(a[i])' if t == 'string' else 'a[i]'
            body = ['s := []%s{%s}' % (t, vals), 'a := [%d]%s(s[ix(%s, len(s)+1):])' % (n, t, self.expr('int')),
                    'p := (*[%d]%s)(s)' % (min(n, 1), t), 'for i := range a {', '\tprintln("arr", i, %s)' % pr,
                    '}', 'println("aptr", p != nil, len(s))']
        else:
            body = ['type inner struct{ a, b int }', 'type outer struct {', '\tinner', '\tb string', '\tR Rect',
                    '}', 'o := outer{inner{%s, %s}, %s, %s}' % (self.expr('int'), self.expr('int'),
                                                             self.expr('string'), self.literal('Rect')),
                    'o2 := o', 'o2.a++', 'o2.inner.b--', 'o2.R.W *= 2',
                    'println("embed", o.a, o.inner.b, strconv.Quote(o.b), o.R.Area(), o2.a, o2.inner.b, o == o2, o.inner == inner{o2.a - 1, o2.inner.b + 1})']
        return [p + '{'] + [p + '\t' + l for l in body] + [p + '}']

    def decl(self, ind):
        t = self.any_type()
        e = self.expr(t) if t in SCALARS or t == 'MyInt' else self.literal(t)
        n = self.new(t)
        p = '\t' * ind
        if self.r.random() < 0.5 and t in SCALARS:
            return [p + 'var %s %s = %s' % (n, t, e), p + '_ = %s' % n]
        return [p + '%s := %s' % (n, e) if not t.startswith('Pair') else p + 'var %s %s = %s' % (n, t, e),
                p + '_ = %s' % n]

    def assign(self, ind):
        r = self.r
        p = '\t' * ind
        vs = self.vars(lambda t: t in SCALARS or t == 'MyInt', writable=True)
        if not vs:
            return self.decl(ind)
        n, t = r.choice(vs)
        if t in INT_TYPES and r.random() < 0.4:
            op = r.choice(['+=', '-=', '*=', '|=', '&=', '^=', '<<=', '>>=', '/=', '%=', '++', '--'])
            if op in ('++', '--'):
                return [p + n + op]
            if op in ('<<=', '>>='):
                return [p + '%s %s %s' % (n, op, r.randint(0, 70))]
            return [p + '%s %s %s' % (n, op, self.expr(t))]
        if t == 'string':
            # clip: a string assigned from itself in nested loops grows
            # geometrically (gc needs gigabytes too); a generator issue, not a finding.
            if r.random() < 0.3:
                return [p + '%s = clip(%s + %s)' % (n, n, self.expr('string'))]
            return [p + '%s = clip(%s)' % (n, self.expr('string'))]
        if r.random() < 0.15:
            vs2 = [v for v in vs if v[1] == t and v[0] != n]
            if vs2:
                m = r.choice(vs2)[0]
                return [p + '%s, %s = %s, %s' % (n, m, m, n)]
        return [p + '%s = %s' % (n, self.expr(t))]

    def block(self, ind, d, nstmts):
        self.push()
        out = []
        for _ in range(nstmts):
            out += self.stmt(ind, d + 1)
        self.pop()
        return out

    def if_stmt(self, ind, d):
        p = '\t' * ind
        out = [p + 'if %s {' % self.expr('bool')]
        out += self.block(ind + 1, d, self.r.randint(1, 3))
        if self.r.random() < 0.5:
            out.append(p + '} else {')
            out += self.block(ind + 1, d, self.r.randint(1, 3))
        out.append(p + '}')
        return out

    def for_stmt(self, ind, d):
        r = self.r
        p = '\t' * ind
        self.push()
        k = r.random()
        label = None
        if k < 0.35:
            i = self.new('int')
            self.readonly.add(i)
            out = [p + 'for %s := 0; %s < %d; %s++ {' % (i, i, r.randint(0, 5), i)]
        elif k < 0.55:
            i = self.new('int')
            self.readonly.add(i)
            out = [p + 'for %s := range %d {' % (i, r.randint(0, 5)), p + '\t_ = %s' % i]
        elif k < 0.8:
            sl = self.vars(lambda t: t.startswith('[') or t == 'string')
            if not sl:
                self.pop()
                return self.decl(ind)
            n, t = r.choice(sl)
            i = self.new('int')
            el = 'int32' if t == 'string' else t[t.index(']') + 1:]
            x = self.new(el)
            self.readonly.update([i, x])
            out = [p + 'for %s, %s := range %s {' % (i, x, n), p + '\t_, _ = %s, %s' % (i, x)]
        else:
            label = 'L%d' % self.nvar
            i = self.new('int')
            self.readonly.add(i)
            out = [p + '%s:' % label, p + 'for %s := 0; %s < %d; %s++ {' % (i, i, r.randint(1, 4), i)]
        body = self.block(ind + 1, d, r.randint(1, 3))
        if label:
            j = 'j%d' % self.nvar
            body = [p + '\tfor %s := range 3 {' % j,
                    p + '\t\tif %s == %d {' % (j, r.randint(0, 2)),
                    p + '\t\t\t%s %s' % (r.choice(['break', 'continue']), label),
                    p + '\t\t}'] + ['\t' + l for l in body] + [p + '\t}']
        out += body
        if r.random() < 0.2 and not label:
            out.insert(len(out), p + '\tif %s {' % self.expr('bool', 2))
            out.append(p + '\t\t%s' % r.choice(['break', 'continue']))
            out.append(p + '\t}')
        out.append(p + '}')
        self.pop()
        return out

    def switch_stmt(self, ind, d):
        r = self.r
        p = '\t' * ind
        t = r.choice(['int', 'int8', 'uint16', 'string'])
        out = [p + 'switch %s {' % self.expr(t)]
        used = set()
        for c in range(r.randint(1, 3)):
            v = self.literal(t, canon=True)
            if v in used:
                continue
            used.add(v)
            out.append(p + 'case %s:' % v)
            out += self.block(ind + 1, d, r.randint(1, 2))
            if r.random() < 0.2 and c < 2:
                out.append(p + '\tfallthrough')
        out.append(p + 'default:')
        out += self.block(ind + 1, d, 1)
        out.append(p + '}')
        # a trailing fallthrough before default is legal; one into nothing is not
        return out

    def container_stmt(self, ind):
        r = self.r
        p = '\t' * ind
        vs = self.vars(lambda t: t.startswith('[') or t.startswith('map['), writable=True)
        if not vs:
            return self.decl(ind)
        n, t = r.choice(vs)
        if t.startswith('[]'):
            el = t[2:]
            k = r.random()
            if k < 0.4:
                return [p + '%s = append(%s, %s)' % (n, n, ', '.join(self.expr(el) for _ in range(r.randint(1, 3))))]
            if k < 0.55:
                return [p + '%s[%s] = %s' % (n, self.index_expr(0, n), self.expr(el))]
            if k < 0.65:
                a, b = r.randint(0, 2), r.randint(0, 4)
                # Bounded by len: past len the result depends on cap, which depends on
                # gc's escape analysis (BUG-0027); the panic text names cap too.
                return [p + '%s = %s[min(%d, len(%s)):min(%d, len(%s))]' % (n, n, min(a, b), n, max(a, b), n)]
            if k < 0.72:
                m = self.new(t)
                return [p + '%s := slices.Clone(%s)' % (m, n), p + '_ = %s' % m]
            if k < 0.8 and el != 'bool':
                return [p + 'slices.Sort(%s)' % n]
            if k < 0.87:
                return [p + 'copy(%s, %s[%d:])' % (n, n, r.randint(0, 2))]
            if k < 0.93:
                m = self.new(t)
                return [p + '%s := gFilter(%s, func(x %s) bool { return %s })' % (
                    m, n, el, '%s(x) %% 2 == 0' % 'int' if el in INT_TYPES else 'true'),
                    p + '_ = %s' % m]
            return [p + 'slices.Reverse(%s)' % n]
        if t.startswith('['):
            el = t[t.index(']') + 1:]
            if r.random() < 0.5:
                e = self.expr(el)
                m = self.new(t)
                return [p + '%s := %s' % (m, n), p + '%s[0] = %s' % (m, e), p + '_ = %s' % m]
            return [p + '%s[%s] = %s' % (n, self.index_expr(0, n), self.expr(el))]
        k_, v = t[4:].split(']', 1)
        if r.random() < 0.6:
            return [p + '%s[%s] = %s' % (n, self.expr(k_), self.expr(v))]
        if r.random() < 0.5:
            return [p + 'delete(%s, %s)' % (n, self.expr(k_))]
        if r.random() < 0.3:
            return [p + 'clear(%s)' % n]
        e = self.expr(k_)
        m = self.new(v)
        ok = self.new('bool')
        return [p + '%s, %s := %s[%s]' % (m, ok, n, e), p + '_, _ = %s, %s' % (m, ok)]

    def closure_stmt(self, ind):
        r = self.r
        p = '\t' * ind
        vs = self.vars(lambda t: t in INT_TYPES, writable=True)
        if not vs:
            return self.decl(ind)
        n, t = r.choice(vs)
        f = 'c%d' % self.nvar
        self.nvar += 1
        out = [p + '%s := func(k %s) %s {' % (f, t, t),
               p + '\t%s += k' % n,
               p + '\treturn %s * k' % n,
               p + '}']
        e1, e2 = self.expr(t), self.expr(t)
        res = self.new(t)
        out.append(p + '%s := %s(%s) + %s(%s)' % (res, f, e1, f, e2))
        out.append(p + '_ = %s' % res)
        if r.random() < 0.5:
            out += [p + 'func() {', p + '\tdefer func() { %s++ }()' % n, p + '\t%s *= 3' % n, p + '}()']
        return out

    def iface_stmt(self, ind):
        r = self.r
        p = '\t' * ind
        k = r.random()
        if k < 0.4:
            e = self.expr(r.choice(['Rect', '*Circle', 'Named']))
            s = self.new('string')
            return [p + 'var sh%d Shape = %s' % (self.nvar, e),
                    p + '%s := sh%d.Name() + strconv.FormatFloat(sh%d.Area(), \'g\', 5, 64)' % (s, self.nvar, self.nvar),
                    p + '_ = %s' % s]
        if k < 0.7:
            e = self.expr(r.choice(['int', 'int64', 'MyInt', 'string']))
            s = self.new('int')
            return [p + 'var a%d any = %s' % (self.nvar, e),
                    p + '%s := a%d.(int)' % (s, self.nvar) if r.random() < 0.5 else
                    p + '%s, _ := a%d.(int)' % (s, self.nvar), p + '_ = %s' % s]
        e = self.expr(r.choice(['int', 'string', 'MyInt', 'Rect']))
        s = self.new('bool')
        return [p + '%s := any(%s) == any(%s)' % (s, e, self.expr('int')), p + '_ = %s' % s]

    def generic_stmt(self, ind):
        r = self.r
        p = '\t' * ind
        k = r.random()
        if k < 0.4:
            t = r.choice(INT_TYPES + ['string', 'float64'])
            pushes = [self.expr(t) for _ in range(r.randint(0, 4))]
            st = self.new('int')
            sn = 'st%d' % self.nvar
            out = [p + 'var %s Stack[%s]' % (sn, t)]
            for e in pushes:
                out.append(p + '%s.Push(%s)' % (sn, e))
            out.append(p + 'if x, ok := %s.Pop(); ok {' % sn)
            out.append(p + '\tprintln("pop", %s)' % ('strconv.Quote(x)' if t == 'string' else
                                                       'uint64(x)' if t == 'uintptr' else 'x'))
            out.append(p + '}')
            out.append(p + '%s := %s.Len()' % (st, sn))
            out.append(p + '_ = %s' % st)
            return out
        vs = self.vars(lambda t: t.startswith('[]') and t[2:] in INT_TYPES)
        if vs:
            n, t = r.choice(vs)
            m = self.new('[]string')
            return [p + '%s := gMap(%s, func(x %s) string { return strconv.FormatUint(uint64(x), %d) })' % (
                m, n, t[2:], r.choice([2, 10, 16, 36])), p + '_ = %s' % m]
        return self.decl(ind)

    def print_stmt(self, ind):
        vs = self.vars()
        if not vs:
            return []
        n, t = self.r.choice(vs)
        return self.dump(n, t, ind)

    def func(self, idx):
        self.push()
        nst = self.r.randint(4, 14)
        body = []
        for _ in range(nst):
            body += self.stmt(1, 0)
        live = list(self.scopes[0])
        for n, t in live:
            body += self.dump(n, t, 1)
        self.pop()
        self.readonly.clear()
        return ['func t%d() {' % idx] + body + ['}']

    def program(self):
        out = ['// Code generated by test/fuzz/gen.py; DO NOT EDIT.', '',
               'package main', '', 'import (', '\t"cmp"', '\t"math"', '\t"slices"', '\t"sort"',
               '\t"strconv"', '\t"strings"', ')', PRELUDE]
        n = self.r.randint(3, 6)
        for i in range(n):
            out += self.func(i) + ['']
        out.append('func main() {')
        for i in range(n):
            out.append('\trun("t%d", t%d)' % (i, i))
        out.append('}')
        return '\n'.join(out) + '\n'


def generate(seed):
    return Gen(seed).program()


if __name__ == '__main__':
    sys.stdout.write(generate(int(sys.argv[1]) if len(sys.argv) > 1 else 1))
