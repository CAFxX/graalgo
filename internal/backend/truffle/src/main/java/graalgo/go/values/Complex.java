package graalgo.go.values;

import com.oracle.truffle.api.CompilerDirectives.ValueType;

/** An immutable complex64/complex128 value (complex64 parts are float-rounded doubles). */
@ValueType
public record Complex(double re, double im) {
    public static final Complex ZERO = new Complex(0, 0);

    @Override
    public boolean equals(Object o) {
        return o instanceof Complex c && re == c.re && im == c.im;
    }

    @Override
    public int hashCode() {
        return Double.hashCode(re == 0 ? 0 : re) * 31 + Double.hashCode(im == 0 ? 0 : im);
    }
}
