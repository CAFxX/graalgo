package graalgo.conventions;

import java.io.IOException;
import java.io.InputStream;
import java.io.UncheckedIOException;
import java.lang.classfile.Annotation;
import java.lang.classfile.AnnotationElement;
import java.lang.classfile.AnnotationValue;
import java.lang.classfile.Attributes;
import java.lang.classfile.ClassFile;
import java.lang.classfile.ClassModel;
import java.lang.classfile.CodeElement;
import java.lang.classfile.CodeModel;
import java.lang.classfile.FieldModel;
import java.lang.classfile.Instruction;
import java.lang.classfile.Label;
import java.lang.classfile.MethodModel;
import java.lang.classfile.Opcode;
import java.lang.classfile.constantpool.ClassEntry;
import java.lang.classfile.constantpool.PoolEntry;
import java.lang.classfile.instruction.BranchInstruction;
import java.lang.classfile.instruction.ExceptionCatch;
import java.lang.classfile.instruction.FieldInstruction;
import java.lang.classfile.instruction.InvokeDynamicInstruction;
import java.lang.classfile.instruction.InvokeInstruction;
import java.lang.classfile.instruction.LabelTarget;
import java.lang.classfile.instruction.LookupSwitchInstruction;
import java.lang.classfile.instruction.MonitorInstruction;
import java.lang.classfile.instruction.ReturnInstruction;
import java.lang.classfile.instruction.SwitchCase;
import java.lang.classfile.instruction.TableSwitchInstruction;
import java.lang.classfile.instruction.ThrowInstruction;
import java.lang.classfile.instruction.TypeCheckInstruction;
import java.lang.reflect.AccessFlag;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;
import java.util.TreeMap;
import java.util.TreeSet;
import java.util.function.Predicate;
import java.util.regex.Matcher;
import java.util.regex.Pattern;
import java.util.stream.Stream;

/**
 * Class-file checker for the Truffle coding conventions of truffle-design.md §11/§16 (the
 * rules the owner's standing rules A and B name), run by {@link NodeConventionsTest} over the
 * compiled classes of the new node package and the shared runtime it uses.
 *
 * <p>The checker reads class files with the JDK's {@code java.lang.classfile} API; nothing is
 * loaded or initialized, so a run over the whole module is a fraction of a second. Each finding
 * is a {@link Violation}: a rule id, a location ({@code binary.ClassName} or
 * {@code binary.ClassName#member}) and a detail. The test compares the set of
 * {@code rule location} keys with the allowlist; see {@link #RULES} for the rules.
 */
public final class NodeConventions {

    /** Rule id → one-line statement (truffle-design.md §16 states them in full). */
    static final Map<String, String> RULES = Map.ofEntries(
        Map.entry("N1-field", "Node instance field is neither final nor @Child/@Children/@CompilationFinal"),
        Map.entry("N1-volatile", "volatile field in a Node class"),
        Map.entry("N1-sync", "synchronized method or block in a Node class"),
        Map.entry("N1-cfdims", "@CompilationFinal on an array field without explicit dimensions"),
        Map.entry("N1-children", "@Children field is not a final array"),
        Map.entry("N2-switch", "execute* method switches on the node's own field (the factory should choose the class)"),
        Map.entry("N4-explode", "@ExplodeLoop method reads an array field that is neither @Children nor @CompilationFinal(dimensions >= 1)"),
        Map.entry("N8-typecase", "execute* method of a non-DSL node tests >= 3 types by instanceof (a DSL specialization set)"),
        Map.entry("S1-static", "static field is not final"),
        Map.entry("S2-static-read", "PE-reachable code reads a non-final static field"),
        Map.entry("C1-final", "concrete class without subclasses is not final"),
        Map.entry("R1-record", "immutable carrier (final fields, accessor/equals/hashCode/toString only) is not a record"),
        Map.entry("D2-limit", "@Specialization with guards and a value @Cached has no explicit limit"),
        Map.entry("D4-idempotent", "guard helper is neither @Idempotent nor @NonIdempotent"),
        Map.entry("D6-neverdefault", "@Cached initializer helper returning a reference is not @NeverDefault"),
        Map.entry("B6-invalidate", "PE-reachable write of a @CompilationFinal/@Child field, or tree rewrite, not preceded by transferToInterpreterAndInvalidate"),
        Map.entry("N4-unexploded", "PE-reachable loop over a @Children array without @ExplodeLoop"),
        Map.entry("B2-frame", "@TruffleBoundary method takes a VirtualFrame/Frame (forces materialization)"),
        Map.entry("B3-boundary", "PE-reachable code calls a PE-hostile JDK method outside a @TruffleBoundary"),
        Map.entry("G1-calltarget", "calls CallTarget.call(Object...): its EncapsulatingNodeReference bracket would "
            + "span a goroutine park and be restored on another carrier; pass a location, call(Node, Object...)"));

    record Violation(String rule, String location, String detail) implements Comparable<Violation> {
        String key() {
            return rule + " " + location;
        }

        @Override
        public int compareTo(Violation o) {
            int c = key().compareTo(o.key());
            return c != 0 ? c : detail.compareTo(o.detail);
        }

        @Override
        public String toString() {
            return key() + "  (" + detail + ")";
        }
    }

