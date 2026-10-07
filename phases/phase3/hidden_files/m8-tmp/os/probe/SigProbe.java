public class SigProbe {
    public static void main(String[] a) throws Exception {
        Class<?> sigCls = Class.forName("sun.misc.Signal");
        Class<?> handlerCls = Class.forName("sun.misc.SignalHandler");
        Object sigPipe = sigCls.getConstructor(String.class).newInstance("PIPE");
        Object sigDfl = handlerCls.getField("SIG_DFL").get(null);
        sigCls.getMethod("handle", sigCls, handlerCls).invoke(null, sigPipe, sigDfl);
        System.out.println("handler set to DFL, raising...");
        System.out.flush();
        sigCls.getMethod("raise", sigCls).invoke(null, sigPipe);
        System.out.println("SURVIVED THE RAISE (unexpected)");
    }
}
