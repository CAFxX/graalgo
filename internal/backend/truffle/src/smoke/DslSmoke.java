import com.oracle.truffle.api.dsl.*;
import com.oracle.truffle.api.nodes.Node;

// Smoke fixture only: the node-object-inlining decision is irrelevant here,
// so the DSL processor's inlining-candidacy hint is suppressed at the source
// (real nodes in M1+ will make a deliberate @GenerateInline choice).
@SuppressWarnings("truffle-inlining")
abstract class AddNode extends Node {
    abstract Object execute(Object left, Object right);
    @Specialization
    long addLong(long l, long r) { return l + r; }
    @Specialization
    double addDouble(double l, double r) { return l + r; }
}
public final class DslSmoke {
    public static void main(String[] a) {
        AddNode n = AddNodeGen.create();
        Object r1 = n.execute(2L, 3L);
        Object r2 = n.execute(2.5, 3.5);
        if (!r1.equals(5L) || !r2.equals(6.0)) throw new AssertionError(r1 + "/" + r2);
        System.out.println("DSL OK: long=" + r1 + " double=" + r2);
    }
}
