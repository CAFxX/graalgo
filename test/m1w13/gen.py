#!/usr/bin/env python3
"""Generator of the W13 pointer-model corpus (ADR-0015, "gates that must be
able to fail"): a deterministic set of struct types and, from it,

  layout    a Go program that prints gc's own layout of every type — size,
            alignment and the byte offset and kind of every leaf — from
            unsafe.Sizeof/Alignof/Offsetof, which gc folds at compile time.
            Its output under gc is the committed fixture
            test/m1w13/layout_corpus.txt, which the backend's leaf tables are
            checked against (graalgo.go.types.LeafTableTest).
  access    a Go program for the differential gate (test/m1w13/run.sh): for
            every type and every addressable kind (escaping variable, package
            variable, struct field, array element, slice element) it loads
            1/2/4/8-byte scalars at every byte offset through
            unsafe.Pointer views, stores through them and reads the typed
            fields back, and loads and stores every pointer word.

Types are described in the fixture by a small grammar the Java test parses:
  b i8 u8 i16 u16 i32 u32 i64 u64 f32 f64 c64 c128 str ptr sl if   leaves
  [N]T                                                             array
  {T T ...}                                                        struct
(`{}` is the empty struct; `ptr` is *int64, `sl` is []int32, `if` is any.)

Usage: gen.py layout|access [--types N] [--seed S] > main.go
(the layout fixture is --types 300; the gate runs access with --types 60)
"""

import argparse
import random
import sys

LEAVES = {
    # tag: (Go type, gc size, kind in the fixture)
    "b": ("bool", 1, "bool"),
    "i8": ("int8", 1, "int"),
    "u8": ("uint8", 1, "uint"),
    "i16": ("int16", 2, "int"),
    "u16": ("uint16", 2, "uint"),
    "i32": ("int32", 4, "int"),
    "u32": ("uint32", 4, "uint"),
    "i64": ("int64", 8, "int"),
    "u64": ("uint64", 8, "uint"),
    "f32": ("float32", 4, "float"),
    "f64": ("float64", 8, "float"),
    "c64": ("complex64", 8, "complex"),
    "c128": ("complex128", 16, "complex"),
    "str": ("string", 16, "string"),
    "ptr": ("*int64", 8, "pointer"),
    "sl": ("[]int32", 24, "slice"),
    "if": ("any", 16, "iface"),
}
SCALARS = ["b", "i8", "u8", "i16", "u16", "i32", "u32", "i64", "u64", "f32", "f64"]
REFS = ["c64", "c128", "str", "ptr", "sl", "if"]


class T:
    """A type tree: kind 'leaf' (tag), 'array' (n, elem) or 'struct' (fields)."""

    def __init__(self, kind, tag=None, n=0, elem=None, fields=None):
        self.kind, self.tag, self.n, self.elem, self.fields = kind, tag, n, elem, fields or []

    def desc(self):
        if self.kind == "leaf":
            return self.tag
        if self.kind == "array":
            return "[%d]%s" % (self.n, self.elem.desc())
        return "{" + " ".join(f.desc() for f in self.fields) + "}"

    def go(self):
        if self.kind == "leaf":
            return LEAVES[self.tag][0]
        if self.kind == "array":
            return "[%d]%s" % (self.n, self.elem.go())
        return "struct{" + "; ".join("F%d %s" % (i, f.go()) for i, f in enumerate(self.fields)) + "}"

    def leaves(self, path="", off="0"):
        """(Go selector path, Go offset expression relative to the root, tag) per leaf."""
        if self.kind == "leaf":
            yield path, off, self.tag
        elif self.kind == "array":
            for i in range(self.n):
                yield from self.elem.leaves("%s[%d]" % (path, i), "%s+%d*unsafe.Sizeof(v%s[0])" % (off, i, path))
        else:
            for i, f in enumerate(self.fields):
                yield from f.leaves("%s.F%d" % (path, i), "%s+unsafe.Offsetof(v%s.F%d)" % (off, path, i))


def gen_type(rng, depth, refs=True):
    r = rng.random()
    if depth > 0 and r < 0.18:
        return T("array", n=rng.choice([0, 1, 2, 3]), elem=gen_type(rng, depth - 1, refs))
    if depth > 0 and r < 0.40:
        return gen_struct(rng, depth - 1, refs, rng.choice([0, 1, 2, 3, 4]))
    pool = SCALARS * 2 + (REFS if refs else [])
    return T("leaf", tag=rng.choice(pool))


def gen_struct(rng, depth, refs, n):
    return T("struct", fields=[gen_type(rng, depth, refs) for _ in range(n)])


