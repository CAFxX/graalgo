package graalgo.runtime;

import java.lang.reflect.Method;
import java.lang.reflect.Modifier;

/**
 * P2-M3: covers the four frozen signatures that Phase-1's
 * FreezeSignatureTest does not machine-check (reviewer1 P2-4):
 * Timers.startTimer/stopTimer and Netpoll.waitForEvents/breakWait.
 * Phase-1 files are immutable to Phase 2, so the gap is closed here
 * by reflection against the freeze-stub contract
 * (pre-migration phase-1 freeze-stub contract, §§ Timers, Netpoller API contract).
 */
public final class FreezeGapTest {

    private static void assertSig(Class<?> cls, String name,
                                  Class<?> ret, Class<?>... params) throws Exception {
        Method m = cls.getMethod(name, params);
        int mods = m.getModifiers();
        if (!Modifier.isPublic(mods) || !Modifier.isStatic(mods)) {
            throw new AssertionError(cls.getSimpleName() + "." + name
                    + " must be public static");
        }
        if (!m.getReturnType().equals(ret)) {
            throw new AssertionError(cls.getSimpleName() + "." + name
                    + " return: expected " + ret + " got " + m.getReturnType());
        }
    }

    public void testFrozenTimerSignatures() throws Exception {
        assertSig(Timers.class, "nanotime", long.class);
        assertSig(Timers.class, "sleep", void.class, long.class);
        assertSig(Timers.class, "startTimer", void.class, long.class, Runnable.class);
        assertSig(Timers.class, "stopTimer", void.class, long.class);
    }

    public void testFrozenNetpollSignatures() throws Exception {
        assertSig(Netpoll.class, "init", void.class);
        assertSig(Netpoll.class, "open", long.class, int.class, int.class);
        assertSig(Netpoll.class, "close", void.class, int.class);
        assertSig(Netpoll.class, "waitForEvents", boolean.class, boolean.class);
        assertSig(Netpoll.class, "breakWait", void.class);
    }
}
