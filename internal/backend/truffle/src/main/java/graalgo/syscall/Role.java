package graalgo.syscall;

/**
 * What one argument word of a system call is (internal/toolexec/syscalls/table.txt): an
 * integer, a pointer to NUL-terminated bytes, or a pointer to a byte buffer whose length is
 * another argument. A role never states a size or an offset; lengths are argument values and
 * bounds come from the Go object the pointer points into.
 */
final class Role {
    static final int K_VAL = 0, K_CSTR = 1, K_BYTES = 2;

    static final Role VAL = new Role(K_VAL, -1, false, false);
    static final Role CSTR = new Role(K_CSTR, -1, false, false);

    final int kind;
    /** Bytes: the 0-based index of the length argument. */
    final int lenArg;
    /** Bytes: the kernel writes the buffer (copied back after the call). */
    final boolean out;
    /** Bytes out: the kernel fills only as many bytes as the call returns. */
    final boolean filledResult;

    private Role(int kind, int lenArg, boolean out, boolean filledResult) {
        this.kind = kind;
        this.lenArg = lenArg;
        this.out = out;
        this.filledResult = filledResult;
    }

    static Role bytes(int lenArg, boolean out, boolean filledResult) {
        return new Role(K_BYTES, lenArg, out, filledResult);
    }
}
