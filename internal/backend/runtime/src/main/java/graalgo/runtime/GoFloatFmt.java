package graalgo.runtime;

/**
 * Go-exact float/decimal formatting (WS G).
 *
 * <p>Ports {@code internal/strconv/ftoa.go}'s {@code fmtEFG} (the '%e'/'%f'
 * layout, including the '%g'→'%e'/'%f' decision) verbatim. Digit production:
 * <ul>
 *   <li>precision {@code < 0} (shortest): digits come from
 *       {@link Double#toString}/{@link Float#toString}, which — like Go's
 *       Ryu — produce the unique shortest round-trip representation, so the
 *       digit sequences agree with gc's on every input;</li>
 *   <li>precision {@code >= 0}: digits come from the exact binary expansion
 *       ({@link java.math.BigDecimal#BigDecimal(double)}, which is exact;
 *       float32 values are widened first, also exact), rounded half-to-even
 *       — matching Go's {@code decimal.shouldRoundUp} on its exact
 *       expansion (exact halves round to even; the expansion never
 *       truncates, so the {@code trunc} arm never fires).</li>
 * </ul>
 *
 * <p>Special values: NaN → {@code "NaN"}, +Inf → {@code "+Inf"},
 * -Inf → {@code "-Inf"} (sign prefix applied by callers for the
 * {@code '+'} / {@code ' '} flags is handled in {@link Fmt}).
 */
public final class GoFloatFmt {
    private GoFloatFmt() {}

    /**
     * Formats {@code v} (a float32 value carried as float, or float64 as
     * double) with Go's {@code strconv.FormatFloat} semantics:
     * {@code fmt} ∈ {'e','E','f','g','G'}, {@code prec} significant digits
     * ({@code -1} = shortest), {@code bitSize} ∈ {32, 64}.
     */
    public static String formatFloat(double v, char fmt, int prec, int bitSize) {
        boolean neg = isNegative(v, bitSize);
        double av = neg ? -v : v; // careful: -(-0.0) is +0.0; handled below
        if (Double.isNaN(av)) {
            return "NaN";
        }
        if (Double.isInfinite(av)) {
            return neg ? "-Inf" : "+Inf";
        }
        if (av == 0.0) {
            // Preserve the sign of -0.0 through the digit path.
            return fmtEFG(neg, new byte[0], 0, 0, prec, fmt, true);
        }
        DecParts d;
        boolean shortest = prec < 0;
        int effPrec = prec;
        if (shortest) {
            // Mirror ftoa.go: the precision is rewritten from the shortest
            // digit count before fmtEFG (shortest flag stays true).
            d = shortestParts(av, bitSize);
            int nd = d.digits.length;
            switch (fmt) {
                case 'e', 'E' -> effPrec = Math.max(nd - 1, 0);
                case 'f' -> effPrec = Math.max(nd - d.dp, 0);
                case 'g', 'G' -> effPrec = nd;
                default -> {}
            }
        } else {
            // Mirror ftoa.go: %g with prec 0 formats 1 significant digit.
            if ((fmt == 'g' || fmt == 'G') && prec == 0) {
                effPrec = 1;
            }
            d = roundedParts(av, bitSize, fmt, prec);
        }
        return fmtEFG(neg, d.digits, d.dp, d.digits.length, effPrec, fmt, shortest);
    }

    /** Formats a complex value like Go's {@code AppendComplex}: {@code (re+imi)}. */
    public static String formatComplex(double re, double im, char fmt, int prec, int bitSize) {
        StringBuilder sb = new StringBuilder();
        sb.append('(');
        sb.append(formatFloat(re, fmt, prec, bitSize >> 1));
        int mark = sb.length();
        sb.append(formatFloat(im, fmt, prec, bitSize >> 1));
        if (sb.charAt(mark) != '+' && sb.charAt(mark) != '-') {
            sb.insert(mark, '+');
        }
        sb.append("i)");
        return sb.toString();
    }

