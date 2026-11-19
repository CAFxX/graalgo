package graalgo.go.runtime;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.nodes.Node;
import graalgo.go.types.GoType;
import graalgo.go.types.Method;
import graalgo.go.types.TypeTable;
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
    public final TypeTable types;
    /** The {@code uintptr} handle table of this process (GG-0062). */
    public final Handles handles = new Handles();
    /**
     * {@code sync}'s pool cleanup ({@code runtime_registerPoolCleanup}), run by {@code runtime.GC}
     * as gc runs it at the start of every collection; null until {@code sync} initializes.
     */
    public volatile graalgo.go.values.Closure poolCleanup;

    public GoProcess(Flat[] globals, SchedulerCore sched, OutputStream err, TypeTable types) {
        this.globals = globals;
        this.sched = sched;
        this.err = err;
        this.types = types;
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
        } catch (Fatal f) {
            // Reported where it was thrown; the process is exiting.
        }
    }

    /** Unwinds the goroutine after a fatal error was reported: no deferred call runs (gc throw). */
    public static final class Fatal extends com.oracle.truffle.api.nodes.ControlFlowException {
        private static final long serialVersionUID = 1L;
    }

    /**
     * gc's {@code throw}: {@code fatal error: text}, the goroutine header, exit status 2. Not a
     * panic — nothing recovers it and no deferred call runs.
     */
    @TruffleBoundary
    public Fatal fatal(String text) {
        writeErr(("fatal error: " + text + "\n\ngoroutine " + sched.currentGoroutine().id + " [running]:\n")
                .getBytes(StandardCharsets.UTF_8));
        sched.requestExit(2);
        return new Fatal();
    }

    private void fatalPanic(GoPanic top) {
        // gc preprintpanics: every Error()/String() runs before anything prints; a method that
        // panics turns the report into the throw "panic while printing panic value".
        String chain;
        try {
            chain = renderChain(top);
        } catch (GoPanic inner) {
            writeErr(("fatal error: panic while printing panic value: " + throwValueText(inner.value)
                    + "\n\ngoroutine " + sched.currentGoroutine().id + " [running]:\n")
                    .getBytes(StandardCharsets.UTF_8));
            sched.requestExit(2);
            return;
        }
        StringBuilder sb = new StringBuilder(chain);
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
    private String renderChain(GoPanic top) {
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
                    continue; // gc: the re-raise of a repanicked value is not printed
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

    /**
     * gc printpanicval: {@code Error()} for a value that implements {@code error},
     * {@code String()} for a {@code Stringer} — satisfaction by name and signature, as gc's
     * type switch decides it — else by kind; any other value prints as its type and the
     * address of its data word, which the test normalizer masks.
     */
    private String renderValue(Object value) {
        Iface v = (Iface) value;
        if (v.data instanceof GoErrorValue e) {
            return indent(e.errorString());
        }
        for (GoType iface : new GoType[] {types.error, types.stringer}) {
            graalgo.go.types.Itab itab = v.type.itab(iface);
            if (itab != null) {
                Method m = itab.methods[0];
                Object s = m.target().call(new Object[] {null, Receivers.adapt(m, v.data, null)});
                return indent(s.toString());
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
        return "(" + t.str() + ") " + address(v.data);
    }

    /**
     * An address-like rendering of a data word, shaped like a gc heap pointer
     * ({@code 0xc000…}): the object's identity hash scaled to 16-byte granules; {@code 0x0}
     * for nil. Not a real address — the JVM has none to give — but distinct per object.
     */
    private static String address(Object data) {
        Object o = data instanceof graalgo.go.values.Ptr p ? p.obj : data;
        if (o == null) {
            return "0x0";
        }
        return "0x" + Long.toHexString(0xc000000000L | ((long) System.identityHashCode(o) << 4));
    }

    /**
     * gc preprintpanics' recover: a value of the predeclared type {@code string} prints as is,
     * any other value as {@code type <dynamic type>}.
     */
    private static String throwValueText(Object value) {
        Iface v = (Iface) value;
        if (v.data instanceof GoErrorValue e && e.typeName() != null) {
            return "type " + e.typeName();
        }
        if (!v.type.isNamed() && v.type.kind == GoType.STRING) {
            return v.data.toString();
        }
        return "type " + v.type.str();
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
