package graalgo.p3sync.launcher;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.concurrent.TimeUnit;

/**
 * The documented stack-depth limit (docs/arch/limitations.md) on the
 * PRODUCT launcher (P3SyncMain). Guest recursion burns host stack; gc grows
 * goroutine stacks, so 100k-deep recursion that gc runs is rejected here.
 * The contract is fail-fast: a non-zero exit and a one-line
 * "stack overflow" diagnostic on stderr, never a JVM stack trace and never
 * a wrong result. Moved here 2026-11-18 from the v1 MainTest
 * (graalgo-run), which is quarantined until its deletion at M3.
 *
 * <p>The program runs in a CHILD JVM through the product launcher script
 * (internal/backend/graalgo-sync-run): a host StackOverflowError inside a
 * class initializer poisons that class for the rest of the JVM, so an
 * in-process run would break every later test in the list.
 *
 * <p>Falsifiability: if the launcher let the StackOverflowError escape,
 * stderr would carry "\tat " frames; if it swallowed it, the exit would be
 * 0 (or stdout would carry a value).
 */
public final class StackDepthLimitTest {

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    public void testDeepRecursionFailsFastWithoutJvmTrace() throws Exception {
        Path dir = Files.createTempDirectory("graalgo-stack-depth");
        Path prog = dir.resolve("deep.go");
        Files.writeString(prog,
                "package main\n\n"
                        + "func rec(n int) int {\n\tif n <= 0 {\n\t\treturn 0\n\t}\n\treturn 1 + rec(n-1)\n}\n\n"
                        + "func main() { println(rec(100000)) }\n",
                StandardCharsets.UTF_8);
        Path classes = Path.of(P3SyncMain.class.getProtectionDomain().getCodeSource().getLocation().toURI());
        // .../internal/backend/interop/out/classes -> .../internal/backend
        Path launcher = classes.getParent().getParent().getParent().resolve("graalgo-sync-run");
        check(Files.isExecutable(launcher), "product launcher not found at " + launcher);
        ProcessBuilder pb = new ProcessBuilder(launcher.toString(), prog.toString());
        pb.environment().put("TMPDIR", dir.toString());
        pb.redirectOutput(dir.resolve("out").toFile());
        pb.redirectError(dir.resolve("err").toFile());
        Process p = pb.start();
        check(p.waitFor(300, TimeUnit.SECONDS), "launcher did not finish in 300 s");
        int code = p.exitValue();
        String out = Files.readString(dir.resolve("out"), StandardCharsets.UTF_8);
        String e = Files.readString(dir.resolve("err"), StandardCharsets.UTF_8);
        check(code != 0, "deep recursion must fail, got exit 0; stderr <" + e + ">");
        check(out.isEmpty(), "no result may be printed: <" + out + ">");
        check(e.contains("stack overflow"), "stderr names the stack overflow: <" + e + ">");
        check(!e.contains("\tat "), "no JVM stack trace may leak: <" + e + ">");
    }
}
