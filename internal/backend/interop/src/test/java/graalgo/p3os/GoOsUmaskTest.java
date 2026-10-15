package graalgo.p3os;

import java.io.IOException;
import java.lang.foreign.FunctionDescriptor;
import java.lang.foreign.Linker;
import java.lang.foreign.SymbolLookup;
import java.lang.foreign.ValueLayout;
import java.lang.invoke.MethodHandle;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.attribute.PosixFilePermission;
import java.util.ArrayList;
import java.util.List;
import java.util.Set;

import graalgo.values.GoString;

/**
 * Unit tests for P1-R5-5: the process umask (read once from libc via
 * Panama FFM, cached in {@link Umask}) is applied as {@code perm &^ umask}
 * on the create paths ({@code GoOs.open} with O_CREAT, {@code GoOs.mkdir}),
 * exactly like gc. {@code GoOs.chmod} is pinned to keep the exact bits
 * (chmod is not a creation call and gc does not mask it).
 *
 * <p>The expected mask is read with an independent FFM downcall in the
 * test itself, so the tests hold under any ambient umask and also verify
 * that {@link Umask}'s set/restore read left the live process mask intact.
 */
public final class GoOsUmaskTest {
    // Linux open(2) flags, mirroring the synthetic os package.
    private static final int O_WRONLY = 1;
    private static final int O_CREAT = 64;
    private static final int O_TRUNC = 512;

    private static Path scratch;

    private static void initScratch() throws IOException {
        GoOs.init(new String[0]);
        String tmpdir = System.getenv("TMPDIR");
        Path base = tmpdir != null
                ? Path.of(tmpdir, "junit")
                : Path.of(System.getProperty("user.dir"), "junit-scratch");
        Files.createDirectories(base);
        scratch = Files.createTempDirectory(base, "goos-umask-");
    }

    private static void cleanScratch() throws IOException {
        if (scratch == null) {
            return;
        }
        List<Path> paths = new ArrayList<>();
        try (var stream = Files.walk(scratch)) {
            stream.forEach(paths::add);
        }
        paths.sort((a, b) -> b.compareTo(a));
        for (Path p : paths) {
            Files.deleteIfExists(p);
        }
        scratch = null;
    }

    private static Object gs(String s) {
        return GoString.fromUtf8(s);
    }

    /** Reads the live process umask with an independent FFM downcall. */
    private static int nativeUmask() throws Throwable {
        Linker linker = Linker.nativeLinker();
        SymbolLookup libc = linker.defaultLookup();
        MethodHandle umask = linker.downcallHandle(
                libc.find("umask").orElseThrow(UnsatisfiedLinkError::new),
                FunctionDescriptor.of(ValueLayout.JAVA_INT, ValueLayout.JAVA_INT));
        int old = (int) umask.invokeExact(0);
        try {
            return old;
        } finally {
            // Restore immediately (bind the int result: see Umask.java).
            int restored = (int) umask.invokeExact(old);
            if (restored != 0) {
                throw new IllegalStateException("umask restore failed");
            }
        }
    }

    private static int modeBits(Path p) throws IOException {
        int bits = 0;
        Set<PosixFilePermission> s = Files.getPosixFilePermissions(p);
        if (s.contains(PosixFilePermission.OWNER_READ)) bits |= 0400;
        if (s.contains(PosixFilePermission.OWNER_WRITE)) bits |= 0200;
        if (s.contains(PosixFilePermission.OWNER_EXECUTE)) bits |= 0100;
        if (s.contains(PosixFilePermission.GROUP_READ)) bits |= 040;
        if (s.contains(PosixFilePermission.GROUP_WRITE)) bits |= 020;
        if (s.contains(PosixFilePermission.GROUP_EXECUTE)) bits |= 010;
        if (s.contains(PosixFilePermission.OTHERS_READ)) bits |= 04;
        if (s.contains(PosixFilePermission.OTHERS_WRITE)) bits |= 02;
        if (s.contains(PosixFilePermission.OTHERS_EXECUTE)) bits |= 01;
        return bits;
    }

    private static void checkEq(int want, int got, String what) {
        if (want != got) {
            throw new AssertionError(what + ": want 0" + Integer.toOctalString(want)
                    + " got 0" + Integer.toOctalString(got));
        }
    }

    public void testUmaskCacheMatchesNative() throws Throwable {
        initScratch();
        try {
            checkEq(nativeUmask(), Umask.MASK, "cached umask vs live umask");
        } finally {
            cleanScratch();
        }
    }

    public void testOpenCreateAppliesUmask() throws Throwable {
        initScratch();
        try {
            int mask = nativeUmask();
            Path f = scratch.resolve("created.txt");
            long h = GoOs.open(gs(f.toString()), O_WRONLY | O_CREAT | O_TRUNC, 0666);
            if (h < 0) {
                throw new AssertionError("open failed: " + h);
            }
            GoOs.close(h);
            checkEq(0666 & ~mask, modeBits(f), "open-create mode");
        } finally {
            cleanScratch();
        }
    }

    public void testMkdirAppliesUmask() throws Throwable {
        initScratch();
        try {
            int mask = nativeUmask();
            Path d = scratch.resolve("created-dir");
            long r = GoOs.mkdir(gs(d.toString()), 0777);
            if (r != 0) {
                throw new AssertionError("mkdir failed: " + r);
            }
            checkEq(0777 & ~mask, modeBits(d), "mkdir mode");
        } finally {
            cleanScratch();
        }
    }

    public void testChmodKeepsExactBits() throws Throwable {
        initScratch();
        try {
            int mask = nativeUmask();
            Path f = scratch.resolve("chmod.txt");
            long h = GoOs.open(gs(f.toString()), O_WRONLY | O_CREAT | O_TRUNC, 0666);
            if (h < 0) {
                throw new AssertionError("open failed: " + h);
            }
            GoOs.close(h);
            long r = GoOs.chmod(gs(f.toString()), 0600 | (mask & 0777));
            if (r != 0) {
                throw new AssertionError("chmod failed: " + r);
            }
            // chmod must NOT mask: bits that umask would clear stay set.
            checkEq(0600 | (mask & 0777), modeBits(f), "chmod mode");
        } finally {
            cleanScratch();
        }
    }
}
