package graalgo.p3chan.nodes;

import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.Node;

import graalgo.frontend.AssignNode;
import graalgo.frontend.BlockNode;
import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.GoStmtNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.runtime.ChanOps;
import graalgo.values.GoKind;

/**
 * The {@code select} statement (P3-M5, WS E, ADR-0005 Appendix C): the
 * {@code "select"} wire node (statement kind).
 *
 * <p>Evaluation (gc semantics):
 * <ol>
 *   <li>On entry, every case's channel operand (and a send case's value)
 *       is evaluated exactly once, in source order.
 *   <li>{@link ChanOps#selectGo} picks the case: uniformly at random
 *       among ready cases; the {@code default} case (a
 *       {@code SelectCase} with {@code chan == null} — the documented
 *       Phase-2 convention) when nothing is ready; otherwise the
 *       goroutine parks scheduler-aware.
 *   <li>A chosen recv case's value/ok are assigned to its
 *       {@code targets} (before the body) via
 *       {@link ChanOps#selectRecvValue}/{@link ChanOps#selectRecvOk}.
 *   <li>The chosen case's body (a statement list) executes.
 * </ol>
 *
 * <p>Nil-channel cases are <em>dropped</em> before {@code selectGo} (a
 * nil Go channel evaluates to Java {@code null}, which the Phase-2
 * convention would misread as {@code default}). Dropping is gc-exact: a
 * nil-channel case is never ready and can never be chosen, and a nil
 * channel cannot be closed (so no poison) — with all cases dropped the
 * select parks forever ({@code select {}} semantics) or takes
 * {@code default}, exactly like gc. No phase-2 change needed.
 *
 * <p>Wire {@code targets} (recv cases only): an array of 0–2 objids
 * (entry 0 the value, entry 1 the ok bit, which must be bool). The
 * exporter declares every target — including {@code :=} variables and
 * blank temps — with an ordinary {@code vardecl} dominating the select
 * (it owns scope per the ADR) and passes their objids here; the
 * variable's type is read from the wire {@code objects} array. Targets
 * must be plain locals of the current function frame — an undeclared
 * objid fails fast (a global {@code =} target is not in the frame maps
 * and must be desugared by the exporter through a temporary), as do
 * boxed targets.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerStmtKind} hook — no Phase-1/Phase-2 change.
 *
 * <p>verifies: GG-0017 (chan slice)
 */
public final class SelectNode extends GoStmtNode {

    /** One case: operands, pre-built target assignments, and the body. */
    static final class SelectCaseNode extends Node {
        @Child GoExprNode chanExpr;    // null for default
        @Child GoExprNode valueExpr;   // null unless send case
        @Child GoStmtNode body;         // BlockNode over the case's statements
        @Child GoStmtNode valueAssign;  // null unless a value target
        @Child GoStmtNode okAssign;     // null unless an ok target
        final boolean isDefault;
        final boolean isSend;

        SelectCaseNode(GoExprNode chanExpr, GoExprNode valueExpr, GoStmtNode body,
                       GoStmtNode valueAssign, GoStmtNode okAssign,
                       boolean isDefault, boolean isSend) {
            this.chanExpr = chanExpr;
            this.valueExpr = valueExpr;
            this.body = body;
            this.valueAssign = valueAssign;
            this.okAssign = okAssign;
            this.isDefault = isDefault;
            this.isSend = isSend;
        }
    }

    @Children
    private final SelectCaseNode[] cases;

    private SelectNode(SelectCaseNode[] cases) {
        this.cases = cases;
    }

