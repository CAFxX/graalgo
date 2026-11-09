#!/usr/bin/env python3
"""Schema validator for the graalgo frontend/v1 JSON wire format.

Enforces the contract documented in ../../docs/arch/frontend-v1.md:
  - top-level document shape and "frontend": "v1" version pin
  - type-table entries: known kinds, required detail fields, in-range refs
  - object-table entries
  - AST nodes: known kinds, required fields per kind, positions present,
    every type/obj reference resolves

Usage:
  validate_frontend_v1.py [--selftest] <doc.json> [...]

Exit 0: all documents valid. Exit 1: any validation error (details on stdout).
--selftest runs the embedded negative cases; every one must be rejected.
It also runs the kind-coverage check: every node kind the exporter
constructs (internal/frontend/*.go) must have a branch in check_node.
"""

import json
import sys

VERSION = "v1"

BINARY_OPS = {"+", "-", "*", "/", "%", "&", "|", "^", "&^", "<<", ">>",
              "==", "!=", "<", "<=", ">", ">=", "&&", "||"}
UNARY_OPS = {"+", "-", "!", "^"}
ASSIGN_TOKS = {"=", "+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "&^=",
               "<<=", ">>="}

TYPE_KINDS = {"basic", "named", "pointer", "slice", "array", "map", "chan",
              "struct", "signature", "interface", "union", "typeparam", "alias"}


class Ctx:
    def __init__(self, doc):
        self.doc = doc
        self.errors = []
        self.types = doc.get("types", [])
        self.objects = doc.get("objects", [])

    def err(self, path, msg):
        self.errors.append(f"{path}: {msg}")

    def check_type_id(self, path, tid):
        if not isinstance(tid, int) or isinstance(tid, bool):
            self.err(path, f"type id must be int, got {tid!r}")
        elif not 0 <= tid < len(self.types):
            self.err(path, f"dangling type id {tid} (table has {len(self.types)})")

    def check_obj_id(self, path, oid):
        if not isinstance(oid, int) or isinstance(oid, bool):
            self.err(path, f"object id must be int, got {oid!r}")
        elif not 0 <= oid < len(self.objects):
            self.err(path, f"dangling object id {oid} (table has {len(self.objects)})")


def check_pos(ctx, path, pos):
    if not isinstance(pos, dict):
        ctx.err(path, f"pos must be object, got {pos!r}")
        return
    for k in ("file", "line", "col", "off", "len"):
        v = pos.get(k)
        if not isinstance(v, int) or isinstance(v, bool) or v < 0:
            ctx.err(f"{path}.{k}", f"must be non-negative int, got {v!r}")
    if not isinstance(pos.get("line"), int) or pos.get("line", 0) < 1:
        ctx.err(f"{path}.line", "line is 1-based")


def check_types(ctx):
    if not isinstance(ctx.types, list):
        ctx.err("types", "must be an array")
        return
    for i, e in enumerate(ctx.types):
        p = f"types[{i}]"
        if not isinstance(e, dict):
            ctx.err(p, "must be an object")
            continue
        kind = e.get("kind")
        if kind not in TYPE_KINDS:
            ctx.err(p, f"unknown type kind {kind!r}")
            continue
        d = e.get("detail")
        if not isinstance(d, dict):
            ctx.err(p, "detail must be an object")
            continue
        req = {
            "basic": ["name", "class", "untyped"],
            "named": ["name", "underlying", "methods"],
            "pointer": ["elem"],
            "slice": ["elem"],
            "array": ["len", "elem"],
            "map": ["key", "elem"],
            "chan": ["dir", "elem"],
            "struct": ["fields"],
            "signature": ["params", "results", "variadic"],
            "interface": ["empty", "methods", "embedded"],
            "union": ["terms"],
            "typeparam": ["name", "constraint"],
            "alias": ["name", "rhs"],
        }[kind]
        for k in req:
            if k not in d:
                ctx.err(p, f"kind {kind}: missing detail field {k!r}")
        # ref fields must be in-range type ids
        for k in ("elem", "key", "underlying", "rhs", "constraint"):
            if k in d:
                ctx.check_type_id(f"{p}.detail.{k}", d[k])
        if kind == "basic" and d.get("class") not in {
                "bool", "int", "uint", "float", "complex", "string",
                "pointer", "untyped", "unknown"}:
            ctx.err(p, f"unknown basic class {d.get('class')!r}")
        if kind == "struct":
            for j, f in enumerate(d.get("fields", [])):
                for k in ("name", "type", "tag", "embedded", "offset"):
                    if k not in f:
                        ctx.err(f"{p}.detail.fields[{j}]", f"missing {k!r}")
                if "type" in f:
                    ctx.check_type_id(f"{p}.detail.fields[{j}].type", f["type"])
        if kind == "signature":
            for sec in ("params", "results"):
                for j, prm in enumerate(d.get(sec, [])):
                    if "type" in prm:
                        ctx.check_type_id(f"{p}.detail.{sec}[{j}].type", prm["type"])


def check_objects(ctx):
    if not isinstance(ctx.objects, list):
        ctx.err("objects", "must be an array")
        return
    for i, o in enumerate(ctx.objects):
        p = f"objects[{i}]"
        if not isinstance(o, dict):
            ctx.err(p, "must be an object")
            continue
        kind = o.get("kind")
        # M6: "func" (function/method objects) and "inst" (monomorphized
        # instantiations) joined the original "var" kind.
        if kind not in ("var", "func", "inst"):
            ctx.err(p, f"unknown object kind {kind!r}")
        if not isinstance(o.get("name"), str):
            ctx.err(p, "name must be a string")
        if kind == "inst":
            if not isinstance(o.get("instof"), int):
                ctx.err(p, "inst: instof must be an int")
            if not isinstance(o.get("typeargs"), list):
                ctx.err(p, "inst: typeargs must be a list")
        else:
            if not isinstance(o.get("param"), bool):
                ctx.err(p, "param must be a bool")
            ctx.check_type_id(f"{p}.type", o.get("type"))


