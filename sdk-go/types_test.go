package rbac

import (
	"encoding/json"
	"testing"
	"time"
)

func tp(t time.Time) *time.Time { return &t }

func TestConditionMarshal(t *testing.T) {
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		c    Condition
		want string
	}{
		{"all", AllCondition(), `{"kind":"ALL"}`},
		{"time both", TimeRange(tp(start), tp(end)), `{"kind":"TIME","start":"2026-06-01T00:00:00Z","end":"2026-07-01T00:00:00Z"}`},
		{"time start only", TimeRange(tp(start), nil), `{"kind":"TIME","start":"2026-06-01T00:00:00Z"}`},
		{"time end only", TimeRange(nil, tp(end)), `{"kind":"TIME","end":"2026-07-01T00:00:00Z"}`},
		{"time unbounded", TimeRange(nil, nil), `{"kind":"TIME"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.c)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestConditionRoundTrip(t *testing.T) {
	start := time.Date(2026, 6, 1, 12, 30, 0, 0, time.UTC)
	end := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	orig := []Condition{AllCondition(), TimeRange(tp(start), tp(end))}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back []Condition
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back) != 2 {
		t.Fatalf("want 2 conditions, got %d", len(back))
	}
	if back[0].Kind != KindAll {
		t.Fatalf("first kind: %v", back[0].Kind)
	}
	tc := back[1]
	if tc.Kind != KindTime || tc.Start == nil || !tc.Start.Equal(start) || tc.End == nil || !tc.End.Equal(end) {
		t.Fatalf("time condition mismatch: %+v", tc)
	}
}

func TestConditionUnmarshalErrors(t *testing.T) {
	var c Condition
	if err := json.Unmarshal([]byte(`{"kind":"TIME","start":"not-a-time"}`), &c); err == nil {
		t.Fatal("want error for bad start")
	}
	if err := json.Unmarshal([]byte(`{"kind":"TIME","end":"not-a-time"}`), &c); err == nil {
		t.Fatal("want error for bad end")
	}
	if err := json.Unmarshal([]byte(`{`), &c); err == nil {
		t.Fatal("want error for malformed json")
	}
}

func TestConditionUnknownKind(t *testing.T) {
	var c Condition
	if err := json.Unmarshal([]byte(`{"kind":"BOGUS"}`), &c); err == nil {
		t.Fatal("want error for unknown condition kind")
	}
}

func TestBindingEnabledOmitted(t *testing.T) {
	b := Binding{Src: "a", Dst: "b"}
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(data, &raw)
	if _, present := raw["enabled"]; present {
		t.Fatalf("enabled should be omitted when nil, got %s", data)
	}

	b2 := Binding{Src: "a", Dst: "b", Enabled: Bool(false)}
	data2, err := json.Marshal(b2)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw2 map[string]json.RawMessage
	_ = json.Unmarshal(data2, &raw2)
	if _, present := raw2["enabled"]; !present {
		t.Fatalf("enabled should be present when explicitly set, got %s", data2)
	}
}

func TestBindingRoundTrip(t *testing.T) {
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	b := Binding{
		Src: "alice", Dst: "role:editor", Scenario: "VIP", Enabled: Bool(true),
		Conditions: []Condition{TimeRange(tp(start), nil)},
	}
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Binding
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Src != b.Src || back.Dst != b.Dst || back.Scenario != b.Scenario {
		t.Fatalf("binding mismatch: %+v", back)
	}
	if back.Enabled == nil || *back.Enabled != true {
		t.Fatalf("enabled: got %v, want true", back.Enabled)
	}
	if len(back.Conditions) != 1 || back.Conditions[0].Start == nil || !back.Conditions[0].Start.Equal(start) {
		t.Fatalf("condition mismatch: %+v", back.Conditions)
	}
}
