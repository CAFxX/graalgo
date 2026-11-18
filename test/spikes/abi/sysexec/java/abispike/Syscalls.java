package abispike;

import abispike.Mem.Addr;
import abispike.Mem.Blob;
import abispike.Mem.UnsafeTrap;
import abispike.SyscallTable.Dir;
import abispike.SyscallTable.Filled;
import abispike.Values.Layout;
import abispike.Values.Leaf;
import abispike.Values.LeafKind;
import java.lang.foreign.Arena;
import java.lang.foreign.FunctionDescriptor;
import java.lang.foreign.Linker;
import java.lang.foreign.MemorySegment;
import java.lang.foreign.StructLayout;
import java.lang.foreign.ValueLayout;
import java.lang.invoke.MethodHandle;
import java.lang.invoke.VarHandle;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.function.LongConsumer;

/**
 * The raw-syscall intrinsic: the Java body of
 * {@code syscall.RawSyscall6(trap, a1..a6 uintptr) (r1, r2 uintptr, err Errno)}.
 *
 * <p>Arguments arrive as the integers Go passed. For positions the table
 * marks as pointers, the integer is resolved through the handle table to
 * {@code (object, offset)}, the pointee is copied into native memory of a
 * call-scoped arena according to its Go type's layout (following nested
 * pointers by rule), libc's {@code syscall(2)} is called through one Panama
 * downcall handle, and OUT data is copied back. No Go object is pinned and
 * no native address outlives the call.
 */
public final class Syscalls {
    public record Result(long r1, long r2, long errno) {}

    private static final MethodHandle SYSCALL;
    private static final StructLayout CAPTURE = Linker.Option.captureStateLayout();
    private static final VarHandle ERRNO = CAPTURE.varHandle(java.lang.foreign.MemoryLayout.PathElement.groupElement("errno"));

    static {
        // The one restricted call of the executor; the launcher grants it with --enable-native-access.
        SYSCALL = link();
    }

    @SuppressWarnings("restricted")
    private static MethodHandle link() {
        Linker linker = Linker.nativeLinker();
        ValueLayout.OfLong w = ValueLayout.JAVA_LONG;
        // One descriptor for every system call: long syscall(long nr, ...six words).
        return linker.downcallHandle(linker.defaultLookup().find("syscall").orElseThrow(),
                FunctionDescriptor.of(w, w, w, w, w, w, w, w),
                Linker.Option.firstVariadicArg(1), Linker.Option.captureCallState("errno"));
    }

    private final TypeTab tab;
    private final SyscallTable table;
    private final HandleTable handles;
    private final Map<String, Layout> layouts = new HashMap<>();
    /** Calls the table marks as possibly blocking: where the scheduler slot is released (runtime.entersyscall). */
    public long blockingCalls;

    public Syscalls(TypeTab tab, SyscallTable table, HandleTable handles) {
        this.tab = tab;
        this.table = table;
        this.handles = handles;
    }

    private Layout layout(String type) {
        return layouts.computeIfAbsent(type, t -> new Layout(tab, SyscallTable.type(tab, t)));
    }

