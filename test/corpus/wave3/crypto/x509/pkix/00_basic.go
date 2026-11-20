package main

// L11 (REVIEW-2026-11-18-05): crypto/x509/pkix — zero corpus imports
// before this program. pkix.Name <-> RDNSequence round-trip (exact DER
// pin), ExtraNames, Extension marshal, AttributeTypeAndValue encoding.

import (
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
)

func main() {
	name := pkix.Name{
		CommonName:         "example.com",
		Organization:       []string{"Example Org"},
		OrganizationalUnit: []string{"Engineering"},
		Country:            []string{"IT"},
		Province:           []string{"Piedmont"},
		Locality:           []string{"Turin"},
	}
	rdn := name.ToRDNSequence()
	der, err := asn1.Marshal(rdn)
	if err != nil {
		fmt.Printf("marshal-err=%v\n", err)
		return
	}
	fmt.Printf("rdn-der=%x\n", der)
	// Parse the DER back and compare the printable form.
	var back pkix.RDNSequence
	if _, err := asn1.Unmarshal(der, &back); err != nil {
		fmt.Printf("unmarshal-err=%v\n", err)
		return
	}
	name2 := pkix.Name{}
	name2.FillFromRDNSequence(&back)
	fmt.Printf("roundtrip-cn=%q org=%q country=%q\n",
		name2.CommonName, name2.Organization, name2.Country)
	fmt.Printf("roundtrip-string=%q\n", name2.String())

	// ExtraNames: an explicit OID attribute survives the round-trip.
	oidMail := asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 1}
	name3 := pkix.Name{
		CommonName: "extra",
		ExtraNames: []pkix.AttributeTypeAndValue{
			{Type: oidMail, Value: "a@example.com"},
		},
	}
	rdn3 := name3.ToRDNSequence()
	der3, _ := asn1.Marshal(rdn3)
	fmt.Printf("extra-der=%x\n", der3)
	fmt.Printf("extra-string=%q\n", name3.String())

	// Extension: marshal a fixed extension and read it back.
	ext := pkix.Extension{
		Id:       asn1.ObjectIdentifier{2, 5, 29, 19},
		Critical: true,
		Value:    []byte{0x30, 0x03, 0x01, 0x01, 0xff},
	}
	derExt, err := asn1.Marshal(ext)
	fmt.Printf("ext-der=%x err=%v\n", derExt, err)
	var backExt pkix.Extension
	if _, err := asn1.Unmarshal(derExt, &backExt); err == nil {
		fmt.Printf("ext-roundtrip-id=%v critical=%v\n", backExt.Id, backExt.Critical)
	}

	// Empty name marshals to an empty sequence.
	empty := pkix.Name{}
	rdnEmpty := empty.ToRDNSequence()
	derEmpty, _ := asn1.Marshal(rdnEmpty)
	fmt.Printf("empty-der=%x\n", derEmpty)
}
