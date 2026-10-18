package graalgo.frontend.v2;

import graalgo.launcher.ToolPaths;
import graalgo.values.GoKind;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.TimeUnit;

/**
 * L01 decode roundtrip: a real Go program using bool and every unsigned int
 * kind is run through the actual Go frontend (parse → check → emit →
 * binary encode via {@code encode-demo}) and decoded here with
 * {@link BinaryDecoder}; every basic type-table entry must map through
 * {@link GoKind#fromBasic} without throwing, to the expected kind.
 *
 * <p>Before the L01 fix the emitter classified typed bool as class
 * "unknown" and uint8..uint64/uint/uintptr as class "int"; fromBasic throws
 * IllegalArgumentException on both, so any bool/uint program failed at
 * decode. This test is demonstrably capable of failing: with the fix
 * reverted, encode-demo emits the old classes and fromBasic throws.
 *
 * <p>Runs the stock Go toolchain like the other corpus tests
 * (ToolPaths.stockGo, GOTOOLCHAIN=local); the v2 module is located via
 * -Dgraalgo.repo.dir like the phase-1 corpus tests.
 */
public final class BasicClassRoundtripTest {

    private static final String PROG = """
            package main

            func main() {
                var b bool = true
                var u uint = 1
                var u8 uint8 = 2
                var u16 uint16 = 3
                var u32 uint32 = 4
                var u64 uint64 = 5
                var up uintptr = 6
                var by byte = 7
                var i int = -1
                var i8 int8 = -2
                var i16 int16 = -3
                var i32 int32 = -4
                var i64 int64 = -5
                var r rune = 'a'
                var f32 float32 = 1.5
                var f64 float64 = 2.5
                var c64 complex64 = 1 + 2i
                var c128 complex128 = 3 + 4i
                var s string = "hi"
                _ = b
                _ = u
                _ = u8
                _ = u16
                _ = u32
                _ = u64
                _ = up
                _ = by
                _ = i
                _ = i8
                _ = i16
                _ = i32
                _ = i64
                _ = r
                _ = f32
                _ = f64
                _ = c64
                _ = c128
                _ = s
            }
            """;

    /** Expected go/types basic name → GoKind for the program above. */
    private static final Map<String, GoKind> EXPECTED = Map.ofEntries(
            Map.entry("bool", GoKind.BOOL),
            Map.entry("uint", GoKind.UINT64),
            Map.entry("uint8", GoKind.UINT8),
            Map.entry("uint16", GoKind.UINT16),
            Map.entry("uint32", GoKind.UINT32),
            Map.entry("uint64", GoKind.UINT64),
            Map.entry("uintptr", GoKind.UINT64),
            Map.entry("int", GoKind.INT64),
            Map.entry("int8", GoKind.INT8),
            Map.entry("int16", GoKind.INT16),
            Map.entry("int32", GoKind.INT32),
            Map.entry("int64", GoKind.INT64),
            Map.entry("float32", GoKind.FLOAT32),
            Map.entry("float64", GoKind.FLOAT64),
            Map.entry("complex64", GoKind.COMPLEX64),
            Map.entry("complex128", GoKind.COMPLEX128),
            Map.entry("string", GoKind.STRING));

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    public void testBoolAndUintRoundtrip() throws Exception {
        Path tmp = Files.createTempDirectory("graalgo-l01-roundtrip");
        Path src = tmp.resolve("main.go");
        Files.write(src, PROG.getBytes(StandardCharsets.UTF_8));
        Path out = tmp.resolve("doc.g2bi");

        Path v2dir = Path.of(System.getProperty("graalgo.repo.dir", "."))
                .resolve("internal").resolve("frontend").resolve("v2");
        check(Files.isRegularFile(v2dir.resolve("go.mod")),
                "v2 module not found at " + v2dir + " (set -Dgraalgo.repo.dir)");

        // Full Go frontend: parse → check → emit → binary encode.
        ProcessBuilder pb = new ProcessBuilder(
                ToolPaths.stockGo().toString(), "run", "./encode/cmd/encode-demo",
                src.toString(), out.toString());
        pb.directory(v2dir.toFile());
        pb.environment().put("GOTOOLCHAIN", "local");
        pb.environment().put("GOMAXPROCS", "1");
        pb.redirectErrorStream(true);
        Process p = pb.start();
        String log = new String(p.getInputStream().readAllBytes(), StandardCharsets.UTF_8);
        boolean done = p.waitFor(300, TimeUnit.SECONDS);
        if (!done) {
            p.destroyForcibly();
            throw new AssertionError("encode-demo timed out: " + log);
        }
        check(p.exitValue() == 0,
                "encode-demo failed (exit " + p.exitValue() + "): " + log);
        check(Files.isRegularFile(out), "encode-demo produced no output: " + log);

        Map<String, Object> doc = BinaryDecoder.decodeDocument(Files.readAllBytes(out));
        Object types = doc.get("types");
        check(types instanceof List, "doc has no types list");
        Map<String, GoKind> seen = new HashMap<>();
        for (Object te : (List<?>) types) {
            check(te instanceof Map, "type entry not a map: " + te);
            Map<?, ?> m = (Map<?, ?>) te;
            if (!"basic".equals(m.get("kind"))) {
                continue;
            }
            Object detail = m.get("detail");
            check(detail instanceof Map, "basic detail not a map: " + m);
            String name = String.valueOf(((Map<?, ?>) detail).get("name"));
            String cls = String.valueOf(((Map<?, ?>) detail).get("class"));
            // The decode step: must not throw, and must yield the expected kind.
            GoKind kind;
            try {
                kind = GoKind.fromBasic(name, cls);
            } catch (IllegalArgumentException e) {
                throw new AssertionError(
                        "GoKind.fromBasic rejected emitted (name=" + name
                                + ", class=" + cls + "): " + e.getMessage());
            }
            seen.put(name, kind);
        }
        for (Map.Entry<String, GoKind> e : EXPECTED.entrySet()) {
            GoKind got = seen.get(e.getKey());
            check(got != null, "type " + e.getKey() + " missing from decoded type table");
            check(got == e.getValue(),
                    "type " + e.getKey() + ": decoded as " + got + ", want " + e.getValue());
        }
    }
}
