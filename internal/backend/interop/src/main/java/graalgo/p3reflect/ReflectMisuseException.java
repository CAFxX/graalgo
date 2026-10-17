package graalgo.p3reflect;

/**
 * Narrow misuse signal for the {@code "reflectop"} wire node
 * ({@link ReflectOpNode}).
 *
 * <p>Thrown ONLY at sites where the failure is provoked by guest Go code
 * misusing the reflect API — i.e. where gc would panic (ADR-0002 D4).
 * {@link ReflectOpNode} catches exactly this type at the node boundary and
 * translates it into a Go panic (the {@code SyncOpNode.panicOnMisuse}
 * pattern).
 *
 * <p>Deliberately NOT thrown for:
 * <ul>
 *   <li>frontend/backend contract violations (e.g. a non-token where the
 *       frontend always emits a type token, an unknown op string) — those
 *       stay plain {@link IllegalStateException} and surface as honest
 *       internal errors;
 *   <li>backend representation invariants (e.g. a non-{@code Cell} handle
 *       where the frontend's {@code __mustBeAssignable} guard already
 *       panicked in Go code) — same, plain {@code IllegalStateException};
 *   <li>unimplemented features (e.g. {@code sliceat}, "needs type
 *       synthesis") — a missing backend feature must never masquerade as a
 *       guest-recoverable panic.
 * </ul>
 *
 * <p>Extends {@link IllegalStateException} so that a misuse site missed by
 * the audit still fails loudly (as an internal error) rather than silently
 * slipping past a {@code catch (IllegalStateException)} scope.
 */
public final class ReflectMisuseException extends IllegalStateException {
    public ReflectMisuseException(String message) {
        super(message);
    }
}
