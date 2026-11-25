package work;

/** Blackhole against dead-code elimination in spinner loops. */
public final class Blackhole {
    static volatile long sink;
    public static void consume(long v) { sink = v; }
    private Blackhole() {}
}
