package adminui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aid297/rbac/server/config"
	"github.com/aid297/rbac/server/persist"
	"github.com/aid297/rbac/server/policy"
)

func TestBasicAuthAndQuery(t *testing.T) {
	s, err := persist.Open(filepath.Join(t.TempDir(), persist.DefaultFile))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.AddBinding(policy.Binding{
		Src: "u", Dst: "r", Enabled: true,
		Conditions: []policy.Condition{policy.AllCondition{}},
	}); err != nil {
		t.Fatal(err)
	}
	creds := NewCreds("admin", "admin")
	ts := httptest.NewServer(NewHandler(s, creds, []byte("<html>ok</html>")))
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 got %d", res.StatusCode)
	}
	_ = res.Body.Close()

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/api/bindings", nil)
	req.SetBasicAuth("admin", "admin")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatal(res.Status)
	}
	var body struct {
		Bindings []bindingDTO `json:"bindings"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Bindings) != 1 || body.Bindings[0].Src != "u" {
		t.Fatalf("%+v", body.Bindings)
	}

	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/api/enforce", strings.NewReader(`{"subject":"u","target":"r"}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "admin")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var en struct {
		Allow bool `json:"allow"`
	}
	if err := json.NewDecoder(res.Body).Decode(&en); err != nil {
		t.Fatal(err)
	}
	if !en.Allow {
		t.Fatal("enforce")
	}
}

func TestCredsLiveReplace(t *testing.T) {
	creds := NewCreds("admin", "admin")
	if !creds.Match("admin", "admin") {
		t.Fatal("default")
	}
	creds.Set("ops", "s3cret")
	if creds.Match("admin", "admin") {
		t.Fatal("old password must stop working")
	}
	if !creds.Match("ops", "s3cret") {
		t.Fatal("new password")
	}
}

func TestWatchReloadsPassword(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	adminYAML := func(user, pass string) string {
		return "admin:\n  enable: true\n  username: " + user + "\n  password: " + pass + "\n  host: 127.0.0.1\n  port: 9090\n"
	}
	if err := os.WriteFile(path, []byte(adminYAML("admin", "admin")), 0o600); err != nil {
		t.Fatal(err)
	}
	creds := NewCreds("admin", "admin")
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	if err := config.WatchFile(path, stop, func() {
		a, err := config.ReadAdmin(path)
		if err != nil {
			return
		}
		creds.Set(a.Username, a.Password)
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if err := os.WriteFile(path, []byte(adminYAML("admin", "n3wpass")), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if creds.Match("admin", "n3wpass") && !creds.Match("admin", "admin") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("password did not reload from config file")
		}
		time.Sleep(30 * time.Millisecond)
	}
}
