package graalgo.rtstack;

import java.util.List;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.nodes.RootNode;

import graalgo.iface.IfaceMethodTable;
import graalgo.iface.IfaceMethodTarget;
import graalgo.runtime.GoFuncInfo;
import graalgo.runtime.StackTrace;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;

/**
 * Interface method-table entries for the synthetic {@code *runtime.Func}
 * type (REVIEW-2026-11-03-10 P1-2).
 *
 * <p>Why this exists: {@code (*runtime.Func).Name/Entry/FileLine} are
 * wire-node intrinsics, not funcdecls. Direct calls and concrete method
 * values are intercepted by the exporter and lowered to the
 * {@code runtimefuncname} / {@code runtimefuncentry} /
 * {@code runtimefuncfileline} wire nodes, so no funcdecl object id exists
 * for the method-table builder to point at. The wire type table therefore
 * carries no method entries for {@code *runtime.Func}, the table resolver
 * skips the empty entry, and an interface call
 * ({@code var n interface{ Name() string } = fn; n.Name()}) dies in the
 * backend with "no method table installed for dynamic type
 * {@code *runtime.Func}". The itab entry cannot come from the wire
 * document; it must be installed from the intrinsic implementations
 * themselves, which is exactly what {@link IfaceMethodTable#installMethod}
 * is for (Java-implemented methods with no Go body).
 *
 * <p>The installed targets are synthetic {@link GoFunc}s whose roots reuse
 * the same dereference and lookup logic as the direct wire nodes
 * ({@link RtStack#derefFunc} plus {@link StackTrace}), so interface
 * dispatch, direct calls, and method values agree, including the nil
 * {@code *Func} Go panic. The descriptor key is the pointer-to-named
 * form the dispatch path computes from the boxed value
 * ({@code *runtime.Func}); the receiver the dispatcher passes is the
 * boxed {@code *Func} value itself.
 *
 * <p>FileLine pair layout: the interface multi-result desugar synthesizes
 * a pair struct for the {@code (string, int)} signature and extracts its
 * fields by index. The pair's field order is always (string, int), so
 * the synthetic pair descriptor below lists the string field first
 * (objects carrier) and the int field second (longs carrier); index-based
 * extraction then reads the right carriers whatever the synthesized
 * pair's field names are.
 *
 * <p>Called by both product launchers after the wholesale table install
 * (which would otherwise wipe these entries): the sync launcher and the
 * main launcher's post-build registrar. Installing unconditionally is
 * inert for programs that never dispatch through an interface on a
 * {@code *runtime.Func}.
 */
public final class RtStackItab {
    private RtStackItab() {}

    /** Descriptor name of the dynamic type, as computed by interface boxing. */
    public static final String FUNC_DESCRIPTOR = "*runtime.Func";

    /** Pair descriptor for the FileLine interface result (see class javadoc). */
    static final GoStructDescriptor FILE_LINE_PAIR_DESCRIPTOR = new GoStructDescriptor(
            "runtime.__wsd_ifacepair_FileLine",
            List.of(
                    new GoStructDescriptor.Field("f0", GoKind.STRING, false, 0, -1, null, null, null),
                    new GoStructDescriptor.Field("f1", GoKind.INT64, true, 0, -1, null, null, null)));

    /** Installs the {@code *runtime.Func} method-table entries. */
    public static void install() {
        IfaceMethodTable.installMethod(FUNC_DESCRIPTOR, "Name",
                new IfaceMethodTarget(makeNameFunc(), new int[0], true, false, -1, null));
        IfaceMethodTable.installMethod(FUNC_DESCRIPTOR, "Entry",
                new IfaceMethodTarget(makeEntryFunc(), new int[0], true, false, -1, null));
        IfaceMethodTable.installMethod(FUNC_DESCRIPTOR, "FileLine",
                new IfaceMethodTarget(makeFileLineFunc(), new int[0], true, false, -1, null));
    }

    /**
     * Frame arguments for every target below follow the {@code GoCalls}
     * convention: {@code [GoFunc, receiver, methodArgs...]}.
     */
    private static GoFunc makeNameFunc() {
        RootNode root = new RootNode(null) {
            @Override
            public Object execute(VirtualFrame frame) {
                Object[] args = frame.getArguments();
                GoFuncInfo info = RtStack.derefFunc(args[1], this);
                return RtStack.goString(info.funcName());
            }
        };
        return new GoFunc(root.getCallTarget(), null);
    }

    private static GoFunc makeEntryFunc() {
        RootNode root = new RootNode(null) {
            @Override
            public Object execute(VirtualFrame frame) {
                Object[] args = frame.getArguments();
                GoFuncInfo info = RtStack.derefFunc(args[1], this);
                return Long.valueOf(info.entry());
            }
        };
        return new GoFunc(root.getCallTarget(), null);
    }

    private static GoFunc makeFileLineFunc() {
        RootNode root = new RootNode(null) {
            @Override
            public Object execute(VirtualFrame frame) {
                Object[] args = frame.getArguments();
                Node site = this;
                RtStack.derefFunc(args[1], site);
                long pc = ((Number) args[2]).longValue();
                StackTrace.PcInfo info = StackTrace.lookup(pc);
                String file = info == null ? "" : info.file();
                int line = info == null ? 0 : info.line();
                long[] longs = new long[FILE_LINE_PAIR_DESCRIPTOR.longsCount()];
                Object[] objs = new Object[FILE_LINE_PAIR_DESCRIPTOR.objectsCount()];
                objs[0] = RtStack.goString(file);
                longs[0] = line;
                return new GoStruct(FILE_LINE_PAIR_DESCRIPTOR, longs, objs);
            }
        };
        return new GoFunc(root.getCallTarget(), null);
    }
}
