package graalgo.iface;

import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

import graalgo.frontend.FrontendException;
import graalgo.values.GoTypeDescriptor;

/**
 * Static registry of interface method tables (P3-M3, WS D, ADR-0003):
 * descriptor name → (method name → {@link IfaceMethodTarget}).
 *
 * <p>Installed once per program by the phase-3 launcher after
 * {@code factory.build()} resolves method object ids through
 * {@code Program.functions()}. Installs are synchronized and defensively
 * copied; installing also drops the itab cache (itabs hold {@link GoFunc}s
 * from the previous install).
 *
 * <p>Concurrency: the table reference is volatile and replaced wholesale,
 * so the dispatch read path is lock-free; install/clear take the lock for
 * the atomic table+cache swap. Installs happen before goroutines start
 * (and single-threaded in tests), so a dispatch racing an install is
 * outside the supported lifecycle.
 *
 * <p>Descriptor-name keying: named dynamic types key by the display
 * name ({@code GoTypeDescriptor.name()}, canonical via the factory's
 * {@code NAMED_DESCS} — the package NAME form, e.g. {@code json.Number},
 * not the qualified path); pointer-to-named types key by
 * {@code "*"+qualifiedName} (e.g. {@code *errors.errorString}); other
 * pointer types key by the factory's structural descriptor name — see
 * {@link DescriptorNames} for the replica and the recorded ADR-0003 D5
 * deviation.
 */
public final class IfaceMethodTable {
    private IfaceMethodTable() {}

    private static final Object LOCK = new Object();
    private static volatile Map<String, Map<String, IfaceMethodTarget>> table = Map.of();
    /** (descriptor name, iface id) → built itab. */
    private static final ConcurrentHashMap<ItabKey, IfaceItab> itabs = new ConcurrentHashMap<>();
    /**
     * P3-M4 (WS C, ADR-0004 D3; P2-1: values are now
     * {@link IfaceWireEntry}): interface descriptor name → wire identity,
     * for the {@code Intrinsics.itabFor} intrinsic (which has no wire
     * ifacemethod node to carry the id or the method list). Populated by
     * the launcher from the doc type table alongside {@link #install}.
     */
    private static volatile Map<String, IfaceWireEntry> ifaceMethods = Map.of();
    /**
     * P3-M4 remediation (P2-1): the wire identity of an interface for the
     * {@code Intrinsics.itabFor} intrinsic path — the type-table id plus
     * the method names in declaration order. The intrinsic path uses the
     * SAME id namespace as the dispatch path (whose ifacemethod "iface"
     * field is a type-table id), so the two paths share the {@code itabs}
     * cache key unambiguously. (The old process-counter minting, starting
     * at 1, could collide with a wire id for a different interface and
     * return the wrong itab.)
     */
    public record IfaceWireEntry(int wireId, List<String> methodNames) {}

    private record ItabKey(String descriptorName, int ifaceId) {}

    /**
     * Installs a single method target (P3-M9, RC-6): used by the runtime to
     * register Java-implemented methods (e.g. fmt.State)
     * that have no Go body in the export. Drops the itab cache.
     *
     * @param descriptorName the concrete type's descriptor name
     * @param methodName the method name
     * @param target the method target
     */
    public static void installMethod(String descriptorName, String methodName, IfaceMethodTarget target) {
        synchronized (LOCK) {
            Map<String, Map<String, IfaceMethodTarget>> copy = new HashMap<>(table);
            Map<String, IfaceMethodTarget> methods = new HashMap<>(copy.getOrDefault(descriptorName, Map.of()));
            methods.put(methodName, target);
            copy.put(descriptorName, Map.copyOf(methods));
            table = Map.copyOf(copy);
            itabs.clear();
        }
    }

    /**
     * Installs the method tables. Defensive deep copy: later mutation of
     * the caller's maps cannot corrupt the registry. Drops the itab cache.
     */
    public static void install(Map<String, Map<String, IfaceMethodTarget>> methodTable) {
        Map<String, Map<String, IfaceMethodTarget>> copy = new HashMap<>();
        for (Map.Entry<String, Map<String, IfaceMethodTarget>> e : methodTable.entrySet()) {
            copy.put(e.getKey(), Map.copyOf(e.getValue()));
        }
        Map<String, Map<String, IfaceMethodTarget>> frozen = Map.copyOf(copy);
        synchronized (LOCK) {
            table = frozen;
            itabs.clear();
        }
    }

    /** Clears the registry and the itab cache (tests). */
    public static void clear() {
        synchronized (LOCK) {
            table = Map.of();
            ifaceMethods = Map.of();
            itabs.clear();
        }
    }

