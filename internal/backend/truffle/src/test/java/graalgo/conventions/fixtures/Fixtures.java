package graalgo.conventions.fixtures;

import com.oracle.truffle.api.CompilerDirectives;
import com.oracle.truffle.api.CompilerDirectives.CompilationFinal;
import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.dsl.Cached;
import com.oracle.truffle.api.dsl.Idempotent;
import com.oracle.truffle.api.dsl.NeverDefault;
import com.oracle.truffle.api.dsl.Specialization;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.ExplodeLoop;
import com.oracle.truffle.api.nodes.Node;
import java.util.HashMap;
import java.util.Map;

/**
 * Planted violations of every rule of {@code NodeConventions} (NodeConventionsTest proves each
 * rule fires on its plant and stays silent on {@link Clean}). Never executed; the test reads the
 * class files only. Compiled without the DSL processor, so the DSL classes are annotations only.
 */
@SuppressWarnings("unused")
public final class Fixtures {
    private Fixtures() {
    }

    /** N1-field, N1-volatile. */
    public static final class BadFields extends Node {
        int count;
        volatile int flag;
    }

    /** N1-cfdims, N1-children. */
    public static final class BadArrays extends Node {
        @CompilationFinal private Object[] table;
        @Children private Node[] kids;
    }

    /** N1-sync. */
    public static final class Synchronized extends Node {
        synchronized void touch() {
        }
    }

    /** N2-switch: the execute method switches on the node's own configuration. */
    public static final class SwitchOnKind extends Node {
        private final int kind;

        SwitchOnKind(int kind) {
            this.kind = kind;
        }

        public int execute() {
            switch (kind) {
                case 0:
                    return 10;
                case 1:
                    return 20;
                case 2:
                    return 30;
                default:
                    return 40;
            }
        }
    }

    /** N4-explode: the exploded array's elements are not compilation constants. */
    public static final class ExplodePlain extends Node {
        private final int[] xs = {1, 2, 3};

        @ExplodeLoop
        public int execute() {
            int s = 0;
            for (int x : xs) {
                s += x;
            }
            return s;
        }
    }

    /** N4-unexploded: a loop over children without @ExplodeLoop. */
    public static final class LoopChildren extends Node {
        @Children private final Leaf[] kids = {};

        public int execute() {
            int s = 0;
            for (Leaf k : kids) {
                s += k.execute();
            }
            return s;
        }
    }

    public static final class Leaf extends Node {
        public int execute() {
            return 1;
        }
    }

    /** N8-typecase: a hand-written type dispatch. */
    public static final class TypeCase extends Node {
        public int execute(Object o) {
            if (o instanceof Integer) {
                return 1;
            } else if (o instanceof Long) {
                return 2;
            } else if (o instanceof String) {
                return 3;
            }
            return 0;
        }
    }

    /** S1-static; S2-static-read from {@link ReadsStatic}. */
    public static final class MutableStatic {
        static int counter;
    }

    public static final class ReadsStatic extends Node {
        public int execute() {
            return MutableStatic.counter;
        }
    }

    /** C1-final: no subclass, not final. */
    public static class NotFinal {
        void m() {
        }
    }

    /** R1-record: an immutable carrier with an accessor. */
    public static final class Pair {
        private final int a;
        private final int b;

        Pair(int a, int b) {
            this.a = a;
            this.b = b;
        }

        int a() {
            return a;
        }
    }

    /** D2-limit (value cache, guards, no limit), D4-idempotent (isSmall), D6-neverdefault (make). */
    public abstract static class Dsl extends Node {
        public abstract int execute(int x);

        @Specialization(guards = {"x == cachedX", "isSmall(x)"})
        int cached(int x, @Cached("x") int cachedX, @Cached("make()") Object made) {
            return cachedX;
        }

        static boolean isSmall(int x) {
            return x < 10;
        }

        static Object make() {
            return new Object();
        }
    }

    /** B2-frame. */
    public static final class FrameBoundary extends Node {
        @TruffleBoundary
        static int slow(VirtualFrame frame) {
            return 0;
        }
    }

    /** B3-boundary: a JDK collection and string formatting on the fast path. */
    public static final class Hostile extends Node {
        private final Map<String, Integer> m = new HashMap<>();

        public Object execute(String k) {
            Integer v = m.get(k);
            return v == null ? String.format("%s", k) : v;
        }
    }

    /** B6-invalidate: a compilation-final write without transferToInterpreterAndInvalidate. */
    public static final class WritesFinal extends Node {
        @CompilationFinal private int seen;

        public int execute(int x) {
            if (seen == 0) {
                seen = x;
            }
            return seen;
        }
    }

    /** Conforms to every rule: the test asserts no finding names it. */
    public static final class Clean extends Node {
        @Children private final Leaf[] kids = {};
        @CompilationFinal(dimensions = 1) private final int[] table = {1};
        @CompilationFinal private int seen;
        private final Map<String, Integer> m = new HashMap<>();
        static final int LIMIT = 3;

        @ExplodeLoop
        public int execute(VirtualFrame frame, String k) {
            int s = table[0];
            for (Leaf l : kids) {
                s += l.execute();
            }
            if (seen == 0) {
                CompilerDirectives.transferToInterpreterAndInvalidate();
                seen = s + String.valueOf(s).length();
            }
            return s + lookup(k);
        }

        @TruffleBoundary
        private int lookup(String k) {
            return m.getOrDefault(k, LIMIT);
        }
    }

    /** Conforming DSL: explicit limit, annotated helpers. */
    public abstract static class CleanDsl extends Node {
        public abstract int execute(int x);

        @Specialization(guards = {"x == cachedX", "ok(x)"}, limit = "3")
        int cached(int x, @Cached("x") int cachedX, @Cached("make()") Object made) {
            return cachedX;
        }

        @Idempotent
        static boolean ok(int x) {
            return x < 10;
        }

        @NeverDefault
        static Object make() {
            return new Object();
        }
    }
}
