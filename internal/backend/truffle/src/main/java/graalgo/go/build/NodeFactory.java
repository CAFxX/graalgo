package graalgo.go.build;

import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.FrameSlotKind;
import graalgo.gir.Gir;
import graalgo.go.loader.FunctionUnit;
import graalgo.go.loader.PackageUnit;
import graalgo.go.loader.TypeEnv;
import graalgo.go.nodes.Arith;
import graalgo.go.nodes.ArithFactory;
import graalgo.go.nodes.Calls;
import graalgo.go.nodes.Chans;
import graalgo.go.nodes.Ctl;
import graalgo.go.nodes.Data;
import graalgo.go.nodes.Ifaces;
import graalgo.go.nodes.Mem;
import graalgo.go.nodes.Mem.Loc;
import graalgo.go.nodes.N;
import graalgo.go.nodes.N.Bool;
import graalgo.go.nodes.N.Expr;
import graalgo.go.nodes.N.F64;
import graalgo.go.nodes.N.I64;
import graalgo.go.nodes.N.Jump;
import graalgo.go.nodes.N.Ref;
import graalgo.go.nodes.N.Stmt;
import graalgo.go.types.GoType;
import graalgo.go.types.GoType.Field;
import graalgo.go.types.Method;
import graalgo.go.types.TypeTable;
import graalgo.go.values.Complex;
import graalgo.go.values.Slice;
import graalgo.go.values.Str;
import graalgo.language.GoLanguage;
import java.math.BigDecimal;
import java.math.BigInteger;
import java.math.MathContext;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.List;
import java.util.function.Consumer;
import java.util.function.Supplier;

/**
 * Builds the Truffle tree of one function instantiation from its decoded GIR body
 * (toolexec-design §4.3, truffle-design §4.2): the factory, not the nodes, selects every
 * operation from the static Go type — carrier, signedness, width, element layout, pointer or
 * value receiver — and resolves locals to frame slots, struct fields to slot offsets, jumps to
 * their target statements and callees to function units. Because a generic body is built once
 * per type environment (D6), all of these decisions are taken on instantiated types.
 *
 * <p>Value semantics: a struct or array expression may yield an object owned by a variable;
 * wherever the value is stored into something that outlives the expression (a variable, an
 * argument, an interface, a channel, a map key) the factory inserts a copy unless the
 * expression is known to yield a fresh object ({@link #fresh}).
 *
 * <p>Evaluation order: an assignment whose operands have side effects evaluates the operands
 * of its targets into temporaries first, then the right-hand sides, then stores (Go spec,
 * "Order of evaluation"); pure assignments skip the temporaries.
 */
public final class NodeFactory {
    /** Thrown for a construct this lane does not build yet; surfaces as an internal error. */
    public static final class Unsupported extends RuntimeException {
        private static final long serialVersionUID = 1L;

        Unsupported(String what) {
            super("graalgo: not supported by the new node package yet: " + what);
        }
    }

    /** A {@code break}/{@code continue} target under construction. */
    private static final class Target {
        final int label;
        final boolean loop;
        Jump brk;
        Jump cont;

        Target(int label, boolean loop) {
            this.label = label;
            this.loop = loop;
        }
    }

    /** Builds the location node for a path with trailing constant offsets. */
    private interface LocMk {
        Loc make(int p, int r);
    }

    /** An assignable operand. */
    private abstract static class LV {
        abstract Expr load();

        abstract Stmt store(Expr value);
    }

    private final FunctionUnit.Instance inst;
    private final PackageUnit pkg;
    private final TypeEnv env;
    private final GoLanguage lang;
    private final TypeTable types;
    private final Gir.Sections sec;
    private final Gir.FuncBody body;
    private final String name;
    private final FrameDescriptor.Builder fdb = FrameDescriptor.newBuilder();
    private final int[] slots;
    private final GoType[] ltypes;
    private final boolean[] boxed;
    private int auxP = -1;
    private int auxR = -1;
    private final ArrayDeque<Target> targets = new ArrayDeque<>();
    private int pendingLabel = -1;
    private final Jump[] gotoJumps;
    private boolean hasDefer;
    /** Defers are open-coded: each site has a bit and two slots (function, arguments). */
    private boolean openDefers;
    private int deferBits = -1;
    private final List<int[]> deferSites = new ArrayList<>();
    private static final int MAX_OPEN_DEFERS = 8;
    private boolean inThunk;
    private int litCount;

    private NodeFactory(FunctionUnit.Instance inst, Gir.FuncBody body, String name) {
        this.inst = inst;
        this.pkg = inst.unit().pkg;
        this.env = inst.env;
        this.lang = pkg.program.language;
        this.types = pkg.program.types;
        this.sec = pkg.sections();
        this.body = body;
        this.name = name;
        int n = body.locals().length;
        this.slots = new int[n];
        this.ltypes = new GoType[n];
        this.boxed = new boolean[n];
        for (int i = 0; i < n; i++) {
            Gir.Local l = body.locals()[i];
            GoType t = pkg.type(l.type(), env);
            ltypes[i] = t;
            boolean escapes = (l.flags() & (8 | 16)) != 0;
            boxed[i] = escapes && t.carrier() != GoType.C_FLAT;
            slots[i] = fdb.addSlot(boxed[i] ? FrameSlotKind.Object : kind(t), pkg.str(l.name()), null);
        }
        this.gotoJumps = new Jump[body.labels().length];
        int[] defers = new int[1];
        boolean[] repeatable = new boolean[1];
        walk(body.body(), s -> {
            if (s instanceof Gir.Stmt.Defer) {
                hasDefer = true;
                defers[0]++;
            } else if (s instanceof Gir.Stmt.Branch b && b.tok() == Gir.BranchTok.GOTO) {
                gotoJumps[b.label()] = new Jump();
                repeatable[0] = true;
            } else if (s instanceof Gir.Stmt.For f) {
                walk(f.body(), inner -> repeatable[0] |= inner instanceof Gir.Stmt.Defer);
                walk(f.post(), inner -> repeatable[0] |= inner instanceof Gir.Stmt.Defer);
            } else if (s instanceof Gir.Stmt.Range r) {
                walk(r.body(), inner -> repeatable[0] |= inner instanceof Gir.Stmt.Defer);
            }
        });
        // gc's rule: open-code only when no defer statement can execute twice.
        this.openDefers = hasDefer && !repeatable[0] && defers[0] <= MAX_OPEN_DEFERS;
        if (openDefers) {
            deferBits = fdb.addSlot(FrameSlotKind.Int, null, null);
        }
    }

    /** The call target of a function instantiation (called once per instance). */
    public static RootCallTarget buildFunction(FunctionUnit.Instance inst) {
        FunctionUnit u = inst.unit();
        Gir.FuncBody b = u.pkg.gir.body(u.index);
        return new NodeFactory(inst, b, u.symbol).buildRoot(0);
    }

    private RootCallTarget buildRoot(int captures) {
        List<Stmt> pro = new ArrayList<>();
        for (int i = 0; i < captures; i++) {
            pro.add(new N.WriteRef(slots[i], new Calls.EnvRead(i)));
        }
        int k = 1;
        if (body.recv() >= 0) {
            pro.add(param(body.recv(), k++));
        }
        for (int p : body.params()) {
            pro.add(param(p, k++));
        }
        for (int r : body.results()) {
            pro.add(declare(r, null));
        }
        Stmt b = block(body.body());
        Expr result = null;
        int[] rs = body.results();
        if (rs.length == 1) {
            result = resultRead(rs[0]);
        } else if (rs.length > 1) {
            Expr[] elems = new Expr[rs.length];
            for (int i = 0; i < rs.length; i++) {
                elems[i] = resultRead(rs[i]);
            }
            result = new Calls.Tuple(elems);
        }
        Stmt[] prologue = pro.toArray(new Stmt[0]);
        FrameDescriptor fd = fdb.build();
        Calls.FuncRoot root;
        if (openDefers) {
            int[] targetSlots = new int[deferSites.size()];
            int[] argsSlots = new int[deferSites.size()];
            for (int i = 0; i < targetSlots.length; i++) {
                targetSlots[i] = deferSites.get(i)[0];
                argsSlots[i] = deferSites.get(i)[1];
            }
            root = new Calls.OpenDeferRoot(lang, fd, name, prologue, b, result, deferBits, targetSlots, argsSlots);
        } else if (hasDefer) {
            root = new Calls.DeferRoot(lang, fd, name, prologue, b, result);
        } else {
            root = new Calls.FuncRoot(lang, fd, name, prologue, b, result);
        }
        return root.getCallTarget();
    }

    private Stmt param(int local, int arg) {
        GoType t = ltypes[local];
        if (t.carrier() == GoType.C_FLAT) {
            return new N.WriteRef(slots[local], new N.Arg(arg));
        }
        return declare(local, typed(t, new N.Arg(arg)));
    }

    /** A result variable's value at return; a shared struct/array result is copied out. */
    private Expr resultRead(int local) {
        Expr r = readLocal(local);
        boolean shared = (body.locals()[local].flags() & (8 | 16)) != 0;
        return shared && ltypes[local].carrier() == GoType.C_FLAT ? new Mem.Copy(r) : r;
    }

    // ---------------------------------------------------------------- small helpers

    private static FrameSlotKind kind(GoType t) {
        switch (t.carrier()) {
            case GoType.C_I64:
                return FrameSlotKind.Long;
            case GoType.C_F64:
                return FrameSlotKind.Double;
            case GoType.C_BOOL:
                return FrameSlotKind.Boolean;
            default:
                return FrameSlotKind.Object;
        }
    }

    private GoType ty(int id) {
        return pkg.type(id, env);
    }

    private GoType ty(Gir.Expr e) {
        return ty(typeId(e));
    }

    private static int typeId(Gir.Expr e) {
        return switch (e) {
            case Gir.Expr.Const x -> x.type();
            case Gir.Expr.Nil x -> x.type();
            case Gir.Expr.Local x -> x.type();
            case Gir.Expr.Global x -> x.type();
            case Gir.Expr.Ext x -> x.type();
            case Gir.Expr.Inst x -> x.type();
            case Gir.Expr.Blank x -> x.type();
            case Gir.Expr.Unary x -> x.type();
            case Gir.Expr.Binary x -> x.type();
            case Gir.Expr.Deref x -> x.type();
            case Gir.Expr.Addr x -> x.type();
            case Gir.Expr.Recv x -> x.type();
            case Gir.Expr.Field x -> x.type();
            case Gir.Expr.Index x -> x.type();
            case Gir.Expr.Slice x -> x.type();
            case Gir.Expr.TypeAssert x -> x.type();
            case Gir.Expr.Convert x -> x.type();
            case Gir.Expr.Call x -> x.type();
            case Gir.Expr.MethodCall x -> x.type();
            case Gir.Expr.IfaceCall x -> x.type();
            case Gir.Expr.BuiltinCall x -> x.type();
            case Gir.Expr.MethodVal x -> x.type();
            case Gir.Expr.IfaceMethodVal x -> x.type();
            case Gir.Expr.MethodExpr x -> x.type();
            case Gir.Expr.IfaceMethodExpr x -> x.type();
            case Gir.Expr.StructLit x -> x.type();
            case Gir.Expr.ArrayLit x -> x.type();
            case Gir.Expr.MapLit x -> x.type();
            case Gir.Expr.FuncLit x -> x.type();
            case Gir.Expr.TupleConvert x -> x.type();
        };
    }

    private void aux() {
        if (auxP < 0) {
            auxP = fdb.addSlot(FrameSlotKind.Int, null, null);
            auxR = fdb.addSlot(FrameSlotKind.Int, null, null);
        }
    }

    private int temp(GoType t) {
        return fdb.addSlot(kind(t), null, null);
    }

    private int tempObject() {
        return fdb.addSlot(FrameSlotKind.Object, null, null);
    }

    private int tempLong() {
        return fdb.addSlot(FrameSlotKind.Long, null, null);
    }

    private static Stmt writeSlot(int slot, GoType t, Expr v) {
        switch (t.carrier()) {
            case GoType.C_I64:
                return new N.WriteI64(slot, asI64(v));
            case GoType.C_F64:
                return new N.WriteF64(slot, asF64(v));
            case GoType.C_BOOL:
                return new N.WriteBool(slot, asBool(v));
            default:
                return new N.WriteRef(slot, v);
        }
    }

    private static Expr readSlot(int slot, GoType t) {
        switch (t.carrier()) {
            case GoType.C_I64:
                return new N.ReadI64(slot);
            case GoType.C_F64:
                return new N.ReadF64(slot);
            case GoType.C_BOOL:
                return new N.ReadBool(slot);
            default:
                return new N.ReadRef(slot);
        }
    }

    private static I64 asI64(Expr n) {
        return n instanceof I64 i ? i : new N.AsI64(n);
    }

    private static F64 asF64(Expr n) {
        return n instanceof F64 f ? f : new N.AsF64(n);
    }

    private static Bool asBool(Expr n) {
        return n instanceof Bool b ? b : new N.AsBool(n);
    }

    private static Ref asRef(Expr n) {
        return n instanceof Ref r ? r : new N.AsRef(n);
    }

