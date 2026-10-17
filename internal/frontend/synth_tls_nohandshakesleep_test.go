// Regression test for the 2026-10-17 P0-1: the hand-authored synthetic
// crypto/tls (synth_crypto_tls.go) contained
//
//	time.Sleep(10 * time.Second)
//
// on the server side of (*Conn).Handshake(), purely to order the
// in-process handshake differential test's prints (the client's x509
// verifyPeer under Truffle is slower than the server's no-op verify, so
// without it the server printed first while gc printed the client first).
// That sleep stalled EVERY TLS server handshake in EVERY user program by
// 10 seconds and papered over a real scheduling difference vs gc.
//
// The print ordering is now synchronized explicitly in the test program
// itself (test/corpus/wave3/crypto/tls/01_handshake.go: clientPrinted
// barrier), where ordering belongs. This gate pins that no second-scale
// sleep can reappear in the synthetic TLS source: any literal
// time.Sleep with a Second-scale duration fails the test.
//
// The gate is source-level (not timing-based) because it must run in the
// fast frontend unit suite without the JVM backend. A reintroduced sleep
// makes this test fail — it is not tautological.
//
// Honest scope: this catches literal second-scale sleeps. A sleep through
// a variable (e.g. time.Sleep(d) with d in seconds) would evade it; the
// file is small enough that review covers that class.
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// Matches time.Sleep(<anything mentioning the time.Second unit>) — the
// bodge class. The legitimate 1ms scheduler yield
// (time.Sleep(time.Millisecond)) does not match: "Millisecond" has no word
// boundary before "second", while "time.Second" does.
var secondScaleSleepRe = regexp.MustCompile(`time\.Sleep\([^)]*\btime\.Second\b`)

func TestSyntheticTLSNoSecondScaleSleep(t *testing.T) {
	// The synthetic source lives in this package's directory (embedded as
	// the syntheticCryptoTlsSource string constant in synth_crypto_tls.go).
	path := filepath.Join("synth_crypto_tls.go")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	if m := secondScaleSleepRe.FindString(string(raw)); m != "" {
		t.Fatalf("synth_crypto_tls.go contains a second-scale sleep %q: "+
			"sleep-based ordering hacks do not belong in the handshake path; "+
			"synchronize the test program explicitly instead", m)
	}
}
