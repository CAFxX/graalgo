package graalgo.frontend.v2;

import com.oracle.truffle.api.source.Source;
import graalgo.frontend.FrontendException;
import java.nio.file.Files;
import java.nio.file.Path;

/**
 * P0-3: the v2 factory must fail fast on multi-value returns instead of
 * silently keeping only the first value.
 *
 * <p>Fixtures are binary v2 documents produced by the Go emitter
 * ({@code internal/frontend/v2/encode/cmd/encode-demo}):
 * <ul>
 *   <li>{@code multireturn.bin}: {@code func f() (int, int) { return 1, 2 }}</li>
 *   <li>{@code singlereturn.bin}: {@code func g() int { return 42 }} (positive control)</li>
 * </ul>
 */
public final class MultiReturnFailFastTest {

    private static byte[] fixture(String name) throws Exception {
        return Files.readAllBytes(Path.of("src/test/resources/frontend/v2", name));
    }

    private static Object buildProgram(byte[] data) throws Exception {
        Source source = Source.newBuilder("go", "package main", "multireturn.go").build();
        return new NodeFactoryV2(source).buildProgram(data);
    }

    /** Multi-value return must fail fast with a named FrontendException. */
    public void testMultiValueReturnFailsFast() throws Exception {
        try {
            buildProgram(fixture("multireturn.bin"));
        } catch (FrontendException e) {
            if (!e.getMessage().contains("multi-value return")) {
                throw new AssertionError(
                    "expected 'multi-value return' in FrontendException, got <" + e.getMessage() + ">");
            }
            return; // expected: clean fail-fast
        }
        throw new AssertionError(
            "multi-value return must fail fast with FrontendException, not silently keep the first value");
    }

    /** Positive control: a single-value return still builds. */
    public void testSingleValueReturnBuilds() throws Exception {
        Object program = buildProgram(fixture("singlereturn.bin"));
        if (program == null) {
            throw new AssertionError("single-value return program must build");
        }
    }
}
