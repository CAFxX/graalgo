package graalgo.syscall;

import java.io.IOException;
import java.lang.foreign.Arena;
import java.lang.foreign.FunctionDescriptor;
import java.lang.foreign.Linker;
import java.lang.foreign.MemoryLayout;
import java.lang.foreign.MemorySegment;
import java.lang.foreign.StructLayout;
import java.lang.foreign.ValueLayout;
import java.lang.invoke.MethodHandle;
import java.lang.invoke.VarHandle;

/**
 * The process-wide standard streams and working directory, for an embedder that runs programs one
 * after another in one JVM ({@code graalgo.launcher.BundleBatchMain}, the test gates' batch
 * runner): a program's system calls reach the kernel's file descriptors 0, 1 and 2 and its
 * working directory directly ({@link Executor}), so running it "with its stdin, stdout, stderr
 * and directory" means pointing those at its files before its context starts, as a shell does
 * for a child process. Never called by a running program: the caller owns the process between
 * two programs.
 */
public final class ProcessStdio {
    private ProcessStdio() {}

    /** {@code O_RDONLY}. */
    public static final int O_RDONLY = 0;
    /** {@code O_WRONLY | O_CREAT | O_TRUNC} (linux values). */
    public static final int O_WRITE_TRUNC = 01 | 0100 | 01000;
    private static final int O_CLOEXEC = 02000000;

    private static final Linker LINKER = Linker.nativeLinker();
    private static final StructLayout CAPTURE = Linker.Option.captureStateLayout();
    private static final VarHandle ERRNO = CAPTURE.varHandle(MemoryLayout.PathElement.groupElement("errno"));
    private static final MethodHandle OPEN;
    private static final MethodHandle DUP;
    private static final MethodHandle DUP2;
    private static final MethodHandle CLOSE;
    private static final MethodHandle CHDIR;

    static {
        ValueLayout.OfInt i = ValueLayout.JAVA_INT;
        Linker.Option errno = Linker.Option.captureCallState("errno");
        OPEN = handle("open", FunctionDescriptor.of(i, ValueLayout.ADDRESS, i, i),
                Linker.Option.firstVariadicArg(2), errno);
        DUP = handle("dup", FunctionDescriptor.of(i, i), errno);
        DUP2 = handle("dup2", FunctionDescriptor.of(i, i, i), errno);
        CLOSE = handle("close", FunctionDescriptor.of(i, i), errno);
        CHDIR = handle("chdir", FunctionDescriptor.of(i, ValueLayout.ADDRESS), errno);
    }

    private static MethodHandle handle(String name, FunctionDescriptor d, Linker.Option... options) {
        return LINKER.downcallHandle(LINKER.defaultLookup().find(name).orElseThrow(), d, options);
    }

    /** {@code open(path, flags | O_CLOEXEC, 0644)}: a new descriptor of {@code path}. */
    public static int open(String path, int flags) throws IOException {
        try (Arena a = Arena.ofConfined()) {
            MemorySegment st = a.allocate(CAPTURE);
            int fd = (int) OPEN.invokeExact(st, a.allocateFrom(path), flags | O_CLOEXEC, 0644);
            return check(fd, st, "open " + path);
        } catch (IOException | RuntimeException | Error e) {
            throw e;
        } catch (Throwable t) {
            throw new IllegalStateException(t);
        }
    }

    /** {@code dup(fd)}. */
    public static int dup(int fd) throws IOException {
        return call(DUP, fd, -1, "dup");
    }

    /** {@code dup2(from, to)}: descriptor {@code to} now refers to {@code from}'s file. */
    public static void dup2(int from, int to) throws IOException {
        call(DUP2, from, to, "dup2");
    }

    /** {@code close(fd)}. */
    public static void close(int fd) throws IOException {
        call(CLOSE, fd, -1, "close");
    }

    /** {@code chdir(path)}: the process's working directory. */
    public static void chdir(String path) throws IOException {
        try (Arena a = Arena.ofConfined()) {
            MemorySegment st = a.allocate(CAPTURE);
            int r = (int) CHDIR.invokeExact(st, a.allocateFrom(path));
            check(r, st, "chdir " + path);
        } catch (IOException | RuntimeException | Error e) {
            throw e;
        } catch (Throwable t) {
            throw new IllegalStateException(t);
        }
    }

    private static int call(MethodHandle h, int a, int b, String what) throws IOException {
        try (Arena arena = Arena.ofConfined()) {
            MemorySegment st = arena.allocate(CAPTURE);
            int r = b < 0 ? (int) h.invokeExact(st, a) : (int) h.invokeExact(st, a, b);
            return check(r, st, what);
        } catch (IOException | RuntimeException | Error e) {
            throw e;
        } catch (Throwable t) {
            throw new IllegalStateException(t);
        }
    }

    private static int check(int r, MemorySegment st, String what) throws IOException {
        if (r < 0) {
            throw new IOException(what + ": errno " + (int) ERRNO.get(st, 0L));
        }
        return r;
    }
}
