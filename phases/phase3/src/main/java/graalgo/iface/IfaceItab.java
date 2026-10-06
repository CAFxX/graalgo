package graalgo.iface;

import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.Map;

import graalgo.values.GoFunc;

/**
 * An immutable interface method table (D6): method name → {@link GoFunc}
 * for one (dynamic type, interface) pair.
 *
 * <p>Built once per (descriptor name, interface id) and cached in
 * {@link IfaceMethodTable}; itabs never change afterwards, so dispatch
 * PICs can compare them by identity with no invalidation machinery
 * (SPEC §3.3, D6: itab speculation needs no Assumptions). The method map
 * preserves interface-method order (a {@link LinkedHashMap} snapshot).
 */
public final class IfaceItab {
    private final String descriptorName;
    private final int ifaceId;
    private final Map<String, GoFunc> methods;

    IfaceItab(String descriptorName, int ifaceId, Map<String, GoFunc> methodsInIfaceOrder) {
        this.descriptorName = descriptorName;
        this.ifaceId = ifaceId;
        this.methods = Collections.unmodifiableMap(new LinkedHashMap<>(methodsInIfaceOrder));
    }

    /** The dynamic type's descriptor name (the {@link IfaceMethodTable} key). */
    public String descriptorName() {
        return descriptorName;
    }

    /** The wire interface type-table id this itab was built for. */
    public int ifaceId() {
        return ifaceId;
    }

    /** Method name → GoFunc, in interface-method order. Never null values. */
    public Map<String, GoFunc> methods() {
        return methods;
    }

    @Override
    public String toString() {
        return "IfaceItab{" + descriptorName + ", iface=" + ifaceId
                + ", methods=" + methods.keySet() + "}";
    }
}
