package policy

import (
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func TestTimeConditionEval(t *testing.T) {
	start := mustTime(t, "2026-06-01T00:00:00+08:00")
	end := mustTime(t, "2026-07-01T00:00:00+08:00")
	atStart := start
	atEnd := end
	before := start.Add(-time.Second)
	inside := start.Add(time.Hour)
	after := end.Add(time.Second)

	tests := []struct {
		name string
		c    TimeCondition
		now  time.Time
		want bool
	}{
		{"open-end now==start", TimeCondition{Start: new(start)}, atStart, true},
		{"open-end before start", TimeCondition{Start: new(start)}, before, false},
		{"open-start now==end", TimeCondition{End: new(end)}, atEnd, false},
		{"open-start just before end", TimeCondition{End: new(end)}, end.Add(-time.Nanosecond), true},
		{"range now==start", TimeCondition{Start: new(start), End: new(end)}, atStart, true},
		{"range now==end", TimeCondition{Start: new(start), End: new(end)}, atEnd, false},
		{"range inside", TimeCondition{Start: new(start), End: new(end)}, inside, true},
		{"range before", TimeCondition{Start: new(start), End: new(end)}, before, false},
		{"range after", TimeCondition{Start: new(start), End: new(end)}, after, false},
		{"unbounded", TimeCondition{}, inside, true},
		{"empty interval defensive", TimeCondition{Start: new(end), End: new(start)}, inside, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.c.Eval(&EvalContext{Now: tc.now})
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestEvalConditions(t *testing.T) {
	start := mustTime(t, "2026-06-01T00:00:00Z")
	end := mustTime(t, "2026-07-01T00:00:00Z")
	now := mustTime(t, "2026-06-15T00:00:00Z")
	ctx := &EvalContext{Now: now}

	if !evalConditions(nil, ctx) {
		t.Fatal("empty should be true")
	}
	if !evalConditions([]Condition{AllCondition{}}, ctx) {
		t.Fatal("ALL should be true")
	}

	t1 := TimeCondition{Start: new(start), End: new(end)}
	t2 := TimeCondition{Start: new(mustTime(t, "2026-01-01T00:00:00Z")), End: new(mustTime(t, "2026-02-01T00:00:00Z"))}
	if !evalConditions([]Condition{t1, t2}, ctx) {
		t.Fatal("same-kind OR should hit t1")
	}
	if evalConditions([]Condition{t2}, ctx) {
		t.Fatal("t2 should miss")
	}
}

func TestValidateConditionMix(t *testing.T) {
	err := validateConditions([]Condition{AllCondition{}, TimeCondition{}})
	if err != ErrInvalidConditionMix {
		t.Fatalf("mix: %v", err)
	}
	err = validateConditions([]Condition{AllCondition{}, AllCondition{}})
	if err != ErrInvalidConditionMix {
		t.Fatalf("ALL;ALL: %v", err)
	}
	start := mustTime(t, "2026-07-01T00:00:00Z")
	end := mustTime(t, "2026-06-01T00:00:00Z")
	err = validateConditions([]Condition{TimeCondition{Start: new(start), End: new(end)}})
	if err != ErrInvalidTimeRange {
		t.Fatalf("range: %v", err)
	}
}

func TestValidateResource(t *testing.T) {
	if err := validateBinding(Binding{Src: "", Dst: "x"}); err != ErrInvalidResource {
		t.Fatalf("empty src: %v", err)
	}
	if err := validateBinding(Binding{Src: "a,b", Dst: "x"}); err != ErrInvalidResource {
		t.Fatalf("comma: %v", err)
	}
	if err := validateBinding(Binding{Src: "a", Dst: "b", Scenario: "ok"}); err != nil {
		t.Fatal(err)
	}
}
