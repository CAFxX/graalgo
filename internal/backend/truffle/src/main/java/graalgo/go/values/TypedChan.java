package graalgo.go.values;

import graalgo.go.types.GoType;
import graalgo.runtime.GoChan;

/**
 * A channel of the node package: the runtime's {@link GoChan} with its element type, which
 * reflect's channel operations need to copy an element to or from an address (gc keeps it in
 * {@code hchan.elemtype}).
 */
public final class TypedChan extends GoChan {
    public final GoType elem;

    public TypedChan(GoType elem, int capacity) {
        super(capacity, elem.zero());
        this.elem = elem;
    }
}
