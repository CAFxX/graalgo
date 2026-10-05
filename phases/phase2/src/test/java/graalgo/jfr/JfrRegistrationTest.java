package graalgo.jfr;

import jdk.jfr.FlightRecorder;

/**
 * P2-M7 (GG-0057): {@link JfrEvents#ensureRegistered()} must actually
 * initialize the event classes, not just load them.
 *
 * <p>Background: class literals do NOT trigger {@code <clinit>} (JLS 12.4.1),
 * so the old {@code ensureRegistered()} (class literals only) never ran the
 * event classes' static initializers. The one-time
 * {@code FlightRecorder.register} — which parks while pinned — then fired
 * lazily on the first goroutine carrier, emitting
 * {@code jdk.VirtualThreadPinned} on the conformance corpus (13 events).
 * The fix forces {@code <clinit>} via each class's own {@code enabled()}.
 *
 * <p>ORDERING: this test must run before any scheduler activity (it is
 * first in {@code SchedTestRunner}): once any goroutine emits a JFR event,
 * the classes are initialized regardless and the assertion is vacuous.
 */
public final class JfrRegistrationTest {

    public void testEnsureRegisteredInitializesEventClasses() {
        JfrEvents.ensureRegistered();
        var types = FlightRecorder.getFlightRecorder().getEventTypes();
        String[] expected = {
            "graalgo.GoroutineStart",
            "graalgo.GoroutineEnd",
            "graalgo.GoroutinePark",
            "graalgo.ChannelBlock",
        };
        for (String name : expected) {
            boolean found = types.stream().anyMatch(t -> t.getName().equals(name));
            if (!found) {
                throw new AssertionError(
                        "event type not registered after ensureRegistered(): " + name
                        + " (ensureRegistered() did not initialize the event class)");
            }
        }
    }
}
