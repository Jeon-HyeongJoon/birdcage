package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadinessRefusesUnreadyLoops(t *testing.T) {
	root := sandbox(t)
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	// 의존성이 없으면 worker 루프는 시작할 수 없다.
	if err := os.RemoveAll(filepath.Join(root, "panel", "core", "node_modules")); err != nil {
		t.Fatal(err)
	}
	s := loadOK(t, root)
	if _, err := begin(s, "T-001", "worker", "h", "s", "", now); err == nil || !strings.Contains(err.Error(), "R-readiness") {
		t.Fatalf("worker without gate deps must be refused: %v", err)
	}
	// 0→1 대화형(design)은 readiness 대상이 아니다.
	if _, err := begin(s, "T-001", "design", "h", "s", "", now); err != nil {
		t.Fatalf("design session must not need readiness: %v", err)
	}
	// 사람 작업·기계 판정 불가 작업은 자율 루프로 시작하지 않는다.
	write(t, root, "panel/core/node_modules/.keep", "")
	write(t, root, "loop/backlog.json", `{"tasks":[
	 {"id":"T-001","title":"a","mode":"human","status":"open","passes":false,"acceptance":[{"kind":"human","maxLevel":"L5","what":"x"}]},
	 {"id":"T-002","title":"b","mode":"compile","status":"open","passes":false,"acceptance":[{"kind":"review","maxLevel":"L4","what":"x"}]}]}`)
	s = loadOK(t, root)
	for _, id := range []string{"T-001", "T-002"} {
		var task = &s.backlog.Tasks[0]
		if id == "T-002" {
			task = &s.backlog.Tasks[1]
		}
		if unmet := readiness(s, task, "worker", now); len(unmet) == 0 {
			t.Errorf("%s should not be ready for a worker loop", id)
		}
	}
}

func TestGateLadderOrderRule(t *testing.T) {
	root := sandbox(t)
	if errs := checkGateLadder(root); len(errs) > 0 {
		t.Fatalf("real ladder must pass: %v", errs)
	}
	write(t, root, "loop/policy.json", strings.Replace(readFile(t, root, "loop/policy.json"), `"id": "G-fmt",
      "level": "L2",
      "kind": "build",
      "cost": "free"`, `"id": "G-fmt",
      "level": "L2",
      "kind": "build",
      "cost": "network"`, 1))
	if errs := checkGateLadder(root); len(errs) == 0 || !strings.Contains(strings.Join(errs, "\n"), "more expensive gate") {
		t.Fatalf("expensive gate before cheap ones must fail: %v", errs)
	}
}

func TestPrincipleRefsResolve(t *testing.T) {
	real := realRoot(t)
	if errs := checkPrinciples(real); len(errs) > 0 {
		t.Fatalf("repo principles must resolve: %v", errs)
	}
	root := sandbox(t)
	for _, rel := range []string{"specs/principles.schema.json"} {
		write(t, root, rel, readFile(t, real, rel))
	}
	write(t, root, "specs/principles.json", `{"version":1,"groups":[{"id":"work","title":"w"}],"principles":[
	 {"id":"W1","group":"work","rule":"r","why":"w","status":"enforced","enforcedBy":[{"type":"loopctl-rule","ref":"R-nope"}]},
	 {"id":"W2","group":"work","rule":"r","why":"w","status":"enforced","enforcedBy":[{"type":"policy","ref":"budgets.nope"},{"type":"honor"}]},
	 {"id":"W3","group":"work","rule":"r","why":"w","status":"partial","enforcedBy":[{"type":"gate","ref":"G-fmt"}]}]}`)
	joined := strings.Join(checkPrinciples(root), "\n")
	for _, want := range []string{"unknown loopctl rule R-nope", "has no key budgets.nope", "W2 is 'enforced' but relies on honor", "W3 is 'partial'"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
}

func TestBriefCompressesFailure(t *testing.T) {
	root := sandbox(t)
	now := clock(time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC))
	s := loadOK(t, root)
	id, err := begin(s, "T-001", "worker", "h", "s", "", now())
	if err != nil {
		t.Fatal(err)
	}
	s = loadOK(t, root)
	script := "i=0; while [ $i -lt 100 ]; do echo line$i; i=$((i+1)); done; echo boom >&2; exit 3"
	if _, err := runCommand(t.Context(), s, id, "test", "L1", "unit tests pass", "", []string{"sh", "-c", script}, now); err != nil {
		t.Fatal(err)
	}
	s = loadOK(t, root)
	out, err := brief(s, id, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "claim: unit tests pass") || !strings.Contains(out, "signatures: sig:") || !strings.Contains(out, "boom") {
		t.Fatalf("brief missing parts:\n%s", out)
	}
	if strings.Contains(out, "line10\n") || strings.Count(out, "\n") > 30 {
		t.Fatalf("brief must keep only the tail (policy.feedback.tailLines):\n%s", out)
	}
}

func realRoot(t *testing.T) string {
	t.Helper()
	wd, _ := os.Getwd()
	return filepath.Clean(filepath.Join(wd, "..", "..", ".."))
}

func readFile(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// 전제 검사 소요 시간이 실제로 기록되어야 한다(반환값에 반영되지 않던 버그 회귀 방지).
func TestPremiseDurationRecorded(t *testing.T) {
	root := sandbox(t)
	r := runPremise(t.Context(), root, premise{ID: "P-0001-a", Check: premiseCheck{Type: "command", Argv: []string{"sleep", "0.05"}}}, time.Now())
	if r.Verdict != "pass" || r.Duration < 40*time.Millisecond {
		t.Fatalf("duration not recorded: %v %s", r.Duration, r.Verdict)
	}
}
