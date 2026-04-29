package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

// CipherStore wraps a Store and encrypts values at rest.
type CipherStore struct {
	store Store
	key   []byte
}

// NewCipherStore creates a store that encrypts values with AES-GCM.
func NewCipherStore(store Store, key []byte) (*CipherStore, error) {
	if len(key) != 32 {
		return nil, errors.New("secrets: cipher key must be 32 bytes")
	}
	return &CipherStore{store: store, key: key}, nil
}

func (c *CipherStore) Set(key string, value []byte) error {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	ciphertext := gcm.Seal(nil, nonce, value, nil)
	return c.store.Set(key, append(nonce, ciphertext...))
}

func (c *CipherStore) Get(key string) ([]byte, error) {
	data, err := c.store.Get(key)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, errors.New("secrets: ciphertext too short")
	}
	plaintext, err := gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
	if err != nil {
		return nil, fmt.Errorf("secrets: decrypt failed: %w", err)
	}
	return plaintext, nil
}

func (c *CipherStore) Delete(key string) error {
	return c.store.Delete(key)
}

// PrefixStore adds a namespace prefix to keys.
type PrefixStore struct {
	store  Store
	prefix string
}

// NewPrefixStore wraps a Store and prepends the given prefix to all keys.
func NewPrefixStore(store Store, prefix string) *PrefixStore {
	return &PrefixStore{store: store, prefix: prefix}
}

func (p *PrefixStore) Set(key string, value []byte) error {
	return p.store.Set(p.prefix+key, value)
}

func (p *PrefixStore) Get(key string) ([]byte, error) {
	return p.store.Get(p.prefix + key)
}

func (p *PrefixStore) Delete(key string) error {
	return p.store.Delete(p.prefix + key)
}

// FallbackStore tries the primary store first, then falls back to secondary.
type FallbackStore struct {
	primary   Store
	secondary Store
}

// NewFallbackStore creates a store that tries primary first, then falls back to secondary.
func NewFallbackStore(primary, secondary Store) *FallbackStore {
	return &FallbackStore{primary: primary, secondary: secondary}
}

func (f *FallbackStore) Set(key string, value []byte) error {
	err := f.primary.Set(key, value)
	if err != nil {
		return f.secondary.Set(key, value)
	}
	return nil
}

func (f *FallbackStore) Get(key string) ([]byte, error) {
	v, err := f.primary.Get(key)
	if err == nil {
		return v, nil
	}
	return f.secondary.Get(key)
}

func (f *FallbackStore) Delete(key string) error {
	err1 := f.primary.Delete(key)
	err2 := f.secondary.Delete(key)
	if err1 != nil && err2 != nil {
		return err1
	}
	return nil
}
