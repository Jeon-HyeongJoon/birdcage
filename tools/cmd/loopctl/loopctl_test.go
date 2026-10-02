package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"birdcage/tools/internal/loopkit"
)

// sandbox builds a throwaway git repo with the real policy and schemas.
func sandbox(t *testing.T) string {
	t.Helper()
	real, err := loopkit.Root(".")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	copyFile := func(rel string) {
		b, err := os.ReadFile(filepath.Join(real, rel))
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{"loop/policy.json", "loop/schema/policy.schema.json", "loop/schema/backlog.schema.json",
		"loop/schema/iteration.schema.json", "loop/schema/ledger.schema.json", "docs/decisions/premises.schema.json",
		"specs/principles.schema.json"} {
		copyFile(rel)
	}
	write(t, dir, "docs/decisions/0001-x.md", "# x\n")
	write(t, dir, "specs/principles.json", `{"version":1,"groups":[{"id":"loop","title":"l"}],"principles":[
	 {"id":"L5","group":"loop","rule":"stall","why":"w","status":"enforced","enforcedBy":[{"type":"loopctl-rule","ref":"R-stall"},{"type":"policy","ref":"budgets.stallAfterSameSignature"}]}]}`)
	write(t, dir, "docs/decisions/premises.json", `{"premises":[{"id":"P-0001-a","adr":"0001","claim":"c","level":"L1","check":{"type":"file-regex","path":"docs/decisions/0001-x.md","pattern":"x"}}]}`)
	write(t, dir, "loop/backlog.json", `{"tasks":[
	 {"id":"T-001","title":"a","mode":"compile","status":"open","passes":false,"acceptance":[{"kind":"test","maxLevel":"L1","what":"unit tests"}]},
	 {"id":"T-002","title":"b","mode":"compile","status":"open","passes":false,"acceptance":[{"kind":"test","maxLevel":"L1","what":"unit tests"}]}]}`)
	write(t, dir, "helper/x_test.go", "package x\n")
	// readiness: 게이트가 요구하는 의존성 폴더(node_modules)가 있어야 worker 루프가 시작된다.
	write(t, dir, "panel/cep/node_modules/.keep", "")
	write(t, dir, "panel/core/node_modules/.keep", "")
	git(t, dir, "init", "-q")
	git(t, dir, "add", "-A")
	git(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "base")
	return dir
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

func loadOK(t *testing.T, root string) *state {
	t.Helper()
	s, errs := load(root)
	if len(errs) > 0 {
		t.Fatalf("load: %v", errs)
	}
	return s
}

func clock(start time.Time) func() time.Time {
	n := 0
	return func() time.Time { n++; return start.Add(time.Duration(n) * time.Second) }
}

func TestIterationLifecycle(t *testing.T) {
	root := sandbox(t)
	now := clock(time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC))
	s := loadOK(t, root)
	id, err := begin(s, "T-001", "worker", "unit tests prove it", "sess-1", "m", now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(id, "-T-001") {
		t.Fatalf("id %s", id)
	}
	s = loadOK(t, root)
	// 성공 선언은 L1 테스트 증적 없이는 거부된다.
	if err := end(s, id, endOpts{state: "success", summary: "x", retroNone: true}, now()); err == nil || !strings.Contains(err.Error(), "success refused") {
		t.Fatalf("success without evidence: %v", err)
	}
	s = loadOK(t, root)
	e, err := runCommand(context.Background(), s, id, "test", "L1", "true passes", "", []string{"true"}, now)
	if err != nil || e.Verdict != "pass" || e.Artifact == nil {
		t.Fatalf("run: %+v %v", e, err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(e.Artifact.Path))); err != nil {
		t.Fatal("artifact not stored locally:", err)
	}
	// 회고 없이도 거부.
	s = loadOK(t, root)
	if err := end(s, id, endOpts{state: "success", summary: "x"}, now()); err == nil || !strings.Contains(err.Error(), "retrospective") {
		t.Fatalf("success without retro: %v", err)
	}
	s = loadOK(t, root)
	if err := end(s, id, endOpts{state: "success", summary: "done", less: []string{"l"}, passTask: true}, now()); err != nil {
		t.Fatal(err)
	}
	s = loadOK(t, root)
	if task := s.backlog.Tasks[0]; !task.Passes || task.Status != "done" || len(task.Evidence) != 1 {
		t.Fatalf("task %+v", task)
	}
	if errs, _ := checkRules(s, now()); len(errs) > 0 {
		t.Fatalf("check after a clean iteration: %v", errs)
	}
}

func TestCheckCatchesViolations(t *testing.T) {
	root := sandbox(t)
	now := clock(time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC))
	// passes=true인데 증적 없음.
	write(t, root, "loop/backlog.json", `{"tasks":[{"id":"T-001","title":"a","mode":"compile","status":"done","passes":true,"acceptance":[{"kind":"test","maxLevel":"L1","what":"u"}]}]}`)
	// 원본 산출물을 커밋.
	write(t, root, "loop/.runs/x/raw.log", "raw")
	git(t, root, "add", "-f", "loop/.runs/x/raw.log")
	// 비밀이 담긴 원장.
	write(t, root, "loop/iterations/20261002-090000-T-001.json", `{"id":"20261002-090000-T-001","task":"T-001","session":{"kind":"worker"},"startedAt":"2026-10-02T09:00:00Z","base":{"branch":"main","commit":"abc"},"state":"running","hypothesis":"h"}`)
	write(t, root, "loop/ledger/20261002-090000-T-001.jsonl", `{"id":"E-20261002-090000-001","iteration":"20261002-090000-T-001","at":"2026-10-02T09:00:01Z","kind":"log","level":"L3","claim":"saw token=0123456789abcdef0123","verdict":"info","ref":{"type":"file","value":"x"}}`+"\n")
	// ADR에 전제가 없음.
	write(t, root, "docs/decisions/0002-y.md", "# y\n")
	s := loadOK(t, root)
	errs, _ := checkRules(s, now())
	joined := strings.Join(errs, "\n")
	for _, want := range []string{"passes=true but no cited passing test", "is local-only", "matches redact pattern", "ADR 0002 has no executable premise"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing violation %q in:\n%s", want, joined)
		}
	}
}

