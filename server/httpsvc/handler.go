package httpsvc

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aid297/rbac/server/persist"
	"github.com/aid297/rbac/server/policy"
)

const maxBody = 1 << 20

type api struct {
	store     *persist.Store
	caCertPEM []byte
}

func NewHandler(store *persist.Store, caCertPEM []byte) http.Handler {
	a := &api{store: store, caCertPEM: caCertPEM}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.healthz)
	mux.HandleFunc("GET /v1/ca-cert", a.getCACert)
	mux.HandleFunc("POST /v1/enforce", a.enforce)
	mux.HandleFunc("GET /v1/reachable", a.reachable)
	mux.HandleFunc("GET /v1/bindings", a.getBindings)
	mux.HandleFunc("POST /v1/bindings", a.addBinding)
	mux.HandleFunc("PUT /v1/bindings", a.updateBinding)
	mux.HandleFunc("PATCH /v1/bindings/enabled", a.setEnabled)
	mux.HandleFunc("DELETE /v1/bindings", a.removeBinding)
	return pauseMux(mux)
}

func pauseMux(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paused, why := persist.Paused()
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if paused {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error":  "service paused",
				"reason": why,
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *api) healthz(w http.ResponseWriter, _ *http.Request) {
	paused, why := persist.Paused()
	if paused {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "paused", "reason": why})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *api) getCACert(w http.ResponseWriter, _ *http.Request) {
	if len(a.caCertPEM) == 0 {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "CA certificate not available"})
		return
	}
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(a.caCertPEM)
}

type enforceReq struct {
	Subject   string     `json:"subject"`
	Target    string     `json:"target"`
	Scenarios []string   `json:"scenarios"`
	Now       *time.Time `json:"now"`
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
	ok := a.store.Enforce(req.Subject, req.Target, evalCtx(req.Scenarios, req.Now))
	writeJSON(w, http.StatusOK, map[string]any{"allow": ok})
}

func (a *api) reachable(w http.ResponseWriter, r *http.Request) {
	subject := strings.TrimSpace(r.URL.Query().Get("subject"))
	if subject == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "subject required"})
		return
	}
	scenarios := r.URL.Query()["scenario"]
	writeJSON(w, http.StatusOK, map[string]any{
		"subject":   subject,
		"reachable": a.store.Reachable(subject, evalCtx(scenarios, nil)),
	})
}

func (a *api) getBindings(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	src, dst := q.Get("src"), q.Get("dst")
	if src == "" && dst == "" {
		list := a.store.ListBindings()
		out := make([]bindingDTO, 0, len(list))
		for _, b := range list {
			out = append(out, bindingToDTO(b))
		}
		writeJSON(w, http.StatusOK, map[string]any{"bindings": out})
		return
	}
	if src == "" || dst == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "src and dst required"})
		return
	}
	b, ok := a.store.GetBinding(src, dst, q.Get("scenario"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": policy.ErrBindingNotFound.Error()})
		return
	}
	writeJSON(w, http.StatusOK, bindingToDTO(b))
}

func (a *api) addBinding(w http.ResponseWriter, r *http.Request) {
	b, err := readBinding(r)
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
	b, err := readBinding(r)
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

type enabledReq struct {
	Src      string `json:"src"`
	Dst      string `json:"dst"`
	Scenario string `json:"scenario"`
	Enabled  bool   `json:"enabled"`
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
	q := r.URL.Query()
	src, dst := q.Get("src"), q.Get("dst")
	if src == "" || dst == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "src and dst required"})
		return
	}
	if err := a.store.RemoveBinding(src, dst, q.Get("scenario")); err != nil {
		writePolicyErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func evalCtx(scenarios []string, now *time.Time) *policy.EvalContext {
	ctx := &policy.EvalContext{Scenarios: scenarios}
	if now != nil {
		ctx.Now = *now
	}
	return ctx
}

func writePolicyErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, policy.ErrDuplicateBinding):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, policy.ErrBindingNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, policy.ErrInvalidResource), errors.Is(err, policy.ErrInvalidTimeRange), errors.Is(err, policy.ErrInvalidConditionMix), errors.Is(err, policy.ErrParseLine):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBody))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
