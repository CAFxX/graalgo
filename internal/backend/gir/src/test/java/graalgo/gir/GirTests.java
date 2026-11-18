package graalgo.gir;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.Random;
import java.util.stream.Stream;

/**
 * Tests of the Java GIR decoder (no test framework: the module depends on the JDK only).
 *
 * <pre>
 * GirTests [-fuzz N] [-seed S] [-stats] PATH...
 * </pre>
 *
 * Every *.gir file under the PATHs (the golden vectors written by the Go encoder, and optionally a
 * directory of GIR files of the standard library) must decode, validate, and re-encode to the same
 * bytes, body by body and as a whole: the Go encoder and the Java decoder/encoder agree on every
 * field. A file whose name starts with "bad-" must instead fail with GirFormatException. With
 * -fuzz, each file is mutated N times; the decoder must either throw GirFormatException or
 * decode a file that re-encodes to exactly the mutated bytes, and must never throw anything else.
 * A watchdog fails the run if it hangs.
 */
public final class GirTests {
    private static int failures;

    private static void fail(String msg) {
        failures++;
        System.err.println("FAIL: " + msg);
    }

    public static void main(String[] args) throws Exception {
        int fuzz = 0;
        long seed = 1;
        boolean stats = false;
        List<Path> files = new ArrayList<>();
        for (int i = 0; i < args.length; i++) {
            switch (args[i]) {
                case "-fuzz" -> fuzz = Integer.parseInt(args[++i]);
                case "-seed" -> seed = Long.parseLong(args[++i]);
                case "-stats" -> stats = true;
                default -> collect(Path.of(args[i]), files);
            }
        }
        if (files.isEmpty()) {
            System.err.println("FAIL: no .gir files found");
            System.exit(1);
        }
        Thread watchdog = new Thread(() -> {
            try {
                Thread.sleep(600_000);
                System.err.println("FAIL: watchdog: GirTests ran for 10 minutes (decoder hang?)");
                Runtime.getRuntime().halt(3);
            } catch (InterruptedException e) {
                // finished in time
            }
        });
        watchdog.setDaemon(true);
        watchdog.start();

        long bytes = 0;
        long funcs = 0;
        long t0 = System.nanoTime();
        long decodeNanos = 0;
        for (Path p : files) {
            byte[] data = Files.readAllBytes(p);
            boolean bad = p.getFileName().toString().startsWith("bad-");
            if (bad) {
                expectFailure(p, data);
                continue;
            }
            bytes += data.length;
            long d0 = System.nanoTime();
            GirFile f;
            try {
                f = GirFile.decode(data);
                f.validate();
            } catch (GirFormatException e) {
                fail(p + ": " + e.getMessage());
                continue;
            }
            decodeNanos += System.nanoTime() - d0;
            funcs += f.sections().funcs.length;
            roundTrip(p, f, data);
            checkDataRange(p, f);
        }
        long wall = System.nanoTime() - t0;
        if (stats) {
            System.out.printf("java: %d files, %d bytes, %d funcs; decode+validate %.1f ms (%.1f MB/s); total with re-encode %.1f ms%n",
                            files.size(), bytes, funcs, decodeNanos / 1e6, bytes / (decodeNanos / 1e9) / 1e6, wall / 1e6);
        }
        if (fuzz > 0) {
            Random rnd = new Random(seed);
            long accepted = 0;
            long rejected = 0;
            for (Path p : files) {
                byte[] data = Files.readAllBytes(p);
                for (int i = 0; i < fuzz; i++) {
                    byte[] m = mutate(data, rnd);
                    switch (fuzzOne(p, i, m)) {
                        case 1 -> accepted++;
                        case 0 -> rejected++;
                        default -> {
                            // failure already reported
                        }
                    }
                }
            }
            System.out.printf("fuzz: %d mutants rejected cleanly, %d accepted (and re-encoded identically)%n", rejected, accepted);
        }
        watchdog.interrupt();
        if (failures > 0) {
            System.err.println(failures + " failure(s)");
            System.exit(1);
        }
        System.out.println("ok: " + files.size() + " GIR files");
    }

