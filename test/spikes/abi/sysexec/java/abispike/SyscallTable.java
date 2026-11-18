package abispike;

import java.util.HashMap;
import java.util.List;
import java.util.Map;

/**
 * The per-syscall-number argument-role table (toolexec-design §5.6): what
 * each of the six words of a raw system call is, so that the executor can
 * turn handle-table pointers into native memory and back. Layouts are not
 * in this table: a role names a Go type and the layout comes from the type
 * table.
 */
public final class SyscallTable {
    public enum Dir { IN, OUT, INOUT }

    /** How many units of an OUT buffer the kernel filled. */
    public enum Filled { ALL, RESULT }

    public sealed interface Role permits Val, CStr, Bytes, Struct, Array {}

    /** A plain integer (fd, flags, length, offset). */
    public record Val() implements Role {}

    /** Pointer to NUL-terminated bytes (paths). */
    public record CStr() implements Role {}

    /** Pointer to {@code arg[lenArg]} bytes. */
    public record Bytes(int lenArg, Dir dir, Filled filled) implements Role {}

    /** Pointer to one value of a Go type; may be nil if {@code nullable}. Nested pointers follow the type's rules. */
    public record Struct(String type, Dir dir, Dir nested, boolean nullable) implements Role {}

    /** Pointer to {@code arg[countArg]} consecutive values of a Go type. */
    public record Array(String elemType, int countArg, Dir dir, Dir nested, Filled filled) implements Role {}

    /**
     * A pointer-typed field inside an argument struct: where the pointer
     * and its extent are. {@code elemType == null} means bytes;
     * {@code lenSize} is the width of the length/count field.
     */
    public record Nested(String field, long ptrOff, long lenOff, int lenSize, String elemType) {}

    public record Entry(int nr, String name, boolean mayBlock, List<Role> roles) {}

    private final Map<Integer, Entry> entries = new HashMap<>();
    private final Map<String, List<Nested>> nested = new HashMap<>();

    public Entry get(long nr) {
        Entry e = entries.get((int) nr);
        if (e == null || nr != e.nr()) {
            // GG-0062 spirit: never guess. An unknown number would pass handle integers to the kernel as addresses.
            throw new UnsupportedOperationException("syscall " + nr + " is not in the graalgo syscall table");
        }
        return e;
    }

    public List<Nested> nestedOf(String type) {
        return nested.getOrDefault(type, List.of());
    }

    private void add(int nr, String name, boolean mayBlock, Role... roles) {
        entries.put(nr, new Entry(nr, name, mayBlock, List.of(roles)));
    }

    private static final Val V = new Val();

    /** linux/amd64 numbers; the representative set of the prototype (17 of std's 149). */
    public static SyscallTable linuxAmd64(TypeTab tab) {
        SyscallTable t = new SyscallTable();
        // The complete nested-pointer inventory of std's syscall argument types (sysscan): Iovec and Msghdr.
        long iovLen = fieldOff(tab, "syscall.Iovec", "Len");
        t.nested.put("syscall.Iovec", List.of(new Nested("Base", fieldOff(tab, "syscall.Iovec", "Base"), iovLen, 8, null)));
        t.nested.put("syscall.Msghdr", List.of(
                new Nested("Name", fieldOff(tab, "syscall.Msghdr", "Name"), fieldOff(tab, "syscall.Msghdr", "Namelen"), 4, null),
                new Nested("Iov", fieldOff(tab, "syscall.Msghdr", "Iov"), fieldOff(tab, "syscall.Msghdr", "Iovlen"), 8, "syscall.Iovec"),
                new Nested("Control", fieldOff(tab, "syscall.Msghdr", "Control"), fieldOff(tab, "syscall.Msghdr", "Controllen"), 8, null)));

        t.add(0, "read", true, V, new Bytes(2, Dir.OUT, Filled.RESULT), V);
        t.add(1, "write", true, V, new Bytes(2, Dir.IN, Filled.ALL), V);
        t.add(3, "close", false, V);
        t.add(5, "fstat", false, V, new Struct("syscall.Stat_t", Dir.OUT, Dir.IN, false));
        t.add(19, "readv", true, V, new Array("syscall.Iovec", 2, Dir.IN, Dir.OUT, Filled.ALL), V);
        t.add(20, "writev", true, V, new Array("syscall.Iovec", 2, Dir.IN, Dir.IN, Filled.ALL), V);
        t.add(39, "getpid", false);
        t.add(46, "sendmsg", true, V, new Struct("syscall.Msghdr", Dir.IN, Dir.IN, false), V);
        t.add(47, "recvmsg", true, V, new Struct("syscall.Msghdr", Dir.INOUT, Dir.OUT, false), V);
        t.add(53, "socketpair", false, V, V, V, new Struct("[2]int32", Dir.OUT, Dir.IN, false));
        t.add(217, "getdents64", false, V, new Bytes(2, Dir.OUT, Filled.RESULT), V);
        t.add(228, "clock_gettime", false, V, new Struct("syscall.Timespec", Dir.OUT, Dir.IN, false));
        t.add(232, "epoll_wait", true, V, new Array("syscall.EpollEvent", 2, Dir.OUT, Dir.IN, Filled.RESULT), V, V);
        t.add(233, "epoll_ctl", false, V, V, V, new Struct("syscall.EpollEvent", Dir.IN, Dir.IN, true));
        t.add(257, "openat", true, V, new CStr(), V, V);
        t.add(291, "epoll_create1", false, V);
        t.add(293, "pipe2", false, new Struct("[2]int32", Dir.OUT, Dir.IN, false), V);
        return t;
    }

    static TypeTab.Type type(TypeTab tab, String str) {
        for (TypeTab.Type t : tab.types) {
            if (t.str.equals(str)) {
                return t;
            }
        }
        throw new IllegalArgumentException("type table has no " + str);
    }

    static long fieldOff(TypeTab tab, String type, String field) {
        for (TypeTab.Field f : type(tab, type).fields) {
            if (f.name.equals(field)) {
                return f.offset;
            }
        }
        throw new IllegalArgumentException(type + " has no field " + field);
    }
}
