package graalgo.go.types;

import graalgo.go.types.GoType.Field;
import graalgo.go.types.GoType.IMethod;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.function.Supplier;

/**
 * The language-level interning table: one {@link GoType} per canonical key (toolexec-design
 * §3.4). Keys are built here from the components, in the shape of the frontend's
 * {@code ir.TypeKeyer}, so that a type reached through a generic instantiation and the same type
 * written out in another package intern to one descriptor.
 */
public final class TypeTable {
    private static final GoType[] NONE = new GoType[0];
    private static final String[] BASIC_NAMES = {null, "bool", "int", "int8", "int16", "int32", "int64", "uint",
            "uint8", "uint16", "uint32", "uint64", "uintptr", "float32", "float64", "complex64", "complex128",
            "string", "unsafe.Pointer"};

    private final ConcurrentHashMap<String, GoType> types = new ConcurrentHashMap<>();
    private final GoType[] basics = new GoType[BASIC_NAMES.length];

    public final GoType bool;
    public final GoType int_;
    public final GoType string;
    public final GoType float64;
    public final GoType complex128;
    public final GoType emptyInterface;
    /** The universe type {@code error}. */
    public final GoType error;
    /** {@code interface { String() string }}: gc's printpanicval {@code stringer} case. */
    public final GoType stringer;

    public TypeTable() {
        for (int k = 1; k < BASIC_NAMES.length; k++) {
            basics[k] = intern(new GoType(this, BASIC_NAMES[k], k, null, null, NONE, null, null, 0, 0, null, null, null,
                    false, null));
        }
        bool = basics[GoType.BOOL];
        int_ = basics[GoType.INT];
        string = basics[GoType.STRING];
        float64 = basics[GoType.FLOAT64];
        complex128 = basics[GoType.COMPLEX128];
        emptyInterface = iface(new IMethod[0], "");
        GoType errorSig = func(NONE, new GoType[] {string}, false);
        GoType errorUnder = iface(new IMethod[] {new IMethod("Error", "", errorSig)}, "");
        error = named("", "error", NONE, () -> errorUnder, null, null);
        stringer = iface(new IMethod[] {new IMethod("String", "", errorSig)}, "");
        named("", "comparable", NONE, () -> emptyInterface, null, null);
    }

    private GoType intern(GoType t) {
        GoType prev = types.putIfAbsent(t.key, t);
        return prev != null ? prev : t;
    }

    /** A basic type by GIR BasicKind; untyped kinds map to their default types. */
    public GoType basic(int kind) {
        return switch (kind) {
            case 19 -> basics[GoType.BOOL];
            case 20 -> basics[GoType.INT];
            case 21 -> basics[GoType.INT32];
            case 22 -> basics[GoType.FLOAT64];
            case 23 -> basics[GoType.COMPLEX128];
            case 24 -> basics[GoType.STRING];
            case 25 -> emptyInterface;
            default -> basics[kind];
        };
    }

    public GoType pointer(GoType elem) {
        String key = "*" + elem.key;
        GoType t = types.get(key);
        return t != null ? t
                : intern(new GoType(this, key, GoType.POINTER, null, null, NONE, elem, null, 0, 0, null, null, null, false,
                        null));
    }

    public GoType slice(GoType elem) {
        String key = "[]" + elem.key;
        GoType t = types.get(key);
        return t != null ? t
                : intern(new GoType(this, key, GoType.SLICE, null, null, NONE, elem, null, 0, 0, null, null, null, false,
                        null));
    }

    public GoType array(long len, GoType elem) {
        String key = "[" + len + "]" + elem.key;
        GoType t = types.get(key);
        return t != null ? t
                : intern(new GoType(this, key, GoType.ARRAY, null, null, NONE, elem, null, len, 0, null, null, null, false,
                        null));
    }

    public GoType map(GoType k, GoType v) {
        String key = "map[" + k.key + "]" + v.key;
        GoType t = types.get(key);
        return t != null ? t
                : intern(new GoType(this, key, GoType.MAP, null, null, NONE, v, k, 0, 0, null, null, null, false, null));
    }

    /** dir: GIR ChanDir (1 both, 2 send, 3 recv). */
    public GoType chan(int dir, GoType elem) {
        String e = elem.kind == GoType.CHAN ? "(" + elem.key + ")" : elem.key;
        String key = (dir == 3 ? "<-chan " : dir == 2 ? "chan<- " : "chan ") + e;
        GoType t = types.get(key);
        return t != null ? t
                : intern(new GoType(this, key, GoType.CHAN, null, null, NONE, elem, null, 0, dir, null, null, null, false,
                        null));
    }

