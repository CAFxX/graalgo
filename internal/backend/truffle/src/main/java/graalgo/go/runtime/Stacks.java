package graalgo.go.runtime;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.Truffle;
import com.oracle.truffle.api.TruffleStackTrace;
import com.oracle.truffle.api.TruffleStackTraceElement;
import com.oracle.truffle.api.frame.FrameInstance;
import com.oracle.truffle.api.nodes.Node;
import com.oracle.truffle.api.nodes.RootNode;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Go call stacks from Truffle frames (WP-TR-AUDIT-6, BUG-0047): {@code runtime.Callers}, the
 * program counters of {@code runtime.Caller}/{@code CallersFrames}/{@code FuncForPC}, and the
 * frames of a fatal panic's traceback. Everything here walks or decodes a stack: it runs behind
 * {@link TruffleBoundary}, never on a compiled fast path.
 *
 * <p>A Go frame is the frame of a root implementing {@link Positions.Root} (a function, a
 * range-over-func body) that is not a generated wrapper; intrinsic roots and thunks are not Go
 * frames (gc's assembly stubs and elided wrappers). A frame's position is its call node's (or
 * the panicking node's), found by walking up to the nearest node with one: a call, else its
 * statement; a frame with neither reports the function's declaration line.
 *
 * <p>Program counters (one table per process): {@code BASE + (id << SHIFT) + (pos << 2)}, where
 * {@code id} numbers the functions that ever appeared in a stack and {@code pos} is the
 * position index; the function's entry is position 0. A "return pc" as {@code Callers} reports
 * it is the call's pc plus one, which decodes to the same position. The range lies far below
 * the {@link Handles} heap range, so {@code FuncForPC} also resolves a func value's
 * {@code uintptr} handle ({@code reflect.Value.Pointer}).
 */
public final class Stacks {
    static final long BASE = 0x400000L;
    static final int SHIFT = 20;
    static final int MAX_POS = (1 << (SHIFT - 2)) - 1;

    /** One function of the pc table. */
    public record Fn(int id, String name, Positions positions) {
        public long entry() {
            return BASE + ((long) id << SHIFT);
        }
    }

    /** A Go frame: the function and the position it is at. */
    public record Frame(Fn fn, int pos) {
        public String file() {
            return fn.positions.file(pos);
        }

        public int line() {
            return fn.positions.line(pos);
        }

        /** The pc of the position (the call instruction's, for a caller frame). */
        public long pc() {
            return fn.entry() + ((long) (pos <= MAX_POS ? pos : 0) << 2);
        }
    }

    private final ConcurrentHashMap<String, Fn> byName = new ConcurrentHashMap<>();
    private final List<Fn> byId = new ArrayList<>();

    /** The table entry of the Go function {@code name} with {@code positions}. */
    @TruffleBoundary
    public Fn fn(String name, Positions positions) {
        Fn f = byName.get(name);
        if (f != null) {
            return f;
        }
        synchronized (byId) {
            return byName.computeIfAbsent(name, n -> {
                Fn e = new Fn(byId.size(), n, positions);
                byId.add(e);
                return e;
            });
        }
    }

    /** The function containing {@code pc}, or null. */
    @TruffleBoundary
    public Fn fnAt(GoProcess process, long pc) {
        if (pc >= BASE && pc < Handles.FIRST) {
            long id = (pc - BASE) >>> SHIFT;
            synchronized (byId) {
                return id < byId.size() ? byId.get((int) id) : null;
            }
        }
        // A func value's uintptr (internal/abi.FuncPCABI0 and reflect): its call target.
        Object[] hit = process.handles.resolve(pc);
        if (hit != null && hit[0] instanceof RootCallTarget t) {
            Frame f = frameOf(t.getRootNode(), null);
            return f == null ? null : f.fn;
        }
        return null;
    }

    /** The frame {@code pc} names ({@link #fnAt} and its position), or null. */
    @TruffleBoundary
    public Frame frameAt(GoProcess process, long pc) {
        Fn f = fnAt(process, pc);
        if (f == null) {
            return null;
        }
        int pos = pc >= BASE && pc < Handles.FIRST ? (int) (((pc - BASE) & ((1L << SHIFT) - 1)) >>> 2) : 0;
        return new Frame(f, f.positions.valid(pos) ? pos : 0);
    }

    /** The Go frame of {@code root} at {@code location} (a node of root's tree, or null), or null. */
    private Frame frameOf(RootNode root, Node location) {
        if (!(root instanceof Positions.Root pr)) {
            return null;
        }
        Positions ps = pr.positions();
        if (ps == null || ps.wrapper) {
            return null;
        }
        int pos = posAt(location);
        if (pos == 0 && location != null) {
            // A call (or panic) by the root itself: a deferred call at the function's exit.
            pos = pr.exitPos();
        }
        return new Frame(fn(root.getName(), ps), pos);
    }

    /** The position of {@code n}: its own, else its nearest positioned ancestor's in the same root. */
    public static int posAt(Node n) {
        for (Node c = n; c != null && !(c instanceof RootNode); c = c.getParent()) {
            if (c instanceof Positions.At a && a.pos() > 0) {
                return a.pos();
            }
        }
        return 0;
    }

    /**
     * The Go frames of the running goroutine, innermost first, after skipping {@code skip} of
     * them, at most {@code max}. The innermost frame is the Go caller of the running intrinsic
     * (the intrinsic's own frame is not a Go frame).
     */
    @TruffleBoundary
    public List<Frame> current(int skip, int max) {
        List<Frame> out = new ArrayList<>();
        int[] toSkip = {skip};
        Truffle.getRuntime().iterateFrames((FrameInstance fi) -> {
            if (!(fi.getCallTarget() instanceof RootCallTarget t)) {
                return null;
            }
            Frame f = frameOf(t.getRootNode(), fi.getCallNode());
            if (f == null) {
                return null;
            }
            if (toSkip[0] > 0) {
                toSkip[0]--;
                return null;
            }
            out.add(f);
            return out.size() >= max ? Boolean.TRUE : null;
        });
        return out;
    }

    /** The Go frames {@code panic} unwound through, innermost (the panic site) first. */
    @TruffleBoundary
    public List<Frame> of(Throwable panic) {
        List<Frame> out = new ArrayList<>();
        List<TruffleStackTraceElement> trace = TruffleStackTrace.getStackTrace(panic);
        if (trace == null) {
            return out;
        }
        for (TruffleStackTraceElement e : trace) {
            Frame f = frameOf(e.getTarget().getRootNode(), e.getLocation());
            if (f != null) {
                out.add(f);
            }
        }
        return out;
    }

    /**
     * gc's traceback of one goroutine's frames: {@code pkg.f(...)} then a tab, the file, the
     * line and a pc offset. Argument words are not recorded ({@code ...}, gc's inlined-frame
     * form); the offset is the position's, in gc's {@code +0x} form. Frames of package
     * {@code runtime} are elided, as gc elides them.
     */
    @TruffleBoundary
    public static void render(StringBuilder sb, List<Frame> frames) {
        for (Frame f : frames) {
            String name = f.fn.name;
            if (name.startsWith("runtime.")) {
                continue;
            }
            sb.append(name).append("(...)\n\t").append(f.file()).append(':').append(f.line()).append(" +0x")
                    .append(Long.toHexString(f.pc() - f.fn.entry())).append('\n');
        }
    }
}
