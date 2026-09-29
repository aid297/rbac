package rbac

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	rbacv1 "github.com/aid297/rbac/sdk-go/internal/rbacv1"
)

// NewGRPCClient builds a Client that talks to the service over gRPC.
//
// target is a dial address such as "localhost:9080" (plaintext) or
// "localhost:9443" (TLS). Plaintext is the default; pass WithCACert /
// WithCACertFile / WithInsecureSkipVerify to enable TLS (grpc_tls).
//
// The returned Client exposes the same methods as NewClient (Health, Enforce,
// …). Call Close when finished to release the underlying connection.
func NewGRPCClient(target string, opts ...Option) (*Client, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, fmt.Errorf("rbac: gRPC target must be non-empty")
	}
	// Reject URL schemes that belong to the HTTP client.
	if strings.Contains(target, "://") {
		return nil, fmt.Errorf("rbac: gRPC target must be host:port (got %q); use NewClient for http(s):// URLs", target)
	}

	cfg := &clientConfig{userAgent: defaultUserAgent}
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, err
		}
	}
	if cfg.httpClientSet {
		return nil, fmt.Errorf("rbac: WithHTTPClient is not supported with NewGRPCClient")
	}
	if cfg.caCertPath != "" {
		return nil, fmt.Errorf("rbac: WithCACertPath is not supported with NewGRPCClient; use WithCACert/WithCACertFile")
	}

	dialOpts, err := cfg.buildGRPCDialOptions()
	if err != nil {
		return nil, err
	}
	timeout := defaultTimeout
	if cfg.timeoutSet {
		timeout = cfg.timeout
	}

	conn, err := grpc.NewClient(target, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("rbac: grpc dial %s: %w", target, err)
	}

	ua := cfg.userAgent
	if ua == "" {
		ua = defaultUserAgent
	}
	return &Client{
		grpcConn:  conn,
		grpc:      rbacv1.NewRbacServiceClient(conn),
		userAgent: ua,
		timeout:   timeout,
		isGRPC:    true,
	}, nil
}

func (c *clientConfig) buildGRPCDialOptions() ([]grpc.DialOption, error) {
	useTLS := len(c.caCerts) > 0 || c.insecure
	var opts []grpc.DialOption
	if useTLS {
		tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: c.insecure}
		if len(c.caCerts) > 0 {
			pool := x509.NewCertPool()
			for _, pem := range c.caCerts {
				if !pool.AppendCertsFromPEM(pem) {
					return nil, fmt.Errorf("rbac: failed to parse CA certificate PEM")
				}
			}
			tlsCfg.RootCAs = pool
		}
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	return opts, nil
}

func (c *Client) grpcHealth(ctx context.Context) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	_, err := c.grpc.Health(ctx, &rbacv1.HealthRequest{})
	return mapGRPCError("Health", err)
}

func (c *Client) grpcEnforce(ctx context.Context, subject, target string, p *callParams) (bool, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	req := &rbacv1.EnforceRequest{
		Subject:   subject,
		Target:    target,
		Scenarios: p.scenarios,
	}
	if p.now != nil {
		req.Now = timestamppb.New(p.now.UTC().Truncate(time.Second))
	}
	out, err := c.grpc.Enforce(ctx, req)
	if err != nil {
		return false, mapGRPCError("Enforce", err)
	}
	return out.GetAllow(), nil
}

func (c *Client) grpcReachable(ctx context.Context, subject string, p *callParams) ([]string, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	out, err := c.grpc.Reachable(ctx, &rbacv1.ReachableRequest{
		Subject:   subject,
		Scenarios: p.scenarios,
	})
	if err != nil {
		return nil, mapGRPCError("Reachable", err)
	}
	return out.GetReachable(), nil
}

func (c *Client) grpcListBindings(ctx context.Context) ([]Binding, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	out, err := c.grpc.ListBindings(ctx, &rbacv1.ListBindingsRequest{})
	if err != nil {
		return nil, mapGRPCError("ListBindings", err)
	}
	return bindingsFromProto(out.GetBindings()), nil
}

