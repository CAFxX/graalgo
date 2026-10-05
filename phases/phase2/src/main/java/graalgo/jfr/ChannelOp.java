package graalgo.jfr;

/**
 * Channel operation that blocked (P2-M6, WS F).
 *
 * <p>Recorded on {@code graalgo.ChannelBlock}.
 */
public enum ChannelOp {
    SEND,
    RECV,
    SELECT
}
