package rbac

import (
	"encoding/json"
	"fmt"
	"time"
)

// ConditionKind identifies a family of conditions. Same-kind instances OR;
// different kinds AND. ALL is exclusive and cannot be mixed with others.
type ConditionKind string

const (
	KindAll  ConditionKind = "ALL"
	KindTime ConditionKind = "TIME"
)

// Binding is a directed edge Src -> Dst, mirroring the service /v1 wire DTO.
type Binding struct {
	Src        string      `json:"src"`
	Dst        string      `json:"dst"`
	Scenario   string      `json:"scenario"`
	Enabled    bool        `json:"enabled"`
	Conditions []Condition `json:"conditions"`
}

// Condition is a predicate attached to a binding. For KindAll, Start and End
// are ignored. For KindTime, the interval is half-open [Start, End); a nil
// bound means unbounded on that side.
//
// The service stores time at RFC3339 second precision, so sub-second values
// are lost on a round trip.
type Condition struct {
	Kind  ConditionKind
	Start *time.Time
	End   *time.Time
}

// AllCondition returns an unconditional, always-true condition.
func AllCondition() Condition { return Condition{Kind: KindAll} }

// TimeRange returns a TIME condition for the half-open interval [start, end).
// Either bound may be nil to leave that side unbounded.
func TimeRange(start, end *time.Time) Condition {
	return Condition{Kind: KindTime, Start: start, End: end}
}

// conditionWire is the JSON shape exchanged with the service.
type conditionWire struct {
	Kind  ConditionKind `json:"kind"`
	Start string        `json:"start,omitempty"`
	End   string        `json:"end,omitempty"`
}

func (c Condition) MarshalJSON() ([]byte, error) {
	w := conditionWire{Kind: c.Kind}
	if c.Kind == KindTime {
		if c.Start != nil {
			w.Start = c.Start.UTC().Format(time.RFC3339)
		}
		if c.End != nil {
			w.End = c.End.UTC().Format(time.RFC3339)
		}
	}
	return json.Marshal(w)
}

func (c *Condition) UnmarshalJSON(data []byte) error {
	var w conditionWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	c.Kind = w.Kind
	c.Start = nil
	c.End = nil
	if w.Start != "" {
		t, err := time.Parse(time.RFC3339, w.Start)
		if err != nil {
			return fmt.Errorf("condition start: %w", err)
		}
		c.Start = &t
	}
	if w.End != "" {
		t, err := time.Parse(time.RFC3339, w.End)
		if err != nil {
			return fmt.Errorf("condition end: %w", err)
		}
		c.End = &t
	}
	return nil
}
