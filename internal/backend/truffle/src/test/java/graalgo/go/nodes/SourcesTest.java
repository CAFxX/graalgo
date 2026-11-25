package graalgo.go.nodes;

import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.frame.FrameSlotKind;
import com.oracle.truffle.api.instrumentation.InstrumentableNode;
import com.oracle.truffle.api.instrumentation.StandardTags;
import com.oracle.truffle.api.source.Source;
import com.oracle.truffle.api.source.SourceSection;
import graalgo.go.nodes.N.Expr;
import graalgo.go.nodes.N.Stmt;
import graalgo.go.runtime.Positions;
import graalgo.go.runtime.Stacks;

/**
 * Source sections, root names and instrumentation tags of the new node package (WP-TR-AUDIT-6,
 * truffle-design §6.2): a statement's and a call's section come from their function's position
 * table, a function root has gc's name and its declaration's section, and the tags sit where
 * tools look for them — and nowhere else. The end-to-end checks are test/m1r/tools.sh (the
 * Truffle tools on a running program) and the traceback/callers programs of test/m1a and
 * test/m1r.
 */
public final class SourcesTest {
    private static final FrameDescriptor FD;
    private static final int SLOT;
    private static final Source FILE = source("/src/m/main.go");
    private static final Source OTHER = source("/src/m/gen.go");

    static {
        FrameDescriptor.Builder b = FrameDescriptor.newBuilder();
        SLOT = b.addSlot(FrameSlotKind.Long, null, null);
        FD = b.build();
    }

    private static Source source(String name) {
        return Source.newBuilder("go", "", name).content(Source.CONTENT_NONE).build();
    }

    /** Three positions: main.go:10:2, main.go:12:5 (4 long), gen.go:3:1; declared at main.go:9. */
    private static Positions positions(String name, boolean wrapper) {
        return new Positions(name, wrapper, () -> new Positions.Table(FILE, 9, new Source[] {FILE, FILE, OTHER},
                new int[] {10, 12, 3}, new int[] {2, 5, 1}, new int[] {1, 4, 1}));
    }

    private static Stmt stmt(int pos) {
        Stmt s = new N.WriteI64(SLOT, new N.ConstI64(1));
        s.setPos(pos);
        return s;
    }

    private static Calls.FuncRoot root(Positions ps, Stmt body) {
        Calls.FuncRoot r = new Calls.FuncRoot(null, FD, ps, new Stmt[0], body, null);
        r.adoptChildren();
        return r;
    }

    private static void eq(Object got, Object want, String what) {
        if (!java.util.Objects.equals(got, want)) {
            throw new AssertionError(what + ": got " + got + ", want " + want);
        }
    }

    private static void check(boolean cond, String what) {
        if (!cond) {
            throw new AssertionError(what);
        }
    }

    public void testRootNamesAndDeclaration() {
        Calls.FuncRoot r = root(positions("main.(*T).M", false), stmt(1));
        eq(r.getName(), "main.(*T).M", "name");
        eq(r.getQualifiedName(), "main.(*T).M", "qualified name");
        SourceSection decl = r.getSourceSection();
        eq(decl.getSource().getName(), "/src/m/main.go", "declaration file");
        eq(decl.getStartLine(), 9, "declaration line");
        check(!r.isInternal(), "a function is not internal");
        check(root(positions("main.T.M-fm", true), stmt(1)).isInternal(), "a wrapper is internal");
    }

    public void testStatementSections() {
        Stmt a = stmt(2);
        Stmt b = stmt(3);
        root(positions("main.f", false), new Ctl.Block(new Stmt[] {a, b}));
        SourceSection s = a.getSourceSection();
        eq(s.getStartLine(), 12, "line");
        eq(s.getStartColumn(), 5, "column");
        eq(s.getEndColumn(), 8, "end column");
        eq(s.getSource(), FILE, "source");
        eq(b.getSourceSection().getSource(), OTHER, "a //line position keeps its own file");
        eq(stmt(0).getSourceSection(), null, "unpositioned");
        Stmt orphan = stmt(1);
        eq(orphan.getSourceSection(), null, "outside a positioned root");
    }

    public void testTags() {
        Stmt s = stmt(1);
        Calls.Call call = new Calls.Call(() -> null, new Expr[0]);
        call.setPos(2);
        Calls.Call bare = new Calls.Call(() -> null, new Expr[0]);
        Calls.FuncRoot r = root(positions("main.f", false),
                new Ctl.Block(new Stmt[] {s, new N.WriteRef(SLOT, call), new N.WriteRef(SLOT, bare)}));
        check(s.isInstrumentable() && s.hasTag(StandardTags.StatementTag.class), "a statement is a StatementTag");
        check(!s.hasTag(StandardTags.CallTag.class) && !s.hasTag(StandardTags.RootTag.class), "only StatementTag");
        check(!stmt(0).isInstrumentable(), "an unpositioned statement is not instrumentable");
        check(call.isInstrumentable() && call.hasTag(StandardTags.CallTag.class), "a call is a CallTag");
        check(!call.hasTag(StandardTags.StatementTag.class), "a call is not a statement");
        eq(call.getSourceSection().getStartLine(), 12, "call line");
        check(!bare.isInstrumentable(), "an unpositioned call is not instrumentable");
        check(!new N.ConstRef(null).isInstrumentable(), "a non-call Ref is not instrumentable");
        // The body node is the function to tools.
        Stmt body = (Stmt) r.getChildren().iterator().next();
        check(body instanceof Calls.Body, "the root's first child is its Body");
        check(body.isInstrumentable() && body.hasTag(StandardTags.RootTag.class)
                && body.hasTag(StandardTags.RootBodyTag.class) && !body.hasTag(StandardTags.StatementTag.class),
                "Body carries RootTag and RootBodyTag");
        eq(body.getSourceSection().getStartLine(), 9, "Body section is the declaration");
        // Wrappers are generated for the instrumentable families.
        check(s.createWrapper(null) instanceof InstrumentableNode.WrapperNode w && w.getDelegateNode() == s,
                "statement wrapper");
        check(call.createWrapper(null) instanceof InstrumentableNode.WrapperNode w2 && w2.getDelegateNode() == call,
                "call wrapper");
    }

    public void testStackPositions() {
        N.ConstI64 inner = new N.ConstI64(1);
        Stmt s = new N.WriteI64(SLOT, inner);
        s.setPos(2);
        Calls.Call call = new Calls.Call(() -> null, new Expr[0]);
        call.setPos(3);
        Stmt holder = new N.WriteRef(SLOT, call);
        holder.setPos(1);
        root(positions("main.f", false), new Ctl.Block(new Stmt[] {s, holder}));
        eq(Stacks.posAt(inner), 2, "a node inside a statement is at the statement");
        eq(Stacks.posAt(call), 3, "a call is at its own position, not its statement's");
        eq(Stacks.posAt(null), 0, "no node");
        Positions ps = positions("main.f", false);
        eq(ps.line(0), 9, "position 0 is the declaration");
        eq(ps.file(3), "/src/m/gen.go", "file of a //line position");
        check(!ps.valid(4) && !ps.valid(0) && ps.valid(3), "valid range");
    }
}