def check_globals(ctx):
    """R-1108-170: `globals` was never validated — dangling obj ids,
    invalid positions, unknown init kinds and even a non-array
    `globals` passed clean. The exporter always emits an array
    (exportPackageVars builds it with make, never nil), so a missing
    or non-array value is a wire-format error."""
    g = ctx.doc.get("globals")
    if not isinstance(g, list):
        ctx.err("globals", "must be an array")
        return
    for i, e in enumerate(g):
        p = f"globals[{i}]"
        if not isinstance(e, dict):
            ctx.err(p, "must be an object")
            continue
        # obj: -1 is the sentinel for side-effect-only synthetic
        # entries (export.go: "They are emitted as side-effect-only
        # globals entries (Obj == -1)"); accept it explicitly, never
        # via the range check. Anything else must be a valid object id.
        obj = e.get("obj")
        if not (isinstance(obj, int) and not isinstance(obj, bool) and obj == -1):
            ctx.check_obj_id(p + ".obj", obj)
        if "pos" in e and e["pos"] is not None:
            check_pos(ctx, p + ".pos", e["pos"])
        if e.get("init") is not None:
            check_node(ctx, p + ".init", e["init"], True)


def check_node(ctx, path, node, expr):
    """expr=True when the node must carry a type reference (expression)."""
    if not isinstance(node, dict):
        ctx.err(path, f"node must be an object, got {node!r}")
        return
    kind = node.get("kind")
    if not isinstance(kind, str):
        ctx.err(path, "node missing string 'kind'")
        return
    check_pos(ctx, f"{path}.pos", node.get("pos"))
    f = node.get("fields")
    if f is not None and not isinstance(f, dict):
        ctx.err(path, "fields must be an object")
        return
    f = f or {}
    fp = f"{path}.fields"

    def duration_field(ctx, fp, f):
        # P3-M5 (WS E, ADR-0005 App W): the `duration` field of time
        # nodes accepts an expression node (what the exporter emits) or
        # a JSON number of nanoseconds (what the Java builders accept).
        if "duration" not in f:
            ctx.err(fp, "time node: missing field 'duration'")
            return
        d = f["duration"]
        if isinstance(d, dict):
            check_node(ctx, f"{fp}.duration", d, True)
        elif not isinstance(d, (int, float)) or isinstance(d, bool):
            ctx.err(fp, f"time node: duration must be an expr node or a number, got {d!r}")

    def need(k):
        if k not in f:
            ctx.err(fp, f"kind {kind}: missing field {k!r}")

    def node_list(k):
        need(k)
        for j, s in enumerate(f.get(k, [])):
            check_node(ctx, f"{fp}.{k}[{j}]", s, False)

    def node_list_opt(k):
        if k in f and f[k] is not None:
            for j, s in enumerate(f.get(k, [])):
                check_node(ctx, f"{fp}.{k}[{j}]", s, False)

    def type_list(k):
        need(k)
        for j, tid in enumerate(f.get(k, [])):
            ctx.check_type_id(f"{fp}.{k}[{j}]", tid)

    def obj_ref(k):
        need(k)
        if k in f:
            ctx.check_obj_id(f"{fp}.{k}", f[k])

    def str_field(k, values=None):
        need(k)
        v = f.get(k)
        if not isinstance(v, str):
            ctx.err(fp, f"kind {kind}: field {k!r} must be a string, got {v!r}")
        elif values is not None and v not in values:
            ctx.err(fp, f"kind {kind}: field {k!r} must be one of {sorted(values)}, got {v!r}")

    def bool_field(k):
        need(k)
        if not isinstance(f.get(k), bool):
            ctx.err(fp, f"kind {kind}: field {k!r} must be a bool")

    def expr_node(k):
        need(k)
        if k in f:
            check_node(ctx, f"{fp}.{k}", f[k], True)

    def type_ref(k, nullable=False):
        if k in f and f[k] is not None:
            ctx.check_type_id(f"{fp}.{k}", f[k])
        elif not nullable:
            need(k)

    if kind == "funcdecl":
        # handled structurally by caller; still validate common shape.
        # Spec §6: the linkname form and the bodyless (`unimplemented`)
        # form carry NO body; every other form requires one. `results`
        # are param nodes; `instof`/`typeargs` pin monomorphizations.
        for k in ("name", "sig", "params"):
            need(k)
        ctx.check_type_id(f"{fp}.sig", f.get("sig"))
        for j, prm in enumerate(f.get("params", [])):
            check_node(ctx, f"{fp}.params[{j}]", prm, False)
        node_list_opt("results")
        if "obj" in f and f["obj"] is not None:
            ctx.check_obj_id(f"{fp}.obj", f["obj"])
        if "instof" in f and f["instof"] is not None:
            ctx.check_obj_id(f"{fp}.instof", f["instof"])
        if "typeargs" in f and f["typeargs"] is not None:
            type_list("typeargs")
        if f.get("linkname") is not None or f.get("unimplemented") is True:
            if f.get("body") is not None:
                ctx.err(fp, "funcdecl: linkname/bodyless form must not carry a body")
        else:
            check_node(ctx, f"{fp}.body", f.get("body"), False)
    elif kind == "param":
        for k in ("name", "obj", "type"):
            need(k)
        ctx.check_obj_id(f"{fp}.obj", f.get("obj"))
        ctx.check_type_id(f"{fp}.type", f.get("type"))
    elif kind == "block":
        node_list("stmts")
    elif kind == "vardecl":
        for k in ("names", "objs", "define", "values"):
            need(k)
        if not isinstance(f.get("define"), bool):
            ctx.err(fp, "define must be bool")
        if len(f.get("names", [])) != len(f.get("objs", [])):
            ctx.err(fp, "names/objs length mismatch")
        for j, oid in enumerate(f.get("objs", [])):
            ctx.check_obj_id(f"{fp}.objs[{j}]", oid)
        type_ref("type", nullable=True)
        node_list("values")
    elif kind == "assign":
        need("tok")
        if f.get("tok") not in ASSIGN_TOKS:
            ctx.err(fp, f"unknown assign tok {f.get('tok')!r}")
        node_list("lhs")
        node_list("rhs")
    elif kind == "exprstmt":
        expr_node("x")
    elif kind == "return":
        node_list("results")
    elif kind == "incdec":
        need("tok")
        if f.get("tok") not in ("++", "--"):
            ctx.err(fp, f"bad incdec tok {f.get('tok')!r}")
        expr_node("x")
    elif kind == "blank":
        pass
    elif kind == "if":
        if f.get("init") is not None:
            check_node(ctx, f"{fp}.init", f["init"], False)
        expr_node("cond")
        check_node(ctx, f"{fp}.then", f.get("then"), False)
        if f.get("else") is not None:
            check_node(ctx, f"{fp}.else", f["else"], False)
    elif kind == "for":
        if f.get("init") is not None:
            check_node(ctx, f"{fp}.init", f["init"], False)
        if f.get("cond") is not None:
            check_node(ctx, f"{fp}.cond", f["cond"], True)
        if f.get("post") is not None:
            check_node(ctx, f"{fp}.post", f["post"], False)
        check_node(ctx, f"{fp}.body", f.get("body"), False)
    elif kind == "range":
        for k in ("key", "value"):
            if f.get(k) is not None:
                ctx.check_obj_id(f"{fp}.{k}", f[k])
        if not isinstance(f.get("define"), bool):
            ctx.err(fp, "define must be bool")
        expr_node("x")
        check_node(ctx, f"{fp}.body", f.get("body"), False)
    elif kind == "switch":
        if f.get("init") is not None:
            check_node(ctx, f"{fp}.init", f["init"], False)
        if f.get("tag") is not None:
            check_node(ctx, f"{fp}.tag", f["tag"], True)
        need("clauses")
        for j, c in enumerate(f.get("clauses", [])):
            check_node(ctx, f"{fp}.clauses[{j}]", c, False)
    elif kind == "caseclause":
        for j, c in enumerate(f.get("cases") or []):
            check_node(ctx, f"{fp}.cases[{j}]", c, True)
        if not isinstance(f.get("default"), bool):
            ctx.err(fp, "default must be bool")
        check_node(ctx, f"{fp}.body", f.get("body"), False)
    elif kind == "typeswitch":
        if f.get("init") is not None:
            check_node(ctx, f"{fp}.init", f["init"], False)
        expr_node("tag")
        need("clauses")
        for j, c in enumerate(f.get("clauses", [])):
            check_node(ctx, f"{fp}.clauses[{j}]", c, False)
    elif kind == "typecase":
        for j, tid in enumerate(f.get("types") or []):
            ctx.check_type_id(f"{fp}.types[{j}]", tid)
        for k in ("nilcase", "default"):
            if not isinstance(f.get(k), bool):
                ctx.err(fp, f"{k} must be bool")
        if f.get("bindobj") is not None:
            ctx.check_obj_id(f"{fp}.bindobj", f["bindobj"])
            ctx.check_type_id(f"{fp}.bindtype", f["bindtype"])
        check_node(ctx, f"{fp}.body", f.get("body"), False)
    elif kind == "branch":
        need("op")
        if f.get("op") not in ("break", "continue", "goto", "fallthrough"):
            ctx.err(fp, f"unknown branch op {f.get('op')!r}")
    elif kind == "labeled":
        need("label")
        check_node(ctx, f"{fp}.stmt", f.get("stmt"), False)
    elif kind == "ident":
        need("name")
        if "obj" in f and f["obj"] is not None:
            ctx.check_obj_id(f"{fp}.obj", f["obj"])
        if "builtin" in f:
            # Builtin form (spec §6): `name` + `builtin` str, no `type`
            # (builtins have no go/types signature).
            if not isinstance(f.get("builtin"), str):
                ctx.err(fp, "ident: builtin must be a string")
        else:
            type_ref("type")
    elif kind == "basiclit":
        need("value")
        type_ref("type")
    elif kind == "binary":
        need("op")
        if f.get("op") not in BINARY_OPS:
            ctx.err(fp, f"unknown binary op {f.get('op')!r}")
        expr_node("x")
        expr_node("y")
        type_ref("type")
    elif kind == "unary":
        need("op")
        if f.get("op") not in UNARY_OPS:
            ctx.err(fp, f"unknown unary op {f.get('op')!r}")
        expr_node("x")
        type_ref("type")
    elif kind == "paren":
        expr_node("x")
        type_ref("type")
    elif kind == "call":
        if "gcinterop" in f or "goexit" in f:
            # Marker calls: runtime.SetFinalizer/KeepAlive/GC (P2-M4) and
            # runtime.Goexit. The marker REPLACES `fun` (and `type`) —
            # the backend builds the intrinsic node directly
            # (NodeFactory buildCall: "no \"fun\" key, like goexit").
            if "fun" in f:
                ctx.err(fp, "call: marker call (gcinterop/goexit) must not carry 'fun'")
            if "gcinterop" in f:
                str_field("gcinterop", {"SetFinalizer", "KeepAlive", "GC"})
            if "goexit" in f and f.get("goexit") is not True:
                ctx.err(fp, "call: goexit must be true")
            node_list("args")
        else:
            expr_node("fun")
            node_list("args")
            type_ref("type", nullable=True)
    elif kind == "convert":
        expr_node("x")
        type_ref("to")
        type_ref("type")
        if "from" in f and f["from"] is not None:
            ctx.check_type_id(f"{fp}.from", f["from"])
    elif kind == "complit":
        type_ref("type")
        if not isinstance(f.get("keyed"), bool):
            ctx.err(fp, "keyed must be bool")
        for j, v in enumerate(f.get("values") or []):
            check_node(ctx, f"{fp}.values[{j}]", v, True)
        for j, k in enumerate(f.get("keys") or []):
            check_node(ctx, f"{fp}.keys[{j}]", k, True)
        if "fieldkeys" in f and not all(isinstance(s, str) for s in f["fieldkeys"]):
            ctx.err(fp, "fieldkeys must be strings")
    elif kind == "index":
        expr_node("x")
        expr_node("index")
        type_ref("type")
        if not isinstance(f.get("commaOk"), bool):
            ctx.err(fp, "commaOk must be bool")
    elif kind == "sliceexpr":
        expr_node("x")
        for k in ("lo", "hi", "max"):
            if f.get(k) is not None:
                check_node(ctx, f"{fp}.{k}", f[k], True)
        type_ref("type")
    elif kind == "selector":
        expr_node("x")
        need("field")
        need("selpath")
        type_ref("type")
    elif kind == "typeassert":
        expr_node("x")
        type_ref("type")
        if not isinstance(f.get("commaOk"), bool):
            ctx.err(fp, "commaOk must be bool")
    elif kind == "makechan":
        # P3-M5 (WS E, ADR-0005 App C): make(chan T[, n]).
        type_ref("elem")
        if f.get("size") is not None:
            check_node(ctx, f"{fp}.size", f["size"], True)
        type_ref("type")
    elif kind == "send":
        # P3-M5 (WS E, ADR-0005 App C): ch <- v (statement).
        expr_node("chan")
        expr_node("value")
    elif kind == "recv":
        # P3-M5 (WS E, ADR-0005 App C): <-ch. The exporter NEVER emits
        # commaOk on recv (the v, ok := <-ch shape desugars to a
        # single-case select); a present commaOk is rejected as a
        # contract violation.
        expr_node("x")
        type_ref("type")
        if "commaOk" in f:
            ctx.err(fp, "recv must not carry commaOk (desugared to select)")
    elif kind == "chanlen":
        # P3-M5 (WS E, ADR-0005 App C): len(ch) — additive expression
        # kind; type is the int result type id (validated when present).
        expr_node("x")
        type_ref("type")
    elif kind == "chancap":
        # P3-M5 (WS E, ADR-0005 App C): cap(ch) — same shape as chanlen.
        expr_node("x")
        type_ref("type")
    elif kind == "chanclose":
        # P3-M5 (WS E, ADR-0005 App C): close(ch) (statement).
        expr_node("x")
    elif kind == "select":
        # P3-M5 (WS E, ADR-0005 App C): select statement.
        need("cases")
        for j, c in enumerate(f.get("cases", [])):
            check_node(ctx, f"{fp}.cases[{j}]", c, False)
    elif kind == "selectcase":
        # P3-M5 (WS E, ADR-0005 App C): one select case.
        ck = f.get("kind")
        if ck == "recv":
            expr_node("chan")
            if "targets" in f and not all(isinstance(t, int) for t in f["targets"]):
                ctx.err(fp, "targets must be object ids")
        elif ck == "send":
            expr_node("chan")
            expr_node("value")
        elif ck != "default":
            ctx.err(fp, f"selectcase: unknown kind {ck!r}")
        if f.get("body") is not None:
            check_node(ctx, f"{fp}.body", f["body"], False)
    elif kind == "timenew":
        # P3-M5 (WS E, ADR-0005 App W): time.NewTimer/NewTicker.
        if f.get("kind") not in ("timer", "ticker"):
            ctx.err(fp, f"timenew kind must be 'timer' or 'ticker', got {f.get('kind')!r}")
        duration_field(ctx, fp, f)
        type_ref("type")
    elif kind == "timeop":
        # P3-M5 (WS E, ADR-0005 App W): (*Timer/*Ticker).Stop/Reset.
        # duration required for Reset, absent for Stop; type is bool,
        # absent for void (*Ticker).Stop().
        if f.get("op") not in ("Stop", "Reset"):
            ctx.err(fp, f"timeop op must be 'Stop' or 'Reset', got {f.get('op')!r}")
        expr_node("recv")
        if f.get("op") == "Reset":
            duration_field(ctx, fp, f)
        if "type" in f and f["type"] is not None:
            ctx.check_type_id(f"{fp}.type", f["type"])
    elif kind == "timec":
        # P3-M5 (WS E, ADR-0005 App W): t.C channel accessor.
        expr_node("recv")
        type_ref("type")
    elif kind == "timenow":
        # P3-M5 (WS E, ADR-0005 App W): time.Now().
        type_ref("type")
    elif kind == "timesleep":
        # P3-M5 (WS E, ADR-0005 App W): time.Sleep(d) — void, no type.
        duration_field(ctx, fp, f)
    elif kind == "timeafter":
        # P3-M5 (WS E, ADR-0005 App W): time.After(d).
        duration_field(ctx, fp, f)
        type_ref("type")
    elif kind == "timetick":
        # P3-M5 (WS E, ADR-0005 App W): time.Tick(d).
        duration_field(ctx, fp, f)
        type_ref("type")
    elif kind == "timeafterfunc":
        # P3-M5 (WS E, ADR-0005 App W): time.AfterFunc(d, f).
        duration_field(ctx, fp, f)
        expr_node("func")
        type_ref("type")
    elif kind == "defer":
        # M5 (WS F): `defer f(args)` — the deferred call node.
        expr_node("call")
    elif kind == "gostmt":
        # P2-M1 (WS A): `go f(args)` — the spawned call node.
        expr_node("call")
    elif kind == "funclit":
        # M5 (WS F): function literal. `params`/`results` are param
        # nodes; desugared funclits carry no `type`, real literals do.
        node_list("params")
        node_list("results")
        check_node(ctx, f"{fp}.body", f.get("body"), False)
        type_ref("type", nullable=True)
    elif kind == "nil":
        # M3: the nil literal. The type is the switch tag's type for
        # `case nil:` and the expression's type elsewhere.
        type_ref("type")
    elif kind == "addrof":
        # `&x`, plus the `&a[i]` form (`array` + `index` instead of `x`).
        if "array" in f or "index" in f:
            expr_node("array")
            expr_node("index")
        else:
            expr_node("x")
        type_ref("type")
    elif kind == "deref":
        # `*p` (read and assignment-lhs forms share the node).
        expr_node("x")
        type_ref("type")
    elif kind == "methodref":
        # M6 (WS H): method-call target — resolved function/instantiation
        # object id, receiver expr, signature type id (receiver is
        # params[0] of the callee funcdecl's sig).
        obj_ref("obj")
        expr_node("recv")
        type_ref("type")
    elif kind == "methodvalue":
        # P3-M9 (WS H): method value `x.M` — object id, receiver expr
        # (evaluated once at creation), static func type id (no receiver).
        obj_ref("obj")
        expr_node("recv")
        type_ref("type")
    elif kind == "ifacemethod":
        # P3-M3 (WS D, ADR-0003): interface method call `w.M(args)` with
        # dynamic dispatch. `type` is null when the call has no results.
        expr_node("recv")
        str_field("method")
        type_ref("iface")
        node_list("args")
        type_ref("sig")
        type_ref("type", nullable=True)
    elif kind == "reflectop":
        # Table-backed reflect operation. The `op` vocabulary is owned
        # by the exporter (export.go reflect*MethodOp tables plus the
        # `__reflect_*` intrinsic names in synth_reflect.go), so it is
        # checked as a non-empty string, not a frozen enum.
        str_field("op")
        node_list("args")
        type_ref("type")
    elif kind == "minmax":
        # WS G builtin: `min`/`max` — op is the builtin name, args are
        # the operands.
        str_field("op", {"min", "max"})
        node_list("args")
        type_ref("type")
    elif kind == "clear":
        # P4: `clear(x)` — void, no `type` field.
        expr_node("x")
    elif kind == "abifuncpc":
        # internal/abi FuncPCABI0/FuncPCABIInternal: `kind` is the
        # intrinsic name, `arg` the func value, `type` the uintptr result.
        str_field("kind", {"FuncPCABI0", "FuncPCABIInternal"})
        expr_node("arg")
        type_ref("type")
    elif kind == "sliceoverlap":
        # crypto/internal/fips140/alias AnyOverlap/InexactOverlap:
        # `inexact` is true for InexactOverlap.
        expr_node("x")
        expr_node("y")
        bool_field("inexact")
        type_ref("type")
    elif kind == "atomicaddr":
        # sync/atomic word-op address cell. `cell` is the word kind
        # (export.go atomicWordOp); the backend wraps the address in a
        # delegating AtomicLong/AtomicInteger cell.
        expr_node("addr")
        str_field("cell", {"int32", "int64", "bool"})
    elif kind == "atomicptrop":
        # atomic.SwapPointer/CompareAndSwapPointer: `op` is "swap"/"cas",
        # `addr` the addressed word, `args` the remaining operands.
        str_field("op", {"swap", "cas"})
        expr_node("addr")
        node_list("args")
        type_ref("type")
    elif kind == "syncop":
        # sync/atomic word ops (Load/Store/Add/Swap/CompareAndSwap/And/Or
        # over `atomic.*` synctyps, `recv` an atomicaddr cell) and
        # sync.Map multi-result methods (Load/LoadOrStore/LoadAndDelete/
        # Swap over synctyp "Map", `recv` the map expr).
        str_field("op", {"Load", "Store", "Add", "Swap", "CompareAndSwap",
                         "And", "Or", "LoadOrStore", "LoadAndDelete"})
        str_field("synctyp")
        expr_node("recv")
        node_list("args")
        type_ref("type")
    elif kind == "runtimecaller":
        # runtime.Caller(skip) multi-result desugar: `skip` is the skip
        # count, `type` the pair type of the desugared temp.
        expr_node("skip")
        type_ref("type")
    elif kind == "runtimecallers":
        # runtime.Callers(skip, pc).
        expr_node("skip")
        expr_node("pc")
        type_ref("type")
    elif kind == "runtimecallersframes":
        # runtime.CallersFrames(pcs).
        expr_node("pcs")
        type_ref("type")
    elif kind == "runtimeframesnext":
        # (*runtime.Frames).Next() multi-result desugar: `f` is the
        # Frames receiver, `type` the pair type of the desugared temp.
        expr_node("f")
        type_ref("type")
    elif kind == "runtimefuncfileline":
        # (*runtime.Func).FileLine(pc) multi-result desugar.
        expr_node("f")
        expr_node("pc")
        type_ref("type")
    elif kind == "runtimefuncforpc":
        # runtime.FuncForPC(pc).
        expr_node("pc")
        type_ref("type")
    elif kind == "runtimefuncname":
        # (*runtime.Func).Name(): `f` is the Func receiver.
        expr_node("f")
        type_ref("type")
    elif kind == "runtimefuncentry":
        # (*runtime.Func).Entry(): `f` is the Func receiver.
        expr_node("f")
        type_ref("type")
    elif kind == "runtimegomaxprocs":
        # runtime.GOMAXPROCS(n).
        expr_node("x")
        type_ref("type")
    elif kind == "runtimegoroot":
        # runtime.GOROOT().
        type_ref("type")
    elif kind == "runtimegosched":
        # runtime.Gosched() — statement node, no fields, no result.
        pass
    elif kind == "runtimenumcpu":
        # runtime.NumCPU().
        type_ref("type")
    elif kind == "runtimenumgoroutine":
        # runtime.NumGoroutine().
        type_ref("type")
    elif kind == "runtimereadmemstats":
        # runtime.ReadMemStats(m) — void, no `type` field.
        expr_node("m")
    elif kind == "runtimestack":
        # runtime.Stack(buf, all).
        node_list("args")
        type_ref("type")
    elif kind == "runtimeversion":
        # runtime.Version().
        type_ref("type")
    elif kind == "unsafeadd":
        # unsafe.Add(ptr, len).
        expr_node("ptr")
        expr_node("len")
        type_ref("type")
    elif kind == "unsafederive":
        # uintptr->unsafe.Pointer provenance-proven derivation: `base`
        # is the pointer-derived term, `offset` the integer offset expr.
        expr_node("base")
        expr_node("offset")
        type_ref("type")
    elif kind == "unsafefromptr":
        # *T / nil -> unsafe.Pointer.
        expr_node("x")
        type_ref("type")
    elif kind == "unsafefromuint":
        # uintptr -> unsafe.Pointer (non-provenance-proven fallback).
        expr_node("x")
        type_ref("type")
    elif kind == "unsafeslice":
        # unsafe.Slice(ptr, len).
        expr_node("ptr")
        expr_node("len")
        type_ref("type")
    elif kind == "unsafeslicedata":
        # unsafe.SliceData(s).
        expr_node("slice")
        type_ref("type")
    elif kind == "unsafestring":
        # unsafe.String(ptr, len).
        expr_node("ptr")
        expr_node("len")
        type_ref("type")
    elif kind == "unsafestringdata":
        # unsafe.StringData(s).
        expr_node("s")
        type_ref("type")
    elif kind == "unsafetooptr":
        # unsafe.Pointer -> *T. `elemSize` (bytes of T, from the same
        # go/types Sizes oracle as unsafe.Sizeof) is present when T has
        # a fixed size, absent otherwise.
        expr_node("x")
        type_ref("type")
        if "elemSize" in f and f["elemSize"] is not None:
            if not isinstance(f["elemSize"], int) or isinstance(f["elemSize"], bool) or f["elemSize"] < 0:
                ctx.err(fp, "unsafetooptr: elemSize must be a non-negative int")
    elif kind in ("mathf64bits", "mathf64frombits", "mathf32bits",
                  "mathf32frombits", "mathsqrt", "mathfma"):
        # Synthetic math intrinsics (exportMathIntrinsic, one wire kind
        # per intrinsic): unary operand `x` for Bits/FromBits/Sqrt,
        # `x`/`y`/`z` for FMA.
        expr_node("x")
        if kind == "mathfma":
            expr_node("y")
            expr_node("z")
        type_ref("type")
    else:
        ctx.err(path, f"unknown node kind {kind!r}")


