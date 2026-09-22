package rbac

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testClient(t *testing.T, h http.HandlerFunc, opts ...Option) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := NewClient(srv.URL, opts...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c, srv
}

func writeResp(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// recorded captures the shape of one inbound request for assertions.
type recorded struct {
	method string
	path   string
	query  map[string][]string
	body   []byte
	header http.Header
}

func recorder(status int, body string, rec *recorded) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.method = r.Method
		rec.path = r.URL.Path
		rec.query = r.URL.Query()
		rec.body = b
		rec.header = r.Header.Clone()
		writeResp(w, status, body)
	}
}

func TestNewClientValidation(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		opts    []Option
		wantErr bool
	}{
		{"http ok", "http://localhost:8080", nil, false},
		{"https ok", "https://localhost:8443", nil, false},
		{"empty", "", nil, true},
		{"bad scheme", "ftp://localhost", nil, true},
		{"no host", "http://", nil, true},
		{"nil http client", "http://localhost", []Option{WithHTTPClient(nil)}, true},
		{"empty ca pem", "https://localhost", []Option{WithCACert(nil)}, true},
		{"bad ca pem", "https://localhost", []Option{WithCACert([]byte("not-pem"))}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewClient(tc.baseURL, tc.opts...)
			if (err != nil) != tc.wantErr {
				t.Fatalf("NewClient(%q) err=%v wantErr=%v", tc.baseURL, err, tc.wantErr)
			}
		})
	}
}

func TestHealth(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeResp(w, http.StatusOK, `{"status":"ok"}`)
		})
		if err := c.Health(context.Background()); err != nil {
			t.Fatalf("Health: %v", err)
		}
	})
	t.Run("paused", func(t *testing.T) {
		c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeResp(w, http.StatusServiceUnavailable, `{"status":"paused","reason":"disk mismatch"}`)
		})
		err := c.Health(context.Background())
		if !IsPaused(err) {
			t.Fatalf("want paused, got %v", err)
		}
		var ae *APIError
		if !errors.As(err, &ae) || ae.Message != "disk mismatch" {
			t.Fatalf("want reason as message, got %v", err)
		}
	})
}

func TestEnforce(t *testing.T) {
	t.Run("allow with options", func(t *testing.T) {
		var rec recorded
		c, _ := testClient(t, recorder(http.StatusOK, `{"allow":true}`, &rec))
		now := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
		ok, err := c.Enforce(context.Background(), "alice", "doc:42",
			WithScenarios([]string{"VIP", "EU"}), WithNow(now))
		if err != nil {
			t.Fatalf("Enforce: %v", err)
		}
		if !ok {
			t.Fatal("want allow true")
		}
		if rec.method != http.MethodPost || rec.path != "/v1/enforce" {
			t.Fatalf("bad request shape: %s %s", rec.method, rec.path)
		}
		var body struct {
			Subject   string    `json:"subject"`
			Target    string    `json:"target"`
			Scenarios []string  `json:"scenarios"`
			Now       time.Time `json:"now"`
		}
		if err := json.Unmarshal(rec.body, &body); err != nil {
			t.Fatalf("decode sent body: %v", err)
		}
		if body.Subject != "alice" || body.Target != "doc:42" {
			t.Fatalf("bad subject/target: %+v", body)
		}
		if len(body.Scenarios) != 2 || body.Scenarios[0] != "VIP" {
			t.Fatalf("bad scenarios: %+v", body.Scenarios)
		}
		if !body.Now.Equal(now) {
			t.Fatalf("bad now: %v", body.Now)
		}
	})
	t.Run("deny omits options", func(t *testing.T) {
		var rec recorded
		c, _ := testClient(t, recorder(http.StatusOK, `{"allow":false}`, &rec))
		ok, err := c.Enforce(context.Background(), "a", "b")
		if err != nil || ok {
			t.Fatalf("want deny, got ok=%v err=%v", ok, err)
		}
		var raw map[string]json.RawMessage
		_ = json.Unmarshal(rec.body, &raw)
		if _, present := raw["scenarios"]; present {
			t.Fatal("scenarios should be omitted when empty")
		}
		if _, present := raw["now"]; present {
			t.Fatal("now should be omitted when unset")
		}
	})
}