    private static boolean isNegative(double v, int bitSize) {
        if (bitSize == 32) {
            return (Float.floatToRawIntBits((float) v) & 0x8000_0000) != 0;
        }
        return (Double.doubleToRawLongBits(v) & 0x8000_0000_0000_0000L) != 0;
    }

    private record DecParts(byte[] digits, int dp) {}

    /**
     * Shortest digits from {@link Double#toString}/{@link Float#toString}.
     * Both produce the unique shortest round-trip decimal, identical to
     * what Go's Ryu emits for prec=-1 — except for subnormals, where
     * Java's algorithm is not shortest-optimal (e.g. 5e-324 prints as
     * "4.9E-324"; Go prints "5e-324"). Subnormals take a dedicated
     * verified-search path below.
     */
    private static DecParts shortestParts(double av, int bitSize) {
        if (isSubnormal(av, bitSize)) {
            return shortestSubnormalParts(av, bitSize);
        }
        String s = bitSize == 32 ? Float.toString((float) av) : Double.toString(av);
        // Forms: "1.0", "0.001", "1.0E23", "1.0E-4".
        int epos = s.indexOf('E');
        int exp = 0;
        String mant = s;
        if (epos >= 0) {
            exp = Integer.parseInt(s.substring(epos + 1));
            mant = s.substring(0, epos);
        }
        int dot = mant.indexOf('.');
        String intPart = mant.substring(0, dot);
        String fracPart = mant.substring(dot + 1);
        String all = intPart + fracPart;
        int pointPos = intPart.length(); // digits before the point
        // Strip leading zeros (adjusts nothing: dp formula uses raw length).
        int lead = 0;
        while (lead < all.length() && all.charAt(lead) == '0') {
            lead++;
        }
        // Strip trailing zeros.
        int trail = all.length();
        while (trail > lead && all.charAt(trail - 1) == '0') {
            trail--;
        }
        byte[] digits;
        int nd;
        if (trail == lead) {
            digits = new byte[0];
            nd = 0;
        } else {
            digits = new byte[trail - lead];
            for (int i = 0; i < digits.length; i++) {
                digits[i] = (byte) all.charAt(lead + i);
            }
            nd = digits.length;
        }
        // Value = int(all) * 10^(pointPos - len(all) + exp); with D the
        // stripped digits, int(all) = D * 10^(len(all) - trail), so in Go
        // decimal terms (value = D * 10^(dp - nd)): dp = pointPos + exp - lead.
        int dp = pointPos + exp - lead;
        return new DecParts(digits, dp);
    }

    /**
     * True when {@code av} (positive, nonzero — callers abs and zero-check
     * first) is subnormal at the given bit size.
     */
    private static boolean isSubnormal(double av, int bitSize) {
        if (bitSize == 32) {
            int bits = Float.floatToRawIntBits((float) av);
            return (bits & 0x7F800000) == 0 && (bits & 0x007FFFFF) != 0;
        }
        long bits = Double.doubleToRawLongBits(av);
        return (bits & 0x7FF0000000000000L) == 0 && (bits & 0x000FFFFFFFFFFFFFL) != 0;
    }

