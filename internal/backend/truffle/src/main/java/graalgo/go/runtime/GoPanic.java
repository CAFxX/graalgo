package graalgo.go.runtime;

import com.oracle.truffle.api.exception.AbstractTruffleException;
import com.oracle.truffle.api.nodes.Node;

/**
 * A Go panic in flight (SPEC §3.5, GG-0070): an {@link AbstractTruffleException} carrying the
 * panic value — always a non-nil {@code any} ({@code panic(nil)} carries
 * {@code *runtime.PanicNilError}). {@code link} is the panic this one was raised during (gc's
 * {@code _panic.link}), for the crash report's chain.
 */
public final class GoPanic extends AbstractTruffleException {
    private static final long serialVersionUID = 1L;

    public final transient Object value;
    public transient GoPanic link;
    public boolean recovered;

    public GoPanic(Node location, Object value) {
        super(location);
        this.value = value;
    }
}
