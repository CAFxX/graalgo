import java.nio.file.*;
import java.nio.file.attribute.BasicFileAttributes; import java.nio.channels.*; import java.util.*;
public class Probe4 {
    static void show(String what, Runnable r) {
        try { r.run(); System.out.println(what + ": OK"); }
        catch (Throwable t) {
            String extra = "";
            if (t instanceof FileSystemException fse) extra = " reason=" + fse.getReason();
            System.out.println(what + ": " + t.getClass().getSimpleName() + " msg=" + t.getMessage() + extra);
        }
    }
    public static void main(String[] a) throws Exception {
        Path f = Files.createTempFile("probe4", ".txt");
        String fp = f.toString();
        show("open file/x", () -> { try { FileChannel.open(Paths.get(fp + "/x"), EnumSet.of(StandardOpenOption.READ)); } catch (java.io.IOException e) { throw new RuntimeException(e); } });
        show("mkdir file/x", () -> { try { Files.createDirectory(Paths.get(fp + "/x")); } catch (java.io.IOException e) { throw new RuntimeException(e); } });
        show("stat file/x", () -> { try { Files.readAttributes(Paths.get(fp + "/x"), BasicFileAttributes.class); } catch (java.io.IOException e) { throw new RuntimeException(e); } });
        show("delete missing", () -> { try { Files.delete(Paths.get(fp + "-nope")); } catch (java.io.IOException e) { throw new RuntimeException(e); } });
        Files.delete(f);
    }
}
