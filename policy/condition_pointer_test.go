package policy

import (
	"strings"
	"testing"
)

// Pointer-form Conditions (*AllCondition / *TimeCondition) are accepted as input.
// unwrapCondition and cloneCondition normalize them to value forms; these tests
// exercise those pointer branches end to end.

func TestUnwrapConditionPointerForms(t *testing.T) {
	start := mustTime(t, "2026-06-01T00:00:00Z")
	end := mustTime(t, "2026-07-01T00:00:00Z")

	t.Run("*AllCondition to value", func(t *testing.T) {
		if _, ok := unwrapCondition(&AllCondition{}).(AllCondition); !ok {
			t.Fatalf("got %T want AllCondition", unwrapCondition(&AllCondition{}))
		}
	})
	t.Run("*TimeCondition to value preserving bounds", func(t *testing.T) {
		got := unwrapCondition(&TimeCondition{Start: new(start), End: new(end)})
		tc, ok := got.(TimeCondition)
		if !ok {
			t.Fatalf("got %T want TimeCondition", got)
		}
		if tc.Start == nil || !tc.Start.Equal(start) || tc.End == nil || !tc.End.Equal(end) {
			t.Fatalf("bounds not preserved: %+v", tc)
		}
	})
	t.Run("nil *AllCondition to nil", func(t *testing.T) {
		if got := unwrapCondition((*AllCondition)(nil)); got != nil {
			t.Fatalf("got %v want nil", got)
		}
	})
	t.Run("nil *TimeCondition to nil", func(t *testing.T) {
		if got := unwrapCondition((*TimeCondition)(nil)); got != nil {
			t.Fatalf("got %v want nil", got)
		}
	})
	t.Run("value forms pass through", func(t *testing.T) {
		if _, ok := unwrapCondition(AllCondition{}).(AllCondition); !ok {
			t.Fatal("AllCondition value should pass through")
		}
		if _, ok := unwrapCondition(TimeCondition{}).(TimeCondition); !ok {
			t.Fatal("TimeCondition value should pass through")
		}
	})
}

func TestCloneConditionPointerForms(t *testing.T) {
	start := mustTime(t, "2026-06-01T00:00:00Z")
	end := mustTime(t, "2026-07-01T00:00:00Z")

	t.Run("*AllCondition to value", func(t *testing.T) {
		if _, ok := cloneCondition(&AllCondition{}).(AllCondition); !ok {
			t.Fatalf("got %T want AllCondition", cloneCondition(&AllCondition{}))
		}
	})
	t.Run("*TimeCondition deep-copied to value", func(t *testing.T) {
		orig := &TimeCondition{Start: new(start), End: new(end)}
		tc, ok := cloneCondition(orig).(TimeCondition)
		if !ok {
			t.Fatalf("got %T want TimeCondition", cloneCondition(orig))
		}
		if tc.Start == orig.Start || tc.End == orig.End {
			t.Fatal("clone must not share bound pointers with source")
		}
		if !tc.Start.Equal(start) || !tc.End.Equal(end) {
			t.Fatalf("clone bounds mismatch: %+v", tc)
		}
	})
	t.Run("value TimeCondition deep-copied", func(t *testing.T) {
		orig := TimeCondition{Start: new(start)}
		got, ok := cloneCondition(orig).(TimeCondition)
		if !ok {
			t.Fatalf("got %T want TimeCondition", cloneCondition(orig))
		}
		if got.Start == orig.Start {
			t.Fatal("clone must not share bound pointer")
		}
	})
	t.Run("nil *TimeCondition to nil", func(t *testing.T) {
		if got := cloneCondition((*TimeCondition)(nil)); got != nil {
			t.Fatalf("got %v want nil", got)
		}
	})
}