def corpus(n, seed):
    rng = random.Random(seed)
    out = [
        # Hand-picked shapes first: padding, trailing zero-size fields, nesting.
        T("struct", fields=[T("leaf", tag="u8"), T("leaf", tag="i64"), T("leaf", tag="u8")]),
        T("struct", fields=[T("leaf", tag="i32"), T("struct")]),
        T("struct", fields=[T("struct"), T("leaf", tag="i32")]),
        T("struct", fields=[T("leaf", tag="u16"), T("array", n=0, elem=T("leaf", tag="i64"))]),
        T("struct", fields=[T("leaf", tag="b"), T("leaf", tag="c64"), T("leaf", tag="u8"), T("leaf", tag="c128")]),
        T("struct", fields=[T("array", n=3, elem=T("leaf", tag="u8")), T("leaf", tag="str"), T("leaf", tag="b")]),
        T("struct", fields=[T("leaf", tag="if"), T("leaf", tag="sl"), T("leaf", tag="ptr"), T("leaf", tag="f32")]),
        T("struct", fields=[T("array", n=2, elem=T("struct", fields=[T("leaf", tag="u8"), T("leaf", tag="u32")]))]),
    ]
    while len(out) < n:
        out.append(gen_struct(rng, 3, True, rng.randint(1, 6)))
    return out


def layout_program(types):
    w = ["// Code generated by test/m1w13/gen.py layout; DO NOT EDIT.", "package main", "", 'import "unsafe"', ""]
    for i, t in enumerate(types):
        w.append("type T%d %s" % (i, t.go()))
    w.append("")
    w.append("func main() {")
    for i, t in enumerate(types):
        w.append("\t{")
        w.append("\t\tvar v T%d" % i)
        w.append("\t\t_ = v")
        w.append('\t\tprint("%s ", unsafe.Sizeof(v), " ", unsafe.Alignof(v))' % t.desc())
        for path, off, tag in t.leaves():
            w.append('\t\tprint(" ", uintptr(%s), ":%s")' % (off, LEAVES[tag][2]))
        w.append("\t\tprintln()")
        w.append("\t}")
    w.append("}")
    return "\n".join(w) + "\n"


ACCESS_PRELUDE = """// Code generated by test/m1w13/gen.py access; DO NOT EDIT.
//
// Loads and stores of 1/2/4/8-byte scalars and of pointer words at every
// byte offset of every corpus type, through unsafe.Pointer views, for each
// addressable kind: an escaping variable, a package variable, a struct
// field, an array element, a slice element.
package main

import "unsafe"

type rng struct{ off, size uintptr }

func hit(rs []rng, o, w uintptr) bool {
	for _, r := range rs {
		if o < r.off+r.size && r.off < o+w {
			return true
		}
	}
	return false
}

// inside reports whether every byte of [o, o+w) is a byte of a scalar leaf.
// Padding is excluded: gc leaves the padding of a variable undefined (a stack
// variable's trailing padding is whatever the stack held).
func inside(scal []rng, o, w uintptr) bool {
	for b := o; b < o+w; b++ {
		if !hit(scal, b, 1) {
			return false
		}
	}
	return true
}

// loads sums every w-byte unsigned load that lies in scalar leaves, across
// leaf boundaries too.
func loads(p unsafe.Pointer, size uintptr, scal []rng) (h1, h2, h4, h8 uint64) {
	for o := uintptr(0); o < size; o++ {
		if inside(scal, o, 1) {
			h1 = h1*31 + uint64(*(*uint8)(unsafe.Add(p, o)))
		}
		if o+2 <= size && inside(scal, o, 2) {
			h2 = h2*31 + uint64(*(*uint16)(unsafe.Add(p, o)))
		}
		if o+4 <= size && inside(scal, o, 4) {
			h4 = h4*31 + uint64(*(*uint32)(unsafe.Add(p, o)))
		}
		if o+8 <= size && inside(scal, o, 8) {
			h8 = h8*31 + *(*uint64)(unsafe.Add(p, o))
		}
	}
	return
}

// pat is byte k of the store at offset o of width w: never 0, never above
// 0x7e, so no float leaf becomes a NaN (whose payload a float32 conversion
// may not preserve) and the top bit pattern still varies.
func pat(o, k, w uintptr) uint64 { return uint64(1 + (o*13+k*29+w)%0x7e) }

// stores writes a w-byte pattern at every offset that touches neither a
// pointer word nor a bool (a bool byte other than 0 and 1 is not a value).
func stores(p unsafe.Pointer, size uintptr, skip []rng, w uintptr) {
	for o := uintptr(0); o+w <= size; o++ {
		if hit(skip, o, w) {
			continue
		}
		var v uint64
		for k := uintptr(0); k < w; k++ {
			v |= pat(o, k, w) << (8 * k)
		}
		switch w {
		case 1:
			*(*uint8)(unsafe.Add(p, o)) = uint8(v)
		case 2:
			*(*uint16)(unsafe.Add(p, o)) = uint16(v)
		case 4:
			*(*uint32)(unsafe.Add(p, o)) = uint32(v)
		default:
			*(*uint64)(unsafe.Add(p, o)) = v
		}
	}
}

func b2u(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

func f32(f float32) uint64 { return uint64(*(*uint32)(unsafe.Pointer(&f))) }
func f64(f float64) uint64 { return *(*uint64)(unsafe.Pointer(&f)) }

var sentinel, sentinel2 int64 = 111, 222
var sentinelSlice = []int32{7, 8, 9}
"""


