package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;

/**
 * Red-green for the fix-interface-chan-convert repair (b88cfc8d): the v1
 * frontend's {@code ConvertNode.checkCombo} rejected INTERFACE -&gt; CHAN
 * as an illegal conversion, breaking builds of packages that use type
 * parameters with channel types. The fix allows the combo and extracts
 * the channel from the interface's data at runtime, mirroring the
 * existing interface -&gt; pointer desugaring.
 *
 * <p>RED: on the unfixed node, constructing
 * {@code new ConvertNode(GoKind.INTERFACE, GoKind.CHAN, child)} throws
 * {@code IllegalArgumentException("illegal conversion: INTERFACE -&gt; CHAN")}.
 * GREEN: it builds, and {@code executeObject} returns the channel carried
 * by the interface's data.
 */
public final class IfaceToChanConvertTest {

    /** Minimal child node returning a fixed object. */
    private static final class ConstChild extends GoExprNode {
        private final Object value;

        ConstChild(GoKind kind, Object value) {
            super(kind);
            this.value = value;
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return value;
        }
    }

    private static void assertSame(Object expected, Object actual, String msg) {
        if (expected != actual) {
            throw new AssertionError(msg + " expected identical instance");
        }
    }

    /** The constructor itself was the pre-fix failure point. */
    public void testIfaceToChanAllowed() {
        new ConvertNode(GoKind.INTERFACE, GoKind.CHAN,
                new ConstChild(GoKind.INTERFACE, GoInterface.NIL));
    }

    public void testIfaceToChanExtractsData() {
        Object channel = new Object();
        GoInterface gi = new GoInterface(new Object(), channel, null);
        ConvertNode node = new ConvertNode(GoKind.INTERFACE, GoKind.CHAN,
                new ConstChild(GoKind.INTERFACE, gi));
        assertSame(channel, node.executeObject(null),
                "interface->chan must extract the channel from the interface data");
    }

    public void testIfaceToChanNonInterfacePassthrough() {
        Object channel = new Object();
        ConvertNode node = new ConvertNode(GoKind.INTERFACE, GoKind.CHAN,
                new ConstChild(GoKind.INTERFACE, channel));
        assertSame(channel, node.executeObject(null),
                "a non-interface child must pass through unchanged");
    }
}
