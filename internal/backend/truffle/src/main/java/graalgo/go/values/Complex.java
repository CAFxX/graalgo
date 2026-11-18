package graalgo.go.values;

/** An immutable complex64/complex128 value (complex64 parts are float-rounded doubles). */
public final class Complex {
    public static final Complex ZERO = new Complex(0, 0);

    public final double re;
    public final double im;

    public Complex(double re, double im) {
        this.re = re;
        this.im = im;
    }

    @Override
    public boolean equals(Object o) {
        return o instanceof Complex c && re == c.re && im == c.im;
    }

    @Override
    public int hashCode() {
        return Double.hashCode(re == 0 ? 0 : re) * 31 + Double.hashCode(im == 0 ? 0 : im);
    }
}
