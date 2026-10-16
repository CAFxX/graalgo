package graalgo.rtmemstats;

import java.util.ArrayList;
import java.util.List;

import graalgo.values.GoKind;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * Unit tests for the PKG-051 MemStats-family intrinsics
 * ({@code runtimereadmemstats}, {@code runtimegoroot},
 * {@code runtimeversion}, {@code runtimestack}).
 *
 * <p>These pin the backend side of the contract the frontend's MemStats
 * family relies on: JVM-telemetry population of {@code runtime.MemStats},
 * GOROOT/version resolution, and gc-style stack rendering. The wire-kind
 * registration itself is covered end to end by
 * {@code graalgo test encoding/base64} (PKG-051: {@code runtime/debug}
 * calls {@code runtime.Version()} via {@code ReadBuildInfo}).
 */
public final class RtMemStatsTest {

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEq(long e, long a, String msg) {
        if (e != a) {
            throw new AssertionError(msg + " expected=" + e + " actual=" + a);
        }
    }

    /** Runs every test; called by IfaceTestRunner via reflection. */
    public void testVersionLooksLikeGoRelease() {
        String v = GoToolchain.version();
        assertTrue(v != null && v.matches("^go[0-9]+\\.[0-9]+.*"),
                "version() must look like a Go release tag, got: " + v);
    }

    public void testGorootEnvWins() {
        // gc rule: the GOROOT env var at process start wins when set.
        // (We cannot set env vars in-process; assert the contract in the
        // direction observable here.)
        String env = System.getenv("GOROOT");
        String g = GoToolchain.goroot();
        assertTrue(g != null, "goroot() must not return null");
        if (env != null && !env.isEmpty()) {
            assertTrue(g.equals(env),
                    "goroot() must return $GOROOT when set: env=" + env + " got=" + g);
        }
    }

    /**
     * Builds a {@code runtime.MemStats}-shaped descriptor (field names and
     * kinds mirror the synthetic runtime view's declaration) and checks
     * the structural invariants of the JVM-telemetry fill.
     */
    public void testFillMemStatsInvariants() {
        // name, kind, arrayLen (-1 = scalar), arrayElemKind
        String[][] spec = {
                {"Alloc", "UINT64"}, {"TotalAlloc", "UINT64"}, {"Sys", "UINT64"},
                {"Lookups", "UINT64"}, {"Mallocs", "UINT64"}, {"Frees", "UINT64"},
                {"HeapAlloc", "UINT64"}, {"HeapSys", "UINT64"}, {"HeapIdle", "UINT64"},
                {"HeapInuse", "UINT64"}, {"HeapReleased", "UINT64"}, {"HeapObjects", "UINT64"},
                {"StackInuse", "UINT64"}, {"StackSys", "UINT64"},
                {"MSpanInuse", "UINT64"}, {"MSpanSys", "UINT64"},
                {"MCacheInuse", "UINT64"}, {"MCacheSys", "UINT64"},
                {"BuckHashSys", "UINT64"}, {"GCSys", "UINT64"}, {"OtherSys", "UINT64"},
                {"NextGC", "UINT64"}, {"LastGC", "UINT64"}, {"PauseTotalNs", "UINT64"},
                {"PauseNs", "ARRAY", "256", "UINT64"}, {"PauseEnd", "ARRAY", "256", "UINT64"},
                {"NumGC", "UINT32"}, {"NumForcedGC", "UINT32"},
                {"GCCPUFraction", "FLOAT64"},
                {"EnableGC", "BOOL"}, {"DebugGC", "BOOL"},
                {"BySize", "ARRAY", "61", "STRUCT"},
        };
        List<String> names = new ArrayList<>();
        List<GoKind> kinds = new ArrayList<>();
        List<Integer> lens = new ArrayList<>();
        List<GoKind> elemKinds = new ArrayList<>();
        for (String[] row : spec) {
            names.add(row[0]);
            kinds.add(GoKind.valueOf(row[1]));
            if (row.length > 2) {
                lens.add(Integer.parseInt(row[2]));
                elemKinds.add(GoKind.valueOf(row[3]));
            } else {
                lens.add(-1);
                elemKinds.add(null);
            }
        }
        GoStructDescriptor desc = GoStructDescriptor.layout(
                "runtime.MemStats", names, kinds, lens, elemKinds);
        GoStruct ms = GoStruct.zero(desc);
        RtMemStats.fillMemStats(ms);

        long alloc = get(ms, desc, "Alloc");
        long sys = get(ms, desc, "Sys");
        long heapAlloc = get(ms, desc, "HeapAlloc");
        long heapSys = get(ms, desc, "HeapSys");
        long heapIdle = get(ms, desc, "HeapIdle");
        long heapInuse = get(ms, desc, "HeapInuse");
        assertEq(heapAlloc, alloc, "Alloc must equal HeapAlloc");
        assertEq(heapInuse, alloc, "HeapInuse must equal Alloc");
        assertTrue(sys >= heapSys, "Sys must cover HeapSys");
        assertTrue(heapSys >= heapAlloc, "HeapSys must cover HeapAlloc");
        assertEq(heapIdle, heapSys - heapInuse, "HeapIdle must be HeapSys-HeapInuse");
        assertTrue(get(ms, desc, "NumGC") >= 0, "NumGC must be >= 0");
        assertTrue(get(ms, desc, "PauseTotalNs") >= 0, "PauseTotalNs must be >= 0");
        assertEq(1, get(ms, desc, "EnableGC"), "EnableGC must be 1");
        assertEq(0, get(ms, desc, "DebugGC"), "DebugGC must be 0");
        double frac = Double.longBitsToDouble(get(ms, desc, "GCCPUFraction"));
        assertTrue(frac >= 0.0 && frac <= 1.0,
                "GCCPUFraction must be in [0,1], got " + frac);
        // Zero-equivalent fields the JVM cannot observe must stay zero.
        assertEq(0, get(ms, desc, "Lookups"), "Lookups must be 0");
        assertEq(0, get(ms, desc, "HeapReleased"), "HeapReleased must be 0");
        assertEq(0, get(ms, desc, "HeapObjects"), "HeapObjects must be 0");
    }

    /** Reads a longs-carried field by name (fail loud on layout drift). */
    private static long get(GoStruct ms, GoStructDescriptor desc, String name) {
        List<GoStructDescriptor.Field> fields = desc.fields();
        for (int i = 0; i < fields.size(); i++) {
            GoStructDescriptor.Field f = fields.get(i);
            if (f.name().equals(name)) {
                if (!f.inLongs()) {
                    throw new AssertionError("field " + name + " is not longs-carried");
                }
                // getLong takes the descriptor ordinal, not the carrier index.
                return ms.getLong(i);
            }
        }
        throw new AssertionError("no field " + name);
    }

    public void testFormatCurrentStack() {
        byte[] text = RtMemStats.formatCurrentStack(null);
        String s = new String(text, java.nio.charset.StandardCharsets.UTF_8);
        assertTrue(s.startsWith("goroutine "), "stack must start with 'goroutine '");
        assertTrue(s.contains("[running]:"), "stack must contain '[running]:'");
        assertTrue(s.endsWith("\n\n"), "stack must end with a blank line");
    }
}
