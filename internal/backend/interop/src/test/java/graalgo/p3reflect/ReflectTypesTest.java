package graalgo.p3reflect;

import graalgo.frontend.FrontendException;
import graalgo.values.GoKind;

import java.util.Map;

/**
 * Regression tests for reviewer-2 P1-4 (an "invalid type" wire entry must
 * fail loud, never become a fabricated kind) and P1-5 (forward references
 * must resolve to complete TypeInfos, not baked shells), plus the unsafe
 * follow-up (bare "Pointer" wire name for unsafe.Pointer).
 *
 * <p>Registered in {@code graalgo.iface.IfaceTestRunner}.
 */
public final class ReflectTypesTest {

    private static void assertTrue(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static ReflectTypes.TypeInfo findByKind(ReflectTypes.Table t, GoKind kind) {
        for (ReflectTypes.TypeInfo ti : t.byTok) {
            if (ti != null && ti.kind == kind) {
                return ti;
            }
        }
        return null;
    }

    /**
     * P1-4: a basic type named "invalid type" on the wire (types.Invalid
     * leaking from the exporter) must throw, not be laundered into a fake
     * INTERFACE kind with fabricated size/align.
     */
        private static graalgo.frontend.Json.Object parseDoc(String text) {
        graalgo.frontend.Json.Value v = graalgo.frontend.Json.parse(text);
        if (!(v instanceof graalgo.frontend.Json.Object doc)) {
            throw new AssertionError("test doc must be a JSON object");
        }
        return doc;
    }

public void testInvalidTypeWireEntryFailsLoud() {
        String doc = "{\"types\": [{\"kind\": \"basic\", \"detail\": "
                + "{\"name\": \"invalid type\", \"class\": \"unknown\", \"untyped\": false}}]}";
        try {
            ReflectTypes.install(parseDoc(doc), Map.of());
        } catch (FrontendException e) {
            assertTrue(e.getMessage().contains("unknown basic type"),
                    "unexpected message: " + e.getMessage());
            return;
        }
        throw new AssertionError("expected FrontendException for \"invalid type\" wire entry");
    }

    /**
     * P1-5: a signature referencing a later-declared named type is a
     * forward reference (signature wire id &lt; named wire id). The built
     * parameter TypeInfo must be complete — kind, fields, size — not a
     * baked shell (which had kind null, empty fields, size -1).
     */
    public void testForwardReferenceBuildsCompleteTypeInfo() {
        String doc = """
                {"types": [
                  {"kind": "signature", "detail": {
                     "params": [{"name": "v", "type": 1}], "results": [], "variadic": false}},
                  {"kind": "named", "detail": {
                     "name": "main.T", "pkgpath": "main", "underlying": 2, "methods": []}},
                  {"kind": "struct", "detail": {
                     "fields": [{"name": "X", "type": 3, "tag": "", "offset": 0}]}},
                  {"kind": "basic", "detail": {"name": "int", "class": "int", "untyped": false}}
                ]}
                """;
        ReflectTypes.install(parseDoc(doc), Map.of());
        ReflectTypes.Table table = ReflectTypes.table();

        ReflectTypes.TypeInfo sig = findByKind(table, GoKind.FUNC);
        assertTrue(sig != null, "no FUNC TypeInfo built");
        assertTrue(sig.ins.size() == 1, "sig should have 1 param, got " + sig.ins.size());
        ReflectTypes.TypeInfo param = sig.ins.get(0);
        assertTrue(param.tok != -1, "param is a baked shell (tok == -1)");
        assertTrue(param.kind == GoKind.STRUCT,
                "param kind should be STRUCT, got " + param.kind);
        assertTrue(param.fields.size() == 1,
                "param should have 1 field, got " + param.fields.size());
        assertTrue("X".equals(param.fields.get(0).name),
                "param field name should be X, got " + param.fields.get(0).name);
        assertTrue(param.size == 8, "param size should be 8, got " + param.size);

        ReflectTypes.TypeInfo named = null;
        for (ReflectTypes.TypeInfo ti : table.byTok) {
            if (ti != null && "main.T".equals(ti.name)) {
                named = ti;
            }
        }
        assertTrue(named != null, "no named main.T TypeInfo built");
        assertTrue(named.kind == GoKind.STRUCT, "named kind should be STRUCT, got " + named.kind);
        assertTrue(named.fields.size() == 1,
                "named should have 1 field, got " + named.fields.size());
        assertTrue(named.size == 8, "named size should be 8, got " + named.size);
    }

    /**
     * Unsafe-differential follow-up: go/types names the unsafe.Pointer
     * basic "Pointer" (not "unsafe.Pointer"), and the exporter sends
     * t.Name() verbatim. The table must accept the bare wire name and
     * still report Kind() == UnsafePointer (ordinal 26) and
     * Type.String() == "unsafe.Pointer", exactly like gc.
     */
    public void testBarePointerWireNameIsUnsafePointer() {
        String doc = "{\"types\": [{\"kind\": \"basic\", \"detail\": "
                + "{\"name\": \"Pointer\", \"class\": \"pointer\", \"untyped\": false}}]}";
        ReflectTypes.install(parseDoc(doc), Map.of());
        ReflectTypes.Table table = ReflectTypes.table();
        ReflectTypes.TypeInfo ptr = findByKind(table, GoKind.UNSAFE_POINTER);
        assertTrue(ptr != null, "no UNSAFE_POINTER TypeInfo built for wire name \"Pointer\"");
        assertTrue(ReflectTypes.kindOrdinal(ptr) == 26,
                "Kind ordinal should be 26 (UnsafePointer), got " + ReflectTypes.kindOrdinal(ptr));
        assertTrue("unsafe.Pointer".equals(ReflectTypes.typeString(ptr.tok)),
                "Type.String should be \"unsafe.Pointer\", got " + ReflectTypes.typeString(ptr.tok));
        assertTrue(ptr.size == 8 && ptr.align == 8,
                "unsafe.Pointer should be size 8 align 8, got " + ptr.size + "/" + ptr.align);
    }

    /**
     * M8b os-hang follow-up: {@code build()} used to call {@code resolve()}
     * once per type id, and each call re-ran token minting + TypeInfo
     * building over all ids (the id parameter was unused) — an O(n^2) key
     * recomputation. On the synthetic os package (2297 types) build() took
     * minutes, which a 60s corpus timeout misreported as a directory-
     * iteration hang. This test pins the linear behavior: 1200 struct
     * types install in ~1s; the old loop needed ~54s for the same table.
     * Bound is 20s (13x headroom over the fixed code on a loaded VM).
     */
    public void testLargeTypeTableInstallIsLinear() {
        StringBuilder sb = new StringBuilder("{\"types\": [");
        sb.append("{\"kind\": \"basic\", \"detail\": "
                + "{\"name\": \"int\", \"class\": \"int\", \"untyped\": false}}");
        for (int i = 0; i < 1200; i++) {
            sb.append(",{\"kind\": \"struct\", \"detail\": {\"fields\": ["
                    + "{\"name\": \"a\", \"type\": 0, \"tag\": \"\", \"offset\": 0},"
                    + "{\"name\": \"b\", \"type\": 0, \"tag\": \"\", \"offset\": 8},"
                    + "{\"name\": \"c\", \"type\": 0, \"tag\": \"\", \"offset\": 16}"
                    + "]}}");
        }
        sb.append("]}");
        long t0 = System.nanoTime();
        ReflectTypes.install(parseDoc(sb.toString()), Map.of());
        double secs = (System.nanoTime() - t0) / 1e9;
        assertTrue(secs < 20.0,
                "install of 1201 types took " + secs + "s (O(n^2) resolve loop regressed?)");
        // The 1200 structs are structurally identical, so canonical sharing
        // mints one token for all of them; the timing bound above is the
        // regression pin, not the token count.
    }

    /**
     * P3-M9 (reflect-iface): {@code descriptorForTok} must build the
     * canonical descriptor on demand for a named type the frontend never
     * materialized (e.g. a type only ever touched through
     * reflect.Value.Interface()). Before the fix it threw
     * "reflect: no descriptor for token", which broke
     * {@code reflect.ValueOf(xs).Index(0).Interface()} for any named
     * element type and, downstream, fmt's Stringer dispatch on
     * reflect-obtained values.
     */
    public void testDescriptorForTokBuildsOnDemandForNamedType() {
        String doc = """
                {"types": [
                  {"kind": "named", "detail": {
                     "name": "main.S", "pkgpath": "main", "underlying": 1, "methods": []}},
                  {"kind": "basic", "detail": {"name": "string", "class": "string", "untyped": false}}
                ]}
                """;
        ReflectTypes.install(parseDoc(doc), Map.of());
        ReflectTypes.Table table = ReflectTypes.table();
        ReflectTypes.TypeInfo named = null;
        for (ReflectTypes.TypeInfo ti : table.byTok) {
            if (ti != null && "main.S".equals(ti.name)) {
                named = ti;
            }
        }
        assertTrue(named != null, "no named main.S TypeInfo built");
        // Must not throw; must return the canonical descriptor whose
        // display name matches the frontend's naming.
        graalgo.values.GoTypeDescriptor d =
                ReflectTypes.descriptorForTok(named.tok);
        assertTrue(d != null, "descriptorForTok returned null for main.S");
        assertTrue("main.S".equals(d.name()),
                "descriptor name should be \"main.S\", got \"" + d.name() + "\"");
        // Identity: a second call returns the same canonical instance.
        assertTrue(d == ReflectTypes.descriptorForTok(named.tok),
                "descriptorForTok is not canonical across calls");
    }

    /**
     * PKG-010: a {@code typeparam} wire entry (a type parameter in a
     * template position — e.g. the {@code [0]*T} field of the
     * {@code sync/atomic.Pointer[T]} template, whose exporter describes
     * the template's underlying struct) must build into an opaque
     * placeholder, not fail the whole table install with
     * "reflect: unsupported type kind typeparam".
     *
     * <p>A type parameter has no runtime type identity: at runtime every
     * value of the parameter's type carries its *instantiated* type (the
     * frontend monomorphizes generics), so the placeholder is null-kind
     * with unknown size/align, and its canonical key can never collide
     * with a concrete type's token.
     */
    public void testTypeparamBuildsOpaquePlaceholder() {
        String doc = """
                {"types": [
                  {"kind": "named", "detail": {
                     "name": "sync/atomic.Pointer[T]", "pkgpath": "sync/atomic",
                     "underlying": 1, "methods": []}},
                  {"kind": "struct", "detail": {"fields": [
                     {"name": "_", "type": 2, "tag": "", "offset": 0}]}},
                  {"kind": "array", "detail": {"elem": 3, "len": 0}},
                  {"kind": "pointer", "detail": {"elem": 4, "methods": []}},
                  {"kind": "typeparam", "detail": {"name": "T", "constraint": 5}},
                  {"kind": "interface", "detail": {"embedded": [], "empty": true, "methods": []}}
                ]}
                """;
        // Before the fix this threw:
        // "types[4]: reflect: unsupported type kind typeparam".
        ReflectTypes.install(parseDoc(doc), Map.of());
        ReflectTypes.Table table = ReflectTypes.table();

        ReflectTypes.TypeInfo tp = null;
        ReflectTypes.TypeInfo anyIface = null;
        for (ReflectTypes.TypeInfo ti : table.byTok) {
            if (ti == null) {
                continue;
            }
            if (ti.kind == null) {
                assertTrue(tp == null, "two null-kind TypeInfos built for one typeparam");
                tp = ti;
            } else if (ti.kind == GoKind.INTERFACE && ti.wireId == 5) {
                anyIface = ti;
            }
        }
        assertTrue(tp != null, "no placeholder TypeInfo built for the typeparam entry");
        assertTrue(tp.wireId == 4, "placeholder wireId should be 4, got " + tp.wireId);
        assertTrue(tp.size == -1 && tp.align == -1,
                "placeholder size/align should be unknown (-1/-1), got "
                        + tp.size + "/" + tp.align);
        // No fabricated kind: kindOrdinal reports Invalid(0) for null kind.
        assertTrue(ReflectTypes.kindOrdinal(tp) == 0,
                "kindOrdinal of a typeparam should be 0 (Invalid), got "
                        + ReflectTypes.kindOrdinal(tp));
        // The placeholder must never share a token with a concrete type —
        // in particular not with its own constraint.
        assertTrue(anyIface != null, "no TypeInfo built for the any constraint");
        assertTrue(anyIface.tok != tp.tok,
                "typeparam placeholder shares a token with its constraint");
    }

    /**
     * PKG-010: typeparam canonical keys are structural (name + constraint
     * key) under a "typeparam:" prefix no concrete kind uses. Two wire
     * ids for the same parameter shape share one token; a different
     * parameter name mints a different token.
     */
    public void testTypeparamKeysAreCanonical() {
        String doc = """
                {"types": [
                  {"kind": "typeparam", "detail": {"name": "T", "constraint": 2}},
                  {"kind": "typeparam", "detail": {"name": "T", "constraint": 2}},
                  {"kind": "interface", "detail": {"embedded": [], "empty": true, "methods": []}},
                  {"kind": "typeparam", "detail": {"name": "U", "constraint": 2}}
                ]}
                """;
        ReflectTypes.install(parseDoc(doc), Map.of());
        ReflectTypes.Table table = ReflectTypes.table();

        java.util.List<ReflectTypes.TypeInfo> placeholders = new java.util.ArrayList<>();
        for (ReflectTypes.TypeInfo ti : table.byTok) {
            if (ti != null && ti.kind == null) {
                placeholders.add(ti);
            }
        }
        assertTrue(placeholders.size() == 2,
                "expected 2 canonical typeparam tokens (T shared, U distinct), got "
                        + placeholders.size());
        assertTrue(placeholders.get(0).tok != placeholders.get(1).tok,
                "distinct typeparams T and U share a token");
    }

    /**
     * PKG-010: rendering a typeparam placeholder as a type string must
     * fail loud (a type parameter must never be observed at runtime — any
     * observation means the frontend leaked a template type into
     * executable code), never fabricate a type string. Cf. the P1-4
     * "invalid type" rule: fail loud, never launder into a fake kind.
     */
    public void testTypeparamStringFailsLoud() {
        String doc = """
                {"types": [
                  {"kind": "typeparam", "detail": {"name": "T", "constraint": 1}},
                  {"kind": "interface", "detail": {"embedded": [], "empty": true, "methods": []}}
                ]}
                """;
        ReflectTypes.install(parseDoc(doc), Map.of());
        ReflectTypes.Table table = ReflectTypes.table();
        ReflectTypes.TypeInfo tp = null;
        for (ReflectTypes.TypeInfo ti : table.byTok) {
            if (ti != null && ti.kind == null) {
                tp = ti;
            }
        }
        assertTrue(tp != null, "no placeholder TypeInfo built for the typeparam entry");
        try {
            ReflectTypes.typeString(tp.tok);
        } catch (IllegalStateException e) {
            assertTrue(e.getMessage().contains("types[" + tp.wireId + "]"),
                    "unexpected message: " + e.getMessage());
            return;
        }
        throw new AssertionError("expected IllegalStateException from typeString on a typeparam");
    }

    /**
     * PKG-010: an F-bounded constraint cycle in the wire graph (typeparam
     * T constrained by an unnamed interface whose method signature
     * mentions T — legal Go, e.g. {@code W[T interface{ Get() T }]}) must
     * terminate key canonicalization. The re-entrancy guard cuts the
     * cycle with the parameter's own name as back-reference, mirroring
     * the frontend exporter's typeparamIDs memo-before-describe.
     */
    public void testTypeparamCyclicConstraintTerminates() {
        String doc = """
                {"types": [
                  {"kind": "typeparam", "detail": {"name": "T", "constraint": 1}},
                  {"kind": "interface", "detail": {"methods": [
                     {"name": "Get", "sig": 2}]}},
                  {"kind": "signature", "detail": {
                     "params": [], "results": [{"name": "", "type": 0}], "variadic": false}}
                ]}
                """;
        long t0 = System.nanoTime();
        ReflectTypes.install(parseDoc(doc), Map.of());
        double secs = (System.nanoTime() - t0) / 1e9;
        assertTrue(secs < 20.0,
                "install with a cyclic typeparam constraint took " + secs + "s (key recursion diverged?)");
        ReflectTypes.Table table = ReflectTypes.table();
        boolean found = false;
        for (ReflectTypes.TypeInfo ti : table.byTok) {
            if (ti != null && ti.kind == null && ti.wireId == 0) {
                found = true;
            }
        }
        assertTrue(found, "no placeholder TypeInfo built for the cyclic-constraint typeparam");
    }

    /**
     * I-3 (2026-10-19 review): the runtime type-synthesis paths
     * (ptrTo/arrayOf/sliceOf/mapOf/chanOf/funcOf/structOf) used to be
     * check-then-act on an unsynchronized HashMap + ArrayList, so two
     * threads synthesizing types concurrently could mint the same token
     * twice and skew every later token. This test hammers all seven
     * paths from 8 threads at once (barrier-synchronized cold start, so
     * the first iterations genuinely race) and pins the post-fix
     * invariants: same shape -&gt; same token on every thread, distinct
     * shapes -&gt; distinct tokens, and token == byTok index throughout.
     */
    public void testConcurrentTypeSynthesisIsCanonical() throws Exception {
        String doc = "{\"types\": [{\"kind\": \"basic\", \"detail\": "
                + "{\"name\": \"int\", \"class\": \"int\", \"untyped\": false}}]}";
        ReflectTypes.install(parseDoc(doc), Map.of());
        ReflectTypes.Table table = ReflectTypes.table();
        long intTok = table.tokForKind(GoKind.INT64);

        int threads = 8;
        int iters = 60;
        java.util.concurrent.CyclicBarrier barrier =
                new java.util.concurrent.CyclicBarrier(threads);
        java.util.concurrent.ConcurrentMap<String, Long> seen =
                new java.util.concurrent.ConcurrentHashMap<>();
        java.util.List<Throwable> errors =
                java.util.Collections.synchronizedList(new java.util.ArrayList<>());

        Thread[] ts = new Thread[threads];
        for (int t = 0; t < threads; t++) {
            final int id = t;
            ts[t] = new Thread(() -> {
                try {
                    barrier.await();
                    for (int i = 0; i < iters; i++) {
                        // Shared shapes: every thread synthesizes the same types.
                        agree(seen, errors, "ptr", ReflectTypes.ptrTo(intTok));
                        agree(seen, errors, "slice", ReflectTypes.sliceOf(intTok));
                        agree(seen, errors, "array", ReflectTypes.arrayOf(intTok, 4));
                        agree(seen, errors, "map", ReflectTypes.mapOf(intTok, intTok));
                        agree(seen, errors, "chan", ReflectTypes.chanOf(intTok, 0));
                        agree(seen, errors, "func", ReflectTypes.funcOf(
                                new long[]{intTok}, new long[]{intTok}, false));
                        agree(seen, errors, "struct", ReflectTypes.structOf(
                                new String[]{"a", "b"}, new long[]{intTok, intTok},
                                new String[]{"", ""}, new String[]{"", ""},
                                new boolean[]{false, false}));
                        // Thread-distinct shape: must mint a token no other
                        // thread's shape shares.
                        agree(seen, errors, "mine" + id, ReflectTypes.structOf(
                                new String[]{"f" + id}, new long[]{intTok},
                                new String[]{""}, new String[]{""},
                                new boolean[]{false}));
                    }
                } catch (Throwable th) {
                    errors.add(th);
                }
            });
            ts[t].start();
        }
        for (Thread th : ts) {
            th.join();
        }
        assertTrue(errors.isEmpty(), "concurrent synthesis raised: " + errors);
        // 7 shared shapes + 8 thread-distinct shapes = 15 distinct tokens.
        java.util.Set<Long> distinct = new java.util.HashSet<>(seen.values());
        assertTrue(distinct.size() == 7 + threads,
                "expected " + (7 + threads) + " distinct tokens, got " + distinct.size()
                        + " (tokens: " + seen + ")");
        // Phase B: distinct-key race — every mint is a brand-new key, so
        // every operation is a potential cross-thread collision on the
        // token mint. Pre-fix this reliably mints duplicate tokens.
        int dthreads = 16;
        int diters = 150;
        java.util.concurrent.CyclicBarrier b2 =
                new java.util.concurrent.CyclicBarrier(dthreads);
        java.util.concurrent.ConcurrentMap<String, Long> seen2 =
                new java.util.concurrent.ConcurrentHashMap<>();
        Thread[] ds = new Thread[dthreads];
        for (int t = 0; t < dthreads; t++) {
            final int id = t;
            ds[t] = new Thread(() -> {
                try {
                    b2.await();
                    for (int i = 0; i < diters; i++) {
                        long tok = ReflectTypes.arrayOf(intTok, id * 100000 + i);
                        seen2.put("d" + id + "-" + i, tok);
                    }
                } catch (Throwable th) {
                    errors.add(th);
                }
            });
            ds[t].start();
        }
        for (Thread th : ds) {
            th.join();
        }
        assertTrue(errors.isEmpty(),
                "concurrent distinct-key synthesis raised: " + errors);
        java.util.Set<Long> ddistinct = new java.util.HashSet<>(seen2.values());
        assertTrue(ddistinct.size() == dthreads * diters,
                "expected " + (dthreads * diters) + " distinct tokens for "
                        + (dthreads * diters) + " distinct shapes, got "
                        + ddistinct.size() + " (duplicate tokens minted)");
        // Structural invariants: token == byTok index, and every keyToTok
        // value resolves to its own entry.
        for (int i = 1; i < table.byTok.size(); i++) {
            ReflectTypes.TypeInfo ti = table.byTok.get(i);
            assertTrue(ti != null && ti.tok == i,
                    "token/index skew at byTok[" + i + "]");
        }
        for (Map.Entry<String, Long> e : table.keyToTok.entrySet()) {
            long v = e.getValue();
            assertTrue(v >= 1 && v < table.byTok.size(),
                    "keyToTok value out of range for key " + e.getKey() + ": " + v);
            assertTrue(table.byTok.get((int) v).tok == v,
                    "keyToTok entry " + e.getKey() + " -> " + v
                            + " resolves to a different token");
        }
    }

    private static void agree(java.util.concurrent.ConcurrentMap<String, Long> seen,
            java.util.List<Throwable> errors, String shape, long tok) {
        Long prev = seen.putIfAbsent(shape, tok);
        if (prev != null && prev != tok) {
            errors.add(new AssertionError(
                    "shape " + shape + " got two tokens: " + prev + " and " + tok));
        }
    }

    /**
     * I-7 (2026-10-19 review): reflect-created structs used to get a
     * fresh GoStructDescriptor per structDescFor call, but
     * GoStruct.equals requires descriptor identity and hashCode uses
     * the descriptor's identity hash — so two reflect.New(T).Elem()
     * values of the same type were never ==-equal and never matched as
     * map keys, where gc says equal. The descriptor is now memoized per
     * type token in the TypeInfo: this test pins descriptor identity,
     * value equality, and map-key interchangeability for two
     * independently synthesized values of the same struct type (plus a
     * nested struct level).
     */
    public void testReflectStructDescriptorsAreCanonical() {
        String doc = "{\"types\": ["
                + "{\"kind\": \"basic\", \"detail\": "
                + "{\"name\": \"int\", \"class\": \"int\", \"untyped\": false}},"
                + "{\"kind\": \"basic\", \"detail\": "
                + "{\"name\": \"string\", \"class\": \"string\", \"untyped\": false}}]}";
        ReflectTypes.install(parseDoc(doc), Map.of());
        ReflectTypes.Table table = ReflectTypes.table();
        long intTok = table.tokForKind(GoKind.INT64);
        long strTok = table.tokForKind(GoKind.STRING);

        // Two independent syntheses of the same shape, as two
        // reflect.New(T) calls would produce.
        String[] names = {"a", "b"};
        long[] toks = {intTok, strTok};
        String[] tags = {"", ""};
        String[] pkgs = {"", ""};
        boolean[] anon = {false, false};
        long t1 = ReflectTypes.structOf(names, toks, tags, pkgs, anon);
        long t2 = ReflectTypes.structOf(names, toks, tags, pkgs, anon);
        assertTrue(t1 == t2, "structOf not canonical: " + t1 + " vs " + t2);

        graalgo.values.GoStructDescriptor d1 = table.info(t1).structDescriptor();
        graalgo.values.GoStructDescriptor d2 = table.info(t2).structDescriptor();
        assertTrue(d1 == d2, "struct descriptors not canonical per token");

        // Two zero values of the same type: equal, same hash, and
        // interchangeable as map keys.
        graalgo.values.GoStruct s1 = graalgo.values.GoStruct.zero(d1);
        graalgo.values.GoStruct s2 = graalgo.values.GoStruct.zero(d2);
        assertTrue(s1.equals(s2),
                "two zero structs of the same type are not equal");
        assertTrue(s2.equals(s1), "struct equality not symmetric");
        assertTrue(s1.hashCode() == s2.hashCode(),
                "equal structs have different hashCodes");
        java.util.Map<graalgo.values.GoStruct, String> m = new java.util.HashMap<>();
        m.put(s1, "v");
        assertTrue("v".equals(m.get(s2)),
                "struct map-key lookup failed across two values of the same type");

        // Nested struct level: the inner type's descriptor is canonical too.
        long inner = ReflectTypes.structOf(
                new String[]{"x"}, new long[]{intTok},
                new String[]{""}, new String[]{""}, new boolean[]{false});
        long o1 = ReflectTypes.structOf(
                new String[]{"in"}, new long[]{inner},
                new String[]{""}, new String[]{""}, new boolean[]{false});
        long o2 = ReflectTypes.structOf(
                new String[]{"in"}, new long[]{inner},
                new String[]{""}, new String[]{""}, new boolean[]{false});
        assertTrue(o1 == o2, "nested structOf not canonical");
        assertTrue(table.info(inner).structDescriptor()
                        == table.info(inner).structDescriptor(),
                "nested struct descriptor not canonical");
        assertTrue(table.info(o1).structDescriptor()
                        == table.info(o2).structDescriptor(),
                "outer struct descriptor not canonical");

        // Concurrent first-use: the lazy memoization must publish a
        // single descriptor instance even when raced.
        java.util.concurrent.CyclicBarrier dbarrier =
                new java.util.concurrent.CyclicBarrier(4);
        java.util.concurrent.atomic.AtomicReference<
                graalgo.values.GoStructDescriptor> firstRef =
                new java.util.concurrent.atomic.AtomicReference<>();
        java.util.List<Throwable> derrors =
                java.util.Collections.synchronizedList(new java.util.ArrayList<>());
        Thread[] dt = new Thread[4];
        for (int i = 0; i < 4; i++) {
            dt[i] = new Thread(() -> {
                try {
                    dbarrier.await();
                    for (int j = 0; j < 50; j++) {
                        graalgo.values.GoStructDescriptor d =
                                table.info(t1).structDescriptor();
                        if (!firstRef.compareAndSet(null, d) && firstRef.get() != d) {
                            derrors.add(new AssertionError(
                                    "concurrent structDescriptor() published two instances"));
                        }
                    }
                } catch (Throwable th) {
                    derrors.add(th);
                }
            });
            dt[i].start();
        }
        for (Thread th : dt) {
            try {
                th.join();
            } catch (InterruptedException e) {
                throw new AssertionError("interrupted joining descriptor threads", e);
            }
        }
        assertTrue(derrors.isEmpty(),
                "concurrent descriptor memoization failed: " + derrors);
    }
}
