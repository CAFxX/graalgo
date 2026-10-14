#!/usr/bin/env python3
"""Seeded differential-program generator for the graalgo fuzzing campaign.

Generates deterministic, panic-free (by construction), scope-safe Go
programs. Usage: generator.py --seed N [--out file.go]

Design notes (see ../DESIGN.md):
- Every program is valid Go (gc must build it; else generator bug).
- Output is deterministic: map keys sorted before print, no time/rand,
  no goroutines in gen-1.
- No panics by construction: guarded divisors, modulo-reduced indices,
  maps always made. Any graalgo panic is a bug signal.
- Scope-safe: no range-over-int, no embedded structs, no generics.
- Integer literal pool biased at int/uint boxing edges (0, maxint32,
  minint32, maxuint32, ...) to stress the Integer/Long confusion class.
- Lexical scoping is tracked: block-local vars never leak, every
  declared var is "used" via a trailing `_ =` per scope.
"""
import argparse
import random
import sys

INT_TYPES = ["int", "int8", "int16", "int32", "int64",
             "uint", "uint8", "uint16", "uint32", "uint64"]
FLOAT_TYPES = ["float32", "float64"]

# Per-type edge literals targeting the boxing bugs (a2e3f7b / 487f48c /
# f05255d). CRITICAL: every literal must fit in its type — Go rejects
# constant expressions that overflow at compile time.
TYPE_LITERALS = {
    "int": ["0", "1", "-1", "42", "127", "128", "32767", "32768",
            "2147483647", "-2147483648", "4294967295",
            "9223372036854775807", "-9223372036854775808"],
    "int8": ["0", "1", "-1", "42", "100", "127", "-128"],
    "int16": ["0", "1", "-1", "42", "1000", "32767", "-32768"],
    "int32": ["0", "1", "-1", "42", "2147483647", "-2147483648"],
    "int64": ["0", "1", "-1", "42", "9223372036854775807",
              "-9223372036854775808"],
    "uint": ["0", "1", "2", "42", "255", "65535", "4294967295",
             "18446744073709551615"],
    "uint8": ["0", "1", "2", "42", "127", "128", "255"],
    "uint16": ["0", "1", "2", "42", "255", "32767", "65535"],
    "uint32": ["0", "1", "2", "42", "65535", "2147483647", "2147483648",
               "4294967295"],
    "uint64": ["0", "1", "2", "42", "4294967295", "18446744073709551615"],
}
FLOAT_LITERALS = ["0.0", "1.5", "-2.25", "3.14159", "1e10", "0.1"]
STRING_LITERALS = ['""', '"a"', '"hello"', '"foo bar"', '"\\n\\t"']
BOOL_LITERALS = ["true", "false"]


def is_uint(t):
    return t.startswith("uint")


def is_int(t):
    return t in INT_TYPES