def validate(doc):
    ctx = Ctx(doc)
    if not isinstance(doc, dict):
        return [": document must be a JSON object"]
    if doc.get("frontend") != VERSION:
        ctx.err("frontend", f"must be {VERSION!r}, got {doc.get('frontend')!r}")
    gen = doc.get("generator")
    if not isinstance(gen, dict):
        ctx.err("generator", "must be an object")
    else:
        for k in ("tool", "version", "goroot"):
            if not isinstance(gen.get(k), str):
                ctx.err(f"generator.{k}", "must be a string")
    pkg = doc.get("package")
    if not isinstance(pkg, dict):
        ctx.err("package", "must be an object")
    else:
        for k in ("name", "lang"):
            if not isinstance(pkg.get(k), str):
                ctx.err(f"package.{k}", "must be a string")
        if not isinstance(pkg.get("files"), list) or not pkg["files"]:
            ctx.err("package.files", "must be a non-empty array")
    check_types(ctx)
    check_objects(ctx)
    check_globals(ctx)
    funcs = doc.get("funcs")
    if not isinstance(funcs, list) or not funcs:
        ctx.err("funcs", "must be a non-empty array")
    else:
        for i, fn in enumerate(funcs):
            check_node(ctx, f"funcs[{i}]", fn, False)
            if isinstance(fn, dict) and fn.get("kind") != "funcdecl":
                ctx.err(f"funcs[{i}]", "top-level entries must be funcdecl")
    return ctx.errors


