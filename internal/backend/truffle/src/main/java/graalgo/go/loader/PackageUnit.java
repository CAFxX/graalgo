package graalgo.go.loader;

import graalgo.gir.Gir;
import graalgo.gir.GirFile;
import graalgo.go.types.GoType;
import graalgo.go.types.GoType.Field;
import graalgo.go.types.GoType.IMethod;
import graalgo.go.types.Method;
import graalgo.go.types.TypeTable;
import java.util.HashMap;
import java.util.Map;

/**
 * One package of a bundle (toolexec-design §4.3): the decoded GIR tables, resolved lazily —
 * types into the language-level {@link TypeTable}, functions into {@link FunctionUnit}s whose
 * bodies stay undecoded — plus the layout of the package's globals object.
 */
public final class PackageUnit {
    public final Program program;
    /** Index of this package's globals object in the context ({@code GoProcess.globals}). */
    public final int index;
    public final String path;
    public final String name;
    public final GirFile gir;
    private final Gir.Sections s;
    private final GoType[] typeCache;
    private final boolean[] generic;
    private final FunctionUnit[] funcs;
    private final Map<String, Integer> funcByName = new HashMap<>();
    private final Map<String, Integer> objByName = new HashMap<>();
    private final Map<String, Gir.TypeDef> typeDefs = new HashMap<>();
    /** TYPEDEFS entries of unnamed struct types with promoted methods, by type id. */
    private final Map<Integer, Gir.TypeDef> structDefs = new HashMap<>();
    // Globals layout.
    private int[] globalPrim;
    private int[] globalRef;
    private int[] globalByte;
    private GoType globalsType;

    PackageUnit(Program program, int index, String path, GirFile gir) {
        this.program = program;
        this.index = index;
        this.path = path;
        this.gir = gir;
        this.s = gir.sections();
        this.name = gir.string(s.pkg.name());
        this.typeCache = new GoType[s.types.length + 1];
        this.generic = new boolean[s.types.length + 1];
        for (int i = 0; i < s.types.length; i++) {
            generic[i + 1] = gir.string(s.types[i].key()).indexOf('$') >= 0;
        }
        this.funcs = new FunctionUnit[s.funcs.length];
        for (int i = 0; i < s.funcs.length; i++) {
            funcByName.put(gir.string(s.funcs[i].name()), i);
        }
        for (int i = 0; i < s.objects.length; i++) {
            objByName.put(gir.string(s.objects[i].name()), i);
        }
        for (Gir.TypeDef td : s.typeDefs) {
            if (s.types[td.type() - 1].type() instanceof Gir.Type.Named n) {
                typeDefs.put(gir.string(s.symRefs[n.sym()].name()), td);
            } else {
                structDefs.put(td.type(), td);
            }
        }
    }

    public Gir.Sections sections() {
        return s;
    }

    public String str(int index) {
        return gir.string(index);
    }

    String typeKey(int id) {
        return gir.string(s.types[id - 1].key());
    }

    public synchronized FunctionUnit func(int index) {
        FunctionUnit f = funcs[index];
        if (f == null) {
            f = new FunctionUnit(this, index);
            funcs[index] = f;
        }
        return f;
    }

    /** The function or method with gc symbol name {@code name} ("F", "T.M", "(*T).M"). */
    public FunctionUnit funcNamed(String name) {
        Integer i = funcByName.get(name);
        if (i == null) {
            throw new IllegalStateException("undefined: " + path + "." + name);
        }
        return func(i);
    }

    /** The package-level object named {@code name}, as an OBJECTS index. */
    public int objectNamed(String name) {
        Integer i = objByName.get(name);
        if (i == null) {
            throw new IllegalStateException("undefined: " + path + "." + name);
        }
        return i;
    }

    /** The package a symbol reference names. */
    public PackageUnit packageOf(Gir.SymRef sym) {
        return program.packageNamed(str(sym.pkg()));
    }

    // ---------------------------------------------------------------- globals

    private void layoutGlobals() {
        synchronized (program.types) {
            layoutGlobalsLocked();
        }
    }

    private void layoutGlobalsLocked() {
        if (globalPrim != null) {
            return;
        }
        TypeTable table = program.types;
        int n = s.objects.length;
        int[] gp = new int[n];
        int[] gr = new int[n];
        int[] gb = new int[n];
        java.util.List<Field> fields = new java.util.ArrayList<>();
        for (int i = 0; i < n; i++) {
            if (s.objects[i].kind() == Gir.SymKind.VAR) {
                fields.add(new Field(str(s.objects[i].name()) + "·" + i, path, type(s.objects[i].type(), null),
                        false, ""));
            }
        }
        GoType t = table.struct(fields.toArray(new Field[0]));
        t.nPrim();
        int k = 0;
        for (int i = 0; i < n; i++) {
            if (s.objects[i].kind() == Gir.SymKind.VAR) {
                gp[i] = t.fields[k].primOff;
                gr[i] = t.fields[k].refOff;
                gb[i] = t.fields[k].byteOff;
                k++;
            }
        }
        globalsType = t;
        globalRef = gr;
        globalByte = gb;
        globalPrim = gp;
    }

