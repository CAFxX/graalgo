package graalgo.p3time;

import graalgo.runtime.GoTimer;
import graalgo.values.GoStructDescriptor;

/**
 * Opaque {@code *time.Ticker} Go value (P3-M5, WS E, ADR-0005 D1): a
 * {@link GoTimerT} whose translator loops, delivering a
 * {@code time.Time} per heap tick until {@link #stop()} (slow receivers
 * drop ticks — gc parity).
 *
 * <p>verifies: GG-0017 (time slice)
 */
public final class GoTickerT extends GoTimerT {
    public GoTickerT(GoTimer timer, GoStructDescriptor timeDesc) {
        super(timer, true, null, timeDesc);
    }
}
