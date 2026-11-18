package graalgo.go.runtime;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.nodes.Node;
import graalgo.go.types.GoType;
import graalgo.go.types.Method;
import graalgo.go.values.Flat;
import graalgo.go.values.Iface;
import graalgo.go.values.Str;
import graalgo.jfr.ParkReason;
import graalgo.runtime.GoErrorValue;
import graalgo.sched.SchedulerCore;
import java.io.IOException;
import java.io.OutputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;

/**
 * One run of a bundle inside a Go context (truffle-design §2.4, context-level state): the
 * package-level variable storage and the narrow adapter onto the existing scheduler — goroutine
 * start, the fatal-panic report, and the standard error stream of {@code print}.
 */
public final class GoProcess {
    /** Package-level variables, one flat object per package (indexed by package unit). */
    public final Flat[] globals;
    private final SchedulerCore sched;
    private final OutputStream err;

    public GoProcess(Flat[] globals, SchedulerCore sched, OutputStream err) {
        this.globals = globals;
        this.sched = sched;
        this.err = err;
    }

    @TruffleBoundary
    public void writeErr(byte[] bytes) {
        try {
            err.write(bytes);
            err.flush();
        } catch (IOException e) {
            throw new IllegalStateException("stderr write failed", e);
        }
    }

    /** The {@code go} statement: a new goroutine calling {@code target(args)}. */
    @TruffleBoundary
    public void go(RootCallTarget target, Object[] args) {
        sched.spawn(() -> run(() -> target.call(args)));
    }

    /**
     * Runs a goroutine body. A panic that reaches the goroutine's top is fatal to the process
     * (gc fatalpanic): the panic chain and a goroutine header go to stderr, exit status 2.
     */
    @TruffleBoundary
    public void run(Runnable body) {
        try {
            body.run();
        } catch (GoPanic p) {
            fatalPanic(p);
        }
    }

    private void fatalPanic(GoPanic top) {
        StringBuilder sb = new StringBuilder(renderChain(top));
        // gc prints the signal line whenever the goroutine took the signal, even if a later
        // panic replaced the one it raised.
        for (GoPanic p = top; p != null; p = p.link) {
            if (p.value instanceof Iface i && i.data instanceof GoErrorValue e && e.signalSegv()) {
                sb.append("[signal SIGSEGV: segmentation violation code=0x1 addr=0x0 pc=0x0]\n");
                break;
            }
        }
        sb.append("\ngoroutine ").append(sched.currentGoroutine().id).append(" [running]:\n");
        writeErr(sb.toString().getBytes(StandardCharsets.UTF_8));
        sched.requestExit(2);
    }

    /** gc printpanics: oldest first, later panics indented, recovery annotations. */
    private static String renderChain(GoPanic top) {
        List<GoPanic> chain = new ArrayList<>();
        for (GoPanic p = top; p != null; p = p.link) {
            chain.add(p);
        }
        int n = chain.size();
        boolean[] repanicked = new boolean[n];
        String[] text = new String[n];
        for (int i = 0; i < n; i++) {
            if (i + 1 < n && chain.get(i + 1).value == chain.get(i).value) {
                repanicked[i + 1] = true;
                continue;
            }
            text[i] = renderValue(chain.get(i).value);
        }
        StringBuilder sb = new StringBuilder();
        for (int i = n - 1; i >= 0; i--) {
            if (i < n - 1) {
                if (repanicked[i + 1]) {
                    break;
                }
                sb.append('\t');
            }
            GoPanic p = chain.get(i);
            sb.append("panic: ").append(text[i]);
            if (p.recovered && repanicked[i]) {
                sb.append(" [recovered, repanicked]");
            } else if (p.recovered) {
                sb.append(" [recovered]");
            }
            sb.append('\n');
        }
        return sb.toString();
    }

    /** gc printpanicval: Error()/String() for error and Stringer values, else by kind. */
    private static String renderValue(Object value) {
        Iface v = (Iface) value;
        if (v.data instanceof GoErrorValue e) {
            return indent(e.errorString());
        }
        for (String name : new String[] {"Error", "String"}) {
            Method m = v.type.findMethod(name);
            if (m != null) {
                Object s = m.target().call(new Object[] {null, Receivers.adapt(m, v.data, null)});
                if (s instanceof Str str) {
                    return indent(str.toString());
                }
            }
        }
        GoType t = v.type;
        GoType u = t.u();
        String s;
        if (u.kind == GoType.STRING) {
            s = indent(v.data.toString());
            return t.isNamed() ? t.str() + "(\"" + s + "\")" : s;
        } else if (u.kind == GoType.BOOL || u.isInteger() || u.isFloat() || u.isComplex()) {
            s = new String(Values.format(new GoType[] {u}, new Object[] {v.data}, false), StandardCharsets.UTF_8);
            return t.isNamed() ? t.str() + "(" + s + ")" : s;
        }
        return "(" + t.str() + ") 0xADDR";
    }

    private static String indent(String s) {
        return s.replace("\n", "\n\t");
    }

    /** Blocks the current goroutine forever (operations on nil channels). */
    @TruffleBoundary
    public void blockForever(String waitReason) {
        for (;;) {
            sched.parkCurrent(null, ParkReason.CHANNEL, waitReason);
        }
    }

    /** The process of the context entered on this thread. */
    public static GoProcess get(Node node) {
        return graalgo.language.GoContext.get(node).process();
    }
}
