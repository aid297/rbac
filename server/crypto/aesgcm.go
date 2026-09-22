package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

func init() {
	Register(aesGCM{name: NameAES256GCM, keySize: 32})
	Register(aesGCM{name: NameAES128GCM, keySize: 16})
}

type aesGCM struct {
	name    string
	keySize int
}

func (a aesGCM) Name() string { return a.name }
func (a aesGCM) KeySize() int { return a.keySize }

func (a aesGCM) GenerateKey() ([]byte, error) {
	key := make([]byte, a.keySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	return key, nil
}

func (a aesGCM) Encrypt(key, plaintext []byte) ([]byte, error) {
	aead, err := newAESGCM(key, a.keySize)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plaintext, nil), nil
}

func (a aesGCM) Decrypt(key, ciphertext []byte) ([]byte, error) {
	aead, err := newAESGCM(key, a.keySize)
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

func newAESGCM(key []byte, want int) (cipher.AEAD, error) {
	if len(key) != want {
		return nil, ErrKeySize
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