    /**
     * Shortest round-trip digits for a positive subnormal, matching Go's
     * Ryu output exactly.
     *
     * <p>Searches increasing significant-digit counts k = 1, 2, ...: a
     * k-digit decimal parses back to {@code av} iff it lies in the
     * round-to-nearest interval [exact - ulp/2, exact + ulp/2], so every
     * integer mantissa in that range is tested by parsing (no wrong answer
     * is possible — only verified round-trips are returned). Among the
     * matches at the smallest k, Go's tie-break applies: closest to the
     * exact value, then the even last digit. Termination is guaranteed:
     * the exact binary expansion itself (<= 767 digits) round-trips.
     */
    private static DecParts shortestSubnormalParts(double av, int bitSize) {
        java.math.BigDecimal exact = bitSize == 32
                ? new java.math.BigDecimal((double) (float) av) // widening is exact
                : new java.math.BigDecimal(av); // exact binary expansion
        // Subnormal ulp is constant: 2^-1074 (float64), 2^-149 (float32) —
        // the exact value of the smallest positive subnormal.
        java.math.BigDecimal ulp = bitSize == 32
                ? new java.math.BigDecimal((double) Float.intBitsToFloat(1))
                : new java.math.BigDecimal(Double.longBitsToDouble(1));
        java.math.BigDecimal halfUlp = ulp.divide(java.math.BigDecimal.valueOf(2));
        java.math.BigDecimal lo = exact.subtract(halfUlp);
        java.math.BigDecimal hi = exact.add(halfUlp);
        // floor(log10(exact)): BigDecimal precision/scale give it exactly.
        int exp10 = exact.precision() - exact.scale() - 1;
        for (int k = 1; k <= 800; k++) {
            java.math.BigInteger bestM = null;
            java.math.BigDecimal bestDist = null;
            int bestDp = 0;
            // Two unit alignments: the k-digit mantissa may sit at
            // 10^(exp10-k+1) or, when exact rounds up across a power of
            // ten (e.g. 9.88e-324 -> "1e-323"), at 10^(exp10-k+2).
            for (int eShift = 0; eShift <= 1; eShift++) {
                java.math.BigDecimal unit =
                        java.math.BigDecimal.ONE.movePointRight(exp10 - k + 1 + eShift);
                java.math.BigInteger mLo =
                        lo.divide(unit, 0, java.math.RoundingMode.CEILING).toBigInteger();
                java.math.BigInteger mHi =
                        hi.divide(unit, 0, java.math.RoundingMode.FLOOR).toBigInteger();
                // k-digit mantissas live in [10^(k-1), 10^k).
                java.math.BigInteger minM = java.math.BigInteger.TEN.pow(k - 1);
                java.math.BigInteger maxM = java.math.BigInteger.TEN.pow(k)
                        .subtract(java.math.BigInteger.ONE);
                if (mLo.compareTo(minM) < 0) {
                    mLo = minM;
                }
                if (mHi.compareTo(maxM) > 0) {
                    mHi = maxM;
                }
                for (java.math.BigInteger m = mLo; m.compareTo(mHi) <= 0;
                        m = m.add(java.math.BigInteger.ONE)) {
                    String cand = new java.math.BigDecimal(m).multiply(unit).toPlainString();
                    boolean roundTrips;
                    if (bitSize == 32) {
                        roundTrips = Float.floatToRawIntBits(Float.parseFloat(cand))
                                == Float.floatToRawIntBits((float) av);
                    } else {
                        roundTrips = Double.doubleToRawLongBits(Double.parseDouble(cand))
                                == Double.doubleToRawLongBits(av);
                    }
                    if (!roundTrips) {
                        continue;
                    }
                    java.math.BigDecimal dist = new java.math.BigDecimal(m).multiply(unit)
                            .subtract(exact).abs();
                    if (bestM == null || dist.compareTo(bestDist) < 0
                            || (dist.compareTo(bestDist) == 0
                                && !m.testBit(0) && bestM.testBit(0))) {
                        bestM = m;
                        bestDist = dist;
                        // value = D * 10^(dp - nd), D = m, nd = k.
                        bestDp = exp10 + 1 + eShift;
                    }
                }
            }
            if (bestM != null) {
                String digits = bestM.toString();
                byte[] db = new byte[digits.length()];
                for (int i = 0; i < db.length; i++) {
                    db[i] = (byte) digits.charAt(i);
                }
                return new DecParts(db, bestDp);
            }
        }
        throw new IllegalStateException(
                "shortestSubnormalParts: no round-trip found (unreachable)");
    }

