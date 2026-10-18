package graalgo.frontend;

import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.source.Source;
import graalgo.values.GoKind;
import graalgo.values.GoTypeDescriptor;

/**
 * B-2: process-wide descriptor caches must not contaminate across programs.
 *
 * <p>The corpus/differential harnesses compile many {@code package main}
 * programs in one JVM. {@code NAMED_DESCS} was keyed by qualified name
 * alone, so a second program defining {@code main.Foo} with a different
 * kind or comparability silently inherited the first program's descriptor
 * — including its kind and bug-019 comparability flag. The
 * {@code DESCRIPTOR_WIRE_IDS}/{@code WIRE_ID_DESCRIPTORS} registries are
 * keyed by per-document wire type ids, equally meaningless across
 * programs.
 *
 * <p>RED (pre-fix): program 2's {@code main.Foo} ({@code []int},
 * uncomparable) resolves to program 1's descriptor (struct, comparable).
 * GREEN (post-fix): the named-descriptor key carries (name, kind,
 * comparability) — the STRUCT_DESCS name+layout precedent — and the
 * wire-id registries are reset at the start of every build.
 */
public final class NamedDescriptorIsolationTest {

    /** Program 1: {@code type Foo struct{ x int }} — STRUCT, comparable. */
    private static final String TYPES_P1 = "["
            + "{\"kind\":\"named\",\"detail\":{\"name\":\"main.Foo\",\"underlying\":1}},"
            + "{\"kind\":\"struct\",\"detail\":{\"fields\":[{\"name\":\"x\",\"type\":2}]}},"
            + "{\"kind\":\"basic\",\"detail\":{\"class\":\"int\",\"name\":\"int\"}}"
            + "]";

    /** Program 2: {@code type Foo []int} — SLICE, uncomparable. Same name. */
    private static final String TYPES_P2 = "["
            + "{\"kind\":\"named\",\"detail\":{\"name\":\"main.Foo\",\"underlying\":1}},"
            + "{\"kind\":\"slice\",\"detail\":{\"elem\":2}},"
            + "{\"kind\":\"basic\",\"detail\":{\"class\":\"int\",\"name\":\"int\"}}"
            + "]";

    private static NodeFactory.BuildCtx ctxFor(String typesJson) {
        Source src = Source.newBuilder("go", "package main\n", "p.go").build();
        TypeTable types = new TypeTable((Json.Array) Json.parse(typesJson));
        return new NodeFactory.BuildCtx(src, FrameDescriptor.newBuilder(),
                types, (Json.Array) Json.parse("[]"));
    }

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    public void testNamedDescriptorNotSharedAcrossPrograms() {
        GoTypeDescriptor d1 = NodeFactory.typeDescriptorFor(ctxFor(TYPES_P1), 0, "p1");
        check(d1.kind() == GoKind.STRUCT, "p1 main.Foo kind: " + d1.kind());
        check(d1.isComparable(), "p1 main.Foo should be comparable");

        GoTypeDescriptor d2 = NodeFactory.typeDescriptorFor(ctxFor(TYPES_P2), 0, "p2");
        check(d2.kind() == GoKind.SLICE,
                "p2 main.Foo kind contaminated: got " + d2.kind() + ", want SLICE");
        check(!d2.isComparable(),
                "p2 main.Foo comparability contaminated: uncomparable []int got comparable=true");
        check(d2 != d1, "p2 main.Foo shares program 1's descriptor instance");
    }

    public void testIdenticalNamedTypesStillShare() {
        // Same (name, kind, comparability) across builds: sharing the
        // immutable instance is intended (the M6 cross-BuildCtx identity
        // requirement); dynamic-type universes never mix.
        GoTypeDescriptor d1 = NodeFactory.typeDescriptorFor(ctxFor(TYPES_P1), 0, "p1");
        GoTypeDescriptor d1b = NodeFactory.typeDescriptorFor(ctxFor(TYPES_P1), 0, "p1b");
        check(d1b == d1,
                "identical named types across builds should share the canonical instance");
    }

    public void testWireIdRegistriesResetPerBuild() {
        NodeFactory.resetWireIdRegistries(); // simulate build 1 start
        GoTypeDescriptor d1 = NodeFactory.typeDescriptorFor(ctxFor(TYPES_P1), 0, "p1");
        check(NodeFactory.descriptorForWireId(0) == d1,
                "build 1: wire id 0 should map to d1");
        check(Integer.valueOf(0).equals(NodeFactory.wireIdForDescriptor(d1)),
                "build 1: d1 should map to wire id 0");

        NodeFactory.resetWireIdRegistries(); // simulate build 2 start
        check(NodeFactory.descriptorForWireId(0) == null,
                "build 2 start: stale program-1 wire-id mapping leaked");
        check(NodeFactory.wireIdForDescriptor(d1) == null,
                "build 2 start: stale program-1 descriptor->wire-id mapping leaked");

        GoTypeDescriptor d2 = NodeFactory.typeDescriptorFor(ctxFor(TYPES_P2), 0, "p2");
        check(NodeFactory.descriptorForWireId(0) == d2,
                "build 2: wire id 0 should map to d2");
    }
}
