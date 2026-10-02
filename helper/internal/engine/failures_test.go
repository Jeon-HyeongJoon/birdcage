package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"birdcage/helper/internal/perf/perftest"
)

func TestMain(m *testing.M) { perftest.Main(m) }

// The generated table must agree with the SSOT spec at runtime (specgen -check covers
// file freshness; this covers the compiled behaviour).
func TestFailureTableMatchesSpec(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "specs", "failures.json"))
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Kinds []struct{ ID, Policy, Message string }
	}
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatal(err)
	}
	if len(spec.Kinds) != len(AllKinds) {
		t.Fatalf("spec has %d kinds, generated %d", len(spec.Kinds), len(AllKinds))
	}
	for i, k := range spec.Kinds {
		got := AllKinds[i]
		if string(got) != k.ID || string(PolicyFor(got)) != k.Policy || MessageFor(got) != k.Message {
			t.Errorf("kind %s: generated (%s, %s, %q) != spec (%s, %s, %q)", k.ID, got, PolicyFor(got), MessageFor(got), k.ID, k.Policy, k.Message)
		}
		f := Fail(got, "detail")
		if f.Kind != got || f.Message == "" || f.Policy != PolicyFor(got) {
			t.Errorf("Fail(%s) = %+v", got, f)
		}
	}
	if PolicyFor("no_such_kind") != PolicyStop {
		t.Error("unknown kinds must stop")
	}
}
