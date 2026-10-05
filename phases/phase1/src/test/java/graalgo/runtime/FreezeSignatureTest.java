package graalgo.runtime;

import java.lang.reflect.Method;
// verifies: GG-0020 (WS G: freeze v1 signature verification)
import java.lang.reflect.Modifier;
import java.lang.reflect.Constructor;
import java.lang.reflect.RecordComponent;
import java.util.ArrayList;
import java.util.List;

/**
 * Verifies the GG-0020 freeze v1 signatures via reflection
 * (verifies: GG-0020).
 *
 * <p>Each frozen signature from {@code docs/freeze-stub.md} and
 * {@code INTERFACE_FREEZE.md} is checked for existence with the exact
 * parameter types <em>and</em> return type, plus the public/static shape.
 * This test fails if a frozen signature is renamed, retyped (params or
 * return), or removed without a cross-workstream review.
 *
 * <p>M9: extended with the full interface-freeze surface
 * ({@code INTERFACE_FREEZE.md}): NodeFactory, node protocol, call entry
 * points, values types, the M5 runtime surface, and the M9 probe APIs.
 */
public final class FreezeSignatureTest {
    private record Sig(String cls, String name, Class<?>[] params, Class<?> ret,
                       boolean wantStatic) {
        Sig(String cls, String name, Class<?>[] params, Class<?> ret) {
            this(cls, name, params, ret, true);
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
            new Class<?>[]{Object.class, int.class, Object.class, int.class, int.class},
            void.class));
        FROZEN.add(new Sig(intr.getName(), "memequal",
            new Class<?>[]{Object.class, int.class, Object.class, int.class, int.class},
            boolean.class));
        // Slices.
        FROZEN.add(new Sig(intr.getName(), "makeslice",
            new Class<?>[]{graalgo.values.GoKind.class, int.class, int.class},
            graalgo.values.GoSlice.class));
        FROZEN.add(new Sig(intr.getName(), "growslice",
            new Class<?>[]{graalgo.values.GoSlice.class, graalgo.values.GoKind.class,
                int.class, long.class, boolean.class},
            graalgo.values.GoSlice.class));
        FROZEN.add(new Sig(intr.getName(), "growslice",
            new Class<?>[]{graalgo.values.GoSlice.class, graalgo.values.GoKind.class, int.class},
            graalgo.values.GoSlice.class));
        FROZEN.add(new Sig(intr.getName(), "slicecopy",
            new Class<?>[]{graalgo.values.GoSlice.class, graalgo.values.GoSlice.class,
                graalgo.values.GoKind.class},
            int.class));
        // Maps.
        FROZEN.add(new Sig(intr.getName(), "mapAssign",
            new Class<?>[]{graalgo.values.GoMap.class, Object.class, Object.class},
            void.class));
        FROZEN.add(new Sig(intr.getName(), "mapAccess",
            new Class<?>[]{graalgo.values.GoMap.class, Object.class},
            Object.class));
        FROZEN.add(new Sig(intr.getName(), "mapDelete",
            new Class<?>[]{graalgo.values.GoMap.class, Object.class},
            void.class));
        FROZEN.add(new Sig(intr.getName(), "mapLen",
            new Class<?>[]{graalgo.values.GoMap.class},
            int.class));
        // itab / traceback (stubs, but the signatures are frozen).
        FROZEN.add(new Sig(intr.getName(), "itabFor",
            new Class<?>[]{graalgo.values.GoTypeDescriptor.class,
                graalgo.values.GoTypeDescriptor.class},
            Object.class));
        FROZEN.add(new Sig(intr.getName(), "traceback", new Class<?>[]{},
            String.class));
        // Random.
        FROZEN.add(new Sig(intr.getName(), "fastrand64", new Class<?>[]{},
            long.class));
        FROZEN.add(new Sig(intr.getName(), "fastrand", new Class<?>[]{},
            int.class));
        FROZEN.add(new Sig(intr.getName(), "fastrandn", new Class<?>[]{int.class},
            int.class));

        // Builtin print.
        FROZEN.add(new Sig(BuiltinPrint.class.getName(), "print",
            new Class<?>[]{BuiltinPrint.Arg[].class}, void.class));
        FROZEN.add(new Sig(BuiltinPrint.class.getName(), "println",
            new Class<?>[]{BuiltinPrint.Arg[].class}, void.class));

