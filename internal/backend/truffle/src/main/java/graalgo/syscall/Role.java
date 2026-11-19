package graalgo.syscall;

/**
 * What one argument word of a system call is (internal/toolexec/syscalls/table.txt): an
 * integer, a pointer to NUL-terminated bytes, a pointer to a byte buffer whose length is
 * another argument, or a pointer to one pointer-free Go value of a fixed size (a struct role).
 * The only size a role states is a struct role's, which the table gate checks against the Go
 * type at every standard-library site; buffer lengths are argument values, and every range is
 * bounded by the Go object the pointer points into.
 */
final class Role {
    static final int K_VAL = 0, K_CSTR = 1, K_BYTES = 2, K_STRUCT = 3;

    static final Role VAL = new Role(K_VAL, -1, false, false);
    static final Role CSTR = new Role(K_CSTR, -1, false, false);

    final int kind;
    /** Bytes: the 0-based index of the length argument. */
    final int lenArg;
    /** Bytes: the kernel writes the buffer (copied back after the call). */
    final boolean out;
    /** Bytes out: the kernel fills only as many bytes as the call returns. */
    final boolean filledResult;
    /** Struct: the size of the value in bytes. */
    final long size;

    private Role(int kind, int lenArg, boolean out, boolean filledResult, long size) {
        this.kind = kind;
        this.lenArg = lenArg;
        this.out = out;
        this.filledResult = filledResult;
        this.size = size;
    }

    private Role(int kind, int lenArg, boolean out, boolean filledResult) {
        this(kind, lenArg, out, filledResult, 0);
    }

    static Role bytes(int lenArg, boolean out, boolean filledResult) {
        return new Role(K_BYTES, lenArg, out, filledResult);
    }

    /** A pointer to a pointer-free value of {@code size} bytes; {@code out}: the kernel writes it. */
    static Role struct(long size, boolean out) {
        return new Role(K_STRUCT, -1, out, false, size);
    }
}
