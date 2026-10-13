package graalgo.frontend;

import com.oracle.truffle.api.source.Source;
import com.oracle.truffle.api.source.SourceSection;

/**
 * Red-green for the P3-M5 {@code NodeFactory.sectionFor} TextMap fix
 * (ADR-0005 §D5 documented exception): synthesized wire nodes may carry
 * {@code pos.line} past the end of the source text (hit by
 * {@code time.Sleep} programs via the synthetic package), which
 * Truffle's {@code TextMap.lineStartOffset} rejects with
 * {@code IllegalArgumentException: line out of bounds}. The fix clamps
 * to a valid section instead of failing the build.
 *
 * <p>RED: on the unfixed factory,
 * {@code testOutOfRangeLineClamped} throws
 * {@code IllegalArgumentException: line out of bounds}.
 * GREEN: it builds and yields a valid zero-length section.
 */
public final class SectionClampTest {

    private static final String TYPES = "["
            + "{\"kind\":\"signature\",\"detail\":{\"params\":[],\"results\":[],\"variadic\":false}},"
            + "{\"kind\":\"basic\",\"detail\":{\"class\":\"int\",\"name\":\"int\"}}"
            + "]";

    private static final String OBJECTS = "["
            + "{\"kind\":\"var\",\"name\":\"x\",\"type\":1}"
            + "]";

    /**
     * Minimal program: {@code var x = 6} where the literal's pos.line
     * (9999) is far past the 2-line source. The out-of-range position
     * must clamp, not throw.
     */
    private static final String DOC_OUT_OF_RANGE = "{"
            + "\"frontend\":\"v1\","
            + "\"types\":" + TYPES + ","
            + "\"objects\":" + OBJECTS + ","
            + "\"funcs\":[{"
            + "\"kind\":\"funcdecl\","
            + "\"pos\":{\"line\":1,\"col\":1,\"len\":1},"
            + "\"fields\":{"
            + "\"name\":\"main\",\"obj\":0,\"params\":[],\"sig\":0,"
            + "\"body\":{\"kind\":\"block\","
            + "\"pos\":{\"line\":1,\"col\":1,\"len\":1},"
            + "\"fields\":{\"stmts\":[{"
            + "\"kind\":\"vardecl\","
            + "\"pos\":{\"line\":9999,\"col\":1,\"len\":1},"
            + "\"fields\":{\"define\":true,\"names\":[\"x\"],\"objs\":[0],\"type\":null,"
            + "\"values\":[{"
            + "\"kind\":\"basiclit\","
            + "\"pos\":{\"line\":9999,\"col\":5,\"len\":1},"
            + "\"fields\":{\"const\":\"6\",\"type\":1,\"value\":\"6\"}"
            + "}]}}]}}}}]}";

    /** Same program with an in-range position: behavior must be unchanged. */
    private static final String DOC_IN_RANGE = DOC_OUT_OF_RANGE
            .replace("\"line\":9999", "\"line\":1");

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    public void testOutOfRangeLineClamped() {
        Source src = Source.newBuilder("go", "package main\n\n// end", "clamp.go").build();
        NodeFactory.Program p = new NodeFactory(src).buildProgram(DOC_OUT_OF_RANGE);
        NodeFactory.Program prog = p;
        check(prog != null, "program built");
        // The vardecl node got a valid (clamped) section: zero-length at
        // column 1 of the last source line.
        SourceSection sec = ((BlockNode) prog.root().getBody()).getChildren()
                .iterator().next().getSourceSection();
        check(sec != null, "section present");
        check(sec.getCharLength() == 0, "clamped section is zero-length");
        check(sec.getStartLine() == 3, "clamped to last line, got " + sec.getStartLine());
        check(sec.getStartColumn() == 1, "clamped to column 1");
    }

    public void testInRangePositionUnchanged() {
        Source src = Source.newBuilder("go", "package main\n\n// end", "clamp.go").build();
        NodeFactory.Program p = new NodeFactory(src).buildProgram(DOC_IN_RANGE);
        SourceSection sec = ((BlockNode) p.root().getBody()).getChildren()
                .iterator().next().getSourceSection();
        check(sec != null, "section present");
        // In-range positions take the identical path as before the fix:
        // the requested (line=1, col=1, len=1) section, untouched.
        check(sec.getCharLength() == 1, "in-range length preserved");
        check(sec.getStartLine() == 1 && sec.getStartColumn() == 1,
                "in-range line/col preserved");
    }

    public void testOutOfRangeColumnClamped() {
        // Valid line, absurd column on the literal: also out of range, also clamps.
        String doc = DOC_OUT_OF_RANGE.replace("\"line\":9999", "\"line\":1")
                .replace("\"col\":5", "\"col\":9999");
        Source src = Source.newBuilder("go", "package main\n\n// end", "clamp.go").build();
        NodeFactory.Program p = new NodeFactory(src).buildProgram(doc);
        // First child of the body is the vardecl; its first child is the literal.
        com.oracle.truffle.api.nodes.Node vardecl =
                ((BlockNode) p.root().getBody()).getChildren().iterator().next();
        SourceSection sec = vardecl.getChildren().iterator().next().getSourceSection();
        check(sec != null && sec.getCharLength() == 0, "bad column clamps to zero-length");
    }
}
