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
 * (GG-0020, ADR-0004 D3): the intrinsic path and the dispatch path mint
 * equal itabs for the same (type, interface) pair.
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

    private static void installStringer() {
        IfaceMethodTable.installInterfaces(Map.of("main.Stringer", List.of("String")));
        IfaceMethodTable.install(Map.of("main.T",
                Map.of("String", new IfaceMethodTarget(goFunc(), new int[0], false))));
    }

    /**
     * verifies: GG-0020 — itabForIntrinsic returns an itab equal in
     * content to the dispatch path's itabFor for the same pair
     * (descriptor, iface id, method list).
     */
    public void testIntrinsicItabEqualsDispatchItab() {
        installStringer();
        IfaceItab viaIntrinsic = IfaceMethodTable.itabForIntrinsic("main.T", "main.Stringer");
        IfaceItab viaDispatch =
                IfaceMethodTable.itabFor("main.T", viaIntrinsic.ifaceId(), List.of("String"));
        assertTrue(viaIntrinsic.descriptorName().equals(viaDispatch.descriptorName()),
                "descriptor names match: " + viaIntrinsic + " vs " + viaDispatch);
        assertTrue(viaIntrinsic.ifaceId() == viaDispatch.ifaceId(),
                "iface ids match: " + viaIntrinsic + " vs " + viaDispatch);
        assertTrue(viaIntrinsic.methods().keySet().equals(viaDispatch.methods().keySet()),
                "method sets match: " + viaIntrinsic + " vs " + viaDispatch);
    }

    /**
     * verifies: GG-0020 — the intrinsic path is stable: two calls mint
     * itabs with the same iface id and method set.
     */
    public void testIntrinsicItabStable() {
        installStringer();
        IfaceItab a = IfaceMethodTable.itabForIntrinsic("main.T", "main.Stringer");
        IfaceItab b = IfaceMethodTable.itabForIntrinsic("main.T", "main.Stringer");
        assertTrue(a.ifaceId() == b.ifaceId(), "iface id stable: " + a + " vs " + b);
        assertTrue(a.methods().keySet().equals(b.methods().keySet()),
                "method set stable: " + a + " vs " + b);
    }
}
