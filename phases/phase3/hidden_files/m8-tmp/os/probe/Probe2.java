import java.nio.file.*;
public class Probe2 {
    public static void main(String[] a) throws Exception {
        Class<?> uc = Class.forName("com.sun.security.auth.module.UnixSystem");
        Object u = uc.getDeclaredConstructor().newInstance();
        for (var m : uc.getMethods()) {
            if (m.getName().startsWith("get")) System.out.println(m);
        }
        // FileStore equality for EXDEV check
        var fs1 = Files.getFileStore(Paths.get("/home/hatch"));
        var fs2 = Files.getFileStore(Paths.get("/home/hatch/workspace"));
        System.out.println("same store equals: " + fs1.equals(fs2));
    }
}