class Gen:
    def __init__(self, seed):
        self.rng = random.Random(seed)
        self.seed = seed
        self.scopes = [{}]      # stack of {name: type}; names unique program-wide
        self.var_counter = 0
        self.funcs = []         # (name, [(pname, ptype)], retype)
        self.structs = {}       # name -> [(field, type)]
        self.struct_counter = 0
        self.need_sorted_keys = set()
        self.pending_structs = []
        self.loop_vars = set()  # loop variables: never assigned in body

    def r(self):
        return self.rng.random()

    def pick(self, xs):
        return self.rng.choice(xs)

    def rint(self, a, b):
        return self.rng.randint(a, b)

    # ---- scope management ----
    def push_scope(self):
        self.scopes.append({})

    def pop_scope(self):
        return self.scopes.pop()

    def visible_vars(self):
        merged = {}
        for s in self.scopes:
            merged.update(s)
        return merged

    def vars_of_type(self, pred):
        return [n for n, t in self.visible_vars().items() if pred(t)]

    def declare(self, typ):
        """Create a fresh var of type typ in the current scope."""
        self.var_counter += 1
        name = f"v{self.var_counter}"
        self.scopes[-1][name] = typ
        return name

    # ---- literals ----
    def int_literal(self, typ):
        pool = TYPE_LITERALS[typ]
        if self.r() < 0.7:
            return self.pick(pool)
        # random in-range literal
        bits = {"int8": 8, "uint8": 8, "int16": 16, "uint16": 16,
                "int32": 32, "uint32": 32}.get(typ, 64)
        if is_uint(typ):
            return str(self.rint(0, min(1000, 2**bits - 1)))
        lo, hi = -(2**(bits - 1)), 2**(bits - 1) - 1
        return str(self.rint(max(lo, -1000), min(hi, 1000)))

    def literal(self, typ):
        if is_int(typ):
            return self.int_literal(typ)
        if typ in FLOAT_TYPES:
            return self.pick(FLOAT_LITERALS)
        if typ == "string":
            return self.pick(STRING_LITERALS)
        if typ == "bool":
            return self.pick(BOOL_LITERALS)
        raise AssertionError("no literal for " + typ)

    # ---- expressions ----
    def expr(self, want, depth=0):
        r = self.r()
        if r < 0.35:
            return self.leaf_expr(want)
        if want == "bool":
            return self.bool_expr(depth)
        if is_int(want):
            return self.int_expr(want, depth)
        if want in FLOAT_TYPES:
            return self.float_expr(want, depth)
        if want == "string":
            return self.string_expr(depth)
        return self.leaf_expr(want)

    def leaf_expr(self, want):
        cands = self.vars_of_type(lambda t: t == want)
        if cands and self.r() < 0.6:
            return self.pick(cands)
        if want.startswith("[]"):
            return self.slice_literal(want)
        if want.startswith("map["):
            # empty map literal is valid Go
            return want + "{}"
        if want in self.structs:
            return self.struct_literal(want)
        if want.startswith("*"):
            # nil pointer literal is valid; deref would panic so only
            # use existing pointer vars (always non-nil by construction)
            if cands:
                return self.pick(cands)
            return "nil"
        return self.literal(want)

    def int_expr(self, want, depth):
        # Constant-overflow safety (Go rejects constant expressions that
        # overflow at compile time): every binary/unary int operation uses
        # a VARIABLE operand, making it a runtime (wrapping) computation.
        # If no variable of the type is visible, fall back to a literal.
        varcands = self.vars_of_type(lambda t: t == want)
        r = self.r()
        if r < 0.45:
            if not varcands:
                return self.leaf_expr(want)
            op = self.pick(["+", "-", "*", "/", "%", "&", "|", "^",
                            "<<", ">>"])
            a = self.pick(varcands)
            b = self.expr(want, depth + 1)
            if op in ("/", "%"):
                # nonzero divisor: b|1 is runtime (b may be var) or, if b
                # is a literal, |1 keeps it nonzero — but a constant 0|1=1
                # is fine; ensure b itself isn't constant-zero... (b|1)
                # with b constant is still constant, but nonzero. Safe.
                # However if b is a var holding 0, (0|1)=1, safe.
                b = f"(({b}) | 1)"
            if op in ("<<", ">>"):
                b = f"(({self.expr('uint', depth + 1)}) & 63)"
            return f"({a} {op} {b})"
        if r < 0.6:
            # conversions: uint targets only from uint sources; signed
            # targets from any int source. Source expr may be constant —
            # a constant that doesn't fit the target overflows, so force
            # the source to be a variable when the target is narrower.
            # (Simple rule: convert from a variable if one exists.)
            if is_uint(want):
                srcs = [t for t in INT_TYPES if is_uint(t) and t != want]
            else:
                srcs = [t for t in INT_TYPES if t != want]
            src = self.pick(srcs)
            srccands = self.vars_of_type(lambda t: t == src)
            if srccands and self.r() < 0.8:
                s = self.pick(srccands)
            else:
                # literal source: only safe if it fits the target; use an
                # in-range literal of the TARGET type directly instead.
                return self.leaf_expr(want)
            return f"{want}({s})"
        if r < 0.72:
            if not varcands:
                return self.leaf_expr(want)
            # unary minus on uint is fine at runtime (wraps); on constants
            # it can overflow, but we use variables here.
            op = self.pick(["-", "^"]) if not is_uint(want) else "^"
            return f"({op}({self.pick(varcands)}))"
        if r < 0.82:
            return self.leaf_expr(want)
        cands = [f for f in self.funcs if f[2] == want]
        if cands:
            return self.call_expr(self.pick(cands), depth)
        return self.leaf_expr(want)

    def bool_expr(self, depth):
        r = self.r()
        if r < 0.4:
            t = self.pick(INT_TYPES + FLOAT_TYPES + ["string"])
            op = self.pick(["==", "!=", "<", "<=", ">", ">="])
            return (f"({self.expr(t, depth + 1)} {op} "
                    f"{self.expr(t, depth + 1)})")
        if r < 0.6:
            op = self.pick(["&&", "||"])
            return (f"({self.expr('bool', depth + 1)} {op} "
                    f"{self.expr('bool', depth + 1)})")
        if r < 0.75:
            return f"(!{self.expr('bool', depth + 1)})"
        return self.leaf_expr("bool")

    def float_divisor_safe(self, b):
        """Ensure a float divisor isn't a compile-time zero (Go rejects
        constant x/0.0). Runtime zero is fine (yields +Inf)."""
        import re as _re
        t = b.strip()
        # Normalize: float32(X)/float64(X) -> (X); strip outer parens.
        t = _re.sub(r'float(?:32|64)\(', '(', t)
        while t.startswith("(") and t.endswith(")"):
            t = t[1:-1].strip()
        # If it references any identifier, it's runtime → safe (Inf ok).
        if _re.search(r'[a-zA-Z_]', t):
            return b
        # Constant expression: evaluate safely; if zero, use 1.0.
        try:
            if not _re.fullmatch(r'[\d\s\.\+\-\*\/\(\)eE]+', t):
                return b
            v = eval(t, {"__builtins__": {}}, {})  # noqa: S307
            if v == 0:
                return "1.0"
        except Exception:
            pass
        return b

    def float_expr(self, want, depth):
        r = self.r()
        if r < 0.5:
            op = self.pick(["+", "-", "*", "/"])
            a = self.expr(want, depth + 1)
            b = self.expr(want, depth + 1)
            if op == "/":
                b = self.float_divisor_safe(b)
            return f"({a} {op} {b})"
        if r < 0.65:
            src = "float64" if want == "float32" else "float32"
            return f"{want}({self.expr(src, depth + 1)})"
        return self.leaf_expr(want)

    def string_expr(self, depth):
        r = self.r()
        if r < 0.5:
            return (f"({self.expr('string', depth + 1)} + "
                    f"{self.expr('string', depth + 1)})")
        return self.leaf_expr("string")

    def slice_literal(self, want):
        elem = want[2:]
        n = self.rint(0, 4)
        elems = ", ".join(self.expr(elem, 3) for _ in range(n))
        return f"{want}{{{elems}}}"

    def struct_literal(self, want):
        fields = self.structs[want]
        parts = [f"{fname}: {self.expr(ftype, 3)}"
                 for fname, ftype in fields]
        return f"{want}{{{', '.join(parts)}}}"

    def call_expr(self, func, depth):
        name, params, _ = func
        args = ", ".join(self.expr(pt, depth + 1) for _, pt in params)
        return f"{name}({args})"

    # ---- statements ----
    def stmt(self, depth=0):
        r = self.r()
        if r < 0.22:
            return self.decl_stmt()
        if r < 0.38:
            return self.assign_stmt()
        if r < 0.52:
            return self.if_stmt(depth)
        if r < 0.64:
            return self.for_stmt(depth)
        if r < 0.72:
            return self.switch_stmt(depth)
        if r < 0.82:
            return self.slice_map_stmt()
        if r < 0.90:
            return self.print_stmt()
        # ptr_stmt disabled in gen-1: StarExpr (deref read/write) is WS D
        # out-of-scope. Re-enable when frontend supports it.
        return self.struct_stmt()

    def decl_stmt(self):
        typ = self.pick(INT_TYPES + FLOAT_TYPES + ["string", "bool"])
        e = self.expr(typ)          # generate BEFORE declaring (no self-ref)
        name = self.declare(typ)
        # Use explicit `var` (not `:=`): an untyped literal RHS would
        # otherwise infer the default type (int), not typ.
        return f"var {name} {typ} = {e}"

    def assign_stmt(self):
        # Exclude loop variables (assigning to them can create infinite
        # loops, which are generator bugs, not graalgo bugs).
        cands = [n for n in self.vars_of_type(
            lambda t: is_int(t) or t in FLOAT_TYPES or t in ("string", "bool"))
            if n not in self.loop_vars]
        if not cands:
            return self.decl_stmt()
        name = self.pick(cands)
        typ = self.visible_vars()[name]
        if typ == "bool":
            return f"{name} = {self.expr('bool')}"
        if typ == "string":
            return f"{name} = {self.expr('string')}"
        op = self.pick(["=", "=", "+=", "-="])
        return f"{name} {op} {self.expr(typ)}"

    def block(self, depth, n):
        """Generate a scoped block; returns lines including trailing _ =."""
        self.push_scope()
        lines = [self.stmt(depth) for _ in range(n)]
        popped = self.pop_scope()
        for v in sorted(popped):
            lines.append(f"_ = {v}")
        return lines

    def ind(self, lines, level=1):
        pad = "    " * level
        out = []
        for l in lines:
            out.append(pad + l.replace("\n", "\n" + pad))
        return out

    def if_stmt(self, depth):
        cond = self.expr("bool")
        then_b = self.block(depth + 1, self.rint(1, 3))
        lines = [f"if {cond} {{"] + self.ind(then_b) + ["}"]
        if self.r() < 0.5:
            else_b = self.block(depth + 1, self.rint(1, 3))
            lines[-1] = "} else {"
            lines += self.ind(else_b) + ["}"]
        return "\n".join(lines)

    def for_stmt(self, depth):
        bound = self.rint(1, 20)
        self.push_scope()
        self.var_counter += 1
        i = f"fi{self.var_counter}"
        self.scopes[-1][i] = "int"
        self.loop_vars.add(i)
        body = [self.stmt(depth + 1) for _ in range(self.rint(1, 3))]
        self.loop_vars.discard(i)
        popped = self.pop_scope()
        lines = [f"for {i} := 0; {i} < {bound}; {i}++ {{"]
        lines += self.ind(body)
        for v in sorted(popped):
            lines += self.ind([f"_ = {v}"])
        lines += ["}"]
        return "\n".join(lines)

    def switch_stmt(self, depth):
        typ = self.pick(["int", "string"])
        tag = self.expr(typ)
        lines = [f"switch {tag} {{"]
        used = set()
        for _ in range(self.rint(1, 3)):
            # unique case values (Go rejects duplicates)
            for _ in range(20):
                cl = self.int_literal("int") if typ == "int" else self.pick(STRING_LITERALS)
                if cl not in used:
                    break
            used.add(cl)
            body = self.block(depth + 1, 1)
            lines.append(f"case {cl}:")
            lines += self.ind(body)
        if self.r() < 0.5:
            body = self.block(depth + 1, 1)
            lines.append("default:")
            lines += self.ind(body)
        lines.append("}")
        return "\n".join(lines)

    def slice_map_stmt(self):
        slices = self.vars_of_type(lambda t: t.startswith("[]"))
        maps = self.vars_of_type(lambda t: t.startswith("map["))
        if self.r() < 0.5 or not maps:
            if not slices:
                e = self.slice_literal("[]int")
                name = self.declare("[]int")
                return f"{name} := {e}"
            name = self.pick(slices)
            elem = self.visible_vars()[name][2:]
            op = self.pick(["append", "index", "slicing", "len"])
            if op == "append":
                return f"{name} = append({name}, {self.expr(elem)})"
            if op == "index":
                return (f"if len({name}) > 0 {{\n"
                        f"    {name}[0] = {self.expr(elem)}\n"
                        f"}}")
            if op == "slicing":
                return (f"if len({name}) >= 2 {{\n"
                        f"    {name} = {name}[1:len({name})]\n"
                        f"}}")
            return f"_ = len({name})"
        name = self.pick(maps)
        inner = self.visible_vars()[name][3:]  # "map[K]V" -> "[K]V"
        assert inner.startswith("[")
        kt, vt = inner[1:].split("]", 1)
        op = self.pick(["write", "read", "delete", "len"])
        if op == "write":
            return f"{name}[{self.expr(kt)}] = {self.expr(vt)}"
        if op == "read":
            e = self.expr(kt)
            tmp = self.declare(vt)
            return f"{tmp} := {name}[{e}]"
        if op == "delete":
            return f"delete({name}, {self.expr(kt)})"
        return f"_ = len({name})"

    def print_stmt(self):
        cands = self.vars_of_type(
            lambda t: is_int(t) or t in FLOAT_TYPES or t in ("string", "bool"))
        if not cands:
            return self.decl_stmt()
        k = min(len(cands), self.rint(1, 3))
        return "fmt.Println(" + ", ".join(self.pick(cands) for _ in range(k)) + ")"

    def struct_stmt(self):
        if not self.structs or self.r() < 0.4:
            self.struct_counter += 1
            sname = f"GenS{self.struct_counter}"
            nfields = self.rint(1, 3)
            fields = [(f"F{i}", self.pick(INT_TYPES + ["string", "bool"]))
                      for i in range(nfields)]
            self.structs[sname] = fields
            self.pending_structs.append((sname, fields))
        else:
            sname = self.pick(list(self.structs.keys()))
        e = self.struct_literal(sname)
        vname = self.declare(sname)
        return f"{vname} := {e}"

    def ptr_stmt(self):
        # Note: `*p = x` (assign through pointer) is WS D out-of-scope;
        # only pointer creation and dereference reads are generated.
        cands = self.vars_of_type(is_int)
        base = self.pick(INT_TYPES)
        if not cands or self.r() < 0.5:
            e = self.expr(base)
            bname = self.declare(base)
            pname = self.declare("*" + base)
            rname = self.declare(base)
            return (f"var {bname} {base} = {e}\n"
                    f"{pname} := &{bname}\n"
                    f"{rname} := *{pname}")
        name = self.pick(cands)
        pname = self.declare("*" + self.visible_vars()[name])
        rname = self.declare(self.visible_vars()[name])
        return (f"{pname} := &{name}\n"
                f"{rname} := *{pname}")

    # ---- functions ----
    def gen_func(self):
        nparams = self.rint(0, 3)
        params = [(f"p{i}", self.pick(INT_TYPES + ["string", "bool"]))
                  for i in range(nparams)]
        retype = self.pick(INT_TYPES + ["string", "bool"])
        fname = f"genFn{len(self.funcs)}"
        # Isolate: function bodies see ONLY params + own locals, never
        # main's locals (Go scoping).
        saved_scopes = self.scopes
        self.scopes = [{}]
        for pn, pt in params:
            self.scopes[-1][pn] = pt
        body = []
        for _ in range(self.rint(2, 5)):
            body.append(self.stmt(1))
        # Generate return expr while function scope is still active.
        ret_expr = self.expr(retype)
        # _ = for scope vars must come BEFORE return (Go's termination
        # analysis rejects unreachable statements after return).
        popped = self.scopes.pop()
        self.scopes = saved_scopes
        for v in sorted(popped):
            body.append(f"_ = {v}")
        body.append(f"return {ret_expr}")
        psig = ", ".join(f"{pn} {pt}" for pn, pt in params)
        decl = f"func {fname}({psig}) {retype} {{\n"
        decl += "\n".join(self.ind(body)) + "\n}"
        self.funcs.append((fname, params, retype))
        return decl

    def sorted_key_helpers(self):
        lines = []
        for kt in sorted(self.need_sorted_keys):
            lines.append(f"func fuzzSortedKeys_{kt}(m map[{kt}]int) []{kt} {{")
            lines.append(f"    ks := make([]{kt}, 0, len(m))")
            lines.append("    for k := range m { ks = append(ks, k) }")
            lines.append("    sort.Slice(ks, func(i, j int) bool { return ks[i] < ks[j] })")
            lines.append("    return ks")
            lines.append("}")
        return lines

    # ---- program ----
    def gen_program(self):
        self.pending_structs = []
        self.need_sorted_keys = set()
        imports = ['"fmt"']
        body = []
        # seed vars: maps, slices, scalars
        for _ in range(self.rint(3, 6)):
            r = self.r()
            if r < 0.25:
                kt = self.pick(["string", "int", "uint32"])
                vt = self.pick(["int", "string", "uint32", "bool"])
                name = self.declare(f"map[{kt}]{vt}")
                body.append(f"{name} := make(map[{kt}]{vt})")
            elif r < 0.4:
                et = self.pick(["int", "string", "uint32"])
                e = self.slice_literal("[]" + et)
                name = self.declare("[]" + et)
                body.append(f"{name} := {e}")
            else:
                body.append(self.decl_stmt())
        func_decls = [self.gen_func() for _ in range(self.rint(0, 2))]
        for _ in range(self.rint(8, 18)):
            body.append(self.stmt(0))
        # final deterministic dump (main scope still active)
        dump = []
        for n, t in sorted(self.visible_vars().items()):
            if t.startswith("map["):
                kt, vt = t[4:][1:].split("]", 1)  # "[K]V" -> K, V
                dump.append(f'fmt.Println("maplen {n}:", len({n}))')
                if vt == "int" and kt in ("string", "int", "uint32"):
                    dump.append(f"for _, k := range fuzzSortedKeys_{kt}({n}) {{ fmt.Println(k, {n}[k]) }}")
                    self.need_sorted_keys.add(kt)
            elif t.startswith("[]"):
                dump.append(f'fmt.Println("slicelen {n}:", len({n}), {n})')
            elif t.startswith("*") or t in self.structs:
                pass
            else:
                dump.append(f'fmt.Println("{n}:", {n})')
        if self.need_sorted_keys:
            imports.append('"sort"')
        # assemble
        out = ["package main", ""]
        out.append("import (")
        out += ["    " + i for i in imports]
        out.append(")")
        out.append("")
        for sname, fields in self.pending_structs:
            fs = "; ".join(f"{fn} {ft}" for fn, ft in fields)
            out.append(f"type {sname} struct {{ {fs} }}")
        out += self.sorted_key_helpers()
        out += func_decls
        out.append("")
        out.append("func main() {")
        out += self.ind(body)
        out += self.ind(dump)
        # _ = for any main-scope vars (belt & braces; dump covers most).
        # Separate statements: `_ = a, b` is illegal (blank takes one).
        mains = sorted(self.scopes[0])
        for v in mains:
            out += self.ind([f"_ = {v}"])
        out.append("}")
        out.append("")
        return "\n".join(out)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--seed", type=int, required=True)
    ap.add_argument("--out", default="-")
    args = ap.parse_args()
    g = Gen(args.seed)
    prog = g.gen_program()
    if args.out == "-":
        sys.stdout.write(prog)
    else:
        with open(args.out, "w") as f:
            f.write(prog)


if __name__ == "__main__":
    main()