    /** {@code n} in the node family of type {@code t} (unboxing a generic node). */
    private static Expr typed(GoType t, Expr n) {
        switch (t.carrier()) {
            case GoType.C_I64:
                return asI64(n);
            case GoType.C_F64:
                return asF64(n);
            case GoType.C_BOOL:
                return asBool(n);
            default:
                return n;
        }
    }

    /** The scalar-slot encoding of a value of type {@code t}. */
    private static I64 raw(GoType t, Expr n) {
        switch (t.carrier()) {
            case GoType.C_I64:
                return asI64(n);
            case GoType.C_F64:
                return new N.F64Raw(asF64(n));
            case GoType.C_BOOL:
                return new N.BoolRaw(asBool(n));
            default:
                throw new IllegalStateException("no scalar slot for " + t.key);
        }
    }

    private static Expr load(GoType t, Loc loc) {
        switch (t.carrier()) {
            case GoType.C_I64:
                return new Mem.LoadI64(loc);
            case GoType.C_F64:
                return new N.RawF64(new Mem.LoadI64(loc));
            case GoType.C_BOOL:
                return new N.RawBool(new Mem.LoadI64(loc));
            case GoType.C_REF:
                return new Mem.LoadRef(loc);
            default:
                return new Mem.LoadSub(loc, t);
        }
    }

    private static Stmt store(GoType t, Loc loc, Expr v) {
        switch (t.carrier()) {
            case GoType.C_REF:
                return new Mem.StoreRef(loc, v);
            case GoType.C_FLAT:
                return new Mem.StoreSub(loc, v);
            default:
                return new Mem.StoreI64(loc, raw(t, v));
        }
    }

    private static Object zeroRef(GoType t) {
        switch (t.u().kind) {
            case GoType.STRING:
                return Str.EMPTY;
            case GoType.SLICE:
                return Slice.NIL;
            case GoType.COMPLEX64, GoType.COMPLEX128:
                return Complex.ZERO;
            default:
                return null;
        }
    }

    private static Expr zero(GoType t) {
        switch (t.carrier()) {
            case GoType.C_I64:
                return new N.ConstI64(0);
            case GoType.C_F64:
                return new N.ConstF64(0);
            case GoType.C_BOOL:
                return new N.ConstBool(false);
            case GoType.C_REF:
                return new N.ConstRef(zeroRef(t));
            default:
                return new Mem.Zero(t);
        }
    }

    private static I64 wrap(GoType t, I64 n) {
        GoType u = t.u();
        switch (u.kind) {
            case GoType.INT8:
                return ArithFactory.W8NodeGen.create(n);
            case GoType.INT16:
                return ArithFactory.W16NodeGen.create(n);
            case GoType.INT32:
                return ArithFactory.W32NodeGen.create(n);
            case GoType.UINT8:
                return ArithFactory.U8NodeGen.create(n);
            case GoType.UINT16:
                return ArithFactory.U16NodeGen.create(n);
            case GoType.UINT32:
                return ArithFactory.U32NodeGen.create(n);
            default:
                return n;
        }
    }

    private static boolean u64(GoType t) {
        return t.isUnsigned() && t.bits() == 64;
    }

    /** Does evaluating {@code e} have no effect and depend on no call? */
    private static boolean pure(Gir.Expr e) {
        return switch (e) {
            case null -> true;
            case Gir.Expr.Const x -> true;
            case Gir.Expr.Nil x -> true;
            case Gir.Expr.Local x -> true;
            case Gir.Expr.Global x -> true;
            case Gir.Expr.Ext x -> true;
            case Gir.Expr.Blank x -> true;
            case Gir.Expr.Unary x -> pure(x.x());
            case Gir.Expr.Binary x -> pure(x.x()) && pure(x.y());
            case Gir.Expr.Field x -> pure(x.x());
            case Gir.Expr.Deref x -> pure(x.x());
            case Gir.Expr.Index x -> pure(x.x()) && pure(x.index());
            case Gir.Expr.Convert x -> pure(x.x());
            default -> false;
        };
    }

    private static boolean constant(Gir.Expr e) {
        return e instanceof Gir.Expr.Const || e instanceof Gir.Expr.Nil;
    }

    /** Does {@code e} yield a struct/array object nobody else holds? */
    private static boolean fresh(Gir.Expr e) {
        return switch (e) {
            case Gir.Expr.Local x -> false;
            case Gir.Expr.TypeAssert x -> false;
            case Gir.Expr.Recv x -> false;
            case Gir.Expr.Convert x -> fresh(x.x());
            default -> true;
        };
    }

    private static void walk(Gir.Stmt[] ss, Consumer<Gir.Stmt> f) {
        for (Gir.Stmt s : ss) {
            walk(s, f);
        }
    }

    private static void walk(Gir.Stmt s, Consumer<Gir.Stmt> f) {
        if (s == null) {
            return;
        }
        f.accept(s);
        switch (s) {
            case Gir.Stmt.Labeled x -> walk(x.stmt(), f);
            case Gir.Stmt.Block x -> walk(x.stmts(), f);
            case Gir.Stmt.If x -> {
                walk(x.init(), f);
                walk(x.then(), f);
                walk(x.else_(), f);
            }
            case Gir.Stmt.Switch x -> {
                walk(x.init(), f);
                for (Gir.CaseClause c : x.clauses()) {
                    walk(c.body(), f);
                }
            }
            case Gir.Stmt.TypeSwitch x -> {
                walk(x.init(), f);
                for (Gir.TypeClause c : x.clauses()) {
                    walk(c.body(), f);
                }
            }
            case Gir.Stmt.Select x -> {
                for (Gir.CommClause c : x.clauses()) {
                    walk(c.body(), f);
                }
            }
            case Gir.Stmt.For x -> {
                walk(x.init(), f);
                walk(x.post(), f);
                walk(x.body(), f);
            }
            case Gir.Stmt.Range x -> walk(x.body(), f);
            default -> {
            }
        }
    }

    // ---------------------------------------------------------------- locals

    private Expr readLocal(int i) {
        if (boxed[i]) {
            return load(ltypes[i], new Mem.FlatLoc(new N.ReadRef(slots[i]), 0, 0));
        }
        return readSlot(slots[i], ltypes[i]);
    }

    /** Declares local {@code i}: a fresh variable (a fresh box if it escapes), zero or {@code init}. */
    private Stmt declare(int i, Expr init) {
        GoType t = ltypes[i];
        if (boxed[i]) {
            if (init == null) {
                return new N.WriteRef(slots[i], new Mem.Zero(t));
            }
            Data.Init in = t.carrier() == GoType.C_REF ? new Data.InitRef(0, init) : new Data.InitPrim(0, raw(t, init));
            return new N.WriteRef(slots[i], new Data.FlatLit(t, new Data.Init[] {in}));
        }
        return writeSlot(slots[i], t, init == null ? zero(t) : init);
    }

    /** Assigns to the existing local {@code i}. A struct/array is copied into its object. */
    private Stmt assignLocal(int i, Expr v) {
        GoType t = ltypes[i];
        if (boxed[i] || t.carrier() == GoType.C_FLAT) {
            return store(t, new Mem.FlatLoc(new N.ReadRef(slots[i]), 0, 0), v);
        }
        return writeSlot(slots[i], t, v);
    }

    // ---------------------------------------------------------------- locations

    /** An operand evaluated once: directly when no spill is requested or it is constant. */
    private Supplier<Expr> operand(Gir.Expr e, List<Stmt> pre) {
        if (pre == null || constant(e)) {
            return () -> expr(e);
        }
        GoType t = ty(e);
        int slot = temp(t);
        pre.add(writeSlot(slot, t, expr(e)));
        return () -> readSlot(slot, t);
    }

    /** The location of an addressable (or at least memory-backed) expression, or null. */
    private LocMk locOf(Gir.Expr e, List<Stmt> pre) {
        switch (e) {
            case Gir.Expr.Local l: {
                int i = l.local();
                if (!boxed[i] && ltypes[i].carrier() != GoType.C_FLAT) {
                    return null;
                }
                return (p, r) -> new Mem.FlatLoc(new N.ReadRef(slots[i]), p, r);
            }
            case Gir.Expr.Global g:
                return globalLoc(pkg, g.obj());
            case Gir.Expr.Ext x: {
                Gir.SymRef sym = sec.symRefs[x.sym()];
                PackageUnit u = pkg.packageOf(sym);
                return globalLoc(u, u.objectNamed(pkg.str(sym.name())));
            }
            case Gir.Expr.Deref d: {
                Supplier<Expr> ptr = operand(d.x(), pre);
                aux();
                return (p, r) -> new Mem.PtrLoc(asRef(ptr.get()), p, r, auxP, auxR);
            }
            case Gir.Expr.Field f:
                return fieldLoc(f, pre);
            case Gir.Expr.Index ix:
                return indexLoc(ix, pre);
            default: {
                // A struct or array rvalue (call result, literal): the value object itself.
                if (ty(e).carrier() != GoType.C_FLAT) {
                    return null;
                }
                Supplier<Expr> v = operand(e, pre);
                return (p, r) -> new Mem.FlatLoc(asRef(v.get()), p, r);
            }
        }
    }

    private LocMk globalLoc(PackageUnit u, int obj) {
        if (u.sections().objects[obj].kind() != Gir.SymKind.VAR) {
            return null;
        }
        int gp = u.globalPrim(obj);
        int gr = u.globalRef(obj);
        int index = u.index;
        return (p, r) -> new Mem.FlatLoc(new Mem.Globals(index), gp + p, gr + r);
    }

    /** The location of a struct-or-pointer-to-struct operand, dereferencing a pointer. */
    private LocMk structBase(Gir.Expr x, List<Stmt> pre) {
        GoType t = ty(x).u();
        if (t.kind == GoType.POINTER) {
            Supplier<Expr> ptr = operand(x, pre);
            aux();
            return (p, r) -> new Mem.PtrLoc(asRef(ptr.get()), p, r, auxP, auxR);
        }
        LocMk base = locOf(x, pre);
        if (base == null) {
            throw new IllegalStateException("no location for operand of type " + t.key);
        }
        return base;
    }

    private LocMk fieldLoc(Gir.Expr.Field f, List<Stmt> pre) {
        LocMk cur = structBase(f.x(), pre);
        GoType t = ty(f.x()).u();
        if (t.kind == GoType.POINTER) {
            t = t.elem.u();
        }
        long[] path = f.path();
        for (int k = 0; k < path.length; k++) {
            t.nPrim();
            Field fld = t.fields[(int) path[k]];
            LocMk parent = cur;
            int fp = fld.primOff;
            int fr = fld.refOff;
            cur = (p, r) -> parent.make(fp + p, fr + r);
            GoType ft = fld.type.u();
            if (k < path.length - 1 && ft.kind == GoType.POINTER) {
                // Implicit dereference of an embedded pointer.
                LocMk ptrField = cur;
                aux();
                cur = (p, r) -> new Mem.PtrLoc(new Mem.LoadRef(ptrField.make(0, 0)), p, r, auxP, auxR);
                ft = ft.elem.u();
            }
            t = ft;
        }
        return cur;
    }

    private LocMk indexLoc(Gir.Expr.Index ix, List<Stmt> pre) {
        GoType xt = ty(ix.x()).u();
        aux();
        switch (xt.kind) {
            case GoType.SLICE: {
                Supplier<Expr> s = operand(ix.x(), pre);
                Supplier<Expr> i = operand(ix.index(), pre);
                int np = xt.elem.nPrim();
                int nr = xt.elem.nRef();
                return (p, r) -> new Mem.SliceLoc(asRef(s.get()), asI64(i.get()), np, nr, p, r, auxP, auxR);
            }
            case GoType.ARRAY, GoType.POINTER: {
                GoType at = xt.kind == GoType.POINTER ? xt.elem.u() : xt;
                LocMk base = structBase(ix.x(), pre);
                Supplier<Expr> i = operand(ix.index(), pre);
                int np = at.elem.nPrim();
                int nr = at.elem.nRef();
                long len = at.len;
                return (p, r) -> new Mem.IndexLoc(base.make(0, 0), asI64(i.get()), len, np, nr, p, r, auxP, auxR);
            }
            case GoType.MAP: {
                Supplier<Expr> m = operand(ix.x(), pre);
                Supplier<Expr> k = operand(ix.index(), pre);
                GoType kt = xt.mapKey;
                GoType vt = xt.elem;
                if (primKey(kt)) {
                    return (p, r) -> new Mem.MapGetI(asRef(m.get()), raw(kt, k.get()), vt.zeroFlat(), p, r, auxP,
                            auxR);
                }
                return (p, r) -> new Mem.MapGetG(asRef(m.get()), k.get(), vt.zeroFlat(), p, r, auxP, auxR);
            }
            default:
                return null;
        }
    }

    private static boolean primKey(GoType kt) {
        int c = kt.carrier();
        return c == GoType.C_I64 || c == GoType.C_BOOL;
    }

