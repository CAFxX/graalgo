package graalgo.runtime;

import graalgo.values.GoString;

/**
 * Small helpers for Go string values in the Phase-2 runtime (P2-M2).
 */
final class GoStrings {
    private GoStrings() {}

    /** A Go string value for a constant message (e.g. panic text). */
    static GoString of(String s) {
        return GoString.fromUtf8(s);
    }
}
