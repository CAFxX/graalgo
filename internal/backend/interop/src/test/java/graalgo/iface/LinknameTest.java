package graalgo.iface;

import java.util.List;
import java.util.Map;

import graalgo.frontend.FrontendException;
import graalgo.runtime.Linknames;

/**
 * Tests for the P3-M4 linkname intrinsic surface (GG-0020):
 * intrinsic → Go helper by symbol name fails fast on unbound names.
 * verifies: GG-0020
 */
public final class LinknameTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    /**
     * verifies: GG-0020 — callGo on an unbound symbol fails fast with a
     * named error (not a null dereference or silent no-op).
     */
    public void testCallGoUnknownSymbolFailsFast() {
        try {
            Linknames.callGo("main.noSuchFunction", new Object[0]);
            throw new AssertionError("expected FrontendException for unbound linkname symbol");
        } catch (FrontendException e) {
            assertTrue(e.getMessage().contains("main.noSuchFunction"),
                    "error names the symbol: " + e.getMessage());
            assertTrue(e.getMessage().contains("no Go function bound"),
                    "error says unbound: " + e.getMessage());
        }
    }

    /**
     * verifies: GG-0020 — resolveGo returns null (not throw) for unknown
     * names, so callers can probe before calling.
     */
    public void testResolveGoUnknownReturnsNull() {
        assertTrue(Linknames.resolveGo("main.noSuchFunction") == null,
                "resolveGo must return null for unknown names");
    }
}
