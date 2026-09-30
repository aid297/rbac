package policy

import (
	"errors"
	"strings"
	"testing"
)

func TestSerializeRoundTrip(t *testing.T) {
	e := NewEngine()
	start := mustTime(t, "2026-01-01T00:00:00+08:00")
	end := mustTime(t, "2027-01-01T00:00:00+08:00")
	add(t, e, "user2", "role1", "", true)
	add(t, e, "user1", "role1", "", true, TimeCondition{Start: new(start)})
	add(t, e, "role1", "perm1", "VIP1", true)
	add(t, e, "role1", "perm2", "VIP2", false,
		TimeCondition{End: new(end)},
		TimeCondition{
			Start: new(mustTime(t, "2027-06-01T00:00:00+08:00")),
			End:   new(mustTime(t, "2027-07-01T00:00:00+08:00")),
		},
	)
	s1 := e.Serialize()
	e2 := NewEngine()
	if err := e2.Load(s1); err != nil {
		t.Fatal(err)
	}
	s2 := e2.Serialize()
	if s1 != s2 {
		t.Fatalf("canonical mismatch\n%s\n---\n%s", s1, s2)
	}
	if !strings.HasPrefix(s1, "# rbac-policy v1\n") {
		t.Fatal(s1)
	}
	// sorted: role1 before user1
	if strings.Index(s1, "b, role1,") > strings.Index(s1, "b, user1,") {
		t.Fatalf("not sorted:\n%s", s1)
	}
}

func TestCanonicalALL(t *testing.T) {
	e := NewEngine()
	add(t, e, "a", "b", "", true, TimeCondition{}) // TIME:~
	add(t, e, "c", "d", "", true)                  // ALL via helper
	s := e.Serialize()
	if strings.Contains(s, "TIME:~") {
		t.Fatalf("should canonicalize TIME:~ to ALL:\n%s", s)
	}
	if !strings.Contains(s, "b, a, b, , 1, ALL") {
		t.Fatal(s)
	}
	e2 := NewEngine()
	src := "# rbac-policy v1\nb, x, y, , 1,\nb, m, n, , 1, TIME:~\n"
	if err := e2.Load(src); err != nil {
		t.Fatal(err)
	}
	out := e2.Serialize()
	if !strings.Contains(out, "b, m, n, , 1, ALL") || !strings.Contains(out, "b, x, y, , 1, ALL") {
		t.Fatal(out)
	}
}

func TestLoadFailFast(t *testing.T) {
	e := NewEngine()
	add(t, e, "keep", "me", "", true)
	orig := e.Serialize()

	cases := []struct {
		name string
		src  string
		want error
	}{
		{"mix", "# rbac-policy v1\nb, a, b, , 1, ALL;TIME:2026-01-01T00:00:00Z~\n", ErrInvalidConditionMix},
		{"allall", "# rbac-policy v1\nb, a, b, , 1, ALL;ALL\n", ErrInvalidConditionMix},
		{"dup", "# rbac-policy v1\nb, a, b, , 1, ALL\nb, a, b, , 0, ALL\n", ErrDuplicateBinding},
		{"range", "# rbac-policy v1\nb, a, b, , 1, TIME:2026-07-01T00:00:00Z~2026-06-01T00:00:00Z\n", ErrInvalidTimeRange},
		{"reserved", "# rbac-policy v1\nb, a,b, c, , 1, ALL\n", ErrParseLine}, // extra field from comma in id
		{"empty src", "# rbac-policy v1\nb, , b, , 1, ALL\n", ErrInvalidResource},
		{"enabled", "# rbac-policy v1\nb, a, b, , true, ALL\n", ErrParseLine},
		{"prefix", "# rbac-policy v1\np, a, b, , 1, ALL\n", ErrParseLine},
		{"fields", "# rbac-policy v1\nb, a, b, 1, ALL\n", ErrParseLine},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := e.Load(tc.src)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
			if e.Serialize() != orig {
				t.Fatal("snapshot mutated")
			}
			if !strings.Contains(err.Error(), "line ") {
				t.Fatalf("missing line: %v", err)
			}
		})
	}
}

func TestLoadParseRules(t *testing.T) {
	e := NewEngine()
	src := "# comment\n\n  \nb, user1, role1, , 1, ALL\n# rbac-policy v1\nb, role1, perm1, VIP1, 1, ALL\n"
	if err := e.Load(src); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.GetBinding("user1", "role1", ""); !ok {
		t.Fatal("missing generic")
	}
	if _, ok := e.GetBinding("role1", "perm1", "VIP1"); !ok {
		t.Fatal("missing scenario")
	}
}

func TestSpecExample(t *testing.T) {
	src := `# rbac-policy v1
b, user1, role1, , 1, TIME:2026-01-01T00:00:00+08:00~
b, role1, perm1, VIP1, 1, ALL
b, role1, perm2, VIP2, 0, TIME:~2027-01-01T00:00:00+08:00;TIME:2027-06-01T00:00:00+08:00~2027-07-01T00:00:00+08:00
b, user2, role1, , 1, ALL
`
	e := NewEngine()
	if err := e.Load(src); err != nil {
		t.Fatal(err)
	}
	now := mustTime(t, "2026-06-01T00:00:00+08:00")
	if !e.Enforce("user1", "role1", &EvalContext{Now: now}) {
		t.Fatal("user1-role1")
	}
	if e.Enforce("user1", "perm1", &EvalContext{Now: now}) {
		t.Fatal("named VIP1 should not match empty scenarios")
	}
	if !e.Enforce("user1", "perm1", &EvalContext{Now: now, Scenarios: []string{"VIP1"}}) {
		t.Fatal("with VIP1")
	}
	if e.Enforce("user1", "perm2", &EvalContext{Now: now, Scenarios: []string{"VIP2"}}) {
		t.Fatal("disabled")
	}
}
