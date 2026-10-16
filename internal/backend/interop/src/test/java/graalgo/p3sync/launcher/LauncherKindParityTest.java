package graalgo.p3sync.launcher;

import java.lang.reflect.Field;
import java.util.LinkedHashSet;
import java.util.Map;
import java.util.Set;

import com.oracle.truffle.api.source.Source;

import graalgo.frontend.NodeFactory;

/**
 * Parity pin for the 2026-10-16 P1 launcher drift: the product launcher
 * ({@code graalgo-sync-run}, via {@link P3SyncMain#registerKinds}) and the
 * basic {@code graalgo-run} launcher (via the {@code WireKindRegistrar}
 * ServiceLoader entry, {@link P3KindRegistrations#registerAllStatic}) must
 * register IDENTICAL wire-kind sets. The frontend emits wire kinds (e.g.
 * {@code "abifuncpc"}) with no knowledge of launchers, so any drift means
 * one launcher dies with "unsupported kind" on a program the other runs.
 *
 * <p>The sets are read off each registration path via reflection on
 * {@link NodeFactory}'s private builder maps — the truffle module is a
 * frozen dependency, so no test-only accessor is added to it. (The
 * duplicate-kind FrontendException contract could only prove set
 * OVERLAP, not equality: a duplicate fires on the first shared kind
 * before any extra kind is reached, so reflection is the honest check.)
 */
public final class LauncherKindParityTest {

    private static NodeFactory newFactory() {
        return new NodeFactory(
                Source.newBuilder("go", "package main\n", "parity.go").build());
    }

    @SuppressWarnings("unchecked")
    private static Set<String> builderKinds(NodeFactory factory, String field) {
        try {
            Field f = NodeFactory.class.getDeclaredField(field);
            f.setAccessible(true);
            Map<String, ?> m = (Map<String, ?>) f.get(factory);
            return new LinkedHashSet<>(m.keySet());
        } catch (ReflectiveOperationException e) {
            throw new RuntimeException("cannot read NodeFactory." + field, e);
        }
    }

    private static Set<String> exprKindsOf(NodeFactory factory) {
        Set<String> all = builderKinds(factory, "exprBuilders");
        all.removeAll(baseKinds("exprBuilders"));
        return all;
    }

    private static Set<String> stmtKindsOf(NodeFactory factory) {
        Set<String> all = builderKinds(factory, "stmtBuilders");
        all.removeAll(baseKinds("stmtBuilders"));
        return all;
    }

    /** Kinds a bare NodeFactory registers (ident, basiclit, ...): excluded. */
    private static Set<String> baseKinds(String field) {
        return builderKinds(newFactory(), field);
    }

    private static void assertEq(Set<String> expected, Set<String> actual, String what) {
        Set<String> missing = new LinkedHashSet<>(expected);
        missing.removeAll(actual);
        Set<String> extra = new LinkedHashSet<>(actual);
        extra.removeAll(expected);
        if (!missing.isEmpty() || !extra.isEmpty()) {
            throw new AssertionError(what + " differ: missing=" + missing + " extra=" + extra);
        }
    }

    private static void assertTrue(boolean c, String msg) {
        if (!c) {
            throw new AssertionError(msg);
        }
    }

    /**
     * The two launchers must register identical kind sets. This fails if a
     * kind is ever again registered on only one path (the 2026-10-16 drift
     * mode: {@code P3SyncMain} had {@code "abifuncpc"}, the ServiceLoader
     * path did not).
     */
    public void testLaunchersRegisterIdenticalKinds() {
        NodeFactory viaSync = newFactory();
        NodeFactory viaShared = newFactory();
        P3SyncMain.registerKinds(viaSync);
        P3KindRegistrations.registerAllStatic(viaShared);
        assertEq(exprKindsOf(viaShared), exprKindsOf(viaSync), "expression kinds");
        assertEq(stmtKindsOf(viaShared), stmtKindsOf(viaSync), "statement kinds");
        assertTrue(!exprKindsOf(viaShared).isEmpty(), "expected a non-empty expr-kind set");
    }

    /**
     * Pins the exact 2026-10-16 regression through the path
     * {@code graalgo-run} actually uses (the {@code WireKindRegistrar}
     * instance method, discovered via {@code ServiceLoader}): the
     * {@code "abifuncpc"} wire node emitted by the frontend for the
     * internal/abi FuncPCABI0/FuncPCABIInternal intrinsics must be handled.
     */
    public void testAbifuncpcRegisteredOnServiceLoaderPath() {
        NodeFactory f = newFactory();
        new P3KindRegistrations().registerAll(f);
        assertTrue(exprKindsOf(f).contains("abifuncpc"),
                "\"abifuncpc\" must be registered on the ServiceLoader path (graalgo-run)");
    }
}
