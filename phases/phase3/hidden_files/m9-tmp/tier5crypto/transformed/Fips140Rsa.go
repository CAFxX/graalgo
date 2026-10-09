// P3-M9 tier-5: synthetic `crypto/internal/fips140/rsa`.
// go1.27.1 vendored rsa.go, keygen.go, largeexponent.go, pkcs1v15.go,
// pkcs1v22.go (cast.go excluded). Synthetic-only.
package rsa

import (
	"bytes"
	"crypto/internal/fips140"
	"crypto/internal/fips140/bigmod"
	"errors"
	"crypto/internal/fips140/drbg"
	"io"
	"crypto/internal/constanttime"
	"crypto/internal/fips140/subtle"
	"hash"
	"crypto/internal/fips140/sha256"
	"crypto/internal/fips140/sha3"
	"crypto/internal/fips140/sha512"
)


// ---- vendored from crypto/internal/fips140/rsa/rsa.go ----




type PublicKey struct {
	N *bigmod.Modulus
	E int
}

// Size returns the modulus size in bytes. Raw signatures and ciphertexts
// for or by this public key will have the same size.
func (pub *PublicKey) Size() int {
	return (pub.N.BitLen() + 7) / 8
}

type PrivateKey struct {
	// pub has already been checked with checkPublicKey.
	pub PublicKey
	d   *bigmod.Nat
	// The following values are not set for deprecated multi-prime keys.
	//
	// Since they are always set for keys in FIPS mode, for SP 800-56B Rev. 2
	// purposes we always use the Chinese Remainder Theorem (CRT) format.
	p, q *bigmod.Modulus // p × q = n
	// dP and dQ are used as exponents, so we store them as big-endian byte
	// slices to be passed to [bigmod.Nat.Exp].
	dP   []byte      // d mod (p - 1)
	dQ   []byte      // d mod (q - 1)
	qInv *bigmod.Nat // qInv = q⁻¹ mod p
	// fipsApproved is false if this key does not comply with FIPS 186-5 or
	// SP 800-56B Rev. 2.
	fipsApproved bool
}

func (priv *PrivateKey) PublicKey() *PublicKey {
	return &priv.pub
}

// NewPrivateKey creates a new RSA private key from the given parameters.
//
// All values are in big-endian byte slice format, and may have leading zeros
// or be shorter if leading zeroes were trimmed.
type __mrNewPrivateKeyPair struct {
	v0 *PrivateKey
	v1 error
}

func __mrNewPrivateKey(N []byte, e int, d, P, Q []byte) __mrNewPrivateKeyPair {
	n, err := bigmod.NewModulus(N)
	if err != nil {
		return __mrNewPrivateKeyPair{nil, err}
	}
	p, err := bigmod.NewModulus(P)
	if err != nil {
		return __mrNewPrivateKeyPair{nil, err}
	}
	q, err := bigmod.NewModulus(Q)
	if err != nil {
		return __mrNewPrivateKeyPair{nil, err}
	}
	dN, err := bigmod.NewNat().SetBytes(d, n)
	if err != nil {
		return __mrNewPrivateKeyPair{nil, err}
	}
	__fw0, __fw1 := newPrivateKey(n, e, dN, p, q)
	return __mrNewPrivateKeyPair{__fw0, __fw1}
}

func NewPrivateKey(N []byte, e int, d, P, Q []byte) (*PrivateKey, error) {
	panic("crypto/internal/fips140/rsa: NewPrivateKey: multi-result desugar")
}

type __mrnewPrivateKeyPair struct {
	v0 *PrivateKey
	v1 error
}

func __mrnewPrivateKey(n *bigmod.Modulus, e int, d *bigmod.Nat, p, q *bigmod.Modulus) __mrnewPrivateKeyPair {
	pMinusOne := p.Nat().SubOne(p)
	pMinusOneMod, err := bigmod.NewModulus(pMinusOne.Bytes(p))
	if err != nil {
		return __mrnewPrivateKeyPair{nil, err}
	}
	dP := bigmod.NewNat().Mod(d, pMinusOneMod).Bytes(pMinusOneMod)

	qMinusOne := q.Nat().SubOne(q)
	qMinusOneMod, err := bigmod.NewModulus(qMinusOne.Bytes(q))
	if err != nil {
		return __mrnewPrivateKeyPair{nil, err}
	}
	dQ := bigmod.NewNat().Mod(d, qMinusOneMod).Bytes(qMinusOneMod)

	// Constant-time modular inversion with prime modulus by Fermat's Little
	// Theorem: qInv = q⁻¹ mod p = q^(p-2) mod p.
	if p.Nat().IsOdd() == 0 {
		// [bigmod.Nat.Exp] requires an odd modulus.
		return __mrnewPrivateKeyPair{nil, errors.New("crypto/rsa: p is even")}
	}
	pMinusTwo := p.Nat().SubOne(p).SubOne(p).Bytes(p)
	qInv := bigmod.NewNat().Mod(q.Nat(), p)
	qInv.Exp(qInv, pMinusTwo, p)

	pk := &PrivateKey{
		pub: PublicKey{
			N: n, E: e,
		},
		d: d, p: p, q: q,
		dP: dP, dQ: dQ, qInv: qInv,
	}
	if err := checkPrivateKey(pk); err != nil {
		return __mrnewPrivateKeyPair{nil, err}
	}
	return __mrnewPrivateKeyPair{pk, nil}
}

func newPrivateKey(n *bigmod.Modulus, e int, d *bigmod.Nat, p, q *bigmod.Modulus) (*PrivateKey, error) {
	panic("crypto/internal/fips140/rsa: newPrivateKey: multi-result desugar")
}

// NewPrivateKeyWithPrecomputation creates a new RSA private key from the given
// parameters, which include precomputed CRT values.
type __mrNewPrivateKeyWithPrecomputationPair struct {
	v0 *PrivateKey
	v1 error
}

func __mrNewPrivateKeyWithPrecomputation(N []byte, e int, d, P, Q, dP, dQ, qInv []byte) __mrNewPrivateKeyWithPrecomputationPair {
	n, err := bigmod.NewModulus(N)
	if err != nil {
		return __mrNewPrivateKeyWithPrecomputationPair{nil, err}
	}
	p, err := bigmod.NewModulus(P)
	if err != nil {
		return __mrNewPrivateKeyWithPrecomputationPair{nil, err}
	}
	q, err := bigmod.NewModulus(Q)
	if err != nil {
		return __mrNewPrivateKeyWithPrecomputationPair{nil, err}
	}
	dN, err := bigmod.NewNat().SetBytes(d, n)
	if err != nil {
		return __mrNewPrivateKeyWithPrecomputationPair{nil, err}
	}
	qInvNat, err := bigmod.NewNat().SetBytes(qInv, p)
	if err != nil {
		return __mrNewPrivateKeyWithPrecomputationPair{nil, err}
	}

	pk := &PrivateKey{
		pub: PublicKey{
			N: n, E: e,
		},
		d: dN, p: p, q: q,
		dP: dP, dQ: dQ, qInv: qInvNat,
	}
	if err := checkPrivateKey(pk); err != nil {
		return __mrNewPrivateKeyWithPrecomputationPair{nil, err}
	}
	return __mrNewPrivateKeyWithPrecomputationPair{pk, nil}
}

func NewPrivateKeyWithPrecomputation(N []byte, e int, d, P, Q, dP, dQ, qInv []byte) (*PrivateKey, error) {
	panic("crypto/internal/fips140/rsa: NewPrivateKeyWithPrecomputation: multi-result desugar")
}

// NewPrivateKeyWithoutCRT creates a new RSA private key from the given parameters.
//
// This is meant for deprecated multi-prime keys, and is not FIPS 140 compliant.
type __mrNewPrivateKeyWithoutCRTPair struct {
	v0 *PrivateKey
	v1 error
}

func __mrNewPrivateKeyWithoutCRT(N []byte, e int, d []byte) __mrNewPrivateKeyWithoutCRTPair {
	n, err := bigmod.NewModulus(N)
	if err != nil {
		return __mrNewPrivateKeyWithoutCRTPair{nil, err}
	}
	dN, err := bigmod.NewNat().SetBytes(d, n)
	if err != nil {
		return __mrNewPrivateKeyWithoutCRTPair{nil, err}
	}
	pk := &PrivateKey{
		pub: PublicKey{
			N: n, E: e,
		},
		d: dN,
	}
	if err := checkPrivateKey(pk); err != nil {
		return __mrNewPrivateKeyWithoutCRTPair{nil, err}
	}
	return __mrNewPrivateKeyWithoutCRTPair{pk, nil}
}

func NewPrivateKeyWithoutCRT(N []byte, e int, d []byte) (*PrivateKey, error) {
	panic("crypto/internal/fips140/rsa: NewPrivateKeyWithoutCRT: multi-result desugar")
}

// Export returns the key parameters in big-endian byte slice format.
//
// P, Q, dP, dQ, and qInv may be nil if the key was created with
// NewPrivateKeyWithoutCRT.
type __mrPrivateKey_ExportPair struct {
	N []byte
	e int
	d []byte
	P []byte
	Q []byte
	dP []byte
	dQ []byte
	qInv []byte
}

func __mrPrivateKey_Export(priv *PrivateKey) __mrPrivateKey_ExportPair {
	var N []byte
	var e int
	var d []byte
	var P []byte
	var Q []byte
	var dP []byte
	var dQ []byte
	var qInv []byte

	N = priv.pub.N.Nat().Bytes(priv.pub.N)
	e = priv.pub.E
	d = priv.d.Bytes(priv.pub.N)
	if priv.dP == nil {
		return __mrPrivateKey_ExportPair{N, e, d, P, Q, dP, dQ, qInv}
	}
	P = priv.p.Nat().Bytes(priv.p)
	Q = priv.q.Nat().Bytes(priv.q)
	dP = bytes.Clone(priv.dP)
	dQ = bytes.Clone(priv.dQ)
	qInv = priv.qInv.Bytes(priv.p)
	return __mrPrivateKey_ExportPair{N, e, d, P, Q, dP, dQ, qInv}
}

func (priv *PrivateKey) Export() (N []byte, e int, d, P, Q, dP, dQ, qInv []byte) {
	panic("crypto/internal/fips140/rsa: Export: multi-result desugar")
}

