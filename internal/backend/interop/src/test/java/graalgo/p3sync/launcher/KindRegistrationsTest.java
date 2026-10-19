package graalgo.p3sync.launcher;

import com.oracle.truffle.api.source.Source;

import graalgo.frontend.NodeFactory;

/**
 * Regression test for the fix-redmaster-r16 F1 startup crash
 * (fix-redmaster-r17).
 *
 * <p>{@link P3KindRegistrations} is discovered via {@link java.util.ServiceLoader}
 * and its {@code registerAll} runs on every launcher startup.
 * {@link NodeFactory#registerExprKind} throws on duplicate kind registrations,
 * so a duplicated {@code registerExprKind} call (an editing error in the
 * r16 indexLocal revert duplicated the {@code "unsafeslicedata"} block)
 * crashed every launch during frontend initialization. This test invokes the
 * exact registration path; any duplicate throws and fails the test.
 */
public final class KindRegistrationsTest {

    /**
     * registerAllStatic completes without a duplicate-registration error.
     * Fails on the pre-fix tree with: duplicate expression kind
     * registration: "unsafeslicedata".
     */
    public void testNoDuplicateKindRegistrations() {
        Source src = Source.newBuilder("go", "package main", "kindreg.go").build();
        NodeFactory factory = new NodeFactory(src);
        P3KindRegistrations.registerAllStatic(factory);
    }
}