    public Result syscall6(long trap, long a1, long a2, long a3, long a4, long a5, long a6) {
        SyscallTable.Entry e = table.get(trap);
        long[] go = {a1, a2, a3, a4, a5, a6};
        long[] nat = go.clone();
        List<LongConsumer> after = new ArrayList<>();
        try (Arena arena = Arena.ofConfined()) {
            for (int i = 0; i < e.roles().size(); i++) {
                final long word = go[i];
                switch (e.roles().get(i)) {
                    case SyscallTable.Val v -> { }
                    case SyscallTable.CStr c -> nat[i] = cstring(arena, handles.pointer(word)).address();
                    case SyscallTable.Bytes b -> {
                        long len = go[b.lenArg()];
                        if (word == 0) {
                            break; // nil buffer: the kernel gets NULL, as from Go
                        }
                        Addr p = handles.pointer(word);
                        MemorySegment seg = arena.allocate(Math.max(len, 1));
                        if (b.dir() != Dir.OUT) {
                            copyBytes(p, seg, len);
                        } else {
                            checkBytes(p, len);
                        }
                        if (b.dir() != Dir.IN) {
                            after.add(r -> copyBytesBack(seg, p, b.filled() == Filled.RESULT ? Math.clamp(r, 0, len) : len));
                        }
                        nat[i] = seg.address();
                    }
                    case SyscallTable.Struct s -> {
                        if (word == 0) {
                            if (!s.nullable()) {
                                throw new UnsafeTrap(e.name() + ": nil pointer for argument " + (i + 1));
                            }
                            break;
                        }
                        Addr p = handles.pointer(word);
                        Layout l = layout(s.type());
                        MemorySegment seg = arena.allocate(l.size, 8);
                        marshal(arena, p, s.type(), l, seg, 0, s.dir(), s.nested(), after);
                        nat[i] = seg.address();
                    }
                    case SyscallTable.Array ar -> {
                        long count = go[ar.countArg()];
                        if (word == 0) {
                            break;
                        }
                        Addr p = handles.pointer(word);
                        Layout l = layout(ar.elemType());
                        MemorySegment seg = arena.allocate(Math.max(count * l.size, 1), 8);
                        for (long k = 0; k < count; k++) {
                            final long limit = k;
                            List<LongConsumer> elemAfter = new ArrayList<>();
                            marshal(arena, p.add(k * l.size), ar.elemType(), l, seg, k * l.size, ar.dir(), ar.nested(), elemAfter);
                            for (LongConsumer c : elemAfter) {
                                // Only the elements the kernel reports as filled are copied back.
                                after.add(r -> {
                                    if (ar.filled() == Filled.ALL || limit < r) {
                                        c.accept(r);
                                    }
                                });
                            }
                        }
                        nat[i] = seg.address();
                    }
                }
            }
            if (e.mayBlock()) {
                blockingCalls++;
            }
            MemorySegment capture = arena.allocate(CAPTURE);
            long r;
            try {
                r = (long) SYSCALL.invokeExact(capture, trap, nat[0], nat[1], nat[2], nat[3], nat[4], nat[5]);
            } catch (Throwable t) {
                throw new IllegalStateException(t);
            }
            if (r == -1) {
                // Go's contract: r1 = -1 (all ones), err = errno; nothing is copied back.
                return new Result(-1, 0, (int) ERRNO.get(capture, 0L));
            }
            for (LongConsumer c : after) {
                c.accept(r);
            }
            return new Result(r, 0, 0);
        }
    }

    /** Copies one Go value of the given layout to native memory, leaf by leaf, following pointer fields by rule. */
    private void marshal(Arena arena, Addr p, String type, Layout l, MemorySegment seg, long segOff, Dir dir, Dir nestedDir,
            List<LongConsumer> after) {
        for (Leaf leaf : l.leaves) {
            long at = segOff + leaf.off();
            switch (leaf.kind()) {
                case SCALAR -> {
                    if (dir != Dir.OUT) {
                        put(seg, at, leaf.size(), p.base().loadBits(p.off() + leaf.off(), leaf.size()));
                    }
                    if (dir != Dir.IN) {
                        after.add(r -> p.base().storeBits(p.off() + leaf.off(), leaf.size(), get(seg, at, leaf.size())));
                    }
                }
                case REF -> {
                    SyscallTable.Nested rule = null;
                    for (SyscallTable.Nested n : table.nestedOf(type)) {
                        if (n.ptrOff() == leaf.off()) {
                            rule = n;
                        }
                    }
                    if (rule == null) {
                        throw new UnsafeTrap("syscall argument type " + type + " has a pointer at offset " + leaf.off()
                                + " and the table has no rule for it");
                    }
                    Addr target = (Addr) p.base().loadRef(p.off() + leaf.off());
                    if (target == null) {
                        break; // NULL stays NULL (zero-filled segment)
                    }
                    long n = p.base().loadBits(p.off() + rule.lenOff(), rule.lenSize());
                    MemorySegment nseg;
                    if (rule.elemType() == null) {
                        nseg = arena.allocate(Math.max(n, 1));
                        // Copy in even for OUT: the kernel may fill only a prefix, and the rest must read back unchanged.
                        copyBytes(target, nseg, n);
                        if (nestedDir != Dir.IN) {
                            after.add(r -> copyBytesBack(nseg, target, n));
                        }
                    } else {
                        Layout el = layout(rule.elemType());
                        nseg = arena.allocate(Math.max(n * el.size, 1), 8);
                        for (long k = 0; k < n; k++) {
                            marshal(arena, target.add(k * el.size), rule.elemType(), el, nseg, k * el.size, Dir.IN, nestedDir, after);
                        }
                    }
                    seg.set(ValueLayout.JAVA_LONG_UNALIGNED, at, nseg.address());
                }
                default -> throw new UnsafeTrap("syscall argument type " + type + " contains a Go " + leaf.kind());
            }
        }
    }