// checkPrivateKey is called by the NewPrivateKey and GenerateKey functions, and
// is allowed to modify priv.fipsApproved.
func checkPrivateKey(priv *PrivateKey) error {
	priv.fipsApproved = true

	if fipsApproved, err := checkPublicKey(&priv.pub); err != nil {
		return err
	} else if !fipsApproved {
		priv.fipsApproved = false
	}

	if priv.dP == nil {
		// Legacy and deprecated multi-prime keys.
		priv.fipsApproved = false
		return nil
	}

	N := priv.pub.N
	p := priv.p
	q := priv.q

	// FIPS 186-5, Section 5.1 requires "that p and q be of the same bit length."
	if p.BitLen() != q.BitLen() {
		priv.fipsApproved = false
	}

	// Check that pq ≡ 1 mod N (and that p < N and q < N).
	pN := bigmod.NewNat().ExpandFor(N)
	if _, err := pN.SetBytes(p.Nat().Bytes(p), N); err != nil {
		return errors.New("crypto/rsa: invalid prime")
	}
	qN := bigmod.NewNat().ExpandFor(N)
	if _, err := qN.SetBytes(q.Nat().Bytes(q), N); err != nil {
		return errors.New("crypto/rsa: invalid prime")
	}
	if pN.Mul(qN, N).IsZero() != 1 {
		return errors.New("crypto/rsa: p * q != n")
	}

	// Check that de ≡ 1 mod p-1, and de ≡ 1 mod q-1.
	//
	// This implies that e is coprime to each p-1 as e has a multiplicative
	// inverse. Therefore e is coprime to lcm(p-1,q-1) = λ(N).
	// It also implies that a^de ≡ a mod p as a^(p-1) ≡ 1 mod p. Thus a^de ≡ a
	// mod n for all a coprime to n, as required.
	//
	// This checks dP, dQ, and e.
	pMinus1, err := bigmod.NewModulus(p.Nat().SubOne(p).Bytes(p))
	if err != nil {
		return errors.New("crypto/rsa: invalid prime")
	}
	dP, err := bigmod.NewNat().SetBytes(priv.dP, pMinus1)
	if err != nil {
		return errors.New("crypto/rsa: invalid CRT exponent")
	}
	de := bigmod.NewNat()
	de.SetUint(uint(priv.pub.E)).ExpandFor(pMinus1)
	de.Mul(dP, pMinus1)
	if de.IsOne() != 1 {
		return errors.New("crypto/rsa: invalid CRT exponent")
	}

	qMinus1, err := bigmod.NewModulus(q.Nat().SubOne(q).Bytes(q))
	if err != nil {
		return errors.New("crypto/rsa: invalid prime")
	}
	dQ, err := bigmod.NewNat().SetBytes(priv.dQ, qMinus1)
	if err != nil {
		return errors.New("crypto/rsa: invalid CRT exponent")
	}
	de.SetUint(uint(priv.pub.E)).ExpandFor(qMinus1)
	de.Mul(dQ, qMinus1)
	if de.IsOne() != 1 {
		return errors.New("crypto/rsa: invalid CRT exponent")
	}

	// Check that qInv * q ≡ 1 mod p.
	qP, err := bigmod.NewNat().SetOverflowingBytes(q.Nat().Bytes(q), p)
	if err != nil {
		// q >= 2^⌈log2(p)⌉
		qP = bigmod.NewNat().Mod(q.Nat(), p)
	}
	if qP.Mul(priv.qInv, p).IsOne() != 1 {
		return errors.New("crypto/rsa: invalid CRT coefficient")
	}

	// Check d against dP and dQ, even though we never actually use d,
	// to make sure the key is consistent.
	dP1 := bigmod.NewNat().Mod(priv.d, pMinus1)
	if dP1.Equal(dP) != 1 {
		return errors.New("crypto/rsa: d does not match dP")
	}
	dQ1 := bigmod.NewNat().Mod(priv.d, qMinus1)
	if dQ1.Equal(dQ) != 1 {
		return errors.New("crypto/rsa: d does not match dQ")
	}

	// Check that |p - q| > 2^(nlen/2 - 100).
	//
	// If p and q are very close to each other, then N=pq can be trivially
	// factored using Fermat's factorization method. Broken RSA implementations
	// do generate such keys. See Hanno Böck, Fermat Factorization in the Wild,
	// https://eprint.iacr.org/2023/026.pdf.
	diff := bigmod.NewNat()
	if qP, err := bigmod.NewNat().SetBytes(q.Nat().Bytes(q), p); err != nil {
		// q > p
		pQ, err := bigmod.NewNat().SetBytes(p.Nat().Bytes(p), q)
		if err != nil {
			return errors.New("crypto/rsa: p == q")
		}
		// diff = 0 - p mod q = q - p
		diff.ExpandFor(q).Sub(pQ, q)
	} else {
		// p > q
		// diff = 0 - q mod p = p - q
		diff.ExpandFor(p).Sub(qP, p)
	}
	// A tiny bit of leakage is acceptable because it's not adaptive, an
	// attacker only learns the magnitude of p - q.
	if diff.BitLenVarTime() <= N.BitLen()/2-100 {
		return errors.New("crypto/rsa: |p - q| too small")
	}

	// Check that d > 2^(nlen/2).
	//
	// See section 3 of https://crypto.stanford.edu/~dabo/papers/RSA-survey.pdf
	// for more details about attacks on small d values.
	//
	// Likewise, the leakage of the magnitude of d is not adaptive.
	if priv.d.BitLenVarTime() <= N.BitLen()/2 {
		return errors.New("crypto/rsa: d too small")
	}

	return nil
}

type __mrcheckPublicKeyPair struct {
	fipsApproved bool
	err error
}

func __mrcheckPublicKey(pub *PublicKey) __mrcheckPublicKeyPair {
	var fipsApproved bool

	fipsApproved = true
	if pub.N == nil {
		return __mrcheckPublicKeyPair{false, errors.New("crypto/rsa: missing public modulus")}
	}
	if pub.N.Nat().IsOdd() == 0 {
		return __mrcheckPublicKeyPair{false, errors.New("crypto/rsa: public modulus is even")}
	}
	// FIPS 186-5, Section 5.1: "This standard specifies the use of a modulus
	// whose bit length is an even integer and greater than or equal to 2048
	// bits."
	if pub.N.BitLen() < 2048 {
		fipsApproved = false
	}
	if pub.N.BitLen()%2 == 1 {
		fipsApproved = false
	}
	if pub.E < 2 {
		return __mrcheckPublicKeyPair{false, errors.New("crypto/rsa: public exponent too small or negative")}
	}
	// e needs to be coprime with p-1 and q-1, since it must be invertible
	// modulo λ(pq). Since p and q are prime, this means e needs to be odd.
	if pub.E&1 == 0 {
		return __mrcheckPublicKeyPair{false, errors.New("crypto/rsa: public exponent is even")}
	}
	// FIPS 186-5, Section 5.5(e): "The exponent e shall be an odd, positive
	// integer such that 2¹⁶ < e < 2²⁵⁶."
	if pub.E <= 1<<16 {
		fipsApproved = false
	}
	// We require pub.E to fit into a 32-bit integer so that we
	// do not have different behavior depending on whether
	// int is 32 or 64 bits. See also
	// https://www.imperialviolet.org/2012/03/16/rsae.html.
	if pub.E > 1<<31-1 {
		return __mrcheckPublicKeyPair{false, errors.New("crypto/rsa: public exponent too large")}
	}
	return __mrcheckPublicKeyPair{fipsApproved, nil}
}

func checkPublicKey(pub *PublicKey) (fipsApproved bool, err error) {
	panic("crypto/internal/fips140/rsa: checkPublicKey: multi-result desugar")
}

// Encrypt performs the RSA public key operation.
type __mrEncryptPair struct {
	v0 []byte
	v1 error
}

func __mrEncrypt(pub *PublicKey, plaintext []byte) __mrEncryptPair {
	fips140.RecordNonApproved()
	if _, err := checkPublicKey(pub); err != nil {
		return __mrEncryptPair{nil, err}
	}
	__fw0, __fw1 := encrypt(pub, plaintext)
	return __mrEncryptPair{__fw0, __fw1}
}

func Encrypt(pub *PublicKey, plaintext []byte) ([]byte, error) {
	panic("crypto/internal/fips140/rsa: Encrypt: multi-result desugar")
}

type __mrencryptPair struct {
	v0 []byte
	v1 error
}

func __mrencrypt(pub *PublicKey, plaintext []byte) __mrencryptPair {
	m, err := bigmod.NewNat().SetBytes(plaintext, pub.N)
	if err != nil {
		return __mrencryptPair{nil, err}
	}
	return __mrencryptPair{bigmod.NewNat().ExpShortVarTime(m, uint(pub.E), pub.N).Bytes(pub.N), nil}
}

func encrypt(pub *PublicKey, plaintext []byte) ([]byte, error) {
	panic("crypto/internal/fips140/rsa: encrypt: multi-result desugar")
}

var ErrMessageTooLong = errors.New("crypto/rsa: message too long for RSA key size")
var ErrDecryption = errors.New("crypto/rsa: decryption error")
var ErrVerification = errors.New("crypto/rsa: verification error")

const withCheck = true
const noCheck = false

// DecryptWithoutCheck performs the RSA private key operation.
type __mrDecryptWithoutCheckPair struct {
	v0 []byte
	v1 error
}

func __mrDecryptWithoutCheck(priv *PrivateKey, ciphertext []byte) __mrDecryptWithoutCheckPair {
	fips140.RecordNonApproved()
	__fw0, __fw1 := decrypt(priv, ciphertext, noCheck)
	return __mrDecryptWithoutCheckPair{__fw0, __fw1}
}

func DecryptWithoutCheck(priv *PrivateKey, ciphertext []byte) ([]byte, error) {
	panic("crypto/internal/fips140/rsa: DecryptWithoutCheck: multi-result desugar")
}

// DecryptWithCheck performs the RSA private key operation and checks the
// result to defend against errors in the CRT computation.
type __mrDecryptWithCheckPair struct {
	v0 []byte
	v1 error
}

func __mrDecryptWithCheck(priv *PrivateKey, ciphertext []byte) __mrDecryptWithCheckPair {
	fips140.RecordNonApproved()
	__fw0, __fw1 := decrypt(priv, ciphertext, withCheck)
	return __mrDecryptWithCheckPair{__fw0, __fw1}
}

func DecryptWithCheck(priv *PrivateKey, ciphertext []byte) ([]byte, error) {
	panic("crypto/internal/fips140/rsa: DecryptWithCheck: multi-result desugar")
}

// decrypt performs an RSA decryption of ciphertext into out. If check is true,
// m^e is calculated and compared with ciphertext, in order to defend against
// errors in the CRT computation.
type __mrdecryptPair struct {
	v0 []byte
	v1 error
}