    private static final String NODE = "com/oracle/truffle/api/nodes/Node";
    private static final String THROWABLE = "java/lang/Throwable";
    private static final String OBJECT = "java/lang/Object";
    private static final String A_CHILD = "Lcom/oracle/truffle/api/nodes/Node$Child;";
    private static final String A_CHILDREN = "Lcom/oracle/truffle/api/nodes/Node$Children;";
    private static final String A_CF = "Lcom/oracle/truffle/api/CompilerDirectives$CompilationFinal;";
    private static final String A_BOUNDARY = "Lcom/oracle/truffle/api/CompilerDirectives$TruffleBoundary;";
    private static final String A_EXPLODE = "Lcom/oracle/truffle/api/nodes/ExplodeLoop;";
    private static final String A_GENERATED = "Lcom/oracle/truffle/api/dsl/GeneratedBy;";
    private static final String A_SPEC = "Lcom/oracle/truffle/api/dsl/Specialization;";
    private static final String A_FALLBACK = "Lcom/oracle/truffle/api/dsl/Fallback;";
    private static final String A_CACHED = "Lcom/oracle/truffle/api/dsl/Cached;";
    private static final String A_IDEMPOTENT = "Lcom/oracle/truffle/api/dsl/Idempotent;";
    private static final String A_NONIDEMPOTENT = "Lcom/oracle/truffle/api/dsl/NonIdempotent;";
    private static final String A_NEVERDEFAULT = "Lcom/oracle/truffle/api/dsl/NeverDefault;";
    private static final String A_IMPORTSTATIC = "Lcom/oracle/truffle/api/dsl/ImportStatic;";
    private static final String CD = "com/oracle/truffle/api/CompilerDirectives";
    private static final String CA = "com/oracle/truffle/api/CompilerAsserts";
    private static final Pattern CALL = Pattern.compile("([A-Za-z_$][A-Za-z0-9_$]*)\\s*\\(");

    /** One parsed class. */
    private static final class Info {
        final String name;
        final ClassModel model;
        final String superName;
        final List<String> interfaces;
        final Map<String, Annotation> annotations;
        final boolean scanned;

        Info(String name, ClassModel model, boolean scanned) {
            this.name = name;
            this.model = model;
            this.superName = model.superclass().map(ClassEntry::asInternalName).orElse(null);
            this.interfaces = model.interfaces().stream().map(ClassEntry::asInternalName).toList();
            this.annotations = annotations(model);
            this.scanned = scanned;
        }

        boolean has(AccessFlag f) {
            return model.flags().has(f);
        }

        String dotted() {
            return name.replace('/', '.');
        }
    }

    private final Map<String, Info> classes = new HashMap<>();
    private final Map<String, Info> external = new HashMap<>();
    private final Map<String, List<String>> subtypes = new HashMap<>();
    private final Map<String, Boolean> nodeCache = new HashMap<>();
    private final Set<String> scope = new TreeSet<>();
    private final Set<String> primary = new HashSet<>();
    private final List<Violation> out = new ArrayList<>();

    private NodeConventions() {
    }

    /**
     * Checks the classes under {@code roots} (class-file directories) whose binary name passes
     * {@code inScope}; every class of every root is parsed so that the class hierarchy (final
     * classes, overriding methods, PE reachability) sees subclasses outside the scope.
     */
    static List<Violation> check(List<Path> roots, Predicate<String> inScope, Predicate<String> sharedRef) {
        NodeConventions c = new NodeConventions();
        for (int r = 0; r < roots.size(); r++) {
            c.load(roots.get(r), r == 0);
        }
        c.index();
        for (Info i : c.classes.values()) {
            if (c.primary.contains(i.name) && inScope.test(i.dotted())) {
                c.scope.add(i.name);
            }
        }
        // the shared runtime: classes the in-scope classes reference directly
        Map<String, List<String>> byTop = new HashMap<>();
        for (String n : c.primary) {
            byTop.computeIfAbsent(top(n), k -> new ArrayList<>()).add(n);
        }
        Set<String> shared = new TreeSet<>();
        for (String n : c.scope) {
            for (PoolEntry e : c.classes.get(n).model.constantPool()) {
                if (e instanceof ClassEntry ce) {
                    String t = top(ce.asInternalName());
                    if (sharedRef.test(t.replace('/', '.'))) {
                        shared.addAll(byTop.getOrDefault(t, List.of()));
                    }
                }
            }
        }
        c.scope.addAll(shared);
        c.run();
        c.out.sort(null);
        return c.out;
    }

    private static String top(String n) {
        int d = n.indexOf('$');
        return d < 0 ? n : n.substring(0, d);
    }

    private void load(Path root, boolean first) {
        try (Stream<Path> files = Files.walk(root)) {
            for (Path p : (Iterable<Path>) files.filter(f -> f.toString().endsWith(".class"))::iterator) {
                ClassModel m = ClassFile.of().parse(Files.readAllBytes(p));
                String n = m.thisClass().asInternalName();
                if (n.equals("module-info") || n.endsWith("package-info")) {
                    continue;
                }
                if (classes.putIfAbsent(n, new Info(n, m, true)) == null && first) {
                    primary.add(n);
                }
            }
        } catch (IOException e) {
            throw new UncheckedIOException(e);
        }
    }

    private void index() {
        for (Info i : classes.values()) {
            if (i.superName != null) {
                subtypes.computeIfAbsent(i.superName, k -> new ArrayList<>()).add(i.name);
            }
            for (String s : i.interfaces) {
                subtypes.computeIfAbsent(s, k -> new ArrayList<>()).add(i.name);
            }
        }
    }

    private Info info(String name) {
        Info i = classes.get(name);
        if (i != null) {
            return i;
        }
        if (external.containsKey(name)) {
            return external.get(name);
        }
        Info x = null;
        try (InputStream in = ClassLoader.getSystemResourceAsStream(name + ".class")) {
            if (in != null) {
                x = new Info(name, ClassFile.of().parse(in.readAllBytes()), false);
            }
        } catch (IOException e) {
            throw new UncheckedIOException(e);
        }
        external.put(name, x);
        return x;
    }

    private boolean isA(String name, String target) {
        for (String n = name; n != null; ) {
            if (n.equals(target)) {
                return true;
            }
            Info i = info(n);
            n = i == null ? null : i.superName;
        }
        return false;
    }

    private boolean isNode(String name) {
        return nodeCache.computeIfAbsent(name, n -> isA(n, NODE));
    }

