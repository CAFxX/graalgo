package graalgo.p3link;

import graalgo.frontend.GoFieldCell;
import graalgo.values.GoKind;
import graalgo.values.GoPointer;
import graalgo.values.GoStruct;
import graalgo.values.GoStructDescriptor;
import java.util.List;

/**
 * PKG-012 F1 regression tests: {@code sync.Cond} notifyList state isolation.
 *
 * <p>The {@code *notifyList} behind {@code sync.Cond} arrives at
 * {@link SyncRuntime} as a {@link GoPointer} (almost always a cell pointer
 * from {@code &c.notify}). The state table must key by <em>storage
 * identity</em>, never by structural equality: {@link GoStruct#equals} is
 * structural, so two distinct {@code sync.Cond} values with equal contents —
 * the canonical {@code sync.NewCond(&mu)} sharing one {@code Mutex} — must
 * still get distinct {@code NotifyState}s. Keying structurally (the F1 bug)
 * merged their ticket sequences, so a {@code Signal}/{@code Broadcast} on
 * one cond consumed the other's tickets and woke the other's waiters:
 * silent deterministic corruption.
 *
 * <p>Each test builds two zero-valued {@code sync.Cond}-shaped structs with
 * equal contents (the F1 trigger) and drives the public ticket-lock entry
 * points directly. {@link SyncRuntime#clear()} resets the state table per
 * test. The {@code Wait} parking path needs an installed scheduler and is
 * therefore covered by the {@code graalgo run} end-to-end check, not here;
 * {@code Add}/{@code NotifyOne}/{@code NotifyAll} fully determine the
 * keying behavior under test.
 *
 * <p>Negative control: against the pre-fix (structural-key) classes,
 * {@link #testFieldCellKeyIsolation},
 * {@link #testNotifyAllIsolation}, and
 * {@link #testReferentKeyIsolation} fail — the second cond's first ticket
 * is 1, not 0, and cross-cond notifications consume the wrong tickets.
 * {@link #testAddrEvaluationStability} passes both before and after: it is
 * the positive control proving the fix did not over-correct to per-call
 * identity (two evaluations of {@code &c.notify} must still address the
 * same notifyList, as gc requires).
 *
 * <p>uint32 wraparound is deliberately not unit-tested: the counters are
 * Java {@code int}s (mod-2^32 by definition) and the wrap-safe comparison
 * delegates to {@link Integer#compareUnsigned} (JDK-verified); there is no
 * project logic at the boundary to falsify.
 */
public final class CondIsolationTest {

    /** Field index of {@code notify} in the cond-shaped descriptor below. */
    private static final int NOTIFY_FIELD = 1;

    private static GoStructDescriptor condDesc() {
        return GoStructDescriptor.layout("sync.Cond",
                List.of("L", "notify"),
                List.of(GoKind.POINTER, GoKind.POINTER));
    }

    /** Two distinct but equal-content cond values sharing one descriptor. */
    private static GoStruct[] newCondPair() {
        GoStructDescriptor d = condDesc();
        GoStruct c1 = new GoStruct(d, new long[0], new Object[2]);
        GoStruct c2 = new GoStruct(d, new long[0], new Object[2]);
        if (!c1.equals(c2) || c1 == c2) {
            throw new AssertionError(
                    "precondition broken: cond pair must be distinct but structurally equal");
        }
        return new GoStruct[]{c1, c2};
    }

    /** What the frontend produces for {@code &c.notify}: a fresh cell per evaluation. */
    private static GoPointer notifyAddr(GoStruct cond) {
        return new GoPointer(new GoFieldCell(cond, NOTIFY_FIELD, GoKind.POINTER));
    }

    private static void assertEq(int expected, int actual, String what) {
        if (expected != actual) {
            throw new AssertionError(
                    what + ": expected ticket " + expected + " but got " + actual);
        }
    }

