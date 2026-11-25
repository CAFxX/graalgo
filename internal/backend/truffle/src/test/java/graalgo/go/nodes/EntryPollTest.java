package graalgo.go.nodes;

import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.FrameSlotKind;
import graalgo.go.nodes.N.Expr;
import graalgo.go.nodes.N.Jump;
import graalgo.go.nodes.N.Stmt;
import graalgo.go.runtime.Positions;

/**
 * The entry poll of a Go function (GG-SCHED-0001): present exactly when the body can call Go
 * code. Its absence for a call-free body is the optimization; its presence wherever a call
 * hides — nested in a loop, in the result expression, in a deferred or {@code go} call — is
 * what keeps every recursion cycle polling (test/m1r/programs/preempt_spin.go is the
 * end-to-end check).
 */
public final class EntryPollTest {
    private static final FrameDescriptor FD;
    private static final int SLOT;

    static {
        FrameDescriptor.Builder b = FrameDescriptor.newBuilder();
        SLOT = b.addSlot(FrameSlotKind.Long, null, null);
        FD = b.build();
    }

    private static Calls.FuncRoot root(Stmt body, Expr result) {
        return new Calls.FuncRoot(null, FD, new Positions("f", false, () -> null), new Stmt[0], body, result);
    }

    private static Stmt assign() {
        return new N.WriteI64(SLOT, new N.ConstI64(1));
    }

    private static Stmt loop(Stmt body) {
        return new Ctl.For(new N.ConstBool(false), body, null, new Jump(), new Jump());
    }

    private static Expr call() {
        return new Calls.Call(() -> null, new Expr[0]);
    }

    private static void check(boolean want, Calls.FuncRoot r, String what) {
        if (r.pollsAtEntry() != want) {
            throw new AssertionError(what + ": pollsAtEntry " + r.pollsAtEntry() + ", want " + want);
        }
    }

    public void testCallFreeBodiesDoNotPoll() {
        check(false, root(new Ctl.Block(new Stmt[] {assign()}), new N.ReadI64(SLOT)), "straight line");
        check(false, root(loop(assign()), null), "loop without calls");
    }

    public void testEveryCallSitePolls() {
        check(true, root(new Ctl.Block(new Stmt[0]), call()), "call in the result");
        check(true, root(loop(new Ctl.Block(new Stmt[] {assign(), new N.WriteRef(SLOT, call())})), null),
                "call nested in a loop body");
        check(true, root(new N.WriteRef(SLOT, new Calls.CallClosure(new N.ReadRef(SLOT), new Expr[0])), null),
                "func value call");
        check(true, root(new Calls.Go(new Calls.PrepStatic(SLOT, () -> null, new Expr[0])), null), "go statement");
    }
}
