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
}