        // fmt stub (D4).
        Class<?> goStr = graalgo.values.GoString.class;
        Class<?> goIface = graalgo.values.GoInterface.class;
        FROZEN.add(new Sig(Fmt.class.getName(), "sprintf",
            new Class<?>[]{String.class, Fmt.Arg[].class}, goStr));
        FROZEN.add(new Sig(Fmt.class.getName(), "sprint",
            new Class<?>[]{Fmt.Arg[].class}, goStr));
        FROZEN.add(new Sig(Fmt.class.getName(), "sprintln",
            new Class<?>[]{Fmt.Arg[].class}, goStr));
        FROZEN.add(new Sig(Fmt.class.getName(), "print",
            new Class<?>[]{Fmt.Arg[].class}, int.class));
        FROZEN.add(new Sig(Fmt.class.getName(), "printf",
            new Class<?>[]{String.class, Fmt.Arg[].class}, int.class));
        FROZEN.add(new Sig(Fmt.class.getName(), "println",
            new Class<?>[]{Fmt.Arg[].class}, int.class));
        FROZEN.add(new Sig(Fmt.class.getName(), "errorf",
            new Class<?>[]{String.class, Fmt.Arg[].class}, goIface));

        // Timers.
        FROZEN.add(new Sig(Timers.class.getName(), "nanotime", new Class<?>[]{},
            long.class));
        FROZEN.add(new Sig(Timers.class.getName(), "sleep", new Class<?>[]{long.class},
            void.class));

        // Scheduler.
        FROZEN.add(new Sig(Scheduler.class.getName(), "spawn",
            new Class<?>[]{graalgo.values.GoFunc.class}, void.class));
        FROZEN.add(new Sig(Scheduler.class.getName(), "yield", new Class<?>[]{},
            void.class));
        FROZEN.add(new Sig(Scheduler.class.getName(), "park",
            new Class<?>[]{Runnable.class}, void.class));

        // Netpoll.
        FROZEN.add(new Sig(Netpoll.class.getName(), "init", new Class<?>[]{},
            void.class));
        FROZEN.add(new Sig(Netpoll.class.getName(), "open",
            new Class<?>[]{int.class, int.class}, long.class));
        FROZEN.add(new Sig(Netpoll.class.getName(), "close", new Class<?>[]{int.class},
            void.class));

        // ChanOps.
        FROZEN.add(new Sig(ChanOps.class.getName(), "send",
            new Class<?>[]{ChanOps.Chan.class, Object.class}, void.class));
        FROZEN.add(new Sig(ChanOps.class.getName(), "recv",
            new Class<?>[]{ChanOps.Chan.class}, Object.class));
        FROZEN.add(new Sig(ChanOps.class.getName(), "close",
            new Class<?>[]{ChanOps.Chan.class}, void.class));

        // Panic (frozen cross-workstream interface).
        FROZEN.add(new Sig("graalgo.runtime.Panic", "throwPanic",
            new Class<?>[]{Object.class}, void.class));
        FROZEN.add(new Sig("graalgo.runtime.Panic", "recover", new Class<?>[]{},
            Object.class));

        // M5 runtime surface (INTERFACE_FREEZE.md §3.4, from freeze-stub.md).
        Class<?> goErr = cl("graalgo.runtime.GoErrorValue");
        Class<?> boundsCode = cl("graalgo.runtime.GoErrors$BoundsCode");
        FROZEN.add(new Sig("graalgo.runtime.GoErrors", "bounds",
            new Class<?>[]{boundsCode, long.class, long.class, boolean.class}, goErr));
        FROZEN.add(new Sig("graalgo.runtime.GoErrors", "indexOutOfRange",
            new Class<?>[]{long.class, long.class, boolean.class}, goErr));
        FROZEN.add(new Sig("graalgo.runtime.GoErrors", "assignmentToNilMap",
            new Class<?>[]{}, goErr));
        FROZEN.add(new Sig("graalgo.runtime.GoErrors", "nilDereference",
            new Class<?>[]{}, goErr));
        FROZEN.add(new Sig("graalgo.runtime.GoErrors", "integerDivideByZero",
            new Class<?>[]{}, goErr));
        FROZEN.add(new Sig("graalgo.runtime.GoErrors", "negativeShift",
            new Class<?>[]{}, goErr));
        FROZEN.add(new Sig("graalgo.runtime.GoErrors", "interfaceConversion",
            new Class<?>[]{String.class, String.class, String.class}, goErr));
        Class<?> deferredCall = cl("graalgo.runtime.DeferredCall");
        Class<?> panicExc = cl("graalgo.runtime.PanicException");
        FROZEN.add(new Sig("graalgo.runtime.DeferChain", "push",
            new Class<?>[]{deferredCall}, void.class, false));
        FROZEN.add(new Sig("graalgo.runtime.DeferChain", "runAll",
            new Class<?>[]{}, void.class, false));
        FROZEN.add(new Sig("graalgo.runtime.DeferChain", "unwindReturning",
            new Class<?>[]{int.class}, void.class, false));
        FROZEN.add(new Sig("graalgo.runtime.DeferChain", "unwindPanicking",
            new Class<?>[]{int.class, panicExc}, panicExc, false));
        FROZEN.add(new Sig("graalgo.runtime.DeferChain", "unwindGoexiting",
            new Class<?>[]{int.class}, void.class, false));
        FROZEN.add(new Sig("graalgo.runtime.Goexit", "goexit",
            new Class<?>[]{}, void.class));

