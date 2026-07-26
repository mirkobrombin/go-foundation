package auth

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

var testHMACSecret = hmacSecret('s')

func hmacSecret(value byte) []byte {
	return bytes.Repeat([]byte{value}, 32)
}

func TestSignTokenAndVerifyToken(t *testing.T) {
	secret := testHMACSecret
	token, err := SignToken(Payload{Sub: "u1", Exp: time.Now().Add(time.Minute).Unix()}, secret)
	if err != nil {
		t.Fatalf("SignToken() error = %v", err)
	}
	payload, err := VerifyToken(token, secret)
	if err != nil {
		t.Fatalf("VerifyToken() error = %v", err)
	}
	if payload.Sub != "u1" {
		t.Fatalf("VerifyToken() subject = %q, want %q", payload.Sub, "u1")
	}
}

func TestVerifyTokenRejectsMalformedToken(t *testing.T) {
	_, err := VerifyToken("invalid", testHMACSecret)
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("VerifyToken() error = %v, want ErrInvalidToken", err)
	}
}

func TestVerifyTokenRejectsInvalidSignature(t *testing.T) {
	secret := testHMACSecret
	token, err := SignToken(Payload{Sub: "u1", Exp: time.Now().Add(time.Minute).Unix()}, secret)
	if err != nil {
		t.Fatalf("SignToken() error = %v", err)
	}
	payload, _, ok := strings.Cut(token, ".")
	if !ok {
		t.Fatal("expected 2 parts")
	}
	tampered := payload + "." + base64.RawURLEncoding.EncodeToString([]byte("tampered"))
	_, err = VerifyToken(tampered, secret)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("VerifyToken() error = %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyTokenRejectsExpiredToken(t *testing.T) {
	secret := testHMACSecret
	token, err := SignToken(Payload{Sub: "u1", Exp: time.Now().Add(-time.Minute).Unix()}, secret)
	if err != nil {
		t.Fatalf("SignToken() error = %v", err)
	}
	_, err = VerifyToken(token, secret)
	if !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("VerifyToken() error = %v, want ErrExpiredToken", err)
	}
}

