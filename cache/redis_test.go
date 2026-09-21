package cache

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"rbac/persist"
)

func TestRedisSetGetRoundTrip(t *testing.T) {
	mr := miniredis.RunT(t)
	r, err := OpenRedis(mr.Addr(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })

	if _, err := r.Get(context.Background()); !errors.Is(err, persist.ErrCacheMiss) {
		t.Fatalf("empty get: %v", err)
	}
	blob := []byte("# rbac-policy v1\nb, u, r, , 1, ALL\n")
	if err := r.Set(context.Background(), blob); err != nil {
		t.Fatal(err)
	}
	got, err := r.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(blob) {
		t.Fatalf("got %q", got)
	}
	if mr.TTL(DefaultKey) != 0 {
		t.Fatal("policy blob must not have TTL")
	}
}

func TestOpenRedisPingFails(t *testing.T) {
	mr := miniredis.RunT(t)
	addr := mr.Addr()
	mr.Close()
	if _, err := OpenRedis(addr, "", 0); err == nil {
		t.Fatal("expected ping error")
	}
}
