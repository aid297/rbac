package policy

import (
	"sync"
	"testing"
	"time"
)

func TestCOWRace(t *testing.T) {
	e := NewEngine()
	add(t, e, "u", "r", "", true)
	add(t, e, "r", "p", "", true, TimeCondition{Start: new(mustTime(t, "2000-01-01T00:00:00Z"))})

	var wg sync.WaitGroup
	ctx := &EvalContext{Now: time.Now()}
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				_ = e.Enforce("u", "p", ctx)
				_ = e.Reachable("u", ctx)
				b, ok := e.GetBinding("r", "p", "")
				if ok {
					b.Enabled = false
					if len(b.Conditions) > 0 {
						if tc, ok := b.Conditions[0].(TimeCondition); ok {
							tc.Start = new(time.Time{})
							b.Conditions[0] = tc
						}
					}
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := range 100 {
			_ = e.SetEnabled("u", "r", "", j%2 == 0)
			_ = e.UpdateBinding(Binding{
				Src: "r", Dst: "p", Enabled: true,
				Conditions: []Condition{TimeCondition{Start: new(time.Now().Add(-time.Hour))}},
			})
		}
	}()
	wg.Wait()
}

func BenchmarkEnforceShallow(b *testing.B) {
	e := NewEngine()
	_ = e.AddBinding(Binding{Src: "u", Dst: "r", Enabled: true, Conditions: []Condition{AllCondition{}}})
	_ = e.AddBinding(Binding{Src: "r", Dst: "p", Enabled: true, Conditions: []Condition{AllCondition{}}})
	ctx := &EvalContext{Now: time.Now()}
	b.ResetTimer()
	for b.Loop() {
		if !e.Enforce("u", "p", ctx) {
			b.Fatal()
		}
	}
}

func BenchmarkEnforceDense(b *testing.B) {
	e := NewEngine()
	n := 32
	for i := range n {
		for j := range n {
			if i == j {
				continue
			}
			_ = e.AddBinding(Binding{Src: nid(i), Dst: nid(j), Enabled: true, Conditions: []Condition{AllCondition{}}})
		}
	}
	ctx := &EvalContext{Now: time.Now()}
	b.ResetTimer()
	for b.Loop() {
		_ = e.Enforce(nid(0), nid(n-1), ctx)
		_ = e.Reachable(nid(0), ctx)
	}
}