def _exporter_kinds():
    """Node kinds constructed by the exporter (internal/frontend/*.go).

    Returns None when the exporter sources are not found next to this
    script (standalone use outside the repo); otherwise the set of kind
    strings from `.node("kind", ...)` literals, `kind = "kind"` /
    `kind := "kind"` assignments, and the math-intrinsic map in
    exportMathIntrinsic (synth_machinery.go), whose kinds are map values
    rather than .node() literals.
    """
    import os
    import re
    here = os.path.dirname(os.path.abspath(__file__))
    srcdir = os.path.normpath(os.path.join(here, ".."))
    kinds = set()
    found = False
    try:
        names = os.listdir(srcdir)
    except OSError:
        return None
    for name in names:
        if not name.endswith(".go") or name.endswith("_test.go"):
            continue
        found = True
        with open(os.path.join(srcdir, name)) as fh:
            src = fh.read()
        kinds |= set(re.findall(r'\.node\("([a-z][a-z0-9]*)"', src))
        kinds |= set(re.findall(r'\bkind\s*:?=\s*"([a-z][a-z0-9]*)"', src))
    math_path = os.path.join(srcdir, "synth_machinery.go")
    if os.path.exists(math_path):
        with open(math_path) as fh:
            src = fh.read()
        m = re.search(r"func \(e \*exporter\) exportMathIntrinsic.*?map\[string\]string\{(.*?)\}",
                      src, re.S)
        if m:
            kinds |= set(re.findall(r':\s*"([a-z][a-z0-9]*)"', m.group(1)))
    return kinds if found else None