    private boolean generated(Info i) {
        for (String n = i.name; ; n = n.substring(0, n.lastIndexOf('$'))) {
            Info o = classes.get(n);
            if (o != null && o.annotations.containsKey(A_GENERATED)) {
                return true;
            }
            if (!n.contains("$")) {
                return false;
            }
        }
    }

    private void report(String rule, String location, String detail) {
        out.add(new Violation(rule, location, detail));
    }

    private void run() {
        Set<String> entries = new TreeSet<>();
        for (String n : scope) {
            Info i = classes.get(n);
            boolean gen = generated(i);
            if (!gen) {
                for (MethodModel m : i.model.methods()) {
                    String d = m.methodType().stringValue();
                    if (boundary(m) && (d.contains("Lcom/oracle/truffle/api/frame/VirtualFrame;")
                            || d.contains("Lcom/oracle/truffle/api/frame/Frame;"))) {
                        report("B2-frame", i.dotted() + "#" + m.methodName().stringValue(), d);
                    }
                    checkCallTargetCall(i, m);
                }
                checkStatics(i);
                checkFinalClass(i);
                checkRecord(i);
            }
            if (isNode(n)) {
                if (!gen) {
                    checkNodeFields(i);
                    checkNodeMethods(i);
                    checkDsl(i);
                }
                for (MethodModel m : i.model.methods()) {
                    if (m.code().isPresent() && !boundary(m) && entry(m)) {
                        entries.add(key(n, m));
                    }
                }
            }
        }
        checkReachable(entries);
    }

    // ---- statics, final classes, records --------------------------------------------------

    /**
     * G1-calltarget: {@code CallTarget.call(Object...)} sets the calling thread's
     * EncapsulatingNodeReference on entry and restores it on exit. A goroutine that parks inside
     * the call can resume on another carrier (carrier mode, GG-0074), which then restores the
     * first carrier's reference: a Truffle assertion under -ea, a cross-thread race without.
     */
    private void checkCallTargetCall(Info i, MethodModel m) {
        if (m.code().isEmpty()) {
            return;
        }
        for (CodeElement e : m.code().get()) {
            if (e instanceof InvokeInstruction ii && ii.name().stringValue().equals("call")
                    && ii.type().stringValue().equals("([Ljava/lang/Object;)Ljava/lang/Object;")
                    && CALL_TARGETS.contains(ii.owner().asInternalName())) {
                report("G1-calltarget", i.dotted() + "#" + m.methodName().stringValue(), ii.owner().asInternalName());
                return;
            }
        }
    }

    private static final Set<String> CALL_TARGETS = Set.of("com/oracle/truffle/api/CallTarget",
        "com/oracle/truffle/api/RootCallTarget");

    private void checkStatics(Info i) {
        for (FieldModel f : i.model.fields()) {
            if (f.flags().has(AccessFlag.STATIC) && !f.flags().has(AccessFlag.FINAL)
                    && !f.flags().has(AccessFlag.SYNTHETIC)) {
                report("S1-static", i.dotted() + "#" + f.fieldName().stringValue(),
                    (f.flags().has(AccessFlag.VOLATILE) ? "static volatile " : "static ") + f.fieldType().stringValue());
            }
        }
    }

    private boolean anonymousOrLocal(Info i) {
        return i.model.findAttribute(Attributes.enclosingMethod()).isPresent();
    }

    private void checkFinalClass(Info i) {
        if (i.has(AccessFlag.FINAL) || i.has(AccessFlag.ABSTRACT) || i.has(AccessFlag.INTERFACE)
                || i.has(AccessFlag.ENUM) || i.has(AccessFlag.ANNOTATION) || anonymousOrLocal(i)
                || subtypes.containsKey(i.name)) {
            return;
        }
        // enum constants with bodies: the enum itself has ACC_ENUM; a sealed/permitted class is fine too
        if (i.model.findAttribute(Attributes.permittedSubclasses()).isPresent()) {
            return;
        }
        report("C1-final", i.dotted(), "concrete class with no subclass is not final");
    }

    private void checkRecord(Info i) {
        if (i.has(AccessFlag.INTERFACE) || i.has(AccessFlag.ABSTRACT) || i.has(AccessFlag.ENUM)
                || i.has(AccessFlag.ANNOTATION) || anonymousOrLocal(i) || subtypes.containsKey(i.name)
                || !OBJECT.equals(i.superName) || "java/lang/Record".equals(i.superName)) {
            return;
        }
        int fields = 0;
        Set<String> own = new HashSet<>();
        for (FieldModel f : i.model.fields()) {
            if (f.flags().has(AccessFlag.STATIC)) {
                continue;
            }
            if (!f.flags().has(AccessFlag.FINAL) || f.flags().has(AccessFlag.SYNTHETIC)) {
                return;
            }
            fields++;
            own.add(f.fieldName().stringValue());
        }
        if (fields == 0 || !canonicalConstructor(i)) {
            return;
        }
        for (MethodModel m : i.model.methods()) {
            String name = m.methodName().stringValue();
            if (m.flags().has(AccessFlag.STATIC) || m.flags().has(AccessFlag.SYNTHETIC)
                    || name.equals("<init>") || name.equals("<clinit>")) {
                continue;
            }
            String d = m.methodType().stringValue();
            if ((name.equals("equals") && d.equals("(Ljava/lang/Object;)Z"))
                    || (name.equals("hashCode") && d.equals("()I"))
                    || (name.equals("toString") && d.equals("()Ljava/lang/String;"))) {
                continue;
            }
            if (!accessor(i, m, own)) {
                return;
            }
        }
        report("R1-record", i.dotted(), fields + " final field(s), accessor-only");
    }

