package graalgo.frontend;

import com.oracle.truffle.api.frame.FrameDescriptor;
import com.oracle.truffle.api.source.Source;

import graalgo.values.GoStruct;

/**
 * Red-green for B23-01: a {@code sync.Pool} embedded in a struct must
 * materialize as a plain {@code GoStruct} with a disk body — never as
 * the opaque {@code GoPool} Java object — and two zero values of the
 * struct must own independent pools.
 *
 * <p>Root cause: {@code NodeFactory.fieldZeroTemplate} routed struct
 * fields of type {@code sync.Pool} through
 * {@code syncPrimitiveName} → {@code newSyncPrimitive("Pool")} → a
 * {@code GoPool} stored in the memoized per-type zero template. Every
 * Pool method call routes to the real disk body
 * ({@code (*Pool).Put} → {@code pin()} → {@code &p.localSize} /
 * {@code p.local}), whose first field touch hard-casts to
 * {@code GoStruct} ({@code FieldAddrNode}, {@code SelectorNode}), so
 * {@code var a, b S; a.p.Put("y")} died with
 * {@code ClassCastException} → exit 1. And because the template is
 * memoized and {@code GoStruct.deepCopy()} had no GoPool branch, every
 * zero value shared the template's single pool. The fix follows the
 * PKG-053 precedent: Pool fields are plain GoStructs (the
 * {@code "Pool"} arm is dropped from {@code syncPrimitiveName}), so the
 * disk body works exactly as it already did for direct Pool vars, and
 * {@code deepCopy} gives each evaluation its own pool.
 *
 * <p>The type table below is verbatim from a real gofrontend export of
 * {@code type S struct { p sync.Pool }} (type ids 0–8 as exported, plus
 * the named {@code main.S} entry re-anchored at id 9): the shapes are
 * ground truth from the wire, not hand-invented.
 *
 * <p>RED (pre-fix): {@code testStructFieldPoolIsGoStruct} fails — the
 * template's {@code p} field is a {@code GoPool}, not a
 * {@code GoStruct}; {@code testTwoZeroValuesAreIndependent} fails — both
 * deep copies share the template's single {@code GoPool} instance.
 * GREEN (post-fix): the field is a {@code GoStruct} named
 * {@code sync.Pool}, and each deep copy owns a distinct pool struct
 * with independent longs.
 */
public final class StructPoolZeroTemplateTest {

    /** Wire type table for {@code type S struct { p sync.Pool }}. */
    private static final String TYPES_JSON = "["
            + "{\"kind\": \"struct\", \"detail\": {\"fields\": [{\"embedded\": false,"
            + " \"exported\": false, \"name\": \"p\", \"offset\": 0,"
            + " \"pkgpath\": \"main\", \"tag\": \"\", \"type\": 1}]}}"
            + ", {\"kind\": \"named\", \"detail\": {\"methods\": [],"
            + " \"name\": \"sync.Pool\", \"pkgpath\": \"sync\", \"underlying\": 2}}"
            + ", {\"kind\": \"struct\", \"detail\": {\"fields\": ["
            + "{\"embedded\": false, \"exported\": false, \"name\": \"noCopy\","
            + " \"offset\": 0, \"pkgpath\": \"sync\", \"tag\": \"\", \"type\": 3},"
            + "{\"embedded\": false, \"exported\": false, \"name\": \"local\","
            + " \"offset\": 0, \"pkgpath\": \"sync\", \"tag\": \"\", \"type\": 5},"
            + "{\"embedded\": false, \"exported\": false, \"name\": \"localSize\","
            + " \"offset\": 8, \"pkgpath\": \"sync\", \"tag\": \"\", \"type\": 6},"
            + "{\"embedded\": false, \"exported\": false, \"name\": \"victim\","
            + " \"offset\": 16, \"pkgpath\": \"sync\", \"tag\": \"\", \"type\": 5},"
            + "{\"embedded\": false, \"exported\": false, \"name\": \"victimSize\","
            + " \"offset\": 24, \"pkgpath\": \"sync\", \"tag\": \"\", \"type\": 6},"
            + "{\"embedded\": false, \"exported\": true, \"name\": \"New\","
            + " \"offset\": 32, \"pkgpath\": \"\", \"tag\": \"\", \"type\": 7}]}}"
            + ", {\"kind\": \"named\", \"detail\": {\"methods\": [],"
            + " \"name\": \"sync.noCopy\", \"pkgpath\": \"sync\", \"underlying\": 4}}"
            + ", {\"kind\": \"struct\", \"detail\": {\"fields\": []}}"
            + ", {\"kind\": \"basic\", \"detail\": {\"class\": \"pointer\","
            + " \"name\": \"Pointer\", \"untyped\": false}}"
            + ", {\"kind\": \"basic\", \"detail\": {\"class\": \"uint\","
            + " \"name\": \"uintptr\", \"untyped\": false}}"
            + ", {\"kind\": \"signature\", \"detail\": {\"params\": [],"
            + " \"results\": [{\"name\": \"\", \"type\": 8}], \"variadic\": false}}"
            + ", {\"kind\": \"interface\", \"detail\": {\"embedded\": [],"
            + " \"empty\": true, \"methods\": []}}"
            + ", {\"kind\": \"named\", \"detail\": {\"methods\": [],"
            + " \"name\": \"main.S\", \"pkgpath\": \"main\", \"underlying\": 0}}"
            + "]";

