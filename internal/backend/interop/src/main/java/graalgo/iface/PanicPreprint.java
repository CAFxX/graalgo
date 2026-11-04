package graalgo.iface;

import java.nio.charset.StandardCharsets;

import graalgo.frontend.GoFuncRootNode;
import graalgo.frontend.SelectorNode;
import graalgo.runtime.GoCalls;
import graalgo.runtime.GoroutineState;
import graalgo.runtime.PanicException;
import graalgo.runtime.PanicRendering;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoString;
import graalgo.values.GoTypeDescriptor;

/**
 * The {@code preprintpanics} step for uncaught panics (NEW-P1-10, gc
 * runtime/panic.go): a panic value whose dynamic type implements
 * {@code error} prints its {@code Error()} text, else a value implementing
 * {@code Stringer} prints its {@code String()} text — instead of the
 * {@code (type) 0xADDR} rendering that {@link PanicRendering} falls back
 * to for composite values.
 *
 * <p>Installed as {@link PanicRendering}'s preprint provider by
 * {@code LinknameIntrinsics.install} (next to the itab provider), so it
 * runs only on the product launcher path where method tables exist, and
 * only at fatal-print time: {@link PanicRendering#renderPanicLine} is
 * called solely from the uncaught-panic reporters, so a recovered panic
 * still yields its original value from {@code recover()}.
 *
 * <p>Dispatch mirrors {@link IfaceDispatchNode} (same table, same
 * receiver adaptation: embedded promotion paths, {@code addrOf} for
 * pointer-receiver methods promoted via value-embedded fields, embedded-
 * interface redispatch, the interface-receiver-param rule), minus the
 * PIC: preprint runs once per fatal panic. Anything unresolvable or
 * failing (no method, unsupported promotion, a method that panics, a
 * non-string result) returns null, and the caller falls back to the
 * kind-based rendering — a crash report must never die inside preprint.
 */
public final class PanicPreprint {
    private PanicPreprint() {}

    /**
     * Returns the {@code Error()}/{@code String()} text for the panic
     * value, or null when preprint does not apply or cannot run.
     */
    public static String preprint(PanicException p) {
        Object value = p.goValue();
        if (value instanceof GoInterface gi) {
            // Defensive: throwPanic unboxes interfaces, but never render
            // a wrapper as the value.
            if (gi.isNil()) {
                return null;
            }
            value = gi.data();
        }
        if (value == null) {
            return null;
        }
        GoTypeDescriptor descriptor = null;
        if (p.descriptor() instanceof GoTypeDescriptor gtd) {
            descriptor = gtd;
        } else if (value instanceof GoInterface gi && gi.descriptor() instanceof GoTypeDescriptor gtd2) {
            descriptor = gtd2;
        }
        String descriptorName = descriptor != null ? descriptor.name() : null;
        if (descriptorName == null || descriptorName.isEmpty()) {
            String typeName = p.goTypeName();
            if (typeName == null || typeName.isEmpty()) {
                return null;
            }
            descriptorName = typeName;
        }
        try {
            for (String method : new String[] {"Error", "String"}) {
                String text = tryMethod(value, descriptor, descriptorName, method);
                if (text != null) {
                    return text;
                }
                // A present-but-unsupported Error target must not fall
                // through to String: gc calls Error for such a value.
                if (hasUnsupportedTarget(descriptorName, method)) {
                    return null;
                }
            }
        } catch (Throwable ignored) {
            return null;
        }
        return null;
    }

    private static boolean hasUnsupportedTarget(String descriptorName, String method) {
        IfaceMethodTarget target = IfaceMethodTable.lookup(descriptorName, method);
        return target != null && !target.supported();
    }

    /**
     * Dispatches one method name on the panic value; null when the type
     * has no such method (or it cannot run). Throws only for failures the
     * caller converts to a null preprint (e.g. a panic inside the method).
     */
    private static String tryMethod(Object data, GoTypeDescriptor descriptor,
            String descriptorName, String method) {
        IfaceMethodTarget target = IfaceMethodTable.lookup(descriptorName, method);
        if (target == null) {
            return null;
        }
        if (!target.supported()) {
            return null;
        }
        // Embedded-interface redispatch, mirroring IfaceDispatchNode and
        // the reflect redispatch path: walk interface fields until a
        // concrete target is reached.
        Object curData = data;
        GoTypeDescriptor curDescriptor = descriptor;
        while (target.isEmbeddedRedispatch()) {
            Object fieldVal = SelectorNode.readFieldPath(curData, target.recvPath(), false);
            if (!(fieldVal instanceof GoInterface gi) || gi.isNil()) {
                return null;
            }
            curData = gi.data();
            curDescriptor = (GoTypeDescriptor) gi.descriptor();
            String innerName = curDescriptor.name();
            IfaceItab inner = IfaceMethodTable.itabFor(innerName,
                    target.embeddedIfaceId(), target.embeddedIfaceMethods());
            target = inner.methods().get(method);
            if (target == null || !target.supported()) {
                return null;
            }
        }
        if (target.func() == null) {
            return null;
        }
        Object receiver = target.isDirect()
                ? curData
                : SelectorNode.readFieldPath(curData, target.recvPath(), !target.pointerReceiver());
        if (target.addrOf() && !(receiver instanceof GoPointer)) {
            receiver = new GoPointer(receiver);
        }
        if (target.isDirect() && curDescriptor != null && receiverParamIsInterface(target)) {
            receiver = new GoInterface(curDescriptor, curData, null);
        }
        // FuncValueCallNode's recoverAllowed dance: recover() is only
        // non-nil when called directly by a deferred function; preprint
        // runs after unwinding, so a recover() inside Error()/String()
        // must see nil.
        GoroutineState state = GoroutineState.current();
        boolean prev = state != null && state.isRecoverAllowed();
        if (state != null) {
            state.setRecoverAllowed(false);
        }
        try {
            Object result = GoCalls.invoke(target.func(), new Object[] {receiver});
            if (result instanceof GoString gs) {
                return new String(gs.bytes(), StandardCharsets.UTF_8);
            }
            if (result instanceof String s) {
                return s;
            }
            return null;
        } finally {
            if (state != null) {
                state.setRecoverAllowed(prev);
            }
        }
    }

    /** Mirrors {@code IfaceDispatchNode.receiverParamIsInterface}. */
    private static boolean receiverParamIsInterface(IfaceMethodTarget target) {
        if (target.func().target().getRootNode() instanceof GoFuncRootNode root) {
            GoKind[] kinds = root.paramKinds();
            return kinds.length > 0 && kinds[0] == GoKind.INTERFACE;
        }
        return false;
    }
}
