package auth

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

var (
	// ErrInvalidToken indicates a malformed or unreadable token.
	ErrInvalidToken = errors.New("auth: invalid token")
	// ErrInvalidSignature indicates the token signature verification failed.
	ErrInvalidSignature = errors.New("auth: invalid signature")
	// ErrExpiredToken indicates the token has passed its expiration time.
	ErrExpiredToken = errors.New("auth: token expired")
	// ErrUnknownKeyID indicates the key ID in the token is not recognized.
	ErrUnknownKeyID = errors.New("auth: unknown key id")
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
	if time.Now().Unix() >= p.Exp {
		return ErrExpiredToken
	}
	return nil
}

func (c StandardClaims) Valid() error {
	if time.Now().Unix() >= c.Exp {
		return ErrExpiredToken
	}
	return nil
}

// Algorithm identifies a signing algorithm.
type Algorithm string

const (
	// AlgHMACSHA256 signs tokens using HMAC-SHA256.
	AlgHMACSHA256 Algorithm = "HS256"
	// AlgRS256 signs tokens using RSA-PKCS1v15 with SHA-256.
	AlgRS256 Algorithm = "RS256"
	// AlgES256 signs tokens using ECDSA with P-256 and SHA-256.
	AlgES256 Algorithm = "ES256"
	// AlgEdDSA signs tokens using Ed25519.
	AlgEdDSA Algorithm = "EdDSA"
)

// SignToken signs a payload with HMAC-SHA256.
func SignToken(p Payload, secret []byte) (string, error) {
	if err := ValidateHMACSecret(secret); err != nil {
		return "", err
	}
	return sign(p, secret, AlgHMACSHA256)
}

// VerifyToken verifies and decodes a HMAC-SHA256 token.
func VerifyToken(token string, secret []byte) (Payload, error) {
	if err := ValidateHMACSecret(secret); err != nil {
		return Payload{}, err
	}
	return verify[Payload](token, secret, AlgHMACSHA256)
}

// ValidateHMACSecret checks the minimum key size for HMAC-SHA256.
func ValidateHMACSecret(secret []byte) error {
	if len(secret) < sha256.Size {
		return fmt.Errorf("auth: HMAC secret must be at least %d bytes", sha256.Size)
	}
	return nil
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

const maxTokenSize = 16 << 10

// NewService creates a validated TokenService with one or more keys.
func NewService(keys ...Key) (TokenService, error) {
	if len(keys) == 0 {
		return nil, errors.New("auth: at least one key is required")
	}
	ids := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if key.ID == "" {
			return nil, errors.New("auth: key ID cannot be empty")
		}
		if _, exists := ids[key.ID]; exists {
			return nil, fmt.Errorf("auth: duplicate key ID %q", key.ID)
		}
		ids[key.ID] = struct{}{}
		if err := validateKey(key); err != nil {
			return nil, fmt.Errorf("auth: key %q: %w", key.ID, err)
		}
	}
	cloned := make([]Key, len(keys))
	for index, key := range keys {
		cloned[index] = cloneKey(key)
	}
	return &multiKeyService{keys: cloned}, nil
}

func cloneKey(key Key) Key {
	cloned := key
	cloned.Secret = append([]byte(nil), key.Secret...)
	switch key.Algorithm {
	case AlgRS256:
		private := key.Private.(*rsa.PrivateKey)
		privateClone := &rsa.PrivateKey{
			PublicKey: rsa.PublicKey{N: new(big.Int).Set(private.N), E: private.E},
			D:         new(big.Int).Set(private.D),
			Primes:    make([]*big.Int, len(private.Primes)),
		}
		for index, prime := range private.Primes {
			privateClone.Primes[index] = new(big.Int).Set(prime)
		}
		privateClone.Precompute()
		public := key.Public.(*rsa.PublicKey)
		cloned.Private = privateClone
		cloned.Public = &rsa.PublicKey{N: new(big.Int).Set(public.N), E: public.E}
	case AlgES256:
		private := key.Private.(*ecdsa.PrivateKey)
		public := key.Public.(*ecdsa.PublicKey)
		cloned.Private = &ecdsa.PrivateKey{
			PublicKey: ecdsa.PublicKey{
				Curve: private.Curve,
				X:     new(big.Int).Set(private.X),
				Y:     new(big.Int).Set(private.Y),
			},
			D: new(big.Int).Set(private.D),
		}
		cloned.Public = &ecdsa.PublicKey{
			Curve: public.Curve,
			X:     new(big.Int).Set(public.X),
			Y:     new(big.Int).Set(public.Y),
		}
	case AlgEdDSA:
		cloned.Private = append(ed25519.PrivateKey(nil), key.Private.(ed25519.PrivateKey)...)
		cloned.Public = append(ed25519.PublicKey(nil), key.Public.(ed25519.PublicKey)...)
	}
	return cloned
}

