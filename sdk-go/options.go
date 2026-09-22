package rbac

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"time"
)

// defaultTimeout is applied when no WithTimeout or WithHTTPClient is set,
// preventing requests from hanging indefinitely.
const defaultTimeout = 30 * time.Second

type clientConfig struct {
	httpClient    *http.Client
	httpClientSet bool
	caCerts       [][]byte
	insecure      bool
	userAgent     string
	timeout       time.Duration
	timeoutSet    bool
}

// Option configures a Client at construction time.
type Option func(*clientConfig) error

// WithHTTPClient supplies your own *http.Client. When set, the TLS-related
// options (WithCACert*, WithInsecureSkipVerify) and WithTimeout are ignored;
// the provided client is used verbatim.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *clientConfig) error {
		if hc == nil {
			return fmt.Errorf("rbac: WithHTTPClient requires a non-nil client")
		}
		c.httpClient = hc
		c.httpClientSet = true
		return nil
	}
}

// WithCACert adds a PEM-encoded certificate to the trust pool used for HTTPS.
// Use it to trust the service's self-signed CA.
func WithCACert(pem []byte) Option {
	return func(c *clientConfig) error {
		if len(pem) == 0 {
			return fmt.Errorf("rbac: WithCACert requires non-empty PEM data")
		}
		c.caCerts = append(c.caCerts, append([]byte(nil), pem...))
		return nil
	}
}

// WithCACertFile adds a PEM certificate file to the trust pool used for HTTPS.
func WithCACertFile(path string) Option {
	return func(c *clientConfig) error {
		pem, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("rbac: read CA file %q: %w", path, err)
		}
		return WithCACert(pem)(c)
	}
}

// WithInsecureSkipVerify disables TLS certificate verification. Intended for
// testing only.
func WithInsecureSkipVerify(insecure bool) Option {
	return func(c *clientConfig) error {
		c.insecure = insecure
		return nil
	}
}

// WithUserAgent overrides the default User-Agent header.
func WithUserAgent(ua string) Option {
	return func(c *clientConfig) error {
		c.userAgent = ua
		return nil
	}
}

// WithTimeout sets the overall request timeout on the built *http.Client.
// Ignored when WithHTTPClient is used.
func WithTimeout(d time.Duration) Option {
	return func(c *clientConfig) error {
		c.timeout = d
		c.timeoutSet = true
		return nil
	}
}

// buildClient resolves the configured options into an *http.Client. scheme is
// the base URL scheme; TLS is only configured for https.
func (c *clientConfig) buildClient(scheme string) (*http.Client, error) {
	if c.httpClientSet {
		return c.httpClient, nil
	}
	hc := &http.Client{Timeout: defaultTimeout}
	if c.timeoutSet {
		hc.Timeout = c.timeout
	}
	if scheme == "https" && (len(c.caCerts) > 0 || c.insecure) {
		tlsCfg := &tls.Config{InsecureSkipVerify: c.insecure}
		if len(c.caCerts) > 0 {
			pool := x509.NewCertPool()
			for _, pem := range c.caCerts {
				if !pool.AppendCertsFromPEM(pem) {
					return nil, fmt.Errorf("rbac: failed to parse CA certificate PEM")
				}
			}
			tlsCfg.RootCAs = pool
		}
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.TLSClientConfig = tlsCfg
		hc.Transport = tr
	}
	return hc, nil
}

// callParams holds per-call query options for Enforce and Reachable.
type callParams struct {
	scenarios []string
	now       *time.Time
}

// CallOption configures an individual Enforce or Reachable call.
type CallOption func(*callParams)

// WithScenarios sets the scenario list sent with the call.
func WithScenarios(scenarios []string) CallOption {
	return func(p *callParams) { p.scenarios = scenarios }
}

// WithNow sets the evaluation timestamp (used by Enforce). When omitted, the
// service uses its own current time.
func WithNow(t time.Time) CallOption {
	return func(p *callParams) { p.now = &t }
}
