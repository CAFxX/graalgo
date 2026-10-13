package graalgo.iface;

import java.util.List;
import java.util.Map;

import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;

import graalgo.values.GoFunc;

/**
 * Tests for the P3-M4 {@code Intrinsics.itabFor} provider hook
 * (GG-0020, ADR-0004 D3; remediated P3-M4 P2-1): the intrinsic path and
 * the dispatch path share ONE iface-id namespace (the wire type-table
 * ids), so they agree on the itab for the same (type, interface) pair.
 * verifies: GG-0020
 */
public final class ItabForIntrinsicTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static GoFunc goFunc() {
        RootNode root = new RootNode(null) {
            @Override
            public Object execute(VirtualFrame frame) {
                return null;
            }
        };
        RootCallTarget target = root.getCallTarget();
        return new GoFunc(target, null, null);
    }

    /**
     * Installs two interfaces with independently chosen wire ids (42 and
     * 43 — deliberately NOT 1, so the test cannot pass by accident on a
     * counter that starts at 1) and a method table for main.T.
     */
    private static void installTwoIfaces() {
        IfaceMethodTable.clear();
        IfaceMethodTable.installInterfaces(Map.of(
                "main.Stringer", new IfaceMethodTable.IfaceWireEntry(42, List.of("String")),
                "main.Writer",
                new IfaceMethodTable.IfaceWireEntry(43, List.of("Write"))));
        IfaceMethodTable.install(Map.of("main.T", Map.of(
                "String", new IfaceMethodTarget(goFunc(), new int[0], false, false, -1, null),
                "Write", new IfaceMethodTarget(goFunc(), new int[0], false, false, -1, null))));
    }

    /**
     * verifies: GG-0020 — P2-1: the intrinsic path returns the identical
     * cached itab instance the dispatch path builds for the same
     * (descriptor, wire iface id) pair (one namespace, one cache key).
     */
    public void testIntrinsicItabIdenticalToDispatchItab() {
        installTwoIfaces();
        IfaceItab viaIntrinsic = IfaceMethodTable.itabForIntrinsic("main.T", "main.Stringer");
        IfaceItab viaDispatch = IfaceMethodTable.itabFor("main.T", 42, List.of("String"));
        assertTrue(viaIntrinsic == viaDispatch,
                "intrinsic and dispatch paths must share the itab instance: "
                        + viaIntrinsic + " vs " + viaDispatch);
        assertTrue(viaIntrinsic.ifaceId() == 42,
                "intrinsic path must use the wire id, got " + viaIntrinsic.ifaceId());
    }

    /**
     * verifies: GG-0020 — P2-1: two interfaces with different wire ids
     * never collide in the shared itab cache (the old counter-minted
     * namespace could alias a wire id of a different interface and
     * return the wrong itab).
     */
    public void testNoCrossInterfaceCollision() {
        installTwoIfaces();
        IfaceItab stringer = IfaceMethodTable.itabForIntrinsic("main.T", "main.Stringer");
        IfaceItab writer = IfaceMethodTable.itabForIntrinsic("main.T", "main.Writer");
        assertTrue(stringer != writer, "different interfaces must not share an itab");
        assertTrue(stringer.ifaceId() == 42 && writer.ifaceId() == 43,
                "wire ids must be preserved: " + stringer + " vs " + writer);
        assertTrue(stringer.methods().keySet().equals(java.util.Set.of("String")),
                "Stringer itab has only String: " + stringer.methods().keySet());
        assertTrue(writer.methods().keySet().equals(java.util.Set.of("Write")),
                "Writer itab has only Write: " + writer.methods().keySet());
        // The dispatch path sees the same instances.
        assertTrue(IfaceMethodTable.itabFor("main.T", 43, List.of("Write")) == writer,
                "dispatch path must agree on the Writer itab");
    }

    /**
     * verifies: GG-0020 — the intrinsic path is stable: two calls return
     * the same cached itab.
     */
    public void testIntrinsicItabStable() {
        installTwoIfaces();
        IfaceItab a = IfaceMethodTable.itabForIntrinsic("main.T", "main.Stringer");
        IfaceItab b = IfaceMethodTable.itabForIntrinsic("main.T", "main.Stringer");
        assertTrue(a == b, "itab stable across calls: " + a + " vs " + b);
    }
}
