package main

// L11 (REVIEW-2026-11-18-05): crypto/hpke — zero corpus imports before
// this program. RFC 9180 one-shot Seal/Open with DHKEM(P-256) +
// HKDF-SHA256 + AES-128-GCM: ciphertext length + round-trip equality
// (ciphertext bytes are random), tampered-ciphertext rejection, and the
// Sender/Recipient streaming API.

import (
	"crypto/ecdh"
	"crypto/hpke"
	"crypto/rand"
	"fmt"
)

func main() {
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		fmt.Printf("key-err=%v\n", err)
		return
	}
	pub, err := hpke.NewDHKEMPublicKey(priv.PublicKey())
	if err != nil {
		fmt.Printf("pubkey-err=%v\n", err)
		return
	}
	sk, err := hpke.NewDHKEMPrivateKey(priv)
	if err != nil {
		fmt.Printf("privkey-err=%v\n", err)
		return
	}
	kem := hpke.DHKEM(ecdh.P256())
	_ = kem
	kdf := hpke.HKDFSHA256()
	aead := hpke.AES128GCM()
	info := []byte("hpke-test-info")
	pt := []byte("hello-hpke")

	// One-shot Seal/Open.
	ct, err := hpke.Seal(pub, kdf, aead, info, pt)
	if err != nil {
		fmt.Printf("seal-err=%v\n", err)
		return
	}
	fmt.Printf("seal-ct-len=%d\n", len(ct))
	dec, err := hpke.Open(sk, kdf, aead, info, ct)
	fmt.Printf("open=%q err=%v\n", string(dec), err)

	// Tampered ciphertext must fail to open.
	bad := make([]byte, len(ct))
	copy(bad, ct)
	bad[len(bad)-1] ^= 0xff
	_, err = hpke.Open(sk, kdf, aead, info, bad)
	fmt.Printf("open-tampered-err=%v\n", err != nil)

	// Wrong info must fail to open.
	_, err = hpke.Open(sk, kdf, aead, []byte("other-info"), ct)
	fmt.Printf("open-wronginfo-err=%v\n", err != nil)

	// Sender/Recipient streaming API.
	enc, sender, err := hpke.NewSender(pub, kdf, aead, info)
	if err != nil {
		fmt.Printf("sender-err=%v\n", err)
		return
	}
	fmt.Printf("sender-enc-len=%d\n", len(enc))
	sct, err := sender.Seal(nil, pt)
	if err != nil {
		fmt.Printf("sender-seal-err=%v\n", err)
		return
	}
	recipient, err := hpke.NewRecipient(enc, sk, kdf, aead, info)
	if err != nil {
		fmt.Printf("recipient-err=%v\n", err)
		return
	}
	sdec, err := recipient.Open(nil, sct)
	fmt.Printf("stream-open=%q err=%v\n", string(sdec), err)

	// Export-only AEAD: no encryption, only the exporter.
	enc2, s2, err := hpke.NewSender(pub, kdf, hpke.ExportOnly(), info)
	if err != nil {
		fmt.Printf("export-sender-err=%v\n", err)
		return
	}
	fmt.Printf("export-enc-len=%d\n", len(enc2))
	secret, err := s2.Export("ctx", 32)
	fmt.Printf("export-secret-len=%d err=%v\n", len(secret), err)
}
