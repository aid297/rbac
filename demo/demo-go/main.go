// demo-go runs functional tests against a live RBAC server per demo-spec.md.
//
// Usage:
//
//	go run .                # default http://localhost:8080
//	go run . -addr :9090    # custom address
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	rbac "github.com/aid297/rbac/sdk-go"
)

func main() {
	addr := flag.String("addr", "http://127.0.0.1:8080", "RBAC server base URL")
	flag.Parse()

	c, err := rbac.NewClient(*addr)
	if err != nil {
		fatalf("NewClient: %v", err)
	}
	ctx := context.Background()

	var passed, failed int
	check := func(name string, ok bool, detail string) {
		if ok {
			passed++
			fmt.Printf("[PASS] %s\n", name)
		} else {
			failed++
			fmt.Printf("[FAIL] %s: %s\n", name, detail)
		}
	}

	// ───────── 2.1 Health Check ─────────
	err = c.Health(ctx)
	check("2.1 Health Check", err == nil, errStr(err))

	// ───────── 2.2 AddBinding — no condition ─────────
	b1, err := c.AddBinding(ctx, rbac.Binding{
		Src:      "alice",
		Dst:      "doc:read",
		Scenario: "default",
		Enabled:  rbac.Bool(true),
	})
	check("2.2 AddBinding — no condition", err == nil && b1.Src == "alice" && b1.Dst == "doc:read", errStr(err))

	// ───────── 2.3 AddBinding — time condition ─────────
	t9 := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	t18 := time.Date(2026, 1, 1, 18, 0, 0, 0, time.UTC)
	b2, err := c.AddBinding(ctx, rbac.Binding{
		Src:      "bob",
		Dst:      "doc:write",
		Scenario: "default",
		Conditions: []rbac.Condition{
			rbac.TimeRange(&t9, &t18),
		},
	})
	check("2.3 AddBinding — time condition", err == nil && b2.Src == "bob" && len(b2.Conditions) == 1 && b2.Conditions[0].Kind == rbac.KindTime, errStr(err))

	// ───────── 2.4 AddBinding — duplicate (Conflict) ─────────
	_, err = c.AddBinding(ctx, rbac.Binding{
		Src:      "alice",
		Dst:      "doc:read",
		Scenario: "default",
		Enabled:  rbac.Bool(true),
	})
	check("2.4 AddBinding — duplicate (Conflict)", rbac.IsConflict(err), fmt.Sprintf("expected conflict, got: %v", err))

	// ───────── 2.5 GetBinding ─────────
	got, err := c.GetBinding(ctx, "alice", "doc:read", "default")
	check("2.5 GetBinding", err == nil && got.Src == "alice" && got.Dst == "doc:read" && got.Scenario == "default", errStr(err))

	// ───────── 2.6 ListBindings ─────────
	list, err := c.ListBindings(ctx)
	check("2.6 ListBindings", err == nil && len(list) >= 2, fmt.Sprintf("err=%v len=%d", err, len(list)))

	// ───────── 2.7 Enforce — allow ─────────
	allow, err := c.Enforce(ctx, "alice", "doc:read", rbac.WithScenarios([]string{"default"}))
	check("2.7 Enforce — allow", err == nil && allow, fmt.Sprintf("allow=%v err=%v", allow, err))

	// ───────── 2.8 Enforce — deny ─────────
	allow, err = c.Enforce(ctx, "alice", "doc:delete", rbac.WithScenarios([]string{"default"}))
	check("2.8 Enforce — deny", err == nil && !allow, fmt.Sprintf("allow=%v err=%v", allow, err))

	// ───────── 2.9 Reachable ─────────
	reach, err := c.Reachable(ctx, "alice", rbac.WithScenarios([]string{"default"}))
	check("2.9 Reachable", err == nil && contains(reach, "alice") && contains(reach, "doc:read"),
		fmt.Sprintf("reach=%v err=%v", reach, err))

	// ───────── 2.10 SetEnabled — disable ─────────
	err = c.SetEnabled(ctx, "alice", "doc:read", "default", false)
	check("2.10 SetEnabled — disable", err == nil, errStr(err))

	// ───────── 2.11 Enforce — disabled edge ─────────
	allow, err = c.Enforce(ctx, "alice", "doc:read", rbac.WithScenarios([]string{"default"}))
	check("2.11 Enforce — disabled edge", err == nil && !allow, fmt.Sprintf("allow=%v err=%v", allow, err))

	// ───────── 2.12 SetEnabled — re-enable ─────────
	err = c.SetEnabled(ctx, "alice", "doc:read", "default", true)
	check("2.12 SetEnabled — re-enable", err == nil, errStr(err))

	// ───────── 2.13 UpdateBinding — change condition to ALL ─────────
	upd, err := c.UpdateBinding(ctx, rbac.Binding{
		Src:      "bob",
		Dst:      "doc:write",
		Scenario: "default",
		Conditions: []rbac.Condition{
			rbac.AllCondition(),
		},
	})
	check("2.13 UpdateBinding — change condition", err == nil && len(upd.Conditions) == 1 && upd.Conditions[0].Kind == rbac.KindAll, errStr(err))

	// ───────── 2.14 RemoveBinding ─────────
	err = c.RemoveBinding(ctx, "alice", "doc:read", "default")
	check("2.14 RemoveBinding", err == nil, errStr(err))

	// ───────── 2.15 GetBinding — not found ─────────
	_, err = c.GetBinding(ctx, "alice", "doc:read", "default")
	check("2.15 GetBinding — not found", rbac.IsNotFound(err), fmt.Sprintf("expected not found, got: %v", err))

	// ───────── 2.16 RemoveBinding — not found ─────────
	err = c.RemoveBinding(ctx, "alice", "doc:read", "default")
	check("2.16 RemoveBinding — not found", rbac.IsNotFound(err), fmt.Sprintf("expected not found, got: %v", err))

	// ───────── Cleanup: remove bob's edge ─────────
	_ = c.RemoveBinding(ctx, "bob", "doc:write", "default")

	// ───────── Summary ─────────
	fmt.Printf("\n%d passed, %d failed\n", passed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func contains(ss []string, target string) bool {
	for _, s := range ss {
		if strings.EqualFold(s, target) {
			return true
		}
	}
	return false
}