    /**
     * Exactly-rounded digits for prec >= 0, from the exact binary expansion.
     * Mirrors Go's {@code d.Assign(mant); d.Shift(exp-mantBits)} then the
     * per-verb {@code d.Round(...)}.
     */
    private static DecParts roundedParts(double av, int bitSize, char fmt, int prec) {
        java.math.BigDecimal exact;
        if (bitSize == 32) {
            exact = new java.math.BigDecimal((double) (float) av); // widening is exact
        } else {
            exact = new java.math.BigDecimal(av); // exact binary expansion
        }
        String plain = exact.toPlainString();
        int dot = plain.indexOf('.');
        String intPart = dot < 0 ? plain : plain.substring(0, dot);
        String fracPart = dot < 0 ? "" : plain.substring(dot + 1);
        String all = intPart + fracPart;
        int pointPos = intPart.length();
        int lead = 0;
        while (lead < all.length() && all.charAt(lead) == '0') {
            lead++;
        }
        String sig = all.substring(lead);
        int dp = pointPos - all.length() + sig.length();
        int want;
        switch (fmt) {
            case 'e', 'E' -> want = prec + 1;
            case 'f' -> want = dp + prec;
            default -> { // 'g', 'G'
                want = prec == 0 ? 1 : prec;
            }
        }
        if (want <= 0) {
            // M8 closeout (triage class E): all significant digits lie
            // beyond the requested precision (e.g. %f of 1e-300) — the
            // value rounds to zero. (MathContext requires precision > 0;
            // Go's decimal.Round produces an empty digit string here.)
            return new DecParts(new byte[0], 0);
        }
        if (want < sig.length()) {
            // Round half-to-even on the exact expansion (== Go's
            // shouldRoundUp: exact halves go to even; trunc never set).
            java.math.BigDecimal bd = new java.math.BigDecimal(
                    (sig.isEmpty() ? "0" : sig) + "E" + (dp - sig.length()));
            bd = bd.round(new java.math.MathContext(want, java.math.RoundingMode.HALF_EVEN));
            plain = bd.toPlainString();
            dot = plain.indexOf('.');
            // Handle a possible leading '-' (cannot happen: av >= 0).
            intPart = dot < 0 ? plain : plain.substring(0, dot);
            fracPart = dot < 0 ? "" : plain.substring(dot + 1);
            all = intPart + fracPart;
            pointPos = intPart.length();
            lead = 0;
            while (lead < all.length() && all.charAt(lead) == '0') {
                lead++;
            }
            sig = all.substring(lead);
            dp = pointPos - all.length() + sig.length();
        }
        // Strip trailing zeros (Go's decimal keeps nd minimal after Round).
        int trail = sig.length();
        while (trail > 0 && sig.charAt(trail - 1) == '0') {
            trail--;
        }
        sig = sig.substring(0, trail);
        byte[] digits = new byte[sig.length()];
        for (int i = 0; i < digits.length; i++) {
            digits[i] = (byte) sig.charAt(i);
        }
        return new DecParts(digits, dp);
    }