        // M9 interface freeze (INTERFACE_FREEZE.md §3.2): NodeFactory entry points.
        FROZEN.add(new Sig("graalgo.frontend.NodeFactory", "buildProgram",
            new Class<?>[]{String.class},
            cl("graalgo.frontend.NodeFactory$Program"), false));
        FROZEN.add(new Sig("graalgo.frontend.NodeFactory", "registerExprKind",
            new Class<?>[]{String.class,
                cl("graalgo.frontend.NodeFactory$ExprBuilder")},
            void.class, false));
        FROZEN.add(new Sig("graalgo.frontend.NodeFactory", "registerStmtKind",
            new Class<?>[]{String.class,
                cl("graalgo.frontend.NodeFactory$StmtBuilder")},
            void.class, false));
        FROZEN.add(new Sig("graalgo.frontend.NodeFactory", "registerTypeDecoder",
            new Class<?>[]{cl("graalgo.frontend.NodeFactory$TypeDecoder")},
            void.class, false));
        FROZEN.add(new Sig("graalgo.frontend.NodeFactory", "basicDescriptorFor",
            new Class<?>[]{graalgo.values.GoKind.class},
            graalgo.values.GoTypeDescriptor.class));
        FROZEN.add(new Sig("graalgo.frontend.NodeFactory", "isPredeclaredBasic",
            new Class<?>[]{graalgo.values.GoTypeDescriptor.class},
            boolean.class));

        // M9 (INTERFACE_FREEZE.md §3.3): node execution protocol.
        Class<?> vf = com.oracle.truffle.api.frame.VirtualFrame.class;
        Class<?> goKind = graalgo.values.GoKind.class;
        Class<?> srcSec = com.oracle.truffle.api.source.SourceSection.class;
        FROZEN.add(new Sig("graalgo.frontend.GoExprNode", "executeLong",
            new Class<?>[]{vf}, long.class, false));
        FROZEN.add(new Sig("graalgo.frontend.GoExprNode", "executeInt",
            new Class<?>[]{vf}, int.class, false));
        FROZEN.add(new Sig("graalgo.frontend.GoExprNode", "executeFloat",
            new Class<?>[]{vf}, float.class, false));
        FROZEN.add(new Sig("graalgo.frontend.GoExprNode", "executeDouble",
            new Class<?>[]{vf}, double.class, false));
        FROZEN.add(new Sig("graalgo.frontend.GoExprNode", "executeBoolean",
            new Class<?>[]{vf}, boolean.class, false));
        FROZEN.add(new Sig("graalgo.frontend.GoExprNode", "executeObject",
            new Class<?>[]{vf}, Object.class, false));
        FROZEN.add(new Sig("graalgo.frontend.GoExprNode", "kind",
            new Class<?>[]{}, goKind, false));
        FROZEN.add(new Sig("graalgo.frontend.GoExprNode", "typeId",
            new Class<?>[]{}, int.class, false));
        FROZEN.add(new Sig("graalgo.frontend.GoExprNode", "setSourceSection",
            new Class<?>[]{srcSec}, void.class, false));
        FROZEN.add(new Sig("graalgo.frontend.GoExprNode", "getSourceSection",
            new Class<?>[]{}, srcSec, false));
        FROZEN.add(new Sig("graalgo.frontend.GoStmtNode", "executeVoid",
            new Class<?>[]{vf}, void.class, false));
        FROZEN.add(new Sig("graalgo.frontend.GoStmtNode", "setSourceSection",
            new Class<?>[]{srcSec}, void.class, false));
        FROZEN.add(new Sig("graalgo.frontend.GoStmtNode", "getSourceSection",
            new Class<?>[]{}, srcSec, false));

