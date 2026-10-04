package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoTypeDescriptor;
import java.util.List;

/**
 * A type switch on a {@link GoInterface} value (WS E).
 * <p>
 * Matches the interface's dynamic type descriptor against the clause type
 * lists (identity on the canonical descriptors, so named types never
 * conflate with their underlying types). {@code case nil} matches a nil
 * interface (typed-nil with a nil data pointer also matches its type
 * clause, per Go semantics). Panics are stubbed as internal errors until
 * WS F gives them real semantics.
 *
 * <p>Binding ({@code switch x := y.(type)}): the clause that matches binds
 * a fresh interface value with the clause's asserted type descriptor and a
 * deep copy of the data; {@code default} and {@code case nil} bind the
 * original interface value (which has the switch's static interface type).
 */
public final class TypeSwitchNode extends GoStmtNode {
    /** One type-switch clause. */
    public static final class TypeCaseClause extends Node {
        /** Asserted type descriptors; empty for default/nil-only clauses. */
        public final List<GoTypeDescriptor> types;
        public final boolean nilCase;
        public final boolean isDefault;
        public final int bindSlot; // -1 = no binding
        public final GoKind bindKind; // null = no binding
        @Child private GoStmtNode body;

        public TypeCaseClause(List<GoTypeDescriptor> types, boolean nilCase, boolean isDefault,
                              int bindSlot, GoKind bindKind, GoStmtNode body) {
            this.types = List.copyOf(types);
            this.nilCase = nilCase;
            this.isDefault = isDefault;
            this.bindSlot = bindSlot;
            this.bindKind = bindKind;
            this.body = body;
        }
    }

    @Child private GoExprNode tag;
    @Child private GoStmtNode init; // nullable
    /** Descriptor of the switch's static interface type (for default/nil bindings). */
    private final GoTypeDescriptor ifaceDesc;

    @Children private final TypeCaseClause[] clauseArray;

    public TypeSwitchNode(GoExprNode tag, GoStmtNode init,
                          List<TypeCaseClause> clauses, GoTypeDescriptor ifaceDesc) {
        this.tag = tag;
        this.init = init;
        this.ifaceDesc = ifaceDesc;
        this.clauseArray = clauses.toArray(new TypeCaseClause[0]);
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        if (init != null) {
            init.executeVoid(frame);
        }
        GoInterface v = (GoInterface) tag.executeObject(frame);
        GoTypeDescriptor dyn = v != null ? (GoTypeDescriptor) v.descriptor() : null;
        Object data = v != null ? v.data() : null;
        for (TypeCaseClause c : clauseArray) {
            if (c.isDefault) {
                continue;
            }
            boolean hit = false;
            if (c.nilCase && data == null) {
                hit = true;
            } else if (data != null) {
                for (GoTypeDescriptor t : c.types) {
                    if (t == dyn) {
                        hit = true;
                        break;
                    }
                }
            }
            if (!hit) {
                continue;
            }
            bindAndRun(frame, c, v, dyn, data);
            return;
        }
        for (TypeCaseClause c : clauseArray) {
            if (c.isDefault) {
                bindAndRun(frame, c, v, dyn, data);
                return;
            }
        }
    }

    private void bindAndRun(VirtualFrame frame, TypeCaseClause c,
                            GoInterface v, GoTypeDescriptor dyn, Object data) {
        if (c.bindSlot >= 0) {
            if (c.isDefault || c.nilCase) {
                // v has the switch's interface type: bind the interface value itself.
                frame.setObject(c.bindSlot, Boxing.copyForAssign(GoKind.INTERFACE, v));
            } else {
                // v has the clause's asserted type: bind the raw dynamic value
                // with the clause's kind (Go value semantics: copy structs/arrays).
                // Multi-type clauses bind the guard at interface kind: the
                // slot must hold a GoInterface carrying the dynamic
                // descriptor, not the raw data (else the first interface
                // use throws ClassCastException).
                Object bound = Boxing.copyForAssign(c.bindKind, data);
                if (c.bindKind == GoKind.INTERFACE) {
                    bound = new GoInterface(dyn, bound, null);
                }
                Boxing.setSlot(frame, c.bindSlot, c.bindKind, bound);
            }
        }
        c.body.executeVoid(frame);
    }
}