func __mrdecrypt(priv *PrivateKey, ciphertext []byte, check bool) __mrdecryptPair {
	if !priv.fipsApproved {
		fips140.RecordNonApproved()
	}

	var m *bigmod.Nat
	N, E := priv.pub.N, priv.pub.E

	c, err := bigmod.NewNat().SetBytes(ciphertext, N)
	if err != nil {
		return __mrdecryptPair{nil, ErrDecryption}
	}

	if priv.dP == nil {
		// Legacy codepath for deprecated multi-prime keys.
		fips140.RecordNonApproved()
		m = bigmod.NewNat().Exp(c, priv.d.Bytes(N), N)

	} else {
		P, Q := priv.p, priv.q
		t0 := bigmod.NewNat()
		// m = c ^ Dp mod p
		m = bigmod.NewNat().Exp(t0.Mod(c, P), priv.dP, P)
		// m2 = c ^ Dq mod q
		m2 := bigmod.NewNat().Exp(t0.Mod(c, Q), priv.dQ, Q)
		// m = m - m2 mod p
		m.Sub(t0.Mod(m2, P), P)
		// m = m * Qinv mod p
		m.Mul(priv.qInv, P)
		// m = m * q mod N
		m.ExpandFor(N).Mul(t0.Mod(Q.Nat(), N), N)
		// m = m + m2 mod N
		m.Add(m2.ExpandFor(N), N)
	}

	if check {
		c1 := bigmod.NewNat().ExpShortVarTime(m, uint(E), N)
		if c1.Equal(c) != 1 {
			return __mrdecryptPair{nil, ErrDecryption}
		}
	}

	return __mrdecryptPair{m.Bytes(N), nil}
}

func decrypt(priv *PrivateKey, ciphertext []byte, check bool) ([]byte, error) {
	panic("crypto/internal/fips140/rsa: decrypt: multi-result desugar")
}


// ---- vendored from crypto/internal/fips140/rsa/keygen.go ----




// GenerateKey generates a new RSA key pair of the given bit size.
// bits must be at least 32.
//
// It follows the process described at c2sp.org/det-keygen, which is compliant
// with FIPS 186-5, Appendix A.1, IFC Key Pair Generation and FIPS 186-5,
// Appendix A.1.3, Generation of Random Primes that are Probably Prime.
// The prime candidates are drawn from rand, which in production will be the
// global DRBG, while in tests can be an HMAC_DRBG as specified in
// c2sp.org/det-keygen, to allow using its tests vectors.
type __mrGenerateKeyPair struct {
	v0 *PrivateKey
	v1 error
}

func __mrGenerateKey(rand io.Reader, bits int) __mrGenerateKeyPair {
	if bits < 32 {
		return __mrGenerateKeyPair{nil, errors.New("rsa: key too small")}
	}
	fips140.RecordApproved()
	if bits < 2048 || bits%2 == 1 {
		fips140.RecordNonApproved()
	}

	for {
		p, err := randomPrime(rand, (bits+1)/2)
		if err != nil {
			return __mrGenerateKeyPair{nil, err}
		}
		q, err := randomPrime(rand, bits/2)
		if err != nil {
			return __mrGenerateKeyPair{nil, err}
		}

		P, err := bigmod.NewModulus(p)
		if err != nil {
			return __mrGenerateKeyPair{nil, err}
		}
		Q, err := bigmod.NewModulus(q)
		if err != nil {
			return __mrGenerateKeyPair{nil, err}
		}

		if Q.Nat().ExpandFor(P).Equal(P.Nat()) == 1 {
			return __mrGenerateKeyPair{nil, errors.New("rsa: generated p == q, random source is broken")}
		}

		N, err := bigmod.NewModulusProduct(p, q)
		if err != nil {
			return __mrGenerateKeyPair{nil, err}
		}
		if N.BitLen() != bits {
			return __mrGenerateKeyPair{nil, errors.New("rsa: internal error: modulus size incorrect")}
		}

		// d can be safely computed as e⁻¹ mod φ(N) where φ(N) = (p-1)(q-1), and
		// indeed that's what both the original RSA paper and the pre-FIPS
		// crypto/rsa implementation did.
		//
		// However, FIPS 186-5, A.1.1(3) requires computing it as e⁻¹ mod λ(N)
		// where λ(N) = lcm(p-1, q-1).
		//
		// This makes d smaller by 1.82 bits on average, which is irrelevant both
		// because we exclusively use the CRT for private operations and because
		// we use constant time windowed exponentiation. On the other hand, it
		// requires computing a GCD of two even numbers, and then a division,
		// both complex variable-time operations.
		λ, err := totient(P, Q)
		if err == errDivisorTooLarge {
			// The divisor is too large, try again with different primes.
			continue
		}
		if err != nil {
			return __mrGenerateKeyPair{nil, err}
		}

		e := bigmod.NewNat().SetUint(65537)
		d, ok := bigmod.NewNat().InverseVarTime(e, λ)
		if !ok {
			// This checks that GCD(e, lcm(p-1, q-1)) = 1, which is equivalent
			// to checking GCD(e, p-1) = 1 and GCD(e, q-1) = 1 separately in
			// FIPS 186-5, Appendix A.1.3, steps 4.5 and 5.6.
			//
			// We waste a prime by retrying the whole process, since 65537 is
			// probably only a factor of one of p-1 or q-1, but the probability
			// of this check failing is ≈ 2⁻¹⁵, so it doesn't matter.
			continue
		}

		if e.ExpandFor(λ).Mul(d, λ).IsOne() == 0 {
			return __mrGenerateKeyPair{nil, errors.New("rsa: internal error: e*d != 1 mod λ(N)")}
		}

		// FIPS 186-5, A.1.1(3) requires checking that d > 2^(nlen / 2).
		//
		// The probability of this check failing when d is derived from
		// (e, p, q) is roughly
		//
		//   2^(nlen/2) / λ(N) ≈ 2^(-nlen/2 + 1.82)
		//
		// so less than 2⁻¹²⁶ for keys larger than 256 bits.
		//
		// We still need to check to comply with FIPS 186-5, but knowing it has
		// negligible chance of failure we can defer the check to the end of key
		// generation and return an error if it fails. See [checkPrivateKey].

		k, err := newPrivateKey(N, 65537, d, P, Q)
		if err != nil {
			return __mrGenerateKeyPair{nil, err}
		}

		if k.fipsApproved {
			fips140.PCT("RSA sign and verify PCT", func() error {
				hash := []byte{
					0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
					0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
					0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
					0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				}
				sig, err := signPKCS1v15(k, "SHA-256", hash)
				if err != nil {
					return err
				}
				return verifyPKCS1v15(k.PublicKey(), "SHA-256", hash, sig)
			})
		}

		return __mrGenerateKeyPair{k, nil}
	}
}

func GenerateKey(rand io.Reader, bits int) (*PrivateKey, error) {
	panic("crypto/internal/fips140/rsa: GenerateKey: multi-result desugar")
}

// errDivisorTooLarge is returned by [totient] when gcd(p-1, q-1) is too large.
var errDivisorTooLarge = errors.New("divisor too large")

// totient computes the Carmichael totient function λ(N) = lcm(p-1, q-1).
//
// p and q must be primes congruent to 7 mod 8, so that p-1 and q-1 are both
// even but not divisible by 4.
//
// totient returns errDivisorTooLarge if GCD(p-1, q-1) is larger than 2³²-1.
type __mrtotientPair struct {
	v0 *bigmod.Modulus
	v1 error
}

func __mrtotient(p, q *bigmod.Modulus) __mrtotientPair {
	a, b := p.Nat().SubOne(p), q.Nat().SubOne(q)
	if a.IsOdd() == 1 || b.IsOdd() == 1 {
		return __mrtotientPair{nil, errors.New("rsa: internal error: p and q must be 7 mod 8")}
	}

	// lcm(a, b) = a×b / gcd(a, b) = a × (b / gcd(a, b))

	// Our GCD requires at least one of the numbers to be odd.
	// We know that a / 2 and b / 2 are odd because p and q are 7 mod 8.
	// For LCM we only need to preserve the larger prime power of each
	// prime factor, so we can shift out 2 from either of them.
	// For odd x, y and m >= n, lcm(x×2, y×2) = lcm(x×2, y).
	b = b.ShiftRightByOne()
	if b.IsOdd() == 0 {
		return __mrtotientPair{nil, errors.New("rsa: internal error: p and q must be 7 mod 8")}
	}

	gcd, err := bigmod.NewNat().GCDVarTime(a, b)
	if err != nil {
		return __mrtotientPair{nil, err}
	}
	if gcd.IsOdd() == 0 {
		return __mrtotientPair{nil, errors.New("rsa: internal error: gcd(a, b) is even")}
	}

	// To avoid implementing multiple-precision division, we just try again if
	// the divisor doesn't fit in a single word. This would have a chance of
	// ~2⁻⁶⁴ on 64-bit platforms, and ~2⁻³² on 32-bit platforms, but testing
	// 2⁻⁶⁴ edge cases is impractical, and we'd rather not behave differently on
	// different platforms, so we reject divisors above 2³²-1. Note that we also
	// add back the factor of 2 we shifted out above.
	if gcd.BitLenVarTime()+1 > 32 {
		return __mrtotientPair{nil, errDivisorTooLarge}
	}
	if gcd.IsZero() == 1 || gcd.Bits()[0] == 0 {
		return __mrtotientPair{nil, errors.New("rsa: internal error: gcd(a, b) is zero")}
	}
	if rem := b.DivShortVarTime(gcd.Bits()[0]); rem != 0 {
		return __mrtotientPair{nil, errors.New("rsa: internal error: b is not divisible by gcd(a, b)")}
	}

	__fw0, __fw1 := bigmod.NewModulusProduct(a.Bytes(p), b.Bytes(q))
	return __mrtotientPair{__fw0, __fw1}
}

func totient(p, q *bigmod.Modulus) (*bigmod.Modulus, error) {
	panic("crypto/internal/fips140/rsa: totient: multi-result desugar")
}

// randomPrime returns a random prime number of the given bit size following
// the process in FIPS 186-5, Appendix A.1.3.
type __mrrandomPrimePair struct {
	v0 []byte
	v1 error
}

