package graalgo.launcher;

import java.io.ByteArrayOutputStream;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;

/**
 * End-to-end regression test for review 2026-10-30-21, batch-mode
 * state leak (P1): a guest {@code os.Exit} used to kill the whole
 * {@code --batch} JVM, aborting remaining entries and losing the JSON
 * report. Under the batch exit trap, the exiting entry records its
 * code, the loop continues, and the full JSON array is still printed.
 *
 * <p>Requires the gofrontend binary (the runtime Makefile's test
 * target exports {@code GRAALGO_GOFRONTEND}); like the other
 * subprocess tests in this suite, it fails loud without it.
 */
public final class BatchTrapTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    public void testGuestExitEndsOnlyItsEntry() throws Exception {
        Path dir = Files.createTempDirectory("batch-trap");
        try {
            Path exitProg = dir.resolve("exit.go");
            Files.writeString(exitProg, """
                    package main

                    import "os"

                    func main() {
                        println("before-exit")
                        os.Exit(3)
                        println("after-exit-unreachable")
                    }
                    """, StandardCharsets.UTF_8);
            Path healthyProg = dir.resolve("healthy.go");
            Files.writeString(healthyProg, """
                    package main

                    func main() {
                        println("healthy-ok")
                    }
                    """, StandardCharsets.UTF_8);
            Path batch = dir.resolve("batch.txt");
            Files.writeString(batch, "run:" + exitProg + "\nrun:" + healthyProg + "\n",
                    StandardCharsets.UTF_8);
            PrintStream realOut = System.out;
            ByteArrayOutputStream bout = new ByteArrayOutputStream();
            System.setOut(new PrintStream(bout, true, StandardCharsets.UTF_8));
            int rc;
            try {
                rc = Main.runBatch(batch.toString());
            } finally {
                System.setOut(realOut);
            }
            String json = bout.toString(StandardCharsets.UTF_8);
            assertTrue(rc == 0,
                    "batch process exit should be 0 even with a failing entry, got " + rc);
            assertTrue(json.contains("\"exit\": 3"),
                    "exiting entry should record the guest's exit code 3, got JSON: " + json);
            assertTrue(json.contains("before-exit"),
                    "exiting entry's output should be captured, got JSON: " + json);
            assertTrue(!json.contains("after-exit-unreachable"),
                    "code after os.Exit must not run, got JSON: " + json);
            assertTrue(json.contains("healthy-ok"),
                    "the entry after the exiting one must still run, got JSON: " + json);
        } finally {
            deleteRecursively(dir);
        }
    }

    private static void deleteRecursively(Path p) throws Exception {
        if (Files.isDirectory(p)) {
            try (var entries = Files.list(p)) {
                for (Path e : entries.toList()) {
                    deleteRecursively(e);
                }
            }
        }
        Files.deleteIfExists(p);
    }
}
