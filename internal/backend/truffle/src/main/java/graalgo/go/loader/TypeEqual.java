package graalgo.go.loader;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.frame.VirtualFrame;
import com.oracle.truffle.api.nodes.RootNode;
import graalgo.go.runtime.Panics;
import graalgo.go.runtime.Unsafe;
import graalgo.go.runtime.Values;
import graalgo.go.types.GoType;
import graalgo.language.GoLanguage;

/**
 * The equality function of a type descriptor ({@code abi.Type.Equal},
 * {@code func(p, q unsafe.Pointer) bool}): Go {@code ==} on the two values of the type the
 * pointers address. gc generates one per comparable type (reflectdata.geneq); here one root
 * serves every type, with the type as its constant.
 */
final class TypeEqual extends RootNode {
    private final GoType type;

    TypeEqual(GoLanguage language, GoType type) {
        super(language);
        this.type = type;
    }

    @Override
    public String getName() {
        return "type:.eq." + type.key;
    }

    @Override
    public boolean isInternal() {
        return true;
    }

    @Override
    public Object execute(VirtualFrame frame) {
        Object[] a = frame.getArguments();
        return equal(a[1], a[2]);
    }

    @TruffleBoundary
    private boolean equal(Object p, Object q) {
        try {
            return Values.equal(type, Unsafe.loadThrough(this, p, type), Unsafe.loadThrough(this, q, type));
        } catch (Values.Uncomparable u) {
            // gc's efaceeq/ifaceeq: an interface holding an uncomparable dynamic type panics.
            throw Panics.uncomparable(this, u.type);
        }
    }
}