    /** Zero template for {@code main.S} (type id 9 in the table above). */
    private static GoStruct structZeroTemplateForS() {
        Json.Array types = (Json.Array) Json.parse(TYPES_JSON);
        Json.Array objects = (Json.Array) Json.parse("[]");
        Source source = Source.newBuilder("go", "package main", "poolzero.go").build();
        NodeFactory.BuildCtx ctx = new NodeFactory.BuildCtx(
                source, FrameDescriptor.newBuilder(), new TypeTable(types), objects);
        return NodeFactory.structZeroTemplate(ctx, 9, "test");
    }

    /** Descriptor field position of {@code localSize} in a Pool struct. */
    private static int localSizeField(GoStruct pool) {
        for (int i = 0; i < pool.descriptor().fields().size(); i++) {
            if ("localSize".equals(pool.descriptor().field(i).name())) {
                return i;
            }
        }
        throw new AssertionError("sync.Pool descriptor has no localSize field");
    }

    /**
     * Layer 1 (crash): the template's {@code p} field must be a
     * {@code GoStruct} with the disk layout, not a {@code GoPool} —
     * the disk body hard-casts its receiver's fields to
     * {@code GoStruct}, so a {@code GoPool} here throws
     * {@code ClassCastException} on the first {@code Put}.
     */
    public void testStructFieldPoolIsGoStruct() {
        GoStruct template = structZeroTemplateForS();
        Object p = template.getObject(0);
        if (!(p instanceof GoStruct pool)) {
            throw new AssertionError("struct-field sync.Pool must be a GoStruct, got "
                    + (p == null ? "null" : p.getClass().getName()));
        }
        if (!"sync.Pool".equals(pool.descriptor().typeName())) {
            throw new AssertionError("struct-field pool descriptor must be sync.Pool, got "
                    + pool.descriptor().typeName());
        }
    }

    /**
     * Layer 2 (sharing): two evaluations of the zero value must own
     * independent pools — writing one pool's {@code localSize} must
     * leave the other (and the template) untouched. Pre-fix both
     * deep copies shared the template's single {@code GoPool}.
     */
    public void testTwoZeroValuesAreIndependent() {
        GoStruct template = structZeroTemplateForS();
        GoStruct a = template.deepCopy();
        GoStruct b = template.deepCopy();
        GoStruct poolA = (GoStruct) a.getObject(0);
        GoStruct poolB = (GoStruct) b.getObject(0);
        GoStruct poolT = (GoStruct) template.getObject(0);
        if (poolA == poolB || poolA == poolT || poolB == poolT) {
            throw new AssertionError("zero values of S must own distinct Pool structs");
        }
        int ls = localSizeField(poolA);
        poolA.setLong(ls, 7L);
        if (poolB.getLong(ls) != 0L || poolT.getLong(ls) != 0L) {
            throw new AssertionError("writing one zero value's pool must not affect the other");
        }
    }
}
