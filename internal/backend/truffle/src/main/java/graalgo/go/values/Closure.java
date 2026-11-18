package graalgo.go.values;

import com.oracle.truffle.api.RootCallTarget;
import com.oracle.truffle.api.frame.MaterializedFrame;

/**
 * A Go func value: {@code {RootCallTarget, MaterializedFrame|null}} (SPEC §3.4). {@code env} is
 * the closure's capture environment, a small frame built when the function literal is evaluated
 * whose slots hold the {@link Flat} boxes of the captured variables (by reference; a fresh box
 * per iteration for Go 1.22 loop variables) — never the creating function's own activation
 * frame. It is null for top-level functions. The nil func is Java {@code null}.
 */
public final class Closure {
    public final RootCallTarget target;
    public final MaterializedFrame env;

    public Closure(RootCallTarget target, MaterializedFrame env) {
        this.target = target;
        this.env = env;
    }
}
