package crypto

import (
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"

	"github.com/emmansun/gmsm/sm4"
)

func init() {
	Register(sm4gcm{})
}

type sm4gcm struct{}

func (sm4gcm) Name() string { return NameSM4 }
func (sm4gcm) KeySize() int { return sm4.BlockSize }

func (sm4gcm) GenerateKey() ([]byte, error) {
	key := make([]byte, sm4.BlockSize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	return key, nil
}

func (sm4gcm) Encrypt(key, plaintext []byte) ([]byte, error) {
	aead, err := newSM4GCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plaintext, nil), nil
}

func (sm4gcm) Decrypt(key, ciphertext []byte) ([]byte, error) {
	aead, err := newSM4GCM(key)
	if err != nil {
		return nil, err
	}
	n := aead.NonceSize()
	if len(ciphertext) < n {
		return nil, ErrCiphertext
	}
	pt, err := aead.Open(nil, ciphertext[:n], ciphertext[n:], nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCiphertext, err)
	}
	return pt, nil
}

func newSM4GCM(key []byte) (cipher.AEAD, error) {
	if len(key) != sm4.BlockSize {
		return nil, ErrKeySize
	}
	block, err := sm4.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
