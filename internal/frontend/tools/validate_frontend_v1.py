#!/usr/bin/env python3
"""Schema validator for the graalgo frontend/v1 JSON wire format.

Enforces the contract documented in ../../../docs/arch/frontend-v1.md:
  - top-level document shape and "frontend": "v1" version pin
  - type-table entries: known kinds, required detail fields, in-range refs
  - object-table entries
  - AST nodes: known kinds, required fields per kind, positions present,
    every type/obj reference resolves

Usage:
  validate_frontend_v1.py [--selftest] <doc.json> [...]

Exit 0: all documents valid. Exit 1: any validation error (details on stdout).
--selftest runs the embedded negative cases; every one must be rejected.
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
    for k in ("file", "col", "off"):
        v = pos.get(k)
        if not isinstance(v, int) or isinstance(v, bool) or v < 0:
            ctx.err(f"{path}.{k}", f"must be non-negative int, got {v!r}")
    # line: 1-based for real source nodes; 0 marks a synthesized node with
    # no source span (the exporter passes token.NoPos through).
    v = pos.get("line")
    if not isinstance(v, int) or isinstance(v, bool) or v < 0:
        ctx.err(f"{path}.line", f"must be non-negative int, got {v!r}")
    # len: rune length of the node's source text; negative when a
    # synthesized node's End() precedes its Pos() (e.g. helper params).
    v = pos.get("len")
    if not isinstance(v, int) or isinstance(v, bool):
        ctx.err(f"{path}.len", f"must be int, got {v!r}")


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
        # body is ABSENT (not null) on bodyless declarations: //go:linkname
        # bindings (linkname str), runtime intrinsics and assembly stubs
        # (unimplemented: true). The normal form always carries obj.
        for k in ("name", "sig", "params", "obj"):
            need(k)
        if not isinstance(f.get("name"), str):
            ctx.err(fp, "name must be a string")
        ctx.check_obj_id(f"{fp}.obj", f.get("obj"))
        ctx.check_type_id(f"{fp}.sig", f.get("sig"))
        for j, prm in enumerate(f.get("params", [])):
            check_node(ctx, f"{fp}.params[{j}]", prm, False)
        for j, r in enumerate(f.get("results") or []):
            check_node(ctx, f"{fp}.results[{j}]", r, False)
        if f.get("body") is not None:
            check_node(ctx, f"{fp}.body", f["body"], False)
        for k in ("pkgpath", "linkname"):
            if k in f and not isinstance(f[k], str):
                ctx.err(fp, f"{k} must be a string")
        if "unimplemented" in f and not isinstance(f["unimplemented"], bool):
            ctx.err(fp, "unimplemented must be bool")
        if "instof" in f and not isinstance(f["instof"], int):
            ctx.err(fp, "instof must be an int")
        if "typeargs" in f and not isinstance(f["typeargs"], list):
            ctx.err(fp, "typeargs must be a list")
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
        if "builtin" in f:
            # Builtin form: builtins have no go/types signature (NewBuiltin
            # sets typ: Typ[Invalid], which the exporter must never
            # serialize), so the node carries name+builtin and NO obj/type.
            # Builtin calls dispatch via the call node's "builtin" marker.
            if not isinstance(f.get("builtin"), str):
                ctx.err(fp, "builtin must be a string")
            for k in ("obj", "type"):
                if k in f:
                    ctx.err(fp, f"builtin ident must not carry {k!r}")
        else:
            if "obj" in f and f["obj"] is not None:
                ctx.check_obj_id(f"{fp}.obj", f["obj"])
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
        expr_node("fun")
        node_list("args")
        type_ref("type", nullable=True)
        # builtin: str marker the backend's buildBuiltinCall dispatches on
        # (the fun ident is then the builtin form, without obj/type).
        if "builtin" in f and f["builtin"] is not None \
                and not isinstance(f["builtin"], str):
            ctx.err(fp, "builtin must be a string")
        # ellipsis: append(s, x...) spread marker (M7).
        if "ellipsis" in f and not isinstance(f["ellipsis"], bool):
            ctx.err(fp, "ellipsis must be bool")
    elif kind == "convert":
        expr_node("x")
        type_ref("to")
        type_ref("type")
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
            if "targets" in f:
                if not isinstance(f["targets"], list):
                    ctx.err(fp, "targets must be a list of object ids")
                else:
                    for j, t in enumerate(f["targets"]):
                        ctx.check_obj_id(f"{fp}.targets[{j}]", t)
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
    elif kind == "funclit":
        # M5 (WS F): function literal. The main export path stamps "type"
        # (the signature id, for interface boxing); the synthesized go/defer
        # wrappers omit it (the backend derives the result type from
        # results[0] instead).
        node_list("params")
        node_list("results")
        check_node(ctx, f"{fp}.body", f.get("body"), False)
        type_ref("type", nullable=True)
    elif kind == "defer":
        # M5 (WS F): the deferred call, always a "call"-shaped node (the
        # exporter wraps ifacemethod targets in a funclit so buildDefer
        # sees call.fun).
        need("call")
        if "call" in f:
            if not isinstance(f["call"], dict) or f["call"].get("kind") != "call":
                ctx.err(fp, "defer.call must be a call node")
            else:
                check_node(ctx, f"{fp}.call", f["call"], False)
    elif kind == "gostmt":
        # P2-M1 (WS A): go statement; same call-node payload as defer.
        need("call")
        if "call" in f:
            if not isinstance(f["call"], dict) or f["call"].get("kind") != "call":
                ctx.err(fp, "gostmt.call must be a call node")
            else:
                check_node(ctx, f"{fp}.call", f["call"], False)
    elif kind == "nil":
        # M5 (WS F): the nil identifier in expression position.
        type_ref("type")
    elif kind == "addrof":
        # P3-M9 (WS D): &x. Two shapes: {"x"} for a plain addressable
        # operand, {"array","index"} for &a[i] (element pointer; the array
        # may be a slice or a *[N]T dereference).
        if "array" in f or "index" in f:
            expr_node("array")
            expr_node("index")
        else:
            expr_node("x")
        type_ref("type")
    elif kind == "deref":
        # P3-M9 (WS D): *p (explicit or implicit receiver dereference).
        expr_node("x")
        type_ref("type")
    elif kind == "syncop":
        # P3-M2b/M4 (WS D/C): sync primitive or atomic op. recv is the
        # sync value (or the atomicaddr cell for package-level atomics).
        for k in ("op", "synctyp"):
            need(k)
            if not isinstance(f.get(k), str):
                ctx.err(fp, f"{k} must be a string")
        expr_node("recv")
        node_list("args")
        type_ref("type")
    elif kind == "syncnew":
        # No longer emitted by gofrontend (retired); still accepted by the
        # backend's SyncNewNode.build: synctyp names the sync/atomic type,
        # type is the required result type id, newfunc (Pool only) is the
        # New func value expression.
        need("synctyp")
        if not isinstance(f.get("synctyp"), str):
            ctx.err(fp, "synctyp must be a string")
        type_ref("type")
        if f.get("newfunc") is not None:
            check_node(ctx, f"{fp}.newfunc", f["newfunc"], True)
    elif kind == "ifacemethod":
        # P3-M3 (WS D, ADR-0003): interface method call with dynamic itab
        # dispatch. sig/type are the method signature / result type ids;
        # type is null for void calls.
        expr_node("recv")
        need("method")
        if not isinstance(f.get("method"), str):
            ctx.err(fp, "method must be a string")
        type_ref("iface")
        node_list("args")
        type_ref("sig")
        type_ref("type", nullable=True)
    elif kind == "methodref":
        # M6 (WS H): method-call fun target. obj is the method's
        # funcdecl/instantiation object id; recv evaluates first and is
        # passed as params[0]. The backend tolerates a missing/null type.
        need("obj")
        ctx.check_obj_id(f"{fp}.obj", f.get("obj"))
        expr_node("recv")
        type_ref("type", nullable=True)
    elif kind == "methodvalue":
        # P3-M9 (WS H): method value x.M — obj + recv; the backend binds
        # the receiver into a GoFunc and ignores the static type.
        need("obj")
        ctx.check_obj_id(f"{fp}.obj", f.get("obj"))
        expr_node("recv")
        type_ref("type", nullable=True)
    elif kind == "funcfieldcall":
        # P3-M9 (WS D): multi-result call through a struct function field
        # (c.generate(r)). pairType/pairFields describe the synthetic
        # result pair. NOTE: no backend builder is registered for this
        # kind (the sibling desugarFuncFieldCall path emits plain "call");
        # the validator accepts the wire shape, it does not bless the gap.
        expr_node("recv")
        need("field")
        if not isinstance(f.get("field"), str):
            ctx.err(fp, "field must be a string")
        node_list("args")
        type_ref("pairType")
        need("pairFields")
        if not isinstance(f.get("pairFields"), list) or not all(
                isinstance(s, str) for s in f["pairFields"]):
            ctx.err(fp, "pairFields must be a list of strings")
    elif kind == "unsafetooptr":
        # M8: unsafe.Pointer -> *T.
        expr_node("x")
        type_ref("type")
    elif kind == "unsafefromptr":
        # M8: *T (or untyped nil) -> unsafe.Pointer.
        expr_node("x")
        type_ref("type")
    elif kind == "unsafefromuint":
        # M8: uintptr -> unsafe.Pointer.
        expr_node("x")
        type_ref("type")
    elif kind == "unsafeadd":
        # M8/D5: unsafe.Add(ptr, len) — evaluate-then-trap shape.
        expr_node("ptr")
        expr_node("len")
        type_ref("type")
    elif kind == "unsafeslice":
        # M8: unsafe.Slice(ptr, len).
        expr_node("ptr")
        expr_node("len")
        type_ref("type")
    elif kind == "unsafeslicedata":
        # M8: unsafe.SliceData(s).
        expr_node("slice")
        type_ref("type")
    elif kind == "unsafestring":
        # M8: unsafe.String(ptr, len).
        expr_node("ptr")
        expr_node("len")
        type_ref("type")
    elif kind == "unsafestringdata":
        # M8: unsafe.StringData(s).
        expr_node("s")
        type_ref("type")
    elif kind == "abifuncpc":
        # real-reflect (Phase 1): internal/abi FuncPCABI0/FuncPCABIInternal
        # compile intrinsics. The "kind" FIELD names the intrinsic; arg is
        # the func value; type must be the uintptr result id.
        if f.get("kind") not in ("FuncPCABI0", "FuncPCABIInternal"):
            ctx.err(fp, "abifuncpc kind must be 'FuncPCABI0' or "
                        f"'FuncPCABIInternal', got {f.get('kind')!r}")
        expr_node("arg")
        type_ref("type")
    elif kind == "reflectop":
        # real-reflect: __reflect_*-shimmed reflect calls. op names the
        # shim without the __reflect_ prefix.
        need("op")
        if not isinstance(f.get("op"), str):
            ctx.err(fp, "op must be a string")
        node_list("args")
        type_ref("type")
    elif kind == "sliceoverlap":
        # P3-M9: crypto/internal/fips140/alias AnyOverlap/InexactOverlap.
        expr_node("x")
        expr_node("y")
        if not isinstance(f.get("inexact"), bool):
            ctx.err(fp, "inexact must be bool")
        type_ref("type")
    elif kind == "minmax":
        # P4: builtin min/max — op is the builtin name, args the operands.
        need("op")
        if not isinstance(f.get("op"), str):
            ctx.err(fp, "op must be a string")
        node_list("args")
        type_ref("type")
    elif kind == "clear":
        # P4: builtin clear(x) — void, no type field.
        expr_node("x")
    elif kind == "runtimecallers":
        # runtime.Callers(skip, pc).
        expr_node("skip")
        expr_node("pc")
        type_ref("type")
    elif kind == "runtimecallersframes":
        # runtime.CallersFrames(pcs).
        expr_node("pcs")
        type_ref("type")
    elif kind == "runtimefuncforpc":
        # runtime.FuncForPC(pc).
        expr_node("pc")
        type_ref("type")
    elif kind == "runtimefuncname":
        # (*runtime.Func).Name().
        expr_node("f")
        type_ref("type")
    elif kind == "runtimefuncentry":
        # (*runtime.Func).Entry().
        expr_node("f")
        type_ref("type")
    elif kind == "runtimecaller":
        # runtime.Caller(skip) — multi-result desugar (P3-M9, WS D).
        expr_node("skip")
        type_ref("type")
    elif kind == "runtimeframesnext":
        # (*runtime.Frames).Next() — multi-result desugar (P3-M9, WS D).
        expr_node("f")
        type_ref("type")
    elif kind == "runtimefuncfileline":
        # (*runtime.Func).FileLine(pc) — multi-result desugar (P3-M9, WS D).
        expr_node("f")
        expr_node("pc")
        type_ref("type")
    elif kind == "runtimegomaxprocs":
        # P4: runtime.GOMAXPROCS(n).
        expr_node("x")
        type_ref("type")
    elif kind == "runtimegoroot":
        # runtime.GOROOT().
        type_ref("type")
    elif kind == "runtimegosched":
        # PKG-040: runtime.Gosched() — statement, no fields at all.
        pass
    elif kind == "runtimenumcpu":
        # runtime.NumCPU().
        type_ref("type")
    elif kind == "runtimenumgoroutine":
        # PKG-023: runtime.NumGoroutine().
        type_ref("type")
    elif kind == "runtimereadmemstats":
        # runtime.ReadMemStats(m) — void, no type field.
        expr_node("m")
    elif kind == "runtimestack":
        # runtime.Stack(buf, all).
        node_list("args")
        type_ref("type")
    elif kind == "runtimeversion":
        # runtime.Version().
        type_ref("type")
    elif kind in ("mathf64bits", "mathf64frombits", "mathf32bits",
                  "mathf32frombits", "mathsqrt"):
        # P3-M7 (ADR-0007 D5): synthetic math intrinsics, one wire kind
        # each (the time-intrinsic precedent): single operand x.
        expr_node("x")
        type_ref("type")
    elif kind == "mathfma":
        # P3-M7: math.FMA — three operands.
        expr_node("x")
        expr_node("y")
        expr_node("z")
        type_ref("type")
    elif kind == "atomicaddr":
        # fix-linkname: package-level sync/atomic word ops wrap the *T
        # address operand in a delegating AtomicLong / AtomicInteger cell
        # (fields addr and cell only; no type: the node evaluates to the
        # opaque cell object, never directly observed). gc has no
        # package-level bool atomics, so "bool" is not a cell kind.
        if f.get("cell") not in ("int64", "int32"):
            ctx.err(fp, f"atomicaddr cell must be 'int64' or 'int32', got {f.get('cell')!r}")
        expr_node("addr")
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
    funcs = doc.get("funcs")
    if not isinstance(funcs, list) or not funcs:
        ctx.err("funcs", "must be a non-empty array")
    else:
        for i, fn in enumerate(funcs):
            check_node(ctx, f"funcs[{i}]", fn, False)
            if isinstance(fn, dict) and fn.get("kind") != "funcdecl":
                ctx.err(f"funcs[{i}]", "top-level entries must be funcdecl")
    return ctx.errors


def selftest():
    """Every embedded negative case must be rejected; the positive must pass."""
    base = {
        "frontend": "v1",
        "generator": {"tool": "gofrontend", "version": "0.1.0", "goroot": "go1.27.1"},
        "package": {"name": "main", "files": ["x.go"], "lang": "go1.27"},
        "types": [{"kind": "basic", "detail": {"name": "int", "class": "int", "untyped": False}}],
        "objects": [{"kind": "var", "name": "x", "type": 0, "param": False},
                    {"kind": "func", "name": "main", "type": 0, "param": False}],
        "funcs": [{
            "kind": "funcdecl",
            "pos": {"file": 0, "line": 1, "col": 1, "off": 0, "len": 10},
            "fields": {
                "name": "main", "obj": 1, "sig": 0, "params": [],
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

    # fix-linkname: atomicaddr is a live wire kind (package-level
    # sync/atomic word ops). The positive case uses the exact shape the
    # exporter emits (addr expr, cell string); "bool" is not a cell kind
    # (gc has no package-level bool atomics) and must be rejected,
    # matching AtomicAddrNode.build's FrontendException.
    def _atomicaddr_stmt(cell):
        return {"kind": "atomicaddr",
                "pos": {"file": 0, "line": 1, "col": 1, "off": 0, "len": 1},
                "fields": {"addr": {"kind": "basiclit",
                                    "pos": {"file": 0, "line": 1, "col": 1, "off": 0, "len": 1},
                                    "fields": {"value": "0", "type": 0}},
                           "cell": cell}}

    bad("ok-atomicaddr-int64",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            _atomicaddr_stmt("int64")),
        None)
    bad("ok-atomicaddr-int32",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            _atomicaddr_stmt("int32")),
        None)
    bad("bad-atomicaddr-cell",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            _atomicaddr_stmt("bool")),
        "atomicaddr cell")

    # rev-validator-drift: every live wire kind the validator learned gets
    # at least one positive case in the exact shape the exporter emits.
    _POS1 = {"file": 0, "line": 1, "col": 1, "off": 0, "len": 1}

    def _ex():
        return {"kind": "basiclit", "pos": dict(_POS1),
                "fields": {"value": "1", "type": 0}}

    def _blk():
        return {"kind": "block", "pos": dict(_POS1),
                "fields": {"stmts": []}}

    def _call():
        return {"kind": "call", "pos": dict(_POS1),
                "fields": {"fun": _ex(), "args": [], "type": 0}}

    def _add(name, kind, fields):
        node = {"kind": kind, "pos": dict(_POS1), "fields": fields}
        bad("ok-" + name,
            lambda d, n=node: d["funcs"][0]["fields"]["body"]["fields"]
                ["stmts"].append(n),
            None)

    _add("funclit", "funclit",
         {"params": [], "results": [], "body": _blk(), "type": 0})
    _add("funclit-notype", "funclit",
         {"params": [], "results": [], "body": _blk()})
    _add("defer", "defer", {"call": _call()})
    _add("gostmt", "gostmt", {"call": _call()})
    _add("nil", "nil", {"type": 0})
    _add("addrof-x", "addrof", {"x": _ex(), "type": 0})
    _add("addrof-array", "addrof",
         {"array": _ex(), "index": _ex(), "type": 0})
    _add("deref", "deref", {"x": _ex(), "type": 0})
    _add("syncop", "syncop",
         {"op": "Add", "synctyp": "WaitGroup", "recv": _ex(), "args": [],
          "type": 0})
    _add("syncnew", "syncnew", {"synctyp": "Mutex", "type": 0})
    _add("ifacemethod", "ifacemethod",
         {"recv": _ex(), "method": "M", "iface": 0, "args": [], "sig": 0,
          "type": 0})
    _add("methodref", "methodref", {"obj": 1, "recv": _ex(), "type": 0})
    _add("methodvalue", "methodvalue", {"obj": 1, "recv": _ex(), "type": 0})
    _add("funcfieldcall", "funcfieldcall",
         {"recv": _ex(), "field": "generate", "args": [], "pairType": 0,
          "pairFields": ["r1", "r2"]})
    _add("unsafetooptr", "unsafetooptr", {"x": _ex(), "type": 0})
    _add("unsafefromptr", "unsafefromptr", {"x": _ex(), "type": 0})
    _add("unsafefromuint", "unsafefromuint", {"x": _ex(), "type": 0})
    _add("unsafeadd", "unsafeadd",
         {"ptr": _ex(), "len": _ex(), "type": 0})
    _add("unsafeslice", "unsafeslice",
         {"ptr": _ex(), "len": _ex(), "type": 0})
    _add("unsafeslicedata", "unsafeslicedata", {"slice": _ex(), "type": 0})
    _add("unsafestring", "unsafestring",
         {"ptr": _ex(), "len": _ex(), "type": 0})
    _add("unsafestringdata", "unsafestringdata", {"s": _ex(), "type": 0})
    _add("abifuncpc", "abifuncpc",
         {"kind": "FuncPCABI0", "arg": _ex(), "type": 0})
    _add("reflectop", "reflectop", {"op": "ValueOf", "args": [], "type": 0})
    _add("sliceoverlap", "sliceoverlap",
         {"x": _ex(), "y": _ex(), "inexact": False, "type": 0})
    _add("minmax", "minmax", {"op": "min", "args": [_ex()], "type": 0})
    _add("clear", "clear", {"x": _ex()})
    _add("runtimecallers", "runtimecallers",
         {"skip": _ex(), "pc": _ex(), "type": 0})
    _add("runtimecallersframes", "runtimecallersframes",
         {"pcs": _ex(), "type": 0})
    _add("runtimefuncforpc", "runtimefuncforpc", {"pc": _ex(), "type": 0})
    _add("runtimefuncname", "runtimefuncname", {"f": _ex(), "type": 0})
    _add("runtimefuncentry", "runtimefuncentry", {"f": _ex(), "type": 0})
    _add("runtimecaller", "runtimecaller", {"skip": _ex(), "type": 0})
    _add("runtimeframesnext", "runtimeframesnext", {"f": _ex(), "type": 0})
    _add("runtimefuncfileline", "runtimefuncfileline",
         {"f": _ex(), "pc": _ex(), "type": 0})
    _add("runtimegomaxprocs", "runtimegomaxprocs", {"x": _ex(), "type": 0})
    _add("runtimegoroot", "runtimegoroot", {"type": 0})
    _add("runtimegosched", "runtimegosched", {})
    _add("runtimenumcpu", "runtimenumcpu", {"type": 0})
    _add("runtimenumgoroutine", "runtimenumgoroutine", {"type": 0})
    _add("runtimereadmemstats", "runtimereadmemstats", {"m": _ex()})
    _add("runtimestack", "runtimestack", {"args": [], "type": 0})
    _add("runtimeversion", "runtimeversion", {"type": 0})
    _add("mathf64bits", "mathf64bits", {"x": _ex(), "type": 0})
    _add("mathf64frombits", "mathf64frombits", {"x": _ex(), "type": 0})
    _add("mathf32bits", "mathf32bits", {"x": _ex(), "type": 0})
    _add("mathf32frombits", "mathf32frombits", {"x": _ex(), "type": 0})
    _add("mathsqrt", "mathsqrt", {"x": _ex(), "type": 0})
    _add("mathfma", "mathfma",
         {"x": _ex(), "y": _ex(), "z": _ex(), "type": 0})
    # Bodyless funcdecl (linkname/asm stub): no "body" key at all.
    bad("ok-funcdecl-bodyless",
        lambda d: d["funcs"][0]["fields"].pop("body"),
        None)
    # Builtin ident form: name + builtin, no obj/type.
    bad("ok-ident-builtin",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            {"kind": "ident", "pos": dict(_POS1),
             "fields": {"name": "println", "builtin": "println"}}),
        None)
    # Call with the builtin marker the backend dispatches on.
    bad("ok-call-builtin",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            {"kind": "call", "pos": dict(_POS1),
             "fields": {"fun": {"kind": "ident", "pos": dict(_POS1),
                                "fields": {"name": "println",
                                           "builtin": "println"}},
                        "args": [_ex()], "builtin": "println", "type": 0}}),
        None)

    # Negative cases for the closed sets and new shapes.
    bad("bad-defer-call-kind",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            {"kind": "defer", "pos": dict(_POS1),
             "fields": {"call": _blk()}}),
        "must be a call node")
    bad("bad-gostmt-call-nondict",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            {"kind": "gostmt", "pos": dict(_POS1),
             "fields": {"call": "nope"}}),
        "must be a call node")
    bad("bad-addrof-no-operand",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            {"kind": "addrof", "pos": dict(_POS1),
             "fields": {"type": 0}}),
        "missing field 'x'")
    bad("bad-abifuncpc-kind",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            {"kind": "abifuncpc", "pos": dict(_POS1),
             "fields": {"kind": "Nope", "arg": _ex(), "type": 0}}),
        "abifuncpc kind")
    bad("bad-ident-builtin-type",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            {"kind": "ident", "pos": dict(_POS1),
             "fields": {"name": "println", "builtin": "println", "type": 0}}),
        "must not carry")
    bad("bad-ifacemethod-method",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            {"kind": "ifacemethod", "pos": dict(_POS1),
             "fields": {"recv": _ex(), "method": 42, "iface": 0, "args": [],
                        "sig": 0, "type": 0}}),
        "method must be a string")
    bad("bad-funcfieldcall-pairfields",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            {"kind": "funcfieldcall", "pos": dict(_POS1),
             "fields": {"recv": _ex(), "field": "f", "args": [],
                        "pairType": 0, "pairFields": ["r1", 2]}}),
        "pairFields must be a list of strings")
    bad("bad-sliceoverlap-inexact",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            {"kind": "sliceoverlap", "pos": dict(_POS1),
             "fields": {"x": _ex(), "y": _ex(), "inexact": "yes",
                        "type": 0}}),
        "inexact must be bool")
    bad("bad-syncnew-synctyp",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            {"kind": "syncnew", "pos": dict(_POS1),
             "fields": {"synctyp": 7, "type": 0}}),
        "synctyp must be a string")
    bad("bad-mathfma-z",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            {"kind": "mathfma", "pos": dict(_POS1),
             "fields": {"x": _ex(), "y": _ex(), "type": 0}}),
        "missing field 'z'")
    bad("bad-funclit-no-body",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            {"kind": "funclit", "pos": dict(_POS1),
             "fields": {"params": [], "results": []}}),
        "must be an object")
    bad("bad-funcdecl-linkname-type",
        lambda d: d["funcs"][0]["fields"].update(linkname=42),
        "linkname must be a string")
    bad("bad-call-builtin-type",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            {"kind": "call", "pos": dict(_POS1),
             "fields": {"fun": _ex(), "args": [], "builtin": 42,
                        "type": 0}}),
        "builtin must be a string")
    bad("bad-selectcase-targets",
        lambda d: d["funcs"][0]["fields"]["body"]["fields"]["stmts"].append(
            {"kind": "select", "pos": dict(_POS1),
             "fields": {"cases": [
                 {"kind": "selectcase", "pos": dict(_POS1),
                  "fields": {"kind": "recv", "chan": _ex(),
                             "targets": [0, 99], "body": _blk()}}]}}),
        "dangling object id")

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
