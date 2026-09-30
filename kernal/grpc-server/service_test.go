package grpcserver_test

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"

	rbacv1 "github.com/aid297/rbac/kernal/api/gen/rbac/v1"
	"github.com/aid297/rbac/kernal/grpc-server"
	"github.com/aid297/rbac/kernal/rbac/persist"
	"github.com/aid297/rbac/kernal/rbac/policy"
	"github.com/aid297/rbac/kernal/svcctl"
)

const bufSize = 1 << 20

func testStore(t *testing.T) *persist.Store {
	t.Helper()
	persist.LeavePause()
	s, err := persist.Open(filepath.Join(t.TempDir(), persist.DefaultFile))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(); persist.LeavePause() })
	return s
}

func testClient(t *testing.T, store *persist.Store, ca []byte) rbacv1.RbacServiceClient {
	t.Helper()
	lis := bufconn.Listen(bufSize)
	gate := new(svcctl.Gate)
	srv := grpc.NewServer()
	rbacv1.RegisterRbacServiceServer(srv, grpcserver.NewServer(store, ca, gate))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
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
	_ = ctx
	return rbacv1.NewRbacServiceClient(conn)
}

func TestHealthEnforceReachable(t *testing.T) {
	s := testStore(t)
	if err := s.AddBinding(policy.Binding{
		Src: "u", Dst: "r", Enabled: true,
		Conditions: []policy.Condition{policy.AllCondition{}},
	}); err != nil {
		t.Fatal(err)
	}
	c := testClient(t, s, nil)
	ctx := context.Background()

	h, err := c.Health(ctx, &rbacv1.HealthRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if h.Status != "ok" {
		t.Fatalf("status=%q", h.Status)
	}

	en, err := c.Enforce(ctx, &rbacv1.EnforceRequest{Subject: "u", Target: "r"})
	if err != nil {
		t.Fatal(err)
	}
	if !en.Allow {
		t.Fatal("expected allow")
	}

	reach, err := c.Reachable(ctx, &rbacv1.ReachableRequest{Subject: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if len(reach.Reachable) < 2 {
		t.Fatalf("reachable=%v", reach.Reachable)
	}
}

func TestBindingsCRUD(t *testing.T) {
	s := testStore(t)
	c := testClient(t, s, []byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"))
	ctx := context.Background()

	created, err := c.AddBinding(ctx, &rbacv1.Binding{
		Src: "a", Dst: "b", Enabled: boolPtr(true),
		Conditions: []*rbacv1.Condition{{Kind: "ALL"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.GetSrc() != "a" {
		t.Fatalf("%+v", created)
	}

	got, err := c.GetBinding(ctx, &rbacv1.GetBindingRequest{Src: "a", Dst: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.GetEnabled() {
		t.Fatal("enabled")
	}

	if _, err := c.SetEnabled(ctx, &rbacv1.SetEnabledRequest{Src: "a", Dst: "b", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if s.Enforce("a", "b", nil) {
		t.Fatal("disabled")
	}

	list, err := c.ListBindings(ctx, &rbacv1.ListBindingsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Bindings) != 1 {
		t.Fatalf("list=%d", len(list.Bindings))
	}

	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	_, err = c.UpdateBinding(ctx, &rbacv1.Binding{
		Src: "a", Dst: "b", Enabled: boolPtr(true),
		Conditions: []*rbacv1.Condition{{
			Kind:  "TIME",
			Start: timestamppb.New(start),
			End:   timestamppb.New(end),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = c.AddBinding(ctx, &rbacv1.Binding{Src: "a", Dst: "b"})
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("want AlreadyExists, got %v", err)
	}

	if _, err := c.RemoveBinding(ctx, &rbacv1.RemoveBindingRequest{Src: "a", Dst: "b"}); err != nil {
		t.Fatal(err)
	}
	_, err = c.GetBinding(ctx, &rbacv1.GetBindingRequest{Src: "a", Dst: "b"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("want NotFound, got %v", err)
	}
}

func TestPaused(t *testing.T) {
	s := testStore(t)
	c := testClient(t, s, nil)
	persist.EnterPause("test")
	t.Cleanup(persist.LeavePause)

	_, err := c.Health(context.Background(), &rbacv1.HealthRequest{})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("want FailedPrecondition, got %v", err)
	}
	_, err = c.Enforce(context.Background(), &rbacv1.EnforceRequest{Subject: "a", Target: "b"})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("want FailedPrecondition, got %v", err)
	}
}

func TestGetCACert(t *testing.T) {
	s := testStore(t)
	pem := []byte("-----BEGIN CERTIFICATE-----\nABC\n-----END CERTIFICATE-----\n")
	c := testClient(t, s, pem)
	out, err := c.GetCACert(context.Background(), &rbacv1.GetCACertRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if string(out.Pem) != string(pem) {
		t.Fatalf("pem mismatch")
	}
}

func TestDefaultEnabled(t *testing.T) {
	s := testStore(t)
	c := testClient(t, s, nil)
	// enabled unset → default true
	_, err := c.AddBinding(context.Background(), &rbacv1.Binding{
		Src: "x", Dst: "y",
		Conditions: []*rbacv1.Condition{{Kind: "ALL"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Enforce("x", "y", nil) {
		t.Fatal("expected default enabled")
	}
}

func boolPtr(v bool) *bool { return &v }