    @Override
    public void executeVoid(VirtualFrame frame) {
        int n = cases.length;
        // Phase 1: evaluate channel/value operands once, in source order.
        ChanOps.Chan[] chans = new ChanOps.Chan[n];
        Object[] sendVals = new Object[n];
        for (int i = 0; i < n; i++) {
            SelectCaseNode c = cases[i];
            if (c.isDefault) {
                continue;
            }
            Object chObj = c.chanExpr.executeObject(frame);
            chans[i] = (ChanOps.Chan) chObj; // null = nil channel (dropped below)
            if (c.isSend) {
                // Kind-aware: a primitive-kind value (e.g. int literal)
                // does not carry its value in executeObject.
                sendVals[i] = ChanWire.evalBoxed(c.valueExpr, frame);
            }
        }
        // Phase 2: drop nil-channel cases (gc-exact — never ready); remap
        // the selectGo index back to the original case.
        int kept = 0;
        int[] origIndex = new int[n];
        for (int i = 0; i < n; i++) {
            if (!cases[i].isDefault && chans[i] == null) {
                continue;
            }
            origIndex[kept++] = i;
        }
        ChanOps.SelectCase[] wire = new ChanOps.SelectCase[kept];
        for (int k = 0; k < kept; k++) {
            int i = origIndex[k];
            SelectCaseNode c = cases[i];
            if (c.isDefault) {
                wire[k] = new ChanOps.SelectCase(null, false, null);
            } else if (c.isSend) {
                wire[k] = new ChanOps.SelectCase(chans[i], true, sendVals[i]);
            } else {
                wire[k] = new ChanOps.SelectCase(chans[i], false, null);
            }
        }
        int chosen = origIndex[ChanOps.selectGo(wire)];
        // Phase 3: assign recv targets (value, then ok) before the body.
        // The selectGo → stash-read window contains no park, so the
        // per-thread stash cannot be lost to carrier migration.
        SelectCaseNode c = cases[chosen];
        if (!c.isDefault && !c.isSend) {
            if (c.valueAssign != null) {
                c.valueAssign.executeVoid(frame);
            }
            if (c.okAssign != null) {
                c.okAssign.executeVoid(frame);
            }
        }
        // Phase 4: the chosen case's body.
        c.body.executeVoid(frame);
    }

    /**
     * Builds the node from the {@code "select"} wire node. Fields:
     * {@code cases}: array of {@code "selectcase"} nodes, each with
     * fields {@code {"kind": "recv"|"send"|"default", chan?, value?,
     * targets?, body}} — {@code body} is a {@code "block"} node.
     */
    public static GoStmtNode build(Json.Object node, NodeFactory.BuildCtx ctx,
            NodeFactory factory) {
        Json.Object f = node.object("fields", "select");
        String path = "select.fields";
        Json.Array caseArr = f.array("cases", path);
        SelectCaseNode[] cases = new SelectCaseNode[caseArr.size()];
        for (int i = 0; i < caseArr.size(); i++) {
            String cpath = path + ".cases[" + i + "]";
            if (!(caseArr.get(i) instanceof Json.Object co)) {
                throw new FrontendException(cpath + ": case must be an object");
            }
            if (!"selectcase".equals(co.string("kind", cpath))) {
                throw new FrontendException(cpath + ": case must be a \"selectcase\" node");
            }
            cases[i] = buildCase(co.object("fields", cpath), cpath + ".fields", ctx, factory);
        }
        return new SelectNode(cases);
    }

    private static SelectCaseNode buildCase(Json.Object c, String cpath,
            NodeFactory.BuildCtx ctx, NodeFactory factory) {
        String kind = c.string("kind", cpath);
        GoExprNode chanExpr = null;
        GoExprNode valueExpr = null;
        GoStmtNode valueAssign = null;
        GoStmtNode okAssign = null;
        boolean isDefault = false;
        boolean isSend = false;
        switch (kind) {
            case "default" -> isDefault = true;
            case "recv" -> {
                chanExpr = ChanWire.exprField(c, "chan", "selectcase", factory, ctx);
                GoStmtNode[] assigns = buildTargetAssigns(c, cpath, ctx);
                valueAssign = assigns[0];
                okAssign = assigns[1];
            }
            case "send" -> {
                isSend = true;
                chanExpr = ChanWire.exprField(c, "chan", "selectcase", factory, ctx);
                valueExpr = ChanWire.exprField(c, "value", "selectcase", factory, ctx);
                if (c.opt("targets") != null) {
                    throw new FrontendException(cpath + ": send case takes no targets");
                }
            }
            default -> throw new FrontendException(cpath + ": unknown select case kind \"" + kind + "\"");
        }
        if (isDefault && c.opt("chan") != null) {
            throw new FrontendException(cpath + ": default case takes no chan");
        }
        Json.Value bodyVal = c.opt("body");
        if (!(bodyVal instanceof Json.Object bodyObj)) {
            throw new FrontendException(cpath + ": body must be a \"block\" node");
        }
        GoStmtNode body = factory.buildStmt(bodyObj, ctx);
        if (!(body instanceof BlockNode)) {
            throw new FrontendException(cpath + ": body must be a \"block\" node");
        }
        return new SelectCaseNode(chanExpr, valueExpr, body, valueAssign, okAssign, isDefault, isSend);
    }