    public GoType struct(Field[] fields) {
        StringBuilder sb = new StringBuilder("struct {");
        for (int i = 0; i < fields.length; i++) {
            Field f = fields[i];
            sb.append(i > 0 ? "; " : " ");
            if (f.embedded) {
                sb.append("embed ");
            } else {
                sb.append(f.pkg.isEmpty() ? f.name : f.pkg + "." + f.name).append(' ');
            }
            sb.append(f.type.key);
            if (!f.tag.isEmpty()) {
                sb.append(" \"").append(f.tag.replace("\\", "\\\\").replace("\"", "\\\"")).append('"');
            }
        }
        String key = sb.append(" }").toString();
        GoType t = types.get(key);
        return t != null ? t
                : intern(new GoType(this, key, GoType.STRUCT, null, null, NONE, null, null, 0, 0, fields, null, null, false,
                        null));
    }

    public GoType func(GoType[] params, GoType[] results, boolean variadic) {
        String key = "func" + sigKey(params, results, variadic);
        GoType t = types.get(key);
        return t != null ? t
                : intern(new GoType(this, key, GoType.FUNC, null, null, NONE, null, null, 0, 0, null, params, results,
                        variadic, null));
    }

    private static String sigKey(GoType[] params, GoType[] results, boolean variadic) {
        StringBuilder sb = new StringBuilder("(");
        for (int i = 0; i < params.length; i++) {
            sb.append(i > 0 ? ", " : "");
            if (variadic && i == params.length - 1) {
                sb.append("...").append(params[i].elem.key);
            } else {
                sb.append(params[i].key);
            }
        }
        sb.append(')');
        if (results.length == 1) {
            sb.append(' ').append(results[0].key);
        } else if (results.length > 1) {
            sb.append(' ').append(tupleKey(results));
        }
        return sb.toString();
    }

    private static String tupleKey(GoType[] elems) {
        StringBuilder sb = new StringBuilder("(");
        for (int i = 0; i < elems.length; i++) {
            sb.append(i > 0 ? ", " : "").append(elems[i].key);
        }
        return sb.append(')').toString();
    }

    /** embedKey: the keys of embedded non-interface elements (constraint type sets), or "". */
    public GoType iface(IMethod[] methods, String embedKey) {
        StringBuilder sb = new StringBuilder("interface {");
        int n = 0;
        if (!embedKey.isEmpty()) {
            sb.append(' ').append(embedKey);
            n++;
        }
        for (IMethod m : methods) {
            sb.append(n++ > 0 ? "; " : " ").append(m.key)
                    .append(sigKey(m.sig.params, m.sig.results, m.sig.variadic));
        }
        String key = sb.append(n > 0 ? " }" : "}").toString();
        GoType t = types.get(key);
        return t != null ? t
                : intern(new GoType(this, key, GoType.INTERFACE, null, null, NONE, null, null, 0, 0, null, null, null, false,
                        methods));
    }

    public GoType tuple(GoType[] elems) {
        String key = tupleKey(elems);
        GoType t = types.get(key);
        return t != null ? t
                : intern(new GoType(this, key, GoType.TUPLE, null, null, NONE, null, null, 0, 0, null, null, elems, false,
                        null));
    }

    /**
     * The defined type {@code pkg.name[targs]}; the suppliers give its underlying type and its
     * value and pointer method sets and are used only if this call interns the type.
     */
    public GoType named(String pkg, String name, GoType[] targs, Supplier<GoType> underlying,
            Supplier<Map<String, Method>> valueMethods, Supplier<Map<String, Method>> ptrMethods) {
        StringBuilder sb = new StringBuilder(pkg.isEmpty() ? name : pkg + "." + name);
        if (targs.length > 0) {
            sb.append('[');
            for (int i = 0; i < targs.length; i++) {
                sb.append(i > 0 ? "," : "").append(targs[i].key);
            }
            sb.append(']');
        }
        String key = sb.toString();
        GoType t = types.get(key);
        if (t == null) {
            t = intern(new GoType(this, key, GoType.NAMED, pkg, name, targs, null, null, 0, 0, null, null, null, false,
                    null));
        }
        if (underlying != null) {
            t.define(underlying, valueMethods, ptrMethods);
        }
        return t;
    }

    /** The interned type for a key, or null. */
    public GoType lookup(String key) {
        return types.get(key);
    }

    /** Records the package name used when printing types of package {@code path}. */
    public void setPackageName(GoType named, String pkgName) {
        named.pkgName = pkgName;
    }
}
