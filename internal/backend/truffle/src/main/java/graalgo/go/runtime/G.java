package graalgo.go.runtime;

import com.oracle.truffle.api.nodes.Node;
import graalgo.language.GoLanguage;
import graalgo.language.GoThreadSlot;

/**
 * Per-goroutine state of the new node package (truffle-design §2.5): the explicit defer chain
 * (SPEC §3.5, GG-0071) and the panic being unwound. Reached through the language's
 * {@code ContextThreadLocal} slot — never a host {@code ThreadLocal} — and only by functions
 * that defer, recover, or start a goroutine.
 */
public final class G {
    /** One deferred call: target and its already evaluated arguments (calling convention form). */
    public static class Defer {
        public final Object target;
        public final Object[] args;
        public final Defer next;

        public Defer(Object target, Object[] args, Defer next) {
            this.target = target;
            this.args = args;
            this.next = next;
        }
    }

    /**
     * The deferred calls of the range-over-func loop bodies of one loop execution (gc's
     * {@code deferrangefunc} record): it sits on the chain at the enclosing function's level,
     * below the records of the sequence function, and is run by the enclosing function in its
     * place in the chain; {@link #list} is last-in first.
     */
    public static final class RangeDefers extends Defer {
        public Defer list;

        public RangeDefers(Defer next) {
            super(null, null, next);
        }
    }

    /** Top of the defer chain; a function's own records are those above its entry mark. */
    public Defer defers;
    /**
     * The panic whose deferred calls are running, or null — also while a Goexit's deferred
     * calls run, which is what makes {@code recover()} return nil during a Goexit (GG-0070).
     */
    public GoPanic panic;

    /** The goroutine running on the current thread. */
    public static G current(Node node) {
        GoThreadSlot slot = GoLanguage.get(node).threadSlot();
        G g = slot.g;
        if (g == null) {
            g = new G();
            slot.g = g;
        }
        return g;
    }
}
