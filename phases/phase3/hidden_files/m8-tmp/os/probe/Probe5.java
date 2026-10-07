import java.nio.file.*; import java.nio.file.attribute.*;
public class Probe5 {
    static void show(String what, Thrower r) {
        try { r.run(); System.out.println(what + ": OK"); }
        catch (Throwable t) {
            String extra = "";
            if (t instanceof FileSystemException fse) extra = " reason=" + fse.getReason();
            System.out.println(what + ": " + t.getClass().getSimpleName() + extra);
        }
    }
    interface Thrower { void run() throws Throwable; }
    public static void main(String[] a) throws Exception {
        Path f = Files.createTempFile("probe5", ".txt");
        String fp = f.toString();
        show("delete file/x", () -> Files.delete(Paths.get(fp + "/x")));
        show("move file/x -> y", () -> Files.move(Paths.get(fp + "/x"), Paths.get(fp + "-y")));
        show("symlink new=file/x", () -> Files.createSymbolicLink(Paths.get(fp + "/x"), Paths.get("t")));
        show("readlink file/x", () -> Files.readSymbolicLink(Paths.get(fp + "/x")));
        show("getattr file/x", () -> Files.getAttribute(Paths.get(fp + "/x"), "unix:mode", LinkOption.NOFOLLOW_LINKS));
        show("setperms file/x", () -> Files.setPosixFilePermissions(Paths.get(fp + "/x"), java.util.Set.of()));
        show("newDirStream file/x", () -> Files.newDirectoryStream(Paths.get(fp + "/x")).close());
        Files.delete(f);
    }
}
