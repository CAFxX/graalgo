package work;
import loomsim.*;
import java.lang.foreign.*;
import java.lang.invoke.MethodHandle;
public class FfmPin {
    public static void main(String[] a) throws Exception {
        Safepoint.mode = Safepoint.Mode.OFF;
        Linker linker = Linker.nativeLinker();
        SymbolLookup libc = linker.defaultLookup();
        MethodHandle sleep = linker.downcallHandle(
            libc.find("sleep").orElseThrow(),
            FunctionDescriptor.of(ValueLayout.JAVA_INT, ValueLayout.JAVA_INT));
        Go.go(() -> {
            try {
                System.out.println("vthread entering native sleep(2)");
                int r = (int) sleep.invokeExact(2);
                System.out.println("vthread woke, r=" + r);
            } catch (Throwable t) { System.out.println("t=" + t); }
        });
        Thread.sleep(3000);
        System.out.println("control complete");
        System.exit(0);
    }
}
