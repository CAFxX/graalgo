package graalgo.frontend;

import com.oracle.truffle.api.source.Source;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;

/**
 * M10 (P1-3): frontend robustness against malformed type graphs.
 * The exporter never emits cycles, but the Java walkers are the last line
 * of defense: a cyclic wire document must fail fast with FrontendException,
 * never hang or overflow the stack.
 */
public final class FrontendRobustnessTest {

    public void testCyclicStructTypeFailsFast() throws Exception {
        // Test resource: frontend/v1 document with a cyclic type graph
        // (named T -> struct{ v T }); loaded from the source tree like the
        // corpus files.
        String json = Files.readString(
                Path.of("src/test/resources/frontend/cyclic-struct.json"),
                StandardCharsets.UTF_8);
        Source source = Source.newBuilder("go", "package main", "cyclic.go").build();
        try {
            new NodeFactory(source).buildProgram(json);
        } catch (FrontendException e) {
            if (!e.getMessage().contains("cyclic")) {
                throw new AssertionError("expected 'cyclic' in FrontendException, got <"
                        + e.getMessage() + ">");
            }
            return; // expected: clean fail-fast
        }
        throw new AssertionError("cyclic type graph must fail fast with FrontendException");
    }

    /**
     * M10 (P3-M10 WS J, self-attack F1): a deeply-nested JSON document must
     * fail fast with a named FrontendException, never a raw
     * StackOverflowError. The parser is recursive (value → array/object →
     * value); without a depth cap, 10k+ nesting levels overflow the host
     * stack and escape Main's FrontendException-only catch as an unnamed
     * JVM trace. Regression test for the MAX_DEPTH cap in Json.Parser.
     */
    public void testDeepNestingFailsFast() throws Exception {
        StringBuilder sb = new StringBuilder();
        int depth = 100_000;
        for (int i = 0; i < depth; i++) {
            sb.append('[');
        }
        for (int i = 0; i < depth; i++) {
            sb.append(']');
        }
        Source source = Source.newBuilder("go", "package main", "deep.go").build();
        try {
            new NodeFactory(source).buildProgram(sb.toString());
        } catch (FrontendException e) {
            if (!e.getMessage().contains("nesting too deep")) {
                throw new AssertionError("expected 'nesting too deep' in FrontendException, got <"
                        + e.getMessage() + ">");
            }
            return; // expected: clean fail-fast
        } catch (StackOverflowError e) {
            throw new AssertionError("deeply-nested JSON must not surface StackOverflowError");
        }
        throw new AssertionError("deeply-nested JSON must fail fast with FrontendException");
    }

    /**
     * M10 (P3-M10 WS J, self-attack F2/F3): truncated and corrupt JSON
     * documents must fail fast with a named FrontendException — never a
     * raw JVM error, a hang, or silent acceptance. Each case pins the
     * fail-fast behavior of Json.parse + NodeFactory.buildProgram.
     */
    public void testCorruptJsonFailsFast() throws Exception {
        String[] docs = {
            "", // empty
            "not json at all {{{]", // garbage
            "{\"frontend\": 42}", // wrong top-level type
            "null", // null document
            "[1,2,3]", // array document
            "{\"s\": \"\\q\"}", // bad escape
            "{\"s\": \"\\u12zz\"}", // bad unicode escape
            "{\"n\": 99999999999999999999999}", // integer out of range
            "-", // lone minus
            "{\"n\": 1e}", // bad exponent
            "{\"a\":1} garbage", // trailing content
        };
        String[] names = {
            "empty", "garbage", "wrong-top-type", "null-doc", "array-doc",
            "bad-escape", "bad-unicode-escape", "huge-int", "lone-minus",
            "bad-exponent", "trailing-content",
        };
        for (int i = 0; i < docs.length; i++) {
            expectFrontendException("F2/F3 " + names[i], docs[i]);
        }
        // Truncation at several offsets of a structurally valid document.
        String valid = "{\"frontend\": \"v1\", \"package\": {\"name\": \"main\"},"
                + " \"types\": [], \"objects\": [], \"funcs\": [], \"globals\": []}";
        int[] cuts = {0, 1, 10, 40, valid.length() - 1};
        for (int cut : cuts) {
            expectFrontendException("F2 truncate@" + cut, valid.substring(0, cut));
        }
    }

    private static void expectFrontendException(String name, String doc) {
        Source source = Source.newBuilder("go", "package main", "t.go").build();
        try {
            new NodeFactory(source).buildProgram(doc);
        } catch (FrontendException e) {
            return; // expected: clean fail-fast with a named error
        } catch (StackOverflowError | OutOfMemoryError e) {
            throw new AssertionError(name + ": must not surface raw JVM error, got " + e);
        }
        throw new AssertionError(name + ": corrupt document must fail fast with FrontendException");
    }
}
