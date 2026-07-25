// Package jwttest generates Ed25519 key material in the env wire format the jwt
// package reads, so tests in any package can build an issuer or verifier.
package jwttest

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"testing"
)

// KeyPair returns a fresh Ed25519 pair as "<kid>:<base64 DER>" entries, matching
// JWT_PRIVATE_KEY (PKCS8) and JWT_PUBLIC_KEYS (PKIX).
func KeyPair(t testing.TB, kid string) (private, public string) {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey() error = %v", err)
	}

	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey() error = %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey() error = %v", err)
	}

	return kid + ":" + base64.StdEncoding.EncodeToString(privDER),
		kid + ":" + base64.StdEncoding.EncodeToString(pubDER)
}
