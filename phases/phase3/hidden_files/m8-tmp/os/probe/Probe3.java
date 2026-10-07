import java.nio.file.*;
public class Probe3 {
    public static void main(String[] a) throws Exception {
        Path f = Files.createTempFile("probe3", ".txt");
        try {
            Files.readSymbolicLink(f);
        } catch (Throwable t) {
            System.out.println(t.getClass().getName() + " | msg=" + t.getMessage());
            if (t instanceof FileSystemException fse)
                System.out.println("reason=" + fse.getReason());
        }
        // Also: readlink on a directory
        try {
            Files.readSymbolicLink(Paths.get("/tmp"));
        } catch (Throwable t) {
            System.out.println("dir: " + t.getClass().getName() + " | msg=" + t.getMessage());
        }
        Files.delete(f);
    }
}
