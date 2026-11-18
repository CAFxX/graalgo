package graalgo.runtime.sync;

/**
 * A 32-bit atomic cell (fix-linkname, Caddy/godebug lane).
 *
 * <p>The 32-bit counterpart of {@link LongCell}: abstracts over the
 * {@code sync/atomic} wrapper types ({@link GoAtomicInt32}) and
 * arbitrary Go addresses ({@link GoPointerCell32}, which also serves
 * the bool package ops on the 0/1 int encoding).
 */
public interface IntCell {
    int get();

    void set(int newValue);

    int getAndSet(int newValue);

    boolean compareAndSet(int expect, int update);

    int getAndAdd(int delta);

    int addAndGet(int delta);
}
