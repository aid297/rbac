package policy

import (
	"strconv"
	"testing"
	"time"
)

func add(t *testing.T, e *Engine, src, dst, scenario string, enabled bool, cs ...Condition) {
	t.Helper()
	if cs == nil {
		cs = []Condition{AllCondition{}}
	}
	if err := e.AddBinding(Binding{Src: src, Dst: dst, Scenario: scenario, Enabled: enabled, Conditions: cs}); err != nil {
		t.Fatal(err)
	}
}

func TestEnforceGraph(t *testing.T) {
	e := NewEngine()
	add(t, e, "u", "r", "", true)
	add(t, e, "r", "p", "", true)
	if !e.Enforce("u", "p", nil) {
		t.Fatal("multi-hop")
	}
	if e.Enforce("p", "u", nil) {
		t.Fatal("directed")
	}
	if !e.Enforce("u", "u", nil) {
		t.Fatal("identity")
	}

	// cycle
	e2 := NewEngine()
	add(t, e2, "a", "b", "", true)
	add(t, e2, "b", "a", "", true)
	add(t, e2, "b", "c", "", true)
	if !e2.Enforce("a", "c", nil) {
		t.Fatal("cycle still reaches c")
	}

	// disabled edge
	e3 := NewEngine()
	add(t, e3, "u", "p1", "", false)
	add(t, e3, "u", "p2", "", true)
	if e3.Enforce("u", "p1", nil) {
		t.Fatal("disabled")
	}
	if !e3.Enforce("u", "p2", nil) {
		t.Fatal("other path")
	}
}

func TestScenarioFilter(t *testing.T) {
	e := NewEngine()
	add(t, e, "u", "p", "VIP", true)
	add(t, e, "u", "q", "", true)
	if e.Enforce("u", "p", nil) {
		t.Fatal("nil ctx must ignore named scenario")
	}
	if !e.Enforce("u", "q", nil) {
		t.Fatal("generic edge")
	}
	if !e.Enforce("u", "p", &EvalContext{Now: time.Now(), Scenarios: []string{"VIP"}}) {
		t.Fatal("named")
	}
	if !e.Enforce("u", "p", &EvalContext{Now: time.Now(), Scenarios: []string{"X", "VIP"}}) {
		t.Fatal("multi scenario OR")
	}
	if e.Enforce("u", "p", &EvalContext{Now: time.Now(), Scenarios: []string{}}) {
		t.Fatal("empty scenarios")
	}
}

func TestNilCtxVsZeroNow(t *testing.T) {
	e := NewEngine()
	start := time.Now().Add(-time.Hour)
	add(t, e, "u", "p", "", true, TimeCondition{Start: new(start)})
	if !e.Enforce("u", "p", nil) {
		t.Fatal("nil ctx uses time.Now")
	}
	if e.Enforce("u", "p", &EvalContext{Scenarios: []string{"VIP"}}) {
		t.Fatal("zero Now must not be rewritten")
	}
}

func TestMaxDepth(t *testing.T) {
	e := NewEngine()
	for i := range 33 {
		add(t, e, nid(i), nid(i+1), "", true)
	}
	if !e.Enforce(nid(0), nid(32), nil) {
		t.Fatal("depth 32 should pass")
	}
	if e.Enforce(nid(0), nid(33), nil) {
		t.Fatal("depth 33 should fail")
	}
}

func nid(i int) string {
	return "n" + strconv.Itoa(i)
}

func TestDiamondNoExplosion(t *testing.T) {
	e := NewEngine()
	// 8-layer binary diamond-ish DAG
	layers := 8
	width := 8
	for l := range layers {
		for i := range width {
			id := nid(l*width + i)
			nextBase := (l + 1) * width
			if l == layers-1 {
				add(t, e, id, "sink", "", true)
				continue
			}
			add(t, e, id, nid(nextBase+(i)%width), "", true)
			add(t, e, id, nid(nextBase+(i+1)%width), "", true)
		}
	}
	if !e.Enforce(nid(0), "sink", nil) {
		t.Fatal("should reach sink")
	}
	got := e.Reachable(nid(0), nil)
	if len(got) < 2 {
		t.Fatalf("too few: %v", got)
	}
}

func TestReachableEquiv(t *testing.T) {
	e := NewEngine()
	add(t, e, "u", "r", "", true)
	add(t, e, "r", "p", "VIP", true)
	add(t, e, "r", "q", "", true)
	nodes := []string{"u", "r", "p", "q", "z"}
	ctxs := []*EvalContext{
		nil,
		{Now: time.Now()},
		{Now: time.Now(), Scenarios: []string{"VIP"}},
	}
	for _, s := range nodes {
		for _, ctx := range ctxs {
			set := map[string]bool{}
			for _, tname := range e.Reachable(s, ctx) {
				set[tname] = true
			}
			if !set[s] {
				t.Fatalf("missing self s=%s", s)
			}
			for _, tgt := range nodes {
				en := e.Enforce(s, tgt, ctx)
				if en != set[tgt] {
					t.Fatalf("s=%s t=%s enforce=%v reachable=%v ctx=%v", s, tgt, en, set[tgt], ctx)
				}
			}
		}
	}
}