    private static void put(MemorySegment seg, long at, int size, long bits) {
        switch (size) {
            case 1 -> seg.set(ValueLayout.JAVA_BYTE, at, (byte) bits);
            case 2 -> seg.set(ValueLayout.JAVA_SHORT_UNALIGNED, at, (short) bits);
            case 4 -> seg.set(ValueLayout.JAVA_INT_UNALIGNED, at, (int) bits);
            case 8 -> seg.set(ValueLayout.JAVA_LONG_UNALIGNED, at, bits);
            default -> throw new UnsafeTrap("scalar of " + size + " bytes");
        }
    }

    private static long get(MemorySegment seg, long at, int size) {
        return switch (size) {
            case 1 -> seg.get(ValueLayout.JAVA_BYTE, at);
            case 2 -> seg.get(ValueLayout.JAVA_SHORT_UNALIGNED, at);
            case 4 -> seg.get(ValueLayout.JAVA_INT_UNALIGNED, at);
            case 8 -> seg.get(ValueLayout.JAVA_LONG_UNALIGNED, at);
            default -> throw new UnsafeTrap("scalar of " + size + " bytes");
        };
    }

    private static void checkBytes(Addr p, long len) {
        if (p.base() instanceof Blob b && (p.off() < 0 || p.off() + len > b.data.length)) {
            throw new UnsafeTrap("buffer of " + len + " bytes at offset " + p.off() + " exceeds " + b.label + " (" + b.data.length + ")");
        }
    }

    private static void copyBytes(Addr p, MemorySegment seg, long len) {
        checkBytes(p, len);
        if (p.base() instanceof Blob b) {
            MemorySegment.copy(b.data, (int) p.off(), seg, ValueLayout.JAVA_BYTE, 0, (int) len);
            return;
        }
        for (long i = 0; i < len; i++) {
            seg.set(ValueLayout.JAVA_BYTE, i, (byte) p.base().loadBits(p.off() + i, 1));
        }
    }

    private static void copyBytesBack(MemorySegment seg, Addr p, long len) {
        if (p.base() instanceof Blob b) {
            MemorySegment.copy(seg, ValueLayout.JAVA_BYTE, 0, b.data, (int) p.off(), (int) len);
            return;
        }
        for (long i = 0; i < len; i++) {
            p.base().storeBits(p.off() + i, 1, seg.get(ValueLayout.JAVA_BYTE, i));
        }
    }

    private static MemorySegment cstring(Arena arena, Addr p) {
        int n = 0;
        while (p.u8(n) != 0) { // traps at the end of the object if there is no NUL
            n++;
        }
        MemorySegment seg = arena.allocate(n + 1);
        copyBytes(p, seg, n);
        return seg;
    }
}
