import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
public class Run {
    public static void main(String[] a) throws Exception {
        Class<?> tc = Class.forName("graalgo.p3os.GoOsErrnoTest");
        Object inst = tc.getDeclaredConstructor().newInstance();
        int run = 0, failed = 0;
        for (Method m : tc.getDeclaredMethods()) {
            if (!m.getName().startsWith("test") || m.getParameterCount() != 0
                    || !Modifier.isPublic(m.getModifiers())) continue;
            run++;
            try {
                m.invoke(inst);
                System.out.println("PASS " + m.getName());
            } catch (Exception e) {
                failed++;
                Throwable c = e.getCause() != null ? e.getCause() : e;
                System.out.println("FAIL " + m.getName() + ": " + c);
            }
        }
        System.out.println(run + " tests, " + failed + " failures");
        if (failed > 0) System.exit(1);
    }
}
