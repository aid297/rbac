package policy

import (
	"slices"
	"time"
)

func evalConditions(cs []Condition, ctx *EvalContext) bool {
	if len(cs) == 0 {
		return true
	}
	byKind := make(map[ConditionKind][]Condition, 2)
	for _, c := range cs {
		if c == nil {
			return false
		}
		if c.Kind() == KindAll {
			return true
		}
		k := c.Kind()
		byKind[k] = append(byKind[k], c)
	}
	for _, group := range byKind {
		ok := false
		for _, c := range group {
			if c.Eval(ctx) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func scenarioMatch(b *Binding, ctx *EvalContext) bool {
	if b.Scenario == "" {
		return true
	}
	if ctx == nil || len(ctx.Scenarios) == 0 {
		return false
	}
	return slices.Contains(ctx.Scenarios, b.Scenario)
}

func edgeActive(b *Binding, ctx *EvalContext) bool {
	if b == nil || !b.Enabled {
		return false
	}
	return scenarioMatch(b, ctx) && evalConditions(b.Conditions, ctx)
}

func normalize(ctx *EvalContext) *EvalContext {
	if ctx == nil {
		return &EvalContext{Now: time.Now()}
	}
	return ctx
}