func (c *Client) grpcGetBinding(ctx context.Context, src, dst, scenario string) (Binding, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	out, err := c.grpc.GetBinding(ctx, &rbacv1.GetBindingRequest{
		Src: src, Dst: dst, Scenario: scenario,
	})
	if err != nil {
		return Binding{}, mapGRPCError("GetBinding", err)
	}
	return bindingFromProto(out), nil
}

func (c *Client) grpcAddBinding(ctx context.Context, b Binding) (Binding, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	out, err := c.grpc.AddBinding(ctx, bindingToProto(b))
	if err != nil {
		return Binding{}, mapGRPCError("AddBinding", err)
	}
	return bindingFromProto(out), nil
}

func (c *Client) grpcUpdateBinding(ctx context.Context, b Binding) (Binding, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	out, err := c.grpc.UpdateBinding(ctx, bindingToProto(b))
	if err != nil {
		return Binding{}, mapGRPCError("UpdateBinding", err)
	}
	return bindingFromProto(out), nil
}

func (c *Client) grpcSetEnabled(ctx context.Context, src, dst, scenario string, enabled bool) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	_, err := c.grpc.SetEnabled(ctx, &rbacv1.SetEnabledRequest{
		Src: src, Dst: dst, Scenario: scenario, Enabled: enabled,
	})
	return mapGRPCError("SetEnabled", err)
}

func (c *Client) grpcRemoveBinding(ctx context.Context, src, dst, scenario string) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	_, err := c.grpc.RemoveBinding(ctx, &rbacv1.RemoveBindingRequest{
		Src: src, Dst: dst, Scenario: scenario,
	})
	return mapGRPCError("RemoveBinding", err)
}

func (c *Client) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.timeout <= 0 {
		return context.WithCancel(ctx)
	}
	// Honor an already-shorter deadline on ctx.
	if deadline, ok := ctx.Deadline(); ok {
		if time.Until(deadline) <= c.timeout {
			return context.WithCancel(ctx)
		}
	}
	return context.WithTimeout(ctx, c.timeout)
}

func bindingToProto(b Binding) *rbacv1.Binding {
	out := &rbacv1.Binding{
		Src:      b.Src,
		Dst:      b.Dst,
		Scenario: b.Scenario,
		Enabled:  b.Enabled,
	}
	if len(b.Conditions) == 0 {
		return out
	}
	for _, c := range b.Conditions {
		pc := &rbacv1.Condition{Kind: string(c.Kind)}
		if c.Kind == KindTime {
			if c.Start != nil {
				pc.Start = timestamppb.New(c.Start.UTC().Truncate(time.Second))
			}
			if c.End != nil {
				pc.End = timestamppb.New(c.End.UTC().Truncate(time.Second))
			}
		}
		out.Conditions = append(out.Conditions, pc)
	}
	return out
}

func bindingFromProto(b *rbacv1.Binding) Binding {
	if b == nil {
		return Binding{}
	}
	out := Binding{
		Src:      b.GetSrc(),
		Dst:      b.GetDst(),
		Scenario: b.GetScenario(),
	}
	if b.Enabled != nil {
		v := b.GetEnabled()
		out.Enabled = &v
	}
	for _, c := range b.GetConditions() {
		if c == nil {
			continue
		}
		cond := Condition{Kind: ConditionKind(c.GetKind())}
		if c.Start != nil {
			t := c.Start.AsTime().UTC().Truncate(time.Second)
			cond.Start = &t
		}
		if c.End != nil {
			t := c.End.AsTime().UTC().Truncate(time.Second)
			cond.End = &t
		}
		out.Conditions = append(out.Conditions, cond)
	}
	return out
}

func bindingsFromProto(in []*rbacv1.Binding) []Binding {
	out := make([]Binding, 0, len(in))
	for _, b := range in {
		out = append(out, bindingFromProto(b))
	}
	return out
}
