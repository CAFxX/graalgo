package graalgo.frontend;

import graalgo.values.GoInterface;

/**
 * Pins the D5 {@code wrapInterface} Java-carrier → Go-descriptor mapping
 * (P3-M7R2): the long carrier maps to the canonical "int64" descriptor,
 * identical to the instance {@code basicDescriptorForName("int64")}
 * returns. This is a documented approximation — a Go {@code int} crossing
 * as a Long is boxed as "int64" (matching pre-P3-M7 observable behavior);
 * callers that know the exact Go type must box via the type table.
 * verifies: GG-0014
 */
public final class WrapInterfaceTest {

    private static void assertEquals(Object expected, Object actual, String msg) {
        if (!expected.equals(actual)) {
            throw new AssertionError(msg + " expected=" + expected + " actual=" + actual);
        }
    }

    private static void assertSame(Object expected, Object actual, String msg) {
        if (expected != actual) {
            throw new AssertionError(msg + " expected identical instance");
        }
    }

    public void testLongMapsToInt64Descriptor() {
        GoInterface gi = GoFuncRootNode.wrapInterface(Long.valueOf(42));
        graalgo.values.GoTypeDescriptor desc =
                (graalgo.values.GoTypeDescriptor) gi.descriptor();
        assertEquals("int64", desc.name(), "Long carrier descriptor name");
        assertSame(NodeFactory.basicDescriptorForName("int64", graalgo.values.GoKind.INT64),
                desc, "Long carrier descriptor identity");
    }

    public void testIntegerMapsToInt32Descriptor() {
        GoInterface gi = GoFuncRootNode.wrapInterface(Integer.valueOf(42));
        graalgo.values.GoTypeDescriptor desc =
                (graalgo.values.GoTypeDescriptor) gi.descriptor();
        assertEquals("int32", desc.name(), "Integer carrier descriptor name");
    }

    public void testDoubleMapsToFloat64Descriptor() {
        GoInterface gi = GoFuncRootNode.wrapInterface(Double.valueOf(1.5));
        graalgo.values.GoTypeDescriptor desc =
                (graalgo.values.GoTypeDescriptor) gi.descriptor();
        assertEquals("float64", desc.name(), "Double carrier descriptor name");
    }
}