    /** The assignable form of {@code e}; operands are spilled into {@code pre} when it is non-null. */
    private LV lvalue(Gir.Expr e, List<Stmt> pre) {
        GoType t = ty(e);
        if (e instanceof Gir.Expr.Blank) {
            return new LV() {
                @Override
                Expr load() {
                    throw new IllegalStateException("read of _");
                }

                @Override
                Stmt store(Expr value) {
                    return new N.ExprStmt(value);
                }
            };
        }
        if (e instanceof Gir.Expr.Local l && !boxed[l.local()] && t.carrier() != GoType.C_FLAT) {
            int i = l.local();
            return new LV() {
                @Override
                Expr load() {
                    return readLocal(i);
                }

                @Override
                Stmt store(Expr value) {
                    return assignLocal(i, value);
                }
            };
        }
        if (e instanceof Gir.Expr.Index ix && ty(ix.x()).u().kind == GoType.MAP) {
            GoType mt = ty(ix.x()).u();
            Supplier<Expr> m = operand(ix.x(), pre);
            GoType kt = mt.mapKey;
            Supplier<Expr> k;
            if (pre == null || constant(ix.index())) {
                k = () -> value(ix.index());
            } else {
                int slot = temp(kt);
                pre.add(writeSlot(slot, kt, value(ix.index())));
                k = () -> readSlot(slot, kt);
            }
            aux();
            return new LV() {
                @Override
                Expr load() {
                    Loc loc = primKey(kt)
                            ? new Mem.MapGetI(asRef(m.get()), raw(kt, k.get()), mt.elem.zeroFlat(), 0, 0, auxP, auxR)
                            : new Mem.MapGetG(asRef(m.get()), k.get(), mt.elem.zeroFlat(), 0, 0, auxP, auxR);
                    return NodeFactory.load(t, loc);
                }

                @Override
                Stmt store(Expr value) {
                    Loc loc = primKey(kt) ? new Mem.MapPutI(asRef(m.get()), raw(kt, k.get()), auxP, auxR)
                            : new Mem.MapPutG(asRef(m.get()), k.get(), auxP, auxR);
                    return NodeFactory.store(t, loc, value);
                }
            };
        }
        LocMk mk = locOf(e, pre);
        if (mk == null) {
            throw new IllegalStateException("not assignable: " + e.getClass().getSimpleName());
        }
        return new LV() {
            @Override
            Expr load() {
                return NodeFactory.load(t, mk.make(0, 0));
            }

            @Override
            Stmt store(Expr value) {
                return NodeFactory.store(t, mk.make(0, 0), value);
            }
        };
    }

    // ---------------------------------------------------------------- expressions

    /** {@code e} as a value that may be stored: a private copy of a borrowed struct/array. */
    private Expr value(Gir.Expr e) {
        Expr n = expr(e);
        return ty(e).carrier() == GoType.C_FLAT && !fresh(e) ? new Mem.Copy(n) : n;
    }

    private I64 i64(Gir.Expr e) {
        return asI64(expr(e));
    }

    private Bool bool(Gir.Expr e) {
        return asBool(expr(e));
    }

    private Expr expr(Gir.Expr e) {
        switch (e) {
            case Gir.Expr.Const c:
                return constant(ty(c.type()), c.value());
            case Gir.Expr.Nil n:
                return new N.ConstRef(zeroRef(ty(n.type())));
            case Gir.Expr.Local l:
                return readLocal(l.local());
            case Gir.Expr.Global g:
                return global(pkg, g.obj(), ty(g.type()), e);
            case Gir.Expr.Ext x: {
                Gir.SymRef sym = sec.symRefs[x.sym()];
                PackageUnit u = pkg.packageOf(sym);
                return global(u, u.objectNamed(pkg.str(sym.name())), ty(x.type()), e);
            }
            case Gir.Expr.Inst in: {
                FunctionUnit.Instance fi = instanceOf(in);
                return new Calls.FuncConst(fi::closure);
            }
            case Gir.Expr.Blank b:
                throw new IllegalStateException("_ used as a value");
            case Gir.Expr.Unary u:
                return unary(u);
            case Gir.Expr.Binary b:
                return binary(b.op(), ty(b.x()), ty(b.y()), b.x(), b.y(), () -> expr(b.x()), () -> expr(b.y()),
                        ty(b.type()));
            case Gir.Expr.Deref d:
                aux();
                return load(ty(d.type()), new Mem.PtrLoc(asRef(expr(d.x())), 0, 0, auxP, auxR));
            case Gir.Expr.Addr a:
                return addr(a);
            case Gir.Expr.Recv r: {
                Expr ch = expr(r.ch());
                return r.commaOk() ? new Chans.RecvOk(ch) : typed(ty(r.type()), new Chans.Recv(ch));
            }
            case Gir.Expr.Field f:
                return load(ty(f.type()), fieldLoc(f, null).make(0, 0));
            case Gir.Expr.Index ix:
                return index(ix);
            case Gir.Expr.Slice s:
                return slice(s);
            case Gir.Expr.TypeAssert ta:
                return typeAssert(ta);
            case Gir.Expr.Convert c:
                return convert(ty(c.x()), ty(c.type()), c.x());
            case Gir.Expr.Call c:
                return call(c);
            case Gir.Expr.MethodCall mc:
                return typed(ty(mc.type()), new Calls.Call(methodInstance(mc.method(), mc.recvType(), mc.targs()),
                        withRecv(value(mc.recv()), args(mc.args(), methodSig(mc.method(), mc.recvType(), mc.targs()),
                                mc.spread()))));
            case Gir.Expr.IfaceCall ic:
                return ifaceCall(ic);
            case Gir.Expr.BuiltinCall bc:
                return builtin(bc, null);
            case Gir.Expr.MethodVal mv:
                return methodValue(mv);
            case Gir.Expr.IfaceMethodVal mv:
                return ifaceMethodValue(mv);
            case Gir.Expr.MethodExpr me:
                return methodExpr(me);
            case Gir.Expr.IfaceMethodExpr me:
                return ifaceMethodExpr(me);
            case Gir.Expr.StructLit sl:
                return structLit(sl);
            case Gir.Expr.ArrayLit al:
                return arrayLit(al);
            case Gir.Expr.MapLit ml:
                return mapLit(ml);
            case Gir.Expr.FuncLit fl:
                return funcLit(fl);
            case Gir.Expr.TupleConvert tc:
                return tupleConvert(tc);
        }
    }

    private Expr global(PackageUnit u, int obj, GoType t, Gir.Expr e) {
        Gir.Object o = u.sections().objects[obj];
        if (o.kind() == Gir.SymKind.FUNC) {
            FunctionUnit.Instance fi = u.func(o.fn()).instance(null);
            return new Calls.FuncConst(fi::closure);
        }
        LocMk mk = globalLoc(u, obj);
        if (mk == null) {
            throw new IllegalStateException("not a variable: " + u.path + "." + u.str(o.name()));
        }
        return load(t, mk.make(0, 0));
    }

    private Expr constant(GoType t, Gir.ConstValue v) {
        String text = pkg.str(v.text());
        GoType u = t.u();
        if (u.kind == GoType.INTERFACE) {
            throw new IllegalStateException("constant of interface type");
        }
        switch (v.kind()) {
            case Gir.ConstKind.BOOL:
                return new N.ConstBool(text.equals("true"));
            case Gir.ConstKind.STRING:
                return new N.ConstRef(new Str(pkg.gir.stringBytes(v.text())));
            case Gir.ConstKind.COMPLEX: {
                double re = rational(text).doubleValue();
                double im = rational(pkg.str(v.imag())).doubleValue();
                if (u.kind == GoType.COMPLEX64) {
                    return new N.ConstRef(new Complex(rational(text).floatValue(),
                            rational(pkg.str(v.imag())).floatValue()));
                }
                return new N.ConstRef(new Complex(re, im));
            }
            default:
                break;
        }
        if (u.isInteger()) {
            BigInteger i = text.indexOf('/') >= 0 || text.indexOf('.') >= 0 || text.indexOf('e') >= 0
                    ? rational(text).toBigInteger() : new BigInteger(text);
            return new N.ConstI64(i.longValue());
        } else if (u.isFloat()) {
            BigDecimal d = rational(text);
            return new N.ConstF64(u.kind == GoType.FLOAT32 ? d.floatValue() : d.doubleValue());
        } else if (u.isComplex()) {
            BigDecimal d = rational(text);
            return new N.ConstRef(new Complex(u.kind == GoType.COMPLEX64 ? d.floatValue() : d.doubleValue(), 0));
        }
        throw new IllegalStateException("constant " + text + " of type " + t.key);
    }

    /** Parses constant.Value.ExactString: a decimal or a fraction a/b. */
    private static BigDecimal rational(String text) {
        int slash = text.indexOf('/');
        if (slash < 0) {
            return new BigDecimal(text);
        }
        BigDecimal num = new BigDecimal(text.substring(0, slash));
        BigDecimal den = new BigDecimal(text.substring(slash + 1));
        return num.divide(den, new MathContext(60));
    }

    private Expr unary(Gir.Expr.Unary u) {
        GoType t = ty(u.type());
        Expr x = expr(u.x());
        switch (u.op()) {
            case Gir.UnOp.POS:
                return x;
            case Gir.UnOp.NOT:
                return new Arith.Not(asBool(x));
            case Gir.UnOp.BIT_NOT:
                return wrap(t, ArithFactory.NotI64NodeGen.create(asI64(x)));
            default:
                if (t.isInteger()) {
                    return wrap(t, ArithFactory.NegI64NodeGen.create(asI64(x)));
                } else if (t.isFloat()) {
                    return new Arith.NegF64(asF64(x));
                }
                return new Arith.NegC(x);
        }
    }

    /**
     * A binary operation on operands of type {@code lt} (shift counts: {@code rt}). The operand
     * expressions (for the nil tests) are optional; the node suppliers are called exactly once.
     */
    private Expr binary(int op, GoType lt, GoType rt, Gir.Expr lx, Gir.Expr rx, Supplier<Expr> ls, Supplier<Expr> rs,
            GoType result) {
        GoType u = lt.u();
        switch (op) {
            case Gir.BinOp.LAND:
                return new Arith.AndAnd(asBool(ls.get()), asBool(rs.get()));
            case Gir.BinOp.LOR:
                return new Arith.OrOr(asBool(ls.get()), asBool(rs.get()));
            case Gir.BinOp.EQL:
                return equal(lt, rt, lx, rx, ls, rs);
            case Gir.BinOp.NEQ:
                return new Arith.Not(equal(lt, rt, lx, rx, ls, rs));
            case Gir.BinOp.LSS, Gir.BinOp.LEQ, Gir.BinOp.GTR, Gir.BinOp.GEQ:
                return order(op, u, ls.get(), rs.get());
            default:
                break;
        }
        Expr l = ls.get();
        Expr r = rs.get();
        if (u.isInteger()) {
            I64 a = asI64(l);
            I64 b = asI64(r);
            switch (op) {
                case Gir.BinOp.ADD:
                    return wrap(u, ArithFactory.AddI64NodeGen.create(a, b));
                case Gir.BinOp.SUB:
                    return wrap(u, ArithFactory.SubI64NodeGen.create(a, b));
                case Gir.BinOp.MUL:
                    return wrap(u, ArithFactory.MulI64NodeGen.create(a, b));
                case Gir.BinOp.QUO:
                    return u64(u) ? ArithFactory.DivU64NodeGen.create(a, b)
                            : wrap(u, ArithFactory.DivI64NodeGen.create(a, b));
                case Gir.BinOp.REM:
                    return u64(u) ? ArithFactory.RemU64NodeGen.create(a, b) : ArithFactory.RemI64NodeGen.create(a, b);
                case Gir.BinOp.AND:
                    return ArithFactory.AndI64NodeGen.create(a, b);
                case Gir.BinOp.OR:
                    return ArithFactory.OrI64NodeGen.create(a, b);
                case Gir.BinOp.XOR:
                    return ArithFactory.XorI64NodeGen.create(a, b);
                case Gir.BinOp.AND_NOT:
                    return ArithFactory.AndNotI64NodeGen.create(a, b);
                case Gir.BinOp.SHL:
                    return wrap(u, ArithFactory.ShlI64NodeGen.create(a, shiftCount(rt, rx, b)));
                case Gir.BinOp.SHR:
                    return u.isUnsigned() ? ArithFactory.ShrU64NodeGen.create(a, shiftCount(rt, rx, b))
                            : ArithFactory.ShrI64NodeGen.create(a, shiftCount(rt, rx, b));
                default:
                    break;
            }
        } else if (u.isFloat()) {
            F64 a = asF64(l);
            F64 b = asF64(r);
            F64 n;
            switch (op) {
                case Gir.BinOp.ADD:
                    n = ArithFactory.AddF64NodeGen.create(a, b);
                    break;
                case Gir.BinOp.SUB:
                    n = ArithFactory.SubF64NodeGen.create(a, b);
                    break;
                case Gir.BinOp.MUL:
                    n = ArithFactory.MulF64NodeGen.create(a, b);
                    break;
                case Gir.BinOp.QUO:
                    n = ArithFactory.DivF64NodeGen.create(a, b);
                    break;
                default:
                    throw new IllegalStateException("float operator " + Gir.BinOp.name(op));
            }
            return u.kind == GoType.FLOAT32 ? new Arith.RoundF32(n) : n;
        } else if (u.kind == GoType.STRING && op == Gir.BinOp.ADD) {
            return ArithFactory.ConcatNodeGen.create(l, r);
        } else if (u.isComplex()) {
            boolean single = u.kind == GoType.COMPLEX64;
            switch (op) {
                case Gir.BinOp.ADD:
                    return single ? ArithFactory.AddC64NodeGen.create(l, r) : ArithFactory.AddCNodeGen.create(l, r);
                case Gir.BinOp.SUB:
                    return single ? ArithFactory.SubC64NodeGen.create(l, r) : ArithFactory.SubCNodeGen.create(l, r);
                case Gir.BinOp.MUL:
                    return single ? ArithFactory.MulC64NodeGen.create(l, r) : ArithFactory.MulCNodeGen.create(l, r);
                case Gir.BinOp.QUO:
                    return new Arith.DivC(l, r, single);
                default:
                    break;
            }
        }
        throw new IllegalStateException("operator " + Gir.BinOp.name(op) + " on " + lt.key);
    }

