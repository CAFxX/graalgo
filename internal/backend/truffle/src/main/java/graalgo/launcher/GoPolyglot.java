package graalgo.launcher;

import graalgo.language.GoLanguage;
import java.io.File;
import java.nio.charset.StandardCharsets;
import java.nio.file.Path;
import java.util.List;
import org.graalvm.polyglot.Context;
import org.graalvm.polyglot.Source;
import org.graalvm.polyglot.io.ByteSequence;

/**
 * The launchers' polyglot embedding of the Go language (truffle-design §2,
 * TR-1): one {@link Context} per program run, the frontend/v1 wire document
 * as a byte {@link Source}. {@code Context.parse} builds the AST (the
 * {@code --check} step), {@code Context.eval} runs the program and returns
 * its exit status.
 */
public final class GoPolyglot {
    private GoPolyglot() {}

    /**
     * A context for one program run.
     *
     * @param gomaxprocs initial GOMAXPROCS ({@code <= 0}: one per CPU)
     * @param argv the program's {@code os.Args} ({@code argv[0]} absolute)
     * @param sourceFiles the Go files of the wire document's file table, for
     *     positions; empty for a run from a stored wire document
     */
    public static Context newContext(int gomaxprocs, String[] argv, List<Path> sourceFiles) {
        StringBuilder files = new StringBuilder();
        for (Path p : sourceFiles) {
            if (files.length() > 0) {
                files.append(File.pathSeparator);
            }
            files.append(p.toString());
        }
        return Context.newBuilder(GoLanguage.ID)
                // Goroutines are polyglot threads (§2.6).
                .allowCreateThread(true)
                // Virtual polyglot threads are experimental upstream; the
                // first one would print a warning on the program's stderr.
                // The switch itself is an experimental engine option.
                .allowExperimentalOptions(true)
                .option("engine.WarnVirtualThreadSupport", "false")
                .option("go.Gomaxprocs", Integer.toString(Math.max(0, gomaxprocs)))
                .option("go.SourceFiles", files.toString())
                .arguments(GoLanguage.ID, argv)
                .in(System.in)
                .out(System.out)
                .err(System.err)
                .build();
    }

    /** The program as a byte source of the frontend/v1 wire MIME type. */
    public static Source wireSource(String json, String name) {
        return Source.newBuilder(GoLanguage.ID,
                ByteSequence.create(json.getBytes(StandardCharsets.UTF_8)), name)
                .mimeType(GoLanguage.WIRE_MIME)
                .buildLiteral();
    }
}