func __mrrandomPrime(rand io.Reader, bits int) __mrrandomPrimePair {
	if bits < 16 {
		return __mrrandomPrimePair{nil, errors.New("rsa: prime size must be at least 16 bits")}
	}

	b := make([]byte, (bits+7)/8)
	for {
		if err := drbg.ReadWithReader(rand, b); err != nil {
			return __mrrandomPrimePair{nil, err}
		}
		// Clear the most significant bits to reach the desired size. We use a
		// mask rather than right-shifting b[0] to make it easier to inject test
		// candidates, which can be represented as simple big-endian integers.
		excess := len(b)*8 - bits
		b[0] &= 0b1111_1111 >> excess

		// Don't let the value be too small: set the most significant two bits.
		// Setting the top two bits, rather than just the top bit, means that
		// when two of these values are multiplied together, the result isn't
		// ever one bit short.
		if excess < 7 {
			b[0] |= 0b1100_0000 >> excess
		} else {
			b[0] |= 0b0000_0001
			b[1] |= 0b1000_0000
		}

		// Set the three least significant bits, which makes the value 7 mod 8
		// (steps 4.3 and 5.3). Even numbers are not prime, and an odd
		// (p - 1) / 2 simplifies [millerRabinIteration] and the GCD in [totient].
		b[len(b)-1] |= 0b0000_0111

		// We don't need to check for p >= √2 × 2^(bits-1) (steps 4.4 and 5.4)
		// because we set the top two bits above, so
		//
		//   p > 2^(bits-1) + 2^(bits-2) = 3⁄2 × 2^(bits-1) > √2 × 2^(bits-1)
		//

		// Step 5.5 requires checking that |p - q| > 2^(nlen/2 - 100).
		//
		// The probability of |p - q| ≤ k where p and q are uniformly random in
		// the range (a, b) is 1 - (b-a-k)^2 / (b-a)^2, so the probability of
		// this check failing during key generation is 2⁻⁹⁷.
		//
		// We still need to check to comply with FIPS 186-5, but knowing it has
		// negligible chance of failure we can defer the check to the end of key
		// generation and return an error if it fails. See [checkPrivateKey].

		if isPrime(b) {
			return __mrrandomPrimePair{b, nil}
		}
	}
}

func randomPrime(rand io.Reader, bits int) ([]byte, error) {
	panic("crypto/internal/fips140/rsa: randomPrime: multi-result desugar")
}

// isPrime runs the Miller-Rabin Probabilistic Primality Test from
// FIPS 186-5, Appendix B.3.1.
//
// w must be a random integer equal to 3 mod 4 in big-endian order.
// isPrime might return false positives for adversarially chosen values.
//
// isPrime is not constant-time.
func isPrime(w []byte) bool {
	mr, err := millerRabinSetup(w)
	if err != nil {
		// w is zero, one, or even.
		return false
	}

	// Before Miller-Rabin, rule out most composites with trial divisions.
	for i := 0; i < len(primes); i += 3 {
		p1, p2, p3 := primes[i], primes[i+1], primes[i+2]
		r := mr.w.Nat().DivShortVarTime(p1 * p2 * p3)
		if r%p1 == 0 || r%p2 == 0 || r%p3 == 0 {
			return false
		}
	}

	// iterations is the number of Miller-Rabin rounds, each with a
	// randomly-selected base.
	//
	// The worst case false positive rate for a single iteration is 1/4 per
	// https://eprint.iacr.org/2018/749, so if w were selected adversarially, we
	// would need up to 64 iterations to get to a negligible (2⁻¹²⁸) chance of
	// false positive.
	//
	// However, since this function is only used for randomly-selected w in the
	// context of RSA key generation, we can use a smaller number of iterations.
	// The exact number depends on the size of the prime (and the implied
	// security level). See BoringSSL for the full formula.
	// https://cs.opensource.google/boringssl/boringssl/+/master:crypto/fipsmodule/bn/prime.c.inc;l=208-283;drc=3a138e43
	bits := mr.w.BitLen()
	var iterations int
	switch {
	case bits >= 3747:
		iterations = 3
	case bits >= 1345:
		iterations = 4
	case bits >= 476:
		iterations = 5
	case bits >= 400:
		iterations = 6
	case bits >= 347:
		iterations = 7
	case bits >= 308:
		iterations = 8
	case bits >= 55:
		iterations = 27
	default:
		iterations = 34
	}

	b := make([]byte, (bits+7)/8)
	for {
		drbg.Read(b)
		excess := len(b)*8 - bits
		b[0] &= 0b1111_1111 >> excess
		result, err := millerRabinIteration(mr, b)
		if err != nil {
			// b was rejected.
			continue
		}
		if result == millerRabinCOMPOSITE {
			return false
		}
		iterations--
		if iterations == 0 {
			return true
		}
	}
}

// primes are the first prime numbers (except 2), such that the product of any
// three primes fits in a uint32.
//
// More primes cause fewer Miller-Rabin tests of composites (nothing can help
// with the final test on the actual prime) but have diminishing returns: these
// 255 primes catch 84.9% of composites, the next 255 would catch 1.5% more.
// Adding primes can still be marginally useful since they only compete with the
// (much more expensive) first Miller-Rabin round for candidates that were not
// rejected by the previous primes.
var primes = []uint{
	3, 5, 7, 11, 13, 17, 19, 23, 29, 31, 37, 41, 43, 47, 53,
	59, 61, 67, 71, 73, 79, 83, 89, 97, 101, 103, 107, 109, 113, 127,
	131, 137, 139, 149, 151, 157, 163, 167, 173, 179, 181, 191, 193, 197, 199,
	211, 223, 227, 229, 233, 239, 241, 251, 257, 263, 269, 271, 277, 281, 283,
	293, 307, 311, 313, 317, 331, 337, 347, 349, 353, 359, 367, 373, 379, 383,
	389, 397, 401, 409, 419, 421, 431, 433, 439, 443, 449, 457, 461, 463, 467,
	479, 487, 491, 499, 503, 509, 521, 523, 541, 547, 557, 563, 569, 571, 577,
	587, 593, 599, 601, 607, 613, 617, 619, 631, 641, 643, 647, 653, 659, 661,
	673, 677, 683, 691, 701, 709, 719, 727, 733, 739, 743, 751, 757, 761, 769,
	773, 787, 797, 809, 811, 821, 823, 827, 829, 839, 853, 857, 859, 863, 877,
	881, 883, 887, 907, 911, 919, 929, 937, 941, 947, 953, 967, 971, 977, 983,
	991, 997, 1009, 1013, 1019, 1021, 1031, 1033, 1039, 1049, 1051, 1061, 1063, 1069,
	1087, 1091, 1093, 1097, 1103, 1109, 1117, 1123, 1129, 1151, 1153, 1163, 1171, 1181,
	1187, 1193, 1201, 1213, 1217, 1223, 1229, 1231, 1237, 1249, 1259, 1277, 1279, 1283,
	1289, 1291, 1297, 1301, 1303, 1307, 1319, 1321, 1327, 1361, 1367, 1373, 1381, 1399,
	1409, 1423, 1427, 1429, 1433, 1439, 1447, 1451, 1453, 1459, 1471, 1481, 1483, 1487,
	1489, 1493, 1499, 1511, 1523, 1531, 1543, 1549, 1553, 1559, 1567, 1571, 1579, 1583,
	1597, 1601, 1607, 1609, 1613, 1619,
}

type millerRabin struct {
	w *bigmod.Modulus
	m []byte
}

// millerRabinSetup prepares state that's reused across multiple iterations of
// the Miller-Rabin test.
//
// w must be a random integer equal to 3 mod 4 in big-endian order.
type __mrmillerRabinSetupPair struct {
	v0 *millerRabin
	v1 error
}

func __mrmillerRabinSetup(w []byte) __mrmillerRabinSetupPair {
	mr := &millerRabin{}

	// Check that w is 3 mod 4, and precompute Montgomery parameters.
	if len(w) == 0 || w[len(w)-1]&0b11 != 0b11 {
		return __mrmillerRabinSetupPair{nil, errors.New("candidate is not 3 mod 4")}
	}
	wm, err := bigmod.NewModulus(w)
	if err != nil {
		return __mrmillerRabinSetupPair{nil, err}
	}
	mr.w = wm

	// Compute m = (w-1)/2^a, where m is odd.
	// Since w is 3 mod 4, a is always 1, so m = (w-1)/2.
	m := mr.w.Nat().SubOne(mr.w).ShiftRightByOne()

	// Store mr.m as a big-endian byte slice with leading zero bytes removed,
	// for use with [bigmod.Nat.Exp].
	mr.m = m.Bytes(mr.w)
	for mr.m[0] == 0 {
		mr.m = mr.m[1:]
	}

	return __mrmillerRabinSetupPair{mr, nil}
}

func millerRabinSetup(w []byte) (*millerRabin, error) {
	panic("crypto/internal/fips140/rsa: millerRabinSetup: multi-result desugar")
}

const millerRabinCOMPOSITE = false
const millerRabinPOSSIBLYPRIME = true

type __mrmillerRabinIterationPair struct {
	v0 bool
	v1 error
}

func __mrmillerRabinIteration(mr *millerRabin, bb []byte) __mrmillerRabinIterationPair {
	// Reject b ≤ 1 or b ≥ w − 1.
	if len(bb) != (mr.w.BitLen()+7)/8 {
		return __mrmillerRabinIterationPair{false, errors.New("incorrect length")}
	}
	b := bigmod.NewNat()
	if _, err := b.SetBytes(bb, mr.w); err != nil {
		return __mrmillerRabinIterationPair{false, err}
	}
	if b.IsZero() == 1 || b.IsOne() == 1 || b.IsMinusOne(mr.w) == 1 {
		return __mrmillerRabinIterationPair{false, errors.New("out-of-range candidate")}
	}

	// Compute b^(m*2^i) mod w for successive i.
	// If b^m mod w = 1, b is a possible prime.
	// If b^(m*2^i) mod w = -1 for some 0 <= i < a, b is a possible prime.
	// Otherwise b is composite.

	// Since a = 1 for our w, we only need to check b^m mod w = +/-1.
	z := bigmod.NewNat().Exp(b, mr.m, mr.w)
	if z.IsOne() == 1 || z.IsMinusOne(mr.w) == 1 {
		return __mrmillerRabinIterationPair{millerRabinPOSSIBLYPRIME, nil}
	}

	return __mrmillerRabinIterationPair{millerRabinCOMPOSITE, nil}
}

func millerRabinIteration(mr *millerRabin, bb []byte) (bool, error) {
	panic("crypto/internal/fips140/rsa: millerRabinIteration: multi-result desugar")
}


// ---- vendored from crypto/internal/fips140/rsa/largeexponent.go ----




// TestingOnlyLargeExponentPublicKey is a variant of [PublicKey] that supports
// large public exponents. It is only meant for supporting the full ACVP test
// suite, which unfortunately forces us to choose between a fixed exponent and
// the full (2¹⁶, 2²⁵⁶) range. This type must not be used in production code,
// nor for e values < 2³¹, which instead must use [PublicKey].
type TestingOnlyLargeExponentPublicKey struct {
	N *bigmod.Modulus
	// E is the public exponent, represented as a big-endian byte slice.
	E []byte
}

