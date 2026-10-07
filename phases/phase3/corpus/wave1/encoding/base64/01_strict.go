package main

import (
	"encoding/base64"
	"fmt"
)

func main() {
	src := []byte("strict mode!!")

	// Strict rejects trailing bits / bad padding more aggressively.
	strict := base64.StdEncoding.Strict()
	s := strict.EncodeToString(src)
	fmt.Println(s)
	d, err := strict.DecodeString(s)
	fmt.Println(string(d), err == nil)

	// Non-strict accepts what strict would reject (trailing 0 bits).
	// "AB" decodes with trailing bits set; strict rejects, lax accepts.
	_, errStrict := strict.DecodeString("AB")
	_, errLax := base64.StdEncoding.DecodeString("AB")
	fmt.Println(errStrict != nil, errLax == nil)

	// WithPadding custom.
	padded := base64.URLEncoding.WithPadding('*')
	sp := padded.EncodeToString(src)
	fmt.Println(sp)

	// NewEncoding custom alphabet.
	custom := base64.NewEncoding("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_")
	fmt.Println(custom.EncodeToString(src))
}
