package graalgo.values;

/**
 * The Phase-1 dynamic-type identity (M4, WS E): one canonical instance per
 * Go type, used to box concrete values into {@link GoInterface} and to
 * resolve type switches / type assertions by identity.
 *
 * <p>Canonicalization rules (see {@code NodeFactory.typeDescriptorFor}):
 * <ul>
 *   <li>basic types: one instance per Go type <em>name</em> — {@code int}
 *       and {@code int64} are distinct types (distinct %T, distinct type
 *       switch cases) even though they share a kind; the default type of
 *       an untyped constant ({@code int}, {@code float64}, {@code string},
 *       {@code bool}) shares the instance with the corresponding basic
 *       type-table entry;</li>
 *   <li>named types: one instance per <em>qualified name</em> (process-wide) —
 *       a named type and its underlying type are <em>different</em> dynamic
 *       types (gc-verified: {@code case MyInt} does not match a dynamic
 *       {@code int}); the name key is safe because qualified names are
 *       unique within a program and descriptors are immutable identity
 *       tokens;</li>
 *   <li>aliases resolve to their right-hand side (an alias <em>is</em> its
 *       rhs as a dynamic type);</li>
 *   <li>unnamed struct/array/slice/map/pointer types: one instance per
 *       <em>structural shape</em> (process-wide) — anonymous types are
 *       identical in Go iff structurally identical, and the exporter's
 *       type table mints one id per syntactic occurrence;</li>
 *   <li>all other kinds: one instance per type-table entry.</li>
 * </ul>
 * Instances are compared by reference; there is no structural equality.
 */
public final class GoTypeDescriptor {
    private final String name;
    private final GoKind kind;
    private final boolean emptyInterface;
    /**
     * Method-table key (rev30-iface-valuecopy, N-P1-2): the qualified
     * identity of a named type (full package path, e.g.
     * {@code "a/foo.T"}), as opposed to {@link #name()}, the %T display
     * form (package NAME only, e.g. {@code "foo.T"}). Two packages with
     * the same package name but different paths mint distinct
     * descriptors whose display names collide, so keying interface
     * method tables on {@link #name()} silently merged their method
     * sets, last wins. The key is the qualified name for named and
     * pointer-to-named types and falls back to {@link #name()} for every
     * other descriptor (basic, structural, interface), where the name
     * already identifies the type. Descriptors minted without a
     * qualified name (tests, reflect shells) default the key to the
     * name, preserving their existing table keys.
     */
    private final String qualifiedName;
    /**
     * bug-019: whether values of this type can be compared with ==.
     * gc panics ("comparing uncomparable type") when two interface values
     * with the identical dynamic type are compared and the type is not
     * comparable (func, slice, map, or struct/array containing one).
     * Computed by the frontend from the type table; defaults to true for
     * descriptors not built from a type entry (all such uses are basic
     * or pointer kinds, which are comparable).
     */
    private final boolean comparable;

    public GoTypeDescriptor(String name, GoKind kind, boolean emptyInterface) {
        this(name, kind, emptyInterface, true);
    }

    public GoTypeDescriptor(String name, GoKind kind, boolean emptyInterface, boolean comparable) {
        this(name, kind, emptyInterface, comparable, name);
    }

    public GoTypeDescriptor(String name, GoKind kind, boolean emptyInterface, boolean comparable,
            String qualifiedName) {
        this.name = name;
        this.kind = kind;
        this.emptyInterface = emptyInterface;
        this.comparable = comparable;
        this.qualifiedName = qualifiedName == null ? name : qualifiedName;
    }

    /** Human-readable type name (qualified for named types). */
    public String name() {
        return name;
    }

    /**
     * The interface-method-table key for this type: the qualified name
     * for named/pointer-to-named types, the display name otherwise.
     * Method tables key on this, never on {@link #name()} alone.
     */
    public String methodTableKey() {
        return qualifiedName;
    }

    public GoKind kind() {
        return kind;
    }

    /** True for the empty interface type (matches every non-nil dynamic type). */
    public boolean isEmptyInterface() {
        return emptyInterface;
    }

    /** True if values of this type support == (bug-019). */
    public boolean isComparable() {
        return comparable;
    }

    @Override
    public String toString() {
        return "GoTypeDescriptor{" + name + "}";
    }
}