    /**
     * Verbatim port of {@code internal/strconv/ftoa.go fmtEFG}: lays out
     * {@code s} (nd digits, decimal point after {@code dp} digits) per
     * {@code fmt} and {@code prec}. {@code shortest} selects the
     * precision-6 rule for the '%g' e-vs-f decision.
     */
    static String fmtEFG(boolean neg, byte[] s, int dp, int nd, int prec, char fmt, boolean shortest) {
        if (fmt == 'g' || fmt == 'G') {
            int eprec = prec;
            if (eprec > nd && nd >= dp) {
                eprec = nd;
            }
            if (shortest) {
                eprec = 6;
            }
            int exp = dp - 1;
            if (exp < -4 || exp >= eprec) {
                if (prec > nd) {
                    prec = nd;
                }
                prec--;
                fmt = (char) (fmt + 'e' - 'g'); // 'g'->'e', 'G'->'E'
            } else {
                if (prec > dp) {
                    prec = nd;
                }
                prec = Math.max(prec - dp, 0);
                fmt = 'f';
            }
        }

        StringBuilder dst = new StringBuilder();
        switch (fmt) {
            case 'e', 'E' -> { // %e: -d.ddddde±dd
                if (neg) {
                    dst.append('-');
                }
                byte ch = '0';
                if (nd != 0) {
                    ch = s[0];
                }
                dst.append((char) ch);
                if (prec > 0) {
                    dst.append('.');
                    int i = 1;
                    int m = Math.min(nd, prec + 1);
                    for (; i < m; i++) {
                        dst.append((char) s[i]);
                    }
                    for (int k = 0; k < prec + 1 - i; k++) {
                        dst.append('0');
                    }
                }
                dst.append(fmt);
                int exp = dp - 1;
                if (nd == 0) {
                    exp = 0; // special case: 0 has exponent 0
                }
                char ech;
                if (exp < 0) {
                    ech = '-';
                    exp = -exp;
                } else {
                    ech = '+';
                }
                dst.append(ech);
                if (exp < 10) {
                    dst.append('0');
                    dst.append((char) (exp + '0'));
                } else if (exp < 100) {
                    dst.append((char) (exp / 10 + '0'));
                    dst.append((char) (exp % 10 + '0'));
                } else {
                    dst.append((char) (exp / 100 + '0'));
                    dst.append((char) (exp / 10 % 10 + '0'));
                    dst.append((char) (exp % 10 + '0'));
                }
                return dst.toString();
            }
            case 'f' -> { // %f: -ddddddd.ddddd
                if (neg) {
                    dst.append('-');
                }
                if (dp > 0) {
                    int m = Math.min(nd, dp);
                    for (int i = 0; i < m; i++) {
                        dst.append((char) s[i]);
                    }
                    for (int k = 0; k < dp - m; k++) {
                        dst.append('0');
                    }
                } else {
                    dst.append('0');
                }
                if (prec > 0) {
                    dst.append('.');
                    int lz = Math.min(prec, Math.max(0, -dp));
                    int off = dp + lz;
                    int m = Math.min(prec - lz, Math.max(0, nd - off));
                    int tz = Math.max(0, prec - lz - m);
                    for (int k = 0; k < lz; k++) {
                        dst.append('0');
                    }
                    for (int i = 0; i < m; i++) {
                        dst.append((char) s[off + i]);
                    }
                    for (int k = 0; k < tz; k++) {
                        dst.append('0');
                    }
                }
                return dst.toString();
            }
            default -> throw new IllegalArgumentException("bad fmt " + fmt);
        }
    }

    // ------------------------------------------------------------------
    // Binary ('b') and hexadecimal ('x'/'X') float formats.
    // ------------------------------------------------------------------

    /**
     * Go's {@code strconv} {@code 'b'} format: {@code [-]mantissap±exp}
     * with the exact mantissa bits (ftoa.go fmtB).
     */
    public static String formatBinary(double v, int bitSize) {
        long b = bitSize == 32 ? Float.floatToRawIntBits((float) v) & 0xFFFF_FFFFL
                : Double.doubleToRawLongBits(v);
        int expBits = bitSize == 32 ? 8 : 11;
        int mantBits = bitSize == 32 ? 23 : 52;
        int bias = bitSize == 32 ? -127 : -1023;
        boolean neg = (b >>> (expBits + mantBits)) != 0;
        int exp = (int) ((b >>> mantBits) & ((1 << expBits) - 1));
        long mant = b & ((1L << mantBits) - 1);
        if (exp == (1 << expBits) - 1) {
            return mant != 0 ? "NaN" : (neg ? "-Inf" : "+Inf");
        }
        if (exp == 0) {
            exp++;
        } else {
            mant |= 1L << mantBits;
        }
        exp += bias;
        int exp2 = exp - mantBits;
        StringBuilder sb = new StringBuilder();
        if (neg) {
            sb.append('-');
        }
        sb.append(Long.toUnsignedString(mant)).append('p');
        sb.append(exp2 < 0 ? '-' : '+');
        sb.append(exp2 == Integer.MIN_VALUE ? "2147483648" : Long.toString(Math.abs((long) exp2)));
        return sb.toString();
    }

