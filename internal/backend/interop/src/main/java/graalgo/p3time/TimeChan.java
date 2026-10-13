package graalgo.p3time;

import graalgo.runtime.TimerChan;

/**
 * The user-visible timer/ticker channel (P3-M5, WS E, ADR-0005 D1/D3):
 * carries {@code time.Time} values ({@link graalgo.values.GoStruct}) as
 * produced by the fire-value translator in {@link GoTimerT}.
 *
 * <p>The DIV-M3-1 masking is inherited verbatim from {@link TimerChan}:
 * the real 1-slot buffer underneath keeps fire values deliverable to
 * {@code recv}/select/close-drain while {@code buffered()} and
 * {@code capacity()} report 0 in every state (pre-fire, fired-undrained,
 * drained, stopped) — gc presents timer channels as unbuffered. The
 * translator is the only sender; the channel is never closed by the
 * runtime (gc never closes timer channels).
 *
 * <p>verifies: GG-0017 (time slice)
 */
public final class TimeChan extends TimerChan {
    public TimeChan() {
        super();
    }
}
