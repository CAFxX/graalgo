package loomsim;

/** RuntimeException carrying a Go panic value (prototype only). */
public final class GoPanic extends RuntimeException {
    public GoPanic(String message) { super(message); }
}