        // M9 (INTERFACE_FREEZE.md §3.4): call entry points.
        FROZEN.add(new Sig("graalgo.runtime.GoCalls", "invoke",
            new Class<?>[]{graalgo.values.GoFunc.class, Object[].class},
            Object.class));
        FROZEN.add(new Sig("graalgo.runtime.GoCalls", "nilFuncCall",
            new Class<?>[]{}, goErr));
        FROZEN.add(new Sig("graalgo.runtime.Runtime", "println",
            new Class<?>[]{String.class}, void.class));
        FROZEN.add(new Sig("graalgo.safepoint.Safepoint", "poll",
            new Class<?>[]{}, void.class));

        // M9 (INTERFACE_FREEZE.md §3.5): values accessors on the boundary.
        Class<?> rct = com.oracle.truffle.api.RootCallTarget.class;
        Class<?> mframe = com.oracle.truffle.api.frame.MaterializedFrame.class;
        Class<?> goBoxArr = cl("[Lgraalgo.values.GoBox;");
        FROZEN.add(new Sig("graalgo.values.GoInterface", "descriptor",
            new Class<?>[]{}, Object.class, false));
        FROZEN.add(new Sig("graalgo.values.GoInterface", "data",
            new Class<?>[]{}, Object.class, false));
        FROZEN.add(new Sig("graalgo.values.GoInterface", "itab",
            new Class<?>[]{}, Object.class, false));
        FROZEN.add(new Sig("graalgo.values.GoFunc", "target",
            new Class<?>[]{}, rct, false));
        FROZEN.add(new Sig("graalgo.values.GoFunc", "closureFrame",
            new Class<?>[]{}, mframe, false));
        FROZEN.add(new Sig("graalgo.values.GoFunc", "boxCells",
            new Class<?>[]{}, goBoxArr, false));

