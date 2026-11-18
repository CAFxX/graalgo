package graalgo.language;

import com.oracle.truffle.api.exception.AbstractTruffleException;

/**
 * A program document the language cannot build (a malformed or out-of-scope
 * wire document, or an unsupported source kind). Surfaces to the embedder
 * as a guest {@code PolyglotException} carrying the factory's message; the
 * launchers print it as "internal error building AST" (exit 1), exactly as
 * they did for the {@code FrontendException} it wraps.
 */
public final class GoParseException extends AbstractTruffleException {
    private static final long serialVersionUID = 1L;

    public GoParseException(String message) {
        super(message);
    }
}