def scalar_value(tag, k):
    """A Go constant for scalar leaf number k of type tag."""
    if tag == "b":
        return "true" if k % 2 == 0 else "false"
    if tag == "i8":
        return str(-(k * 7 % 120) - 1)
    if tag == "u8":
        return str(130 + k * 11 % 120)
    if tag == "i16":
        return str(-(k * 911 % 30000) - 2)
    if tag == "u16":
        return str(40000 + k * 977 % 20000)
    if tag == "i32":
        return str(-(k * 100003 % 2000000000) - 3)
    if tag == "u32":
        return str(3000000000 + k * 100019 % 1000000000)
    if tag == "i64":
        return str(-(k * 1000000007 + 4) * 1000003)
    if tag == "u64":
        return str(0x8000000000000000 + k * 1000000009)
    if tag == "f32":
        return "%d.25" % (k + 1)
    if tag == "f64":
        return "-%d.125" % (k + 1)
    if tag == "c64":
        return "complex(%d.5, -%d.25)" % (k + 1, k + 2)
    if tag == "c128":
        return "complex(-%d.5, %d.75)" % (k + 3, k + 4)
    raise ValueError(tag)


def dump_expr(tag, x):
    """A uint64 Go expression of the bits of typed leaf x."""
    if tag == "b":
        return "b2u(%s)" % x
    if tag == "f32":
        return "f32(%s)" % x
    if tag == "f64":
        return "f64(%s)" % x
    if tag == "c64":
        return "f32(real(%s))*31 + f32(imag(%s))" % (x, x)
    if tag == "c128":
        return "f64(real(%s))*31 + f64(imag(%s))" % (x, x)
    return "uint64(%s)" % x


