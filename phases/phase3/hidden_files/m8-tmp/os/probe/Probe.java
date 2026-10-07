import java.nio.channels.*;
import java.nio.file.*;
import java.nio.file.attribute.*;
import java.util.*;
public class Probe {
    public static void main(String[] a) throws Exception {
        // 1. broken pipe write -> exception type + message
        Pipe p = Pipe.open();
        p.source().close();
        try {
            p.sink().write(java.nio.ByteBuffer.wrap(new byte[]{1,2,3}));
            System.out.println("write returned (no exception)");
        } catch (Exception e) {
            System.out.println("EX: " + e.getClass().getName() + " msg=[" + e.getMessage() + "]");
        }
        // 2. unix: attribute view
        Path tmp = Files.createTempFile("probe", ".txt");
        try {
            Object mode = Files.getAttribute(tmp, "unix:mode", LinkOption.NOFOLLOW_LINKS);
            Object ino = Files.getAttribute(tmp, "unix:ino", LinkOption.NOFOLLOW_LINKS);
            System.out.println("mode=" + mode + " (" + mode.getClass() + ") ino=" + ino);
            BasicFileAttributes ba = Files.readAttributes(tmp, BasicFileAttributes.class, LinkOption.NOFOLLOW_LINKS);
            System.out.println("size=" + ba.size());
        } catch (Exception e) {
            System.out.println("ATTR EX: " + e);
        }
        Files.deleteIfExists(tmp);
        // 3. sun.misc.Signal availability
        try {
            Class<?> sigCls = Class.forName("sun.misc.Signal");
            System.out.println("sun.misc.Signal present: " + sigCls);
        } catch (Throwable t) { System.out.println("no sun.misc.Signal: " + t); }
        // 4. UnixSystem for uids
        try {
            Class<?> uc = Class.forName("com.sun.security.auth.module.UnixSystem");
            Object u = uc.getDeclaredConstructor().newInstance();
            System.out.println("uid=" + uc.getMethod("getUid").invoke(u));
        } catch (Throwable t) { System.out.println("no UnixSystem: " + t); }
        // 5. ENAMETOOLONG behavior
        StringBuilder sb = new StringBuilder();
        for (int i = 0; i < 300; i++) sb.append('x');
        try {
            Files.createFile(Paths.get(System.getProperty("user.dir"), sb.toString()));
            System.out.println("long name created?!");
        } catch (Exception e) {
            System.out.println("LONGNAME EX: " + e.getClass().getName() + " msg=[" + e.getMessage() + "]");
            if (e instanceof FileSystemException fse) System.out.println("  reason=[" + fse.getReason() + "]");
        }
    }
}