    public void testFieldCellKeyIsolation() {
        SyncRuntime.clear();
        GoStruct[] pair = newCondPair();
        GoPointer p1 = notifyAddr(pair[0]);
        GoPointer p2 = notifyAddr(pair[1]);
        // Independent ticket sequences: each cond's first ticket is 0.
        // Pre-fix (structural key): Add(p2) returns 1 — shared state.
        assertEq(0, SyncRuntime.notifyListAdd(p1), "c1 first ticket");
        assertEq(0, SyncRuntime.notifyListAdd(p2), "c2 first ticket (independent sequence)");
        assertEq(1, SyncRuntime.notifyListAdd(p1), "c1 second ticket");
        assertEq(1, SyncRuntime.notifyListAdd(p2), "c2 second ticket");
    }

    public void testNotifyAllIsolation() {
        SyncRuntime.clear();
        GoStruct[] pair = newCondPair();
        GoPointer p1 = notifyAddr(pair[0]);
        GoPointer p2 = notifyAddr(pair[1]);
        assertEq(0, SyncRuntime.notifyListAdd(p1), "c1 ticket");
        assertEq(0, SyncRuntime.notifyListAdd(p2), "c2 ticket");
        // Broadcast on c2 must not consume c1's ticket: c1's next ticket
        // is still 1. Pre-fix: NotifyAll(p2) sets shared notify=wait=2, so
        // Add(p1) returns 2.
        SyncRuntime.notifyListNotifyAll(p2);
        assertEq(1, SyncRuntime.notifyListAdd(p1), "c1 ticket after c2 Broadcast");
    }

    public void testNotifyOneIsolation() {
        SyncRuntime.clear();
        GoStruct[] pair = newCondPair();
        GoPointer p1 = notifyAddr(pair[0]);
        GoPointer p2 = notifyAddr(pair[1]);
        assertEq(0, SyncRuntime.notifyListAdd(p1), "c1 ticket");
        assertEq(0, SyncRuntime.notifyListAdd(p2), "c2 ticket");
        // Signal on c2 consumes c2's own ticket 0 — c1's sequence must be
        // unaffected. Pre-fix: NotifyOne(p2) consumes from the shared
        // state, so Add(p1) returns 2 instead of 1 and Add(p2) returns 3
        // instead of 1.
        SyncRuntime.notifyListNotifyOne(p2);
        assertEq(1, SyncRuntime.notifyListAdd(p1), "c1 ticket after c2 Signal");
        assertEq(1, SyncRuntime.notifyListAdd(p2), "c2 ticket after own Signal");
    }

    public void testReferentKeyIsolation() {
        SyncRuntime.clear();
        // The referent branch of notifyKey (e.g. *notifyList from
        // new(notifyList)): equal-content structs must still key apart.
        GoStruct[] pair = newCondPair();
        GoPointer p1 = new GoPointer((Object) pair[0]);
        GoPointer p2 = new GoPointer((Object) pair[1]);
        assertEq(0, SyncRuntime.notifyListAdd(p1), "first referent ticket");
        assertEq(0, SyncRuntime.notifyListAdd(p2), "second referent ticket (independent)");
    }

    public void testAddrEvaluationStability() {
        SyncRuntime.clear();
        // Positive control: two evaluations of &c.notify on the SAME cond
        // (fresh GoFieldCell each time, as FieldAddrNode mints) must
        // address the same NotifyState — this is why the key is
        // (ownerStruct, fieldIndex) and not the cell object.
        GoStruct[] pair = newCondPair();
        GoPointer first = notifyAddr(pair[0]);
        GoPointer second = notifyAddr(pair[0]);
        if (first.cell() == second.cell()) {
            throw new AssertionError("precondition broken: expected fresh cells per & evaluation");
        }
        assertEq(0, SyncRuntime.notifyListAdd(first), "first &c.notify evaluation");
        assertEq(1, SyncRuntime.notifyListAdd(second), "second &c.notify evaluation (same state)");
    }

    public void testNotifyListCheck() {
        // 32 = wait+notify (2xuint32) + lock (uintptr) + head/tail
        // (2xunsafe.Pointer) on amd64, the frontend-folded size.
        SyncRuntime.notifyListCheck(32);
        try {
            SyncRuntime.notifyListCheck(40);
            throw new AssertionError("notifyListCheck(40) must fail fast");
        } catch (graalgo.frontend.FrontendException expected) {
            // Expected: struct-skew fail-fast, exactly like gc.
        }
    }
}
