package crypto

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
)

const (
	NameAES256GCM = "aes-256-gcm"
	NameAES128GCM = "aes-128-gcm"
	NameSM4       = "sm4"
	DefaultName   = NameAES256GCM

	encHeader = "# rbac-enc v1\n"
)

var (
	ErrUnknownAlg = errors.New("crypto: unknown algorithm")
	ErrKeySize    = errors.New("crypto: key size")
	ErrCiphertext = errors.New("crypto: ciphertext")
	ErrEnvelope   = errors.New("crypto: envelope")
)

// Algorithm is a pluggable AEAD. Implementations must be stateless.
type Algorithm interface {
	Name() string
	KeySize() int
	GenerateKey() ([]byte, error)
	Encrypt(key, plaintext []byte) ([]byte, error)
	Decrypt(key, ciphertext []byte) ([]byte, error)
}

var (
	regMu sync.RWMutex
	reg   = map[string]Algorithm{}
)

func Register(a Algorithm) {
	if a == nil || a.Name() == "" {
		panic("crypto: invalid algorithm")
	}
	regMu.Lock()
	reg[a.Name()] = a
	regMu.Unlock()
}

func Lookup(name string) (Algorithm, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = DefaultName
	}
	regMu.RLock()
	a := reg[name]
	regMu.RUnlock()
	if a == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnknownAlg, name)
	}
	return a, nil
}

func Default() Algorithm {
	a, err := Lookup(DefaultName)
	if err != nil {
		panic(err)
	}
	return a
}

func Names() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(reg))
	for n := range reg {
		out = append(out, n)
	}
	return out
}

func IsSealed(data []byte) bool {
	return bytes.HasPrefix(data, []byte(encHeader))
}

func Seal(alg Algorithm, key, plaintext []byte) ([]byte, error) {
	payload, err := alg.Encrypt(key, plaintext)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString(encHeader)
	b.WriteString(alg.Name())
	b.WriteByte('\n')
	b.WriteString(base64.StdEncoding.EncodeToString(payload))
	b.WriteByte('\n')
	return []byte(b.String()), nil
}

func EnvelopeAlg(wrapped []byte) (string, error) {
	_, name, _, err := splitEnvelope(wrapped)
	return name, err
}

func Open(key, wrapped []byte) ([]byte, error) {
	alg, _, payload, err := splitEnvelope(wrapped)
	if err != nil {
		return nil, err
	}
	return alg.Decrypt(key, payload)
}

func splitEnvelope(wrapped []byte) (Algorithm, string, []byte, error) {
	if !IsSealed(wrapped) {
		return nil, "", nil, ErrEnvelope
	}
	body := string(wrapped[len(encHeader):])
	algName, rest, ok := strings.Cut(strings.TrimSpace(body), "\n")
	if !ok || algName == "" || rest == "" {
		return nil, "", nil, ErrEnvelope
	}
	alg, err := Lookup(algName)
	if err != nil {
		return nil, "", nil, err
	}
	payload, err := base64.StdEncoding.DecodeString(strings.TrimSpace(rest))
	if err != nil {
		return nil, "", nil, fmt.Errorf("%w: %v", ErrEnvelope, err)
	}
	return alg, algName, payload, nil
}