    /**
     * Installs the interface method-name lists (P3-M4, ADR-0004 D3):
     * interface descriptor name → method names in declaration order.
     * Called by the launcher alongside {@link #install}; like it, the
     * install is wholesale and drops nothing else.
     */
    public static void installInterfaces(Map<String, IfaceWireEntry> methods) {
        Map<String, IfaceWireEntry> copy = new HashMap<>();
        for (Map.Entry<String, IfaceWireEntry> e : methods.entrySet()) {
            IfaceWireEntry en = e.getValue();
            copy.put(e.getKey(), new IfaceWireEntry(en.wireId(), List.copyOf(en.methodNames())));
        }
        synchronized (LOCK) {
            ifaceMethods = Map.copyOf(copy);
        }
    }

    /**
     * The {@code Intrinsics.itabFor} intrinsic entry (P3-M4, ADR-0004 D3;
     * P2-1: one id namespace): builds (and caches) the itab for a concrete
     * descriptor against the named interface, resolving the interface's
     * wire id and method list from {@link #installInterfaces}. The
     * returned itab is the identical cached instance the dispatch path
     * builds for the same (type, wire iface id) pair — same key, same
     * itab.
     *
     * @param descriptorName the concrete type's descriptor name
     * @param ifaceName the interface's descriptor name
     */
    public static IfaceItab itabForIntrinsic(String descriptorName, String ifaceName) {
        IfaceWireEntry entry = ifaceMethods.get(ifaceName);
        if (entry == null) {
            throw new FrontendException("interface dispatch: no interface method list installed for \""
                    + ifaceName + "\" (needs gofrontend >= 0.5.0 interface entries)");
        }
        // One namespace: the wire id, identical to the dispatch path's.
        return itabFor(descriptorName, entry.wireId(), entry.methodNames());
    }

    /**
     * The {@code Intrinsics.itabFor} provider target (P3-M4, ADR-0004 D3):
     * adapts the frozen {@code (GoTypeDescriptor, GoTypeDescriptor)}
     * intrinsic signature to {@link #itabForIntrinsic}.
     */
    public static Object itabForDescriptors(GoTypeDescriptor descriptor, GoTypeDescriptor iface) {
        if (descriptor == null || iface == null) {
            throw new FrontendException("itabFor: null descriptor");
        }
        return itabForIntrinsic(descriptor.name(), iface.name());
    }

    /** Number of descriptor entries currently installed (tests). */
    static int size() {
        return table.size();
    }

    /** Number of cached itabs (tests: PIC-hit counting). */
    static int itabCacheSize() {
        return itabs.size();
    }

    /**
     * Looks up a method target by (descriptor name, method name) (P3-M9,
     * RC-6: public for callers installing method targets from the runtime).
     *
     * @return the target, or null if not found
     */
    public static IfaceMethodTarget lookup(String descriptorName, String methodName) {
        Map<String, IfaceMethodTarget> methods = table.get(descriptorName);
        return methods == null ? null : methods.get(methodName);
    }

    /**
     * Returns the itab for (descriptor name, interface id), building and
     * caching it on first use. The method map is laid out in
     * {@code ifaceMethodNames} order.
     *
     * <p>R2 P2-7: unsupported promotions (wire {@code "index"} null) do
     * NOT fail here — the itab carries the unsupported target and the
     * failure fires only if that method is actually dispatched (the
     * dispatch node checks {@link IfaceMethodTarget#supported()}). A
     * program that only dispatches supported methods through the
     * interface works fine.
     *
     * @throws FrontendException if the descriptor has no installed table,
     *         or a method is missing — both unreachable for well-formed
     *         gofrontend ≥ 0.4.0 output (go/types verified assignability),
     *         so both fail fast instead of dispatching wrong.
     *
     * <p>Public for the reflect redispatch path (P3-M9 C-1 fix), which
     * builds synthetic method funcs for embedded-interface-promoted
     * methods outside the iface package.
     */
    public static IfaceItab itabFor(String descriptorName, int ifaceId, List<String> ifaceMethodNames) {
        ItabKey key = new ItabKey(descriptorName, ifaceId);
        return itabs.computeIfAbsent(key,
                k -> buildItab(descriptorName, ifaceId, ifaceMethodNames, table));
    }

    private static IfaceItab buildItab(String descriptorName, int ifaceId,
            List<String> ifaceMethodNames, Map<String, Map<String, IfaceMethodTarget>> snapshot) {
        Map<String, IfaceMethodTarget> installed = snapshot.get(descriptorName);
        if (installed == null) {
            throw new FrontendException("interface dispatch: no method table installed for dynamic type \""
                    + descriptorName + "\" (needs gofrontend >= 0.4.0 WS D method tables)");
        }
        Map<String, IfaceMethodTarget> ordered = new LinkedHashMap<>();
        for (String name : ifaceMethodNames) {
            IfaceMethodTarget t = installed.get(name);
            if (t == null) {
                throw new FrontendException("interface dispatch: method \"" + name
                        + "\" missing for dynamic type \"" + descriptorName
                        + "\" (unreachable: go/types verified assignability)");
            }
            ordered.put(name, t);
        }
        return new IfaceItab(descriptorName, ifaceId, ordered);
    }
}
