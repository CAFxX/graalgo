package graalgo.go.types;

import com.oracle.truffle.api.CompilerDirectives.CompilationFinal;

/**
 * The method table of one (dynamic type, interface type) pair, in the interface's method order,
 * so a call site holds a constant method index (truffle-design §4.6). Immutable; cached on the
 * dynamic type ({@link GoType#itab}).
 */
public final class Itab {
    public final GoType dyn;
    public final GoType iface;
    @CompilationFinal(dimensions = 1) public final Method[] methods;

    Itab(GoType dyn, GoType iface, Method[] methods) {
        this.dyn = dyn;
        this.iface = iface;
        this.methods = methods;
    }
}
