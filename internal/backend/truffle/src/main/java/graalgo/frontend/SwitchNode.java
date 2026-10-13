package graalgo.frontend;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;
import graalgo.values.GoEquality;
import graalgo.values.GoInterface;
import graalgo.values.GoKind;
import graalgo.values.GoTypeDescriptor;

/**
 * Expression {@code switch} (M4, WS E).
 *
 * <p>Go semantics: the tag is evaluated exactly once; case expressions are
 * evaluated left-to-right, top-to-bottom, stopping at the first equal one;
 * the matching clause's body runs; {@code fallthrough} (a
 * {@link FallthroughSignal}) runs the next clause's body without re-testing
 * its cases. A nil tag is {@code switch { }} (cases are booleans).
 * {@code break} (labeled or unlabeled) exits the switch.
 *
 * <p>Interface tags unwrap for comparison: the dynamic type must be
 * identical to the case's type and the values Go-equal (gc-verified:
 * {@code switch i { case 42: }} with {@code i any = 42} matches;
 * {@code i any = "hello"} does not). Dynamic-type descriptors are resolved
 * once at build time (parallel to the case expressions).
 */
public final class SwitchNode extends GoStmtNode implements LabelTarget {
    @Child private GoStmtNode init;
    @Child private GoExprNode tag;
    @Children private GoStmtNode[] bodies;
    /** All case expressions, flattened; clause i spans [clauseOffsets[i], clauseOffsets[i+1]). */
    @Children private GoExprNode[] allCaseExprs;
    private final int[] clauseOffsets;
    private final boolean[] isDefault;
    private final GoKind tagKind;
    /** Dynamic-type descriptor per case expression (build-time resolved), flattened like above. */
    private final GoTypeDescriptor[] allCaseDescs;
    /** Dynamic-type descriptor of the tag (for interface-typed cases). */
    private final GoTypeDescriptor tagDesc;
    private String label;

    public SwitchNode(GoStmtNode init, GoExprNode tag, GoKind tagKind, GoTypeDescriptor tagDesc,
                      GoExprNode[][] caseExprs, GoTypeDescriptor[][] caseDescs,
                      boolean[] isDefault, GoStmtNode[] bodies) {
        this.init = init;
        this.tag = tag;
        this.tagKind = tagKind;
        this.tagDesc = tagDesc;
        int total = 0;
        for (GoExprNode[] c : caseExprs) {
            total += c.length;
        }
        this.allCaseExprs = new GoExprNode[total];
        this.allCaseDescs = new GoTypeDescriptor[total];
        this.clauseOffsets = new int[caseExprs.length + 1];
        int p = 0;
        for (int i = 0; i < caseExprs.length; i++) {
            clauseOffsets[i] = p;
            for (int j = 0; j < caseExprs[i].length; j++) {
                allCaseExprs[p] = caseExprs[i][j];
                allCaseDescs[p] = caseDescs[i][j];
                p++;
            }
        }
        clauseOffsets[caseExprs.length] = p;
        this.isDefault = isDefault;
        this.bodies = bodies;
    }

    @Override
    public void setLabel(String label) {
        this.label = label;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        if (init != null) {
            init.executeVoid(frame);
        }
        // A nil tag is `switch {}`: the tag is implicitly true (boolean cases).
        Object tagVal = tag == null ? Boolean.TRUE : readTag(frame);
        int idx = findMatch(frame, tagVal);
        if (idx < 0) {
            return;
        }
        for (int i = idx; i < bodies.length; i++) {
            try {
                bodies[i].executeVoid(frame);
                return; // No fallthrough: done.
            } catch (FallthroughSignal f) {
                // Run the next clause's body without testing its cases.
            } catch (BreakSignal b) {
                if (b.matches(label)) {
                    return;
                }
                throw b;
            }
        }
    }

    /** Reads the tag into its canonical boxed form. */
    private Object readTag(VirtualFrame frame) {
        if (tagKind == GoKind.INTERFACE) {
            return tag.executeObject(frame);
        }
        return Boxing.box(tagKind, tag, frame);
    }

    private int findMatch(VirtualFrame frame, Object tagVal) {
        int def = -1;
        for (int i = 0; i < isDefault.length; i++) {
            if (isDefault[i]) {
                def = i;
                continue;
            }
            for (int j = clauseOffsets[i]; j < clauseOffsets[i + 1]; j++) {
                if (caseEqual(allCaseExprs[j], allCaseDescs[j], tagVal, frame)) {
                    return i;
                }
            }
        }
        return def;
    }

    /** Go {@code ==} between the switch tag and one case expression. */
    private boolean caseEqual(GoExprNode caseNode, GoTypeDescriptor caseDesc,
                             Object tagVal, VirtualFrame frame) {
        GoKind ck = caseNode.kind();
        if (tagKind == GoKind.INTERFACE) {
            // P3-M7: a nil interface tag may flow as Java null — narrow to NIL.
            GoInterface gi = GoInterface.narrow(tagVal);
            if (ck == GoKind.INTERFACE) {
                // P3-M7: a nil interface case may flow as Java null — narrow to NIL.
                GoInterface ci = GoInterface.narrow(Boxing.box(ck, caseNode, frame));
                if (gi.isNil() || ci.isNil()) {
                    return gi.isNil() && ci.isNil();
                }
                return gi.descriptor() == ci.descriptor()
                        && GoEquality.equal(gi.data(), ci.data());
            }
            if (gi.isNil()) {
                return false;
            }
            return gi.descriptor() == caseDesc
                    && GoEquality.equal(gi.data(), Boxing.box(ck, caseNode, frame));
        }
        if (ck == GoKind.INTERFACE) {
            // P3-M7: a nil interface case may flow as Java null — narrow to NIL.
            GoInterface ci = GoInterface.narrow(Boxing.box(ck, caseNode, frame));
            if (ci.isNil()) {
                return false;
            }
            return ci.descriptor() == tagDesc
                    && GoEquality.equal(Boxing.box(tagKind, tag, frame), ci.data());
        }
        return GoEquality.equal(tagVal, Boxing.box(ck, caseNode, frame));
    }
}
