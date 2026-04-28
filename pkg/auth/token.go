package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidToken     = errors.New("auth: invalid token")
	ErrInvalidSignature = errors.New("auth: invalid signature")
	ErrExpiredToken     = errors.New("auth: token expired")
	ErrUnknownKeyID     = errors.New("auth: unknown key id")
)

// Payload holds the claims for a simple token.
type Payload struct {
	Sub string `json:"sub"`
	Exp int64  `json:"exp"`
}

// StandardClaims holds common JWT-like claims.
type StandardClaims struct {
	Sub string `json:"sub"`
	Exp int64  `json:"exp"`
	Iat int64  `json:"iat,omitempty"`
	Jti string `json:"jti,omitempty"`
	Iss string `json:"iss,omitempty"`
	Aud string `json:"aud,omitempty"`
}

// Claims validates token claims.
type Claims interface {
	Valid() error
}

func (p Payload) Valid() error {
	if time.Now().Unix() > p.Exp {
		return ErrExpiredToken
	}
	return nil
}

func (c StandardClaims) Valid() error {
	if time.Now().Unix() > c.Exp {
		return ErrExpiredToken
	}
	return nil
}

// Algorithm identifies a signing algorithm.
type Algorithm string

const (
	AlgHMACSHA256 Algorithm = "HS256"
	AlgRS256      Algorithm = "RS256"
	AlgES256      Algorithm = "ES256"
	AlgEdDSA      Algorithm = "EdDSA"
)

// SignToken signs a payload with HMAC-SHA256.
func SignToken(p Payload, secret []byte) (string, error) {
	return sign(p, secret, AlgHMACSHA256)
}

// VerifyToken verifies and decodes a HMAC-SHA256 token.
func VerifyToken(token string, secret []byte) (Payload, error) {
	return verify[Payload](token, secret, AlgHMACSHA256)
}

// Key holds signing key material.
type Key struct {
	ID        string
	Secret    []byte
	Algorithm Algorithm
	Private   crypto.PrivateKey
	Public    crypto.PublicKey
}

// SignedToken holds a token string with its key ID and raw payload.
type SignedToken struct {
	Token   string
	KeyID   string
	Payload []byte
	Raw     string
}

// TokenService signs and verifies tokens.
type TokenService interface {
	Sign(claims Claims) (*SignedToken, error)
	Verify(token string) (Claims, error)
}

type multiKeyService struct {
	keys []Key
}

// NewService creates a TokenService with one or more keys.
func NewService(keys ...Key) TokenService {
	return &multiKeyService{keys: keys}
}

func (s *multiKeyService) Sign(claims Claims) (*SignedToken, error) {
	if len(s.keys) == 0 {
		return nil, errors.New("auth: no keys configured")
	}
	k := s.keys[0]
	return signWithKey(claims, k)
}

func (s *multiKeyService) Verify(token string) (Claims, error) {
	for _, k := range s.keys {
		claims, err := verifyWithKey[StandardClaims](token, k)
		if err == nil {
			return claims, nil
		}
		if errors.Is(err, ErrUnknownKeyID) {
			continue
		}
	}
	return nil, ErrInvalidSignature
}

func signWithKey(claims Claims, key Key) (*SignedToken, error) {
	data, err := json.Marshal(claims)
	if err != nil {
		return nil, err
	}

	var sig []byte
	switch key.Algorithm {
	case AlgHMACSHA256:
		sig, err = signHMAC(data, key.Secret)
	case AlgRS256:
		sig, err = signRSA(data, key.Private.(*rsa.PrivateKey))
	case AlgES256:
		sig, err = signECDSA(data, key.Private.(*ecdsa.PrivateKey))
	case AlgEdDSA:
		sig, err = signEd25519(data, key.Private.(ed25519.PrivateKey))
	default:
		return nil, fmt.Errorf("auth: unsupported algorithm: %s", key.Algorithm)
	}
	if err != nil {
		return nil, err
	}

	kid := base64.RawURLEncoding.EncodeToString([]byte(key.ID))
	payload := base64.RawURLEncoding.EncodeToString(data)
	signature := base64.RawURLEncoding.EncodeToString(sig)
	token := kid + "." + payload + "." + signature

	return &SignedToken{
		Token:   token,
		KeyID:   key.ID,
		Payload: data,
		Raw:     token,
	}, nil
}

