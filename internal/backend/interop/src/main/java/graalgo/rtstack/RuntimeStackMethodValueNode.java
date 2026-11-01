package graalgo.rtstack;

import com.oracle.truffle.api.TruffleLanguage;
import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;

import graalgo.frontend.FrontendException;
import graalgo.frontend.GoExprNode;
import graalgo.frontend.Json;
import graalgo.frontend.NodeFactory;
import graalgo.runtime.GoFuncInfo;
import graalgo.safepoint.Safepoint;
import graalgo.values.GoFunc;
import graalgo.values.GoKind;

/**
 * A method value of a synthetic {@code runtime} stack-inspection method:
 * {@code fn.Name} or {@code fn.Entry} in value position, lowered by the
 * frontend to the {@code "rtstackmethodvalue"} wire node (P1-3).
 *
 * <p>Direct calls ({@code fn.Name()}) already lower to the
 * {@code "runtimefuncname"}/{@code "runtimefuncentry"} wire nodes, but
 * a method value cannot use them: there is no funcdecl to bind, and
 * the old frontend path crashed on a nil declaration (and, before
 * that, emitted a runtime panic claiming the PC-to-function table did
 * not exist — it does: {@link graalgo.runtime.StackTrace}, the same
 * interning {@code FuncForPC} resolves through). This node evaluates
 * the receiver once at formation time (Go method-value semantics) and
 * returns a {@link GoFunc} whose bound argument is that receiver; the
 * root below reads the bound receiver at invocation time and returns
 * the name or entry from its {@link GoFuncInfo}. A nil receiver
 * panics at invocation, not formation, exactly like a method value
 * formed on a nil pointer for an ordinary method.
 *
 * <p>Multi-result stack methods ({@code (*Func).FileLine},
 * {@code (*Frames).Next}) have no method-value lowering: the frontend
 * fails those with a named scope error at export time.
 *
 * <p>Registered on the factory via the public
 * {@code NodeFactory.registerExprKind} hook.
 */
public final class RuntimeStackMethodValueNode extends GoExprNode {

    @Child private GoExprNode recv;
    private final MethodValueRoot root;

    private RuntimeStackMethodValueNode(String op, GoExprNode recv) {
        super(GoKind.FUNC);
        this.recv = recv;
        this.root = new MethodValueRoot(op);
    }

    @Override
    public Object executeObject(VirtualFrame frame) {
        Object recvVal = recv.executeObject(frame);
        return new GoFunc(root.getCallTarget(), null, null, new Object[] {recvVal});
    }

    /**
     * Builds the node from the {@code "rtstackmethodvalue"} wire node.
     * Fields: {@code op} ("Name" or "Entry"), {@code recv} (the
     * {@code *runtime.Func} expression node), {@code type} (the method
     * value's static func type id — a func kind).
     */
    public static GoExprNode build(Json.Object node, NodeFactory.BuildCtx ctx,
                                   NodeFactory factory) {
        Json.Object f = node.object("fields", "rtstackmethodvalue");
        String path = "rtstackmethodvalue.fields";
        Json.Value ov = f.opt("op");
        if (!(ov instanceof Json.Str opStr)) {
            throw new FrontendException(path + ": rtstackmethodvalue needs an \"op\" string");
        }
        String op = opStr.value();
        if (!"Name".equals(op) && !"Entry".equals(op)) {
            throw new FrontendException(path + ": rtstackmethodvalue op must be \"Name\" or \"Entry\", got " + op);
        }
        Json.Value rv = f.opt("recv");
        if (!(rv instanceof Json.Object recvObj)) {
            throw new FrontendException(path + ": rtstackmethodvalue needs a \"recv\" node object");
        }
        Json.Value tv = f.opt("type");
        if (!(tv instanceof Json.Num num)) {
            throw new FrontendException(path + ": rtstackmethodvalue needs a numeric \"type\" id");
        }
        int typeId = (int) num.asLong(path + ".type");
        if (NodeFactory.kindOf(ctx, typeId, path) != GoKind.FUNC) {
            throw new FrontendException(
                    path + ": rtstackmethodvalue result must be a func kind");
        }
        return new RuntimeStackMethodValueNode(op, factory.buildExpr(recvObj, ctx));
    }

    /**
     * The call target behind a bound stack-method value. Calling
     * convention (see {@link graalgo.runtime.GoCalls#callArgs}):
     * {@code args[0]} is the {@link GoFunc} itself, {@code args[1]} is
     * the bound receiver, and there are no further arguments (Name
     * and Entry take none).
     */
    private static final class MethodValueRoot extends RootNode {
        private final String op;

        MethodValueRoot(String op) {
            super((TruffleLanguage<?>) null, FrameDescriptor.newBuilder().build());
            this.op = op;
        }

        @Override
        public Object execute(VirtualFrame frame) {
            Safepoint.poll();
            Object[] args = frame.getArguments();
            // GoCalls.callArgs always prepends the single bound receiver.
            GoFuncInfo info = RtStack.derefFunc(args[1], this);
            if ("Name".equals(op)) {
                return RtStack.goString(info.funcName());
            }
            return info.entry();
        }

        @Override
        public String getName() {
            return "runtime.Func." + op;
        }
    }
}