func (pub *TestingOnlyLargeExponentPublicKey) Size() int {
	return (pub.N.BitLen() + 7) / 8
}

func checkLargeExponentPublicKey(pub *TestingOnlyLargeExponentPublicKey) error {
	if pub.N == nil {
		return errors.New("crypto/rsa: missing public modulus")
	}
	if pub.N.Nat().IsOdd() == 0 {
		return errors.New("crypto/rsa: public modulus is even")
	}
	if pub.N.BitLen() < 2048 {
		return errors.New("crypto/rsa: public modulus too small")
	}
	if pub.N.BitLen()%2 == 1 {
		return errors.New("crypto/rsa: public modulus bit length not even")
	}
	E := pub.E
	for len(E) > 0 && E[0] == 0 {
		E = E[1:]
	}
	if len(E) < 32/8 || (len(E) == 32/8 && E[0] < 0x80) {
		// Exponents less than 2^31 must use [PublicKey].
		return errors.New("crypto/rsa: public exponent too small")
	}
	if len(E) > 256/8 {
		return errors.New("crypto/rsa: public exponent too large")
	}
	if E[len(E)-1]&1 == 0 {
		return errors.New("crypto/rsa: public exponent is even")
	}
	return nil
}

type __mrencryptLargeExponentPair struct {
	v0 []byte
	v1 error
}

func __mrencryptLargeExponent(pub *TestingOnlyLargeExponentPublicKey, plaintext []byte) __mrencryptLargeExponentPair {
	m, err := bigmod.NewNat().SetBytes(plaintext, pub.N)
	if err != nil {
		return __mrencryptLargeExponentPair{nil, err}
	}
	return __mrencryptLargeExponentPair{bigmod.NewNat().Exp(m, pub.E, pub.N).Bytes(pub.N), nil}
}

func encryptLargeExponent(pub *TestingOnlyLargeExponentPublicKey, plaintext []byte) ([]byte, error) {
	panic("crypto/internal/fips140/rsa: encryptLargeExponent: multi-result desugar")
}

func TestingOnlyLargeExponentVerifyPKCS1v15(pub *TestingOnlyLargeExponentPublicKey, hash string, hashed []byte, sig []byte) error {
	fipsSelfTest()
	fips140.RecordApproved()
	checkApprovedHashName(hash)
	if err := checkLargeExponentPublicKey(pub); err != nil {
		return err
	}
	if pub.Size() != len(sig) {
		return ErrVerification
	}
	em, err := encryptLargeExponent(pub, sig)
	if err != nil {
		return ErrVerification
	}
	expected, err := pkcs1v15ConstructEM(&PublicKey{N: pub.N}, hash, hashed)
	if err != nil {
		return ErrVerification
	}
	if !bytes.Equal(em, expected) {
		return ErrVerification
	}
	return nil
}

func TestingOnlyLargeExponentVerifyPSS(pub *TestingOnlyLargeExponentPublicKey, hash hash.Hash, digest []byte, sig []byte) error {
	fipsSelfTest()
	fips140.RecordApproved()
	checkApprovedHash(hash)
	if err := checkLargeExponentPublicKey(pub); err != nil {
		return err
	}
	if len(sig) != pub.Size() {
		return ErrVerification
	}
	emBits := pub.N.BitLen() - 1
	emLen := (emBits + 7) / 8
	em, err := encryptLargeExponent(pub, sig)
	if err != nil {
		return ErrVerification
	}
	for len(em) > emLen && len(em) > 0 {
		if em[0] != 0 {
			return ErrVerification
		}
		em = em[1:]
	}
	return emsaPSSVerify(digest, em, emBits, pssSaltLengthAutodetect, hash)
}

// TestingOnlyLargeExponentPrivateKey is a variant of [PrivateKey] that supports
// large public exponents. It is only meant for supporting the full ACVP test
// suite. This type must not be used in production code.
type TestingOnlyLargeExponentPrivateKey struct {
	n    *bigmod.Modulus
	e    []byte // big-endian public exponent
	d    *bigmod.Nat
	p, q *bigmod.Modulus
	dP   []byte
	dQ   []byte
	qInv *bigmod.Nat
}

func (priv *TestingOnlyLargeExponentPrivateKey) Size() int {
	return (priv.n.BitLen() + 7) / 8
}

// TestingOnlyNewLargeExponentPrivateKeyWithPrecomputation creates a new RSA private key
// with a large public exponent from the given parameters. It is only meant for ACVP testing.
type __mrTestingOnlyNewLargeExponentPrivateKeyWithPrecomputationPair struct {
	v0 *TestingOnlyLargeExponentPrivateKey
	v1 error
}

func __mrTestingOnlyNewLargeExponentPrivateKeyWithPrecomputation(N []byte, e []byte, d, P, Q, dP, dQ, qInv []byte) __mrTestingOnlyNewLargeExponentPrivateKeyWithPrecomputationPair {
	n, err := bigmod.NewModulus(N)
	if err != nil {
		return __mrTestingOnlyNewLargeExponentPrivateKeyWithPrecomputationPair{nil, err}
	}
	p, err := bigmod.NewModulus(P)
	if err != nil {
		return __mrTestingOnlyNewLargeExponentPrivateKeyWithPrecomputationPair{nil, err}
	}
	q, err := bigmod.NewModulus(Q)
	if err != nil {
		return __mrTestingOnlyNewLargeExponentPrivateKeyWithPrecomputationPair{nil, err}
	}
	dN, err := bigmod.NewNat().SetBytes(d, n)
	if err != nil {
		return __mrTestingOnlyNewLargeExponentPrivateKeyWithPrecomputationPair{nil, err}
	}
	qInvNat, err := bigmod.NewNat().SetBytes(qInv, p)
	if err != nil {
		return __mrTestingOnlyNewLargeExponentPrivateKeyWithPrecomputationPair{nil, err}
	}

	priv := &TestingOnlyLargeExponentPrivateKey{
		n: n, e: e, d: dN, p: p, q: q,
		dP: dP, dQ: dQ, qInv: qInvNat,
	}
	if err := checkLargeExponentPrivateKey(priv); err != nil {
		return __mrTestingOnlyNewLargeExponentPrivateKeyWithPrecomputationPair{nil, err}
	}
	return __mrTestingOnlyNewLargeExponentPrivateKeyWithPrecomputationPair{priv, nil}
}

func TestingOnlyNewLargeExponentPrivateKeyWithPrecomputation(N []byte, e []byte, d, P, Q, dP, dQ, qInv []byte) (*TestingOnlyLargeExponentPrivateKey, error) {
	panic("crypto/internal/fips140/rsa: TestingOnlyNewLargeExponentPrivateKeyWithPrecomputation: multi-result desugar")
}

func checkLargeExponentPrivateKey(priv *TestingOnlyLargeExponentPrivateKey) error {
	// Check public key portion.
	pub := &TestingOnlyLargeExponentPublicKey{N: priv.n, E: priv.e}
	if err := checkLargeExponentPublicKey(pub); err != nil {
		return err
	}

	N := priv.n
	p := priv.p
	q := priv.q

	// FIPS 186-5, Section 5.1 requires "that p and q be of the same bit length."
	if p.BitLen() != q.BitLen() {
		// We don't enforce this for testing, just note it.
	}

	// Check that pq ≡ 1 mod N (and that p < N and q < N).
	pN := bigmod.NewNat().ExpandFor(N)
	if _, err := pN.SetBytes(p.Nat().Bytes(p), N); err != nil {
		return errors.New("crypto/rsa: invalid prime")
	}
	qN := bigmod.NewNat().ExpandFor(N)
	if _, err := qN.SetBytes(q.Nat().Bytes(q), N); err != nil {
		return errors.New("crypto/rsa: invalid prime")
	}
	if pN.Mul(qN, N).IsZero() != 1 {
		return errors.New("crypto/rsa: p * q != n")
	}

	// Check that de ≡ 1 mod p-1, and de ≡ 1 mod q-1.
	// Uses byte-slice exponent for large exponents.
	pMinus1, err := bigmod.NewModulus(p.Nat().SubOne(p).Bytes(p))
	if err != nil {
		return errors.New("crypto/rsa: invalid prime")
	}
	dP, err := bigmod.NewNat().SetBytes(priv.dP, pMinus1)
	if err != nil {
		return errors.New("crypto/rsa: invalid CRT exponent")
	}
	de := bigmod.NewNat()
	if _, err := de.SetBytes(priv.e, pMinus1); err != nil {
		// Exponent might be larger than p-1, reduce it.
		eNat, _ := bigmod.NewNat().SetBytes(priv.e, priv.n)
		de.Mod(eNat, pMinus1)
	}
	de.Mul(dP, pMinus1)
	if de.IsOne() != 1 {
		return errors.New("crypto/rsa: invalid CRT exponent")
	}

	qMinus1, err := bigmod.NewModulus(q.Nat().SubOne(q).Bytes(q))
	if err != nil {
		return errors.New("crypto/rsa: invalid prime")
	}
	dQ, err := bigmod.NewNat().SetBytes(priv.dQ, qMinus1)
	if err != nil {
		return errors.New("crypto/rsa: invalid CRT exponent")
	}
	if _, err := de.SetBytes(priv.e, qMinus1); err != nil {
		// Exponent might be larger than q-1, reduce it.
		eNat, _ := bigmod.NewNat().SetBytes(priv.e, priv.n)
		de.Mod(eNat, qMinus1)
	}
	de.Mul(dQ, qMinus1)
	if de.IsOne() != 1 {
		return errors.New("crypto/rsa: invalid CRT exponent")
	}

	// Check that qInv * q ≡ 1 mod p.
	qP, err := bigmod.NewNat().SetOverflowingBytes(q.Nat().Bytes(q), p)
	if err != nil {
		// q >= 2^⌈log2(p)⌉
		qP = bigmod.NewNat().Mod(q.Nat(), p)
	}
	if qP.Mul(priv.qInv, p).IsOne() != 1 {
		return errors.New("crypto/rsa: invalid CRT coefficient")
	}

	return nil
}

type __mrdecryptLargeExponentPair struct {
	v0 []byte
	v1 error
}