func TestNewService_KeyRotation(t *testing.T) {
	oldKey := Key{ID: "old", Secret: hmacSecret('o'), Algorithm: AlgHMACSHA256}
	newKey := Key{ID: "new", Secret: hmacSecret('n'), Algorithm: AlgHMACSHA256}
	svc := MustNewService(newKey, oldKey)
	token, err := svc.Sign(&StandardClaims{Sub: "user1", Exp: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}
	if token.KeyID != "new" {
		t.Errorf("got key id %q, want %q", token.KeyID, "new")
	}
	claims, err := svc.Verify(token.Token)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	c := claims.(StandardClaims)
	if c.Sub != "user1" {
		t.Errorf("got sub %s, want user1", c.Sub)
	}
}

func TestNewServiceClonesHMACKeyMaterial(t *testing.T) {
	secret := hmacSecret('k')
	service := MustNewService(Key{ID: "key", Secret: secret, Algorithm: AlgHMACSHA256})
	secret[0] ^= 0xff

	token, err := service.Sign(&StandardClaims{
		Sub: "user",
		Exp: time.Now().Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Verify(token.Token); err != nil {
		t.Fatalf("Verify() after caller key mutation: %v", err)
	}
}

func TestNewService_KeyRotationOldKeyStillWorks(t *testing.T) {
	oldKey := Key{ID: "old", Secret: hmacSecret('o'), Algorithm: AlgHMACSHA256}
	newKey := Key{ID: "new", Secret: hmacSecret('n'), Algorithm: AlgHMACSHA256}
	svcOld := MustNewService(oldKey)
	token, _ := svcOld.Sign(&StandardClaims{Sub: "user1", Exp: time.Now().Add(time.Hour).Unix()})
	svcNew := MustNewService(newKey, oldKey)
	claims, err := svcNew.Verify(token.Token)
	if err != nil {
		t.Fatalf("Verify with old key should still work: %v", err)
	}
	c := claims.(StandardClaims)
	if c.Sub != "user1" {
		t.Errorf("got sub %s, want user1", c.Sub)
	}
}

func TestNewService_WrongKey(t *testing.T) {
	key1 := Key{ID: "k1", Secret: hmacSecret('1'), Algorithm: AlgHMACSHA256}
	key2 := Key{ID: "k2", Secret: hmacSecret('2'), Algorithm: AlgHMACSHA256}
	svc1 := MustNewService(key1)
	token, _ := svc1.Sign(&StandardClaims{Sub: "u", Exp: time.Now().Add(time.Hour).Unix()})
	svc2 := MustNewService(key2)
	_, err := svc2.Verify(token.Token)
	if err == nil {
		t.Error("expected error when verifying with wrong key")
	}
}

func TestNewService_RS256(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	svc := MustNewService(Key{ID: "rsa1", Private: priv, Public: &priv.PublicKey, Algorithm: AlgRS256})
	token, err := svc.Sign(&StandardClaims{Sub: "user_rsa", Exp: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatalf("Sign with RSA failed: %v", err)
	}
	claims, err := svc.Verify(token.Token)
	if err != nil {
		t.Fatalf("Verify with RSA failed: %v", err)
	}
	c := claims.(StandardClaims)
	if c.Sub != "user_rsa" {
		t.Errorf("got sub %s, want user_rsa", c.Sub)
	}
}

func TestNewService_EdDSA(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	svc := MustNewService(Key{ID: "ed1", Private: priv, Public: pub, Algorithm: AlgEdDSA})
	token, err := svc.Sign(&StandardClaims{Sub: "user_ed", Exp: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatalf("Sign with EdDSA failed: %v", err)
	}
	claims, err := svc.Verify(token.Token)
	if err != nil {
		t.Fatalf("Verify with EdDSA failed: %v", err)
	}
	c := claims.(StandardClaims)
	if c.Sub != "user_ed" {
		t.Errorf("got sub %s, want user_ed", c.Sub)
	}
}

func TestNewService_ECDSA(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	svc := MustNewService(Key{ID: "ec1", Private: priv, Public: &priv.PublicKey, Algorithm: AlgES256})
	token, err := svc.Sign(&StandardClaims{Sub: "user_ec", Exp: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatalf("Sign with ECDSA failed: %v", err)
	}
	claims, err := svc.Verify(token.Token)
	if err != nil {
		t.Fatalf("Verify with ECDSA failed: %v", err)
	}
	c := claims.(StandardClaims)
	if c.Sub != "user_ec" {
		t.Errorf("got sub %s, want user_ec", c.Sub)
	}
}

func TestStandardClaims_Valid_Expired(t *testing.T) {
	c := StandardClaims{Sub: "u", Exp: time.Now().Add(-time.Hour).Unix()}
	if err := c.Valid(); err == nil {
		t.Error("expected error for expired claims")
	}
}

func TestStandardClaims_Valid_NotExpired(t *testing.T) {
	c := StandardClaims{Sub: "u", Exp: time.Now().Add(time.Hour).Unix()}
	if err := c.Valid(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewService_NoKeys(t *testing.T) {
	if _, err := NewService(); err == nil {
		t.Error("NewService() accepted no keys")
	}
}

func TestNewService_RejectsInvalidKeys(t *testing.T) {
	tests := []Key{
		{ID: "short", Secret: []byte("short"), Algorithm: AlgHMACSHA256},
		{ID: "rsa", Algorithm: AlgRS256, Private: "wrong", Public: "wrong"},
		{ID: "unknown", Algorithm: Algorithm("unknown")},
	}
	for _, key := range tests {
		if _, err := NewService(key); err == nil {
			t.Fatalf("NewService() accepted key %#v", key)
		}
	}

	key := Key{ID: "same", Secret: testHMACSecret, Algorithm: AlgHMACSHA256}
	if _, err := NewService(key, key); err == nil {
		t.Fatal("NewService() accepted duplicate key IDs")
	}
}

func TestNewService_RejectsMismatchedAsymmetricKeys(t *testing.T) {
	rsaPrivate, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	otherRSA, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ecdsaPrivate, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherECDSA, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, edPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherEdPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p384Private, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	keys := []Key{
		{ID: "rsa", Private: rsaPrivate, Public: &otherRSA.PublicKey, Algorithm: AlgRS256},
		{ID: "ecdsa", Private: ecdsaPrivate, Public: &otherECDSA.PublicKey, Algorithm: AlgES256},
		{ID: "ed25519", Private: edPrivate, Public: otherEdPublic, Algorithm: AlgEdDSA},
		{ID: "p384", Private: p384Private, Public: &p384Private.PublicKey, Algorithm: AlgES256},
	}
	for _, key := range keys {
		if _, err := NewService(key); err == nil {
			t.Fatalf("NewService() accepted mismatched key %q", key.ID)
		}
	}
}

func TestClaimsRejectExactExpirationSecond(t *testing.T) {
	expiration := time.Now().Unix()
	if err := (Payload{Exp: expiration}).Valid(); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("Payload.Valid() error = %v, want ErrExpiredToken", err)
	}
	if err := (StandardClaims{Exp: expiration}).Valid(); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("StandardClaims.Valid() error = %v, want ErrExpiredToken", err)
	}
}

func TestSignTokenRejectsShortSecret(t *testing.T) {
	if _, err := SignToken(Payload{}, []byte("short")); err == nil {
		t.Fatal("SignToken() accepted a short HMAC secret")
	}
}

func TestVerifyRejectsOversizedTokens(t *testing.T) {
	oversized := strings.Repeat(".", maxTokenSize+1)
	if _, err := VerifyToken(oversized, testHMACSecret); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("VerifyToken() error = %v, want ErrInvalidToken", err)
	}

	service, err := NewService(Key{
		ID:        "key",
		Secret:    testHMACSecret,
		Algorithm: AlgHMACSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Verify(oversized); err == nil {
		t.Fatal("TokenService.Verify() accepted an oversized token")
	}
}

type largeClaims struct {
	Data string `json:"data"`
}

func (largeClaims) Valid() error {
	return nil
}

func TestTokenServiceDoesNotCreateUnverifiableOversizedToken(t *testing.T) {
	service, err := NewService(Key{
		ID:        "key",
		Secret:    testHMACSecret,
		Algorithm: AlgHMACSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Sign(largeClaims{Data: strings.Repeat("x", maxTokenSize)}); err == nil {
		t.Fatal("Sign() created a token larger than the verifier accepts")
	}
}
