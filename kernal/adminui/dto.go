package adminui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aid297/rbac/kernal/rbac/policy"
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

type enabledReq struct {
	Src      string `json:"src"`
	Dst      string `json:"dst"`
	Scenario string `json:"scenario"`
	Enabled  bool   `json:"enabled"`
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

func readBindingJSON(r *http.Request) (policy.Binding, error) {
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

func writePolicyErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, policy.ErrDuplicateBinding):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, policy.ErrBindingNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, policy.ErrInvalidResource), errors.Is(err, policy.ErrInvalidTimeRange),
		errors.Is(err, policy.ErrInvalidConditionMix), errors.Is(err, policy.ErrParseLine):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