func __mrdecryptLargeExponent(priv *TestingOnlyLargeExponentPrivateKey, ciphertext []byte) __mrdecryptLargeExponentPair {
	N := priv.n
	c, err := bigmod.NewNat().SetBytes(ciphertext, N)
	if err != nil {
		return __mrdecryptLargeExponentPair{nil, ErrDecryption}
	}

	// CRT-based decryption (same as regular decrypt, doesn't use E).
	P, Q := priv.p, priv.q
	t0 := bigmod.NewNat()
	// m = c ^ Dp mod p
	m := bigmod.NewNat().Exp(t0.Mod(c, P), priv.dP, P)
	// m2 = c ^ Dq mod q
	m2 := bigmod.NewNat().Exp(t0.Mod(c, Q), priv.dQ, Q)
	// m = m - m2 mod p
	m.Sub(t0.Mod(m2, P), P)
	// m = m * Qinv mod p
	m.Mul(priv.qInv, P)
	// m = m * q mod N
	m.ExpandFor(N).Mul(t0.Mod(Q.Nat(), N), N)
	// m = m + m2 mod N
	m.Add(m2.ExpandFor(N), N)

	return __mrdecryptLargeExponentPair{m.Bytes(N), nil}
}

func decryptLargeExponent(priv *TestingOnlyLargeExponentPrivateKey, ciphertext []byte) ([]byte, error) {
	panic("crypto/internal/fips140/rsa: decryptLargeExponent: multi-result desugar")
}

// TestingOnlyLargeExponentDecryptOAEP decrypts ciphertext using RSAES-OAEP with
// a private key that has a large public exponent. It is only meant for ACVP testing.
type __mrTestingOnlyLargeExponentDecryptOAEPPair struct {
	v0 []byte
	v1 error
}

func __mrTestingOnlyLargeExponentDecryptOAEP(hash, mgfHash hash.Hash, priv *TestingOnlyLargeExponentPrivateKey, ciphertext []byte, label []byte) __mrTestingOnlyLargeExponentDecryptOAEPPair {
	fipsSelfTest()
	fips140.RecordApproved()
	checkApprovedHash(hash)

	k := priv.Size()
	if len(ciphertext) > k || k < hash.Size()*2+2 {
		return __mrTestingOnlyLargeExponentDecryptOAEPPair{nil, ErrDecryption}
	}

	em, err := decryptLargeExponent(priv, ciphertext)
	if err != nil {
		return __mrTestingOnlyLargeExponentDecryptOAEPPair{nil, err}
	}

	hash.Reset()
	hash.Write(label)
	lHash := hash.Sum(nil)

	firstByteIsZero := constanttime.ByteEq(em[0], 0)

	seed := em[1 : hash.Size()+1]
	db := em[hash.Size()+1:]

	mgf1XOR(seed, mgfHash, db)
	mgf1XOR(db, mgfHash, seed)

	lHash2 := db[0:hash.Size()]

	lHash2Good := subtle.ConstantTimeCompare(lHash, lHash2)

	var lookingForIndex, index, invalid int
	lookingForIndex = 1
	rest := db[hash.Size():]

	for i := 0; i < len(rest); i++ {
		equals0 := constanttime.ByteEq(rest[i], 0)
		equals1 := constanttime.ByteEq(rest[i], 1)
		index = constanttime.Select(lookingForIndex&equals1, i, index)
		lookingForIndex = constanttime.Select(equals1, 0, lookingForIndex)
		invalid = constanttime.Select(lookingForIndex&^equals0, 1, invalid)
	}

	if firstByteIsZero&lHash2Good&^invalid&^lookingForIndex != 1 {
		return __mrTestingOnlyLargeExponentDecryptOAEPPair{nil, ErrDecryption}
	}

	return __mrTestingOnlyLargeExponentDecryptOAEPPair{rest[index+1:], nil}
}

func TestingOnlyLargeExponentDecryptOAEP(hash, mgfHash hash.Hash, priv *TestingOnlyLargeExponentPrivateKey, ciphertext []byte, label []byte) ([]byte, error) {
	panic("crypto/internal/fips140/rsa: TestingOnlyLargeExponentDecryptOAEP: multi-result desugar")
}

// TestingOnlyLargeExponentEncryptOAEP encrypts the given message with RSAES-OAEP
// using a public key with a large exponent. It is only meant for ACVP testing.
type __mrTestingOnlyLargeExponentEncryptOAEPPair struct {
	v0 []byte
	v1 error
}

func __mrTestingOnlyLargeExponentEncryptOAEP(hash, mgfHash hash.Hash, random io.Reader, pub *TestingOnlyLargeExponentPublicKey, msg []byte, label []byte) __mrTestingOnlyLargeExponentEncryptOAEPPair {
	fipsSelfTest()
	fips140.RecordApproved()
	checkApprovedHash(hash)
	if err := checkLargeExponentPublicKey(pub); err != nil {
		return __mrTestingOnlyLargeExponentEncryptOAEPPair{nil, err}
	}
	k := pub.Size()
	if len(msg) > k-2*hash.Size()-2 {
		return __mrTestingOnlyLargeExponentEncryptOAEPPair{nil, ErrMessageTooLong}
	}

	hash.Reset()
	hash.Write(label)
	lHash := hash.Sum(nil)

	em := make([]byte, k)
	seed := em[1 : 1+hash.Size()]
	db := em[1+hash.Size():]

	copy(db[0:hash.Size()], lHash)
	db[len(db)-len(msg)-1] = 1
	copy(db[len(db)-len(msg):], msg)

	if err := drbg.ReadWithReader(random, seed); err != nil {
		return __mrTestingOnlyLargeExponentEncryptOAEPPair{nil, err}
	}

	mgf1XOR(db, mgfHash, seed)
	mgf1XOR(seed, mgfHash, db)

	__fw0, __fw1 := encryptLargeExponent(pub, em)
	return __mrTestingOnlyLargeExponentEncryptOAEPPair{__fw0, __fw1}
}

func TestingOnlyLargeExponentEncryptOAEP(hash, mgfHash hash.Hash, random io.Reader, pub *TestingOnlyLargeExponentPublicKey, msg []byte, label []byte) ([]byte, error) {
	panic("crypto/internal/fips140/rsa: TestingOnlyLargeExponentEncryptOAEP: multi-result desugar")
}


// ---- vendored from crypto/internal/fips140/rsa/pkcs1v15.go ----


// This file implements signing and verification using PKCS #1 v1.5 signatures.



// These are ASN1 DER structures:
//
//	DigestInfo ::= SEQUENCE {
//	  digestAlgorithm AlgorithmIdentifier,
//	  digest OCTET STRING
//	}
//
// For performance, we don't use the generic ASN1 encoder. Rather, we
// precompute a prefix of the digest value that makes a valid ASN1 DER string
// with the correct contents.
//
// For any new entry, also add the size to [hashSize], and if applicable, add
// the hash name to [checkApprovedHashName].
var hashPrefixes = map[string][]byte{
	"MD5":         {0x30, 0x20, 0x30, 0x0c, 0x06, 0x08, 0x2a, 0x86, 0x48, 0x86, 0xf7, 0x0d, 0x02, 0x05, 0x05, 0x00, 0x04, 0x10},
	"SHA-1":       {0x30, 0x21, 0x30, 0x09, 0x06, 0x05, 0x2b, 0x0e, 0x03, 0x02, 0x1a, 0x05, 0x00, 0x04, 0x14},
	"SHA-224":     {0x30, 0x2d, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x04, 0x05, 0x00, 0x04, 0x1c},
	"SHA-256":     {0x30, 0x31, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x01, 0x05, 0x00, 0x04, 0x20},
	"SHA-384":     {0x30, 0x41, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x02, 0x05, 0x00, 0x04, 0x30},
	"SHA-512":     {0x30, 0x51, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x03, 0x05, 0x00, 0x04, 0x40},
	"SHA-512/224": {0x30, 0x2d, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x05, 0x05, 0x00, 0x04, 0x1C},
	"SHA-512/256": {0x30, 0x31, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x06, 0x05, 0x00, 0x04, 0x20},
	"SHA3-224":    {0x30, 0x2d, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x07, 0x05, 0x00, 0x04, 0x1C},
	"SHA3-256":    {0x30, 0x31, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x08, 0x05, 0x00, 0x04, 0x20},
	"SHA3-384":    {0x30, 0x41, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x09, 0x05, 0x00, 0x04, 0x30},
	"SHA3-512":    {0x30, 0x51, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x0a, 0x05, 0x00, 0x04, 0x40},
	"MD5+SHA1":    {}, // A special TLS case which doesn't use an ASN1 prefix.
	"RIPEMD-160":  {0x30, 0x20, 0x30, 0x08, 0x06, 0x06, 0x28, 0xcf, 0x06, 0x03, 0x00, 0x31, 0x04, 0x14},
}

// SignPKCS1v15 calculates an RSASSA-PKCS1-v1.5 signature.
//
// hash is the name of the hash function as returned by [crypto.Hash.String]
// or the empty string to indicate that the message is signed directly.
type __mrSignPKCS1v15Pair struct {
	v0 []byte
	v1 error
}

func __mrSignPKCS1v15(priv *PrivateKey, hash string, hashed []byte) __mrSignPKCS1v15Pair {
	fipsSelfTest()
	fips140.RecordApproved()
	checkApprovedHashName(hash)

	__fw0, __fw1 := signPKCS1v15(priv, hash, hashed)
	return __mrSignPKCS1v15Pair{__fw0, __fw1}
}

func SignPKCS1v15(priv *PrivateKey, hash string, hashed []byte) ([]byte, error) {
	panic("crypto/internal/fips140/rsa: SignPKCS1v15: multi-result desugar")
}

type __mrsignPKCS1v15Pair struct {
	v0 []byte
	v1 error
}

func __mrsignPKCS1v15(priv *PrivateKey, hash string, hashed []byte) __mrsignPKCS1v15Pair {
	em, err := pkcs1v15ConstructEM(&priv.pub, hash, hashed)
	if err != nil {
		return __mrsignPKCS1v15Pair{nil, err}
	}

	__fw0, __fw1 := decrypt(priv, em, withCheck)
	return __mrsignPKCS1v15Pair{__fw0, __fw1}
}

func signPKCS1v15(priv *PrivateKey, hash string, hashed []byte) ([]byte, error) {
	panic("crypto/internal/fips140/rsa: signPKCS1v15: multi-result desugar")
}

type __mrpkcs1v15ConstructEMPair struct {
	v0 []byte
	v1 error
}

func __mrpkcs1v15ConstructEM(pub *PublicKey, hash string, hashed []byte) __mrpkcs1v15ConstructEMPair {
	// Special case: "" is used to indicate that the data is signed directly.
	var prefix []byte
	if hash != "" {
		var ok bool
		prefix, ok = hashPrefixes[hash]
		if !ok {
			return __mrpkcs1v15ConstructEMPair{nil, errors.New("crypto/rsa: unsupported hash function")}
		}
		if len(hashed) != hashSize(hash) {
			return __mrpkcs1v15ConstructEMPair{nil, errors.New("crypto/rsa: hashed message length does not match hash function")}
		}
	}

	// EM = 0x00 || 0x01 || PS || 0x00 || T
	k := pub.Size()
	if k < len(prefix)+len(hashed)+2+8+1 {
		return __mrpkcs1v15ConstructEMPair{nil, ErrMessageTooLong}
	}
	em := make([]byte, k)
	em[1] = 1
	for i := 2; i < k-len(prefix)-len(hashed)-1; i++ {
		em[i] = 0xff
	}
	copy(em[k-len(prefix)-len(hashed):], prefix)
	copy(em[k-len(hashed):], hashed)
	return __mrpkcs1v15ConstructEMPair{em, nil}
}