// MustNewService creates a TokenService or panics on invalid key configuration.
func MustNewService(keys ...Key) TokenService {
	service, err := NewService(keys...)
	if err != nil {
		panic(err)
	}
	return service
}

func validateKey(key Key) error {
	switch key.Algorithm {
	case AlgHMACSHA256:
		return ValidateHMACSecret(key.Secret)
	case AlgRS256:
		private, privateOK := key.Private.(*rsa.PrivateKey)
		public, publicOK := key.Public.(*rsa.PublicKey)
		if !privateOK || private == nil || !publicOK || public == nil {
			return errors.New("RS256 requires RSA private and public keys")
		}
		if err := private.Validate(); err != nil {
			return fmt.Errorf("invalid RSA private key: %w", err)
		}
		if private.N.BitLen() < 2048 {
			return errors.New("RS256 requires an RSA key of at least 2048 bits")
		}
		if private.PublicKey.E != public.E || private.PublicKey.N.Cmp(public.N) != 0 {
			return errors.New("RSA private and public keys do not match")
		}
	case AlgES256:
		private, privateOK := key.Private.(*ecdsa.PrivateKey)
		public, publicOK := key.Public.(*ecdsa.PublicKey)
		if !privateOK || private == nil || !publicOK || public == nil {
			return errors.New("ES256 requires ECDSA private and public keys")
		}
		if private.Curve != elliptic.P256() || public.Curve != elliptic.P256() {
			return errors.New("ES256 requires the P-256 curve")
		}
		if private.D == nil || private.D.Sign() <= 0 || private.D.Cmp(private.Curve.Params().N) >= 0 {
			return errors.New("invalid ECDSA private scalar")
		}
		derivedX, derivedY := private.Curve.ScalarBaseMult(private.D.Bytes())
		if private.X == nil || private.Y == nil || public.X == nil || public.Y == nil ||
			derivedX.Cmp(private.X) != 0 || derivedY.Cmp(private.Y) != 0 ||
			derivedX.Cmp(public.X) != 0 || derivedY.Cmp(public.Y) != 0 {
			return errors.New("ECDSA private and public keys do not match")
		}
	case AlgEdDSA:
		private, privateOK := key.Private.(ed25519.PrivateKey)
		public, publicOK := key.Public.(ed25519.PublicKey)
		if !privateOK || len(private) != ed25519.PrivateKeySize ||
			!publicOK || len(public) != ed25519.PublicKeySize {
			return errors.New("EdDSA requires Ed25519 private and public keys")
		}
		derived := private.Public().(ed25519.PublicKey)
		if !bytes.Equal(derived, public) {
			return errors.New("Ed25519 private and public keys do not match")
		}
	default:
		return fmt.Errorf("unsupported algorithm %q", key.Algorithm)
	}
	return nil
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
	if len(token) > maxTokenSize {
		return nil, fmt.Errorf("auth: signed token exceeds %d bytes", maxTokenSize)
	}

	return &SignedToken{
		Token:   token,
		KeyID:   key.ID,
		Payload: data,
		Raw:     token,
	}, nil
}

func verifyWithKey[T Claims](token string, key Key) (T, error) {
	var zero T

	parts, ok := splitToken(token)
	if !ok {
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

	token := base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(sig)
	if len(token) > maxTokenSize {
		return "", fmt.Errorf("auth: signed token exceeds %d bytes", maxTokenSize)
	}
	return token, nil
}

func verify[T any](token string, secret []byte, alg Algorithm) (T, error) {
	var zero T

	if len(token) == 0 || len(token) > maxTokenSize {
		return zero, ErrInvalidToken
	}
	payload, signature, ok := strings.Cut(token, ".")
	if !ok || payload == "" || signature == "" || strings.Contains(signature, ".") {
		return zero, ErrInvalidToken
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return zero, ErrInvalidToken
	}

	sig, err := base64.RawURLEncoding.DecodeString(signature)
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

func splitToken(token string) ([3]string, bool) {
	var parts [3]string
	if len(token) == 0 || len(token) > maxTokenSize {
		return parts, false
	}
	var ok bool
	parts[0], token, ok = strings.Cut(token, ".")
	if !ok {
		return parts, false
	}
	parts[1], parts[2], ok = strings.Cut(token, ".")
	if !ok || strings.Contains(parts[2], ".") ||
		parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return parts, false
	}
	return parts, true
}