    /** dataRange is exactly the slice of data() and refuses ranges outside it. */
    private static void checkDataRange(Path p, GirFile f) {
        byte[] all = f.data();
        int n = all.length;
        if (!Arrays.equals(f.dataRange(0, n), all)) {
            fail(p + ": dataRange(0, len) differs from data()");
        }
        if (n > 1 && !Arrays.equals(f.dataRange(1, n - 1), Arrays.copyOfRange(all, 1, n))) {
            fail(p + ": dataRange(1, len-1) differs from data()[1:]");
        }
        if (f.dataRange(0, n + 1) != null || f.dataRange(n + 1, 0) != null || f.dataRange(-1, 1) != null
                || f.dataRange(1, Long.MAX_VALUE) != null) {
            fail(p + ": dataRange accepted a range outside the DATA section");
        }
    }

    private static void collect(Path p, List<Path> out) throws IOException {
        if (Files.isDirectory(p)) {
            try (Stream<Path> s = Files.walk(p)) {
                s.filter(f -> f.toString().endsWith(".gir")).sorted().forEach(out::add);
            }
        } else {
            out.add(p);
        }
    }

    private static void expectFailure(Path p, byte[] data) {
        try {
            GirFile f = GirFile.decode(data);
            f.validate();
            fail(p + ": malformed vector decoded without error");
        } catch (GirFormatException e) {
            // expected
        } catch (Throwable t) {
            fail(p + ": malformed vector threw " + t);
        }
    }

    private static void roundTrip(Path p, GirFile f, byte[] data) {
        for (int i = 0; i < f.sections().funcs.length; i++) {
            Gir.FuncBody b = f.body(i);
            if (b == null) {
                continue;
            }
            byte[] orig = f.bodyBytes(i);
            byte[] again = GirFile.encodeBody(b);
            if (!Arrays.equals(orig, again)) {
                fail(p + ": body of func " + i + " (" + f.string(f.sections().funcs[i].name()) + ") re-encodes differently");
            }
        }
        byte[] again = f.encode();
        if (!Arrays.equals(data, again)) {
            int at = Arrays.mismatch(data, again);
            fail(p + ": file re-encodes differently (first difference at byte " + at + " of " + data.length + ")");
        }
    }

    /** Returns 1 accepted, 0 rejected, -1 failure. */
    private static int fuzzOne(Path p, int i, byte[] m) {
        try {
            GirFile f = GirFile.decode(m);
            f.validate();
            byte[] again = f.encode();
            if (!Arrays.equals(m, again)) {
                fail(p + " mutant " + i + ": accepted but re-encodes differently");
                return -1;
            }
            return 1;
        } catch (GirFormatException e) {
            return 0;
        } catch (Throwable t) {
            fail(p + " mutant " + i + ": decoder threw " + t);
            t.printStackTrace();
            return -1;
        }
    }

    private static byte[] mutate(byte[] data, Random rnd) {
        byte[] m = data.clone();
        int n = data.length;
        switch (rnd.nextInt(6)) {
            case 0 -> { // flip one byte
                int i = rnd.nextInt(n);
                m[i] = (byte) rnd.nextInt(256);
            }
            case 1 -> { // flip a few bits
                for (int k = 0; k < 1 + rnd.nextInt(4); k++) {
                    int i = rnd.nextInt(n);
                    m[i] ^= (byte) (1 << rnd.nextInt(8));
                }
            }
            case 2 -> m = Arrays.copyOf(data, rnd.nextInt(n)); // truncate
            case 3 -> { // delete a span
                int i = rnd.nextInt(n);
                int len = Math.min(n - i, 1 + rnd.nextInt(16));
                byte[] r = new byte[n - len];
                System.arraycopy(data, 0, r, 0, i);
                System.arraycopy(data, i + len, r, i, n - i - len);
                m = r;
            }
            case 4 -> { // duplicate a span
                int i = rnd.nextInt(n);
                int len = Math.min(n - i, 1 + rnd.nextInt(16));
                byte[] r = new byte[n + len];
                System.arraycopy(data, 0, r, 0, i + len);
                System.arraycopy(data, i, r, i + len, n - i);
                m = r;
            }
            default -> { // large varint
                int i = rnd.nextInt(n);
                m[i] = (byte) 0xff;
                if (i + 1 < n) {
                    m[i + 1] = (byte) 0xff;
                }
            }
        }
        return m;
    }
}