    /** The struct type whose fields are this package's variables (the globals object's layout). */
    public GoType globalsType() {
        layoutGlobals();
        return globalsType;
    }

    public int globalPrim(int obj) {
        layoutGlobals();
        return globalPrim[obj];
    }

    public int globalRef(int obj) {
        layoutGlobals();
        return globalRef[obj];
    }

    public int globalByte(int obj) {
        layoutGlobals();
        return globalByte[obj];
    }

    /** A variable's storage: the package whose globals object holds it and its OBJECTS index. */
    public record Global(PackageUnit unit, int obj) {}

    /**
     * Where variable {@code obj} lives: in this package, unless the link unified it with
     * another package's variable ({@code //go:linkname} on variables, toolexec-design §3.3).
     */
    public Global global(int obj) {
        String[] alias = program.varAlias(this, str(s.objects[obj].name()));
        if (alias == null) {
            return new Global(this, obj);
        }
        PackageUnit owner = program.packageNamed(alias[0]);
        return new Global(owner, owner.objectNamed(alias[1]));
    }

    // ---------------------------------------------------------------- types

    /** The descriptor of TYPES entry {@code id} (1-based; 0: none) under {@code env}. */
    public GoType type(int id, TypeEnv env) {
        if (id == 0) {
            return null;
        }
        synchronized (program.types) {
            return typeLocked(id, env);
        }
    }

    private GoType typeLocked(int id, TypeEnv env) {
        if (!generic[id]) {
            GoType t = typeCache[id];
            if (t == null) {
                t = build(id, null);
                typeCache[id] = t;
            }
            return t;
        }
        if (env == null) {
            throw new IllegalStateException("type " + typeKey(id) + " used outside an instantiation");
        }
        long key = ((long) index << 32) | id;
        GoType t = env.cache.get(key);
        if (t == null) {
            t = build(id, env);
            env.cache.put(key, t);
        }
        return t;
    }

    private GoType[] types(int[] ids, TypeEnv env) {
        GoType[] out = new GoType[ids.length];
        for (int i = 0; i < ids.length; i++) {
            out[i] = type(ids[i], env);
        }
        return out;
    }

    private GoType build(int id, TypeEnv env) {
        TypeTable table = program.types;
        Gir.Type t = s.types[id - 1].type();
        switch (t) {
            case Gir.Type.Basic b:
                return table.basic(b.kind());
            case Gir.Type.Pointer p:
                return table.pointer(type(p.elem(), env));
            case Gir.Type.Slice sl:
                return table.slice(type(sl.elem(), env));
            case Gir.Type.Array a:
                return table.array(a.len(), type(a.elem(), env));
            case Gir.Type.Map m:
                return table.map(type(m.key(), env), type(m.elem(), env));
            case Gir.Type.Chan c:
                return table.chan(c.dir(), type(c.elem(), env));
            case Gir.Type.Struct st: {
                Field[] fields = new Field[st.fields().length];
                for (int i = 0; i < fields.length; i++) {
                    Gir.Field f = st.fields()[i];
                    fields[i] = new Field(str(f.name()), str(f.pkg()), type(f.type(), env), f.embedded(),
                            str(f.tag()));
                }
                GoType struct = table.struct(fields);
                Gir.TypeDef td = structDefs.get(id);
                if (td != null) {
                    struct.attachMethods(() -> methodSet(td, struct, env, false),
                            () -> methodSet(td, struct, env, true));
                }
                return struct;
            }
            case Gir.Type.Signature sig:
                return table.func(types(sig.params(), env), types(sig.results(), env), sig.variadic());
            case Gir.Type.Interface in: {
                IMethod[] methods = new IMethod[in.methods().length];
                for (int i = 0; i < methods.length; i++) {
                    Gir.IfaceMethod m = in.methods()[i];
                    methods[i] = new IMethod(str(m.name()), str(m.pkg()), type(m.sig(), env));
                }
                // Embedded non-interface elements only occur in constraints, which never have
                // values; their key keeps distinct constraints distinct.
                return table.iface(methods, in.embeddeds().length == 0 ? "" : typeKey(id));
            }
            case Gir.Type.Tuple tu:
                return table.tuple(types(tu.elems(), env));
            case Gir.Type.Named n:
                return named(s.symRefs[n.sym()], types(n.targs(), env));
            case Gir.Type.TypeParam tp: {
                GoType arg = env == null ? null : env.params.get(typeKey(id));
                if (arg == null) {
                    throw new IllegalStateException("type parameter " + typeKey(id) + " is not bound");
                }
                return arg;
            }
            case Gir.Type.Union u:
                return table.emptyInterface;
        }
    }

