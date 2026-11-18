package graalgo.launcher;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.FileVisitResult;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.SimpleFileVisitor;
import java.nio.file.attribute.BasicFileAttributes;

/**
 * B-02: the plain launcher initializes the Go os surface for every run.
 *
 * <p>Before the fix, {@code Main.runOne} never called {@code GoOs.init},
 * so {@code os.Args} was empty (and env/vcwd state leaked across
 * {@code --batch} entries) for every program run via {@code graalgo-run}.
 *
 * <p>Falsifiability: each test drives {@link Main} on a small program
 * through the real frontend and execution path, and asserts on the
 * program's own output. With the {@code initializeOsSurface} call
 * removed, the args tests print {@code -1} / nothing (empty
 * {@code os.Args}) and the batch test prints the {@code FAIL} markers
 * (and entry 2 panics on the empty {@code os.Args[0]}) instead of
 * {@code entry2-ok}. The {@code buildGoArgv} test is
 * hermetic (no gofrontend needed) and pins the argv[0] rule directly.
 *
 * <p>The probe programs mirror {@code test/corpus/wave2/os/10_args.go}
 * (whose header documents the {@code --} contract). No probe calls
 * {@code os.Exit}: it would {@code System.exit} the test JVM.
 */
public final class LauncherOsArgsTest {

    /** Prints len(os.Args)-1, then os.Args[1:] one per line. */
    private static final String ARGS_PROG = "package main\n"
            + "\n"
            + "import (\n"
            + "\t\"fmt\"\n"
            + "\t\"os\"\n"
            + ")\n"
            + "\n"
            + "func main() {\n"
            + "\tfmt.Println(len(os.Args) - 1)\n"
            + "\tfor _, a := range os.Args[1:] {\n"
            + "\t\tfmt.Println(a)\n"
            + "\t}\n"
            + "}\n";

    /** Prints os.Args[0]. */
    private static final String ARGV0_PROG = "package main\n"
            + "\n"
            + "import (\n"
            + "\t\"fmt\"\n"
            + "\t\"os\"\n"
            + ")\n"
            + "\n"
            + "func main() {\n"
            + "\tfmt.Println(os.Args[0])\n"
            + "}\n";

    /** Dirties the os surface (Setenv then Clearenv) and prints ok. */
    private static final String BATCH_ENTRY1_PROG = "package main\n"
            + "\n"
            + "import (\n"
            + "\t\"fmt\"\n"
            + "\t\"os\"\n"
            + ")\n"
            + "\n"
            + "func main() {\n"
            + "\tos.Setenv(\"B02_BATCH_MARK\", \"leak\")\n"
            + "\tos.Clearenv()\n"
            + "\tfmt.Println(\"entry1-ok\")\n"
            + "}\n";

    /** Prints FAIL markers if entry 1's state leaked, then ok. */
    private static final String BATCH_ENTRY2_PROG = "package main\n"
            + "\n"
            + "import (\n"
            + "\t\"fmt\"\n"
            + "\t\"os\"\n"
            + ")\n"
            + "\n"
            + "func main() {\n"
            + "\tif os.Getenv(\"PATH\") == \"\" {\n"
            + "\t\tfmt.Println(\"FAIL: PATH cleared\")\n"
            + "\t}\n"
            + "\tif _, ok := os.LookupEnv(\"B02_BATCH_MARK\"); ok {\n"
            + "\t\tfmt.Println(\"FAIL: mark leaked\")\n"
            + "\t}\n"
            + "\tfmt.Println(\"ARGV0=\" + os.Args[0])\n"
            + "\tfmt.Println(\"entry2-ok\")\n"
            + "}\n";

    /** verifies: B-02 (args after -- reach os.Args[1:]). */
    public void testArgsAfterDashDash() throws Exception {
        Path dir = null;
        try {
            dir = Files.createTempDirectory("graalgo-osargs-");
            Path prog = writeProg(dir, "args.go", ARGS_PROG);
            RunResult r = runCaptured(new String[]{prog.toString(), "--", "alpha", "beta"});
            assertEquals(Main.EXIT_OK, r.exit, "run exit code");
            assertEquals("2\nalpha\nbeta\n", r.stdout, "os.Args[1:] output");
        } finally {
            deleteTree(dir);
        }
    }

    /** verifies: B-02 (no -- args means os.Args is just argv[0]). */
    public void testNoProgramArgs() throws Exception {
        Path dir = null;
        try {
            dir = Files.createTempDirectory("graalgo-osargs-");
            Path prog = writeProg(dir, "args.go", ARGS_PROG);
            RunResult r = runCaptured(new String[]{prog.toString()});
            assertEquals(Main.EXIT_OK, r.exit, "run exit code");
            assertEquals("0\n", r.stdout, "os.Args[1:] output");
        } finally {
            deleteTree(dir);
        }
    }

    /** verifies: B-02 (-- ends flag parsing even before the file). */
    public void testDashDashBeforeFile() throws Exception {
        Path dir = null;
        try {
            dir = Files.createTempDirectory("graalgo-osargs-");
            Path prog = writeProg(dir, "args.go", ARGS_PROG);
            RunResult r = runCaptured(new String[]{"--", prog.toString(), "x"});
            assertEquals(Main.EXIT_OK, r.exit, "run exit code");
            assertEquals("1\nx\n", r.stdout, "os.Args[1:] output");
        } finally {
            deleteTree(dir);
        }
    }

