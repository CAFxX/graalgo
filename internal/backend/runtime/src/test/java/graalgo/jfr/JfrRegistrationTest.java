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

    /**
     * Reviewer1 P2-2: every {@code Event} subclass in {@code graalgo.jfr}
     * must be registered by {@link JfrEvents#ensureRegistered()}. A new
     * event class added without updating the list would silently
     * reintroduce lazy-init pins on carriers.
     *
     * <p>Lists event classes reflectively from the package directory
     * (test classpath mirrors the package layout).
     */
    public void testAllEventSubclassesRegistered() throws Exception {
        JfrEvents.ensureRegistered();
        var types = FlightRecorder.getFlightRecorder().getEventTypes();
        // Event classes live in out/classes (main); locate via the class
        // resource so the test works regardless of working directory.
        java.nio.file.Path pkgDir;
        String res = "/graalgo/jfr/JfrEvents.class";
        java.net.URL url = JfrEvents.class.getResource(res);
        if (url == null || !"file".equals(url.getProtocol())) {
            throw new AssertionError("cannot locate graalgo/jfr package dir");
        }
        try {
            pkgDir = java.nio.file.Paths.get(url.toURI()).getParent();
        } catch (java.net.URISyntaxException e) {
            throw new AssertionError(e);
        }
        int checked = 0;
        try (var stream = java.nio.file.Files.list(pkgDir)) {
            for (java.nio.file.Path p : (Iterable<java.nio.file.Path>) stream::iterator) {
                String name = p.getFileName().toString();
                if (!name.endsWith("Event.class") || name.contains("$")) {
                    continue;
                }
                String className =
                        "graalgo.jfr." + name.substring(0, name.length() - 6);
                Class<?> c = Class.forName(className);
                if (!jdk.jfr.Event.class.isAssignableFrom(c)) {
                    continue;
                }
                checked++;
                jdk.jfr.Name ann = c.getAnnotation(jdk.jfr.Name.class);
                String eventName = ann == null ? className : ann.value();
                boolean found = types.stream()
                        .anyMatch(t -> t.getName().equals(eventName));
                if (!found) {
                    throw new AssertionError(
                            "Event subclass not registered by ensureRegistered(): "
                            + className + " (add it to JfrEvents.ensureRegistered())");
                }
            }
        }
        if (checked == 0) {
            throw new AssertionError("no Event subclasses found; test is vacuous");
        }
    }
}