def _validator_kinds():
    """Node kinds with a branch in check_node (this file)."""
    import os
    import re
    with open(os.path.abspath(__file__)) as fh:
        src = fh.read()
    m = re.search(r"def check_node\(.*?\ndef validate\(", src, re.S)
    body = m.group(0)
    kinds = set(re.findall(r'kind == "([a-z][a-z0-9]*)"', body))
    for tup in re.findall(r'kind in \(([^)]*)\)', body):
        kinds |= set(re.findall(r'"([a-z][a-z0-9]*)"', tup))
    return kinds


def selftest():
    """Every embedded negative case must be rejected; the positive must pass."""
    base = {
        "frontend": "v1",
        "generator": {"tool": "gofrontend", "version": "0.1.0", "goroot": "go1.27.1"},
        "package": {"name": "main", "files": ["x.go"], "lang": "go1.27"},
        "types": [{"kind": "basic", "detail": {"name": "int", "class": "int", "untyped": False}}],
        "objects": [{"kind": "var", "name": "x", "type": 0, "param": False}],
        "globals": [],
        "funcs": [{
            "kind": "funcdecl",
            "pos": {"file": 0, "line": 1, "col": 1, "off": 0, "len": 10},
            "fields": {
                "name": "main", "sig": 0, "params": [],
                "body": {"kind": "block",
                         "pos": {"file": 0, "line": 1, "col": 1, "off": 0, "len": 10},
                         "fields": {"stmts": []}},
            },
        }],
    }
    import copy
    cases = []

    def bad(name, mutate, want_substr):
        d = copy.deepcopy(base)
        mutate(d)
        cases.append((name, d, want_substr))

    bad("ok-positive", lambda d: None, None)
    bad("bad-version", lambda d: d.update(frontend="v2"), "frontend")
    bad("unknown-node-kind",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            {"kind": "frobnicate", "pos": {"file": 0, "line": 1, "col": 1, "off": 0, "len": 1}}),
        "unknown node kind")
    bad("dangling-type-id",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            {"kind": "basiclit",
             "pos": {"file": 0, "line": 1, "col": 1, "off": 0, "len": 1},
             "fields": {"value": "1", "type": 99}}),
        "dangling type id")
    bad("dangling-obj-id",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            {"kind": "ident",
             "pos": {"file": 0, "line": 1, "col": 1, "off": 0, "len": 1},
             "fields": {"name": "x", "obj": 42, "type": 0}}),
        "dangling object id")
    bad("globals-not-a-list",
        lambda d: d.update(globals="not-a-list"),
        "must be an array")
    bad("globals-dangling-obj",
        lambda d: d.update(globals=[{"obj": 99999, "init": None,
                                     "pos": {"file": 0, "line": 0, "col": -3,
                                             "off": -1, "len": -9}}]),
        "dangling object id")
    bad("globals-bad-pos",
        lambda d: d.update(globals=[{"obj": 0, "init": None,
                                     "pos": {"file": 0, "line": 0, "col": -3,
                                             "off": -1, "len": -9}}]),
        "must be non-negative int")
    bad("globals-unknown-init-kind",
        lambda d: d.update(globals=[{"obj": 0,
                                     "init": {"kind": "frobnicate",
                                              "pos": {"file": 0, "line": 1, "col": 1,
                                                      "off": 0, "len": 1},
                                              "fields": {}},
                                     "pos": {"file": 0, "line": 1, "col": 1,
                                             "off": 0, "len": 1}}]),
        "unknown node kind")
    bad("ok-globals-sentinel",
        lambda d: d.update(globals=[{"obj": -1, "init": None,
                                     "pos": {"file": 0, "line": 1, "col": 1,
                                             "off": 0, "len": 1}}]),
        None)
    bad("ok-globals-init",
        lambda d: d.update(globals=[{"obj": 0,
                                     "init": {"kind": "basiclit",
                                              "pos": {"file": 0, "line": 1, "col": 1,
                                                      "off": 0, "len": 1},
                                              "fields": {"value": "1", "type": 0}},
                                     "pos": {"file": 0, "line": 1, "col": 1,
                                             "off": 0, "len": 1}}]),
        None)
    bad("missing-pos",
        lambda d: d["funcs"][0]["fields"]["body"].pop("pos"),
        "pos")
    bad("bad-binary-op",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            {"kind": "binary",
             "pos": {"file": 0, "line": 1, "col": 1, "off": 0, "len": 1},
             "fields": {"op": "<=>",
                        "x": {"kind": "basiclit",
                              "pos": {"file": 0, "line": 1, "col": 1, "off": 0, "len": 1},
                              "fields": {"value": "1", "type": 0}},
                        "y": {"kind": "basiclit",
                              "pos": {"file": 0, "line": 1, "col": 1, "off": 0, "len": 1},
                              "fields": {"value": "2", "type": 0}},
                        "type": 0}}),
        "unknown binary op")
    bad("unknown-type-kind",
        lambda d: d["types"].append({"kind": "quux", "detail": {}}),
        "unknown type kind")
    bad("missing-detail-field",
        lambda d: d["types"][0]["detail"].pop("class"),
        "missing detail field")
    bad("convert-dangling-to",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            {"kind": "convert",
             "pos": {"file": 0, "line": 1, "col": 1, "off": 0, "len": 1},
             "fields": {"x": {"kind": "basiclit",
                              "pos": {"file": 0, "line": 1, "col": 1, "off": 0, "len": 1},
                              "fields": {"value": "1", "type": 0}},
                        "to": 99}}),
        "dangling type id")

    # Positive: one minimal node per kind added for NEW-P1-1 (42 exporter
    # kinds + 6 math-intrinsic kinds). Each entry is the exact field
    # shape the exporter emits (see the branch comments); a branch that
    # requires a wrong field fails this case.
    POS = {"file": 0, "line": 1, "col": 1, "off": 0, "len": 1}

    def nd(kind, fields):
        return {"kind": kind, "pos": dict(POS), "fields": fields}

    def lit():
        return nd("basiclit", {"value": "1", "type": 0})

    def call():
        return nd("call", {"fun": nd("ident", {"name": "f", "obj": 0, "type": 0}),
                           "args": [], "type": 0})

    NEW_KIND_FIELDS = {
        "defer": {"call": call()},
        "gostmt": {"call": call()},
        "funclit": {"params": [], "results": [],
                    "body": nd("block", {"stmts": []})},
        "nil": {"type": 0},
        "addrof": {"x": lit(), "type": 0},
        "deref": {"x": lit(), "type": 0},
        "methodref": {"obj": 0, "recv": lit(), "type": 0},
        "methodvalue": {"obj": 0, "recv": lit(), "type": 0},
        "ifacemethod": {"recv": lit(), "method": "M", "iface": 0,
                        "args": [], "sig": 0, "type": 0},
        "reflectop": {"op": "call", "args": [], "type": 0},
        "minmax": {"op": "min", "args": [lit()], "type": 0},
        "clear": {"x": lit()},
        "abifuncpc": {"kind": "FuncPCABI0", "arg": lit(), "type": 0},
        "sliceoverlap": {"x": lit(), "y": lit(), "inexact": False, "type": 0},
        "atomicaddr": {"addr": lit(), "cell": "int64"},
        "atomicptrop": {"op": "swap", "addr": lit(), "args": [], "type": 0},
        "syncop": {"op": "Load", "synctyp": "atomic.Int64",
                   "recv": nd("atomicaddr", {"addr": lit(), "cell": "int64"}),
                   "args": [], "type": 0},
        "runtimecaller": {"skip": lit(), "type": 0},
        "runtimecallers": {"skip": lit(), "pc": lit(), "type": 0},
        "runtimecallersframes": {"pcs": lit(), "type": 0},
        "runtimeframesnext": {"f": lit(), "type": 0},
        "runtimefuncfileline": {"f": lit(), "pc": lit(), "type": 0},
        "runtimefuncforpc": {"pc": lit(), "type": 0},
        "runtimefuncname": {"f": lit(), "type": 0},
        "runtimefuncentry": {"f": lit(), "type": 0},
        "runtimegomaxprocs": {"x": lit(), "type": 0},
        "runtimegoroot": {"type": 0},
        "runtimegosched": {},
        "runtimenumcpu": {"type": 0},
        "runtimenumgoroutine": {"type": 0},
        "runtimereadmemstats": {"m": lit()},
        "runtimestack": {"args": [lit()], "type": 0},
        "runtimeversion": {"type": 0},
        "unsafeadd": {"ptr": lit(), "len": lit(), "type": 0},
        "unsafederive": {"base": lit(), "offset": lit(), "type": 0},
        "unsafefromptr": {"x": lit(), "type": 0},
        "unsafefromuint": {"x": lit(), "type": 0},
        "unsafeslice": {"ptr": lit(), "len": lit(), "type": 0},
        "unsafeslicedata": {"slice": lit(), "type": 0},
        "unsafestring": {"ptr": lit(), "len": lit(), "type": 0},
        "unsafestringdata": {"s": lit(), "type": 0},
        "unsafetooptr": {"x": lit(), "type": 0, "elemSize": 8},
        "mathf64bits": {"x": lit(), "type": 0},
        "mathf64frombits": {"x": lit(), "type": 0},
        "mathf32bits": {"x": lit(), "type": 0},
        "mathf32frombits": {"x": lit(), "type": 0},
        "mathsqrt": {"x": lit(), "type": 0},
        "mathfma": {"x": lit(), "y": lit(), "z": lit(), "type": 0},
    }

    def add_new_kinds(d):
        for kind, fields in NEW_KIND_FIELDS.items():
            d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
                nd(kind, fields))

    bad("ok-new-kinds", add_new_kinds, None)
    bad("defer-missing-call",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            nd("defer", {})),
        "missing field 'call'")
    bad("minmax-bad-op",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            nd("minmax", {"op": "clamp", "args": [], "type": 0})),
        "must be one of")
    bad("reflectop-missing-args",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            nd("reflectop", {"op": "call", "type": 0})),
        "missing field 'args'")
    bad("atomicaddr-bad-cell",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            nd("atomicaddr", {"addr": lit(), "cell": "float64"})),
        "must be one of")
    bad("linkname-with-body",
        lambda d: d["funcs"][0]["fields"].update(linkname="pkg.F"),
        "must not carry a body")
    bad("ok-builtin-ident",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            {"kind": "exprstmt", "pos": dict(POS),
             "fields": {"x": nd("ident", {"name": "len", "builtin": "len"})}}),
        None)
    bad("ok-linkname-funcdecl",
        lambda d: (d["funcs"][0]["fields"].update(linkname="pkg.F"),
                   d["funcs"][0]["fields"].pop("body")),
        None)
    bad("ok-gcinterop-call",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            nd("call", {"gcinterop": "GC", "args": []})),
        None)
    bad("ok-goexit-call",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            nd("call", {"goexit": True, "args": []})),
        None)
    bad("gcinterop-bad-name",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            nd("call", {"gcinterop": "Nope", "args": []})),
        "must be one of")
    bad("marker-call-with-fun",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            nd("call", {"gcinterop": "GC", "fun": lit(), "args": []})),
        "must not carry 'fun'")

    failures = 0
    for name, doc, want in cases:
        errs = validate(doc)
        if want is None:
            if errs:
                print(f"SELFTEST FAIL {name}: positive case rejected: {errs}")
                failures += 1
            else:
                print(f"selftest ok: {name} accepted")
        else:
            if not any(want in e for e in errs):
                print(f"SELFTEST FAIL {name}: expected error containing {want!r}, got {errs}")
                failures += 1
            else:
                print(f"selftest ok: {name} rejected")
    # Kind coverage: every node kind the exporter constructs must have a
    # validator branch. Without this, the next exporter kind silently
    # rejoins the 42-kind gap (NEW-P1-1).
    exp = _exporter_kinds()
    if exp is None:
        print("selftest skip: kind-coverage (exporter sources not found)")
    else:
        val = _validator_kinds()
        missing = sorted(exp - val)
        if missing:
            print(f"SELFTEST FAIL kind-coverage: exporter kinds with no validator branch: {missing}")
            failures += 1
        else:
            print(f"selftest ok: kind-coverage ({len(exp)} exporter kinds all have branches)")
    return failures


def main(argv):
    if "--selftest" in argv:
        return 1 if selftest() else 0
    paths = [a for a in argv[1:] if not a.startswith("-")]
    if not paths:
        print(__doc__)
        return 2
    failed = False
    for p in paths:
        try:
            with open(p) as fh:
                doc = json.load(fh)
        except (OSError, json.JSONDecodeError) as ex:
            print(f"{p}: cannot load: {ex}")
            failed = True
            continue
        errs = validate(doc)
        if errs:
            failed = True
            print(f"{p}: {len(errs)} schema error(s):")
            for e in errs:
                print(f"  {e}")
        else:
            print(f"{p}: valid frontend/v1")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
