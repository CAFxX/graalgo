package graalgo.runtime;

import java.lang.reflect.Method;
// verifies: GG-0020 (WS G: freeze v1 signature verification)
import java.lang.reflect.Modifier;
import java.util.ArrayList;
import java.util.List;

/**
 * Verifies the GG-0020 freeze v1 signatures via reflection
 * (verifies: GG-0020).
 *
 * <p>Each frozen signature from {@code docs/freeze-stub.md} is checked for
 * existence with the exact parameter types. This test fails if a frozen
 * signature is renamed, retyped, or removed without a cross-workstream
 * review.
 */
public final class FreezeSignatureTest {
    private record Sig(String cls, String name, Class<?>[] params) {}

    private static final List<Sig> FROZEN = new ArrayList<>();

    static {
        Class<?> intr = Intrinsics.class;
        // Memory.
        FROZEN.add(new Sig(intr.getName(), "memmove",
            new Class<?>[]{Object.class, int.class, Object.class, int.class, int.class}));
        FROZEN.add(new Sig(intr.getName(), "memequal",
            new Class<?>[]{Object.class, int.class, Object.class, int.class, int.class}));
        // Slices.
        FROZEN.add(new Sig(intr.getName(), "makeslice",
            new Class<?>[]{graalgo.values.GoKind.class, int.class, int.class}));
        FROZEN.add(new Sig(intr.getName(), "growslice",
            new Class<?>[]{graalgo.values.GoSlice.class, graalgo.values.GoKind.class,
                int.class, long.class, boolean.class}));
        FROZEN.add(new Sig(intr.getName(), "growslice",
            new Class<?>[]{graalgo.values.GoSlice.class, graalgo.values.GoKind.class, int.class}));
        FROZEN.add(new Sig(intr.getName(), "slicecopy",
            new Class<?>[]{graalgo.values.GoSlice.class, graalgo.values.GoSlice.class,
                graalgo.values.GoKind.class}));
        // Maps.
        FROZEN.add(new Sig(intr.getName(), "mapAssign",
            new Class<?>[]{graalgo.values.GoMap.class, Object.class, Object.class}));
        FROZEN.add(new Sig(intr.getName(), "mapAccess",
            new Class<?>[]{graalgo.values.GoMap.class, Object.class}));
        FROZEN.add(new Sig(intr.getName(), "mapDelete",
            new Class<?>[]{graalgo.values.GoMap.class, Object.class}));
        FROZEN.add(new Sig(intr.getName(), "mapLen",
            new Class<?>[]{graalgo.values.GoMap.class}));
        // itab / traceback (stubs, but the signatures are frozen).
        FROZEN.add(new Sig(intr.getName(), "itabFor",
            new Class<?>[]{graalgo.values.GoTypeDescriptor.class,
                graalgo.values.GoTypeDescriptor.class}));
        FROZEN.add(new Sig(intr.getName(), "traceback", new Class<?>[]{}));
        // Random.
        FROZEN.add(new Sig(intr.getName(), "fastrand64", new Class<?>[]{}));
        FROZEN.add(new Sig(intr.getName(), "fastrand", new Class<?>[]{}));
        FROZEN.add(new Sig(intr.getName(), "fastrandn", new Class<?>[]{int.class}));

        // Builtin print.
        FROZEN.add(new Sig(BuiltinPrint.class.getName(), "print",
            new Class<?>[]{BuiltinPrint.Arg[].class}));
        FROZEN.add(new Sig(BuiltinPrint.class.getName(), "println",
            new Class<?>[]{BuiltinPrint.Arg[].class}));

        // fmt stub (D4).
        FROZEN.add(new Sig(Fmt.class.getName(), "sprintf",
            new Class<?>[]{String.class, Fmt.Arg[].class}));
        FROZEN.add(new Sig(Fmt.class.getName(), "sprint",
            new Class<?>[]{Fmt.Arg[].class}));
        FROZEN.add(new Sig(Fmt.class.getName(), "sprintln",
            new Class<?>[]{Fmt.Arg[].class}));
        FROZEN.add(new Sig(Fmt.class.getName(), "print",
            new Class<?>[]{Fmt.Arg[].class}));
        FROZEN.add(new Sig(Fmt.class.getName(), "printf",
            new Class<?>[]{String.class, Fmt.Arg[].class}));
        FROZEN.add(new Sig(Fmt.class.getName(), "println",
            new Class<?>[]{Fmt.Arg[].class}));
        FROZEN.add(new Sig(Fmt.class.getName(), "errorf",
            new Class<?>[]{String.class, Fmt.Arg[].class}));

        // Timers.
        FROZEN.add(new Sig(Timers.class.getName(), "nanotime", new Class<?>[]{}));
        FROZEN.add(new Sig(Timers.class.getName(), "sleep", new Class<?>[]{long.class}));

        // Scheduler.
        FROZEN.add(new Sig(Scheduler.class.getName(), "spawn",
            new Class<?>[]{graalgo.values.GoFunc.class}));
        FROZEN.add(new Sig(Scheduler.class.getName(), "yield", new Class<?>[]{}));
        FROZEN.add(new Sig(Scheduler.class.getName(), "park",
            new Class<?>[]{Runnable.class}));

        // Netpoll.
        FROZEN.add(new Sig(Netpoll.class.getName(), "init", new Class<?>[]{}));
        FROZEN.add(new Sig(Netpoll.class.getName(), "open",
            new Class<?>[]{int.class, int.class}));
        FROZEN.add(new Sig(Netpoll.class.getName(), "close", new Class<?>[]{int.class}));

        // ChanOps.
        FROZEN.add(new Sig(ChanOps.class.getName(), "send",
            new Class<?>[]{ChanOps.Chan.class, Object.class}));
        FROZEN.add(new Sig(ChanOps.class.getName(), "recv",
            new Class<?>[]{ChanOps.Chan.class}));
        FROZEN.add(new Sig(ChanOps.class.getName(), "close",
            new Class<?>[]{ChanOps.Chan.class}));

        // Panic (frozen cross-workstream interface).
        FROZEN.add(new Sig("graalgo.runtime.Panic", "throwPanic",
            new Class<?>[]{Object.class}));
        FROZEN.add(new Sig("graalgo.runtime.Panic", "recover", new Class<?>[]{}));
    }

    public void testFrozenSignatures() {
        List<String> missing = new ArrayList<>();
        for (Sig s : FROZEN) {
            try {
                Class<?> cls = Class.forName(s.cls());
                Method m = cls.getMethod(s.name(), s.params());
                if (!Modifier.isStatic(m.getModifiers())
                        || !Modifier.isPublic(m.getModifiers())) {
                    missing.add(s.cls() + "#" + s.name() + " (not public static)");
                }
            } catch (ClassNotFoundException | NoSuchMethodException e) {
                missing.add(s.cls() + "#" + s.name() + " (" + e.getMessage() + ")");
            }
        }
        if (!missing.isEmpty()) {
            throw new AssertionError("frozen signatures missing/changed:\n"
                + String.join("\n", missing));
        }
        System.out.println("  (freeze v1: " + FROZEN.size() + " signatures verified)");
    }
}