    /**
     * Whether the class has a constructor shaped like a record's canonical one: its parameters
     * are the instance fields' types in declaration order and its body only stores them (no
     * derived field, no validation), so the record conversion is mechanical.
     */
    private static boolean canonicalConstructor(Info i) {
        StringBuilder want = new StringBuilder("(");
        for (FieldModel f : i.model.fields()) {
            if (!f.flags().has(AccessFlag.STATIC)) {
                want.append(f.fieldType().stringValue());
            }
        }
        want.append(")V");
        for (MethodModel m : i.model.methods()) {
            if (!m.methodName().stringValue().equals("<init>") || !m.methodType().stringValue().equals(want.toString())
                    || m.code().isEmpty()) {
                continue;
            }
            boolean plain = true;
            for (Instruction x : instructions(m.code().get())) {
                boolean ok = x instanceof java.lang.classfile.instruction.LoadInstruction
                    || x instanceof ReturnInstruction
                    || (x instanceof FieldInstruction fi && fi.opcode() == Opcode.PUTFIELD && fi.owner().asInternalName().equals(i.name))
                    || (x instanceof InvokeInstruction ii && ii.opcode() == Opcode.INVOKESPECIAL
                        && ii.owner().asInternalName().equals(OBJECT) && ii.name().stringValue().equals("<init>"));
                if (!ok) {
                    plain = false;
                    break;
                }
            }
            if (plain) {
                return true;
            }
        }
        return false;
    }

    private static boolean accessor(Info i, MethodModel m, Set<String> own) {
        if (!m.methodType().stringValue().startsWith("()") || m.code().isEmpty()) {
            return false;
        }
        List<Instruction> ins = instructions(m.code().get());
        return ins.size() == 3 && ins.get(0).opcode() == Opcode.ALOAD_0
            && ins.get(1) instanceof FieldInstruction fi && fi.opcode() == Opcode.GETFIELD
            && fi.owner().asInternalName().equals(i.name) && own.contains(fi.name().stringValue())
            && ins.get(2) instanceof ReturnInstruction;
    }

    // ---- node fields and methods ----------------------------------------------------------

    private void checkNodeFields(Info i) {
        for (FieldModel f : i.model.fields()) {
            if (f.flags().has(AccessFlag.STATIC) || f.flags().has(AccessFlag.SYNTHETIC)) {
                continue;
            }
            String loc = i.dotted() + "#" + f.fieldName().stringValue();
            Map<String, Annotation> a = annotations(f);
            String type = f.fieldType().stringValue();
            if (f.flags().has(AccessFlag.VOLATILE)) {
                report("N1-volatile", loc, "volatile " + type);
            }
            boolean fin = f.flags().has(AccessFlag.FINAL);
            if (!fin && !a.containsKey(A_CHILD) && !a.containsKey(A_CHILDREN) && !a.containsKey(A_CF)) {
                report("N1-field", loc, type);
            }
            if (a.containsKey(A_CHILDREN) && (!fin || !type.startsWith("["))) {
                report("N1-children", loc, (fin ? "" : "non-final ") + type);
            }
            if (a.containsKey(A_CF) && type.startsWith("[") && intElement(a.get(A_CF), "dimensions") == null) {
                report("N1-cfdims", loc, type);
            }
        }
    }

    private void checkNodeMethods(Info i) {
        for (MethodModel m : i.model.methods()) {
            String loc = i.dotted() + "#" + m.methodName().stringValue();
            if (m.flags().has(AccessFlag.SYNCHRONIZED)) {
                report("N1-sync", loc, "synchronized method");
            }
            if (m.code().isEmpty()) {
                continue;
            }
            List<Instruction> ins = instructions(m.code().get());
            boolean bnd = boundary(m);
            if (!bnd) {
                for (Instruction x : ins) {
                    if (x instanceof MonitorInstruction mi && mi.opcode() == Opcode.MONITORENTER) {
                        report("N1-sync", loc, "synchronized block");
                        break;
                    }
                }
            }
            Map<String, Annotation> ma = annotations(m);
            if (ma.containsKey(A_EXPLODE)) {
                for (Instruction x : ins) {
                    if (x instanceof FieldInstruction fi && fi.opcode() == Opcode.GETFIELD
                            && fi.type().stringValue().startsWith("[") && isNode(fi.owner().asInternalName())) {
                        FieldModel f = field(fi.owner().asInternalName(), fi.name().stringValue());
                        if (f == null) {
                            continue;
                        }
                        Map<String, Annotation> fa = annotations(f);
                        Integer dims = fa.containsKey(A_CF) ? intElement(fa.get(A_CF), "dimensions") : null;
                        if (!fa.containsKey(A_CHILDREN) && (dims == null || dims < 1)) {
                            report("N4-explode", loc, "reads " + fi.name().stringValue() + " " + fi.type().stringValue());
                        }
                    }
                }
            }
            if (!m.methodName().stringValue().startsWith("execute") || bnd) {
                continue;
            }
            Set<String> typeTests = new TreeSet<>();
            for (int k = 0; k < ins.size(); k++) {
                Instruction x = ins.get(k);
                if (x instanceof TableSwitchInstruction || x instanceof LookupSwitchInstruction) {
                    String f = switchOnOwnField(i, ins, k);
                    if (f != null) {
                        report("N2-switch", loc, "switch on this." + f);
                    }
                } else if (x instanceof TypeCheckInstruction tc && tc.opcode() == Opcode.INSTANCEOF) {
                    typeTests.add(tc.type().asInternalName());
                }
            }
            if (typeTests.size() >= 3 && !dslNode(i)) {
                report("N8-typecase", loc, "instanceof " + typeTests);
            }
        }
    }

    private boolean dslNode(Info i) {
        for (MethodModel m : i.model.methods()) {
            Map<String, Annotation> a = annotations(m);
            if (a.containsKey(A_SPEC) || a.containsKey(A_FALLBACK)) {
                return true;
            }
        }
        return false;
    }

