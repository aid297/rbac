package policy

import (
	"strings"
	"time"
	"unicode"
)

const reservedChars = ",;~#\n\r"

func validateBinding(b Binding) error {
	if err := validateResourceID(b.Src, true); err != nil {
		return err
	}
	if err := validateResourceID(b.Dst, true); err != nil {
		return err
	}
	if err := validateResourceID(b.Scenario, false); err != nil {
		return err
	}
	return validateConditions(b.Conditions)
}

func validateResourceID(id string, rejectEmpty bool) error {
	if rejectEmpty && id == "" {
		return ErrInvalidResource
	}
	if strings.ContainsAny(id, reservedChars) {
		return ErrInvalidResource
	}
	for _, r := range id {
		if unicode.IsControl(r) {
			return ErrInvalidResource
		}
	}
	return nil
}

func validateConditions(cs []Condition) error {
	if len(cs) == 0 {
		return nil
	}
	allCount := 0
	for _, c := range cs {
		if c == nil {
			return ErrInvalidConditionMix
		}
		switch t := unwrapCondition(c).(type) {
		case AllCondition:
			allCount++
		case TimeCondition:
			if t.Start != nil && t.End != nil && !t.Start.Before(*t.End) {
				return ErrInvalidTimeRange
			}
		}
	}
	if allCount > 0 && (allCount > 1 || len(cs) > 1) {
		return ErrInvalidConditionMix
	}
	return nil
}

func unwrapCondition(c Condition) Condition {
	switch t := c.(type) {
	case *AllCondition:
		if t == nil {
			return nil
		}
		return AllCondition{}
	case *TimeCondition:
		if t == nil {
			return nil
		}
		return *t
	default:
		return c
	}
}

func cloneConditions(in []Condition) []Condition {
	if in == nil {
		return nil
	}
	out := make([]Condition, len(in))
	for i, c := range in {
		out[i] = cloneCondition(c)
	}
	return out
}

func cloneCondition(c Condition) Condition {
	switch t := c.(type) {
	case AllCondition:
		return AllCondition{}
	case *AllCondition:
		return AllCondition{}
	case TimeCondition:
		return cloneTime(t)
	case *TimeCondition:
		if t == nil {
			return nil
		}
		return cloneTime(*t)
	default:
		return c
	}
}

func cloneTime(t TimeCondition) TimeCondition {
	var start, end *time.Time
	if t.Start != nil {
		start = new(*t.Start)
	}
	if t.End != nil {
		end = new(*t.End)
	}
	return TimeCondition{Start: start, End: end}
}

func cloneBindingPtr(b *Binding) *Binding {
	if b == nil {
		return nil
	}
	nb := *b
	nb.Conditions = cloneConditions(b.Conditions)
	return new(nb)
}

func copyBindingValue(b *Binding) Binding {
	if b == nil {
		return Binding{}
	}
	out := *b
	out.Conditions = cloneConditions(b.Conditions)
	return out
}

func cloneSnapshot(src *snapshot) *snapshot {
	if src == nil {
		return emptySnapshot()
	}
	dst := &snapshot{
		out:   make(map[string][]*Binding, len(src.out)),
		all:   make([]*Binding, 0, len(src.all)),
		byKey: make(map[string]*Binding, len(src.all)),
	}
	for _, b := range src.all {
		nb := cloneBindingPtr(b)
		dst.all = append(dst.all, nb)
		dst.byKey[bindingKey(nb.Src, nb.Dst, nb.Scenario)] = nb
		dst.out[nb.Src] = append(dst.out[nb.Src], nb)
	}
	return dst
}

func snapshotFromBindings(bs []Binding) (*snapshot, error) {
	dst := emptySnapshot()
	for i := range bs {
		if err := validateBinding(bs[i]); err != nil {
			return nil, err
		}
		key := bindingKey(bs[i].Src, bs[i].Dst, bs[i].Scenario)
		if _, ok := dst.byKey[key]; ok {
			return nil, ErrDuplicateBinding
		}
		nb := cloneBindingPtr(&bs[i])
		dst.all = append(dst.all, nb)
		dst.byKey[key] = nb
		dst.out[nb.Src] = append(dst.out[nb.Src], nb)
	}
	return dst, nil
}