    private I64 shiftCount(GoType rt, Gir.Expr rx, I64 count) {
        if (rt.isUnsigned() || rx instanceof Gir.Expr.Const) {
            return count;
        }
        return ArithFactory.ShiftCountNodeGen.create(count);
    }

    private Bool order(int op, GoType u, Expr l, Expr r) {
        if (u.isInteger()) {
            I64 a = asI64(l);
            I64 b = asI64(r);
            boolean uns = u64(u);
            switch (op) {
                case Gir.BinOp.LSS:
                    return uns ? ArithFactory.LtU64NodeGen.create(a, b) : ArithFactory.LtI64NodeGen.create(a, b);
                case Gir.BinOp.LEQ:
                    return uns ? ArithFactory.LeU64NodeGen.create(a, b) : ArithFactory.LeI64NodeGen.create(a, b);
                case Gir.BinOp.GTR:
                    return uns ? ArithFactory.GtU64NodeGen.create(a, b) : ArithFactory.GtI64NodeGen.create(a, b);
                default:
                    return uns ? ArithFactory.GeU64NodeGen.create(a, b) : ArithFactory.GeI64NodeGen.create(a, b);
            }
        } else if (u.isFloat()) {
            F64 a = asF64(l);
            F64 b = asF64(r);
            switch (op) {
                case Gir.BinOp.LSS:
                    return ArithFactory.LtF64NodeGen.create(a, b);
                case Gir.BinOp.LEQ:
                    return ArithFactory.LeF64NodeGen.create(a, b);
                case Gir.BinOp.GTR:
                    return ArithFactory.GtF64NodeGen.create(a, b);
                default:
                    return ArithFactory.GeF64NodeGen.create(a, b);
            }
        }
        switch (op) {
            case Gir.BinOp.LSS:
                return ArithFactory.LtStrNodeGen.create(l, r);
            case Gir.BinOp.LEQ:
                return ArithFactory.LeStrNodeGen.create(l, r);
            case Gir.BinOp.GTR:
                return ArithFactory.GtStrNodeGen.create(l, r);
            default:
                return ArithFactory.GeStrNodeGen.create(l, r);
        }
    }

    private Bool equal(GoType lt, GoType rt, Gir.Expr lx, Gir.Expr rx, Supplier<Expr> ls, Supplier<Expr> rs) {
        GoType u = lt.u();
        boolean lnil = lx instanceof Gir.Expr.Nil;
        boolean rnil = rx instanceof Gir.Expr.Nil;
        if (lnil || rnil) {
            GoType other = (lnil ? rt : lt).u();
            Expr x = lnil ? rs.get() : ls.get();
            return other.kind == GoType.SLICE ? new Arith.IsNilSlice(x) : new Arith.IsNil(x);
        }
        if (lt.isInterface() != rt.isInterface()) {
            // Mixed comparison: the concrete operand is converted to the interface type.
            Expr l = lt.isInterface() ? ls.get() : new Ifaces.ToIface(lt, ls.get());
            Expr r = rt.isInterface() ? rs.get() : new Ifaces.ToIface(rt, rs.get());
            return new Arith.EqIface(l, r);
        }
        Expr l = ls.get();
        Expr r = rs.get();
        switch (u.kind) {
            case GoType.BOOL:
                return ArithFactory.EqBoolNodeGen.create(l, r);
            case GoType.STRING:
                return ArithFactory.EqStrNodeGen.create(l, r);
            case GoType.FLOAT32, GoType.FLOAT64:
                return ArithFactory.EqF64NodeGen.create(asF64(l), asF64(r));
            case GoType.COMPLEX64, GoType.COMPLEX128:
                return ArithFactory.EqComplexNodeGen.create(l, r);
            case GoType.POINTER, GoType.UNSAFE_POINTER:
                return ArithFactory.EqPtrNodeGen.create(l, r);
            case GoType.CHAN, GoType.FUNC, GoType.MAP:
                return ArithFactory.EqIdentityNodeGen.create(l, r);
            case GoType.INTERFACE:
                return new Arith.EqIface(l, r);
            case GoType.STRUCT, GoType.ARRAY:
                return new Arith.EqFlat(u, l, r);
            default:
                if (u.isInteger()) {
                    return ArithFactory.EqI64NodeGen.create(asI64(l), asI64(r));
                }
                throw new IllegalStateException("== on " + lt.key);
        }
    }

    private Expr addr(Gir.Expr.Addr a) {
        Gir.Expr x = a.x();
        if (x instanceof Gir.Expr.StructLit || x instanceof Gir.Expr.ArrayLit) {
            return expr(x);
        }
        if (x instanceof Gir.Expr.Local l) {
            int i = l.local();
            if (!boxed[i] && ltypes[i].carrier() != GoType.C_FLAT) {
                throw new IllegalStateException("address of local " + pkg.str(body.locals()[i].name())
                        + " that is not flagged address-taken");
            }
            return new N.ReadRef(slots[i]);
        }
        LocMk mk = locOf(x, null);
        if (mk == null) {
            throw new IllegalStateException("address of " + x.getClass().getSimpleName());
        }
        return new Mem.AddrOf(mk.make(0, 0));
    }

    private Expr index(Gir.Expr.Index ix) {
        GoType xt = ty(ix.x()).u();
        if (xt.kind == GoType.STRING) {
            return new Data.StrIndex(expr(ix.x()), i64(ix.index()));
        }
        if (xt.kind == GoType.MAP && ix.commaOk()) {
            return new Data.MapGetOk(expr(ix.x()), expr(ix.index()), xt.elem, primKey(xt.mapKey));
        }
        GoType elem = xt.kind == GoType.MAP ? xt.elem : xt.kind == GoType.POINTER ? xt.elem.u().elem : xt.elem;
        return load(elem, indexLoc(ix, null).make(0, 0));
    }

    private Expr slice(Gir.Expr.Slice s) {
        GoType xt = ty(s.x()).u();
        I64 lo = s.lo() == null ? null : i64(s.lo());
        I64 hi = s.hi() == null ? null : i64(s.hi());
        I64 max = s.max() == null ? null : i64(s.max());
        switch (xt.kind) {
            case GoType.STRING:
                return new Data.StrSlice(expr(s.x()), lo, hi);
            case GoType.SLICE:
                return new Data.SliceOfSlice(expr(s.x()), lo, hi, max, xt.elem.nPrim(), xt.elem.nRef());
            default: {
                GoType at = xt.kind == GoType.POINTER ? xt.elem.u() : xt;
                LocMk base = structBase(s.x(), null);
                return new Data.SliceOfArray(base.make(0, 0), at.len, lo, hi, max, at.elem.nPrim(), at.elem.nRef());
            }
        }
    }

    private Expr typeAssert(Gir.Expr.TypeAssert ta) {
        GoType from = ty(ta.x());
        GoType target = ty(ta.target());
        Expr x = expr(ta.x());
        if (!from.isInterface()) {
            throw new Unsupported("type assertion on a type-parameter operand");
        }
        if (target.isInterface()) {
            return ta.commaOk() ? new Ifaces.AssertIfaceOk(x, target) : new Ifaces.AssertIface(x, from, target);
        }
        return ta.commaOk() ? new Ifaces.AssertConcreteOk(x, target)
                : typed(target, new Ifaces.AssertConcrete(x, from, target));
    }

    /** The conversion {@code to(x)}, {@code x} of type {@code from}. */
    private Expr convert(GoType from, GoType to, Gir.Expr x) {
        return convertNode(from, to, () -> to.isInterface() && !from.isInterface() ? value(x) : expr(x));
    }

    private Expr convertNode(GoType from, GoType to, Supplier<Expr> xs) {
        GoType f = from.u();
        GoType t = to.u();
        if (t.kind == GoType.INTERFACE) {
            return f.kind == GoType.INTERFACE ? xs.get() : new Ifaces.ToIface(from, xs.get());
        }
        Expr x = xs.get();
        if (t.isInteger()) {
            if (f.isInteger()) {
                return f.kind == t.kind ? x : wrap(t, asI64(x));
            } else if (f.isFloat()) {
                F64 d = asF64(x);
                if (u64(t)) {
                    return new Arith.F64ToU64(d);
                } else if (t.bits() == 64) {
                    return new Arith.F64ToI64(d);
                } else if (t.isUnsigned()) {
                    return wrap(t, new Arith.F64ToI64(d));
                }
                return wrap(t, new Arith.F64ToI32(d));
            }
        } else if (t.isFloat()) {
            if (f.isInteger()) {
                I64 i = asI64(x);
                if (t.kind == GoType.FLOAT32) {
                    return u64(f) ? new Arith.U64ToF32(i) : new Arith.I64ToF32(i);
                }
                return u64(f) ? new Arith.U64ToF64(i) : new Arith.I64ToF64(i);
            } else if (f.isFloat()) {
                return t.kind == GoType.FLOAT32 && f.kind != GoType.FLOAT32 ? new Arith.RoundF32(asF64(x)) : x;
            }
        } else if (t.isComplex() && f.isComplex()) {
            return t.kind == GoType.COMPLEX64 && f.kind != GoType.COMPLEX64 ? new Arith.RoundC64(x) : x;
        } else if (t.kind == GoType.STRING) {
            if (f.isInteger()) {
                return new Data.RuneToStr(x);
            } else if (f.kind == GoType.SLICE) {
                return f.elem.u().kind == GoType.UINT8 ? new Data.BytesToStr(x) : new Data.RunesToStr(x);
            }
            return x;
        } else if (t.kind == GoType.SLICE && f.kind == GoType.STRING) {
            return t.elem.u().kind == GoType.UINT8 ? new Data.StrToBytes(x) : new Data.StrToRunes(x);
        }
        if (f.kind == t.kind) {
            return x;
        }
        if (f.kind == GoType.POINTER && t.kind == GoType.UNSAFE_POINTER
                || f.kind == GoType.UNSAFE_POINTER && t.kind == GoType.POINTER) {
            return x;
        }
        throw new Unsupported("conversion " + from.key + " -> " + to.key);
    }

    // ---------------------------------------------------------------- calls

    private FunctionUnit.Instance instanceOf(Gir.Expr.Inst in) {
        FunctionUnit u = funcUnit(in.fn());
        GoType[] targs = new GoType[in.targs().length];
        for (int i = 0; i < targs.length; i++) {
            targs[i] = ty(in.targs()[i]);
        }
        return u.instance(targs);
    }

    /** The function unit a Global/Ext expression names, or null if it is not a function. */
    private FunctionUnit funcUnit(Gir.Expr fn) {
        if (fn instanceof Gir.Expr.Global g) {
            Gir.Object o = sec.objects[g.obj()];
            return o.kind() == Gir.SymKind.FUNC ? pkg.func(o.fn()) : null;
        } else if (fn instanceof Gir.Expr.Ext x) {
            Gir.SymRef sym = sec.symRefs[x.sym()];
            PackageUnit u = pkg.packageOf(sym);
            Gir.Object o = u.sections().objects[u.objectNamed(pkg.str(sym.name()))];
            return o.kind() == Gir.SymKind.FUNC ? u.func(o.fn()) : null;
        }
        return null;
    }

    /** The statically known callee of a call's function operand, or null. */
    private FunctionUnit.Instance staticCallee(Gir.Expr fn) {
        if (fn instanceof Gir.Expr.Inst in) {
            return instanceOf(in);
        }
        FunctionUnit u = funcUnit(fn);
        return u == null ? null : u.instance(null);
    }

    private FunctionUnit methodUnit(int methodSym) {
        Gir.SymRef sym = sec.symRefs[methodSym];
        return pkg.packageOf(sym).funcNamed(pkg.str(sym.name()));
    }

    private FunctionUnit.Instance methodInstance(int methodSym, int recvType, int[] targIds) {
        FunctionUnit u = methodUnit(methodSym);
        GoType rt = ty(recvType);
        if (rt.kind == GoType.POINTER) {
            rt = rt.elem;
        }
        GoType[] recvArgs = rt.targs == null ? new GoType[0] : rt.targs;
        GoType[] targs = new GoType[recvArgs.length + targIds.length];
        System.arraycopy(recvArgs, 0, targs, 0, recvArgs.length);
        for (int i = 0; i < targIds.length; i++) {
            targs[recvArgs.length + i] = ty(targIds[i]);
        }
        return u.instance(targs);
    }

