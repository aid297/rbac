package policy

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// ConditionKind identifies a family of conditions. Same-kind instances OR;
// different kinds AND.
type ConditionKind uint8

const (
	KindAll  ConditionKind = iota // unconditional; always true
	KindTime                      // time window
)

// maxDepth is the v1 hop limit for grant transitivity (not a cycle brake).
const maxDepth = 32

var (
	ErrInvalidResource     = errors.New("resource id contains reserved char")
	ErrInvalidTimeRange    = errors.New("start >= end")
	ErrInvalidConditionMix = errors.New("ALL cannot be mixed with other conditions")
	ErrParseLine           = errors.New("malformed policy line")
	ErrBindingNotFound     = errors.New("binding not found")
	ErrDuplicateBinding    = errors.New("binding (src,dst,scenario) exists")
)

// Condition is an immutable predicate attached to a binding.
type Condition interface {
	Kind() ConditionKind
	Eval(ctx *EvalContext) bool
}

// AllCondition is the exclusive always-true condition.
type AllCondition struct{}

func (AllCondition) Kind() ConditionKind { return KindAll }

func (AllCondition) Eval(*EvalContext) bool { return true }

// TimeCondition is a half-open interval [Start, End). Nil bound means unbounded.
type TimeCondition struct {
	Start *time.Time
	End   *time.Time
}

func (TimeCondition) Kind() ConditionKind { return KindTime }

func (c TimeCondition) Eval(ctx *EvalContext) bool {
	if ctx == nil {
		return false
	}
	now := ctx.Now
	if c.Start != nil && c.End != nil && !c.Start.Before(*c.End) {
		return false
	}
	if c.Start != nil && now.Before(*c.Start) {
		return false
	}
	if c.End != nil && !now.Before(*c.End) {
		return false
	}
	return true
}

// Binding is a directed edge Src → Dst.
type Binding struct {
	Src        string
	Dst        string
	Scenario   string
	Conditions []Condition
	Enabled    bool
}

// EvalContext is the query-time environment. A nil *EvalContext is normalized
// at Enforce/Reachable entry; a non-nil zero Now is respected as-is.
type EvalContext struct {
	Now       time.Time
	Scenarios []string
}

type snapshot struct {
	out   map[string][]*Binding
	all   []*Binding
	byKey map[string]*Binding
}

// Engine is a COW policy graph. Construct with NewEngine; a zero Engine panics on query.
type Engine struct {
	current atomic.Pointer[snapshot]
	writeMu sync.Mutex
}

func NewEngine() *Engine {
	e := new(Engine)
	e.current.Store(emptySnapshot())
	return e
}

func emptySnapshot() *snapshot {
	return &snapshot{
		out:   make(map[string][]*Binding),
		all:   make([]*Binding, 0),
		byKey: make(map[string]*Binding),
	}
}

func bindingKey(src, dst, scenario string) string {
	return src + "\x00" + dst + "\x00" + scenario
}
