package policy

import (
	"errors"
	"testing"
	"time"
)

func TestNewEngineEmpty(t *testing.T) {
	e := NewEngine()
	if e.Enforce("s", "t", nil) {
		t.Fatal("expected deny")
	}
	got := e.Reachable("s", nil)
	if len(got) != 1 || got[0] != "s" {
		t.Fatalf("reachable=%v", got)
	}
	if e.Enforce("s", "s", nil) != true {
		t.Fatal("identity")
	}
}

func TestZeroEnginePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	var e Engine
	e.Enforce("a", "b", nil)
}

func TestMutateCRUD(t *testing.T) {
	e := NewEngine()
	b := Binding{Src: "u", Dst: "r", Enabled: true, Conditions: []Condition{AllCondition{}}}
	if err := e.AddBinding(b); err != nil {
		t.Fatal(err)
	}
	if err := e.AddBinding(b); !errors.Is(err, ErrDuplicateBinding) {
		t.Fatalf("dup: %v", err)
	}
	if err := e.UpdateBinding(Binding{Src: "no", Dst: "r", Enabled: true}); !errors.Is(err, ErrBindingNotFound) {
		t.Fatalf("upd missing: %v", err)
	}
	if err := e.SetEnabled("u", "r", "", false); err != nil {
		t.Fatal(err)
	}
	got, ok := e.GetBinding("u", "r", "")
	if !ok || got.Enabled {
		t.Fatalf("get %#v ok=%v", got, ok)
	}
	if err := e.RemoveBinding("u", "r", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.RemoveBinding("u", "r", ""); !errors.Is(err, ErrBindingNotFound) {
		t.Fatalf("rm: %v", err)
	}
}

func TestGetListCopies(t *testing.T) {
	e := NewEngine()
	start := mustTime(t, "2026-01-01T00:00:00Z")
	if err := e.AddBinding(Binding{
		Src: "u", Dst: "p", Enabled: true,
		Conditions: []Condition{TimeCondition{Start: new(start)}},
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := e.GetBinding("u", "p", "")
	got.Enabled = false
	if tc, ok := got.Conditions[0].(TimeCondition); ok {
		tc.Start = new(time.Time{})
		got.Conditions[0] = tc
	}
	got.Conditions = append(got.Conditions, AllCondition{})

	still, _ := e.GetBinding("u", "p", "")
	if !still.Enabled {
		t.Fatal("internal enabled mutated")
	}
	list := e.ListBindings()
	list[0].Enabled = false
	list[0].Src = "hack"
	if !e.Enforce("u", "p", &EvalContext{Now: start}) {
		t.Fatal("list mutation leaked")
	}
}

func TestUpdateDoesNotInsert(t *testing.T) {
	e := NewEngine()
	err := e.UpdateBinding(Binding{Src: "a", Dst: "b", Enabled: true, Conditions: []Condition{AllCondition{}}})
	if !errors.Is(err, ErrBindingNotFound) {
		t.Fatal(err)
	}
	if len(e.ListBindings()) != 0 {
		t.Fatal("inserted")
	}
}
