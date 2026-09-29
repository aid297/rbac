package rbac

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"

	rbacv1 "github.com/aid297/rbac/sdk-go/internal/rbacv1"
)

const testBufSize = 1 << 20

func testGRPCClient(t *testing.T, svc rbacv1.RbacServiceServer) *Client {
	t.Helper()
	lis := bufconn.Listen(testBufSize)
	s := grpc.NewServer()
	rbacv1.RegisterRbacServiceServer(s, svc)
	go func() { _ = s.Serve(lis) }()
	t.Cleanup(s.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &Client{
		grpcConn:  conn,
		grpc:      rbacv1.NewRbacServiceClient(conn),
		userAgent: defaultUserAgent,
		timeout:   defaultTimeout,
		isGRPC:    true,
	}
}

func TestNewGRPCClientValidation(t *testing.T) {
	if _, err := NewGRPCClient(""); err == nil {
		t.Fatal("want error for empty target")
	}
	if _, err := NewGRPCClient("http://localhost:9080"); err == nil {
		t.Fatal("want error for http URL")
	}
	if _, err := NewGRPCClient("localhost:9080", WithHTTPClient(&http.Client{})); err == nil {
		t.Fatal("want error for WithHTTPClient")
	}
	if _, err := NewGRPCClient("localhost:9080", WithCACertPath("/tmp/ca.crt")); err == nil {
		t.Fatal("want error for WithCACertPath")
	}
	if _, err := NewGRPCClient("localhost:9443", WithCACert(nil)); err == nil {
		t.Fatal("want error for empty CA")
	}
}

func TestGRPCHealthEnforceCRUD(t *testing.T) {
	svc := newFakeRbac()
	c := testGRPCClient(t, svc)
	ctx := context.Background()

	if err := c.Health(ctx); err != nil {
		t.Fatal(err)
	}

	created, err := c.AddBinding(ctx, Binding{
		Src: "alice", Dst: "doc:42", Enabled: Bool(true),
		Conditions: []Condition{AllCondition()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Src != "alice" {
		t.Fatalf("%+v", created)
	}

	ok, err := c.Enforce(ctx, "alice", "doc:42")
	if err != nil || !ok {
		t.Fatalf("allow=%v err=%v", ok, err)
	}

	nodes, err := c.Reachable(ctx, "alice", WithScenarios([]string{"VIP"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 || nodes[0] != "alice" {
		t.Fatalf("nodes=%v", nodes)
	}

	got, err := c.GetBinding(ctx, "alice", "doc:42", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Dst != "doc:42" {
		t.Fatalf("%+v", got)
	}

	list, err := c.ListBindings(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}

	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	_, err = c.UpdateBinding(ctx, Binding{
		Src: "alice", Dst: "doc:42", Enabled: Bool(true),
		Conditions: []Condition{TimeRange(&start, &end)},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := c.SetEnabled(ctx, "alice", "doc:42", "", false); err != nil {
		t.Fatal(err)
	}
	ok, err = c.Enforce(ctx, "alice", "doc:42")
	if err != nil || ok {
		t.Fatalf("want deny after disable, allow=%v err=%v", ok, err)
	}

	_, err = c.AddBinding(ctx, Binding{Src: "alice", Dst: "doc:42"})
	if !IsConflict(err) {
		t.Fatalf("want conflict, got %v", err)
	}

	if err := c.RemoveBinding(ctx, "alice", "doc:42", ""); err != nil {
		t.Fatal(err)
	}
	_, err = c.GetBinding(ctx, "alice", "doc:42", "")
	if !IsNotFound(err) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestGRPCPaused(t *testing.T) {
	svc := newFakeRbac()
	svc.paused = true
	c := testGRPCClient(t, svc)
	err := c.Health(context.Background())
	if !IsPaused(err) {
		t.Fatalf("want paused, got %v", err)
	}
	if !IsGRPC(err) {
		t.Fatal("want IsGRPC")
	}
}

func TestGRPCUnavailableNotPaused(t *testing.T) {
	// Transport-level Unavailable must not look like service pause.
	err := mapGRPCError("Health", status.Error(codes.Unavailable, "connection refused"))
	if IsPaused(err) {
		t.Fatalf("Unavailable must not be IsPaused: %v", err)
	}
	var ae *APIError
	if errors.As(err, &ae) {
		t.Fatalf("Unavailable must not be wrapped as *APIError: %v", err)
	}
}

func TestGRPCCloseThenCall(t *testing.T) {
	c := testGRPCClient(t, newFakeRbac())
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Health(context.Background()); !errors.Is(err, errClientClosed) {
		t.Fatalf("want errClientClosed, got %v", err)
	}
	if _, err := c.Enforce(context.Background(), "a", "b"); !errors.Is(err, errClientClosed) {
		t.Fatalf("want errClientClosed, got %v", err)
	}
	// Close is idempotent.
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestGRPCCloseRaceWithInFlight(t *testing.T) {
	c := testGRPCClient(t, newFakeRbac())
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.Health(ctx)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = c.Close()
	}()
	wg.Wait()
	if err := c.Health(ctx); !errors.Is(err, errClientClosed) {
		t.Fatalf("after close want errClientClosed, got %v", err)
	}
}

func TestGRPCEnforceWithNow(t *testing.T) {
	svc := newFakeRbac()
	c := testGRPCClient(t, svc)
	now := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	_, err := c.Enforce(context.Background(), "a", "b", WithNow(now), WithScenarios([]string{"S"}))
	if err != nil {
		t.Fatal(err)
	}
	if svc.lastEnforce == nil || svc.lastEnforce.GetSubject() != "a" {
		t.Fatalf("last=%v", svc.lastEnforce)
	}
	if len(svc.lastEnforce.Scenarios) != 1 || svc.lastEnforce.Scenarios[0] != "S" {
		t.Fatalf("scenarios=%v", svc.lastEnforce.Scenarios)
	}
	if svc.lastEnforce.Now == nil || !svc.lastEnforce.Now.AsTime().Equal(now) {
		t.Fatalf("now=%v", svc.lastEnforce.Now)
	}
}

func TestMapGRPCError(t *testing.T) {
	err := mapGRPCError("GetBinding", status.Error(codes.NotFound, "missing"))
	if !IsNotFound(err) {
		t.Fatal(err)
	}
	err = mapGRPCError("AddBinding", status.Error(codes.AlreadyExists, "dup"))
	if !IsConflict(err) {
		t.Fatal(err)
	}
	err = mapGRPCError("Health", status.Error(codes.FailedPrecondition, "service paused: x"))
	if !IsPaused(err) {
		t.Fatal(err)
	}
	err = mapGRPCError("Enforce", status.Error(codes.InvalidArgument, "bad"))
	if !IsBadRequest(err) {
		t.Fatal(err)
	}
	err = mapGRPCError("Health", status.Error(codes.Unavailable, "connection error"))
	if IsPaused(err) {
		t.Fatal("Unavailable must not be paused")
	}
}

// fakeRbac is a minimal in-memory RbacService for SDK tests.
type fakeRbac struct {
	rbacv1.UnimplementedRbacServiceServer
	mu          sync.Mutex
	bindings    map[string]*rbacv1.Binding
	paused      bool
	lastEnforce *rbacv1.EnforceRequest
}

func newFakeRbac() *fakeRbac {
	return &fakeRbac{bindings: map[string]*rbacv1.Binding{}}
}

func key(src, dst, scenario string) string { return src + "\x00" + dst + "\x00" + scenario }

func (f *fakeRbac) Health(context.Context, *rbacv1.HealthRequest) (*rbacv1.HealthResponse, error) {
	if f.paused {
		return nil, status.Error(codes.FailedPrecondition, "service paused: test")
	}
	return &rbacv1.HealthResponse{Status: "ok"}, nil
}

func (f *fakeRbac) Enforce(_ context.Context, req *rbacv1.EnforceRequest) (*rbacv1.EnforceResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastEnforce = req
	if f.paused {
		return nil, status.Error(codes.FailedPrecondition, "service paused: test")
	}
	b, ok := f.bindings[key(req.Subject, req.Target, "")]
	allow := ok && (b.Enabled == nil || b.GetEnabled())
	return &rbacv1.EnforceResponse{Allow: allow}, nil
}

func (f *fakeRbac) Reachable(_ context.Context, req *rbacv1.ReachableRequest) (*rbacv1.ReachableResponse, error) {
	out := []string{req.Subject}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range f.bindings {
		if b.Src == req.Subject && (b.Enabled == nil || b.GetEnabled()) {
			out = append(out, b.Dst)
		}
	}
	return &rbacv1.ReachableResponse{Subject: req.Subject, Reachable: out}, nil
}

func (f *fakeRbac) ListBindings(context.Context, *rbacv1.ListBindingsRequest) (*rbacv1.ListBindingsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var list []*rbacv1.Binding
	for _, b := range f.bindings {
		list = append(list, b)
	}
	return &rbacv1.ListBindingsResponse{Bindings: list}, nil
}

func (f *fakeRbac) GetBinding(_ context.Context, req *rbacv1.GetBindingRequest) (*rbacv1.Binding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.bindings[key(req.Src, req.Dst, req.Scenario)]
	if !ok {
		return nil, status.Error(codes.NotFound, "binding not found")
	}
	return b, nil
}

func (f *fakeRbac) AddBinding(_ context.Context, b *rbacv1.Binding) (*rbacv1.Binding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := key(b.Src, b.Dst, b.Scenario)
	if _, ok := f.bindings[k]; ok {
		return nil, status.Error(codes.AlreadyExists, "duplicate")
	}
	cp := cloneBinding(b)
	if cp.Enabled == nil {
		v := true
		cp.Enabled = &v
	}
	if len(cp.Conditions) == 0 {
		cp.Conditions = []*rbacv1.Condition{{Kind: "ALL"}}
	}
	f.bindings[k] = cp
	return cp, nil
}

func (f *fakeRbac) UpdateBinding(_ context.Context, b *rbacv1.Binding) (*rbacv1.Binding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := key(b.Src, b.Dst, b.Scenario)
	if _, ok := f.bindings[k]; !ok {
		return nil, status.Error(codes.NotFound, "binding not found")
	}
	cp := cloneBinding(b)
	f.bindings[k] = cp
	return cp, nil
}

func (f *fakeRbac) SetEnabled(_ context.Context, req *rbacv1.SetEnabledRequest) (*rbacv1.SetEnabledResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.bindings[key(req.Src, req.Dst, req.Scenario)]
	if !ok {
		return nil, status.Error(codes.NotFound, "binding not found")
	}
	v := req.Enabled
	b.Enabled = &v
	return &rbacv1.SetEnabledResponse{Ok: true}, nil
}

func (f *fakeRbac) RemoveBinding(_ context.Context, req *rbacv1.RemoveBindingRequest) (*rbacv1.RemoveBindingResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := key(req.Src, req.Dst, req.Scenario)
	if _, ok := f.bindings[k]; !ok {
		return nil, status.Error(codes.NotFound, "binding not found")
	}
	delete(f.bindings, k)
	return &rbacv1.RemoveBindingResponse{}, nil
}

func cloneBinding(b *rbacv1.Binding) *rbacv1.Binding {
	cp := &rbacv1.Binding{
		Src: b.Src, Dst: b.Dst, Scenario: b.Scenario, Enabled: b.Enabled,
	}
	for _, c := range b.Conditions {
		nc := &rbacv1.Condition{Kind: c.Kind}
		if c.Start != nil {
			nc.Start = timestamppb.New(c.Start.AsTime())
		}
		if c.End != nil {
			nc.End = timestamppb.New(c.End.AsTime())
		}
		cp.Conditions = append(cp.Conditions, nc)
	}
	return cp
}
