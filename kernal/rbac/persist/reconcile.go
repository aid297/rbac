package persist

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/aid297/rbac/kernal/rbac/crypto"
)

const EnvPolicyKeyPrev = "RBAC_POLICY_KEY_PREV"

var (
	ErrCryptoMismatch = errors.New("persist: crypto settings do not match stored data")
)

// ServiceControl is implemented by subsystem ⑤ to drain traffic during crypto migration.
type ServiceControl interface {
	Pause(reason string) error
	Resume() error
}

var (
	ctlMu      sync.Mutex
	serviceCtl ServiceControl
	paused     bool
	pauseWhy   string
)

func SetServiceControl(c ServiceControl) {
	ctlMu.Lock()
	serviceCtl = c
	ctlMu.Unlock()
}

func Paused() (bool, string) {
	ctlMu.Lock()
	defer ctlMu.Unlock()
	return paused, pauseWhy
}

func requestPause(reason string) {
	ctlMu.Lock()
	paused = true
	pauseWhy = reason
	ctl := serviceCtl
	ctlMu.Unlock()
	logWarn("service paused", "reason", reason)
	if ctl != nil {
		_ = ctl.Pause(reason)
	}
}

func requestResume() {
	ctlMu.Lock()
	was := paused
	pauseWhy = ""
	paused = false
	ctl := serviceCtl
	ctlMu.Unlock()
	if was {
		logInfo("service resumed")
	}
	if ctl != nil {
		_ = ctl.Resume()
	}
}

// EnterPause is used by crypto migration and by the HTTP layer tests.
func EnterPause(reason string) { requestPause(reason) }

// LeavePause clears the process-wide pause flag.
func LeavePause() { requestResume() }

func prevKeyFromEnv() []byte {
	s := strings.TrimSpace(os.Getenv(EnvPolicyKeyPrev))
	if s == "" {
		return nil
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil
	}
	return b
}

func (s *Store) wantEncrypt() bool {
	return s.cipher != nil && len(s.key) > 0
}

func (s *Store) openBlob(data []byte) ([]byte, error) {
	if !crypto.IsSealed(data) {
		return data, nil
	}
	for _, k := range [][]byte{s.key, s.prevKey} {
		if len(k) == 0 {
			continue
		}
		pt, err := crypto.Open(k, data)
		if err == nil {
			return pt, nil
		}
	}
	return nil, fmt.Errorf("%w: cannot decrypt with current or previous key", ErrCryptoMismatch)
}

func (s *Store) diskMismatch(data []byte) (bool, string, error) {
	sealed := crypto.IsSealed(data)
	want := s.wantEncrypt()
	if !sealed && !want {
		return false, "", nil
	}
	if !sealed && want {
		return true, "plaintext store, encryption enabled", nil
	}
	if sealed && !want {
		if _, err := s.openBlob(data); err != nil {
			return false, "", err
		}
		return true, "encrypted store, encryption disabled", nil
	}
	name, err := crypto.EnvelopeAlg(data)
	if err != nil {
		return false, "", err
	}
	if _, err := crypto.Open(s.key, data); err == nil {
		if s.cipher != nil && name != s.cipher.Name() {
			return true, "algorithm changed", nil
		}
		return false, "", nil
	}
	if _, err := s.openBlob(data); err != nil {
		return false, "", err
	}
	return true, "key changed", nil
}

// Reconcile compares the on-disk blob with current encrypt/alg/key and migrates if needed.
func (s *Store) Reconcile() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	need, reason, err := s.diskMismatch(data)
	if err != nil {
		logError("reconcile failed", "path", s.path, "error", err)
		requestPause(err.Error())
		return err
	}
	if !need {
		logInfo("reconcile ok", "path", s.path)
		return nil
	}
	logWarn("reconcile migrating policy file", "path", s.path, "reason", reason)
	requestPause(reason)
	if err := s.saveLocked(); err != nil {
		logError("reconcile save failed", "path", s.path, "error", err)
		return err
	}
	if err := s.loadLocked(); err != nil {
		logError("reconcile reload failed", "path", s.path, "error", err)
		return err
	}
	if err := s.cacheSetLocked(); err != nil {
		logError("reconcile cache sync failed", "path", s.path, "error", err)
		return err
	}
	requestResume()
	logInfo("reconcile migration completed", "path", s.path, "reason", reason)
	return nil
}

// Reconfigure applies new crypto settings to the in-memory snapshot and rewrites disk.
func (s *Store) Reconfigure(alg crypto.Algorithm, key []byte) error {
	logWarn("crypto reconfigure started", "path", s.path, "algorithm", algName(alg))
	requestPause("crypto reconfigure")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prevKey = append([]byte(nil), s.key...)
	s.cipher = alg
	s.key = append([]byte(nil), key...)
	if err := s.saveLocked(); err != nil {
		logError("crypto reconfigure save failed", "path", s.path, "error", err)
		return err
	}
	if err := s.loadLocked(); err != nil {
		logError("crypto reconfigure reload failed", "path", s.path, "error", err)
		return err
	}
	if err := s.cacheSetLocked(); err != nil {
		logError("crypto reconfigure cache sync failed", "path", s.path, "error", err)
		return err
	}
	requestResume()
	logInfo("crypto reconfigure completed", "path", s.path, "algorithm", algName(alg))
	return nil
}

func algName(alg crypto.Algorithm) string {
	if alg == nil {
		return "none"
	}
	return alg.Name()
}