func pkcs1v15ConstructEM(pub *PublicKey, hash string, hashed []byte) ([]byte, error) {
	panic("crypto/internal/fips140/rsa: pkcs1v15ConstructEM: multi-result desugar")
}

// VerifyPKCS1v15 verifies an RSASSA-PKCS1-v1.5 signature.
//
// hash is the name of the hash function as returned by [crypto.Hash.String]
// or the empty string to indicate that the message is signed directly.
func VerifyPKCS1v15(pub *PublicKey, hash string, hashed []byte, sig []byte) error {
	fipsSelfTest()
	fips140.RecordApproved()
	checkApprovedHashName(hash)

	return verifyPKCS1v15(pub, hash, hashed, sig)
}

func verifyPKCS1v15(pub *PublicKey, hash string, hashed []byte, sig []byte) error {
	if fipsApproved, err := checkPublicKey(pub); err != nil {
		return err
	} else if !fipsApproved {
		fips140.RecordNonApproved()
	}

	// RFC 8017 Section 8.2.2: If the length of the signature S is not k
	// octets (where k is the length in octets of the RSA modulus n), output
	// "invalid signature" and stop.
	if pub.Size() != len(sig) {
		return ErrVerification
	}

	em, err := encrypt(pub, sig)
	if err != nil {
		return ErrVerification
	}

	expected, err := pkcs1v15ConstructEM(pub, hash, hashed)
	if err != nil {
		return ErrVerification
	}
	if !bytes.Equal(em, expected) {
		return ErrVerification
	}

	return nil
}

func hashSize(hash string) int {
	switch hash {
	case "MD5":
		return 16
	case "SHA-1", "RIPEMD-160":
		return 20
	case "SHA-224", "SHA-512/224", "SHA3-224":
		return 28
	case "SHA-256", "SHA-512/256", "SHA3-256":
		return 32
	case "SHA-384", "SHA3-384":
		return 48
	case "SHA-512", "SHA3-512":
		return 64
	case "MD5+SHA1":
		return 36
	default:
		return -1
	}
}

func checkApprovedHashName(hash string) {
	switch hash {
	case "SHA-224", "SHA-256", "SHA-384", "SHA-512", "SHA-512/224", "SHA-512/256",
		"SHA3-224", "SHA3-256", "SHA3-384", "SHA3-512":
	default:
		fips140.RecordNonApproved()
	}
}


// ---- vendored from crypto/internal/fips140/rsa/pkcs1v22.go ----


// This file implements the RSASSA-PSS signature scheme and the RSAES-OAEP
// encryption scheme according to RFC 8017, aka PKCS #1 v2.2.



// Per RFC 8017, Section 9.1
//
//     EM = MGF1 xor DB || H( 8*0x00 || mHash || salt ) || 0xbc
//
// where
//
//     DB = PS || 0x01 || salt
//
// and PS can be empty so
//
//     emLen = dbLen + hLen + 1 = psLen + sLen + hLen + 2
//

// incCounter increments a four byte, big-endian counter.
func incCounter(c *[4]byte) {
	if c[3]++; c[3] != 0 {
		return
	}
	if c[2]++; c[2] != 0 {
		return
	}
	if c[1]++; c[1] != 0 {
		return
	}
	c[0]++
}

// mgf1XOR XORs the bytes in out with a mask generated using the MGF1 function
// specified in PKCS #1 v2.1.
func mgf1XOR(out []byte, hash hash.Hash, seed []byte) {
	var counter [4]byte
	var digest []byte

	done := 0
	for done < len(out) {
		hash.Reset()
		hash.Write(seed)
		hash.Write(counter[0:4])
		digest = hash.Sum(digest[:0])

		for i := 0; i < len(digest) && done < len(out); i++ {
			out[done] ^= digest[i]
			done++
		}
		incCounter(&counter)
	}
}

type __mremsaPSSEncodePair struct {
	v0 []byte
	v1 error
}

func __mremsaPSSEncode(mHash []byte, emBits int, salt []byte, hash hash.Hash) __mremsaPSSEncodePair {
	// See RFC 8017, Section 9.1.1.

	hLen := hash.Size()
	sLen := len(salt)
	emLen := (emBits + 7) / 8

	// 1.  If the length of M is greater than the input limitation for the
	//     hash function (2^61 - 1 octets for SHA-1), output "message too
	//     long" and stop.
	//
	// 2.  Let mHash = Hash(M), an octet string of length hLen.

	if len(mHash) != hLen {
		return __mremsaPSSEncodePair{nil, errors.New("crypto/rsa: input must be hashed with given hash")}
	}

	// 3.  If emLen < hLen + sLen + 2, output "encoding error" and stop.

	if emLen < hLen+sLen+2 {
		return __mremsaPSSEncodePair{nil, ErrMessageTooLong}
	}

	em := make([]byte, emLen)
	psLen := emLen - sLen - hLen - 2
	db := em[:psLen+1+sLen]
	h := em[psLen+1+sLen : emLen-1]

	// 4.  Generate a random octet string salt of length sLen; if sLen = 0,
	//     then salt is the empty string.
	//
	// 5.  Let
	//       M' = (0x)00 00 00 00 00 00 00 00 || mHash || salt;
	//
	//     M' is an octet string of length 8 + hLen + sLen with eight
	//     initial zero octets.
	//
	// 6.  Let H = Hash(M'), an octet string of length hLen.

	var prefix [8]byte

	hash.Reset()
	hash.Write(prefix[:])
	hash.Write(mHash)
	hash.Write(salt)

	h = hash.Sum(h[:0])

	// 7.  Generate an octet string PS consisting of emLen - sLen - hLen - 2
	//     zero octets. The length of PS may be 0.
	//
	// 8.  Let DB = PS || 0x01 || salt; DB is an octet string of length
	//     emLen - hLen - 1.

	db[psLen] = 0x01
	copy(db[psLen+1:], salt)

	// 9.  Let dbMask = MGF(H, emLen - hLen - 1).
	//
	// 10. Let maskedDB = DB \xor dbMask.

	mgf1XOR(db, hash, h)

	// 11. Set the leftmost 8 * emLen - emBits bits of the leftmost octet in
	//     maskedDB to zero.

	db[0] &= 0xff >> (8*emLen - emBits)

	// 12. Let EM = maskedDB || H || 0xbc.
	em[emLen-1] = 0xbc

	// 13. Output EM.
	return __mremsaPSSEncodePair{em, nil}
}

func emsaPSSEncode(mHash []byte, emBits int, salt []byte, hash hash.Hash) ([]byte, error) {
	panic("crypto/internal/fips140/rsa: emsaPSSEncode: multi-result desugar")
}

const pssSaltLengthAutodetect = -1

func emsaPSSVerify(mHash, em []byte, emBits, sLen int, hash hash.Hash) error {
	// See RFC 8017, Section 9.1.2.

	hLen := hash.Size()
	emLen := (emBits + 7) / 8
	if emLen != len(em) {
		return errors.New("rsa: internal error: inconsistent length")
	}

	// 1.  If the length of M is greater than the input limitation for the
	//     hash function (2^61 - 1 octets for SHA-1), output "inconsistent"
	//     and stop.
	//
	// 2.  Let mHash = Hash(M), an octet string of length hLen.
	if hLen != len(mHash) {
		return ErrVerification
	}

	// 3.  If emLen < hLen + sLen + 2, output "inconsistent" and stop.
	if emLen < hLen+sLen+2 {
		return ErrVerification
	}

	// 4.  If the rightmost octet of EM does not have hexadecimal value
	//     0xbc, output "inconsistent" and stop.
	if em[emLen-1] != 0xbc {
		return ErrVerification
	}

	// 5.  Let maskedDB be the leftmost emLen - hLen - 1 octets of EM, and
	//     let H be the next hLen octets.
	db := em[:emLen-hLen-1]
	h := em[emLen-hLen-1 : emLen-1]

	// 6.  If the leftmost 8 * emLen - emBits bits of the leftmost octet in
	//     maskedDB are not all equal to zero, output "inconsistent" and
	//     stop.
	var bitMask byte = 0xff >> (8*emLen - emBits)
	if em[0] & ^bitMask != 0 {
		return ErrVerification
	}

	// 7.  Let dbMask = MGF(H, emLen - hLen - 1).
	//
	// 8.  Let DB = maskedDB \xor dbMask.
	mgf1XOR(db, hash, h)

	// 9.  Set the leftmost 8 * emLen - emBits bits of the leftmost octet in DB
	//     to zero.
	db[0] &= bitMask

	// If we don't know the salt length, look for the 0x01 delimiter.
	if sLen == pssSaltLengthAutodetect {
		psLen := bytes.IndexByte(db, 0x01)
		if psLen < 0 {
			return ErrVerification
		}
		sLen = len(db) - psLen - 1
	}

	// FIPS 186-5, Section 5.4(g): "the length (in bytes) of the salt (sLen)
	// shall satisfy 0 ≤ sLen ≤ hLen".
	if sLen > hLen {
		fips140.RecordNonApproved()
	}

	// 10. If the emLen - hLen - sLen - 2 leftmost octets of DB are not zero
	//     or if the octet at position emLen - hLen - sLen - 1 (the leftmost
	//     position is "position 1") does not have hexadecimal value 0x01,
	//     output "inconsistent" and stop.
	psLen := emLen - hLen - sLen - 2
	for _, e := range db[:psLen] {
		if e != 0x00 {
			return ErrVerification
		}
	}
	if db[psLen] != 0x01 {
		return ErrVerification
	}

	// 11.  Let salt be the last sLen octets of DB.
	salt := db[len(db)-sLen:]

	// 12.  Let
	//          M' = (0x)00 00 00 00 00 00 00 00 || mHash || salt ;
	//     M' is an octet string of length 8 + hLen + sLen with eight
	//     initial zero octets.
	//
	// 13. Let H' = Hash(M'), an octet string of length hLen.
	hash.Reset()
	var prefix [8]byte
	hash.Write(prefix[:])
	hash.Write(mHash)
	hash.Write(salt)

	h0 := hash.Sum(nil)

	// 14. If H = H', output "consistent." Otherwise, output "inconsistent."
	if !bytes.Equal(h0, h) {
		return ErrVerification
	}
	return nil
}

