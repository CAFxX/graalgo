package main

import (
	"crypto/tls"
	"fmt"
)

func main() {
	// Test tls.Config creation (now supported via synthetic crypto/tls)
	cfg := &tls.Config{InsecureSkipVerify: true}
	fmt.Println("config ok:", cfg != nil)
	fmt.Println("insecure:", cfg.InsecureSkipVerify)

	// Test version constants
	fmt.Println("tls10:", tls.VersionTLS10 == 0x0301)
	fmt.Println("tls13:", tls.VersionTLS13 == 0x0304)

	// Test cipher suite constants
	fmt.Println("aes128gcm:", tls.TLS_AES_128_GCM_SHA256 == 0x1301)

	// Test ClientAuth constants
	fmt.Println("noClientCert:", tls.NoClientCert == 0)
}