func TestReachable(t *testing.T) {
	var rec recorded
	c, _ := testClient(t, recorder(http.StatusOK,
		`{"subject":"alice","reachable":["alice","doc:42"]}`, &rec))
	got, err := c.Reachable(context.Background(), "alice", WithScenarios([]string{"VIP", "EU"}))
	if err != nil {
		t.Fatalf("Reachable: %v", err)
	}
	if len(got) != 2 || got[0] != "alice" || got[1] != "doc:42" {
		t.Fatalf("bad result: %v", got)
	}
	if rec.query["subject"][0] != "alice" {
		t.Fatalf("bad subject query: %v", rec.query)
	}
	if sc := rec.query["scenario"]; len(sc) != 2 || sc[0] != "VIP" || sc[1] != "EU" {
		t.Fatalf("bad scenario query: %v", sc)
	}
}

func TestBindingCRUD(t *testing.T) {
	ctx := context.Background()
	sample := `{"src":"alice","dst":"role:editor","scenario":"","enabled":true,"conditions":[{"kind":"ALL"}]}`

	t.Run("ListBindings", func(t *testing.T) {
		c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeResp(w, http.StatusOK, `{"bindings":[`+sample+`]}`)
		})
		bs, err := c.ListBindings(ctx)
		if err != nil {
			t.Fatalf("ListBindings: %v", err)
		}
		if len(bs) != 1 || bs[0].Src != "alice" {
			t.Fatalf("bad list: %+v", bs)
		}
	})

	t.Run("GetBinding ok", func(t *testing.T) {
		var rec recorded
		c, _ := testClient(t, recorder(http.StatusOK, sample, &rec))
		b, err := c.GetBinding(ctx, "alice", "role:editor", "")
		if err != nil {
			t.Fatalf("GetBinding: %v", err)
		}
		if b.Dst != "role:editor" {
			t.Fatalf("bad binding: %+v", b)
		}
		if rec.query["src"][0] != "alice" || rec.query["dst"][0] != "role:editor" {
			t.Fatalf("bad query: %v", rec.query)
		}
	})

	t.Run("GetBinding not found", func(t *testing.T) {
		c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeResp(w, http.StatusNotFound, `{"error":"binding not found"}`)
		})
		_, err := c.GetBinding(ctx, "x", "y", "")
		if !IsNotFound(err) {
			t.Fatalf("want not found, got %v", err)
		}
	})

	t.Run("AddBinding created", func(t *testing.T) {
		var rec recorded
		c, _ := testClient(t, recorder(http.StatusCreated, sample, &rec))
		b, err := c.AddBinding(ctx, Binding{
			Src: "alice", Dst: "role:editor", Enabled: true,
			Conditions: []Condition{AllCondition()},
		})
		if err != nil {
			t.Fatalf("AddBinding: %v", err)
		}
		if b.Src != "alice" {
			t.Fatalf("bad echo: %+v", b)
		}
		if rec.method != http.MethodPost {
			t.Fatalf("want POST, got %s", rec.method)
		}
		var sent Binding
		if err := json.Unmarshal(rec.body, &sent); err != nil {
			t.Fatalf("decode sent: %v", err)
		}
		if len(sent.Conditions) != 1 || sent.Conditions[0].Kind != KindAll {
			t.Fatalf("bad sent conditions: %+v", sent.Conditions)
		}
	})

	t.Run("AddBinding conflict", func(t *testing.T) {
		c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeResp(w, http.StatusConflict, `{"error":"binding (src,dst,scenario) exists"}`)
		})
		_, err := c.AddBinding(ctx, Binding{Src: "a", Dst: "b"})
		if !IsConflict(err) {
			t.Fatalf("want conflict, got %v", err)
		}
	})

	t.Run("UpdateBinding ok", func(t *testing.T) {
		var rec recorded
		c, _ := testClient(t, recorder(http.StatusOK, sample, &rec))
		if _, err := c.UpdateBinding(ctx, Binding{Src: "alice", Dst: "role:editor", Enabled: true}); err != nil {
			t.Fatalf("UpdateBinding: %v", err)
		}
		if rec.method != http.MethodPut {
			t.Fatalf("want PUT, got %s", rec.method)
		}
	})

	t.Run("SetEnabled", func(t *testing.T) {
		var rec recorded
		c, _ := testClient(t, recorder(http.StatusOK, `{"ok":true}`, &rec))
		if err := c.SetEnabled(ctx, "alice", "role:editor", "", false); err != nil {
			t.Fatalf("SetEnabled: %v", err)
		}
		if rec.method != http.MethodPatch || rec.path != "/v1/bindings/enabled" {
			t.Fatalf("bad shape: %s %s", rec.method, rec.path)
		}
		var body struct {
			Enabled bool `json:"enabled"`
		}
		_ = json.Unmarshal(rec.body, &body)
		if body.Enabled {
			t.Fatal("want enabled false in body")
		}
	})

	t.Run("RemoveBinding no content", func(t *testing.T) {
		var rec recorded
		c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			rec.method = r.Method
			rec.path = r.URL.Path
			rec.query = r.URL.Query()
			w.WriteHeader(http.StatusNoContent)
		})
		if err := c.RemoveBinding(ctx, "alice", "role:editor", "VIP"); err != nil {
			t.Fatalf("RemoveBinding: %v", err)
		}
		if rec.method != http.MethodDelete {
			t.Fatalf("want DELETE, got %s", rec.method)
		}
		if rec.query["scenario"][0] != "VIP" {
			t.Fatalf("bad scenario query: %v", rec.query)
		}
	})
}

