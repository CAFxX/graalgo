package graalgo.go.intrinsics;

import com.oracle.truffle.api.CompilerDirectives.TruffleBoundary;
import com.oracle.truffle.api.nodes.Node;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;

/**
 * The x86 feature probes upstream implements in assembly: {@code internal/cpu}'s
 * {@code cpu_x86.s} ({@code cpuid}, {@code xgetbv}, {@code getGOAMD64level}) and its vendored
 * twin {@code golang.org/x/sys/cpu} ({@code cpu_gc_x86.s}); registered in
 * {@code internal/toolexec/intrinsics/registry.txt}.
 *
 * <p>A JVM cannot execute {@code CPUID}; the answers are a consistent model of what graalgo
 * supports (docs/arch/limitations.md): <b>no optional feature</b> — every feature leaf
 * (1, 7, 0x80000001) and {@code XGETBV} read zero, so a package that probes them takes its
 * portable path, exactly as gc does under {@code GODEBUG=cpu.all=off}. The identification leaves
 * are the host's: the vendor (leaf 0) and the processor brand string (leaves
 * 0x80000002..4, which {@code cpu.Name} and {@code testing}'s {@code cpu:} header read) come from
 * {@code /proc/cpuinfo}, the kernel's copy of the same {@code CPUID} answers.
 */
final class CpuIntrinsics {
    private CpuIntrinsics() {}

    /** Leaf 0's EBX, EDX, ECX (the 12-byte vendor id) and 0x80000002..4's 48 brand bytes. */
    private static final class Host {
        static final byte[] VENDOR = pad(field("vendor_id"), 12);
        static final byte[] BRAND = pad(field("model name"), 48);

        private static String field(String key) {
            try {
                for (String line : Files.readAllLines(Path.of("/proc/cpuinfo"), StandardCharsets.UTF_8)) {
                    int colon = line.indexOf(':');
                    if (colon > 0 && line.substring(0, colon).trim().equals(key)) {
                        return line.substring(colon + 1).trim();
                    }
                }
            } catch (IOException | RuntimeException e) {
                // No /proc: an empty identification, like a CPU without the leaves.
            }
            return "";
        }

        private static byte[] pad(String s, int n) {
            byte[] out = new byte[n];
            byte[] b = s.getBytes(StandardCharsets.US_ASCII);
            System.arraycopy(b, 0, out, 0, Math.min(b.length, n));
            return out;
        }
    }

    private static long word(byte[] b, int off) {
        return (b[off] & 0xFFL) | (b[off + 1] & 0xFFL) << 8 | (b[off + 2] & 0xFFL) << 16 | (b[off + 3] & 0xFFL) << 24;
    }

    /** {@code CPUID(eaxArg, ecxArg)} on the model: {eax, ebx, ecx, edx}. */
    @TruffleBoundary
    private static Object cpuid(long eaxArg) {
        long eax = 0, ebx = 0, ecx = 0, edx = 0;
        switch ((int) eaxArg) {
            case 0:
                eax = 7; // highest basic leaf (the feature leaves exist and read zero)
                ebx = word(Host.VENDOR, 0);
                edx = word(Host.VENDOR, 4);
                ecx = word(Host.VENDOR, 8);
                break;
            case 0x80000000:
                eax = 0x80000004L; // the brand string leaves exist
                break;
            case 0x80000002:
            case 0x80000003:
            case 0x80000004: {
                int off = ((int) eaxArg - 0x80000002) * 16;
                eax = word(Host.BRAND, off);
                ebx = word(Host.BRAND, off + 4);
                ecx = word(Host.BRAND, off + 8);
                edx = word(Host.BRAND, off + 12);
                break;
            }
            default:
                break; // every feature bit clear
        }
        return new Object[] {eax, ebx, ecx, edx};
    }

    /** {@code internal/cpu.cpuid(eaxArg, ecxArg uint32) (eax, ebx, ecx, edx uint32)}. */
    static Object internal_cpu_cpuid(Node node, long eaxArg, long ecxArg) {
        return cpuid(eaxArg);
    }

    /** {@code internal/cpu.xgetbv() (eax, edx uint32)}: no OS-enabled extended state. */
    static Object internal_cpu_xgetbv(Node node) {
        return new Object[] {0L, 0L};
    }

    /** {@code internal/cpu.getGOAMD64level() int32}: the baseline graalgo compiles for (v1). */
    static long internal_cpu_getGOAMD64level(Node node) {
        return 1;
    }

    /** {@code golang.org/x/sys/cpu.cpuid} (vendored in std): as {@code internal/cpu}'s. */
    static Object vendor_golang_org_x_sys_cpu_cpuid(Node node, long eaxArg, long ecxArg) {
        return cpuid(eaxArg);
    }

    /** {@code golang.org/x/sys/cpu.xgetbv} (vendored in std). */
    static Object vendor_golang_org_x_sys_cpu_xgetbv(Node node) {
        return new Object[] {0L, 0L};
    }
}
