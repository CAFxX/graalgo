package graalgo.launcher;

import graalgo.language.GoLanguage;
import java.io.IOException;
import java.io.RandomAccessFile;
import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.nio.charset.StandardCharsets;
import org.graalvm.polyglot.Context;
import org.graalvm.polyglot.PolyglotException;
import org.graalvm.polyglot.Source;
import org.graalvm.polyglot.io.ByteSequence;

/**
 * Runs a program linked by {@code graalgo-tool} (toolexec-design §4.2): the launcher stub execs
 * this runner with the executable's path, then the {@code argv[0]} the program was invoked with
 * and the program's arguments ({@code os.Args}). The bundle is
 * located through the executable's trailer ({@code offset | length | "GGB1"}) and evaluated as
 * a byte source of the bundle MIME type; the process exits with the program's status.
 */
public final class BundleMain {
    private BundleMain() {}

    public static void main(String[] args) throws IOException {
        if (args.length < 2) {
            fail("usage: graalgo-bundle-run <linked executable> <argv[0]> [program arguments]");
        }
        byte[] bundle = readBundle(args[0]);
        int status;
        try (Context context = GoPolyglot.newContext(gomaxprocs(),
                java.util.Arrays.copyOfRange(args, 1, args.length), java.util.List.of())) {
            Source source = Source.newBuilder(GoLanguage.ID, ByteSequence.create(bundle), args[0])
                    .mimeType(GoLanguage.BUNDLE_MIME).buildLiteral();
            status = context.eval(source).asInt();
        } catch (PolyglotException e) {
            if (!e.isExit()) {
                fail("graalgo: " + e.getMessage());
                return;
            }
            // os.Exit(n), an uncaught panic or a fatal error: the context's hard exit.
            status = e.getExitStatus();
        }
        System.out.flush();
        System.err.flush();
        Runtime.getRuntime().halt(status);
    }

    private static int gomaxprocs() {
        String v = System.getenv("GOMAXPROCS");
        try {
            return v == null ? 0 : Integer.parseInt(v.trim());
        } catch (NumberFormatException e) {
            return 0;
        }
    }

    static byte[] readBundle(String exe) throws IOException {
        try (RandomAccessFile f = new RandomAccessFile(exe, "r")) {
            long size = f.length();
            byte[] trailer = new byte[20];
            if (size < trailer.length) {
                fail(exe + ": too short for a bundle trailer");
            }
            f.seek(size - trailer.length);
            f.readFully(trailer);
            if (!new String(trailer, 16, 4, StandardCharsets.US_ASCII).equals("GGB1")) {
                fail(exe + ": no program bundle trailer (not linked by graalgo-tool?)");
            }
            ByteBuffer b = ByteBuffer.wrap(trailer).order(ByteOrder.LITTLE_ENDIAN);
            long offset = b.getLong(0);
            long length = b.getLong(8);
            if (offset < 0 || length < 0 || length > Integer.MAX_VALUE || offset + length != size - trailer.length) {
                fail(exe + ": trailer does not describe this file");
            }
            byte[] bundle = new byte[(int) length];
            f.seek(offset);
            f.readFully(bundle);
            return bundle;
        }
    }

    private static void fail(String message) {
        byte[] line = (message + "\n").getBytes(StandardCharsets.UTF_8);
        System.err.write(line, 0, line.length);
        System.err.flush();
        Runtime.getRuntime().halt(2);
    }
}
