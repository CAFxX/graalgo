package work;

/** Anti-dead-code sink. */
public final class Blackhole {
    public static volatile long sink;
    public static void consume(long v) { sink = v; }
    private Blackhole() {}
}
