import com.oracle.truffle.api.*;
import com.oracle.truffle.api.frame.*;
import com.oracle.truffle.api.nodes.*;

public final class Smoke {
    static final class ConstNode extends Node {
        Object run(VirtualFrame f) { return 42L; }
    }
    static final class Root extends RootNode {
        @Child ConstNode body = new ConstNode();
        Root() { super(null); }
        @Override public Object execute(VirtualFrame f) { return body.run(f); }
    }
    public static void main(String[] a) {
        RootNode r = new Root();
        Object v = r.getCallTarget().call();
        if (!v.equals(42L)) throw new AssertionError("got " + v);
        System.out.println("SMOKE OK: result=" + v
            + " runtime=" + Truffle.getRuntime().getName());
    }
}
