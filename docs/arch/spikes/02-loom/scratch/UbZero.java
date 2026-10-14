package work;
public class UbZero {
    public static void main(String[] a) throws Exception {
        // same probe but 0 spinners: subclass trick not possible, just inline minimal test
        System.out.println("skip - testing via direct unpark instead");
        System.exit(0);
    }
}