func verifyWithKey[T Claims](token string, key Key) (T, error) {
	var zero T

	parts := splitToken(token)
	if len(parts) != 3 {
		return zero, ErrInvalidToken
	}

	kidBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return zero, ErrInvalidToken
	}
	if string(kidBytes) != key.ID {
		return zero, ErrUnknownKeyID
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return zero, ErrInvalidToken
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return zero, ErrInvalidToken
	}

	switch key.Algorithm {
	case AlgRS256:
		if err := verifyRSA(payloadBytes, sig, key.Public.(*rsa.PublicKey)); err != nil {
			return zero, err
		}
	case AlgHMACSHA256:
		if !verifyHMAC(payloadBytes, sig, key.Secret) {
			return zero, ErrInvalidSignature
		}
	case AlgES256:
		if !verifyECDSA(payloadBytes, sig, key.Public.(*ecdsa.PublicKey)) {
			return zero, ErrInvalidSignature
		}
	case AlgEdDSA:
		if !verifyEd25519(payloadBytes, sig, key.Public.(ed25519.PublicKey)) {
			return zero, ErrInvalidSignature
		}
	default:
		return zero, fmt.Errorf("auth: unsupported algorithm: %s", key.Algorithm)
	}

	var claims T
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return zero, err
	}
	if c, ok := any(&claims).(Claims); ok {
		if err := c.Valid(); err != nil {
			return zero, err
		}
	}
	return claims, nil
}

func sign[T any](p T, secret []byte, alg Algorithm) (string, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return "", err
	}

	var sig []byte
	switch alg {
	case AlgHMACSHA256:
		sig = computeSig(data, secret)
	default:
		return "", fmt.Errorf("auth: unsupported algorithm: %s", alg)
	}

	return base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func verify[T any](token string, secret []byte, alg Algorithm) (T, error) {
	var zero T

	idx := -1
	for i := 0; i < len(token); i++ {
		if token[i] == '.' {
			idx = i
			break
		}
	}
	if idx < 0 {
		return zero, ErrInvalidToken
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(token[:idx])
	if err != nil {
		return zero, ErrInvalidToken
	}

	sig, err := base64.RawURLEncoding.DecodeString(token[idx+1:])
	if err != nil {
		return zero, ErrInvalidToken
	}

	var valid bool
	switch alg {
	case AlgHMACSHA256:
		expected := computeSig(payloadBytes, secret)
		valid = hmac.Equal(sig, expected)
	default:
		return zero, fmt.Errorf("auth: unsupported algorithm: %s", alg)
	}

	if !valid {
		return zero, ErrInvalidSignature
	}

	if err := json.Unmarshal(payloadBytes, &zero); err != nil {
		return zero, err
	}

	if p, ok := any(zero).(Claims); ok {
		if err := p.Valid(); err != nil {
			return zero, err
		}
	}

	return zero, nil
}

func signHMAC(data, secret []byte) ([]byte, error) {
	return computeSig(data, secret), nil
}

func signRSA(data []byte, key *rsa.PrivateKey) ([]byte, error) {
	hash := sha256.Sum256(data)
	return rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
}

func signECDSA(data []byte, key *ecdsa.PrivateKey) ([]byte, error) {
	hash := sha256.Sum256(data)
	return ecdsa.SignASN1(rand.Reader, key, hash[:])
}

func signEd25519(data []byte, key ed25519.PrivateKey) ([]byte, error) {
	return ed25519.Sign(key, data), nil
}

func verifyRSA(data, sig []byte, key *rsa.PublicKey) error {
	hash := sha256.Sum256(data)
	return rsa.VerifyPKCS1v15(key, crypto.SHA256, hash[:], sig)
}

func verifyECDSA(data, sig []byte, key *ecdsa.PublicKey) bool {
	hash := sha256.Sum256(data)
	return ecdsa.VerifyASN1(key, hash[:], sig)
}

func verifyEd25519(data, sig []byte, key ed25519.PublicKey) bool {
	return ed25519.Verify(key, data, sig)
}

func verifyHMAC(data, sig, secret []byte) bool {
	expected := computeSig(data, secret)
	return hmac.Equal(sig, expected)
}

func computeSig(payload, secret []byte) []byte {
	h := hmac.New(sha256.New, secret)
	_, _ = h.Write(payload)
	return h.Sum(nil)
}

func splitToken(token string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(token); i++ {
		if token[i] == '.' {
			parts = append(parts, token[start:i])
			start = i + 1
		}
	}
	if start < len(token) {
		parts = append(parts, token[start:])
	}
	return parts
}
