package graalgo.gir;

/** Malformed GIR input. The decoder throws nothing else for any input bytes. */
public final class GirFormatException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    private final long offset;

    GirFormatException(long offset, String message) {
        super("gir: offset " + offset + ": " + message);
        this.offset = offset;
    }

    /** Byte offset in the file where decoding failed. */
    public long offset() {
        return offset;
    }
}
