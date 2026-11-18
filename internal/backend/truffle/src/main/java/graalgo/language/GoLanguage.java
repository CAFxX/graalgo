package graalgo.language;

import com.oracle.truffle.api.CallTarget;
import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.ContextThreadLocal;
import com.oracle.truffle.api.Option;
import com.oracle.truffle.api.TruffleLanguage;
import com.oracle.truffle.api.source.Source;
import graalgo.frontend.FrontendException;
import graalgo.frontend.NodeFactory;
import java.nio.charset.StandardCharsets;
import org.graalvm.options.OptionCategory;
import org.graalvm.options.OptionDescriptors;
import org.graalvm.options.OptionKey;
import org.graalvm.options.OptionStability;

/**
 * The Go language registration (SPEC GG-0005/0024; truffle-design.md §2,
 * work package TR-1): {@code Context.newBuilder("go")} evaluates a compiled
 * Go program.
 *
 * <p>{@link #parse} never sees Go source text: the frontend ({@code go/types}
 * in a Go process, GG-0012) already produced typed IR. Two byte MIME types
 * are registered:
 * <ul>
 *   <li>{@link #WIRE_MIME} — the v1 frontend/v1 JSON wire document (what
 *       {@code gofrontend} prints and {@code graalgo build} artifacts embed).
 *       Temporary: deleted with v1 at toolexec M3 (§2.2).
 *   <li>{@link #BUNDLE_MIME} — the linked bundle of the toolexec design
 *       (§4.2: per-package GIR), loaded by {@code graalgo.go.loader} and run
 *       by the new node package {@code graalgo.go} (toolexec W4).
 * </ul>
 *
 * <p>Evaluating a program source initializes and runs it (package init,
 * then {@code main.main} as the first goroutine) and returns the process
 * exit status as an {@code int}: 0 when {@code main} returns, 2 for an
 * uncaught panic or deadlock, {@code n} for {@code os.Exit(n)}.
 * {@code Context.parse} builds the AST without running it (the launchers'
 * {@code --check}).
 *
 * <p>Context policy is {@code EXCLUSIVE} until the context-ification work
 * package TR-5 moves the runtime's process-global statics (scheduler,
 * itab/type tables, FuncIds, GoOs) into {@link GoContext} (§2.4: "Until
 * TR-5 lands the language declares EXCLUSIVE"). Goroutine threads are
 * polyglot threads ({@link GoContext#newGoroutineThread}), so the context
 * requires {@code allowCreateThread(true)}.
 */
@TruffleLanguage.Registration(
        id = GoLanguage.ID,
        name = "Go",
        implementationName = "graalgo",
        version = "1.27.1",
        byteMimeTypes = {GoLanguage.WIRE_MIME, GoLanguage.BUNDLE_MIME},
        defaultMimeType = GoLanguage.WIRE_MIME,
        contextPolicy = TruffleLanguage.ContextPolicy.EXCLUSIVE,
        website = "https://go.dev")
public final class GoLanguage extends TruffleLanguage<GoContext> {
    public static final String ID = "go";
    /** frontend/v1 JSON wire document (temporary; deleted with v1). */
    public static final String WIRE_MIME = "application/x-graalgo-wire+json";
    /** Linked bundle (toolexec design §4.2); reserved. */
    public static final String BUNDLE_MIME = "application/x-graalgo-bundle";

    @Option(help = "Initial GOMAXPROCS (0: the default, as for an unset GOMAXPROCS).",
            category = OptionCategory.USER, stability = OptionStability.STABLE)
    static final OptionKey<Integer> Gomaxprocs = new OptionKey<>(0);

    @Option(help = "Go source files of the program, separated by the platform path "
            + "separator, in the order of the wire document's file table (positions "
            + "and stack traces). Empty: one placeholder source.",
            category = OptionCategory.USER, stability = OptionStability.STABLE)
    static final OptionKey<String> SourceFiles = new OptionKey<>("");

    @Option(help = "Goroutine carrier threads: 'virtual' (default) or 'platform' "
            + "(truffle-design §2.6 item 5).",
            category = OptionCategory.USER, stability = OptionStability.STABLE)
    static final OptionKey<String> GoroutineThreads = new OptionKey<>("virtual");

