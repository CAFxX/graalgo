package graalgo.p3link;

import graalgo.frontend.FrontendException;
import graalgo.p3unsafe.UnsafeTable;
import graalgo.runtime.sync.IntCell;

/**
 * An {@link IntCell} view over a Go {@code *uint32} passed as a D5-boxed
 * address (long). Used by the {@code runtime_Semacquire}/
 * {@code runtime_Semrelease} intrinsics.
 *
 * <p>The address resolves via {@link UnsafeTable} to an
 * {@link UnsafeTable.ArrayRef}; the uint32 lives in the backing array
 * at the ref's offset. For a {@code *uint32}, the backing array is an
 * {@code int[]} (Java int is 32 bits).
 */
public final class AddressIntCell implements IntCell {
    private static final Object LOCK = new Object();

    private final long address;

    public AddressIntCell(long address) {
        if (address == 0) {
            throw new IllegalArgumentException("AddressIntCell: nil address");
        }
        this.address = address;
    }

    private int[] arrayAndOffset(int[] offsetOut) {
        Object ref = UnsafeTable.current().resolve(address);
        if (!(ref instanceof UnsafeTable.ArrayRef ar)) {
            throw new FrontendException(
                    "AddressIntCell: address does not resolve to ArrayRef (got "
                            + (ref == null ? "null" : ref.getClass().getSimpleName()) + ")");
        }
        Object arr = ar.array();
        if (!(arr instanceof int[] ints)) {
            throw new FrontendException(
                    "AddressIntCell: expected int[] backing for uint32 (got "
                            + arr.getClass().getSimpleName() + ")");
        }
        offsetOut[0] = ar.offset();
        return ints;
    }

    @Override
    public int get() {
        synchronized (LOCK) {
            int[] off = new int[1];
            int[] arr = arrayAndOffset(off);
            return arr[off[0]];
        }
    }

    @Override
    public void set(int newValue) {
        synchronized (LOCK) {
            int[] off = new int[1];
            int[] arr = arrayAndOffset(off);
            arr[off[0]] = newValue;
        }
    }

    @Override
    public int getAndSet(int newValue) {
        synchronized (LOCK) {
            int[] off = new int[1];
            int[] arr = arrayAndOffset(off);
            int old = arr[off[0]];
            arr[off[0]] = newValue;
            return old;
        }
    }

    @Override
    public boolean compareAndSet(int expect, int update) {
        synchronized (LOCK) {
            int[] off = new int[1];
            int[] arr = arrayAndOffset(off);
            if (arr[off[0]] == expect) {
                arr[off[0]] = update;
                return true;
            }
            return false;
        }
    }

    @Override
    public int getAndAdd(int delta) {
        synchronized (LOCK) {
            int[] off = new int[1];
            int[] arr = arrayAndOffset(off);
            int old = arr[off[0]];
            arr[off[0]] = old + delta;
            return old;
        }
    }

    @Override
    public int addAndGet(int delta) {
        synchronized (LOCK) {
            int[] off = new int[1];
            int[] arr = arrayAndOffset(off);
            int next = arr[off[0]] + delta;
            arr[off[0]] = next;
            return next;
        }
    }

    @Override
    public boolean equals(Object o) {
        if (!(o instanceof AddressIntCell other)) {
            return false;
        }
        return address == other.address;
    }

    @Override
    public int hashCode() {
        return Long.hashCode(address);
    }
}