    private GoType named(Gir.SymRef sym, GoType[] targs) {
        String pkgPath = str(sym.pkg());
        String typeName = str(sym.name());
        TypeTable table = program.types;
        if (pkgPath.isEmpty()) {
            GoType universe = table.lookup(typeName);
            if (universe == null) {
                throw new IllegalStateException("unknown universe type " + typeName);
            }
            return universe;
        }
        PackageUnit decl = program.packageNamed(pkgPath);
        GoType[] self = new GoType[1];
        GoType t = table.named(pkgPath, typeName, targs, () -> decl.underlying(typeName, targs),
                () -> decl.methodSet(typeName, self[0], targs, false),
                () -> decl.methodSet(typeName, self[0], targs, true));
        self[0] = t;
        table.setPackageName(t, decl.name);
        Gir.TypeDef td = decl.typeDefs.get(typeName);
        if (td != null && td.size() >= 0 && td.tparams().length == 0) {
            t.expectLayout(td.size(), td.align(), td.offsets());
        }
        return t;
    }

    private Gir.TypeDef typeDef(String typeName) {
        Gir.TypeDef td = typeDefs.get(typeName);
        if (td == null) {
            throw new IllegalStateException("no definition of type " + path + "." + typeName + " in the bundle");
        }
        return td;
    }

    private TypeEnv typeEnv(Gir.TypeDef td, GoType[] targs) {
        if (td.tparams().length == 0) {
            return null;
        }
        String[] keys = new String[td.tparams().length];
        for (int i = 0; i < keys.length; i++) {
            keys[i] = typeKey(td.tparams()[i]);
        }
        return new TypeEnv(keys, targs);
    }

    private GoType underlying(String typeName, GoType[] targs) {
        Gir.TypeDef td = typeDef(typeName);
        return type(td.underlying(), typeEnv(td, targs));
    }

    private Map<String, Method> methodSet(String typeName, GoType owner, GoType[] targs, boolean pointer) {
        Gir.TypeDef td = typeDef(typeName);
        return methodSet(td, owner, typeEnv(td, targs), pointer);
    }

    private Map<String, Method> methodSet(Gir.TypeDef td, GoType owner, TypeEnv env, boolean pointer) {
        Map<String, Method> out = new HashMap<>();
        for (Gir.MsetEntry e : pointer ? td.ptrMset() : td.mset()) {
            String mname = str(e.name());
            String mpkg = str(e.pkg());
            String key = mpkg.isEmpty() ? mname : mpkg + "." + mname;
            int[] path = new int[e.path().length];
            for (int i = 0; i < path.length; i++) {
                path[i] = (int) e.path()[i];
            }
            if (e.method() < 0) {
                // Promoted from an embedded interface: the receiver the path leads to is that
                // interface value, and the method is a dynamic call on it.
                GoType iface = type(e.recvType(), env);
                out.put(key, new Method(key, path, false, owner, pointer,
                        () -> graalgo.go.build.NodeFactory.ifaceForwarder(program.language, iface, key),
                        () -> imethodSig(iface, key)));
                continue;
            }
            Gir.SymRef msym = s.symRefs[e.method()];
            String symName = str(msym.name());
            PackageUnit mpkgUnit = packageOf(msym);
            GoType recv = type(e.recvType(), env);
            out.put(key, new Method(key, path, symName.startsWith("(*"), owner, pointer,
                    () -> mpkgUnit.funcNamed(symName).instance(recv.targs).get(),
                    () -> mpkgUnit.methodSig(symName, recv.targs)));
        }
        return out;
    }

    /** The receiver-less signature of method {@code symName} under the receiver's type arguments. */
    private GoType methodSig(String symName, GoType[] recvTargs) {
        FunctionUnit fu = funcNamed(symName);
        FunctionUnit.Instance in = fu.func.tparams().length == 0 ? fu.instance(null) : fu.instance(recvTargs);
        return type(fu.func.sig(), in.env);
    }

    private static GoType imethodSig(GoType iface, String key) {
        for (GoType.IMethod m : iface.u().imethods) {
            if (m.key.equals(key)) {
                return m.sig;
            }
        }
        throw new IllegalStateException("interface " + iface.key + " has no method " + key);
    }
}
