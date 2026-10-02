package loopkit

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		glob, path string
		want       bool
	}{
		{"loop/schema/**", "loop/schema/policy.schema.json", true},
		{"loop/schema/**", "loop/policy.json", false},
		{"helper/**/*_test.go", "helper/internal/jobs/jobs_test.go", true},
		{"helper/**/*_test.go", "helper/cmd/x/main_test.go", true},
		{"helper/**/*_test.go", "helper/internal/jobs/jobs.go", false},
		{"helper/**/*_test.go", "tools/cmd/x/a_test.go", false},
		{"**/*.md", "README.md", true},
		{"loop/.runs/**", "loop/.runs/20261002-000000-T-001/001-go.log", true},
		{"specs/**", "specs/failures.json", true},
		{"panel/cep/src/jsx/**/*.ts", "panel/cep/src/jsx/ppro/ppro.ts", true},
		{"panel/cep/src/jsx/**/*.ts", "panel/cep/src/jsx/index.ts", true},
	}
	for _, c := range cases {
		if got := Match(c.glob, c.path); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.glob, c.path, got, c.want)
		}
	}
}

func TestSatisfies(t *testing.T) {
	e := Entry{Kind: "test", Level: "L1", Verdict: "pass"}
	if !Satisfies(e, Requirement{Kind: "test", MaxLevel: "L3"}) {
		t.Error("L1 evidence must satisfy an L3 requirement")
	}
	if Satisfies(Entry{Kind: "test", Level: "L4", Verdict: "pass"}, Requirement{Kind: "test", MaxLevel: "L1"}) {
		t.Error("L4 evidence must not satisfy an L1 requirement")
	}
	if Satisfies(Entry{Kind: "test", Level: "L1", Verdict: "fail"}, Requirement{Kind: "test", MaxLevel: "L1"}) {
		t.Error("failing evidence never satisfies")
	}
}