func TestValidateConditionsPointerForms(t *testing.T) {
	valid := &TimeCondition{
		Start: new(mustTime(t, "2026-06-01T00:00:00Z")),
		End:   new(mustTime(t, "2026-07-01T00:00:00Z")),
	}
	badRange := &TimeCondition{
		Start: new(mustTime(t, "2026-07-01T00:00:00Z")),
		End:   new(mustTime(t, "2026-06-01T00:00:00Z")),
	}

	tests := []struct {
		name string
		cs   []Condition
		want error
	}{
		{"single *ALL ok", []Condition{&AllCondition{}}, nil},
		{"single *TIME ok", []Condition{valid}, nil},
		{"*ALL mixed with *TIME", []Condition{&AllCondition{}, valid}, ErrInvalidConditionMix},
		{"*ALL;*ALL", []Condition{&AllCondition{}, &AllCondition{}}, ErrInvalidConditionMix},
		{"*TIME bad range", []Condition{badRange}, ErrInvalidTimeRange},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateConditions(tc.cs); err != tc.want {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
}

func TestEvalConditionsPointerForms(t *testing.T) {
	ctx := &EvalContext{Now: mustTime(t, "2026-06-15T00:00:00Z")}
	in := &TimeCondition{
		Start: new(mustTime(t, "2026-06-01T00:00:00Z")),
		End:   new(mustTime(t, "2026-07-01T00:00:00Z")),
	}
	out := &TimeCondition{
		Start: new(mustTime(t, "2026-01-01T00:00:00Z")),
		End:   new(mustTime(t, "2026-02-01T00:00:00Z")),
	}

	if !evalConditions([]Condition{&AllCondition{}}, ctx) {
		t.Fatal("*ALL should be true")
	}
	if !evalConditions([]Condition{in}, ctx) {
		t.Fatal("*TIME inside window should be true")
	}
	if evalConditions([]Condition{out}, ctx) {
		t.Fatal("*TIME outside window should be false")
	}
	if !evalConditions([]Condition{out, in}, ctx) {
		t.Fatal("same-kind OR over pointer forms should hit the inside window")
	}
}

func TestEdgeActivePointerForms(t *testing.T) {
	ctx := &EvalContext{
		Now:       mustTime(t, "2026-06-15T00:00:00Z"),
		Scenarios: []string{"VIP"},
	}
	in := &TimeCondition{
		Start: new(mustTime(t, "2026-06-01T00:00:00Z")),
		End:   new(mustTime(t, "2026-07-01T00:00:00Z")),
	}
	b := &Binding{Src: "u", Dst: "r", Scenario: "VIP", Enabled: true, Conditions: []Condition{in}}

	if !edgeActive(b, ctx) {
		t.Fatal("enabled + scenario match + *TIME inside should be active")
	}
	b.Enabled = false
	if edgeActive(b, ctx) {
		t.Fatal("disabled edge should be inactive")
	}
	b.Enabled = true
	b.Scenario = "OTHER"
	if edgeActive(b, ctx) {
		t.Fatal("scenario mismatch should be inactive")
	}
}

// Pointer-form conditions are normalized to value forms on write, evaluate
// correctly through the engine, and canonicalize on Serialize.
func TestEnginePointerConditionRoundTrip(t *testing.T) {
	e := NewEngine()
	start := mustTime(t, "2026-06-01T00:00:00Z")
	end := mustTime(t, "2026-07-01T00:00:00Z")

	if err := e.AddBinding(Binding{
		Src: "u1", Dst: "r1", Enabled: true,
		Conditions: []Condition{&TimeCondition{Start: new(start), End: new(end)}},
	}); err != nil {
		t.Fatalf("add u1->r1: %v", err)
	}
	if err := e.AddBinding(Binding{
		Src: "r1", Dst: "p1", Enabled: true,
		Conditions: []Condition{&AllCondition{}},
	}); err != nil {
		t.Fatalf("add r1->p1: %v", err)
	}

	inside := &EvalContext{Now: mustTime(t, "2026-06-15T00:00:00Z")}
	if !e.Enforce("u1", "p1", inside) {
		t.Fatal("u1 should reach p1 inside the window")
	}
	outside := &EvalContext{Now: mustTime(t, "2026-08-01T00:00:00Z")}
	if e.Enforce("u1", "p1", outside) {
		t.Fatal("u1 should not reach p1 outside the window")
	}

	got, ok := e.GetBinding("u1", "r1", "")
	if !ok {
		t.Fatal("u1->r1 missing after add")
	}
	if len(got.Conditions) != 1 {
		t.Fatalf("want 1 condition, got %d", len(got.Conditions))
	}
	if _, isVal := got.Conditions[0].(TimeCondition); !isVal {
		t.Fatalf("stored condition should be normalized to value form, got %T", got.Conditions[0])
	}

	s := e.Serialize()
	if !strings.Contains(s, "b, r1, p1, , 1, ALL") {
		t.Fatalf("*AllCondition not canonicalized to ALL:\n%s", s)
	}

	e2 := NewEngine()
	if err := e2.Load(s); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if e2.Serialize() != s {
		t.Fatal("Serialize is not idempotent across Load")
	}
	if !e2.Enforce("u1", "p1", inside) || e2.Enforce("u1", "p1", outside) {
		t.Fatal("reloaded engine should preserve time-window semantics")
	}
}
