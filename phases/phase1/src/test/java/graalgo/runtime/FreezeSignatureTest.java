package graalgo.runtime;

import java.lang.reflect.Method;
// verifies: GG-0020 (WS G: freeze v1 signature verification)
import java.lang.reflect.Modifier;
import java.lang.reflect.Constructor;
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
 *
 * <p>M9: extended with the full interface-freeze surface
 * ({@code INTERFACE_FREEZE.md}): NodeFactory, node protocol, call entry
 * points, values types, and the M9 probe APIs.
 */
public final class FreezeSignatureTest {
    private record Sig(String cls, String name, Class<?>[] params, boolean wantStatic) {
        Sig(String cls, String name, Class<?>[] params) {
            this(cls, name, params, true);
        }
    }

    private static final List<Sig> FROZEN = new ArrayList<>();

    /** Resolves a class for the frozen list; unchecked so the static block compiles. */
    private static Class<?> cl(String name) {
        try {
            return Class.forName(name);
        } catch (ClassNotFoundException e) {
            throw new IllegalStateException("frozen referenced class missing: " + name, e);
        }
    }

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

        // M9 interface freeze (INTERFACE_FREEZE.md §3.2): NodeFactory entry points.
        FROZEN.add(new Sig("graalgo.frontend.NodeFactory", "buildProgram",
            new Class<?>[]{String.class}, false));
        FROZEN.add(new Sig("graalgo.frontend.NodeFactory", "registerExprKind",
            new Class<?>[]{String.class,
                cl("graalgo.frontend.NodeFactory$ExprBuilder")}, false));
        FROZEN.add(new Sig("graalgo.frontend.NodeFactory", "registerStmtKind",
            new Class<?>[]{String.class,
                cl("graalgo.frontend.NodeFactory$StmtBuilder")}, false));
        FROZEN.add(new Sig("graalgo.frontend.NodeFactory", "registerTypeDecoder",
            new Class<?>[]{cl("graalgo.frontend.NodeFactory$TypeDecoder")},
            false));

        // M9 (INTERFACE_FREEZE.md §3.3): node execution protocol.
        for (String m : new String[]{"executeLong", "executeInt", "executeFloat",
                "executeDouble", "executeBoolean", "executeObject"}) {
            FROZEN.add(new Sig("graalgo.frontend.GoExprNode", m,
                new Class<?>[]{com.oracle.truffle.api.frame.VirtualFrame.class}, false));
        }

        // M9 (INTERFACE_FREEZE.md §3.4): call entry points.
        FROZEN.add(new Sig("graalgo.runtime.GoCalls", "invoke",
            new Class<?>[]{graalgo.values.GoFunc.class, Object[].class}));
        FROZEN.add(new Sig("graalgo.runtime.GoCalls", "nilFuncCall",
            new Class<?>[]{}));
        FROZEN.add(new Sig("graalgo.runtime.Runtime", "println",
            new Class<?>[]{String.class}));

        // M9 (INTERFACE_FREEZE.md §3.5/§6): probe APIs.
        FROZEN.add(new Sig("graalgo.values.CallSiteProbes", "setEnabled",
            new Class<?>[]{boolean.class}));
        FROZEN.add(new Sig("graalgo.values.CallSiteProbes", "registerSite",
            new Class<?>[]{String.class}));
        FROZEN.add(new Sig("graalgo.values.CallSiteProbes", "snapshotJson",
            new Class<?>[]{}));
        FROZEN.add(new Sig("graalgo.values.CallSiteProbes", "receiverKey",
            new Class<?>[]{Object.class}));
        FROZEN.add(new Sig("graalgo.values.InterfaceDispatchProbes", "registerSite",
            new Class<?>[]{String.class}));
        FROZEN.add(new Sig("graalgo.values.InterfaceDispatchProbes", "sites",
            new Class<?>[]{}));
    }

    private record Ctor(String cls, Class<?>[] params) {}

    private static final List<Ctor> FROZEN_CTORS = new ArrayList<>();

    static {
        // M9 (INTERFACE_FREEZE.md §3.5): values constructors that cross the boundary.
        FROZEN_CTORS.add(new Ctor("graalgo.values.GoStruct",
            new Class<?>[]{graalgo.values.GoStructDescriptor.class, long[].class,
                Object[].class}));
        FROZEN_CTORS.add(new Ctor("graalgo.values.GoPointer",
            new Class<?>[]{Object.class}));
        FROZEN_CTORS.add(new Ctor("graalgo.values.GoInterface",
            new Class<?>[]{Object.class, Object.class, Object.class}));
        FROZEN_CTORS.add(new Ctor("graalgo.values.GoFunc",
            new Class<?>[]{com.oracle.truffle.api.RootCallTarget.class,
                com.oracle.truffle.api.frame.MaterializedFrame.class}));
    }

    public void testFrozenSignatures() {
        List<String> missing = new ArrayList<>();
        for (Sig s : FROZEN) {
            try {
                Class<?> cls = Class.forName(s.cls());
                Method m = cls.getMethod(s.name(), s.params());
                boolean isStatic = Modifier.isStatic(m.getModifiers());
                if (!Modifier.isPublic(m.getModifiers())
                        || (s.wantStatic() && !isStatic)
                        || (!s.wantStatic() && isStatic)) {
                    missing.add(s.cls() + "#" + s.name()
                        + " (expected " + (s.wantStatic() ? "public static" : "public instance")
                        + ")");
                }
            } catch (ClassNotFoundException | NoSuchMethodException e) {
                missing.add(s.cls() + "#" + s.name() + " (" + e.getMessage() + ")");
            }
        }
        for (Ctor c : FROZEN_CTORS) {
            try {
                Class<?> cls = Class.forName(c.cls());
                Constructor<?> k = cls.getConstructor(c.params());
                if (!Modifier.isPublic(k.getModifiers())) {
                    missing.add(c.cls() + "#<init> (not public)");
                }
            } catch (ClassNotFoundException | NoSuchMethodException e) {
                missing.add(c.cls() + "#<init> (" + e.getMessage() + ")");
            }
        }
        if (!missing.isEmpty()) {
            throw new AssertionError("frozen signatures missing/changed:\n"
                + String.join("\n", missing));
        }
        System.out.println("  (freeze v1: " + FROZEN.size() + " signatures + "
            + FROZEN_CTORS.size() + " ctors verified)");
    }
}
