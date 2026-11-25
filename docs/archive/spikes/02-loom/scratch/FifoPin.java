package work;
import loomsim.*;
import java.io.FileInputStream;
public class FifoPin {
    public static void main(String[] a) throws Exception {
        Safepoint.mode = Safepoint.Mode.OFF;
        Go.go(() -> {
            try {
                FileInputStream fis = new FileInputStream("/tmp/pinfifo");
                byte[] buf = new byte[1];
                fis.read(buf); // blocks in native read -> must pin carrier
            } catch (Exception e) { System.out.println("reader done: " + e); }
        });
        Thread.sleep(400);
        System.out.println("control complete");
        System.exit(0);
    }
}
