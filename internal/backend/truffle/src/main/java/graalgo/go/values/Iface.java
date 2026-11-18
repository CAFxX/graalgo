package graalgo.go.values;

import graalgo.go.types.GoType;

/**
 * A non-nil Go interface value: dynamic type descriptor and data (SPEC §3.4). The nil interface
 * is Java {@code null}. {@code data} is the value in its carrier form, boxed: {@code Long} for
 * every integer type, {@code Double} for floats, {@code Boolean}, or the reference; struct and
 * array values are a private {@link Flat} copy that is never mutated.
 */
public final class Iface {
    public final GoType type;
    public final Object data;

    public Iface(GoType type, Object data) {
        this.type = type;
        this.data = data;
    }
}
