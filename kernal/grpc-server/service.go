package grpcsvc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	rbacv1 "github.com/aid297/rbac/server/api/gen/rbac/v1"
	"github.com/aid297/rbac/server/persist"
	"github.com/aid297/rbac/server/policy"
	"github.com/aid297/rbac/server/svcctl"
)

// Server implements rbac.v1.RbacService.
type Server struct {
	rbacv1.UnimplementedRbacServiceServer
	store     *persist.Store
	caCertPEM []byte
	gate      *svcctl.Gate
}

// NewServer builds a gRPC service backed by store.
func NewServer(store *persist.Store, caCertPEM []byte, gate *svcctl.Gate) *Server {
	return &Server{store: store, caCertPEM: caCertPEM, gate: gate}
}

func (s *Server) track() func() { return s.gate.Track() }

func (s *Server) checkPaused() error {
	paused, why := persist.Paused()
	if !paused {
		return nil
	}
	// FailedPrecondition (not Unavailable): Unavailable is also used by gRPC
	// for dial/transport failures, and must not look like "service paused".
	return status.Errorf(codes.FailedPrecondition, "service paused: %s", why)
}

func (s *Server) Health(ctx context.Context, _ *rbacv1.HealthRequest) (*rbacv1.HealthResponse, error) {
	done := s.track()
	defer done()
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	if err := s.checkPaused(); err != nil {
		return nil, err
	}
	return &rbacv1.HealthResponse{Status: "ok"}, nil
}

func (s *Server) GetCACert(ctx context.Context, _ *rbacv1.GetCACertRequest) (*rbacv1.GetCACertResponse, error) {
	done := s.track()
	defer done()
	if err := s.checkPaused(); err != nil {
		return nil, err
	}
	if len(s.caCertPEM) == 0 {
		return nil, status.Error(codes.Internal, "CA certificate not available")
	}
	return &rbacv1.GetCACertResponse{Pem: append([]byte(nil), s.caCertPEM...)}, nil
}

func (s *Server) Enforce(ctx context.Context, req *rbacv1.EnforceRequest) (*rbacv1.EnforceResponse, error) {
	done := s.track()
	defer done()
	if err := s.checkPaused(); err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.Subject) == "" || strings.TrimSpace(req.Target) == "" {
		return nil, status.Error(codes.InvalidArgument, "subject and target required")
	}
	var now *time.Time
	if req.Now != nil {
		t := req.Now.AsTime().UTC()
		// Align with HTTP: second precision.
		t = t.Truncate(time.Second)
		now = &t
	}
	ok := s.store.Enforce(req.Subject, req.Target, evalCtx(req.Scenarios, now))
	return &rbacv1.EnforceResponse{Allow: ok}, nil
}

func (s *Server) Reachable(ctx context.Context, req *rbacv1.ReachableRequest) (*rbacv1.ReachableResponse, error) {
	done := s.track()
	defer done()
	if err := s.checkPaused(); err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.Subject) == "" {
		return nil, status.Error(codes.InvalidArgument, "subject required")
	}
	return &rbacv1.ReachableResponse{
		Subject:   req.Subject,
		Reachable: s.store.Reachable(req.Subject, evalCtx(req.Scenarios, nil)),
	}, nil
}

func (s *Server) ListBindings(ctx context.Context, _ *rbacv1.ListBindingsRequest) (*rbacv1.ListBindingsResponse, error) {
	done := s.track()
	defer done()
	if err := s.checkPaused(); err != nil {
		return nil, err
	}
	list := s.store.ListBindings()
	out := make([]*rbacv1.Binding, 0, len(list))
	for _, b := range list {
		out = append(out, bindingToProto(b))
	}
	return &rbacv1.ListBindingsResponse{Bindings: out}, nil
}

func (s *Server) GetBinding(ctx context.Context, req *rbacv1.GetBindingRequest) (*rbacv1.Binding, error) {
	done := s.track()
	defer done()
	if err := s.checkPaused(); err != nil {
		return nil, err
	}
	if req == nil || req.Src == "" || req.Dst == "" {
		return nil, status.Error(codes.InvalidArgument, "src and dst required")
	}
	b, ok := s.store.GetBinding(req.Src, req.Dst, req.Scenario)
	if !ok {
		return nil, status.Error(codes.NotFound, policy.ErrBindingNotFound.Error())
	}
	return bindingToProto(b), nil
}

func (s *Server) AddBinding(ctx context.Context, req *rbacv1.Binding) (*rbacv1.Binding, error) {
	done := s.track()
	defer done()
	if err := s.checkPaused(); err != nil {
		return nil, err
	}
	b, err := protoToBinding(req)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := s.store.AddBinding(b); err != nil {
		return nil, mapPolicyErr(err)
	}
	return bindingToProto(b), nil
}

