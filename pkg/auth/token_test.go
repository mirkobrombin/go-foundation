package auth

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

func TestSignTokenAndVerifyToken(t *testing.T) {
	secret := []byte("s3cr3t")
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
	_, err := VerifyToken("invalid", []byte("s3cr3t"))
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("VerifyToken() error = %v, want ErrInvalidToken", err)
	}
}

func TestVerifyTokenRejectsInvalidSignature(t *testing.T) {
	secret := []byte("s3cr3t")
	token, err := SignToken(Payload{Sub: "u1", Exp: time.Now().Add(time.Minute).Unix()}, secret)
	if err != nil {
		t.Fatalf("SignToken() error = %v", err)
	}
	parts := splitToken(token)
	if len(parts) != 2 {
		t.Fatal("expected 2 parts")
	}
	tampered := parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte("tampered"))
	_, err = VerifyToken(tampered, secret)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("VerifyToken() error = %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyTokenRejectsExpiredToken(t *testing.T) {
	secret := []byte("s3cr3t")
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
	oldKey := Key{ID: "old", Secret: []byte("old-secret"), Algorithm: AlgHMACSHA256}
	newKey := Key{ID: "new", Secret: []byte("new-secret"), Algorithm: AlgHMACSHA256}
	svc := NewService(newKey, oldKey)
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

func TestNewService_KeyRotationOldKeyStillWorks(t *testing.T) {
	oldKey := Key{ID: "old", Secret: []byte("old-secret"), Algorithm: AlgHMACSHA256}
	newKey := Key{ID: "new", Secret: []byte("new-secret"), Algorithm: AlgHMACSHA256}
	svcOld := NewService(oldKey)
	token, _ := svcOld.Sign(&StandardClaims{Sub: "user1", Exp: time.Now().Add(time.Hour).Unix()})
	svcNew := NewService(newKey, oldKey)
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
	key1 := Key{ID: "k1", Secret: []byte("secret1"), Algorithm: AlgHMACSHA256}
	key2 := Key{ID: "k2", Secret: []byte("secret2"), Algorithm: AlgHMACSHA256}
	svc1 := NewService(key1)
	token, _ := svc1.Sign(&StandardClaims{Sub: "u", Exp: time.Now().Add(time.Hour).Unix()})
	svc2 := NewService(key2)
	_, err := svc2.Verify(token.Token)
	if err == nil {
		t.Error("expected error when verifying with wrong key")
	}
}

func TestNewService_RS256(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	svc := NewService(Key{ID: "rsa1", Private: priv, Public: &priv.PublicKey, Algorithm: AlgRS256})
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
	svc := NewService(Key{ID: "ed1", Private: priv, Public: pub, Algorithm: AlgEdDSA})
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
	svc := NewService(Key{ID: "ec1", Private: priv, Public: &priv.PublicKey, Algorithm: AlgES256})
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
	svc := NewService()
	_, err := svc.Sign(&StandardClaims{Sub: "u", Exp: 0})
	if err == nil {
		t.Error("expected error with no keys")
	}
}
