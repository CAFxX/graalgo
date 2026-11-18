package graalgo.go.intrinsics;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.nodes.Node;
import graalgo.go.values.GoMap;
import graalgo.go.values.Iface;

/**
 * What the runtime provides to package {@code maps} by {@code //go:linkname}; registered in
 * {@code internal/toolexec/intrinsics/registry.txt}.
 */
final class MapsIntrinsics {
    private MapsIntrinsics() {}

    /** {@code maps.clone(m any) any}: m holds a map; the result holds its copy (nil stays nil). */
    @TruffleBoundary
    static Object maps_clone(Node node, Object m) {
        Iface i = (Iface) m;
        return i.data == null ? i : new Iface(i.type, ((GoMap) i.data).copy());
    }
}