    /** The receiver-less signature of a method under the instantiation of a call. */
    private GoType methodSig(int methodSym, int recvType, int[] targIds) {
        FunctionUnit.Instance mi = methodInstance(methodSym, recvType, targIds);
        return mi.unit().pkg.type(mi.unit().func.sig(), mi.env);
    }

    private static Expr[] withRecv(Expr recv, Expr[] args) {
        Expr[] out = new Expr[args.length + 1];
        out[0] = recv;
        System.arraycopy(args, 0, out, 1, args.length);
        return out;
    }

    /** Argument nodes for a call of signature {@code sig}: copies, variadic packing, spreads. */
    private Expr[] args(Gir.Expr[] args, GoType sig, boolean spread) {
        GoType s = sig.u();
        int np = s.params.length;
        if (args.length == 1 && ty(args[0]).kind == GoType.TUPLE) {
            // f(g()): one multi-value operand supplies every parameter.
            GoType tuple = ty(args[0]);
            int slot = tempObject();
            int n = tuple.results.length;
            List<Expr> elems = new ArrayList<>();
            for (int i = 0; i < n; i++) {
                Expr get = new Calls.TupleGet(new N.ReadRef(slot), i);
                if (i == 0) {
                    get = new N.Seq(new Stmt[] {new N.WriteRef(slot, expr(args[0]))}, get);
                }
                GoType et = tuple.results[i];
                elems.add(et.carrier() == GoType.C_FLAT ? new Mem.Copy(get) : get);
            }
            if (s.variadic) {
                GoType vt = s.params[np - 1];
                List<Expr> fixed = new ArrayList<>(elems.subList(0, Math.min(np - 1, n)));
                fixed.add(sliceOf(vt.u().elem, elems.subList(Math.min(np - 1, n), n)));
                return fixed.toArray(new Expr[0]);
            }
            return elems.toArray(new Expr[0]);
        }
        if (!s.variadic || spread) {
            Expr[] out = new Expr[args.length];
            for (int i = 0; i < args.length; i++) {
                out[i] = value(args[i]);
            }
            return out;
        }
        Expr[] out = new Expr[np];
        for (int i = 0; i < np - 1; i++) {
            out[i] = value(args[i]);
        }
        List<Expr> rest = new ArrayList<>();
        for (int i = np - 1; i < args.length; i++) {
            rest.add(expr(args[i]));
        }
        out[np - 1] = sliceOf(s.params[np - 1].u().elem, rest);
        return out;
    }

    /** A slice of the given element nodes (the variadic parameter); nil when there are none. */
    private Expr sliceOf(GoType elem, List<Expr> values) {
        if (values.isEmpty()) {
            return new N.ConstRef(Slice.NIL);
        }
        Data.Init[] inits = new Data.Init[values.size()];
        for (int i = 0; i < inits.length; i++) {
            inits[i] = init(elem, i * elem.nPrim(), i * elem.nRef(), values.get(i));
        }
        return new Data.SliceLit(elem, inits.length, inits);
    }

    private static Data.Init init(GoType t, int p, int r, Expr v) {
        switch (t.carrier()) {
            case GoType.C_REF:
                return new Data.InitRef(r, v);
            case GoType.C_FLAT:
                return new Data.InitSub(p, r, v);
            default:
                return new Data.InitPrim(p, raw(t, v));
        }
    }

    private Expr call(Gir.Expr.Call c) {
        GoType sig = ty(c.fn());
        GoType result = ty(c.type());
        FunctionUnit.Instance callee = staticCallee(c.fn());
        Expr[] a = args(c.args(), sig, c.spread());
        if (callee != null) {
            return typed(result, new Calls.Call(callee, a));
        }
        return typed(result, new Calls.CallClosure(expr(c.fn()), a));
    }

    private static String methodKey(String name, String pkgPath) {
        return pkgPath.isEmpty() ? name : pkgPath + "." + name;
    }

    private static int methodIndex(GoType iface, String key) {
        GoType.IMethod[] ms = iface.u().imethods;
        for (int i = 0; i < ms.length; i++) {
            if (ms[i].key.equals(key)) {
                return i;
            }
        }
        throw new IllegalStateException("interface " + iface.key + " has no method " + key);
    }

    private static Method methodOf(GoType dyn, String key) {
        Method m = dyn.findMethod(key);
        if (m == null) {
            throw new IllegalStateException("type " + dyn.key + " has no method " + key);
        }
        return m;
    }

    private Expr ifaceCall(Gir.Expr.IfaceCall ic) {
        GoType rt = ty(ic.recv());
        GoType result = ty(ic.type());
        String key = methodKey(pkg.str(ic.name()), pkg.str(ic.pkg()));
        if (rt.isInterface()) {
            int index = methodIndex(rt, key);
            Expr[] a = args(ic.args(), rt.u().imethods[index].sig, ic.spread());
            return typed(result, new Calls.IfaceCall(expr(ic.recv()), rt, index, a));
        }
        // A type parameter instantiated with a concrete type: the method is known now (D6).
        Method m = methodOf(rt, key);
        GoType sig = dynamicSig(rt, key, ic.args().length);
        Expr[] a = sig == null ? plainArgs(ic.args()) : args(ic.args(), sig, ic.spread());
        return typed(result, new Calls.Call(m::target, withRecv(new Calls.Adapt(m, expr(ic.recv())), a)));
    }

    /** No signature is recorded for a method reached through a type parameter. */
    private GoType dynamicSig(GoType rt, String key, int nargs) {
        return null;
    }

    private Expr[] plainArgs(Gir.Expr[] args) {
        Expr[] out = new Expr[args.length];
        for (int i = 0; i < args.length; i++) {
            out[i] = value(args[i]);
        }
        return out;
    }

    private static Expr[] argNodes(int from, int count) {
        Expr[] out = new Expr[count];
        for (int i = 0; i < count; i++) {
            out[i] = new N.Arg(from + i);
        }
        return out;
    }

    private RootCallTarget thunk(String what, Expr body) {
        return new Calls.ThunkRoot(lang, FrameDescriptor.newBuilder().build(), name + "-" + what, body)
                .getCallTarget();
    }

    private static FrameDescriptor envDescriptor(int n) {
        return graalgo.go.values.Env.layout(n);
    }

    private Expr methodValue(Gir.Expr.MethodVal mv) {
        FunctionUnit.Instance mi = methodInstance(mv.method(), mv.recvType(), mv.targs());
        GoType ft = ty(mv.type()).u();
        GoType recvT = ty(mv.recv());
        Expr recv = new Calls.EnvRead(0);
        if (recvT.carrier() == GoType.C_FLAT) {
            recv = new Mem.Copy(recv);
        }
        RootCallTarget t = thunk("fm", new Calls.Call(mi, withRecv(recv, argNodes(1, ft.params.length))));
        return new Calls.Bind1(t, envDescriptor(1), value(mv.recv()), false);
    }

    private Expr ifaceMethodValue(Gir.Expr.IfaceMethodVal mv) {
        GoType rt = ty(mv.recv());
        GoType ft = ty(mv.type()).u();
        String key = methodKey(pkg.str(mv.name()), pkg.str(mv.pkg()));
        Expr[] a = argNodes(1, ft.params.length);
        if (rt.isInterface()) {
            RootCallTarget t = thunk("fm", new Calls.IfaceCall(new Calls.EnvRead(0), rt, methodIndex(rt, key), a));
            return new Calls.Bind1(t, envDescriptor(1), expr(mv.recv()), true);
        }
        Method m = methodOf(rt, key);
        RootCallTarget t = thunk("fm",
                new Calls.Call(m::target, withRecv(new Calls.Adapt(m, new Calls.EnvRead(0)), a)));
        return new Calls.Bind1(t, envDescriptor(1), value(mv.recv()), false);
    }

    private Expr methodExpr(Gir.Expr.MethodExpr me) {
        GoType recvT = ty(me.recvType());
        GoType ft = ty(me.type()).u();
        Gir.SymRef sym = sec.symRefs[me.method()];
        String symName = pkg.str(sym.name());
        String mname = symName.substring(symName.lastIndexOf('.') + 1);
        boolean exported = Character.isUpperCase(mname.codePointAt(0));
        Method m = methodOf(recvT, exported ? mname : pkg.str(sym.pkg()) + "." + mname);
        RootCallTarget t = thunk("me", new Calls.Call(m::target,
                withRecv(new Calls.Adapt(m, new N.Arg(1)), argNodes(2, ft.params.length - 1))));
        return new N.ConstRef(new graalgo.go.values.Closure(t, null));
    }

    private Expr ifaceMethodExpr(Gir.Expr.IfaceMethodExpr me) {
        GoType it = ty(me.iface());
        GoType ft = ty(me.type()).u();
        String key = methodKey(pkg.str(me.name()), pkg.str(me.pkg()));
        RootCallTarget t = thunk("me",
                new Calls.IfaceCall(new N.Arg(1), it, methodIndex(it, key), argNodes(2, ft.params.length - 1)));
        return new N.ConstRef(new graalgo.go.values.Closure(t, null));
    }

    private Expr funcLit(Gir.Expr.FuncLit fl) {
        int[] caps = fl.captures();
        int[] capSlots = new int[caps.length];
        for (int i = 0; i < caps.length; i++) {
            int local = caps[i];
            if (!boxed[local] && ltypes[local].carrier() != GoType.C_FLAT) {
                throw new IllegalStateException("captured local " + pkg.str(body.locals()[local].name())
                        + " is not flagged as captured");
            }
            capSlots[i] = slots[local];
        }
        String litName = name + ".func" + (++litCount);
        Supplier<RootCallTarget> target = new Supplier<>() {
            private RootCallTarget built;

            @Override
            public synchronized RootCallTarget get() {
                if (built == null) {
                    built = new NodeFactory(inst, fl.body(), litName).buildRoot(caps.length);
                }
                return built;
            }
        };
        return new Calls.MakeClosure(target, envDescriptor(caps.length), capSlots);
    }

    private Expr tupleConvert(Gir.Expr.TupleConvert tc) {
        GoType to = ty(tc.type());
        GoType from = ty(tc.x());
        int slot = tempObject();
        Expr[] elems = new Expr[to.results.length];
        for (int i = 0; i < elems.length; i++) {
            Expr get = new Calls.TupleGet(new N.ReadRef(slot), i);
            GoType ft = from.results[i];
            GoType tt = to.results[i];
            elems[i] = tt.isInterface() && !ft.isInterface() ? new Ifaces.ToIface(ft, get) : get;
        }
        return new N.Seq(new Stmt[] {new N.WriteRef(slot, expr(tc.x()))}, new Calls.Tuple(elems));
    }

    // ---------------------------------------------------------------- composite literals

    private Expr structLit(Gir.Expr.StructLit sl) {
        GoType t = ty(sl.type());
        Data.Init[] inits = new Data.Init[sl.fields().length];
        for (int i = 0; i < inits.length; i++) {
            Gir.FieldInit fi = sl.fields()[i];
            GoType cur = t.u();
            int p = 0;
            int r = 0;
            for (long index : fi.path()) {
                cur.nPrim();
                Field f = cur.fields[(int) index];
                p += f.primOff;
                r += f.refOff;
                cur = f.type.u();
            }
            inits[i] = init(ty(fi.value()), p, r, expr(fi.value()));
        }
        return new Data.FlatLit(t, inits);
    }

    private Expr arrayLit(Gir.Expr.ArrayLit al) {
        GoType t = ty(al.type());
        GoType u = t.u();
        GoType elem = u.elem;
        int np = elem.nPrim();
        int nr = elem.nRef();
        Data.Init[] inits = new Data.Init[al.elems().length];
        long len = 0;
        for (int i = 0; i < inits.length; i++) {
            Gir.Elem e = al.elems()[i];
            int at = (int) e.index();
            inits[i] = init(elem, at * np, at * nr, expr(e.value()));
            len = Math.max(len, e.index() + 1);
        }
        if (u.kind == GoType.ARRAY) {
            return new Data.FlatLit(t, inits);
        }
        return new Data.SliceLit(elem, (int) len, inits);
    }

    private Expr mapLit(Gir.Expr.MapLit ml) {
        GoType t = ty(ml.type());
        GoType u = t.u();
        int slot = tempObject();
        List<Stmt> pre = new ArrayList<>();
        pre.add(new N.WriteRef(slot,
                new Data.MakeMap(u.mapKey, u.elem, new N.ConstI64(ml.entries().length))));
        aux();
        for (Gir.MapEntry e : ml.entries()) {
            Loc loc = primKey(u.mapKey)
                    ? new Mem.MapPutI(new N.ReadRef(slot), raw(u.mapKey, expr(e.key())), auxP, auxR)
                    : new Mem.MapPutG(new N.ReadRef(slot), value(e.key()), auxP, auxR);
            pre.add(store(u.elem, loc, expr(e.value())));
        }
        return new N.Seq(pre.toArray(new Stmt[0]), new N.ReadRef(slot));
    }