    /** The field a switch dispatches on when it is {@code this.f} (directly, by ordinal or a switch map). */
    private static String switchOnOwnField(Info i, List<Instruction> ins, int at) {
        int k = at - 1;
        if (k >= 0 && ins.get(k).opcode() == Opcode.IALOAD) {
            k--;
        }
        if (k >= 0 && ins.get(k) instanceof InvokeInstruction ii && ii.name().stringValue().equals("ordinal")) {
            k--;
        }
        if (k >= 1 && ins.get(k) instanceof FieldInstruction fi && fi.opcode() == Opcode.GETFIELD
                && fi.owner().asInternalName().equals(i.name) && ins.get(k - 1).opcode() == Opcode.ALOAD_0) {
            return fi.name().stringValue();
        }
        return null;
    }

    // ---- DSL ------------------------------------------------------------------------------

    private void checkDsl(Info i) {
        List<String> statics = new ArrayList<>();
        for (Info c = i; c != null; c = c.superName == null ? null : classes.get(c.superName)) {
            Annotation is = c.annotations.get(A_IMPORTSTATIC);
            if (is != null) {
                for (AnnotationElement e : is.elements()) {
                    if (e.value() instanceof AnnotationValue.OfArray arr) {
                        for (AnnotationValue v : arr.values()) {
                            if (v instanceof AnnotationValue.OfClass oc) {
                                statics.add(internal(oc.classSymbol().descriptorString()));
                            }
                        }
                    }
                }
            }
        }
        for (MethodModel m : i.model.methods()) {
            Annotation spec = annotations(m).get(A_SPEC);
            List<List<Annotation>> params = parameterAnnotations(m);
            List<String> guards = new ArrayList<>(stringsElement(spec, "guards"));
            guards.addAll(stringsElement(spec, "assumptions"));
            boolean valueCache = false;
            List<java.lang.constant.ClassDesc> ptypes = m.methodTypeSymbol().parameterList();
            for (int p = 0; p < params.size() && p < ptypes.size(); p++) {
                for (Annotation a : params.get(p)) {
                    if (!a.className().stringValue().equals(A_CACHED)) {
                        continue;
                    }
                    String pt = ptypes.get(p).descriptorString();
                    boolean nodeLike = pt.startsWith("L") && (isNode(internal(pt))
                        || internal(pt).startsWith("com/oracle/truffle/api/profiles/")
                        || internal(pt).startsWith("com/oracle/truffle/api/library/"));
                    if (!nodeLike) {
                        valueCache = true;
                    }
                    boolean neverDefaultSet = a.elements().stream().anyMatch(e -> e.name().stringValue().equals("neverDefault"));
                    for (String expr : stringsElement(a, "value")) {
                        for (MethodModel h : helpers(i, statics, expr)) {
                            if (!neverDefaultSet && h.methodType().stringValue().matches(".*\\)[L\\[].*")
                                    && !annotations(h).containsKey(A_NEVERDEFAULT)) {
                                report("D6-neverdefault", owner(i, statics, h) + "#" + h.methodName().stringValue(),
                                    "@Cached(\"" + expr + "\") in " + i.dotted() + "#" + m.methodName().stringValue());
                            }
                        }
                    }
                }
            }
            if (spec == null) {
                continue;
            }
            if (valueCache && !guards.isEmpty() && spec.elements().stream().noneMatch(e -> e.name().stringValue().equals("limit"))) {
                report("D2-limit", i.dotted() + "#" + m.methodName().stringValue(), "guards " + guards);
            }
            for (String g : guards) {
                for (MethodModel h : helpers(i, statics, g)) {
                    Map<String, Annotation> ha = annotations(h);
                    if (!ha.containsKey(A_IDEMPOTENT) && !ha.containsKey(A_NONIDEMPOTENT)) {
                        report("D4-idempotent", owner(i, statics, h) + "#" + h.methodName().stringValue(),
                            "guard \"" + g + "\" of " + i.dotted() + "#" + m.methodName().stringValue());
                    }
                }
            }
        }
    }

    private final Map<MethodModel, String> ownerOf = new HashMap<>();

    private String owner(Info i, List<String> statics, MethodModel h) {
        return ownerOf.getOrDefault(h, i.dotted());
    }

    /** Scanned, non-generated methods an expression calls: in the node, its supers and @ImportStatic classes. */
    private List<MethodModel> helpers(Info i, List<String> statics, String expr) {
        List<MethodModel> r = new ArrayList<>();
        Matcher mt = CALL.matcher(expr);
        while (mt.find()) {
            String name = mt.group(1);
            List<String> where = new ArrayList<>();
            for (Info c = i; c != null && c.scanned; c = c.superName == null ? null : classes.get(c.superName)) {
                where.add(c.name);
            }
            where.addAll(statics);
            for (String w : where) {
                Info c = classes.get(w);
                if (c == null || generated(c)) {
                    continue;
                }
                boolean found = false;
                for (MethodModel h : c.model.methods()) {
                    if (h.methodName().stringValue().equals(name)) {
                        r.add(h);
                        ownerOf.put(h, c.dotted());
                        found = true;
                    }
                }
                if (found) {
                    break;
                }
            }
        }
        return r;
    }

    // ---- PE reachability ------------------------------------------------------------------

    private static boolean entry(MethodModel m) {
        String n = m.methodName().stringValue();
        Map<String, Annotation> a = annotations(m);
        return n.startsWith("execute") || n.equals("isLoopCondition")
            || a.containsKey(A_SPEC) || a.containsKey(A_FALLBACK);
    }

    private static boolean boundary(MethodModel m) {
        return annotations(m).containsKey(A_BOUNDARY);
    }

    private static String key(String owner, MethodModel m) {
        return owner + "." + m.methodName().stringValue() + m.methodType().stringValue();
    }

    private MethodModel method(String owner, String name, String desc) {
        Info i = classes.get(owner);
        if (i == null) {
            return null;
        }
        for (MethodModel m : i.model.methods()) {
            if (m.methodName().stringValue().equals(name) && m.methodType().stringValue().equals(desc)) {
                return m;
            }
        }
        return null;
    }

