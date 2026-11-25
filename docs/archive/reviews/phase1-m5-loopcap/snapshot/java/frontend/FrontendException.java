package graalgo.frontend;

/**
 * Failure to parse a frontend/v1 document or to build Truffle nodes from it.
 *
 * <p>Also the deliberate extension seam: kinds the v1 skeleton does not cover
 * (expressions beyond WS D's slice, statements beyond WS E's slice, types
 * beyond the int64 subset, builtins before WS G) fail with a
 * FrontendException naming the owning workstream — never silently.
 */
public final class FrontendException extends RuntimeException {
    public FrontendException(String message) {
        super(message);
    }
}
