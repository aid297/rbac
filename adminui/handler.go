package adminui

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"rbac/persist"
	"rbac/policy"
)

type pair struct {
	User string
	Pass string
}

// Creds holds the live admin username and password. Set may be called when
// the config file changes.
type Creds struct {
	cur atomic.Pointer[pair]
}

func NewCreds(user, pass string) *Creds {
	c := new(Creds)
	c.Set(user, pass)
	return c
}

func (c *Creds) Set(user, pass string) {
	if strings.TrimSpace(user) == "" {
		user = "admin"
	}
	if pass == "" {
		pass = "admin"
	}
	c.cur.Store(&pair{User: user, Pass: pass})
}

func (c *Creds) Match(user, pass string) bool {
	p := c.cur.Load()
	if p == nil {
		return false
	}
	uok := subtle.ConstantTimeCompare([]byte(user), []byte(p.User)) == 1
	pok := subtle.ConstantTimeCompare([]byte(pass), []byte(p.Pass)) == 1
	return uok && pok
}

type api struct {
	store *persist.Store
	creds *Creds
	page  []byte
}

func NewHandler(store *persist.Store, creds *Creds, page []byte) http.Handler {
	a := &api{store: store, creds: creds, page: page}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", a.index)
	mux.HandleFunc("GET /api/bindings", a.bindings)
	mux.HandleFunc("GET /api/policy", a.policy)
	mux.HandleFunc("POST /api/enforce", a.enforce)
	mux.HandleFunc("GET /api/reachable", a.reachable)
	return a.basicAuth(mux)
}

func (a *api) basicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || a.creds == nil || !a.creds.Match(u, p) {
			w.Header().Set("WWW-Authenticate", `Basic realm="rbac-admin"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *api) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(a.page)
}

func (a *api) bindings(w http.ResponseWriter, _ *http.Request) {
	list := a.store.ListBindings()
	out := make([]bindingDTO, 0, len(list))
	for _, b := range list {
		out = append(out, bindingToDTO(b))
	}
	writeJSON(w, http.StatusOK, map[string]any{"bindings": out})
}

func (a *api) policy(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"text": a.store.Serialize()})
}

type enforceReq struct {
	Subject   string   `json:"subject"`
	Target    string   `json:"target"`
	Scenarios []string `json:"scenarios"`
}

func (a *api) enforce(w http.ResponseWriter, r *http.Request) {
	var req enforceReq
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Subject == "" || req.Target == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "subject and target required"})
		return
	}
	ok := a.store.Enforce(req.Subject, req.Target, &policy.EvalContext{Scenarios: req.Scenarios})
	writeJSON(w, http.StatusOK, map[string]any{"allow": ok})
}

func (a *api) reachable(w http.ResponseWriter, r *http.Request) {
	subject := strings.TrimSpace(r.URL.Query().Get("subject"))
	if subject == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "subject required"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"subject":   subject,
		"reachable": a.store.Reachable(subject, &policy.EvalContext{Scenarios: r.URL.Query()["scenario"]}),
	})
}

type bindingDTO struct {
	Src        string         `json:"src"`
	Dst        string         `json:"dst"`
	Scenario   string         `json:"scenario"`
	Enabled    bool           `json:"enabled"`
	Conditions []conditionDTO `json:"conditions"`
}

type conditionDTO struct {
	Kind  string `json:"kind"`
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

func bindingToDTO(b policy.Binding) bindingDTO {
	dto := bindingDTO{Src: b.Src, Dst: b.Dst, Scenario: b.Scenario, Enabled: b.Enabled}
	if len(b.Conditions) == 0 {
		dto.Conditions = []conditionDTO{{Kind: "ALL"}}
		return dto
	}
	for _, c := range b.Conditions {
		switch t := c.(type) {
		case policy.AllCondition:
			dto.Conditions = append(dto.Conditions, conditionDTO{Kind: "ALL"})
		case policy.TimeCondition:
			cd := conditionDTO{Kind: "TIME"}
			if t.Start != nil {
				cd.Start = t.Start.UTC().Format(time.RFC3339)
			}
			if t.End != nil {
				cd.End = t.End.UTC().Format(time.RFC3339)
			}
			dto.Conditions = append(dto.Conditions, cd)
		}
	}
	return dto
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
