package httpsvc

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aid297/rbac/server/policy"
)

type bindingDTO struct {
	Src        string         `json:"src"`
	Dst        string         `json:"dst"`
	Scenario   string         `json:"scenario"`
	Enabled    bool           `json:"enabled"`
	Conditions []conditionDTO `json:"conditions"`
}

type conditionDTO struct {
	Kind  string `json:"kind"`
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

func bindingToDTO(b policy.Binding) bindingDTO {
	dto := bindingDTO{Src: b.Src, Dst: b.Dst, Scenario: b.Scenario, Enabled: b.Enabled}
	if len(b.Conditions) == 0 {
		dto.Conditions = []conditionDTO{{Kind: "ALL"}}
		return dto
	}
	for _, c := range b.Conditions {
		switch t := c.(type) {
		case policy.AllCondition:
			dto.Conditions = append(dto.Conditions, conditionDTO{Kind: "ALL"})
		case policy.TimeCondition:
			cd := conditionDTO{Kind: "TIME"}
			if t.Start != nil {
				cd.Start = t.Start.UTC().Format(time.RFC3339)
			}
			if t.End != nil {
				cd.End = t.End.UTC().Format(time.RFC3339)
			}
			dto.Conditions = append(dto.Conditions, cd)
		default:
			dto.Conditions = append(dto.Conditions, conditionDTO{Kind: fmt.Sprintf("%d", c.Kind())})
		}
	}
	return dto
}

func (d bindingDTO) toBinding() (policy.Binding, error) {
	b := policy.Binding{Src: d.Src, Dst: d.Dst, Scenario: d.Scenario, Enabled: d.Enabled}
	if len(d.Conditions) == 0 {
		b.Conditions = []policy.Condition{policy.AllCondition{}}
		return b, nil
	}
	for _, c := range d.Conditions {
		switch strings.ToUpper(strings.TrimSpace(c.Kind)) {
		case "ALL":
			b.Conditions = append(b.Conditions, policy.AllCondition{})
		case "TIME":
			tc := policy.TimeCondition{}
			if c.Start != "" {
				t, err := time.Parse(time.RFC3339, c.Start)
				if err != nil {
					return policy.Binding{}, fmt.Errorf("start: %w", err)
				}
				tc.Start = &t
			}
			if c.End != "" {
				t, err := time.Parse(time.RFC3339, c.End)
				if err != nil {
					return policy.Binding{}, fmt.Errorf("end: %w", err)
				}
				tc.End = &t
			}
			b.Conditions = append(b.Conditions, tc)
		default:
			return policy.Binding{}, fmt.Errorf("unknown condition kind %q", c.Kind)
		}
	}
	return b, nil
}

func readBinding(r *http.Request) (policy.Binding, error) {
	var dto bindingDTO
	dto.Enabled = true
	if err := decodeJSON(r, &dto); err != nil {
		if err.Error() == "EOF" {
			return policy.Binding{}, fmt.Errorf("empty body")
		}
		return policy.Binding{}, err
	}
	if dto.Src == "" || dto.Dst == "" {
		return policy.Binding{}, fmt.Errorf("src and dst required")
	}
	return dto.toBinding()
}
