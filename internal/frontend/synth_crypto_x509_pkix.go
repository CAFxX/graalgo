// P3-M9 (tier-5): synthetic `crypto/x509/pkix` package.
//
// Real-Go sources: adapted from gc go1.27.1 ($GOROOT/src/crypto/x509/pkix/pkix.go).
// Provides the PKIX types needed by crypto/x509.
// Retirement: see ../../docs/arch/synth-retirement-tracker.md#cryptox509pkix for this package's root cause and retires-when trigger (crypto/x509/pkix).
package main

// syntheticCryptoX509PkixSource is the P3-M9 synthetic `crypto/x509/pkix` package.
const syntheticCryptoX509PkixSource = `package pkix

import (
	"encoding/asn1"
	"math/big"
	"time"
)

// AttributeTypeAndValueSET represents a set of ASN.1 Attributes.
type AttributeTypeAndValueSET struct {
	Type     asn1.ObjectIdentifier
	Value    any
}

// Extension represents an ASN.1 Extension.
type Extension struct {
	Id       asn1.ObjectIdentifier
	Critical bool
	Value    []byte
}

// Name represents an X.509 distinguished name.
type Name struct {
	Country            []string
	Organization       []string
	OrganizationalUnit []string
	Locality           []string
	Province           []string
	StreetAddress      []string
	PostalCode         []string
	SerialNumber       string
	CommonName         string
	ExtraNames         []AttributeTypeAndValue
}

// String returns the string representation of the Name.
func (n Name) String() string {
	// Simplified: return CommonName or empty
	if n.CommonName != "" {
		return n.CommonName
	}
	return ""
}

// ToRDNSequence converts the Name to an RDNSequence.
func (n Name) ToRDNSequence() (ret RDNSequence) {
	// Simplified for M9
	return ret
}

// AttributeTypeAndValue represents an ASN.1 attribute.
type AttributeTypeAndValue struct {
	Type  asn1.ObjectIdentifier
	Value any
}

// RDNSequence is a sequence of RelativeDistinguishedNames.
type RDNSequence []RelativeDistinguishedNameSet

// RelativeDistinguishedNameSet is a set of AttributeTypeAndValue.
type RelativeDistinguishedNameSet []AttributeTypeAndValue

// AlgorithmIdentifier represents an ASN.1 algorithm identifier.
type AlgorithmIdentifier struct {
	Algorithm  asn1.ObjectIdentifier
	Parameters asn1.RawValue `+"`asn1:\"optional\"`"+`
}

// CertificateList represents a CRL.
type CertificateList struct {
	TBSCertList        TBSCertificateList
	SignatureAlgorithm AlgorithmIdentifier
	SignatureValue     asn1.BitString
}

// TBSCertificateList represents the TBS part of a CRL.
type TBSCertificateList struct {
	Raw                 asn1.RawContent
	Version             int `+"`asn1:\"optional,explicit,default:0,tag:0\"`"+`
	Signature           AlgorithmIdentifier
	Issuer              RDNSequence
	ThisUpdate          time.Time
	NextUpdate          time.Time            `+"`asn1:\"optional\"`"+`
	RevokedCertificates []RevokedCertificate `+"`asn1:\"optional\"`"+`
	Extensions          []Extension          `+"`asn1:\"tag:0,optional,explicit\"`"+`
}

// RevokedCertificate represents a revoked certificate in a CRL.
type RevokedCertificate struct {
	SerialNumber   *big.Int
	RevocationTime time.Time
	Extensions     []Extension `+"`asn1:\"optional\"`"+`
}
`
