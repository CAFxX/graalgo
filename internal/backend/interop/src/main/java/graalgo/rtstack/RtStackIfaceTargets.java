package graalgo.rtstack;

import java.util.List;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;

import graalgo.iface.IfaceMethodTable;
import graalgo.iface.IfaceMethodTarget;
import graalgo.runtime.GoCalls;
import graalgo.runtime.GoFuncInfo;
import graalgo.values.GoFunc;

/**
 * Interface-dispatch targets for the synthetic runtime stack-inspection
 * methods ({@code (*runtime.Func).Name()} / {@code Entry()}).
 *
 * <p>Why this exists: these methods have no Go body and no funcdecl, so
 * the frontend's type table carries an empty {@code "methods"} list for
 * {@code runtime.Func} and {@link graalgo.iface.IfaceMethodTables#resolve}
 * installs nothing for {@code *runtime.Func}. A program that calls
 * {@code Name()} through an interface (e.g. {@code var i interface{
 * Name() string } = fn; i.Name()}) therefore compiled to an
 * {@code ifacemethod} node whose dispatch died at runtime with
 * "no method table installed for dynamic type \"*runtime.Func\"" — a
 * host fatal error on a program gc runs fine. Direct calls and method
 * values never hit this: they lower to the {@code runtimefuncname} /
 * {@code runtimefuncentry} wire nodes.
 *
 * <p>This uses {@link IfaceMethodTable#installMethod} — the extension
 * point for Java-implemented methods with no Go body in the export —
 * exactly as documented there. The targets are plain {@link GoFunc}s
 * built from {@link RootNode}s following the {@link GoCalls} convention
 * ({@code args[0]} is the GoFunc, {@code args[1]} the receiver), so
 * dispatch invokes them through the ordinary itab path with no
 * dispatch-site special case. Nil-receiver semantics match gc exactly:
 * Name() returns {@code ""} for nil, Entry() panics (nil dereference).
 * Multi-result members
 * ({@code (*Func).FileLine}, {@code (*Frames).Next}) are NOT installed:
 * their interface form needs the pair machinery, and the frontend keeps
 * its existing handling for them.
 *
 * <p>Installed by both launchers' post-build installs (P3SyncMain and
 * P3KindRegistrations.afterBuild), after the doc-derived tables, so the
 * targets are present on every build and never leak across programs
 * (installMethod drops the itab cache; the next build re-installs).
 */
public final class RtStackIfaceTargets {
    private RtStackIfaceTargets() {}

    /** Descriptor name of the dynamic type, as interface dispatch computes it. */
    static final String FUNC_DESCRIPTOR = "*runtime.Func";

    /**
     * Installs the {@code *runtime.Func} Name/Entry targets. Idempotent:
     * re-installing replaces the same entries and drops the itab cache.
     */
    public static void install() {
        IfaceMethodTable.installMethod(FUNC_DESCRIPTOR, "Name",
                new IfaceMethodTarget(funcNameTarget(), new int[0], true, false, -1, List.of()));
        IfaceMethodTable.installMethod(FUNC_DESCRIPTOR, "Entry",
                new IfaceMethodTarget(funcEntryTarget(), new int[0], true, false, -1, List.of()));
    }

    /**
     * Unwraps the dispatch receiver to its {@link GoFuncInfo} handle. The
     * interface box carries the pointer value, so the receiver is the
     * pointer the direct wire nodes dereference; a bare handle is
     * accepted too so a boxing variant cannot silently misroute.
     *
     * @param panicOnNil true for Entry() (gc panics on a nil receiver),
     *                   false for Name() (gc returns "" for nil)
     */
    private static GoFuncInfo derefReceiver(Object recv, RootNode site, boolean panicOnNil) {
        if (recv instanceof GoFuncInfo info) {
            return info;
        }
        if (panicOnNil) {
            return RtStack.derefFunc(recv, site);
        }
        return RtStack.funcInfoOrNull(recv);
    }

    /** {@code (*runtime.Func).Name()} as a dispatchable {@link GoFunc}. */
    static GoFunc funcNameTarget() {
        RootNode root = new RootNode(null) {
            @Override
            public Object execute(VirtualFrame frame) {
                Object recv = frame.getArguments()[1];
                GoFuncInfo info = derefReceiver(recv, this, false);
                return RtStack.goString(info == null ? "" : info.funcName());
            }
        };
        return new GoFunc(root.getCallTarget(), null);
    }

    /** {@code (*runtime.Func).Entry()} as a dispatchable {@link GoFunc}. */
    static GoFunc funcEntryTarget() {
        RootNode root = new RootNode(null) {
            @Override
            public Object execute(VirtualFrame frame) {
                Object recv = frame.getArguments()[1];
                return derefReceiver(recv, this, true).entry();
            }
        };
        return new GoFunc(root.getCallTarget(), null);
    }
}
