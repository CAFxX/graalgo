package graalgo.go.runtime;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.source.Source;
import com.oracle.truffle.api.source.SourceSection;
import java.util.function.Supplier;

/**
 * The source positions of one Go function (truffle-design §6.2, WP-TR-AUDIT-6): its gc name and
 * the GIR body's position table, decoded on first use. Shared by the function's split copies
 * ({@code FuncRoot.cloneUninitialized} rebuilds the tree, not this). Nodes store only the
 * 1-based index of their position (0: none); a {@link SourceSection} is made on demand, off the
 * hot path: tools, stack walks and tracebacks ask for them, compiled code never does.
 *
 * <p>Sources carry no characters ({@link Source#CONTENT_NONE}): a section is a line and column
 * range, which is all a Go position records, and nothing reads the file.
 */
public final class Positions {
    /** gc's function name: {@code pkg.F}, {@code pkg.(*T).M}, {@code pkg.F.func1}. */
    public final String function;
    /** A generated wrapper (gc's {@code WRAPPER}): internal to tools, elided from tracebacks. */
    public final boolean wrapper;
    private final Supplier<Table> decoder;
    private volatile Table table;

    /**
     * A decoded position table: entry {@code i} (0-based, position {@code i + 1}) is in file
     * {@code sources[i]} at {@code lines[i]}, {@code cols[i]}, {@code lens[i]} characters long;
     * the function's declaration is {@code declSource}:{@code declLine}.
     */
    public record Table(Source declSource, int declLine, Source[] sources, int[] lines, int[] cols, int[] lens) {}

    public Positions(String function, boolean wrapper, Supplier<Table> decoder) {
        this.function = function;
        this.wrapper = wrapper;
        this.decoder = decoder;
    }

    @TruffleBoundary
    public Table table() {
        Table t = table;
        if (t == null) {
            t = decoder.get(); // idempotent: a race decodes twice, either result is the same
            table = t;
        }
        return t;
    }

    /** Whether {@code pos} names an entry of the table. */
    @TruffleBoundary
    public boolean valid(int pos) {
        return pos > 0 && pos <= table().lines.length;
    }

    /** The file of position {@code pos} (0: the declaration). */
    @TruffleBoundary
    public String file(int pos) {
        Table t = table();
        Source s = valid(pos) ? t.sources[pos - 1] : t.declSource;
        return s == null ? "" : s.getName();
    }

    /** The line of position {@code pos} (0: the declaration). */
    @TruffleBoundary
    public int line(int pos) {
        Table t = table();
        return valid(pos) ? t.lines[pos - 1] : t.declLine;
    }

    /** The section of position {@code pos}, or of the declaration line for 0. */
    @TruffleBoundary
    public SourceSection section(int pos) {
        Table t = table();
        if (!valid(pos)) {
            return t.declSource == null || t.declLine <= 0 ? null : t.declSource.createSection(t.declLine);
        }
        Source s = t.sources[pos - 1];
        int line = t.lines[pos - 1];
        if (s == null || line <= 0) {
            return null;
        }
        int col = Math.max(1, t.cols[pos - 1]);
        // A character-less source takes line/column ranges; a multi-line node keeps to its first
        // line (GIR records a length, not an end position).
        return s.createSection(line, col, line, col + Math.max(1, t.lens[pos - 1]) - 1);
    }

    @Override
    public String toString() {
        return function;
    }

    /** A root whose nodes carry positions of a Go function's table (the function, a range-over-func body). */
    public interface Root {
        Positions positions();

        /**
         * The position a frame of this root is at when its call node has none: the
         * function's exit, where its deferred calls run (gc: the closing brace or final
         * {@code return}); 0 if unknown.
         */
        default int exitPos() {
            return 0;
        }
    }

    /** A node that records a position of its function's table (a statement, a call). */
    public interface At {
        /** The 1-based index into the function's position table; 0: none. */
        int pos();
    }

    /** The position table of {@code root}'s nodes, or null. */
    public static Positions of(com.oracle.truffle.api.nodes.RootNode root) {
        return root instanceof Root p ? p.positions() : null;
    }
}