    // ---------------------------------------------------------------- builtins

    /** A builtin call as an expression; {@code given} replaces the argument nodes (deferred calls). */
    private Expr builtin(Gir.Expr.BuiltinCall c, Expr[] given) {
        Gir.Expr[] a = c.args();
        Supplier<Expr> a0 = () -> given != null ? given[0] : expr(a[0]);
        Supplier<Expr> a1 = () -> given != null ? given[1] : expr(a[1]);
        GoType result = ty(c.type());
        switch (c.builtin()) {
            case Gir.Builtin.LEN, Gir.Builtin.CAP: {
                GoType xt = ty(a[0]).u();
                boolean cap = c.builtin() == Gir.Builtin.CAP;
                switch (xt.kind) {
                    case GoType.STRING:
                        return new Data.StrLen(a0.get());
                    case GoType.SLICE:
                        return cap ? new Data.SliceCap(a0.get()) : new Data.SliceLen(a0.get());
                    case GoType.MAP:
                        return new Data.MapLen(a0.get());
                    case GoType.CHAN:
                        return new Chans.ChanLen(a0.get(), cap);
                    case GoType.ARRAY:
                        return new N.ConstI64(xt.len);
                    default:
                        return new N.ConstI64(xt.elem.u().len);
                }
            }
            case Gir.Builtin.APPEND: {
                GoType st = result.u();
                GoType elem = st.elem;
                Expr s = a0.get();
                if (a.length == 1) {
                    return s;
                }
                if (c.spread()) {
                    return ty(a[1]).u().kind == GoType.STRING ? new Data.AppendStr(s, elem, a1.get())
                            : new Data.AppendSlice(s, elem, a1.get());
                }
                Expr[] vals = new Expr[a.length - 1];
                for (int i = 1; i < a.length; i++) {
                    vals[i - 1] = given != null ? given[i] : expr(a[i]);
                }
                switch (elem.carrier()) {
                    case GoType.C_REF:
                        return new Data.AppendRef(s, elem, vals);
                    case GoType.C_FLAT:
                        return new Data.AppendFlat(s, elem, vals);
                    default: {
                        I64[] raws = new I64[vals.length];
                        for (int i = 0; i < raws.length; i++) {
                            raws[i] = raw(elem, vals[i]);
                        }
                        return new Data.AppendPrim(s, elem, raws);
                    }
                }
            }
            case Gir.Builtin.COPY: {
                GoType dt = ty(a[0]).u();
                if (ty(a[1]).u().kind == GoType.STRING) {
                    return new Data.CopyStr(a0.get(), a1.get());
                }
                return new Data.CopySlice(a0.get(), a1.get(), dt.elem.nPrim(), dt.elem.nRef());
            }
            case Gir.Builtin.COMPLEX: {
                Expr n = new Arith.MakeComplex(asF64(a0.get()), asF64(a1.get()));
                return n;
            }
            case Gir.Builtin.REAL:
                return new Arith.Real(a0.get());
            case Gir.Builtin.IMAG:
                return new Arith.Imag(a0.get());
            case Gir.Builtin.MAKE: {
                GoType t = ty(c.targ()).u();
                I64 n0 = a.length > 0 ? asI64(a0.get()) : null;
                I64 n1 = a.length > 1 ? asI64(a1.get()) : null;
                switch (t.kind) {
                    case GoType.SLICE:
                        return new Data.MakeSlice(t.elem, n0, n1);
                    case GoType.MAP:
                        return new Data.MakeMap(t.mapKey, t.elem, n0);
                    default:
                        return new Chans.MakeChan(t.elem, n0);
                }
            }
            case Gir.Builtin.NEW: {
                GoType t = result.u().elem;
                if (a.length == 0) {
                    return new Mem.Zero(t);
                }
                return new Data.FlatLit(t, new Data.Init[] {init(t, 0, 0, a0.get())});
            }
            case Gir.Builtin.MIN, Gir.Builtin.MAX: {
                boolean min = c.builtin() == Gir.Builtin.MIN;
                GoType u = result.u();
                Expr acc = a0.get();
                for (int i = 1; i < a.length; i++) {
                    Expr x = given != null ? given[i] : expr(a[i]);
                    if (u.isInteger()) {
                        I64 l = asI64(acc);
                        I64 r = asI64(x);
                        if (u64(u)) {
                            acc = min ? ArithFactory.MinU64NodeGen.create(l, r) : ArithFactory.MaxU64NodeGen.create(l, r);
                        } else {
                            acc = min ? ArithFactory.MinI64NodeGen.create(l, r) : ArithFactory.MaxI64NodeGen.create(l, r);
                        }
                    } else if (u.isFloat()) {
                        acc = min ? ArithFactory.MinF64NodeGen.create(asF64(acc), asF64(x))
                                : ArithFactory.MaxF64NodeGen.create(asF64(acc), asF64(x));
                    } else {
                        acc = min ? ArithFactory.MinStrNodeGen.create(acc, x) : ArithFactory.MaxStrNodeGen.create(acc, x);
                    }
                }
                return acc;
            }
            case Gir.Builtin.RECOVER:
                // `defer recover()` does not recover: recover is not called by a deferred function.
                return inThunk ? new N.ConstRef(null) : new Calls.Recover();
            default: {
                Stmt s = builtinStmt(c, given);
                if (s != null) {
                    return new N.Seq(new Stmt[] {s}, new N.ConstRef(null));
                }
                throw new Unsupported("builtin " + Gir.Builtin.name(c.builtin()));
            }
        }
    }

    /** A builtin that is a statement (no value), or null. */
    private Stmt builtinStmt(Gir.Expr.BuiltinCall c, Expr[] given) {
        Gir.Expr[] a = c.args();
        Supplier<Expr> a0 = () -> given != null ? given[0] : expr(a[0]);
        switch (c.builtin()) {
            case Gir.Builtin.PRINT, Gir.Builtin.PRINTLN: {
                GoType[] ts = new GoType[a.length];
                Expr[] xs = new Expr[a.length];
                for (int i = 0; i < a.length; i++) {
                    ts[i] = ty(a[i]);
                    xs[i] = given != null ? given[i] : expr(a[i]);
                }
                return new Data.Print(ts, xs, c.builtin() == Gir.Builtin.PRINTLN);
            }
            case Gir.Builtin.PANIC: {
                GoType at = ty(a[0]);
                Expr v = given != null ? given[0] : at.isInterface() ? expr(a[0]) : new Ifaces.ToIface(at, value(a[0]));
                return new Calls.Panic(v);
            }
            case Gir.Builtin.CLOSE:
                return new Chans.Close(a0.get());
            case Gir.Builtin.DELETE: {
                GoType kt = ty(a[0]).u().mapKey;
                Expr k = given != null ? given[1] : expr(a[1]);
                return primKey(kt) ? new Data.MapDeleteI(a0.get(), raw(kt, k)) : new Data.MapDeleteG(a0.get(), k);
            }
            case Gir.Builtin.CLEAR: {
                GoType t = ty(a[0]).u();
                return t.kind == GoType.MAP ? new Data.ClearMap(a0.get()) : new Data.ClearSlice(a0.get(), t.elem);
            }
            default:
                return null;
        }
    }

    // ---------------------------------------------------------------- statements

    private Stmt block(Gir.Stmt[] ss) {
        boolean labels = false;
        for (Gir.Stmt s : ss) {
            if (s instanceof Gir.Stmt.Labeled l && gotoJumps[l.label()] != null) {
                labels = true;
            }
        }
        Stmt[] out = new Stmt[ss.length];
        if (!labels) {
            for (int i = 0; i < ss.length; i++) {
                out[i] = stmt(ss[i]);
            }
            return out.length == 1 ? out[0] : new Ctl.Block(out);
        }
        List<Jump> jumps = new ArrayList<>();
        List<Integer> at = new ArrayList<>();
        for (int i = 0; i < ss.length; i++) {
            if (ss[i] instanceof Gir.Stmt.Labeled l && gotoJumps[l.label()] != null) {
                jumps.add(gotoJumps[l.label()]);
                at.add(i);
            }
            out[i] = stmt(ss[i]);
        }
        int[] targetsOf = new int[at.size()];
        for (int i = 0; i < targetsOf.length; i++) {
            targetsOf[i] = at.get(i);
        }
        return new Ctl.GotoBlock(out, jumps.toArray(new Jump[0]), targetsOf);
    }

    private Stmt seq(List<Stmt> ss) {
        return ss.size() == 1 ? ss.get(0) : new Ctl.Block(ss.toArray(new Stmt[0]));
    }

    private Stmt stmt(Gir.Stmt s) {
        switch (s) {
            case Gir.Stmt.Expr x: {
                if (x.x() instanceof Gir.Expr.BuiltinCall bc) {
                    Stmt b = builtinStmt(bc, null);
                    if (b != null) {
                        return b;
                    }
                }
                return new N.ExprStmt(expr(x.x()));
            }
            case Gir.Stmt.Assign a:
                return assign(a);
            case Gir.Stmt.Define d:
                return define(d.lhs(), d.rhs(), d.fresh());
            case Gir.Stmt.Var v:
                return var(v);
            case Gir.Stmt.IncDec id: {
                List<Stmt> pre = new ArrayList<>();
                GoType t = ty(id.x());
                LV lv = lvalue(id.x(), pure(id.x()) ? null : pre);
                Supplier<Expr> one = () -> t.isInteger() ? new N.ConstI64(1) : t.isFloat() ? new N.ConstF64(1)
                        : new N.ConstRef(new Complex(1, 0));
                pre.add(lv.store(binary(id.inc() ? Gir.BinOp.ADD : Gir.BinOp.SUB, t, t, null, null, lv::load, one, t)));
                return seq(pre);
            }
            case Gir.Stmt.Send sn:
                return new Chans.Send(expr(sn.ch()), value(sn.value()));
            case Gir.Stmt.Go g:
                return new Calls.Go(prepared(g.call()));
            case Gir.Stmt.Defer d: {
                if (!openDefers) {
                    return new Calls.Defer(prepared(d.call()));
                }
                int[] site = {tempObject(), tempObject()};
                deferSites.add(site);
                return new Calls.OpenDefer(prepared(d.call()), deferSites.size() - 1, deferBits, site[0], site[1]);
            }
            case Gir.Stmt.Return r:
                return ret(r);
            case Gir.Stmt.Branch b:
                return branch(b);
            case Gir.Stmt.Labeled l: {
                pendingLabel = l.label();
                Stmt inner = stmt(l.stmt());
                if (pendingLabel == l.label()) {
                    // Not a loop/switch/select: a labeled block that `break L` may leave.
                    pendingLabel = -1;
                }
                return inner;
            }
            case Gir.Stmt.Block b: {
                int label = takeLabel();
                if (label < 0) {
                    return block(b.stmts());
                }
                Target t = new Target(label, false);
                targets.push(t);
                Stmt inner = block(b.stmts());
                targets.pop();
                return t.brk == null ? inner : new Ctl.BreakTarget(inner, t.brk);
            }
            case Gir.Stmt.If i: {
                takeLabel();
                List<Stmt> out = new ArrayList<>();
                if (i.init() != null) {
                    out.add(stmt(i.init()));
                }
                Bool cond = bool(i.cond());
                Stmt then = block(i.then());
                Stmt els = i.else_() == null ? null : stmt(i.else_());
                out.add(new Ctl.If(cond, then, els));
                return seq(out);
            }
            case Gir.Stmt.Switch sw:
                return switchStmt(sw);
            case Gir.Stmt.TypeSwitch ts:
                return typeSwitch(ts);
            case Gir.Stmt.Select sel:
                return select(sel);
            case Gir.Stmt.For f:
                return forStmt(f);
            case Gir.Stmt.Range r:
                return range(r);
        }
    }

    private int takeLabel() {
        int l = pendingLabel;
        pendingLabel = -1;
        return l;
    }

    private Stmt branch(Gir.Stmt.Branch b) {
        switch (b.tok()) {
            case Gir.BranchTok.GOTO:
                return new Ctl.Throw(gotoJumps[b.label()]);
            case Gir.BranchTok.BREAK: {
                for (Target t : targets) {
                    if (b.label() < 0 || t.label == b.label()) {
                        if (t.brk == null) {
                            t.brk = new Jump();
                        }
                        return new Ctl.Throw(t.brk);
                    }
                }
                throw new IllegalStateException("break outside a breakable statement");
            }
            case Gir.BranchTok.CONTINUE: {
                for (Target t : targets) {
                    if (t.loop && (b.label() < 0 || t.label == b.label())) {
                        if (t.cont == null) {
                            t.cont = new Jump();
                        }
                        return new Ctl.Throw(t.cont);
                    }
                }
                throw new IllegalStateException("continue outside a loop");
            }
            default:
                throw new IllegalStateException("fallthrough outside the end of a switch clause");
        }
    }