    /** verifies: B-02 (argv[0] is the absolute entry-point path). */
    public void testArgvZeroIsAbsoluteEntryPath() throws Exception {
        Path dir = null;
        try {
            dir = Files.createTempDirectory("graalgo-osargs-");
            Path prog = writeProg(dir, "argv0.go", ARGV0_PROG);
            RunResult r = runCaptured(new String[]{prog.toString()});
            assertEquals(Main.EXIT_OK, r.exit, "run exit code");
            assertEquals(prog.toAbsolutePath().normalize().toString() + "\n",
                    r.stdout, "os.Args[0] output");
        } finally {
            deleteTree(dir);
        }
    }

    /** verifies: B-02 (argv[0] rule, hermetic — no gofrontend needed). */
    public void testBuildGoArgv() {
        String[] goArgv = Main.buildGoArgv("rel/prog.go", new String[]{"a", "b"});
        assertEquals(3, goArgv.length, "goArgv length");
        assertTrue(Path.of(goArgv[0]).isAbsolute(), "argv[0] must be absolute: " + goArgv[0]);
        assertTrue(goArgv[0].endsWith("rel/prog.go"), "argv[0] must end with the entry path");
        assertEquals("a", goArgv[1], "argv[1]");
        assertEquals("b", goArgv[2], "argv[2]");
    }

    /** verifies: B-02 (per-batch-entry os-surface reset). */
    public void testBatchEntriesIsolated() throws Exception {
        Path dir = null;
        try {
            dir = Files.createTempDirectory("graalgo-osargs-");
            Path entry1 = writeProg(dir, "entry1.go", BATCH_ENTRY1_PROG);
            Path entry2 = writeProg(dir, "entry2.go", BATCH_ENTRY2_PROG);
            Path batch = dir.resolve("batch.txt");
            Files.writeString(batch, "run:" + entry1 + "\nrun:" + entry2 + "\n",
                    StandardCharsets.UTF_8);
            RunResult r = runCaptured(new String[]{"--batch=" + batch});
            assertEquals(Main.EXIT_OK, r.exit, "batch exit code");
            assertTrue(r.stdout.contains("entry1-ok"), "entry1 must print ok: " + r.stdout);
            assertTrue(r.stdout.contains("entry2-ok"), "entry2 must print ok: " + r.stdout);
            assertTrue(r.stdout.contains("ARGV0=" + entry2.toAbsolutePath().normalize().toString()),
                    "entry2 argv[0] must be its own path: " + r.stdout);
            assertTrue(!r.stdout.contains("FAIL"),
                    "no state may leak from entry1 into entry2: " + r.stdout);
        } finally {
            deleteTree(dir);
        }
    }

    /** verifies: B-02 (--batch takes no program arguments). */
    public void testBatchRejectsProgramArgs() throws Exception {
        Path dir = null;
        try {
            dir = Files.createTempDirectory("graalgo-osargs-");
            Path prog = writeProg(dir, "args.go", ARGS_PROG);
            Path batch = dir.resolve("batch.txt");
            Files.writeString(batch, "run:" + prog + "\n", StandardCharsets.UTF_8);
            RunResult r = runCaptured(new String[]{"--batch=" + batch, "--", "x"});
            assertEquals(Main.EXIT_INPUT_ERROR, r.exit, "batch + -- args must be a usage error");
        } finally {
            deleteTree(dir);
        }
    }

    /** Runs {@link Main#run} with stdout captured; restores it after. */
    private static RunResult runCaptured(String[] args) {
        PrintStream realOut = System.out;
        ByteArrayOutputStream bout = new ByteArrayOutputStream();
        System.setOut(new PrintStream(bout, true, StandardCharsets.UTF_8));
        int exit;
        try {
            exit = Main.run(args);
        } finally {
            System.setOut(realOut);
        }
        return new RunResult(exit, bout.toString(StandardCharsets.UTF_8));
    }

    private static final class RunResult {
        final int exit;
        final String stdout;

        RunResult(int exit, String stdout) {
            this.exit = exit;
            this.stdout = stdout;
        }
    }

    private static Path writeProg(Path dir, String name, String src) throws IOException {
        Path p = dir.resolve(name);
        Files.writeString(p, src, StandardCharsets.UTF_8);
        return p;
    }

    private static void deleteTree(Path root) throws IOException {
        if (root == null || !Files.exists(root)) {
            return;
        }
        Files.walkFileTree(root, new SimpleFileVisitor<>() {
            @Override
            public FileVisitResult visitFile(Path file, BasicFileAttributes attrs) throws IOException {
                Files.deleteIfExists(file);
                return FileVisitResult.CONTINUE;
            }

            @Override
            public FileVisitResult postVisitDirectory(Path dir, IOException exc) throws IOException {
                Files.deleteIfExists(dir);
                return FileVisitResult.CONTINUE;
            }
        });
    }

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void assertEquals(int expected, int actual, String what) {
        if (expected != actual) {
            throw new AssertionError(what + ": expected " + expected + " but got " + actual);
        }
    }

    private static void assertEquals(String expected, String actual, String what) {
        if (!expected.equals(actual)) {
            throw new AssertionError(what + ": expected <" + expected + "> but got <" + actual + ">");
        }
    }
}