    private FieldModel field(String owner, String name) {
        for (String o = owner; o != null; ) {
            Info i = info(o);
            if (i == null) {
                return null;
            }
            for (FieldModel f : i.model.fields()) {
                if (f.fieldName().stringValue().equals(name)) {
                    return f;
                }
            }
            o = i.superName;
        }
        return null;
    }

    /** The scanned implementations an invoke can reach: the resolved method and every override below. */
    private List<String> targets(InvokeInstruction ii) {
        String owner = ii.owner().asInternalName();
        String name = ii.name().stringValue();
        String desc = ii.type().stringValue();
        List<String> r = new ArrayList<>();
        for (String o = owner; o != null; ) {
            Info i = classes.get(o);
            if (i == null) {
                break;
            }
            if (method(o, name, desc) != null) {
                r.add(o + "." + name + desc);
                break;
            }
            o = i.superName;
        }
        if (ii.opcode() == Opcode.INVOKEVIRTUAL || ii.opcode() == Opcode.INVOKEINTERFACE) {
            ArrayDeque<String> q = new ArrayDeque<>(subtypes.getOrDefault(owner, List.of()));
            Set<String> seen = new HashSet<>();
            while (!q.isEmpty()) {
                String s = q.poll();
                if (!seen.add(s)) {
                    continue;
                }
                if (method(s, name, desc) != null) {
                    r.add(s + "." + name + desc);
                }
                q.addAll(subtypes.getOrDefault(s, List.of()));
            }
        }
        return r;
    }

    private void checkReachable(Set<String> entries) {
        ArrayDeque<String> work = new ArrayDeque<>(entries);
        Set<String> seen = new HashSet<>(entries);
        Map<String, Set<String>> hostile = new TreeMap<>();
        Map<String, Set<String>> staticReads = new TreeMap<>();
        Map<String, String> parent = new HashMap<>();
        Map<String, String> via = new TreeMap<>();
        Map<String, Set<String>> cfWrites = new TreeMap<>();
        while (!work.isEmpty()) {
            String k = work.poll();
            int paren = k.indexOf('(');
            int dot = k.lastIndexOf('.', paren);
            String owner = k.substring(0, dot);
            MethodModel m = method(owner, k.substring(dot + 1, paren), k.substring(paren));
            if (m == null || m.code().isEmpty() || boundary(m)) {
                continue;
            }
            Info oi = classes.get(owner);
            boolean report = scope.contains(owner) && !generated(oi);
            String loc = owner.replace('/', '.') + "#" + m.methodName().stringValue();
            if (m.flags().has(AccessFlag.SYNCHRONIZED) && report) {
                hostile.computeIfAbsent(loc, x -> new TreeSet<>()).add("synchronized method");
            }
            List<Instruction> live = peInstructions(m.code().get());
            if (report && isNode(owner) && !m.methodName().stringValue().equals("<init>")) {
                String arr = constantArrayLoop(m, live);
                if (arr != null) {
                    report("N4-unexploded", loc, "loops over " + arr);
                }
            }
            for (Instruction x : live) {
                if (report && !m.methodName().stringValue().equals("<init>")) {
                    String w = treeWrite(x);
                    if (w != null) {
                        cfWrites.computeIfAbsent(loc, y -> new TreeSet<>()).add(w);
                    }
                }
                String h = hostile(x);
                if (h != null && report) {
                    hostile.computeIfAbsent(loc, y -> new TreeSet<>()).add(h);
                    via.putIfAbsent(loc, chain(parent, k));
                }
                if (x instanceof FieldInstruction fi && fi.opcode() == Opcode.GETSTATIC && report) {
                    FieldModel f = field(fi.owner().asInternalName(), fi.name().stringValue());
                    if (f != null && !f.flags().has(AccessFlag.FINAL)) {
                        staticReads.computeIfAbsent(loc, y -> new TreeSet<>())
                            .add(fi.owner().asInternalName().replace('/', '.') + "." + fi.name().stringValue());
                    }
                }
                if (x instanceof InvokeInstruction ii && classes.containsKey(ii.owner().asInternalName())) {
                    for (String t : targets(ii)) {
                        String to = t.substring(0, t.lastIndexOf('.', t.indexOf('(')));
                        // v1 code (the interop module, v1 nodes) is not the new path: M3 deletes it
                        if (!primary.contains(to) || (isNode(to) && !scope.contains(to))) {
                            continue;
                        }
                        if (seen.add(t)) {
                            parent.put(t, k);
                            work.add(t);
                        }
                    }
                }
            }
        }
        hostile.forEach((loc, d) -> report("B3-boundary", loc, String.join(", ", d) + "; reached " + via.get(loc)));
        staticReads.forEach((loc, d) -> report("S2-static-read", loc, String.join(", ", d)));
        cfWrites.forEach((loc, d) -> report("B6-invalidate", loc, String.join(", ", d)));
    }

    /** A write to compilation-final node state, or a tree rewrite, made by an instruction. */
    private String treeWrite(Instruction x) {
        if (x instanceof FieldInstruction fi && fi.opcode() == Opcode.PUTFIELD) {
            FieldModel f = field(fi.owner().asInternalName(), fi.name().stringValue());
            if (f != null) {
                Map<String, Annotation> a = annotations(f);
                if (a.containsKey(A_CF) || a.containsKey(A_CHILD)) {
                    return "writes " + fi.name().stringValue();
                }
            }
        }
        if (x instanceof InvokeInstruction ii && isNode(ii.owner().asInternalName())) {
            String n = ii.name().stringValue();
            if (n.equals("replace") || n.equals("insert") || n.equals("adoptChildren")) {
                return "calls " + n;
            }
        }
        return null;
    }

