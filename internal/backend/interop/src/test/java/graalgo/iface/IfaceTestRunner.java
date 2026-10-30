package graalgo.iface;

import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import java.util.ArrayList;
import java.util.List;

/**
 * Minimal red-green test runner (mirrors Phase-1 {@code graalgo.test.Runner}
 * and Phase-2 {@code SchedTestRunner}): instantiates each registered test
 * class and invokes every public no-arg method whose name starts with
 * "test". Exits 0 on all-pass, 1 on any failure. New test classes register
 * in TESTS below.
 */
public final class IfaceTestRunner {
    private static final List<Class<?>> TESTS = new ArrayList<>();

    static {
        TESTS.add(IfaceDispatchNodeTest.class);
        TESTS.add(IfaceMethodTablesTest.class);
        TESTS.add(graalgo.frontend.DescriptorNamesTest.class);
        TESTS.add(LinknameTest.class);
        // fix-linkname (Caddy/godebug lane): sync.Map backing store.
        TESTS.add(SyncMapTest.class);
        TESTS.add(ItabForIntrinsicTest.class);
        TESTS.add(AtomicContentionTest.class);
        TESTS.add(graalgo.p3sync.launcher.LinknameEndToEndTest.class);
        // M10 F-1: concurrent range loops must not lose iterations.
        TESTS.add(graalgo.p3sync.launcher.RangeConcurrentRegressionTest.class);
        TESTS.add(graalgo.p3time.TimeChanLenCapTest.class);
        TESTS.add(graalgo.frontend.ChanBuilderTest.class);
        // P3-M8 (wave 2): the GoOs errno-mapper + intrinsic unit tests.
        TESTS.add(graalgo.p3os.GoOsErrnoTest.class);
        // P3-M8 (reviewer-2 P1-4/P1-5): reflect TypeInfo build pins.
        TESTS.add(graalgo.p3reflect.ReflectTypesTest.class);
        // N-P1-5 (review 2026-10-30-15): CallSlice over primitive slices.
        TESTS.add(graalgo.p3reflect.ReflectCallSliceTest.class);
        // P3-M9 (RISK-M9-1/RISK-M9-2): netpoller + socket traps.
        TESTS.add(graalgo.p3net.GoNetpollTest.class);
        // N4 review P1-N6 (2026-10-15): fmt.State itab lookup is fail-loud.
        // (fix-redmaster-r52: the FmtStateItabTest registration is removed
        // — the test class itself was deleted by the real-fmt2 removal
        // a527063d together with the Java fmt implementation it tested,
        // and the stale registration broke test compilation.)
        // PKG-012 remediation F1 (2026-10-15): sync.Cond notifyList state
        // isolation — identity-keyed, never structural.
        TESTS.add(graalgo.p3link.CondIsolationTest.class);
        // PKG-051 (2026-10-16): the runtime MemStats-family wire kinds
        // (runtimereadmemstats/runtimegoroot/runtimeversion/runtimestack) —
        // backend intrinsics with real JVM-telemetry semantics.
        TESTS.add(graalgo.rtmemstats.RtMemStatsTest.class);
        TESTS.add(graalgo.frontend.RtMemStatsBuilderTest.class);
        // fix-redmaster-r39: provenance-preserving byte derivation
        // (UnsafeTable.derive) soundness — a derivation can only ever
        // view its own base referent, never alias another live object.
        TESTS.add(graalgo.p3unsafe.UnsafeDeriveTest.class);
        // fix-redmaster-r40: string/slice header views for
        // unsafe.Pointer -> *Header reinterpretations (reflect Len).
        TESTS.add(graalgo.p3unsafe.HeaderViewTest.class);
        // fix-redmaster-r40: embedding prefix views for
        // unsafe.Pointer -> *Wrapper reinterpretations (reflect's
        // sliceType/structType over their abi shells).
        TESTS.add(graalgo.p3unsafe.PrefixViewTest.class);
        // fix-redmaster-r40: GoStruct.prefixParent first-store-wins
        // (the canonical AbiTypes shell owns the link, transient
        // wrappers must not steal it).
        TESTS.add(graalgo.p3unsafe.PrefixParentTest.class);
        // fix-redmaster-r40: AbiTypes abi.Name byte encoding
        // (flags + LEB128 varints), decoded gc-style.
        TESTS.add(graalgo.p3reflect.AbiNameEncodingTest.class);
    }

    private IfaceTestRunner() {}

    public static void main(String[] args) {
        int run = 0;
        int failed = 0;
        List<String> failures = new ArrayList<>();
        for (Class<?> tc : TESTS) {
            Object inst;
            try {
                inst = tc.getDeclaredConstructor().newInstance();
            } catch (Exception e) {
                throw new RuntimeException("cannot instantiate " + tc.getName(), e);
            }
            for (Method m : tc.getDeclaredMethods()) {
                if (!m.getName().startsWith("test") || m.getParameterCount() != 0
                        || !Modifier.isPublic(m.getModifiers())) {
                    continue;
                }
                run++;
                String name = tc.getSimpleName() + "." + m.getName();
                try {
                    m.invoke(inst);
                    System.out.println("PASS " + name);
                } catch (Exception e) {
                    failed++;
                    Throwable cause = e.getCause() != null ? e.getCause() : e;
                    failures.add(name + ": " + cause);
                    System.out.println("FAIL " + name + ": " + cause);
                }
            }
        }
        System.out.println(run + " tests, " + failed + " failures");
        if (failed > 0) {
            for (String f : failures) {
                System.out.println("  " + f);
            }
            System.exit(1);
        }
    }
}
