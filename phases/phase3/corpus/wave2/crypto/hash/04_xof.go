// SHAKE128 XOF vs hard-coded FIPS 202 vectors (generated from gc 1.27.1,
// cross-checked against the FIPS 202 empty-input prefix 7f9c2ba4...).
// Hard-coded (not impl-vs-impl) so the test is capable of failing.
package main

import (
	"bytes"
	"crypto/sha3"
	"encoding/hex"
	"fmt"
	"os"
)

var shakeKat = []struct {
	in   []byte
	size int
	want string
}{
	{[]byte{}, 32, "7f9c2ba4e88f827d616045507605853ed73b8093f6efbc88eb1a6eacfa66ef26"},
	{[]byte{}, 64, "7f9c2ba4e88f827d616045507605853ed73b8093f6efbc88eb1a6eacfa66ef263cb1eea988004b93103cfb0aeefd2a686e01fa4a58e8a3639ca8a1e3f9ae57e2"},
	{[]byte{}, 100, "7f9c2ba4e88f827d616045507605853ed73b8093f6efbc88eb1a6eacfa66ef263cb1eea988004b93103cfb0aeefd2a686e01fa4a58e8a3639ca8a1e3f9ae57e235b8cc873c23dc62b8d260169afa2f75ab916a58d974918835d25e6a435085b2badfd6df"},
	{[]byte("abc"), 32, "5881092dd818bf5cf8a3ddb793fbcba74097d5c526a6d35f97b83351940f2cc8"},
	{[]byte("abc"), 64, "5881092dd818bf5cf8a3ddb793fbcba74097d5c526a6d35f97b83351940f2cc844c50af32acd3f2cdd066568706f509bc1bdde58295dae3f891a9a0fca578378"},
	{[]byte("abc"), 100, "5881092dd818bf5cf8a3ddb793fbcba74097d5c526a6d35f97b83351940f2cc844c50af32acd3f2cdd066568706f509bc1bdde58295dae3f891a9a0fca5783789a41f8611214ce612394df286a62d1a2252aa94db9c538956c717dc2bed4f232a0294c85"},
	{bytes.Repeat([]byte("z"), 200), 32, "8c1dd38183ef8b7b2608e64bead6b59450cc02d4dc10e8d9fb94f21d655d9009"},
	{bytes.Repeat([]byte("z"), 200), 64, "8c1dd38183ef8b7b2608e64bead6b59450cc02d4dc10e8d9fb94f21d655d9009efaef3c66ba5346296c0458f8dbea69d3f421b9da7a716c3ada089c8263b0756"},
	{bytes.Repeat([]byte("z"), 200), 100, "8c1dd38183ef8b7b2608e64bead6b59450cc02d4dc10e8d9fb94f21d655d9009efaef3c66ba5346296c0458f8dbea69d3f421b9da7a716c3ada089c8263b07565e84a6a9ce1aed4115dc92746ab2eec3fdaa8bb6ea5ee2a63f3c9131070ec2ccdc6aedb1"},
}

func main() {
	failed := false
	for _, v := range shakeKat {
		got := make([]byte, v.size)
		h := sha3.NewSHAKE128()
		h.Write(v.in)
		h.Read(got)
		if hex.EncodeToString(got) != v.want {
			fmt.Printf("FAIL shake128 inlen=%d size=%d\n", len(v.in), v.size)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
	fmt.Println("PASS")
}