    private Stmt assign(Gir.Stmt.Assign a) {
        Gir.Expr[] lhs = a.lhs();
        Gir.Expr[] rhs = a.rhs();
        if (a.op() != Gir.AssignOp.PLAIN) {
            List<Stmt> pre = new ArrayList<>();
            GoType t = ty(lhs[0]);
            LV lv = lvalue(lhs[0], pure(lhs[0]) && pure(rhs[0]) ? null : pre);
            int op = switch (a.op()) {
                case Gir.AssignOp.ADD -> Gir.BinOp.ADD;
                case Gir.AssignOp.SUB -> Gir.BinOp.SUB;
                case Gir.AssignOp.MUL -> Gir.BinOp.MUL;
                case Gir.AssignOp.QUO -> Gir.BinOp.QUO;
                case Gir.AssignOp.REM -> Gir.BinOp.REM;
                case Gir.AssignOp.AND -> Gir.BinOp.AND;
                case Gir.AssignOp.OR -> Gir.BinOp.OR;
                case Gir.AssignOp.XOR -> Gir.BinOp.XOR;
                case Gir.AssignOp.SHL -> Gir.BinOp.SHL;
                case Gir.AssignOp.SHR -> Gir.BinOp.SHR;
                default -> Gir.BinOp.AND_NOT;
            };
            pre.add(lv.store(binary(op, t, ty(rhs[0]), null, rhs[0], lv::load, () -> expr(rhs[0]), t)));
            return seq(pre);
        }
        return assignAll(lhs, rhs, null);
    }

    private Stmt define(Gir.Expr[] lhs, Gir.Expr[] rhs, int[] fresh) {
        return assignAll(lhs, rhs, fresh);
    }

    private static boolean contains(int[] xs, int x) {
        if (xs != null) {
            for (int v : xs) {
                if (v == x) {
                    return true;
                }
            }
        }
        return false;
    }

    /** Stores {@code v} to target {@code e}: a declaration when {@code e} is a fresh local. */
    private Stmt storeTo(Gir.Expr e, int[] fresh, Expr v, List<Stmt> pre) {
        if (e instanceof Gir.Expr.Local l && contains(fresh, l.local())) {
            return declare(l.local(), v);
        }
        return lvalue(e, pre).store(v);
    }

    /**
     * {@code lhs = rhs} / {@code lhs := rhs} in all three shapes: single, one multi-value
     * operand, parallel.
     */
    private Stmt assignAll(Gir.Expr[] lhs, Gir.Expr[] rhs, int[] fresh) {
        List<Stmt> out = new ArrayList<>();
        if (lhs.length == 1 && rhs.length == 1) {
            boolean simple = pure(rhs[0]) && pure(lhs[0]) || lhs[0] instanceof Gir.Expr.Local
                    || lhs[0] instanceof Gir.Expr.Blank;
            List<Stmt> pre = simple ? null : out;
            if (lhs[0] instanceof Gir.Expr.Local l && contains(fresh, l.local())) {
                out.add(declare(l.local(), value(rhs[0])));
            } else {
                LV lv = lvalue(lhs[0], pre);
                out.add(lv.store(storedValue(lhs[0], rhs[0])));
            }
            return seq(out);
        }
        if (rhs.length == 1) {
            // a, b = f() or a comma-ok form: the tuple, then element-wise stores.
            GoType tuple = ty(rhs[0]);
            boolean callResult = rhs[0] instanceof Gir.Expr.Call || rhs[0] instanceof Gir.Expr.MethodCall
                    || rhs[0] instanceof Gir.Expr.IfaceCall;
            List<LV> lvs = new ArrayList<>();
            for (Gir.Expr l : lhs) {
                lvs.add(l instanceof Gir.Expr.Local x && contains(fresh, x.local()) ? null : lvalue(l, out));
            }
            int slot = tempObject();
            out.add(new N.WriteRef(slot, expr(rhs[0])));
            for (int i = 0; i < lhs.length; i++) {
                GoType et = tupleElem(tuple, i);
                Expr get = new Calls.TupleGet(new N.ReadRef(slot), i);
                Expr v = et.carrier() == GoType.C_FLAT && !callResult ? new Mem.Copy(get) : typed(et, get);
                out.add(lvs.get(i) == null ? declare(((Gir.Expr.Local) lhs[i]).local(), v) : lvs.get(i).store(v));
            }
            return seq(out);
        }
        // Parallel assignment: target operands, then every right-hand side, then the stores.
        boolean allFresh = true;
        for (Gir.Expr l : lhs) {
            allFresh &= l instanceof Gir.Expr.Local x && contains(fresh, x.local()) || l instanceof Gir.Expr.Blank;
        }
        if (allFresh) {
            for (int i = 0; i < lhs.length; i++) {
                out.add(storeTo(lhs[i], fresh, value(rhs[i]), null));
            }
            return seq(out);
        }
        List<LV> lvs = new ArrayList<>();
        for (Gir.Expr l : lhs) {
            lvs.add(l instanceof Gir.Expr.Local x && contains(fresh, x.local()) ? null : lvalue(l, out));
        }
        int[] tmp = new int[rhs.length];
        for (int i = 0; i < rhs.length; i++) {
            GoType t = ty(rhs[i]);
            tmp[i] = temp(t);
            out.add(writeSlot(tmp[i], t, value(rhs[i])));
        }
        for (int i = 0; i < lhs.length; i++) {
            Expr v = readSlot(tmp[i], ty(rhs[i]));
            out.add(lvs.get(i) == null ? declare(((Gir.Expr.Local) lhs[i]).local(), v) : lvs.get(i).store(v));
        }
        return seq(out);
    }

    /** The node to store for {@code rhs} into target {@code lhs}: copy-in targets need no copy. */
    private Expr storedValue(Gir.Expr lhs, Gir.Expr rhs) {
        boolean copiesIn = ty(rhs).carrier() == GoType.C_FLAT && !(lhs instanceof Gir.Expr.Blank);
        return copiesIn ? expr(rhs) : value(rhs);
    }

    private static GoType tupleElem(GoType tuple, int i) {
        return tuple.results[i];
    }

    private Stmt var(Gir.Stmt.Var v) {
        int[] locals = v.locals();
        Gir.Expr[] values = v.values();
        List<Stmt> out = new ArrayList<>();
        if (values.length == 0) {
            for (int l : locals) {
                out.add(declare(l, null));
            }
        } else if (values.length == locals.length) {
            for (int i = 0; i < locals.length; i++) {
                out.add(declare(locals[i], value(values[i])));
            }
        } else {
            GoType tuple = ty(values[0]);
            int slot = tempObject();
            out.add(new N.WriteRef(slot, expr(values[0])));
            for (int i = 0; i < locals.length; i++) {
                GoType et = tupleElem(tuple, i);
                Expr get = new Calls.TupleGet(new N.ReadRef(slot), i);
                out.add(declare(locals[i], et.carrier() == GoType.C_FLAT ? new Mem.Copy(get) : typed(et, get)));
            }
        }
        return seq(out);
    }

    private Stmt ret(Gir.Stmt.Return r) {
        int[] rs = body.results();
        Gir.Expr[] xs = r.results();
        List<Stmt> out = new ArrayList<>();
        if (xs.length == 1 && rs.length == 1) {
            out.add(assignLocal(rs[0], expr(xs[0])));
        } else if (xs.length == 1 && rs.length > 1) {
            GoType tuple = ty(xs[0]);
            int slot = tempObject();
            out.add(new N.WriteRef(slot, expr(xs[0])));
            for (int i = 0; i < rs.length; i++) {
                out.add(assignLocal(rs[i], typed(tupleElem(tuple, i), new Calls.TupleGet(new N.ReadRef(slot), i))));
            }
        } else if (xs.length > 1) {
            int[] tmp = new int[xs.length];
            for (int i = 0; i < xs.length; i++) {
                GoType t = ty(xs[i]);
                tmp[i] = temp(t);
                out.add(writeSlot(tmp[i], t, value(xs[i])));
            }
            for (int i = 0; i < xs.length; i++) {
                out.add(assignLocal(rs[i], readSlot(tmp[i], ty(xs[i]))));
            }
        }
        out.add(new Ctl.Throw(Jump.RETURN));
        return seq(out);
    }

    /** The function and arguments of a {@code defer}/{@code go} call, evaluated at the statement. */
    private Calls.Prepared prepared(Gir.Expr call) {
        switch (call) {
            case Gir.Expr.Call c: {
                GoType sig = ty(c.fn());
                Expr[] a = args(c.args(), sig, c.spread());
                FunctionUnit.Instance callee = staticCallee(c.fn());
                return callee != null ? new Calls.PrepStatic(callee, a) : new Calls.PrepClosure(expr(c.fn()), a);
            }
            case Gir.Expr.MethodCall mc:
                return new Calls.PrepStatic(methodInstance(mc.method(), mc.recvType(), mc.targs()),
                        withRecv(value(mc.recv()),
                                args(mc.args(), methodSig(mc.method(), mc.recvType(), mc.targs()), mc.spread())));
            case Gir.Expr.IfaceCall ic: {
                GoType rt = ty(ic.recv());
                String key = methodKey(pkg.str(ic.name()), pkg.str(ic.pkg()));
                if (rt.isInterface()) {
                    int index = methodIndex(rt, key);
                    return new Calls.PrepIface(expr(ic.recv()), rt, index,
                            args(ic.args(), rt.u().imethods[index].sig, ic.spread()));
                }
                Method m = methodOf(rt, key);
                return new Calls.PrepStatic(m::target,
                        withRecv(new Calls.Adapt(m, expr(ic.recv())), plainArgs(ic.args())));
            }
            case Gir.Expr.BuiltinCall bc: {
                // The arguments are evaluated now; a generated thunk applies the builtin later.
                Gir.Expr[] a = bc.args();
                Expr[] now = new Expr[a.length];
                Expr[] later = new Expr[a.length];
                for (int i = 0; i < a.length; i++) {
                    GoType t = ty(a[i]);
                    now[i] = bc.builtin() == Gir.Builtin.PANIC && !t.isInterface() ? new Ifaces.ToIface(t, value(a[i]))
                            : value(a[i]);
                    later[i] = typed(t, new N.Arg(i + 1));
                }
                boolean saved = inThunk;
                inThunk = true;
                RootCallTarget t = thunk("defer", builtin(bc, later));
                inThunk = saved;
                return new Calls.PrepStatic(() -> t, now);
            }
            default:
                throw new IllegalStateException("defer/go of " + call.getClass().getSimpleName());
        }
    }

    // ---------------------------------------------------------------- switch, select

    /** The body of clause {@code i}; a trailing {@code fallthrough} chains the next clause's body. */
    private Stmt clauseBody(Gir.CaseClause[] clauses, int i) {
        Gir.Stmt[] b = clauses[i].body();
        if (b.length > 0 && b[b.length - 1] instanceof Gir.Stmt.Branch br && br.tok() == Gir.BranchTok.FALLTHROUGH) {
            Gir.Stmt[] head = java.util.Arrays.copyOf(b, b.length - 1);
            return new Ctl.Block(new Stmt[] {block(head), clauseBody(clauses, i + 1)});
        }
        return block(b);
    }

    private Stmt switchStmt(Gir.Stmt.Switch sw) {
        Target target = new Target(takeLabel(), false);
        targets.push(target);
        List<Stmt> out = new ArrayList<>();
        if (sw.init() != null) {
            out.add(stmt(sw.init()));
        }
        GoType tagT = sw.tag() == null ? null : ty(sw.tag());
        int tagSlot = -1;
        if (sw.tag() != null) {
            tagSlot = temp(tagT);
            out.add(writeSlot(tagSlot, tagT, expr(sw.tag())));
        }
        Gir.CaseClause[] cs = sw.clauses();
        Stmt chain = null;
        for (int i = 0; i < cs.length; i++) {
            if (cs[i].isDefault()) {
                chain = clauseBody(cs, i);
            }
        }
        for (int i = cs.length - 1; i >= 0; i--) {
            if (cs[i].isDefault()) {
                continue;
            }
            Bool cond = null;
            for (Gir.Expr e : cs[i].exprs()) {
                Bool one;
                if (sw.tag() == null) {
                    one = bool(e);
                } else {
                    int slot = tagSlot;
                    one = equal(tagT, ty(e), sw.tag(), e, () -> readSlot(slot, tagT), () -> expr(e));
                }
                cond = cond == null ? one : new Arith.OrOr(cond, one);
            }
            chain = new Ctl.If(cond, clauseBody(cs, i), chain);
        }
        if (chain != null) {
            out.add(chain);
        }
        targets.pop();
        Stmt all = seq(out.isEmpty() ? List.of(new Ctl.Block(new Stmt[0])) : out);
        return target.brk == null ? all : new Ctl.BreakTarget(all, target.brk);
    }

