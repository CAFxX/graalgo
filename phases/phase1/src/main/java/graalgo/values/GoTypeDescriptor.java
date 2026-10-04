package graalgo.values;

/**
 * The Phase-1 dynamic-type identity (M4, WS E): one canonical instance per
 * Go type, used to box concrete values into {@link GoInterface} and to
 * resolve type switches / type assertions by identity.
 *
 * <p>Canonicalization rules (see {@code NodeFactory.typeDescriptorFor}):
 * <ul>
 *   <li>basic types: one instance per {@link GoKind} — the default type of
 *       an untyped constant ({@code int}, {@code float64}, {@code string},
 *       {@code bool}) shares the instance with the corresponding basic
 *       type-table entry;</li>
 *   <li>named types: one instance per named type — a named type and its
 *       underlying type are <em>different</em> dynamic types (gc-verified:
 *       {@code case MyInt} does not match a dynamic {@code int});</li>
 *   <li>aliases resolve to their right-hand side (an alias <em>is</em> its
 *       rhs as a dynamic type);</li>
 *   <li>all other composite types: one instance per type-table entry.</li>
 * </ul>
 * Instances are compared by reference; there is no structural equality.
 */
public final class GoTypeDescriptor {
    private final String name;
    private final GoKind kind;
    private final boolean emptyInterface;

    public GoTypeDescriptor(String name, GoKind kind, boolean emptyInterface) {
        this.name = name;
        this.kind = kind;
        this.emptyInterface = emptyInterface;
    }

    /** Human-readable type name (qualified for named types). */
    public String name() {
        return name;
    }

    public GoKind kind() {
        return kind;
    }

    /** True for the empty interface type (matches every non-nil dynamic type). */
    public boolean isEmptyInterface() {
        return emptyInterface;
    }

    @Override
    public String toString() {
        return "GoTypeDescriptor{" + name + "}";
    }
}