def access_program(types):
    w = [ACCESS_PRELUDE]
    for i, t in enumerate(types):
        w.append("type T%d %s" % (i, t.go()))
        w.append("")
        w.append("var g%d T%d" % (i, i))
        w.append("")
        leaves = list(t.leaves())
        # fill: typed assignments of every leaf.
        w.append("func fill%d(v *T%d) {" % (i, i))
        for k, (path, off, tag) in enumerate(leaves):
            x = "v" + path
            if tag == "ptr":
                w.append("\t%s = &sentinel" % x)
            elif tag == "str":
                w.append('\t%s = "s%d-%s"' % (x, k, "x" * (k % 5)))
            elif tag == "sl":
                w.append("\t%s = sentinelSlice[%d:]" % (x, k % 3))
            elif tag == "if":
                w.append("\t%s = %d" % (x, 1000 + k))
            else:
                w.append("\t%s = %s" % (x, scalar_value(tag, k)))
        w.append("}")
        w.append("")
        # dump: the bits of every scalar leaf, typed.
        w.append("func dump%d(v *T%d) (h uint64) {" % (i, i))
        for path, off, tag in leaves:
            if tag in ("ptr", "str", "sl", "if"):
                continue
            w.append("\th = h*31 + %s" % dump_expr(tag, "v" + path))
        w.append("\treturn")
        w.append("}")
        w.append("")
        w.append("func test%d(kind string, p unsafe.Pointer, v *T%d) {" % (i, i))
        w.append("\tsize := unsafe.Sizeof(*v)")
        w.append("\tvar refs, skip, scal []rng")
        for path, off, tag in leaves:
            if tag in ("ptr", "str", "sl", "if"):
                w.append("\trefs = append(refs, rng{%s, unsafe.Sizeof(v%s)})" % (off, path))
            else:
                w.append("\tscal = append(scal, rng{%s, unsafe.Sizeof(v%s)})" % (off, path))
            if tag == "b":
                w.append("\tskip = append(skip, rng{%s, 1})" % off)
        w.append("\tskip = append(skip, refs...)")
        w.append("\tfill%d(v)" % i)
        w.append("\th1, h2, h4, h8 := loads(p, size, scal)")
        w.append('\tprint("T%d ", kind, " load ", h1, " ", h2, " ", h4, " ", h8)' % i)
        # pointer words and headers, loaded through views.
        for k, (path, off, tag) in enumerate(leaves):
            at = "unsafe.Add(p, %s)" % off
            if tag == "ptr":
                w.append('\tprint(" p", *(*unsafe.Pointer)(%s) == unsafe.Pointer(&sentinel), **(**int64)(%s))' % (at, at))
            elif tag == "str":
                w.append('\tprint(" s", *(*string)(%s), *(*int)(unsafe.Add(%s, 8)), *(*unsafe.Pointer)(%s) == unsafe.Pointer(unsafe.StringData(v%s)))' % (at, at, at, path))
            elif tag == "sl":
                w.append('\tprint(" l", *(*int)(unsafe.Add(%s, 8)), *(*int)(unsafe.Add(%s, 16)), *(*unsafe.Pointer)(%s) == unsafe.Pointer(&v%s[0]), (*(*[]int32)(%s))[0])' % (at, at, at, path, at))
            elif tag == "if":
                w.append('\tprint(" i", *(*unsafe.Pointer)(%s) != nil, *(*int)(*(*unsafe.Pointer)(unsafe.Add(%s, 8))), (*(*any)(%s)).(int))' % (at, at, at))
        w.append("\tprintln()")
        w.append('\tprint("T%d ", kind, " store")' % i)
        w.append("\tfor _, w := range []uintptr{8, 4, 2, 1} {")
        w.append("\t\tstores(p, size, skip, w)")
        w.append('\t\tprint(" ", dump%d(v))' % i)
        w.append("\t}")
        # pointer-word stores.
        for path, off, tag in leaves:
            at = "unsafe.Add(p, %s)" % off
            if tag == "ptr":
                w.append("\t*(*unsafe.Pointer)(%s) = unsafe.Pointer(&sentinel2)" % at)
                w.append('\tprint(" p", v%s == &sentinel2, *v%s)' % (path, path))
            elif tag == "str":
                w.append('\t*(*string)(%s) = "stored"' % at)
                w.append("\t*(*int)(unsafe.Add(%s, 8)) = 3" % at)
                w.append('\tprint(" s", v%s)' % path)
            elif tag == "sl":
                w.append("\t*(*[]int32)(%s) = sentinelSlice" % at)
                w.append("\t*(*int)(unsafe.Add(%s, 8)) = 2" % at)
                w.append('\tprint(" l", len(v%s), cap(v%s), v%s[1])' % (path, path, path))
            elif tag == "if":
                w.append('\t*(*any)(%s) = "iface"' % at)
                w.append('\tprint(" i", v%s.(string))' % path)
        w.append("\tprintln()")
        w.append("}")
        w.append("")
        w.append("func run%d() {" % i)
        w.append("\t{\n\t\tvar v T%d\n\t\ttest%d(\"var\", unsafe.Pointer(&v), &v)\n\t}" % (i, i))
        w.append("\ttest%d(\"global\", unsafe.Pointer(&g%d), &g%d)" % (i, i, i))
        w.append("\t{\n\t\tvar s struct {\n\t\t\tpre  uint8\n\t\t\tv    T%d\n\t\t\tpost uint16\n\t\t}\n\t\ttest%d(\"field\", unsafe.Pointer(&s.v), &s.v)\n\t\tprintln(s.pre, s.post)\n\t}" % (i, i))
        w.append("\t{\n\t\tvar a [3]T%d\n\t\ttest%d(\"array\", unsafe.Pointer(&a[1]), &a[1])\n\t\tprintln(dump%d(&a[0]), dump%d(&a[2]))\n\t}" % (i, i, i, i))
        w.append("\t{\n\t\ts := make([]T%d, 3)\n\t\ttest%d(\"slice\", unsafe.Pointer(&s[1]), &s[1])\n\t\tprintln(dump%d(&s[0]), dump%d(&s[2]))\n\t}" % (i, i, i, i))
        w.append("}")
        w.append("")
    w.append("func main() {")
    for i, t in enumerate(types):
        w.append("\trun%d()" % i)
    w.append("}")
    return "\n".join(w) + "\n"


def sized(t):
    """Whether a value of the type has at least one leaf (a zero-size type has nothing to access)."""
    return any(True for _ in t.leaves())


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("mode", choices=["layout", "access"])
    ap.add_argument("--types", type=int, default=300)
    ap.add_argument("--seed", type=int, default=13)
    a = ap.parse_args()
    types = corpus(a.types, a.seed)
    if a.mode == "layout":
        sys.stdout.write(layout_program(types))
    else:
        sys.stdout.write(access_program([t for t in types if sized(t)]))


if __name__ == "__main__":
    main()