    /**
     * Go's {@code strconv} {@code 'x'}/{@code 'X'} format:
     * {@code [-]0x1.yyyyp±ddd} (ftoa.go fmtX, verbatim port).
     */
    public static String formatHex(double v, char fmt, int prec, int bitSize) {
        long b = bitSize == 32 ? Float.floatToRawIntBits((float) v) & 0xFFFF_FFFFL
                : Double.doubleToRawLongBits(v);
        int expBits = bitSize == 32 ? 8 : 11;
        int mantBits = bitSize == 32 ? 23 : 52;
        int bias = bitSize == 32 ? -127 : -1023;
        boolean neg = (b >>> (expBits + mantBits)) != 0;
        int exp = (int) ((b >>> mantBits) & ((1 << expBits) - 1));
        long mant = b & ((1L << mantBits) - 1);
        if (exp == (1 << expBits) - 1) {
            return mant != 0 ? "NaN" : (neg ? "-Inf" : "+Inf");
        }
        if (exp == 0) {
            exp++;
        } else {
            mant |= 1L << mantBits;
        }
        exp += bias;

        if (mant == 0) {
            exp = 0;
        }
        // Shift digits so the leading 1 (if any) is at bit 1<<60.
        mant <<= 60 - mantBits;
        while (mant != 0 && (mant & (1L << 60)) == 0) {
            mant <<= 1;
            exp--;
        }
        // Round if requested.
        if (prec >= 0 && prec < 15) {
            int shift = prec * 4;
            long extra = (mant << shift) & ((1L << 60) - 1);
            mant >>>= 60 - shift;
            if (Long.compareUnsigned(extra | (mant & 1), 1L << 59) > 0) {
                mant++;
            }
            mant <<= 60 - shift;
            if ((mant & (1L << 61)) != 0) {
                mant >>= 1;
                exp++;
            }
        }

        char[] hex = fmt == 'X' ? "0123456789ABCDEF".toCharArray()
                : "0123456789abcdef".toCharArray();
        StringBuilder sb = new StringBuilder();
        if (neg) {
            sb.append('-');
        }
        sb.append('0').append(fmt).append((char) ('0' + ((mant >>> 60) & 1)));
        // .fraction
        mant <<= 4; // remove leading 0 or 1
        if (prec < 0 && mant != 0) {
            sb.append('.');
            while (mant != 0) {
                sb.append(hex[(int) ((mant >>> 60) & 15)]);
                mant <<= 4;
            }
        } else if (prec > 0) {
            sb.append('.');
            for (int i = 0; i < prec; i++) {
                sb.append(hex[(int) ((mant >>> 60) & 15)]);
                mant <<= 4;
            }
        }
        // p±ddd(d)
        sb.append(fmt == 'X' ? 'P' : 'p');
        int e = exp;
        sb.append(e < 0 ? '-' : '+');
        if (e < 0) {
            e = -e;
        }
        if (e < 100) {
            sb.append((char) (e / 10 + '0')).append((char) (e % 10 + '0'));
        } else if (e < 1000) {
            sb.append((char) (e / 100 + '0')).append((char) (e / 10 % 10 + '0'))
                    .append((char) (e % 10 + '0'));
        } else {
            sb.append((char) (e / 1000 + '0')).append((char) (e / 100 % 10 + '0'))
                    .append((char) (e / 10 % 10 + '0')).append((char) (e % 10 + '0'));
        }
        return sb.toString();
    }
}
