package graalgo.frontend;

import java.util.ServiceLoader;

/**
 * Guards the launcher's ServiceLoader classpath: the Phase-3 wire-kind
 * builders live in the interop module and reach {@code graalgo.launcher.Main}
 * only through ServiceLoader discovery. If interop/out/classes ever drops
 * off a launcher classpath again, every program emitting a Phase-3 wire
 * kind (e.g. real fmt emitting {@code ifacemethod}) dies with
 * "unsupported expression kind" — this test fails first, naming the
 * mechanism instead of the symptom.
 *
 * <p>RED: on a classpath without interop, no provider is discovered.
 * GREEN: the interop P3KindRegistrations provider is discovered.
 *
 * <p>Deliberately references the provider by class name, not by type:
 * the test compile classpath has no interop classes (same as the launcher
 * module, which must not depend on interop at compile time).
 */
public final class WireKindRegistrarDiscoveryTest {

    public void testInteropRegistrarDiscovered() {
        boolean found = false;
        for (WireKindRegistrar registrar : ServiceLoader.load(WireKindRegistrar.class)) {
            if (registrar.getClass().getName()
                    .equals("graalgo.p3sync.launcher.P3KindRegistrations")) {
                found = true;
            }
        }
        if (!found) {
            throw new AssertionError("no WireKindRegistrar provider from the interop module "
                    + "discovered via ServiceLoader: is interop/out/classes on the "
                    + "launcher/test classpath?");
        }
    }
}