func TestErrorMapping(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		check  func(error) bool
		msg    string
	}{
		{"bad request", http.StatusBadRequest, `{"error":"resource id contains reserved char"}`, IsBadRequest, "resource id contains reserved char"},
		{"not found", http.StatusNotFound, `{"error":"binding not found"}`, IsNotFound, "binding not found"},
		{"conflict", http.StatusConflict, `{"error":"binding (src,dst,scenario) exists"}`, IsConflict, "binding (src,dst,scenario) exists"},
		{"paused", http.StatusServiceUnavailable, `{"error":"service paused","reason":"x"}`, IsPaused, "service paused"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				writeResp(w, tc.status, tc.body)
			})
			err := c.Health(context.Background())
			if !tc.check(err) {
				t.Fatalf("predicate failed for %v", err)
			}
			var ae *APIError
			if !errors.As(err, &ae) {
				t.Fatalf("want *APIError, got %T", err)
			}
			if ae.Message != tc.msg || ae.StatusCode != tc.status {
				t.Fatalf("bad APIError: %+v", ae)
			}
		})
	}

	t.Run("non-json body preserved", func(t *testing.T) {
		c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, "boom")
		})
		err := c.Health(context.Background())
		var ae *APIError
		if !errors.As(err, &ae) {
			t.Fatalf("want *APIError, got %v", err)
		}
		if ae.Message != "" || string(ae.Body) != "boom" {
			t.Fatalf("bad APIError: %+v", ae)
		}
	})
}

func TestRequestHeaders(t *testing.T) {
	var rec recorded
	c, _ := testClient(t, recorder(http.StatusOK, `{"allow":true}`, &rec), WithUserAgent("custom/1.0"))
	if _, err := c.Enforce(context.Background(), "a", "b"); err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if got := rec.header.Get("User-Agent"); got != "custom/1.0" {
		t.Fatalf("bad User-Agent: %q", got)
	}
	if got := rec.header.Get("Accept"); got != "application/json" {
		t.Fatalf("bad Accept: %q", got)
	}
	if got := rec.header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("bad Content-Type: %q", got)
	}
}

func TestContextCancellation(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeResp(w, http.StatusOK, `{"allow":true}`)
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Enforce(ctx, "a", "b"); err == nil {
		t.Fatal("want error from cancelled context")
	}
}
