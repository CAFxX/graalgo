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
 * conflate with their underlying types). {@code case nil} matches only a
 * nil interface; a typed nil (non-nil interface with nil data) matches the
 * clause of its dynamic type, per Go semantics. Panics are stubbed as
 * internal errors until WS F gives them real semantics.
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
        /**
         * For interface clause types: the interface's method implkeys for
         * the implements check (parallel to {@link #types}; null entry for
         * non-interface types). P3-M8b: type switches on interfaces must
         * match by implementation, not descriptor identity.
         */
        public final List<java.util.List<String>> ifaceMethods;
        public final boolean nilCase;
        public final boolean isDefault;
        public final int bindSlot; // -1 = no binding
        public final GoKind bindKind; // null = no binding
        @Child private GoStmtNode body;

        public TypeCaseClause(List<GoTypeDescriptor> types, List<java.util.List<String>> ifaceMethods,
                              boolean nilCase, boolean isDefault,
                              int bindSlot, GoKind bindKind, GoStmtNode body) {
            this.types = List.copyOf(types);
            // Null entries allowed (non-interface types); copy manually.
            this.ifaceMethods = ifaceMethods == null ? null
                    : java.util.Collections.unmodifiableList(new java.util.ArrayList<>(ifaceMethods));
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
    /**
     * Per-document dynamic-type method implkey sets (descriptor name →
     * implkeys), for interface-clause implements checks. Null when no
     * clause has an interface type.
     */
    private final java.util.Map<String, java.util.Set<String>> dynamicMethodSets;

    @Children private final TypeCaseClause[] clauseArray;

    public TypeSwitchNode(GoExprNode tag, GoStmtNode init,
                          List<TypeCaseClause> clauses, GoTypeDescriptor ifaceDesc,
                          java.util.Map<String, java.util.Set<String>> dynamicMethodSets) {
        this.tag = tag;
        this.init = init;
        this.ifaceDesc = ifaceDesc;
        this.dynamicMethodSets = dynamicMethodSets;
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
            // case nil matches only a nil interface: a typed nil has null
            // data but a non-null descriptor, so it must NOT match here
            // (GoInterface.isNil() is descriptor == null).
            if (c.nilCase && (v == null || v.isNil())) {
                hit = true;
            } else if (dyn != null) {
                // A typed nil (data == null, dyn != null) still carries its
                // dynamic type and matches the clause of that type; a nil
                // interface (dyn == null) matches no type clause.
                for (int i = 0; i < c.types.size(); i++) {
                    GoTypeDescriptor t = c.types.get(i);
                    if (t == dyn) {
                        hit = true;
                        break;
                    }
                    // P3-M8b: interface clause — match by implementation,
                    // not descriptor identity (e.g. case unwrapper: with
                    // dynamic type *os.PathError).
                    java.util.List<String> im = c.ifaceMethods == null ? null
                            : c.ifaceMethods.get(i);
                    if (im != null && dynamicMethodSets != null && implementsDyn(dyn, im)) {
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

    /**
     * Implements check for interface type-switch clauses (P3-M8b): the
     * dynamic type's method implkey set must cover the interface's.
     * Mirrors {@link TypeAssertNode#holds}.
     */
    private boolean implementsDyn(GoTypeDescriptor dyn, java.util.List<String> ifaceMethods) {
        if (dyn == null) {
            return false;
        }
        java.util.Set<String> dynMethods =
                dynamicMethodSets.getOrDefault(dyn.name(), java.util.Set.of());
        if (dynMethods.contains(NodeFactory.TEMPLATE_METHODS_SKIPPED)) {
            throw new FrontendException("cannot switch generic instantiation "
                    + dyn.name() + " on interface: method tables for instantiated methods"
                    + " are not supported (P3-M3 scope)");
        }
        return dynMethods.containsAll(ifaceMethods);
    }
}
