package usched;

/** Go panic model (minimal, for the spike). */
public final class GoPanic extends RuntimeException {
    public GoPanic(String msg) { super(msg); }
    private GoPanic() { super(null, null, false, false); }
}