    /**
     * The @Children array field a method loops over without {@code @ExplodeLoop} (a read of the
     * field plus a backward branch), or null. (A loop over a @CompilationFinal array may be a
     * guest-data loop that only copies from it, so only child arrays are checked.)
     */
    private String constantArrayLoop(MethodModel m, List<Instruction> live) {
        if (annotations(m).containsKey(A_EXPLODE)) {
            return null;
        }
        String arr = null;
        for (Instruction x : live) {
            if (x instanceof FieldInstruction fi && fi.opcode() == Opcode.GETFIELD && fi.type().stringValue().startsWith("[")
                    && isNode(fi.owner().asInternalName())) {
                FieldModel f = field(fi.owner().asInternalName(), fi.name().stringValue());
                if (f != null && annotations(f).containsKey(A_CHILDREN)) {
                    arr = fi.name().stringValue();
                }
            }
        }
        if (arr == null) {
            return null;
        }
        List<CodeElement> els = m.code().get().elementList();
        Map<Label, Integer> at = new HashMap<>();
        for (int k = 0; k < els.size(); k++) {
            if (els.get(k) instanceof LabelTarget lt) {
                at.put(lt.label(), k);
            }
        }
        for (int k = 0; k < els.size(); k++) {
            if (els.get(k) instanceof BranchInstruction b && at.get(b.target()) < k) {
                return arr;
            }
        }
        return null;
    }

    /** The call path from a PE entry point to {@code k}, innermost first (diagnostics). */
    private static String chain(Map<String, String> parent, String k) {
        StringBuilder sb = new StringBuilder();
        int n = 0;
        for (String p = parent.get(k); p != null && n < 4; p = parent.get(p), n++) {
            sb.append(n == 0 ? "from " : " <- ").append(p.substring(p.lastIndexOf('/', p.indexOf('(')) + 1, p.indexOf('(')));
        }
        return n == 0 ? "as an entry point" : sb.toString();
    }

    /** The instructions of a method reachable without passing a transfer to the interpreter. */
    private static List<Instruction> peInstructions(CodeModel code) {
        List<CodeElement> els = code.elementList();
        Map<Label, Integer> at = new HashMap<>();
        List<ExceptionCatch> catches = new ArrayList<>();
        for (int k = 0; k < els.size(); k++) {
            if (els.get(k) instanceof LabelTarget lt) {
                at.put(lt.label(), k);
            } else if (els.get(k) instanceof ExceptionCatch ec) {
                catches.add(ec);
            }
        }
        boolean[] live = new boolean[els.size()];
        ArrayDeque<Integer> q = new ArrayDeque<>();
        q.add(0);
        while (true) {
            while (!q.isEmpty()) {
                int k = q.poll();
                while (k < els.size() && !live[k]) {
                    live[k] = true;
                    CodeElement e = els.get(k);
                    if (e instanceof InvokeInstruction ii && interpreterOnly(ii)) {
                        break;
                    }
                    if (e instanceof BranchInstruction b) {
                        q.add(at.get(b.target()));
                        if (b.opcode() == Opcode.GOTO || b.opcode() == Opcode.GOTO_W) {
                            break;
                        }
                    } else if (e instanceof TableSwitchInstruction ts) {
                        q.add(at.get(ts.defaultTarget()));
                        ts.cases().forEach(c -> q.add(at.get(c.target())));
                        break;
                    } else if (e instanceof LookupSwitchInstruction ls) {
                        q.add(at.get(ls.defaultTarget()));
                        for (SwitchCase c : ls.cases()) {
                            q.add(at.get(c.target()));
                        }
                        break;
                    } else if (e instanceof ReturnInstruction || e instanceof ThrowInstruction) {
                        break;
                    }
                    k++;
                }
            }
            boolean more = false;
            for (ExceptionCatch ec : catches) {
                int h = at.get(ec.handler());
                if (live[h]) {
                    continue;
                }
                for (int k = at.get(ec.tryStart()); k < at.get(ec.tryEnd()); k++) {
                    if (live[k] && els.get(k) instanceof Instruction) {
                        q.add(h);
                        more = true;
                        break;
                    }
                }
            }
            if (!more) {
                break;
            }
        }
        List<Instruction> r = new ArrayList<>();
        for (int k = 0; k < els.size(); k++) {
            if (live[k] && els.get(k) instanceof Instruction in) {
                r.add(in);
            }
        }
        return r;
    }

    private static boolean interpreterOnly(InvokeInstruction ii) {
        String o = ii.owner().asInternalName();
        String n = ii.name().stringValue();
        return (o.equals(CD) && (n.startsWith("transferToInterpreter") || n.equals("shouldNotReachHere")))
            || (o.equals(CA) && n.equals("neverPartOfCompilation"));
    }

    private static final Set<String> STRING_OK = Set.of("length", "charAt", "isEmpty", "equals", "hashCode", "coder");
    private static final Set<String> BOX_OWNERS = Set.of("java/lang/Integer", "java/lang/Long", "java/lang/Short",
        "java/lang/Byte", "java/lang/Character", "java/lang/Boolean", "java/lang/Double", "java/lang/Float");
    private static final Set<String> ARRAYS_OK = Set.of("equals", "mismatch", "compare", "compareUnsigned",
        "copyOf", "copyOfRange");