    /**
     * Parses a recv case's {@code targets} into pre-built assignment
     * statements: {@code [valueAssign, okAssign]} (nulls when absent).
     * Each entry is an objid; the variable must be declared (the
     * exporter emits a dominating {@code vardecl} for every target,
     * including {@code :=} variables and blank temps) and its type is
     * read from the wire {@code objects} array.
     */
    private static GoStmtNode[] buildTargetAssigns(Json.Object c, String cpath,
            NodeFactory.BuildCtx ctx) {
        GoStmtNode valueAssign = null;
        GoStmtNode okAssign = null;
        Json.Value tv = c.opt("targets");
        if (tv == null) {
            return new GoStmtNode[]{null, null};
        }
        if (!(tv instanceof Json.Array arr)) {
            throw new FrontendException(cpath + ": targets must be an array of objids");
        }
        if (arr.size() > 2) {
            throw new FrontendException(cpath + ": at most 2 targets (value, ok)");
        }
        for (int i = 0; i < arr.size(); i++) {
            String tpath = cpath + ".targets[" + i + "]";
            if (!(arr.get(i) instanceof Json.Num num)) {
                throw new FrontendException(tpath + ": target must be an objid number");
            }
            int obj = (int) num.asLong(tpath);
            Integer slot = ctx.objSlots.get(obj);
            if (slot == null) {
                throw new FrontendException(tpath + ": target obj " + obj
                        + " is not declared in the current function scope;"
                        + " the exporter must declare every select target (including"
                        + " := variables and blank temps) with a vardecl dominating"
                        + " the select (ADR-0005 Appendix C)");
            }
            if (ctx.boxed.contains(obj)) {
                throw new FrontendException(tpath + ": boxed target obj " + obj
                        + " not supported; the exporter must desugar through a plain temporary");
            }
            int typeId = objTypeId(ctx, obj, tpath);
            GoKind targetKind = ChanWire.kindOf(ctx, typeId, tpath);
            boolean wantOk = i == 1;
            if (wantOk && targetKind != GoKind.BOOL && targetKind != GoKind.UNTYPED_BOOL) {
                throw new FrontendException(tpath + ": ok target must be bool, got " + targetKind);
            }
            GoStmtNode assign = wantOk
                    ? new AssignNode(slot, targetKind, new SelectOkNode())
                    : new AssignNode(slot, targetKind, new SelectValueNode(targetKind));
            if (wantOk) {
                okAssign = assign;
            } else {
                valueAssign = assign;
            }
        }
        return new GoStmtNode[]{valueAssign, okAssign};
    }

    /** The wire {@code objects} array's {@code "type"} for an objid. */
    private static int objTypeId(NodeFactory.BuildCtx ctx, int obj, String path) {
        if (obj < 0 || obj >= ctx.objects.size()
                || !(ctx.objects.get(obj) instanceof Json.Object o)) {
            throw new FrontendException(path + ": objid " + obj + " not in objects array");
        }
        return o.intValue("type", path);
    }

    /**
     * The value received by the immediately preceding
     * {@link ChanOps#selectGo} on this goroutine, adapted to the target's
     * slot kind (the AssignNode dispatches on it).
     */
    static final class SelectValueNode extends GoExprNode {
        SelectValueNode(GoKind kind) {
            super(kind);
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return ChanOps.selectRecvValue();
        }

        @Override
        public long executeLong(VirtualFrame frame) {
            return ((Number) ChanOps.selectRecvValue()).longValue();
        }

        @Override
        public int executeInt(VirtualFrame frame) {
            return ((Number) ChanOps.selectRecvValue()).intValue();
        }

        @Override
        public double executeDouble(VirtualFrame frame) {
            return ((Number) ChanOps.selectRecvValue()).doubleValue();
        }

        @Override
        public float executeFloat(VirtualFrame frame) {
            return ((Number) ChanOps.selectRecvValue()).floatValue();
        }

        @Override
        public boolean executeBoolean(VirtualFrame frame) {
            return (Boolean) ChanOps.selectRecvValue();
        }
    }

    /** The ok bit received by the immediately preceding {@code selectGo}. */
    static final class SelectOkNode extends GoExprNode {
        SelectOkNode() {
            super(GoKind.BOOL);
        }

        @Override
        public Object executeObject(VirtualFrame frame) {
            return ChanOps.selectRecvOk();
        }

        @Override
        public boolean executeBoolean(VirtualFrame frame) {
            return ChanOps.selectRecvOk();
        }
    }
}
