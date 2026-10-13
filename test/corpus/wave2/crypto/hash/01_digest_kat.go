// KAT: digests of empty, 1-block, multi-block inputs vs hard-coded
// standard vectors (FIPS 180-4 for SHA-1/2, RFC 1321 for MD5, FIPS 202
// for SHA-3). Hard-coded (not impl-vs-impl) so the test is capable of
// failing: any synthetic digest that disagrees with the standard fails.
package main

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/sha3"
	"encoding/hex"
	"fmt"
	"os"
)

func hx(b []byte) string { return hex.EncodeToString(b) }

var kat = []struct {
	name           string
	in             []byte
	md5, sha1      string
	sha256, sha512 string
	sha3           string
}{
	{"empty", []byte{},
		"d41d8cd98f00b204e9800998ecf8427e",
		"da39a3ee5e6b4b0d3255bfef95601890afd80709",
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e",
		"a7ffc6f8bf1ed76651c14756a061d662f580ff4de43b49fa82d80a4b80f8434a"},
	{"abc", []byte("abc"),
		"900150983cd24fb0d6963f7d28e17f72",
		"a9993e364706816aba3e25717850c26c9cd0d89d",
		"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		"ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a2192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49f",
		"3a985da74fe225b2045c172d6bd390bd855f086e3e9d525b46bfe24511431532"},
	{"55a", bytes.Repeat([]byte("a"), 55),
		"ef1772b6dff9a122358552954ad0df65",
		"c1c8bbdc22796e28c0e15163d20899b65621d65a",
		"9f4390f8d30c2dd92ec9f095b65e2b9ae9b0a925a5258e241c9f1e910f734318",
		"b0220c772cbf6c1822e2cb38a437d0e1d58772417a4bbb21c961364f8b6143e05aa6316dca8d1d7b19e16448419076395f6086cb55101fbd6d5497b148e1745f",
		"78c2a04624b9328ae0e40cb8cdd29980f6ff55abf2dca68e3412d09eed4b9d03"},
	{"56b", bytes.Repeat([]byte("b"), 56),
		"b9d955696c7654cd20086bec31670b11",
		"049adea016c8a8e3b7a9054aeeaa8643453bebd9",
		"a5fc6e203a4c2b657d0d153885932414b2ffc6a93f0f8bf8b3183315e5a7212c",
		"af5fc8e26d3a65180cc5346abce4079fbe5111020163cd8787b1942f587cc991eeeb46db3594227a25debf8deb0288d419e5aa8943bfc12ceb4b0e50d3e6514f",
		"b811c7963bd3d871ceb3dd7009bb484f88a2c03ad928a192a839979e91986734"},
	{"64c", bytes.Repeat([]byte("c"), 64),
		"bcd5708ed79b18f0f0aaa27fd0056d86",
		"b29fcb5f41614b4ec81c87e4614ffe8b9ace1d4e",
		"52b6419d27bd7f547cee3b92f8c17a908b8a49601ecbec161e5030de1dfe9e0a",
		"ce733117f9963859337176b7ddf93f889190b36ae0d76b61b8760eb350c48fd48a79437c812701a53dbce608b613e52835618aa5895bde9b75c0d72f7b368bfa",
		"1808e70c666d55dd1826f6e20f1e628f878946cdacc2b28a4f621224ee6ee2f3"},
	{"1000d", bytes.Repeat([]byte("d"), 1000),
		"d9a0e8de165479413f12fc6065062298",
		"40844d526c1d4b472a2da8ed47bd7adea39b93d4",
		"e8cec134fe59530e102e14869a9b0b3796129eb410a827b81d5b38cc80f334ab",
		"34179a2e2771e4dcd975df4361abb0b7b3cc287d981eb230bb2c965ff73ce6c350ddcc1484faa4ae29c83193bd28e552b35930abd8959acf307238836e416eb8",
		"b6fb004c2c73d78d634d6e25e01d5d6577077bd732924e97072f90b24c122b82"},
	{"135a", bytes.Repeat([]byte("a"), 135), // SHA-3 block boundary (rate 136)
		"247dc7ffc545e4dda64ae12def481c4e",
		"71f9ed42b368201e6e0facebedefc3f46575b67b",
		"dfa58dfd72f3c7080d0249a7758fd3636872f63fa24b18473ed36f031e248347",
		"018305457c1804285649e77d1ada038a4db3b166a39dc93504584bbb9f95be2d5a2147798aa71d0f240f442d2daf4928695548cf828c7fe314149544b442fb7c",
		"8094bb53c44cfb1e67b7c30447f9a1c33696d2463ecc1d9c92538913392843c9"},
	{"136e", bytes.Repeat([]byte("e"), 136),
		"218c433c3552cdf5a5dedb54a8a71d3b",
		"f7dec8e45f7d3308c75584093071169a1e4f707f",
		"ef76250302cb38f596531b122b205a4872927d48ceefb4b3eacfc8950a3880ca",
		"f8fd12d64c4f68640673e05165d05b1bca2b8739351b537cd9279bae376f33ac78f0b2cd7fbb91614bbf0b558e870b2ea24c8342304bc7804bda8c5813f3cd38",
		"60af645b2e78de07df552c58e5ad48a80825b8951d6b129447e37da9a4fe9143"},
}

func main() {
	failed := false
	check := func(name, got, want string) {
		if got != want {
			fmt.Printf("FAIL %s: got %s want %s\n", name, got, want)
			failed = true
		}
	}
	for _, v := range kat {
		s := md5.Sum(v.in)
		check("md5-"+v.name, hx(s[:]), v.md5)
		s1 := sha1.Sum(v.in)
		check("sha1-"+v.name, hx(s1[:]), v.sha1)
		s2 := sha256.Sum256(v.in)
		check("sha256-"+v.name, hx(s2[:]), v.sha256)
		s3 := sha512.Sum512(v.in)
		check("sha512-"+v.name, hx(s3[:]), v.sha512)
		s4 := sha3.Sum256(v.in)
		check("sha3-"+v.name, hx(s4[:]), v.sha3)
	}
	if failed {
		os.Exit(1)
	}
	fmt.Println("PASS")
}
