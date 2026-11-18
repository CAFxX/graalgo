package graalgo.go.build;

/**
 * The defer-path trace of GG-0071's boundary test (test/m1a/defer_path.sh): with
 * {@code -Dgraalgo.go.traceDeferPath=true} the node factory reports, for every function that
 * defers, whether its tree is open-coded or on the chain. Off by default; the one place the
 * factory writes to stderr (the debug-leftover gate exempts this class).
 */
final class DeferPathTrace {
    private DeferPathTrace() {}

    static final boolean ENABLED = Boolean.getBoolean("graalgo.go.traceDeferPath");

    static void report(String function, boolean openCoded) {
        System.err.println("graalgo: defer path " + function + " " + (openCoded ? "open-coded" : "chain"));
    }
}