func (s *Server) UpdateBinding(ctx context.Context, req *rbacv1.Binding) (*rbacv1.Binding, error) {
	done := s.track()
	defer done()
	if err := s.checkPaused(); err != nil {
		return nil, err
	}
	b, err := protoToBinding(req)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := s.store.UpdateBinding(b); err != nil {
		return nil, mapPolicyErr(err)
	}
	return bindingToProto(b), nil
}

func (s *Server) SetEnabled(ctx context.Context, req *rbacv1.SetEnabledRequest) (*rbacv1.SetEnabledResponse, error) {
	done := s.track()
	defer done()
	if err := s.checkPaused(); err != nil {
		return nil, err
	}
	if req == nil || req.Src == "" || req.Dst == "" {
		return nil, status.Error(codes.InvalidArgument, "src and dst required")
	}
	if err := s.store.SetEnabled(req.Src, req.Dst, req.Scenario, req.Enabled); err != nil {
		return nil, mapPolicyErr(err)
	}
	return &rbacv1.SetEnabledResponse{Ok: true}, nil
}

func (s *Server) RemoveBinding(ctx context.Context, req *rbacv1.RemoveBindingRequest) (*rbacv1.RemoveBindingResponse, error) {
	done := s.track()
	defer done()
	if err := s.checkPaused(); err != nil {
		return nil, err
	}
	if req == nil || req.Src == "" || req.Dst == "" {
		return nil, status.Error(codes.InvalidArgument, "src and dst required")
	}
	if err := s.store.RemoveBinding(req.Src, req.Dst, req.Scenario); err != nil {
		return nil, mapPolicyErr(err)
	}
	return &rbacv1.RemoveBindingResponse{}, nil
}

func evalCtx(scenarios []string, now *time.Time) *policy.EvalContext {
	ctx := &policy.EvalContext{Scenarios: scenarios}
	if now != nil {
		ctx.Now = *now
	}
	return ctx
}

func mapPolicyErr(err error) error {
	switch {
	case errors.Is(err, policy.ErrDuplicateBinding):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, policy.ErrBindingNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, policy.ErrInvalidResource),
		errors.Is(err, policy.ErrInvalidTimeRange),
		errors.Is(err, policy.ErrInvalidConditionMix),
		errors.Is(err, policy.ErrParseLine):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}

func bindingToProto(b policy.Binding) *rbacv1.Binding {
	enabled := b.Enabled
	out := &rbacv1.Binding{
		Src:      b.Src,
		Dst:      b.Dst,
		Scenario: b.Scenario,
		Enabled:  &enabled,
	}
	if len(b.Conditions) == 0 {
		out.Conditions = []*rbacv1.Condition{{Kind: "ALL"}}
		return out
	}
	for _, c := range b.Conditions {
		switch t := c.(type) {
		case policy.AllCondition:
			out.Conditions = append(out.Conditions, &rbacv1.Condition{Kind: "ALL"})
		case policy.TimeCondition:
			cd := &rbacv1.Condition{Kind: "TIME"}
			if t.Start != nil {
				cd.Start = timestamppb.New(t.Start.UTC())
			}
			if t.End != nil {
				cd.End = timestamppb.New(t.End.UTC())
			}
			out.Conditions = append(out.Conditions, cd)
		default:
			out.Conditions = append(out.Conditions, &rbacv1.Condition{Kind: fmt.Sprintf("%d", c.Kind())})
		}
	}
	return out
}

func protoToBinding(d *rbacv1.Binding) (policy.Binding, error) {
	if d == nil {
		return policy.Binding{}, fmt.Errorf("empty binding")
	}
	if d.Src == "" || d.Dst == "" {
		return policy.Binding{}, fmt.Errorf("src and dst required")
	}
	b := policy.Binding{Src: d.Src, Dst: d.Dst, Scenario: d.Scenario, Enabled: true}
	if d.Enabled != nil {
		b.Enabled = *d.Enabled
	}
	if len(d.Conditions) == 0 {
		b.Conditions = []policy.Condition{policy.AllCondition{}}
		return b, nil
	}
	for _, c := range d.Conditions {
		if c == nil {
			return policy.Binding{}, fmt.Errorf("nil condition")
		}
		switch strings.ToUpper(strings.TrimSpace(c.Kind)) {
		case "ALL":
			b.Conditions = append(b.Conditions, policy.AllCondition{})
		case "TIME":
			tc := policy.TimeCondition{}
			if c.Start != nil {
				t := c.Start.AsTime().UTC().Truncate(time.Second)
				tc.Start = &t
			}
			if c.End != nil {
				t := c.End.AsTime().UTC().Truncate(time.Second)
				tc.End = &t
			}
			b.Conditions = append(b.Conditions, tc)
		default:
			return policy.Binding{}, fmt.Errorf("unknown condition kind %q", c.Kind)
		}
	}
	return b, nil
}
