package policy

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
)

const policyVersionHeader = "# rbac-policy v1"

func (e *Engine) Serialize() string {
	snap := e.current.Load()
	bindings := make([]Binding, 0)
	if snap != nil {
		for _, b := range snap.all {
			bindings = append(bindings, copyBindingValue(b))
		}
	}
	slices.SortFunc(bindings, func(a, b Binding) int {
		if c := cmp.Compare(a.Src, b.Src); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Dst, b.Dst); c != 0 {
			return c
		}
		return cmp.Compare(a.Scenario, b.Scenario)
	})
	var b strings.Builder
	b.WriteString(policyVersionHeader)
	b.WriteByte('\n')
	for _, bd := range bindings {
		b.WriteString(formatBindingLine(bd))
		b.WriteByte('\n')
	}
	return b.String()
}

func (e *Engine) Load(s string) error {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	parsed, err := parsePolicy(s)
	if err != nil {
		return err
	}
	snap, err := snapshotFromBindings(parsed)
	if err != nil {
		return err
	}
	e.current.Store(snap)
	return nil
}

func parsePolicy(s string) ([]Binding, error) {
	lines := strings.Split(s, "\n")
	out := make([]Binding, 0)
	seen := make(map[string]struct{})
	for i, raw := range lines {
		lineNo := i + 1
		line := strings.TrimRight(raw, "\r")
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		b, err := parseBindingLine(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		key := bindingKey(b.Src, b.Dst, b.Scenario)
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("line %d: %w", lineNo, ErrDuplicateBinding)
		}
		seen[key] = struct{}{}
		out = append(out, b)
	}
	return out, nil
}

func parseBindingLine(line string) (Binding, error) {
	fields := splitFields(line)
	if len(fields) != 6 {
		return Binding{}, ErrParseLine
	}
	if fields[0] != "b" {
		return Binding{}, ErrParseLine
	}
	src, dst, scenario := fields[1], fields[2], fields[3]
	var enabled bool
	switch fields[4] {
	case "1":
		enabled = true
	case "0":
		enabled = false
	default:
		return Binding{}, ErrParseLine
	}
	conds, err := parseConditionsField(fields[5])
	if err != nil {
		return Binding{}, err
	}
	b := Binding{
		Src:        src,
		Dst:        dst,
		Scenario:   scenario,
		Enabled:    enabled,
		Conditions: conds,
	}
	if err := validateBinding(b); err != nil {
		return Binding{}, err
	}
	return b, nil
}

func splitFields(line string) []string {
	raw := strings.Split(line, ",")
	out := make([]string, len(raw))
	for i, f := range raw {
		out[i] = strings.TrimSpace(f)
	}
	return out
}

func parseConditionsField(s string) ([]Condition, error) {
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ";")
	cs := make([]Condition, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil, ErrParseLine
		}
		c, err := parseOneCondition(p)
		if err != nil {
			return nil, err
		}
		cs = append(cs, c)
	}
	return cs, nil
}

func parseOneCondition(s string) (Condition, error) {
	if s == "ALL" {
		return AllCondition{}, nil
	}
	rest, ok := strings.CutPrefix(s, "TIME:")
	if !ok {
		return nil, ErrParseLine
	}
	startS, endS, ok := strings.Cut(rest, "~")
	if !ok || strings.Contains(endS, "~") {
		return nil, ErrParseLine
	}
	tc := TimeCondition{}
	if startS != "" {
		t, err := time.Parse(time.RFC3339, startS)
		if err != nil {
			return nil, ErrParseLine
		}
		tc.Start = new(t)
	}
	if endS != "" {
		t, err := time.Parse(time.RFC3339, endS)
		if err != nil {
			return nil, ErrParseLine
		}
		tc.End = new(t)
	}
	return tc, nil
}

func formatBindingLine(b Binding) string {
	en := "0"
	if b.Enabled {
		en = "1"
	}
	return strings.Join([]string{
		"b",
		b.Src,
		b.Dst,
		b.Scenario,
		en,
		formatConditions(b.Conditions),
	}, ", ")
}

func formatConditions(cs []Condition) string {
	if isCanonicalALL(cs) {
		return "ALL"
	}
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		switch t := unwrapCondition(c).(type) {
		case AllCondition:
			return "ALL"
		case TimeCondition:
			parts = append(parts, formatTimeCondition(t))
		default:
			panic("policy: unknown condition kind in Serialize")
		}
	}
	if len(parts) == 0 {
		return "ALL"
	}
	return strings.Join(parts, ";")
}

func isCanonicalALL(cs []Condition) bool {
	if len(cs) == 0 {
		return true
	}
	if len(cs) != 1 {
		return false
	}
	switch t := unwrapCondition(cs[0]).(type) {
	case AllCondition:
		return true
	case TimeCondition:
		return t.Start == nil && t.End == nil
	default:
		return false
	}
}

func formatTimeCondition(t TimeCondition) string {
	var start, end string
	if t.Start != nil {
		start = t.Start.Format(time.RFC3339)
	}
	if t.End != nil {
		end = t.End.Format(time.RFC3339)
	}
	return "TIME:" + start + "~" + end
}
