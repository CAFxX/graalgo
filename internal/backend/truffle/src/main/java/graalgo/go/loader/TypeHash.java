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
 * The hash function of a map type descriptor ({@code abi.MapType.Hasher},
 * {@code func(p unsafe.Pointer, seed uintptr) uintptr}): a hash of the key the pointer
 * addresses, consistent with Go {@code ==} on the key type ({@link Values#hash}, which panics
 * like gc for an unhashable dynamic type), spread over the 64 bits and mixed with the seed —
 * upstream's {@code internal/sync.HashTrieMap} ({@code sync.Map}) consumes it from the top
 * bits down.
 */
final class TypeHash extends RootNode {
    private final GoType key;

    TypeHash(GoLanguage language, GoType key) {
        super(language);
        this.key = key;
    }

    @Override
    public String getName() {
        return "type:.hash." + key.key;
    }

    @Override
    public boolean isInternal() {
        return true;
    }

    @Override
    public Object execute(VirtualFrame frame) {
        Object[] a = frame.getArguments();
        return hash(a[1], (long) a[2]);
    }

    @TruffleBoundary
    private long hash(Object p, long seed) {
        // The finalizer of SplitMix64: every input bit reaches every output bit.
        long z;
        try {
            z = Values.hash(key, Unsafe.loadThrough(this, p, key)) + seed * 0x9E3779B97F4A7C15L;
        } catch (Values.Uncomparable u) {
            // gc's typehash: an interface holding an unhashable dynamic type panics.
            throw Panics.unhashable(this, u.type);
        }
        z = (z ^ (z >>> 30)) * 0xBF58476D1CE4E5B9L;
        z = (z ^ (z >>> 27)) * 0x94D049BB133111EBL;
        return z ^ (z >>> 31);
    }
}
