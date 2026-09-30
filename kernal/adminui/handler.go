package adminui

import (
	"crypto/subtle"
	"io/fs"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/aid297/rbac/kernal/rbac/persist"
	"github.com/aid297/rbac/kernal/rbac/policy"
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
	store  *persist.Store
	creds  *Creds
	static fs.FS
}

// NewHandler serves JSON under /api/* and the embedded SPA at / and /assets/*.
// staticRoot must contain a "static" directory (Vite outDir), unless testing with a custom layout.
func NewHandler(store *persist.Store, creds *Creds, staticRoot fs.FS) http.Handler {
	sub, err := fs.Sub(staticRoot, "static")
	if err != nil {
		sub = staticRoot
	}
	a := &api{store: store, creds: creds, static: sub}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", a.serveIndex)
	if assets, err := fs.Sub(a.static, "assets"); err == nil {
		mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assets))))
	}
	mux.HandleFunc("GET /api/bindings", a.listBindings)
	mux.HandleFunc("POST /api/bindings", a.addBinding)
	mux.HandleFunc("PUT /api/bindings", a.updateBinding)
	mux.HandleFunc("DELETE /api/bindings", a.removeBinding)
	mux.HandleFunc("PATCH /api/bindings/enabled", a.setEnabled)
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
			http.Error(w, "未授权", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *api) serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	b, err := fs.ReadFile(a.static, "index.html")
	if err != nil {
		http.Error(w, "管理界面未构建：请在 adminui/web 执行 npm run build", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}

func (a *api) listBindings(w http.ResponseWriter, r *http.Request) {
	src, dst := r.URL.Query().Get("src"), r.URL.Query().Get("dst")
	if src != "" && dst != "" {
		b, ok := a.store.GetBinding(src, dst, r.URL.Query().Get("scenario"))
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": policy.ErrBindingNotFound.Error()})
			return
		}
		writeJSON(w, http.StatusOK, bindingToDTO(b))
		return
	}
	list := a.store.ListBindings()
	out := make([]bindingDTO, 0, len(list))
	for _, b := range list {
		out = append(out, bindingToDTO(b))
	}
	writeJSON(w, http.StatusOK, map[string]any{"bindings": out})
}

func (a *api) addBinding(w http.ResponseWriter, r *http.Request) {
	b, err := readBindingJSON(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := a.store.AddBinding(b); err != nil {
		writePolicyErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, bindingToDTO(b))
}

func (a *api) updateBinding(w http.ResponseWriter, r *http.Request) {
	b, err := readBindingJSON(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := a.store.UpdateBinding(b); err != nil {
		writePolicyErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bindingToDTO(b))
}

func (a *api) setEnabled(w http.ResponseWriter, r *http.Request) {
	var req enabledReq
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Src == "" || req.Dst == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "src and dst required"})
		return
	}
	if err := a.store.SetEnabled(req.Src, req.Dst, req.Scenario, req.Enabled); err != nil {
		writePolicyErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *api) removeBinding(w http.ResponseWriter, r *http.Request) {
	src, dst := r.URL.Query().Get("src"), r.URL.Query().Get("dst")
	if src == "" || dst == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "src and dst required"})
		return
	}
	if err := a.store.RemoveBinding(src, dst, r.URL.Query().Get("scenario")); err != nil {
		writePolicyErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

