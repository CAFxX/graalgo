package graalgo.go.types;

import com.oracle.truffle.api.CompilerDirectives.CompilationFinal;

/**
 * The method table of one (dynamic type, interface type) pair, in the interface's method order,
 * so a call site holds a constant method index (truffle-design §4.6). Immutable; cached on the
 * dynamic type ({@link GoType#itab}).
 */
public record Itab(GoType dyn, GoType iface, @CompilationFinal(dimensions = 1) Method[] methods) {
}
