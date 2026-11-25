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
    /**
     * The caller location of a call the runtime makes from Java (a goroutine's entry, package
     * initialization, a method value called by the runtime): none. Calls from Java always pass a
     * location, {@code target.call(location, args)}: {@code CallTarget.call(Object...)} brackets
     * the call with the calling thread's {@code EncapsulatingNodeReference} (set on entry, restored
     * on exit), and in carrier mode a goroutine parked inside the call resumes on another carrier,
     * so the restore would write the first carrier's reference from the second (a Truffle assertion
     * under {@code -ea}, a cross-thread race without). NodeConventionsTest rule G1-calltarget
     * rejects {@code call(Object...)} in the new path and the shared runtime.
     */
    public static final Node NO_CALLER = null;

    /** Package-level variables, one flat object per package (indexed by package unit). */
    public final Flat[] globals;
    private final SchedulerCore sched;
    private final OutputStream err;
    public final TypeTable types;
    /** The {@code uintptr} handle table of this process (GG-0062). */
    public final Handles handles = new Handles();
    /** The pc table and stack walks of this process (runtime.Callers, tracebacks). */
    public final Stacks stacks = new Stacks();
    /**
     * {@code sync}'s pool cleanup ({@code runtime_registerPoolCleanup}), run by {@code runtime.GC}
     * as gc runs it at the start of every collection; null until {@code sync} initializes.
     */
    public volatile graalgo.go.values.Closure poolCleanup;
    /** {@code internal/godebug}'s update hook ({@code setUpdate}): called with the GODEBUG setting when it changes. */
    public volatile graalgo.go.values.Closure godebugUpdate;

    /** The link's {@code -X runtime.godebugDefault}: the program's default GODEBUG settings. */
    public final String godebugDefault;
    /** gc's {@code debug.panicnil}: 1 makes {@code panic(nil)} panic with a nil value. */
    private volatile int panicNil;

    public GoProcess(Flat[] globals, SchedulerCore sched, OutputStream err, TypeTable types, String godebugDefault) {
        this.globals = globals;
        this.sched = sched;
        this.err = err;
        this.types = types;
        this.godebugDefault = godebugDefault;
        this.panicNil = Godebug.startup("panicnil", godebugDefault, System.getenv("GODEBUG"));
    }

    public int panicNil() {
        return panicNil;
    }

    /** gc {@code reparsedebugvars}: the GODEBUG environment variable changed to {@code env}. */
    @TruffleBoundary
    public void godebugChanged(String env) {
        panicNil = Godebug.reparse("panicnil", godebugDefault, env, panicNil);
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
        sched.spawn(() -> run(() -> target.call(NO_CALLER, args)));
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
        StringBuilder sb = new StringBuilder("fatal error: ").append(text).append("\n\ngoroutine ")
                .append(sched.currentGoroutine().id).append(" [running]:\n");
        traceback(sb, stacks.current(0, MAX_FRAMES + 1));
        writeErr(sb.toString().getBytes(StandardCharsets.UTF_8));
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
        traceback(sb, stacks.of(top));
        writeErr(sb.toString().getBytes(StandardCharsets.UTF_8));
        sched.requestExit(2);
    }

    /** {@code runtime.Stack}'s text: the calling goroutine's header and frames, gc's format. */
    @TruffleBoundary
    public String currentStack() {
        StringBuilder sb = new StringBuilder("goroutine ").append(sched.currentGoroutine().id).append(" [running]:\n");
        traceback(sb, stacks.current(0, MAX_FRAMES + 1));
        return sb.toString();
    }

    /** gc's traceback frame limit ({@code tracebackInnerFrames + tracebackOuterFrames}). */
    static final int MAX_FRAMES = 100;

    /**
     * The frames of a goroutine's traceback, gc's format (WP-TR-AUDIT-6): at most
     * {@link #MAX_FRAMES}, then gc's elision line.
     */
    private static void traceback(StringBuilder sb, List<Stacks.Frame> frames) {
        if (frames.size() > MAX_FRAMES) {
            Stacks.render(sb, frames.subList(0, MAX_FRAMES));
            sb.append("...additional frames elided...\n");
        } else {
            Stacks.render(sb, frames);
        }
    }

    /**
     * gc's repanic test, {@code *efaceOf(&p.link.arg) == *efaceOf(&p.arg)}: the two interface
     * words are equal. The same boxed value is; so is a re-boxed pointer-shaped value (gc stores
     * it in the data word itself) with the same type and address — {@code panic(r.(*T))} after
     * {@code r := recover()}. Any other re-boxing gets a new data word under gc.
     */
    private static boolean sameEface(Object a, Object b) {
        if (a == b) {
            return true;
        }
        if (a instanceof Iface x && b instanceof Iface y && x.type.key.equals(y.type.key)) {
            int k = x.type.u().kind;
            if (k == GoType.POINTER || k == GoType.UNSAFE_POINTER || k == GoType.CHAN) {
                return Values.equal(x.type, x.data, y.data);
            }
        }
        return false;
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
            if (i + 1 < n && sameEface(chain.get(i + 1).value, chain.get(i).value)) {
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
        if (value == null) {
            return "nil"; // panic(nil) under GODEBUG=panicnil=1
        }
        Iface v = (Iface) value;
        if (v.data instanceof GoErrorValue e) {
            return indent(e.errorString());
        }
        for (GoType iface : new GoType[] {types.error, types.stringer}) {
            graalgo.go.types.Itab itab = v.type.itab(iface);
            if (itab != null) {
                Method m = itab.methods()[0];
                Object s = m.target().call(NO_CALLER, null, Receivers.adapt(m, v.data, null));
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
        Object o = data instanceof graalgo.go.values.Ptr p ? p.obj() : data;
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
        if (value == null) {
            return "nil";
        }
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