    private Stmt typeSwitch(Gir.Stmt.TypeSwitch ts) {
        Target target = new Target(takeLabel(), false);
        targets.push(target);
        List<Stmt> out = new ArrayList<>();
        if (ts.init() != null) {
            out.add(stmt(ts.init()));
        }
        GoType xt = ty(ts.x());
        if (!xt.isInterface()) {
            throw new Unsupported("type switch on a type-parameter operand");
        }
        int slot = tempObject();
        out.add(new N.WriteRef(slot, expr(ts.x())));
        Gir.TypeClause[] cs = ts.clauses();
        Stmt chain = null;
        for (Gir.TypeClause c : cs) {
            if (c.isDefault()) {
                chain = typeClauseBody(c, slot, null);
            }
        }
        for (int i = cs.length - 1; i >= 0; i--) {
            Gir.TypeClause c = cs[i];
            if (c.isDefault()) {
                continue;
            }
            Bool cond = null;
            GoType single = null;
            for (int id : c.types()) {
                Bool one;
                if (id == 0) {
                    one = new Arith.IsNil(new N.ReadRef(slot));
                } else {
                    GoType t = ty(id);
                    one = t.isInterface() ? new Ifaces.TypeImplements(new N.ReadRef(slot), t)
                            : new Ifaces.TypeIs(new N.ReadRef(slot), t);
                    single = c.types().length == 1 && !t.isInterface() ? t : null;
                }
                cond = cond == null ? one : new Arith.OrOr(cond, one);
            }
            chain = new Ctl.If(cond, typeClauseBody(c, slot, single), chain);
        }
        if (chain != null) {
            out.add(chain);
        }
        targets.pop();
        Stmt all = seq(out);
        return target.brk == null ? all : new Ctl.BreakTarget(all, target.brk);
    }

    private Stmt typeClauseBody(Gir.TypeClause c, int slot, GoType concrete) {
        Stmt b = block(c.body());
        if (c.local() < 0) {
            return b;
        }
        Expr v;
        if (concrete != null) {
            Expr data = new Ifaces.Data(new N.ReadRef(slot));
            v = concrete.carrier() == GoType.C_FLAT ? new Mem.Copy(data) : typed(concrete, data);
        } else {
            v = new N.ReadRef(slot);
        }
        return new Ctl.Block(new Stmt[] {declare(c.local(), v), b});
    }

    private Stmt select(Gir.Stmt.Select sel) {
        Target target = new Target(takeLabel(), false);
        targets.push(target);
        Gir.CommClause[] cs = sel.clauses();
        int valueSlot = tempObject();
        int okSlot = fdb.addSlot(FrameSlotKind.Boolean, null, null);
        int indexSlot = tempLong();
        List<Expr> chans = new ArrayList<>();
        List<Expr> sends = new ArrayList<>();
        List<Stmt> bodies = new ArrayList<>();
        Stmt defaultBody = null;
        for (Gir.CommClause c : cs) {
            if (c.isDefault()) {
                defaultBody = block(c.body());
                continue;
            }
            List<Stmt> b = new ArrayList<>();
            switch (c.comm()) {
                case Gir.Stmt.Send s -> {
                    chans.add(expr(s.ch()));
                    sends.add(value(s.value()));
                }
                case Gir.Stmt.Expr x -> {
                    chans.add(expr(((Gir.Expr.Recv) x.x()).ch()));
                    sends.add(null);
                }
                case Gir.Stmt.Assign a -> {
                    Gir.Expr.Recv r = (Gir.Expr.Recv) a.rhs()[0];
                    chans.add(expr(r.ch()));
                    sends.add(null);
                    recvStores(a.lhs(), null, r, valueSlot, okSlot, b);
                }
                case Gir.Stmt.Define d -> {
                    Gir.Expr.Recv r = (Gir.Expr.Recv) d.rhs()[0];
                    chans.add(expr(r.ch()));
                    sends.add(null);
                    recvStores(d.lhs(), d.fresh(), r, valueSlot, okSlot, b);
                }
                default -> throw new IllegalStateException("select communication " + c.comm());
            }
            b.add(block(c.body()));
            bodies.add(seq(b));
        }
        Stmt chain = defaultBody;
        for (int i = bodies.size() - 1; i >= 0; i--) {
            chain = new Ctl.If(ArithFactory.EqI64NodeGen.create(new N.ReadI64(indexSlot), new N.ConstI64(i)),
                    bodies.get(i), chain);
        }
        List<Stmt> out = new ArrayList<>();
        out.add(new N.WriteI64(indexSlot, new Chans.Select(chans.toArray(new Expr[0]), sends.toArray(new Expr[0]),
                defaultBody != null, valueSlot, okSlot)));
        if (chain != null) {
            out.add(chain);
        }
        targets.pop();
        Stmt all = seq(out);
        return target.brk == null ? all : new Ctl.BreakTarget(all, target.brk);
    }

    private void recvStores(Gir.Expr[] lhs, int[] fresh, Gir.Expr.Recv r, int valueSlot, int okSlot, List<Stmt> out) {
        GoType elem = ty(r.ch()).u().elem;
        Expr v = new N.ReadRef(valueSlot);
        v = elem.carrier() == GoType.C_FLAT ? new Mem.Copy(v) : typed(elem, v);
        out.add(storeTo(lhs[0], fresh, v, null));
        if (lhs.length > 1) {
            out.add(storeTo(lhs[1], fresh, new N.ReadBool(okSlot), null));
        }
    }

    // ---------------------------------------------------------------- loops

    private Stmt forStmt(Gir.Stmt.For f) {
        Target target = new Target(takeLabel(), true);
        List<Stmt> out = new ArrayList<>();
        if (f.init() != null) {
            out.add(stmt(f.init()));
        }
        Bool cond = f.cond() == null ? null : bool(f.cond());
        targets.push(target);
        Stmt b = block(f.body());
        targets.pop();
        List<Stmt> post = new ArrayList<>();
        for (int local : f.perIter()) {
            // Go 1.22: each iteration has its own copy of a loop variable that escapes.
            boolean escapes = (body.locals()[local].flags() & (8 | 16)) != 0;
            if (escapes) {
                post.add(new N.WriteRef(slots[local], new Mem.Copy(new N.ReadRef(slots[local]))));
            }
        }
        if (f.post() != null) {
            post.add(stmt(f.post()));
        }
        out.add(new Ctl.For(cond, b, post.isEmpty() ? null : seq(post), target.brk, target.cont));
        return seq(out);
    }

    private Stmt loop(Target target, Bool cond, List<Stmt> prelude, Gir.Stmt[] bodyStmts, Stmt post) {
        targets.push(target);
        Stmt b = block(bodyStmts);
        targets.pop();
        // `continue` must still run the post statement: it targets the user body only.
        Stmt inner = target.cont == null ? b : new Ctl.BreakTarget(b, target.cont);
        prelude.add(inner);
        return new Ctl.For(cond, seq(prelude), post, target.brk, null);
    }

    private Stmt rangeVar(Gir.Stmt.Range r, Gir.Expr target, Expr v) {
        return storeTo(target, r.define() ? r.fresh() : null, v, null);
    }

    private static boolean used(Gir.Expr e) {
        return e != null && !(e instanceof Gir.Expr.Blank);
    }

    private Stmt range(Gir.Stmt.Range r) {
        Target target = new Target(takeLabel(), true);
        GoType xt = ty(r.x()).u();
        List<Stmt> out = new ArrayList<>();
        List<Stmt> pre = new ArrayList<>();
        int idx = tempLong();
        Supplier<I64> readIdx = () -> new N.ReadI64(idx);
        Stmt inc = new N.WriteI64(idx, ArithFactory.AddI64NodeGen.create(readIdx.get(), new N.ConstI64(1)));
        aux();
        switch (xt.kind) {
            case GoType.SLICE: {
                int s = tempObject();
                int len = tempLong();
                out.add(new N.WriteRef(s, expr(r.x())));
                out.add(new N.WriteI64(len, new Data.SliceLen(new N.ReadRef(s))));
                out.add(new N.WriteI64(idx, new N.ConstI64(0)));
                if (used(r.key())) {
                    pre.add(rangeVar(r, r.key(), readIdx.get()));
                }
                if (used(r.value())) {
                    Loc loc = new Mem.SliceLoc(new N.ReadRef(s), readIdx.get(), xt.elem.nPrim(), xt.elem.nRef(), 0, 0,
                            auxP, auxR);
                    pre.add(rangeVar(r, r.value(), load(xt.elem, loc)));
                }
                out.add(loop(target, ArithFactory.LtI64NodeGen.create(readIdx.get(), new N.ReadI64(len)), pre,
                        r.body(), inc));
                return seq(out);
            }
            case GoType.ARRAY, GoType.POINTER: {
                GoType at = xt.kind == GoType.POINTER ? xt.elem.u() : xt;
                int a = tempObject();
                boolean needValue = used(r.value());
                if (needValue || !pure(r.x())) {
                    // Ranging over an array value iterates over a copy of it.
                    out.add(new N.WriteRef(a, xt.kind == GoType.ARRAY ? new Mem.Copy(expr(r.x())) : expr(r.x())));
                }
                out.add(new N.WriteI64(idx, new N.ConstI64(0)));
                if (used(r.key())) {
                    pre.add(rangeVar(r, r.key(), readIdx.get()));
                }
                if (needValue) {
                    Loc base = xt.kind == GoType.ARRAY ? new Mem.FlatLoc(new N.ReadRef(a), 0, 0)
                            : new Mem.PtrLoc(new N.ReadRef(a), 0, 0, auxP, auxR);
                    Loc loc = new Mem.IndexLoc(base, readIdx.get(), at.len, at.elem.nPrim(), at.elem.nRef(), 0, 0,
                            auxP, auxR);
                    pre.add(rangeVar(r, r.value(), load(at.elem, loc)));
                }
                out.add(loop(target, ArithFactory.LtI64NodeGen.create(readIdx.get(), new N.ConstI64(at.len)), pre,
                        r.body(), inc));
                return seq(out);
            }
            case GoType.STRING: {
                int s = tempObject();
                int d = tempLong();
                int next = tempLong();
                out.add(new N.WriteRef(s, expr(r.x())));
                out.add(new N.WriteI64(idx, new N.ConstI64(0)));
                pre.add(new N.WriteI64(d, new Data.StrDecode(new N.ReadRef(s), readIdx.get())));
                pre.add(new N.WriteI64(next, ArithFactory.AddI64NodeGen.create(readIdx.get(),
                        ArithFactory.ShrU64NodeGen.create(new N.ReadI64(d), new N.ConstI64(32)))));
                if (used(r.key())) {
                    pre.add(rangeVar(r, r.key(), readIdx.get()));
                }
                if (used(r.value())) {
                    pre.add(rangeVar(r, r.value(), ArithFactory.W32NodeGen.create(new N.ReadI64(d))));
                }
                out.add(loop(target,
                        ArithFactory.LtI64NodeGen.create(readIdx.get(), new Data.StrLen(new N.ReadRef(s))), pre,
                        r.body(), new N.WriteI64(idx, new N.ReadI64(next))));
                return seq(out);
            }
            case GoType.MAP: {
                int m = tempObject();
                int it = tempObject();
                out.add(new N.WriteRef(m, expr(r.x())));
                out.add(new N.WriteRef(it, new Data.MapIter(new N.ReadRef(m))));
                Bool cond = ArithFactory.GeI64NodeGen.create(new N.TeeI64(idx, new Data.MapNext(new N.ReadRef(it))),
                        new N.ConstI64(0));
                if (used(r.key())) {
                    GoType kt = xt.mapKey;
                    Expr k = new Data.MapKey(new N.ReadRef(m), readIdx.get());
                    switch (kt.carrier()) {
                        case GoType.C_BOOL:
                            k = new N.RawBool(new N.AsI64(k));
                            break;
                        case GoType.C_FLAT:
                            k = new Mem.Copy(k);
                            break;
                        default:
                            k = typed(kt, k);
                    }
                    pre.add(rangeVar(r, r.key(), k));
                }
                if (used(r.value())) {
                    pre.add(rangeVar(r, r.value(),
                            load(xt.elem, new Mem.MapEntryLoc(new N.ReadRef(m), readIdx.get(), auxP, auxR))));
                }
                out.add(loop(target, cond, pre, r.body(), null));
                return seq(out);
            }
            case GoType.CHAN: {
                int c = tempObject();
                int v = tempObject();
                out.add(new N.WriteRef(c, expr(r.x())));
                if (used(r.key())) {
                    Expr e = new N.ReadRef(v);
                    pre.add(rangeVar(r, r.key(),
                            xt.elem.carrier() == GoType.C_FLAT ? new Mem.Copy(e) : typed(xt.elem, e)));
                }
                out.add(loop(target, new Chans.RecvNext(new N.ReadRef(c), v), pre, r.body(), null));
                return seq(out);
            }
            case GoType.FUNC:
                throw new Unsupported("range over func");
            default: {
                if (!xt.isInteger()) {
                    throw new Unsupported("range over " + xt.key);
                }
                int n = tempLong();
                out.add(new N.WriteI64(n, i64(r.x())));
                out.add(new N.WriteI64(idx, new N.ConstI64(0)));
                if (used(r.key())) {
                    pre.add(rangeVar(r, r.key(), readIdx.get()));
                }
                out.add(loop(target, ArithFactory.LtI64NodeGen.create(readIdx.get(), new N.ReadI64(n)), pre, r.body(),
                        inc));
                return seq(out);
            }
        }
    }
}
