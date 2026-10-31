package main

// R-01 regression (independent codebase review 2026-10-31): the synthetic
// crypto/tls server handshake used to sleep 10 seconds after Handshake()
// so the client's slower verifyPeer prints would land first for a
// byte-identical differential, and a sibling 1 ms sleep yielded the peer
// on NEED_WRAP with no output bytes. Both were timing bets, not
// synchronization: any handshake deadline under 10 s failed, every server
// handshake paid 10 s, and under load the ordering could still lose.
// The fix verifies the peer before sending Finished (protocol causality
// gives gc's client-first order) and yields via runtime.Gosched.
// These guards fail if either sleep comes back in any form: the
// Handshake-body assertion is the requirement (no sleep on the shipped
// handshake path); the whole-source assertion guards the sibling pattern
// from reappearing elsewhere in the package.

import (
	"strings"
	"testing"
)

func TestSyntheticTlsHandshakeHasNoSleep(t *testing.T) {
	hs := strings.Index(syntheticCryptoTlsSource, "func (c *Conn) Handshake() error {")
	if hs < 0 {
		t.Fatal("synthetic crypto/tls source has no Conn.Handshake; test needs updating")
	}
	rest := syntheticCryptoTlsSource[hs:]
	end := strings.Index(rest, "func (c *Conn) verifyPeerEarly() error {")
	if end < 0 {
		// No early-verify helper: report it, but still run the sleep
		// assertions over the rest of the source so a pre-fix run
		// shows every violation, not just the first.
		t.Errorf("synthetic crypto/tls source has no verifyPeerEarly: " +
			"deleting the sleep without the early verify reintroduces the " +
			"server-prints-first ordering bug")
		end = len(rest)
	}
	body := rest[:end]
	if strings.Contains(body, "time.Sleep") {
		t.Errorf("synthetic crypto/tls Handshake still sleeps: ordering must come from " +
			"verify-before-Finished causality (verifyPeerEarly), never from a sleep")
	}
	if !strings.Contains(body, "verifyPeerEarly()") {
		t.Errorf("synthetic crypto/tls Handshake does not call verifyPeerEarly: " +
			"deleting the sleep without the early verify reintroduces the " +
			"server-prints-first ordering bug")
	}
	if !strings.Contains(body, "runtime.Gosched()") {
		t.Errorf("synthetic crypto/tls Handshake NEED_WRAP no-output branch must yield " +
			"via runtime.Gosched, not sleep or spin")
	}
	if !strings.Contains(syntheticCryptoTlsSource, "peerVerified bool") {
		t.Errorf("synthetic crypto/tls Conn lost its peerVerified flag: " +
			"early verification must be tracked so the FINISHED fallback cannot double-verify")
	}
	if strings.Contains(syntheticCryptoTlsSource, "time.Sleep") {
		t.Errorf("synthetic crypto/tls source still contains time.Sleep somewhere: " +
			"no sleep of any duration belongs in the shipped handshake path")
	}
}
