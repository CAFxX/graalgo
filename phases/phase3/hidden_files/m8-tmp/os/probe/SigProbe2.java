import java.nio.channels.*;
import java.nio.ByteBuffer;
public class SigProbe2 {
    public static void main(String[] a) throws Exception {
        Pipe p = Pipe.open();
        p.source().close();
        // restore default SIGPIPE disposition via JDK signal API
        Class<?> sigCls = Class.forName("sun.misc.Signal");
        Class<?> handlerCls = Class.forName("sun.misc.SignalHandler");
        Object sigPipe = sigCls.getConstructor(String.class).newInstance("PIPE");
        Object sigDfl = handlerCls.getField("SIG_DFL").get(null);
        sigCls.getMethod("handle", sigCls, handlerCls).invoke(null, sigPipe, sigDfl);
        System.out.println("DFL installed; writing to broken pipe...");
        System.out.flush();
        try {
            p.sink().write(ByteBuffer.wrap(new byte[]{1}));
            System.out.println("SURVIVED write #2 (unexpected)");
        } catch (Exception e) {
            System.out.println("got exception instead of death: " + e);
        }
    }
}
