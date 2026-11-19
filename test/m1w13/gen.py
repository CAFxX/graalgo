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
        sys.exit("gen.py: the access program is not generated yet")


if __name__ == "__main__":
    main()