func TestStallAndSessionRules(t *testing.T) {
	root := sandbox(t)
	mk := func(id, task, sess, started string, sigs string) {
		write(t, root, "loop/iterations/"+id+".json", `{"id":"`+id+`","task":"`+task+`","session":{"kind":"worker","id":"`+sess+`"},"startedAt":"`+started+`","endedAt":"`+started+`","base":{"branch":"b","commit":"c"},"head":{"branch":"b","commit":"c"},"state":"no-op","hypothesis":"h","failureSignatures":[`+sigs+`]}`)
	}
	mk("20261002-090000-T-001", "T-001", "s1", "2026-10-02T09:00:00Z", `"sig:aaaaaaaaaaaa"`)
	mk("20261002-100000-T-001", "T-001", "s2", "2026-10-02T10:00:00Z", `"sig:aaaaaaaaaaaa"`)
	mk("20261002-110000-T-002", "T-002", "s2", "2026-10-02T11:00:00Z", ``)
	s := loadOK(t, root)
	errs, _ := checkRules(s, time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	joined := strings.Join(errs, "\n")
	if !strings.Contains(joined, "repeated 2 times — stalled") {
		t.Errorf("stall not detected:\n%s", joined)
	}
	if !strings.Contains(joined, "session s2 worked on 2 tasks") {
		t.Errorf("one-task-per-session not enforced:\n%s", joined)
	}
}

func TestGuardProtectedPaths(t *testing.T) {
	root := sandbox(t)
	now := clock(time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC))
	s := loadOK(t, root)
	id, err := begin(s, "T-001", "worker", "h", "s", "", now())
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "helper/x_test.go", "package x\n// weakened\n") // 기존 검증기 수정
	write(t, root, "helper/y_test.go", "package x\n")              // 추가는 허용
	s = loadOK(t, root)
	v, err := guardViolations(s, s.iterations[id].Base.Commit)
	if err != nil || len(v) != 1 || v[0] != "helper/x_test.go" {
		t.Fatalf("violations %v %v", v, err)
	}
	if _, err := runCommand(context.Background(), s, id, "test", "L1", "ok", "", []string{"true"}, now); err != nil {
		t.Fatal(err)
	}
	s = loadOK(t, root)
	if err := end(s, id, endOpts{state: "success", summary: "x", retroNone: true}, now()); err == nil || !strings.Contains(err.Error(), "protected files changed") {
		t.Fatalf("guard not enforced at end: %v", err)
	}
}

func TestPremiseTypes(t *testing.T) {
	root := sandbox(t)
	write(t, root, "a/ascii.ts", "const a = 1;\n")
	write(t, root, "a/korean.ts", "// 한글\n")
	write(t, root, "pkg1/a_test.go", "perftest.Main(m)")
	write(t, root, "pkg2/b_test.go", "nothing")
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		c    premiseCheck
		want string
	}{
		{premiseCheck{Type: "file-regex", Path: "a/ascii.ts", Pattern: `const a`}, "pass"},
		{premiseCheck{Type: "file-regex", Path: "a/ascii.ts", Pattern: `nope`}, "fail"},
		{premiseCheck{Type: "file-ascii", Paths: []string{"a/ascii.ts"}}, "pass"},
		{premiseCheck{Type: "file-ascii", Paths: []string{"a/*.ts"}}, "fail"},
		{premiseCheck{Type: "per-dir-regex", Glob: "pkg1/*_test.go", Pattern: `perftest\.Main`}, "pass"},
		{premiseCheck{Type: "per-dir-regex", Glob: "pkg*/*_test.go", Pattern: `perftest\.Main`}, "fail"},
		{premiseCheck{Type: "command", Argv: []string{"true"}}, "pass"},
		{premiseCheck{Type: "command", Argv: []string{"false"}}, "fail"},
		{premiseCheck{Type: "command", Argv: []string{"true"}, RequiresEnv: []string{"BIRDCAGE_TEST_UNSET_VAR"}}, "skip"},
		{premiseCheck{Type: "expires", VerifiedAt: "2026-09-01", TTLDays: 90}, "pass"},
		{premiseCheck{Type: "expires", VerifiedAt: "2026-01-01", TTLDays: 90}, "fail"},
	}
	for _, c := range cases {
		r := runPremise(context.Background(), root, premise{ID: "P-0001-a", Check: c.c}, now)
		if r.Verdict != c.want {
			t.Errorf("%+v → %s (%s), want %s", c.c, r.Verdict, r.Detail, c.want)
		}
	}
}

func TestSignatureNormalizes(t *testing.T) {
	a := signature("tests pass", []byte("--- FAIL: TestX (0.12s)\n/Users/a/x.go:12: boom\nFAIL"))
	b := signature("tests pass", []byte("--- FAIL: TestX (0.98s)\n/home/b/x.go:40: boom\nFAIL"))
	if a != b {
		t.Errorf("same failure with different timings/paths must share a signature: %s vs %s", a, b)
	}
	if c := signature("tests pass", []byte("--- FAIL: TestY\nother")); c == a {
		t.Error("different failures must differ")
	}
}
