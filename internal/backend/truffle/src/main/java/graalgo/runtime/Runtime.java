package graalgo.runtime;

/**
 * Runtime entry points callable from generated Truffle ASTs.
 *
 * <p>M1 scope: the print path only — the hand-built hello AST exercises it.
 * WS G grows this into the intrinsic surface (builtins + runtime stubs) under
 * the frozen signature set (GG-0020/GG-0021, PLAN.md D4/D7).
 */
public final class Runtime {
    private Runtime() {}

    /**
     * Print path for a single string: writes the string followed by a newline
     * to stdout.
     *
     * <p>Parity note (GG-0045 stance, recorded for WS G): Go's print/println
     * builtins write to <em>stderr</em>, not stdout. The M1 hello fixture uses
     * stdout deliberately — the harness compares stdout — and the exact stream
     * routing of the real builtins is decided when they land, not here.
     */
    public static void println(String s) {
        System.out.print(s);
        System.out.print('\n');
    }
}
