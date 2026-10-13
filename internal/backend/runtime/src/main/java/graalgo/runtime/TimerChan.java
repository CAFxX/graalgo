package graalgo.runtime;

/**
 * Timer-channel flavor of {@link GoChan} (P3-M2a; resolves DIV-M3-1).
 *
 * <p>gc presents {@code time.Timer}/{@code time.Ticker} channels as
 * unbuffered: {@code len(t.C)} and {@code cap(t.C)} always report 0,
 * even while a fire value sits physically buffered — the runtime's
 * non-blocking value injection into the timer channel is not counted.
 * The real channel underneath still needs a 1-slot buffer (fire values
 * must remain deliverable to {@code recv}/select/close-drain), so the
 * masking lives here, in the observation probes only:
 * {@link #buffered()} and {@link #capacity()} both report 0 while
 * {@code trySend}/{@code tryRecv}/send/recv/close/select operate on the
 * real buffer unchanged.
 *
 * <p>Safe because the {@code GoChan} contract holds: {@code buffered()}
 * is called only from {@code ChanOps.len}, {@code capacity()} only from
 * {@code ChanOps.cap}; all internal logic (send/recv/select/waiters)
 * uses the buffer fields directly (see {@code GoChan} class javadoc).
 * {@code instanceof GoChan} checks ({@code requireGoChan}) still pass.
 *
 * <p>verifies: GG-0048 (timer slice)
 */
public class TimerChan extends GoChan {
    /** Timer channels carry the fire nanotime boxed as {@link Long}. */
    public TimerChan() {
        super(1, 0L);
    }

    /** gc: timer channels observe as {@code len() == 0} always. */
    @Override
    public synchronized int buffered() {
        return 0;
    }

    /** gc: timer channels observe as {@code cap() == 0} always. */
    @Override
    public synchronized int capacity() {
        return 0;
    }
}
