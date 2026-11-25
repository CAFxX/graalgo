package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"hash/crc32"
	"hash/maphash"
	"math/big"
	mrand "math/rand/v2"
)

// Packages whose amd64 builds call assembly or runtime-provided functions
// (M3 gap G1): crypto (FIPS module hooks: crypto/fips140 bypass, the service
// indicator, getrandom), hash/crc32 (SSE 4.2 / CLMUL kernels behind CPU
// feature checks), math/big (the vector kernels of arith_amd64.s) and
// hash/maphash, and internal/chacha8rand (math/rand/v2's ChaCha8: the
// block function is assembly on amd64). Every output is a deterministic function of fixed inputs.

func main() {
	msg := []byte("The quick brown fox jumps over the lazy dog")
	m5 := md5.Sum(msg)
	println("md5", hex.EncodeToString(m5[:]))
	s1 := sha1.Sum(msg)
	println("sha1", hex.EncodeToString(s1[:]))
	s256 := sha256.Sum256(msg)
	println("sha256", hex.EncodeToString(s256[:]))
	s512 := sha512.Sum512(msg)
	println("sha512", hex.EncodeToString(s512[:16]))
	mac := hmac.New(sha256.New, []byte("key"))
	mac.Write(msg)
	println("hmac", hex.EncodeToString(mac.Sum(nil)))

	key := make([]byte, 16)
	for i := range key {
		key[i] = byte(i)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	nonce := make([]byte, gcm.NonceSize())
	sealed := gcm.Seal(nil, nonce, msg, []byte("ad"))
	println("aes-gcm", hex.EncodeToString(sealed))
	opened, err := gcm.Open(nil, nonce, sealed, []byte("ad"))
	println("aes-gcm open", string(opened), err == nil)

	seed := make([]byte, ed25519.SeedSize)
	copy(seed, key)
	priv := ed25519.NewKeyFromSeed(seed)
	sig := ed25519.Sign(priv, msg)
	println("ed25519", hex.EncodeToString(sig[:16]), ed25519.Verify(priv.Public().(ed25519.PublicKey), msg, sig))

	buf := make([]byte, 32)
	n, err := rand.Read(buf)
	println("crypto/rand", n, err == nil)

	println("crc32 ieee", crc32.ChecksumIEEE(msg))
	cast := crc32.MakeTable(crc32.Castagnoli)
	println("crc32 castagnoli", crc32.Checksum(msg, cast))
	long := make([]byte, 5000)
	for i := range long {
		long[i] = byte(i * 7)
	}
	c := crc32.Update(0, cast, long[:1234])
	c = crc32.Update(c, cast, long[1234:])
	println("crc32 castagnoli long", c, c == crc32.Checksum(long, cast))
	println("crc32 koopman", crc32.Checksum(msg, crc32.MakeTable(crc32.Koopman)))

	f := new(big.Int).MulRange(1, 60)
	println("60!", f.String())
	p := new(big.Int).Lsh(big.NewInt(1), 127)
	p.Sub(p, big.NewInt(1))
	x := new(big.Int).Exp(big.NewInt(3), big.NewInt(1000), p)
	println("3^1000 mod 2^127-1", x.String())
	q, r := new(big.Int).QuoRem(f, p, new(big.Int))
	println("quorem", q.String(), r.String())
	inv := new(big.Int).ModInverse(big.NewInt(12345), p)
	println("inverse", inv.String())
	sh := new(big.Int).Rsh(f, 37)
	println("shift", sh.String(), new(big.Int).Lsh(sh, 37).Cmp(f) <= 0)
	println("sqrt", new(big.Int).Sqrt(f).String())

	var cseed [32]byte
	copy(cseed[:], "chacha8 seed for graalgo tests!")
	cc := mrand.NewChaCha8(cseed)
	println("chacha8", cc.Uint64(), cc.Uint64())
	for range 40 {
		cc.Uint64()
	}
	println("chacha8 refill", cc.Uint64())

	var h1, h2 maphash.Hash
	seedH := maphash.MakeSeed()
	h1.SetSeed(seedH)
	h2.SetSeed(seedH)
	h1.WriteString("graalgo")
	h2.Write([]byte("graalgo"))
	println("maphash", h1.Sum64() == h2.Sum64(), maphash.String(seedH, "x") == maphash.Bytes(seedH, []byte("x")))
	println("maphash comparable", maphash.Comparable(seedH, [2]int{1, 2}) == maphash.Comparable(seedH, [2]int{1, 2}))
}
