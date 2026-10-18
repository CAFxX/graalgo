// SYNTHETIC (Principle 4) — crypto/internal/boring: synthetic-only support package (not in the user import allow-list); exists to serve the synthetic packages importing it.
// RETIRES WHEN: the synthetic packages importing it retire — then delete this file.
// RE-TEST TRIGGER: re-evaluate when its dependents retire (B-4 phased removal: docs/arch/bridge-inventory.md).
package main

// P3-M9 tier-5: synthetic 'crypto/internal/boring' package.
const syntheticCryptoBoringSource = `// P3-M9 tier-5: synthetic 'crypto/internal/boring' (notboring leg).
// go1.27.1 vendored notboring.go (boringcrypto build tag unsatisfied).
// DEVIATION (documented): crypto/internal/boring/sig import dropped —
// sig is assembly-only; Unreachable() is a no-op here (it is never
// called on live paths in the vendored set). Synthetic-only.
package boring

import (
	"crypto"
	"crypto/cipher"
	"hash"
)


// ---- vendored from crypto/internal/boring/notboring.go ----




const available = false

// Enabled reports whether BoringCrypto is available.
const Enabled = available

// Unreachable marks code that should be unreachable
// when BoringCrypto is in use. It is a no-op without BoringCrypto.
func Unreachable() {
	// Code that's unreachable when using BoringCrypto
	// is exactly the code we want to detect for reporting
	// standard Go crypto.
	// P3-M9: sig.StandardCrypto() is assembly-only; no-op here.
}

// UnreachableExceptTests marks code that should be unreachable
// when BoringCrypto is in use. It is a no-op without BoringCrypto.
func UnreachableExceptTests() {}

type randReader int

type __mrrandReader_ReadPair struct {
	v0 int
	v1 error
}

func __mrrandReader_Read(randReader, b []byte) __mrrandReader_ReadPair { panic("boringcrypto: not available") }

func (randReader) Read(b []byte) (int, error) {
	panic("crypto/internal/boring: Read: multi-result desugar")
}

const RandReader = randReader(0)

func NewSHA1() hash.Hash   { panic("boringcrypto: not available") }
func NewSHA224() hash.Hash { panic("boringcrypto: not available") }
func NewSHA256() hash.Hash { panic("boringcrypto: not available") }
func NewSHA384() hash.Hash { panic("boringcrypto: not available") }
func NewSHA512() hash.Hash { panic("boringcrypto: not available") }

func SHA1([]byte) [20]byte   { panic("boringcrypto: not available") }
func SHA224([]byte) [28]byte { panic("boringcrypto: not available") }
func SHA256([]byte) [32]byte { panic("boringcrypto: not available") }
func SHA384([]byte) [48]byte { panic("boringcrypto: not available") }
func SHA512([]byte) [64]byte { panic("boringcrypto: not available") }

func NewHMAC(h func() hash.Hash, key []byte) hash.Hash { panic("boringcrypto: not available") }

type __mrNewAESCipherPair struct {
	v0 cipher.Block
	v1 error
}

func __mrNewAESCipher(key []byte) __mrNewAESCipherPair { panic("boringcrypto: not available") }

func NewAESCipher(key []byte) (cipher.Block, error) {
	panic("crypto/internal/boring: NewAESCipher: multi-result desugar")
}
type __mrNewGCMTLSPair struct {
	v0 cipher.AEAD
	v1 error
}

func __mrNewGCMTLS(cipher.Block) __mrNewGCMTLSPair { panic("boringcrypto: not available") }

func NewGCMTLS(cipher.Block) (cipher.AEAD, error) {
	panic("crypto/internal/boring: NewGCMTLS: multi-result desugar")
}
type __mrNewGCMTLS13Pair struct {
	v0 cipher.AEAD
	v1 error
}

func __mrNewGCMTLS13(cipher.Block) __mrNewGCMTLS13Pair { panic("boringcrypto: not available") }

func NewGCMTLS13(cipher.Block) (cipher.AEAD, error) {
	panic("crypto/internal/boring: NewGCMTLS13: multi-result desugar")
}

type PublicKeyECDSA struct{ _ int }
type PrivateKeyECDSA struct{ _ int }

type __mrGenerateKeyECDSAPair struct {
	X BigInt
	Y BigInt
	D BigInt
	err error
}

func __mrGenerateKeyECDSA(curve string) __mrGenerateKeyECDSAPair {
	panic("boringcrypto: not available")
}

func GenerateKeyECDSA(curve string) (X, Y, D BigInt, err error) {
	panic("crypto/internal/boring: GenerateKeyECDSA: multi-result desugar")
}
type __mrNewPrivateKeyECDSAPair struct {
	v0 *PrivateKeyECDSA
	v1 error
}

func __mrNewPrivateKeyECDSA(curve string, X, Y, D BigInt) __mrNewPrivateKeyECDSAPair {
	panic("boringcrypto: not available")
}

func NewPrivateKeyECDSA(curve string, X, Y, D BigInt) (*PrivateKeyECDSA, error) {
	panic("crypto/internal/boring: NewPrivateKeyECDSA: multi-result desugar")
}
type __mrNewPublicKeyECDSAPair struct {
	v0 *PublicKeyECDSA
	v1 error
}

func __mrNewPublicKeyECDSA(curve string, X, Y BigInt) __mrNewPublicKeyECDSAPair {
	panic("boringcrypto: not available")
}

func NewPublicKeyECDSA(curve string, X, Y BigInt) (*PublicKeyECDSA, error) {
	panic("crypto/internal/boring: NewPublicKeyECDSA: multi-result desugar")
}
type __mrSignMarshalECDSAPair struct {
	v0 []byte
	v1 error
}

func __mrSignMarshalECDSA(priv *PrivateKeyECDSA, hash []byte) __mrSignMarshalECDSAPair {
	panic("boringcrypto: not available")
}

func SignMarshalECDSA(priv *PrivateKeyECDSA, hash []byte) ([]byte, error) {
	panic("crypto/internal/boring: SignMarshalECDSA: multi-result desugar")
}
func VerifyECDSA(pub *PublicKeyECDSA, hash []byte, sig []byte) bool {
	panic("boringcrypto: not available")
}

type PublicKeyRSA struct{ _ int }
type PrivateKeyRSA struct{ _ int }

type __mrDecryptRSAOAEPPair struct {
	v0 []byte
	v1 error
}

func __mrDecryptRSAOAEP(h, mgfHash hash.Hash, priv *PrivateKeyRSA, ciphertext, label []byte) __mrDecryptRSAOAEPPair {
	panic("boringcrypto: not available")
}

func DecryptRSAOAEP(h, mgfHash hash.Hash, priv *PrivateKeyRSA, ciphertext, label []byte) ([]byte, error) {
	panic("crypto/internal/boring: DecryptRSAOAEP: multi-result desugar")
}
type __mrDecryptRSAPKCS1Pair struct {
	v0 []byte
	v1 error
}

func __mrDecryptRSAPKCS1(priv *PrivateKeyRSA, ciphertext []byte) __mrDecryptRSAPKCS1Pair {
	panic("boringcrypto: not available")
}

func DecryptRSAPKCS1(priv *PrivateKeyRSA, ciphertext []byte) ([]byte, error) {
	panic("crypto/internal/boring: DecryptRSAPKCS1: multi-result desugar")
}
type __mrDecryptRSANoPaddingPair struct {
	v0 []byte
	v1 error
}

func __mrDecryptRSANoPadding(priv *PrivateKeyRSA, ciphertext []byte) __mrDecryptRSANoPaddingPair {
	panic("boringcrypto: not available")
}

func DecryptRSANoPadding(priv *PrivateKeyRSA, ciphertext []byte) ([]byte, error) {
	panic("crypto/internal/boring: DecryptRSANoPadding: multi-result desugar")
}
type __mrEncryptRSAOAEPPair struct {
	v0 []byte
	v1 error
}

func __mrEncryptRSAOAEP(h, mgfHash hash.Hash, pub *PublicKeyRSA, msg, label []byte) __mrEncryptRSAOAEPPair {
	panic("boringcrypto: not available")
}

func EncryptRSAOAEP(h, mgfHash hash.Hash, pub *PublicKeyRSA, msg, label []byte) ([]byte, error) {
	panic("crypto/internal/boring: EncryptRSAOAEP: multi-result desugar")
}
type __mrEncryptRSAPKCS1Pair struct {
	v0 []byte
	v1 error
}

func __mrEncryptRSAPKCS1(pub *PublicKeyRSA, msg []byte) __mrEncryptRSAPKCS1Pair {
	panic("boringcrypto: not available")
}

func EncryptRSAPKCS1(pub *PublicKeyRSA, msg []byte) ([]byte, error) {
	panic("crypto/internal/boring: EncryptRSAPKCS1: multi-result desugar")
}
type __mrEncryptRSANoPaddingPair struct {
	v0 []byte
	v1 error
}

func __mrEncryptRSANoPadding(pub *PublicKeyRSA, msg []byte) __mrEncryptRSANoPaddingPair {
	panic("boringcrypto: not available")
}

func EncryptRSANoPadding(pub *PublicKeyRSA, msg []byte) ([]byte, error) {
	panic("crypto/internal/boring: EncryptRSANoPadding: multi-result desugar")
}
type __mrGenerateKeyRSAPair struct {
	N BigInt
	E BigInt
	D BigInt
	P BigInt
	Q BigInt
	Dp BigInt
	Dq BigInt
	Qinv BigInt
	err error
}

func __mrGenerateKeyRSA(bits int) __mrGenerateKeyRSAPair {
	panic("boringcrypto: not available")
}

func GenerateKeyRSA(bits int) (N, E, D, P, Q, Dp, Dq, Qinv BigInt, err error) {
	panic("crypto/internal/boring: GenerateKeyRSA: multi-result desugar")
}
type __mrNewPrivateKeyRSAPair struct {
	v0 *PrivateKeyRSA
	v1 error
}

func __mrNewPrivateKeyRSA(N, E, D, P, Q, Dp, Dq, Qinv BigInt) __mrNewPrivateKeyRSAPair {
	panic("boringcrypto: not available")
}

func NewPrivateKeyRSA(N, E, D, P, Q, Dp, Dq, Qinv BigInt) (*PrivateKeyRSA, error) {
	panic("crypto/internal/boring: NewPrivateKeyRSA: multi-result desugar")
}
type __mrNewPublicKeyRSAPair struct {
	v0 *PublicKeyRSA
	v1 error
}

func __mrNewPublicKeyRSA(N, E BigInt) __mrNewPublicKeyRSAPair { panic("boringcrypto: not available") }

func NewPublicKeyRSA(N, E BigInt) (*PublicKeyRSA, error) {
	panic("crypto/internal/boring: NewPublicKeyRSA: multi-result desugar")
}
type __mrSignRSAPKCS1v15Pair struct {
	v0 []byte
	v1 error
}

func __mrSignRSAPKCS1v15(priv *PrivateKeyRSA, h crypto.Hash, hashed []byte) __mrSignRSAPKCS1v15Pair {
	panic("boringcrypto: not available")
}

func SignRSAPKCS1v15(priv *PrivateKeyRSA, h crypto.Hash, hashed []byte) ([]byte, error) {
	panic("crypto/internal/boring: SignRSAPKCS1v15: multi-result desugar")
}
type __mrSignRSAPSSPair struct {
	v0 []byte
	v1 error
}

func __mrSignRSAPSS(priv *PrivateKeyRSA, h crypto.Hash, hashed []byte, saltLen int) __mrSignRSAPSSPair {
	panic("boringcrypto: not available")
}

func SignRSAPSS(priv *PrivateKeyRSA, h crypto.Hash, hashed []byte, saltLen int) ([]byte, error) {
	panic("crypto/internal/boring: SignRSAPSS: multi-result desugar")
}
func VerifyRSAPKCS1v15(pub *PublicKeyRSA, h crypto.Hash, hashed, sig []byte) error {
	panic("boringcrypto: not available")
}
func VerifyRSAPSS(pub *PublicKeyRSA, h crypto.Hash, hashed, sig []byte, saltLen int) error {
	panic("boringcrypto: not available")
}

type PublicKeyECDH struct{}
type PrivateKeyECDH struct{}

type __mrECDHPair struct {
	v0 []byte
	v1 error
}

func __mrECDH(*PrivateKeyECDH, *PublicKeyECDH) __mrECDHPair { panic("boringcrypto: not available") }

func ECDH(*PrivateKeyECDH, *PublicKeyECDH) ([]byte, error) {
	panic("crypto/internal/boring: ECDH: multi-result desugar")
}
type __mrGenerateKeyECDHPair struct {
	v0 *PrivateKeyECDH
	v1 []byte
	v2 error
}

func __mrGenerateKeyECDH(string) __mrGenerateKeyECDHPair { panic("boringcrypto: not available") }

func GenerateKeyECDH(string) (*PrivateKeyECDH, []byte, error) {
	panic("crypto/internal/boring: GenerateKeyECDH: multi-result desugar")
}
type __mrNewPrivateKeyECDHPair struct {
	v0 *PrivateKeyECDH
	v1 error
}

func __mrNewPrivateKeyECDH(string, []byte) __mrNewPrivateKeyECDHPair { panic("boringcrypto: not available") }

func NewPrivateKeyECDH(string, []byte) (*PrivateKeyECDH, error) {
	panic("crypto/internal/boring: NewPrivateKeyECDH: multi-result desugar")
}
type __mrNewPublicKeyECDHPair struct {
	v0 *PublicKeyECDH
	v1 error
}

func __mrNewPublicKeyECDH(string, []byte) __mrNewPublicKeyECDHPair { panic("boringcrypto: not available") }

func NewPublicKeyECDH(string, []byte) (*PublicKeyECDH, error) {
	panic("crypto/internal/boring: NewPublicKeyECDH: multi-result desugar")
}
func (*PublicKeyECDH) Bytes() []byte                            { panic("boringcrypto: not available") }
type __mrPrivateKeyECDH_PublicKeyPair struct {
	v0 *PublicKeyECDH
	v1 error
}

func __mrPrivateKeyECDH_PublicKey(*PrivateKeyECDH) __mrPrivateKeyECDH_PublicKeyPair { panic("boringcrypto: not available") }

func (*PrivateKeyECDH) PublicKey() (*PublicKeyECDH, error) {
	panic("crypto/internal/boring: PublicKey: multi-result desugar")
}

// P3-M9: BigInt type from crypto/internal/boring/doc.go (not vendored
// separately). This definition avoids importing math/big.
type BigInt []uint

`
