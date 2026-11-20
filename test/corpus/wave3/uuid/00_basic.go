package main

// L11 (REVIEW-2026-11-18-05): uuid — zero corpus imports before
// this program. Parsing, formatting, and comparing UUIDs: Parse /
// MustParse round-trips, Nil/Max, MarshalText/UnmarshalText,
// Compare, and the error paths. Random UUIDs (New/NewV4/NewV7) are
// pinned only by their version/variant nibbles in the string form,
// never printed (the std package has no Version/Variant accessors
// and no name-based constructors).

import (
	"fmt"
	"uuid"
)

func nibbles(s string) (ver, variant byte) {
	// Canonical form: xxxxxxxx-xxxx-Vxxx-Yxxx-xxxxxxxxxxxx.
	return s[14], s[19]
}

func main() {
	// Parse round-trip.
	s := "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	u, err := uuid.Parse(s)
	fmt.Printf("parse-err=%v roundtrip=%v\n", err != nil, u.String() == s)
	fmt.Printf("mustparse=%v\n", uuid.MustParse(s) == u)
	// Uppercase parses and normalizes to lowercase.
	up, err := uuid.Parse("6BA7B810-9DAD-11D1-80B4-00C04FD430C8")
	fmt.Printf("upper-err=%v normalized=%v\n", err != nil, up.String() == s)
	for _, bad := range []string{"not-a-uuid", "6ba7b810-9dad-11d1-80b4-00c04fd430c", "", "zzzzzzzz-9dad-11d1-80b4-00c04fd430c8"} {
		_, err := uuid.Parse(bad)
		fmt.Printf("parse-bad %q err=%v\n", bad, err != nil)
	}
	// Nil and Max.
	fmt.Printf("nil=%s\n", uuid.Nil().String())
	fmt.Printf("max=%s\n", uuid.Max().String())
	fmt.Printf("nil-compare-max=%d nil-compare-nil=%d\n", uuid.Nil().Compare(uuid.Max()), uuid.Nil().Compare(uuid.Nil()))
	// Compare on parsed constants.
	a := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	b := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c9")
	fmt.Printf("compare a-b=%d b-a=%d a-a=%d\n", a.Compare(b), b.Compare(a), a.Compare(a))
	// MarshalText / UnmarshalText round-trip.
	mt, err := u.MarshalText()
	fmt.Printf("marshaltext-err=%v text=%q\n", err != nil, string(mt))
	var u2 uuid.UUID
	err = u2.UnmarshalText(mt)
	fmt.Printf("unmarshaltext-err=%v equal=%v\n", err != nil, u2 == u)
	err = u2.UnmarshalText([]byte("bad"))
	fmt.Printf("unmarshaltext-bad-err=%v\n", err != nil)
	ap, err := u.AppendText([]byte("prefix:"))
	fmt.Printf("appendtext-err=%v text=%q\n", err != nil, string(ap))
	// Random UUIDs: pin only version/variant nibbles.
	for _, f := range []struct {
		name string
		mk   func() uuid.UUID
	}{
		{"new", uuid.New},
		{"v4", uuid.NewV4},
		{"v7", uuid.NewV7},
	} {
		v, vr := nibbles(f.mk().String())
		fmt.Printf("%s ver=%c variant-in-rfc4122=%v\n", f.name, v, vr == '8' || vr == '9' || vr == 'a' || vr == 'b')
	}
	// String shape.
	str := u.String()
	fmt.Printf("strlen=%d dashes=%v\n", len(str), str[8] == '-' && str[13] == '-' && str[18] == '-' && str[23] == '-')
}