    private static final LanguageReference<GoLanguage> REFERENCE =
            LanguageReference.create(GoLanguage.class);

    /**
     * The thread's slot ({@code M} of §2.5): the goroutine state of the
     * goroutine currently running on the thread. One per (context, thread).
     */
    final ContextThreadLocal<GoThreadSlot> threadSlot =
            locals.createContextThreadLocal((context, thread) -> new GoThreadSlot());

    /** This thread's slot; a Go context must be entered on the current thread. */
    public GoThreadSlot threadSlot() {
        return threadSlot.get();
    }

    @Override
    protected GoContext createContext(Env env) {
        if (!env.isCreateThreadAllowed()) {
            throw new IllegalStateException("the go language needs to create goroutine "
                    + "threads: build the context with allowCreateThread(true)");
        }
        return new GoContext(this, env);
    }

    @Override
    protected void initializeContext(GoContext context) {
        GoContext.activate(context);
    }

    @Override
    protected void finalizeContext(GoContext context) {
        context.finalizeContext();
    }

    @Override
    protected void disposeContext(GoContext context) {
        GoContext.deactivate(context);
    }

    @Override
    protected boolean isThreadAccessAllowed(Thread thread, boolean singleThreaded) {
        // Go is multi-threaded from the first instruction (§2.6 item 1).
        return true;
    }

    @Override
    protected OptionDescriptors getOptionDescriptors() {
        return new GoLanguageOptionDescriptors();
    }

    @Override
    protected CallTarget parse(ParsingRequest request) {
        Source source = request.getSource();
        String mime = source.getMimeType();
        if (BUNDLE_MIME.equals(mime)) {
            if (!source.hasBytes()) {
                throw new GoParseException("a linked bundle (" + BUNDLE_MIME + ") is a byte source");
            }
            return new GoBundleRootNode(this, loadBundle(source)).getCallTarget();
        }
        if (!source.hasBytes()) {
            throw new GoParseException("the go language does not accept Go source text; "
                    + "build the program with go build / gofrontend and evaluate its "
                    + WIRE_MIME + " output");
        }
        GoContext context = GoContext.get(null);
        NodeFactory.Program program = buildProgram(context, source);
        return new GoProgramRootNode(this, program).getCallTarget();
    }

    @TruffleBoundary
    private graalgo.go.loader.Program loadBundle(Source source) {
        try {
            return graalgo.go.loader.Program.load(this, source.getBytes().toByteArray());
        } catch (IllegalArgumentException | graalgo.gir.GirFormatException e) {
            throw new GoParseException("malformed bundle: " + e.getMessage());
        }
    }

    private volatile graalgo.go.runtime.RuntimeTypes runtimeTypes;

    /**
     * The language-level type universe of the bundle path (truffle-design §2.4): the interning
     * table and the runtime's own types, shared by every bundle this language instance loads.
     */
    public graalgo.go.runtime.RuntimeTypes runtimeTypes() {
        graalgo.go.runtime.RuntimeTypes t = runtimeTypes;
        if (t == null) {
            synchronized (this) {
                t = runtimeTypes;
                if (t == null) {
                    t = new graalgo.go.runtime.RuntimeTypes(this);
                    runtimeTypes = t;
                }
            }
        }
        return t;
    }

    /** The language of the node's (or, for null, the current thread's) Go context. */
    public static GoLanguage get(com.oracle.truffle.api.nodes.Node node) {
        return REFERENCE.get(node);
    }

    @TruffleBoundary
    private static NodeFactory.Program buildProgram(GoContext context, Source source) {
        String json = new String(source.getBytes().toByteArray(), StandardCharsets.UTF_8);
        try {
            return GoProgramBuilder.build(json, context.sourceFiles());
        } catch (FrontendException e) {
            throw new GoParseException(e.getMessage());
        }
    }

    /**
     * The language of the Go context entered on the current thread, or
     * {@code null} when none is: root nodes built by unit tests and the
     * non-polyglot launchers run without a context and keep the
     * language-less root nodes they had before TR-1.
     */
    public static GoLanguage currentOrNull() {
        if (!GoContext.isEnteredOnCurrentThread()) {
            return null;
        }
        return REFERENCE.get(null);
    }
}