        // M9 (INTERFACE_FREEZE.md §3.5/§6): probe APIs.
        Class<?> site = cl("graalgo.values.CallSiteProbes$Site");
        FROZEN.add(new Sig("graalgo.values.CallSiteProbes", "setEnabled",
            new Class<?>[]{boolean.class}, void.class));
        FROZEN.add(new Sig("graalgo.values.CallSiteProbes", "isEnabled",
            new Class<?>[]{}, boolean.class));
        FROZEN.add(new Sig("graalgo.values.CallSiteProbes", "reset",
            new Class<?>[]{}, void.class));
        FROZEN.add(new Sig("graalgo.values.CallSiteProbes", "registerSite",
            new Class<?>[]{String.class}, site));
        FROZEN.add(new Sig("graalgo.values.CallSiteProbes", "snapshotJson",
            new Class<?>[]{}, String.class));
        FROZEN.add(new Sig("graalgo.values.CallSiteProbes", "receiverKey",
            new Class<?>[]{Object.class}, Object.class));
        FROZEN.add(new Sig(site.getName(), "recordTarget",
            new Class<?>[]{Object.class}, void.class, false));
        FROZEN.add(new Sig(site.getName(), "recordReceiver",
            new Class<?>[]{Object.class}, void.class, false));
        Class<?> isite = cl("graalgo.values.InterfaceDispatchProbes$Site");
        FROZEN.add(new Sig("graalgo.values.InterfaceDispatchProbes", "setEnabled",
            new Class<?>[]{boolean.class}, void.class));
        FROZEN.add(new Sig("graalgo.values.InterfaceDispatchProbes", "isEnabled",
            new Class<?>[]{}, boolean.class));
        FROZEN.add(new Sig("graalgo.values.InterfaceDispatchProbes", "reset",
            new Class<?>[]{}, void.class));
        FROZEN.add(new Sig("graalgo.values.InterfaceDispatchProbes", "registerSite",
            new Class<?>[]{String.class}, isite));
        FROZEN.add(new Sig("graalgo.values.InterfaceDispatchProbes", "sites",
            new Class<?>[]{}, java.util.Map.class));
    }

    /** Classes that must exist as public types on the frozen boundary. */
    private static final List<String> FROZEN_CLASSES = new ArrayList<>();

    static {
        // M9 (INTERFACE_FREEZE.md §3.5): values types frozen as types.
        for (String t : new String[]{"GoSlice", "GoMap", "GoString", "GoStruct",
                "GoStructDescriptor", "GoComplex32", "GoComplex64", "GoBox",
                "GoTypeDescriptor", "GoConvert", "GoEquality", "GoInterface",
                "GoFunc", "GoPointer"}) {
            FROZEN_CLASSES.add("graalgo.values." + t);
        }
        // M5 surface classes.
        FROZEN_CLASSES.add("graalgo.runtime.DeferredCall");
        FROZEN_CLASSES.add("graalgo.runtime.GoroutineState");
    }

    private record Ctor(String cls, Class<?>[] params) {}

    private static final List<Ctor> FROZEN_CTORS = new ArrayList<>();

    static {
        // M9 (INTERFACE_FREEZE.md §3.2): NodeFactory construction.
        FROZEN_CTORS.add(new Ctor("graalgo.frontend.NodeFactory",
            new Class<?>[]{com.oracle.truffle.api.source.Source.class}));
        // M9 (INTERFACE_FREEZE.md §3.3): FuncValueCallNode construction.
        // The 5-arg form carries the enclosing funcdecl objId for probe site
        // disambiguation across monomorphized instantiations (M9 P1-1).
        FROZEN_CTORS.add(new Ctor("graalgo.frontend.FuncValueCallNode",
            new Class<?>[]{graalgo.frontend.GoExprNode.class,
                graalgo.frontend.GoExprNode[].class, graalgo.values.GoKind.class}));
        FROZEN_CTORS.add(new Ctor("graalgo.frontend.FuncValueCallNode",
            new Class<?>[]{graalgo.frontend.GoExprNode.class,
                graalgo.frontend.GoExprNode[].class, graalgo.values.GoKind.class,
                boolean.class}));
        FROZEN_CTORS.add(new Ctor("graalgo.frontend.FuncValueCallNode",
            new Class<?>[]{graalgo.frontend.GoExprNode.class,
                graalgo.frontend.GoExprNode[].class, graalgo.values.GoKind.class,
                boolean.class, int.class}));
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
                boolean shapeOk = Modifier.isPublic(m.getModifiers())
                        && (s.wantStatic() == isStatic)
                        && m.getReturnType().equals(s.ret());
                if (!shapeOk) {
                    missing.add(s.cls() + "#" + s.name()
                        + " (expected " + (s.wantStatic() ? "public static" : "public instance")
                        + " returning " + s.ret().getSimpleName()
                        + ", found " + (isStatic ? "static" : "instance")
                        + " returning " + m.getReturnType().getSimpleName() + ")");
                }
            } catch (ClassNotFoundException | NoSuchMethodException e) {
                missing.add(s.cls() + "#" + s.name() + " (" + e.getMessage() + ")");
            }
        }
        for (String cn : FROZEN_CLASSES) {
            try {
                Class<?> cls = Class.forName(cn);
                if (!Modifier.isPublic(cls.getModifiers())) {
                    missing.add(cn + " (not public)");
                }
            } catch (ClassNotFoundException e) {
                missing.add(cn + " (class missing)");
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
        // The Program record: all five component names are frozen.
        try {
            Class<?> prog = Class.forName("graalgo.frontend.NodeFactory$Program");
            List<String> comps = new ArrayList<>();
            for (RecordComponent rc : prog.getRecordComponents()) {
                comps.add(rc.getName());
            }
            List<String> want = List.of("root", "descriptor", "slotsByName",
                "kindsByName", "functions");
            if (!comps.equals(want)) {
                missing.add("NodeFactory$Program components: expected " + want
                    + ", found " + comps);
            }
        } catch (ClassNotFoundException e) {
            missing.add("NodeFactory$Program (class missing)");
        }
        if (!missing.isEmpty()) {
            throw new AssertionError("frozen signatures missing/changed:\n"
                + String.join("\n", missing));
        }
        System.out.println("  (freeze v1: " + FROZEN.size() + " signatures + "
            + FROZEN_CLASSES.size() + " classes + " + FROZEN_CTORS.size()
            + " ctors + Program record verified)");
    }
}
