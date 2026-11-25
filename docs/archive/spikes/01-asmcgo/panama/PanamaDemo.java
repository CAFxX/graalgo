import java.lang.foreign.*;
import java.lang.invoke.MethodHandle;
import java.lang.invoke.MethodHandles;
import java.nio.file.Path;

/**
 * Panama (JEP 454, final since JDK 22) equivalent of the cgo demo.
 *
 * Covers the three FFI directions a cgo migration must support:
 *  1. downcall into libc (strlen)            -- like C.strlen via cgo
 *  2. downcall into a custom shared library  -- like C.c_checksum via cgo
 *  3. upcall: C calling back into Java       -- like passing a Go func to C
 */
public class PanamaDemo {
    // Interface matching the C `byte_fn` callback signature, for upcalls.
    interface ByteFn {
        long apply(byte b, MemorySegment ctx);
    }

    public static void main(String[] args) throws Throwable {
        String msg = "graalgo-panama-demo";
        Arena arena = Arena.ofConfined();

        Linker linker = Linker.nativeLinker();

        // 1. downcall: libc strlen
        MemorySegment cstr = arena.allocateFrom(msg);
        MethodHandle strlen = linker.downcallHandle(
                linker.defaultLookup().findOrThrow("strlen"),
                FunctionDescriptor.of(ValueLayout.JAVA_LONG, ValueLayout.ADDRESS));
        long len = (long) strlen.invokeExact(cstr);
        System.out.println("panama: strlen(\"" + msg + "\") = " + len);

        // 2. downcall: custom libchecksum.so
        SymbolLookup lib = SymbolLookup.libraryLookup(
                Path.of("libchecksum.so"), arena);
        MethodHandle checksum = linker.downcallHandle(
                lib.findOrThrow("c_checksum"),
                FunctionDescriptor.of(ValueLayout.JAVA_LONG,
                        ValueLayout.ADDRESS, ValueLayout.JAVA_LONG));
        long h = (long) checksum.invokeExact(cstr, (long) msg.length());
        System.out.println("panama: checksum(\"" + msg + "\") = " + Long.toUnsignedString(h));

        // 3. upcall: C c_fold calls back into Java for every byte.
        //    The Java callback returns b*b; C sums the results.
        ByteFn squarer = (b, ctx) -> (long) b * (long) b;
        MemorySegment cb = Linker.nativeLinker().upcallStub(
                MethodHandles.lookup().findVirtual(
                        ByteFn.class, "apply",
                        java.lang.invoke.MethodType.methodType(
                                long.class, byte.class, MemorySegment.class))
                        .bindTo(squarer),
                FunctionDescriptor.of(ValueLayout.JAVA_LONG,
                        ValueLayout.JAVA_BYTE, ValueLayout.ADDRESS),
                arena);
        MethodHandle fold = linker.downcallHandle(
                lib.findOrThrow("c_fold"),
                FunctionDescriptor.of(ValueLayout.JAVA_LONG,
                        ValueLayout.ADDRESS, ValueLayout.JAVA_LONG,
                        ValueLayout.ADDRESS, ValueLayout.ADDRESS));
        long sumSq = (long) fold.invokeExact(
                cstr, (long) msg.length(), cb, MemorySegment.NULL);
        System.out.println("panama: sum of byte squares = " + sumSq);

        // cross-check the checksum against a pure-Java computation
        long expect = 5381;
        for (byte b : msg.getBytes(java.nio.charset.StandardCharsets.UTF_8)) {
            expect = expect * 33 + Byte.toUnsignedLong(b);
        }
        System.out.println("panama: java recomputation matches C: " + (expect == h));
        long expectSq = 0;
        for (byte b : msg.getBytes(java.nio.charset.StandardCharsets.UTF_8)) {
            expectSq += (long) b * (long) b;
        }
        System.out.println("panama: java recomputation matches upcall sum: " + (expectSq == sumSq));
    }
}