    /** The PE-hostile call an instruction makes (truffle-design.md §11 B3/B4), or null. */
    static String hostile(Instruction x) {
        if (x instanceof MonitorInstruction mi) {
            return mi.opcode() == Opcode.MONITORENTER ? "synchronized block" : null;
        }
        if (x instanceof InvokeDynamicInstruction indy) {
            String bsm = indy.bootstrapMethod().owner().descriptorString();
            if (bsm.equals("Ljava/lang/invoke/StringConcatFactory;")) {
                return "string concatenation";
            }
            return bsm.equals("Ljava/lang/runtime/ObjectMethods;") ? "record " + indy.name().stringValue() + " (ObjectMethods)" : null;
        }
        if (!(x instanceof InvokeInstruction ii)) {
            return null;
        }
        String o = ii.owner().asInternalName();
        String n = ii.name().stringValue();
        String d = ii.type().stringValue();
        String what = o.replace('/', '.') + "." + n;
        if (!(o.startsWith("java/") || o.startsWith("javax/") || o.startsWith("jdk/") || o.startsWith("sun/"))) {
            return null;
        }
        if (o.equals("java/lang/AssertionError")) {
            return null; // an assert statement: folds away when assertions are disabled
        }
        if (o.startsWith("java/lang/") && o.substring(10).matches("[A-Za-z]*(Exception|Error)")
                && n.equals("<init>") && !o.equals("java/lang/AssertionError")) {
            return "new " + o.substring(10);
        }
        switch (o) {
            case "java/lang/Math", "java/lang/StrictMath", "java/lang/Object", "java/lang/Enum", "java/lang/Record",
                "java/lang/Number", "java/lang/ref/Reference", "java/lang/ref/WeakReference",
                "java/lang/invoke/VarHandle", "java/lang/Throwable", "java/lang/RuntimeException",
                "java/lang/Exception", "java/lang/Error" -> {
                if (o.equals("java/lang/Object") && n.equals("toString")) {
                    return what;
                }
                if (o.equals("java/lang/Enum") && (n.equals("name") || n.equals("toString") || n.equals("valueOf"))) {
                    return what;
                }
                if (o.startsWith("java/lang/ref/") && !n.equals("get") && !n.equals("<init>")) {
                    return what;
                }
                if (o.equals("java/lang/Throwable") || o.endsWith("Exception") || o.endsWith("Error")) {
                    return n.equals("<init>") ? null : what;
                }
                return null;
            }
            case "java/lang/String" -> {
                return STRING_OK.contains(n) ? null : what;
            }
            case "java/lang/System" -> {
                return n.equals("arraycopy") || n.equals("identityHashCode") || n.equals("nanoTime")
                    || n.equals("currentTimeMillis") ? null : what;
            }
            case "java/lang/Thread" -> {
                return n.equals("currentThread") || n.equals("onSpinWait") ? null : what;
            }
            case "java/lang/Class" -> {
                return n.equals("cast") || n.equals("isInstance") || n.equals("desiredAssertionStatus") ? null : what;
            }
            case "java/util/Arrays" -> {
                return n.equals("fill") || (ARRAYS_OK.contains(n) && !d.contains("[L")) ? null : what;
            }
            case "java/util/Objects" -> {
                return n.equals("requireNonNull") || n.equals("equals") ? null : what;
            }
            default -> {
            }
        }
        if (BOX_OWNERS.contains(o)) {
            return d.endsWith(")Ljava/lang/String;") || n.startsWith("parse") || n.equals("decode") ? what : null;
        }
        if (o.startsWith("java/util/concurrent/atomic/")) {
            return null;
        }
        return what;
    }

    // ---- class-file helpers ---------------------------------------------------------------

    private static List<Instruction> instructions(CodeModel code) {
        List<Instruction> r = new ArrayList<>();
        for (CodeElement e : code) {
            if (e instanceof Instruction in) {
                r.add(in);
            }
        }
        return r;
    }

    private static String internal(String desc) {
        return desc.startsWith("L") ? desc.substring(1, desc.length() - 1) : desc;
    }

    private static Map<String, Annotation> annotations(java.lang.classfile.AttributedElement e) {
        Map<String, Annotation> r = new HashMap<>();
        e.findAttribute(Attributes.runtimeVisibleAnnotations())
            .ifPresent(a -> a.annotations().forEach(x -> r.put(x.className().stringValue(), x)));
        e.findAttribute(Attributes.runtimeInvisibleAnnotations())
            .ifPresent(a -> a.annotations().forEach(x -> r.put(x.className().stringValue(), x)));
        return r;
    }

    private static List<List<Annotation>> parameterAnnotations(MethodModel m) {
        List<List<Annotation>> r = new ArrayList<>();
        Optional<List<List<Annotation>>> vis = m.findAttribute(Attributes.runtimeVisibleParameterAnnotations())
            .map(a -> a.parameterAnnotations());
        Optional<List<List<Annotation>>> inv = m.findAttribute(Attributes.runtimeInvisibleParameterAnnotations())
            .map(a -> a.parameterAnnotations());
        int n = Math.max(vis.map(List::size).orElse(0), inv.map(List::size).orElse(0));
        for (int p = 0; p < n; p++) {
            List<Annotation> l = new ArrayList<>();
            final int q = p;
            vis.filter(v -> q < v.size()).ifPresent(v -> l.addAll(v.get(q)));
            inv.filter(v -> q < v.size()).ifPresent(v -> l.addAll(v.get(q)));
            r.add(l);
        }
        // parameter annotations index the declared parameters; the descriptor may have more (none here)
        return r;
    }

    private static Integer intElement(Annotation a, String name) {
        for (AnnotationElement e : a.elements()) {
            if (e.name().stringValue().equals(name) && e.value() instanceof AnnotationValue.OfInt i) {
                return i.intValue();
            }
        }
        return null;
    }

    private static List<String> stringsElement(Annotation a, String name) {
        List<String> r = new ArrayList<>();
        if (a == null) {
            return r;
        }
        for (AnnotationElement e : a.elements()) {
            if (!e.name().stringValue().equals(name)) {
                continue;
            }
            if (e.value() instanceof AnnotationValue.OfString s) {
                r.add(s.stringValue());
            } else if (e.value() instanceof AnnotationValue.OfArray arr) {
                for (AnnotationValue v : arr.values()) {
                    if (v instanceof AnnotationValue.OfString s) {
                        r.add(s.stringValue());
                    }
                }
            }
        }
        return r;
    }
}