// PSSMaxSaltLength returns the maximum salt length for a given public key and
// hash function.
type __mrPSSMaxSaltLengthPair struct {
	v0 int
	v1 error
}

func __mrPSSMaxSaltLength(pub *PublicKey, hash hash.Hash) __mrPSSMaxSaltLengthPair {
	saltLength := (pub.N.BitLen()-1+7)/8 - 2 - hash.Size()
	if saltLength < 0 {
		return __mrPSSMaxSaltLengthPair{0, ErrMessageTooLong}
	}
	// FIPS 186-5, Section 5.4(g): "the length (in bytes) of the salt (sLen)
	// shall satisfy 0 ≤ sLen ≤ hLen".
	if fips140.Enabled && saltLength > hash.Size() {
		return __mrPSSMaxSaltLengthPair{hash.Size(), nil}
	}
	return __mrPSSMaxSaltLengthPair{saltLength, nil}
}

func PSSMaxSaltLength(pub *PublicKey, hash hash.Hash) (int, error) {
	panic("crypto/internal/fips140/rsa: PSSMaxSaltLength: multi-result desugar")
}

// SignPSS calculates the signature of hashed using RSASSA-PSS.
type __mrSignPSSPair struct {
	v0 []byte
	v1 error
}

func __mrSignPSS(rand io.Reader, priv *PrivateKey, hash hash.Hash, hashed []byte, saltLength int) __mrSignPSSPair {
	fipsSelfTest()
	fips140.RecordApproved()
	checkApprovedHash(hash)

	// Note that while we don't commit to deterministic execution with respect
	// to the rand stream, we also never applied MaybeReadByte, so per Hyrum's
	// Law it's probably relied upon by some. It's a tolerable promise because a
	// well-specified number of random bytes is included in the signature, in a
	// well-specified way.

	if saltLength < 0 {
		return __mrSignPSSPair{nil, errors.New("crypto/rsa: salt length cannot be negative")}
	}
	// FIPS 186-5, Section 5.4(g): "the length (in bytes) of the salt (sLen)
	// shall satisfy 0 ≤ sLen ≤ hLen".
	if saltLength > hash.Size() {
		fips140.RecordNonApproved()
	}
	salt := make([]byte, saltLength)
	if err := drbg.ReadWithReader(rand, salt); err != nil {
		return __mrSignPSSPair{nil, err}
	}

	emBits := priv.pub.N.BitLen() - 1
	em, err := emsaPSSEncode(hashed, emBits, salt, hash)
	if err != nil {
		return __mrSignPSSPair{nil, err}
	}

	// RFC 8017: "Note that the octet length of EM will be one less than k if
	// modBits - 1 is divisible by 8 and equal to k otherwise, where k is the
	// length in octets of the RSA modulus n." 🙄
	//
	// This is extremely annoying, as all other encrypt and decrypt inputs are
	// always the exact same size as the modulus. Since it only happens for
	// weird modulus sizes, fix it by padding inefficiently.
	if emLen, k := len(em), priv.pub.Size(); emLen < k {
		emNew := make([]byte, k)
		copy(emNew[k-emLen:], em)
		em = emNew
	}

	__fw0, __fw1 := decrypt(priv, em, withCheck)
	return __mrSignPSSPair{__fw0, __fw1}
}

func SignPSS(rand io.Reader, priv *PrivateKey, hash hash.Hash, hashed []byte, saltLength int) ([]byte, error) {
	panic("crypto/internal/fips140/rsa: SignPSS: multi-result desugar")
}

// VerifyPSS verifies sig with RSASSA-PSS automatically detecting the salt length.
func VerifyPSS(pub *PublicKey, hash hash.Hash, digest []byte, sig []byte) error {
	return verifyPSS(pub, hash, digest, sig, pssSaltLengthAutodetect)
}

// VerifyPSSWithSaltLength verifies sig with RSASSA-PSS and an expected salt length.
func VerifyPSSWithSaltLength(pub *PublicKey, hash hash.Hash, digest []byte, sig []byte, saltLength int) error {
	if saltLength < 0 {
		return errors.New("crypto/rsa: salt length cannot be negative")
	}
	return verifyPSS(pub, hash, digest, sig, saltLength)
}

func verifyPSS(pub *PublicKey, hash hash.Hash, digest []byte, sig []byte, saltLength int) error {
	fipsSelfTest()
	fips140.RecordApproved()
	checkApprovedHash(hash)
	if fipsApproved, err := checkPublicKey(pub); err != nil {
		return err
	} else if !fipsApproved {
		fips140.RecordNonApproved()
	}

	if len(sig) != pub.Size() {
		return ErrVerification
	}

	emBits := pub.N.BitLen() - 1
	emLen := (emBits + 7) / 8
	em, err := encrypt(pub, sig)
	if err != nil {
		return ErrVerification
	}

	// Like in signPSSWithSalt, deal with mismatches between emLen and the size
	// of the modulus. The spec would have us wire emLen into the encoding
	// function, but we'd rather always encode to the size of the modulus and
	// then strip leading zeroes if necessary. This only happens for weird
	// modulus sizes anyway.
	for len(em) > emLen && len(em) > 0 {
		if em[0] != 0 {
			return ErrVerification
		}
		em = em[1:]
	}

	return emsaPSSVerify(digest, em, emBits, saltLength, hash)
}

func checkApprovedHash(hash hash.Hash) {
	switch hash.(type) {
	case *sha256.Digest, *sha512.Digest, *sha3.Digest:
	default:
		fips140.RecordNonApproved()
	}
}

// EncryptOAEP encrypts the given message with RSAES-OAEP.
type __mrEncryptOAEPPair struct {
	v0 []byte
	v1 error
}

func __mrEncryptOAEP(hash, mgfHash hash.Hash, random io.Reader, pub *PublicKey, msg []byte, label []byte) __mrEncryptOAEPPair {
	// Note that while we don't commit to deterministic execution with respect
	// to the random stream, we also never applied MaybeReadByte, so per Hyrum's
	// Law it's probably relied upon by some. It's a tolerable promise because a
	// well-specified number of random bytes is included in the ciphertext, in a
	// well-specified way.

	fipsSelfTest()
	fips140.RecordApproved()
	checkApprovedHash(hash)
	if fipsApproved, err := checkPublicKey(pub); err != nil {
		return __mrEncryptOAEPPair{nil, err}
	} else if !fipsApproved {
		fips140.RecordNonApproved()
	}
	k := pub.Size()
	if len(msg) > k-2*hash.Size()-2 {
		return __mrEncryptOAEPPair{nil, ErrMessageTooLong}
	}

	hash.Reset()
	hash.Write(label)
	lHash := hash.Sum(nil)

	em := make([]byte, k)
	seed := em[1 : 1+hash.Size()]
	db := em[1+hash.Size():]

	copy(db[0:hash.Size()], lHash)
	db[len(db)-len(msg)-1] = 1
	copy(db[len(db)-len(msg):], msg)

	if err := drbg.ReadWithReader(random, seed); err != nil {
		return __mrEncryptOAEPPair{nil, err}
	}

	mgf1XOR(db, mgfHash, seed)
	mgf1XOR(seed, mgfHash, db)

	__fw0, __fw1 := encrypt(pub, em)
	return __mrEncryptOAEPPair{__fw0, __fw1}
}

func EncryptOAEP(hash, mgfHash hash.Hash, random io.Reader, pub *PublicKey, msg []byte, label []byte) ([]byte, error) {
	panic("crypto/internal/fips140/rsa: EncryptOAEP: multi-result desugar")
}

// DecryptOAEP decrypts ciphertext using RSAES-OAEP.
type __mrDecryptOAEPPair struct {
	v0 []byte
	v1 error
}

func __mrDecryptOAEP(hash, mgfHash hash.Hash, priv *PrivateKey, ciphertext []byte, label []byte) __mrDecryptOAEPPair {
	fipsSelfTest()
	fips140.RecordApproved()
	checkApprovedHash(hash)

	k := priv.pub.Size()
	if len(ciphertext) > k ||
		k < hash.Size()*2+2 {
		return __mrDecryptOAEPPair{nil, ErrDecryption}
	}

	em, err := decrypt(priv, ciphertext, noCheck)
	if err != nil {
		return __mrDecryptOAEPPair{nil, err}
	}

	hash.Reset()
	hash.Write(label)
	lHash := hash.Sum(nil)

	firstByteIsZero := constanttime.ByteEq(em[0], 0)

	seed := em[1 : hash.Size()+1]
	db := em[hash.Size()+1:]

	mgf1XOR(seed, mgfHash, db)
	mgf1XOR(db, mgfHash, seed)

	lHash2 := db[0:hash.Size()]

	// We have to validate the plaintext in constant time in order to avoid
	// attacks like: J. Manger. A Chosen Ciphertext Attack on RSA Optimal
	// Asymmetric Encryption Padding (OAEP) as Standardized in PKCS #1
	// v2.0. In J. Kilian, editor, Advances in Cryptology.
	lHash2Good := subtle.ConstantTimeCompare(lHash, lHash2)

	// The remainder of the plaintext must be zero or more 0x00, followed
	// by 0x01, followed by the message.
	//   lookingForIndex: 1 iff we are still looking for the 0x01
	//   index: the offset of the first 0x01 byte
	//   invalid: 1 iff we saw a non-zero byte before the 0x01.
	var lookingForIndex, index, invalid int
	lookingForIndex = 1
	rest := db[hash.Size():]

	for i := 0; i < len(rest); i++ {
		equals0 := constanttime.ByteEq(rest[i], 0)
		equals1 := constanttime.ByteEq(rest[i], 1)
		index = constanttime.Select(lookingForIndex&equals1, i, index)
		lookingForIndex = constanttime.Select(equals1, 0, lookingForIndex)
		invalid = constanttime.Select(lookingForIndex&^equals0, 1, invalid)
	}

	if firstByteIsZero&lHash2Good&^invalid&^lookingForIndex != 1 {
		return __mrDecryptOAEPPair{nil, ErrDecryption}
	}

	return __mrDecryptOAEPPair{rest[index+1:], nil}
}

func DecryptOAEP(hash, mgfHash hash.Hash, priv *PrivateKey, ciphertext []byte, label []byte) ([]byte, error) {
	panic("crypto/internal/fips140/rsa: DecryptOAEP: multi-result desugar")
}


// ---- synthetic additions ----

// P3-M9: FIPS self-tests are no-ops in default (non-FIPS) mode.
var fipsSelfTest = func() {}
