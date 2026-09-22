package rbac

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Version is the SDK version, reported in the default User-Agent header.
const Version = "0.1.0"

const defaultUserAgent = "rbac-sdk-go/" + Version

// maxResponseBytes caps how much of a response body is read, to avoid memory
// amplification from a misbehaving service.
const maxResponseBytes = 4 << 20

// Client is a concurrency-safe client for the rbac microservice /v1 API.
// Construct it with NewClient.
type Client struct {
	baseURL   *url.URL
	hc        *http.Client
	userAgent string
}

// NewClient builds a Client for the service at baseURL (http or https).
// baseURL should be an origin (e.g. "http://localhost:8080"); any existing
// path is preserved and API paths are appended via url.JoinPath.
func NewClient(baseURL string, opts ...Option) (*Client, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, fmt.Errorf("rbac: invalid base URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("rbac: base URL scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("rbac: base URL must include a host")
	}
	cfg := &clientConfig{userAgent: defaultUserAgent}
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, err
		}
	}
	if u.Scheme == "http" && !cfg.httpClientSet && (len(cfg.caCerts) > 0 || cfg.insecure) {
		return nil, fmt.Errorf("rbac: TLS options (WithCACert, WithInsecureSkipVerify) have no effect with http:// base URL")
	}
	hc, err := cfg.buildClient(u.Scheme)
	if err != nil {
		return nil, err
	}
	if cfg.userAgent == "" {
		cfg.userAgent = defaultUserAgent
	}
	return &Client{baseURL: u, hc: hc, userAgent: cfg.userAgent}, nil
}

// Health checks service liveness. It returns nil on 200, or an *APIError
// (IsPaused) when the service is paused.
func (c *Client) Health(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/healthz", nil, nil, nil)
}

// Enforce reports whether subject can reach target under the given call options.
func (c *Client) Enforce(ctx context.Context, subject, target string, opts ...CallOption) (bool, error) {
	p := &callParams{}
	for _, o := range opts {
		o(p)
	}
	body := struct {
		Subject   string     `json:"subject"`
		Target    string     `json:"target"`
		Scenarios []string   `json:"scenarios,omitempty"`
		Now       *time.Time `json:"now,omitempty"`
	}{Subject: subject, Target: target, Scenarios: p.scenarios, Now: p.now}
	var out struct {
		Allow bool `json:"allow"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/enforce", nil, body, &out); err != nil {
		return false, err
	}
	return out.Allow, nil
}

// Reachable lists every node subject can reach, including subject itself.
func (c *Client) Reachable(ctx context.Context, subject string, opts ...CallOption) ([]string, error) {
	p := &callParams{}
	for _, o := range opts {
		o(p)
	}
	q := url.Values{}
	q.Set("subject", subject)
	for _, s := range p.scenarios {
		q.Add("scenario", s)
	}
	var out struct {
		Reachable []string `json:"reachable"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/reachable", q, nil, &out); err != nil {
		return nil, err
	}
	return out.Reachable, nil
}

// ListBindings returns all bindings.
func (c *Client) ListBindings(ctx context.Context) ([]Binding, error) {
	var out struct {
		Bindings []Binding `json:"bindings"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/bindings", nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Bindings, nil
}

// GetBinding fetches a single binding, or an *APIError (IsNotFound) if absent.
func (c *Client) GetBinding(ctx context.Context, src, dst, scenario string) (Binding, error) {
	q := bindingQuery(src, dst, scenario)
	var out Binding
	if err := c.do(ctx, http.MethodGet, "/v1/bindings", q, nil, &out); err != nil {
		return Binding{}, err
	}
	return out, nil
}

// AddBinding creates a binding. A duplicate returns an *APIError (IsConflict).
func (c *Client) AddBinding(ctx context.Context, b Binding) (Binding, error) {
	var out Binding
	if err := c.do(ctx, http.MethodPost, "/v1/bindings", nil, b, &out); err != nil {
		return Binding{}, err
	}
	return out, nil
}

// UpdateBinding replaces an existing binding. A missing binding returns an
// *APIError (IsNotFound); it is not created.
func (c *Client) UpdateBinding(ctx context.Context, b Binding) (Binding, error) {
	var out Binding
	if err := c.do(ctx, http.MethodPut, "/v1/bindings", nil, b, &out); err != nil {
		return Binding{}, err
	}
	return out, nil
}

// SetEnabled toggles a binding's enabled flag.
func (c *Client) SetEnabled(ctx context.Context, src, dst, scenario string, enabled bool) error {
	body := struct {
		Src      string `json:"src"`
		Dst      string `json:"dst"`
		Scenario string `json:"scenario"`
		Enabled  bool   `json:"enabled"`
	}{Src: src, Dst: dst, Scenario: scenario, Enabled: enabled}
	return c.do(ctx, http.MethodPatch, "/v1/bindings/enabled", nil, body, nil)
}

// RemoveBinding deletes a binding, or returns an *APIError (IsNotFound) if absent.
func (c *Client) RemoveBinding(ctx context.Context, src, dst, scenario string) error {
	q := bindingQuery(src, dst, scenario)
	return c.do(ctx, http.MethodDelete, "/v1/bindings", q, nil, nil)
}

func bindingQuery(src, dst, scenario string) url.Values {
	q := url.Values{}
	q.Set("src", src)
	q.Set("dst", dst)
	if scenario != "" {
		q.Set("scenario", scenario)
	}
	return q
}

// do issues one request and decodes a 2xx JSON response into out (when non-nil).
// Non-2xx responses become an *APIError; transport errors are returned as-is.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u := *c.baseURL
	u = *u.JoinPath(path)
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("rbac: encode request: %w", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rdr)
	if err != nil {
		return fmt.Errorf("rbac: build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("rbac: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return newAPIError(method, path, resp.StatusCode, data)
	}
	if out != nil && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("rbac: decode response: %w", err)
		}
	}
	return nil
}

func newAPIError(method, path string, status int, data []byte) *APIError {
	ae := &APIError{StatusCode: status, Method: method, Path: path, Body: data}
	var e struct {
		Error  string `json:"error"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(data, &e); err == nil {
		switch {
		case e.Error != "":
			ae.Message = e.Error
		case e.Reason != "":
			ae.Message = e.Reason
		}
	}
	return ae
}
