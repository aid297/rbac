package httpsvc

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/aid297/rbac/server/persist"
	"github.com/aid297/rbac/server/policy"
)

func testStore(t *testing.T) *persist.Store {
	t.Helper()
	persist.LeavePause()
	s, err := persist.Open(filepath.Join(t.TempDir(), persist.DefaultFile))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(); persist.LeavePause() })
	return s
}

func TestHealthzAndEnforce(t *testing.T) {
	s := testStore(t)
	if err := s.AddBinding(policy.Binding{
		Src: "u", Dst: "r", Enabled: true,
		Conditions: []policy.Condition{policy.AllCondition{}},
	}); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(NewHandler(s))
	t.Cleanup(ts.Close)

	res, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatal(res.Status)
	}
	_ = res.Body.Close()

	body := []byte(`{"subject":"u","target":"r"}`)
	res, err = http.Post(ts.URL+"/v1/enforce", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatal(res.Status)
	}
	var out struct {
		Allow bool `json:"allow"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if !out.Allow {
		t.Fatal("expected allow")
	}
}

func TestBindingsCRUD(t *testing.T) {
	s := testStore(t)
	ts := httptest.NewServer(NewHandler(s))
	t.Cleanup(ts.Close)

	res, err := http.Post(ts.URL+"/v1/bindings", "application/json", bytes.NewReader([]byte(`{"src":"a","dst":"b","enabled":true,"conditions":[{"kind":"ALL"}]}`)))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusCreated {
		t.Fatal(res.Status)
	}
	_ = res.Body.Close()

	res, err = http.Get(ts.URL + "/v1/bindings?src=a&dst=b")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatal(res.Status)
	}
	_ = res.Body.Close()

	req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/v1/bindings/enabled", bytes.NewReader([]byte(`{"src":"a","dst":"b","enabled":false}`)))
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatal(res.Status)
	}
	_ = res.Body.Close()
	if s.Enforce("a", "b", nil) {
		t.Fatal("disabled")
	}

	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/v1/bindings?src=a&dst=b", nil)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusNoContent {
		t.Fatal(res.Status)
	}
	_ = res.Body.Close()
}

func TestPauseReturns503(t *testing.T) {
	s := testStore(t)
	ts := httptest.NewServer(NewHandler(s))
	t.Cleanup(ts.Close)
	persist.EnterPause("crypto")
	res, err := http.Post(ts.URL+"/v1/enforce", "application/json", bytes.NewReader([]byte(`{"subject":"u","target":"r"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatal(res.Status)
	}
}
