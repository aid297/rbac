package httpserver

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/aid297/rbac/kernal/rbac/persist"
	"github.com/aid297/rbac/kernal/rbac/policy"
)

const maxBody = 1 << 20

type api struct {
	store     *persist.Store
	caCertPEM []byte
}

// MountAPI registers the public /v1 REST API (plus /healthz, /v1/ca-cert) on an existing Gin router.
// Use this to embed RBAC routes in a larger Gin application.
func MountAPI(r gin.IRouter, store *persist.Store, caCertPEM []byte) {
	g := r.Group("")
	g.Use(pauseMiddleware())
	(&api{store: store, caCertPEM: caCertPEM}).registerRoutes(g)
}

// NewEngine builds a standalone Gin engine with the public API (Recovery + pause middleware).
func NewEngine(store *persist.Store, caCertPEM []byte) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	e := gin.New()
	e.Use(gin.Recovery())
	e.Use(requestLogMiddleware())
	MountAPI(e, store, caCertPEM)
	return e
}

// NewHandler returns the API as an http.Handler (same routes as NewEngine).
func NewHandler(store *persist.Store, caCertPEM []byte) http.Handler {
	return NewEngine(store, caCertPEM)
}

func (a *api) registerRoutes(e gin.IRoutes) {
	e.GET("/healthz", a.healthz)
	e.GET("/v1/ca-cert", a.getCACert)
	e.POST("/v1/enforce", a.enforce)
	e.GET("/v1/reachable", a.reachable)
	e.GET("/v1/bindings", a.getBindings)
	e.POST("/v1/bindings", a.addBinding)
	e.PUT("/v1/bindings", a.updateBinding)
	e.PATCH("/v1/bindings/enabled", a.setEnabled)
	e.DELETE("/v1/bindings", a.removeBinding)
}

func pauseMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/healthz" {
			c.Next()
			return
		}
		if paused, why := persist.Paused(); paused {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error":  "service paused",
				"reason": why,
			})
			return
		}
		c.Next()
	}
}

func (a *api) healthz(c *gin.Context) {
	paused, why := persist.Paused()
	if paused {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "paused", "reason": why})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (a *api) getCACert(c *gin.Context) {
	if len(a.caCertPEM) == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "CA certificate not available"})
		return
	}
	c.Data(http.StatusOK, "application/x-pem-file", a.caCertPEM)
}

type enforceReq struct {
	Subject   string     `json:"subject"`
	Target    string     `json:"target"`
	Scenarios []string   `json:"scenarios"`
	Now       *time.Time `json:"now"`
}

func (a *api) enforce(c *gin.Context) {
	var req enforceReq
	if err := decodeJSON(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Subject == "" || req.Target == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "subject and target required"})
		return
	}
	ok := a.store.Enforce(req.Subject, req.Target, evalCtx(req.Scenarios, req.Now))
	c.JSON(http.StatusOK, gin.H{"allow": ok})
}

func (a *api) reachable(c *gin.Context) {
	subject := strings.TrimSpace(c.Query("subject"))
	if subject == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "subject required"})
		return
	}
	scenarios := c.QueryArray("scenario")
	c.JSON(http.StatusOK, gin.H{
		"subject":   subject,
		"reachable": a.store.Reachable(subject, evalCtx(scenarios, nil)),
	})
}

func (a *api) getBindings(c *gin.Context) {
	src, dst := c.Query("src"), c.Query("dst")
	if src == "" && dst == "" {
		list := a.store.ListBindings()
		out := make([]bindingDTO, 0, len(list))
		for _, b := range list {
			out = append(out, bindingToDTO(b))
		}
		c.JSON(http.StatusOK, gin.H{"bindings": out})
		return
	}
	if src == "" || dst == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "src and dst required"})
		return
	}
	b, ok := a.store.GetBinding(src, dst, c.Query("scenario"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": policy.ErrBindingNotFound.Error()})
		return
	}
	c.JSON(http.StatusOK, bindingToDTO(b))
}

func (a *api) addBinding(c *gin.Context) {
	b, err := readBinding(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := a.store.AddBinding(b); err != nil {
		writePolicyErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, bindingToDTO(b))
}

func (a *api) updateBinding(c *gin.Context) {
	b, err := readBinding(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := a.store.UpdateBinding(b); err != nil {
		writePolicyErr(c, err)
		return
	}
	c.JSON(http.StatusOK, bindingToDTO(b))
}

type enabledReq struct {
	Src      string `json:"src"`
	Dst      string `json:"dst"`
	Scenario string `json:"scenario"`
	Enabled  bool   `json:"enabled"`
}

func (a *api) setEnabled(c *gin.Context) {
	var req enabledReq
	if err := decodeJSON(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Src == "" || req.Dst == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "src and dst required"})
		return
	}
	if err := a.store.SetEnabled(req.Src, req.Dst, req.Scenario, req.Enabled); err != nil {
		writePolicyErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (a *api) removeBinding(c *gin.Context) {
	src, dst := c.Query("src"), c.Query("dst")
	if src == "" || dst == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "src and dst required"})
		return
	}
	if err := a.store.RemoveBinding(src, dst, c.Query("scenario")); err != nil {
		writePolicyErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func evalCtx(scenarios []string, now *time.Time) *policy.EvalContext {
	ctx := &policy.EvalContext{Scenarios: scenarios}
	if now != nil {
		ctx.Now = *now
	}
	return ctx
}

func writePolicyErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, policy.ErrDuplicateBinding):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, policy.ErrBindingNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, policy.ErrInvalidResource), errors.Is(err, policy.ErrInvalidTimeRange), errors.Is(err, policy.ErrInvalidConditionMix), errors.Is(err, policy.ErrParseLine):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

func decodeJSON(c *gin.Context, dst any) error {
	defer c.Request.Body.Close()
	dec := json.NewDecoder(io.LimitReader(c.Request.Body, maxBody))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
