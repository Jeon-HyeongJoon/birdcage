package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"birdcage/tools/internal/loopkit"
)

const timeFmt = "20060102-150405"

func runsDir(root, iter string) string { return filepath.Join(root, "loop", ".runs", iter) }

func currentIter(root, flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	b, err := os.ReadFile(filepath.Join(root, "loop", ".runs", "CURRENT"))
	if err != nil {
		return "", errors.New("no current iteration (run `loopctl begin` or pass --iter)")
	}
	return strings.TrimSpace(string(b)), nil
}

func gitRef(root string) loopkit.GitRef {
	br, _ := loopkit.Git(root, "rev-parse", "--abbrev-ref", "HEAD")
	c, _ := loopkit.Git(root, "rev-parse", "--short=12", "HEAD")
	st, _ := loopkit.Git(root, "status", "--porcelain")
	return loopkit.GitRef{Branch: br, Commit: c, Dirty: st != ""}
}

// begin starts an iteration: one task, one session.
func begin(s *state, task, kind, hypothesis, sessionID, model string, now time.Time) (string, error) {
	var t *loopkit.Task
	for i := range s.backlog.Tasks {
		if s.backlog.Tasks[i].ID == task {
			t = &s.backlog.Tasks[i]
		}
	}
	if t == nil {
		return "", fmt.Errorf("unknown task %s", task)
	}
	if t.Status == "done" {
		return "", fmt.Errorf("task %s is already done", task)
	}
	if unmet := readiness(s, t, kind, now); len(unmet) > 0 {
		return "", fmt.Errorf("R-readiness: not ready for a %s loop:\n  - %s\n(0→1 단계는 사람 주도 대화형으로 진행: --kind design)", kind, strings.Join(unmet, "\n  - "))
	}
	n := 0
	for _, it := range s.iterations {
		if it.Task != task {
			continue
		}
		n++
		if it.State == "running" {
			return "", fmt.Errorf("task %s already has running iteration %s (end it first)", task, it.ID)
		}
	}
	if n >= s.policy.Budgets.MaxIterationsPerTask {
		return "", fmt.Errorf("task %s used %d/%d iterations: end as exhausted and escalate", task, n, s.policy.Budgets.MaxIterationsPerTask)
	}
	id := now.UTC().Format(timeFmt) + "-" + task
	it := loopkit.Iteration{
		Schema: "../schema/iteration.schema.json", ID: id, Task: task,
		Session:   loopkit.Session{Kind: kind, ID: sessionID, Model: model},
		StartedAt: now.UTC().Format(time.RFC3339), Base: gitRef(s.root), State: "running", Hypothesis: hypothesis,
	}
	if err := loopkit.WriteJSON(s.root, "loop/iterations/"+id+".json", it); err != nil {
		return "", err
	}
	t.Status = "doing"
	if err := saveBacklog(s); err != nil {
		return "", err
	}
	if err := os.MkdirAll(runsDir(s.root, id), 0o755); err != nil {
		return "", err
	}
	return id, os.WriteFile(filepath.Join(s.root, "loop", ".runs", "CURRENT"), []byte(id+"\n"), 0o644)
}

func saveBacklog(s *state) error {
	if s.backlog.Schema == "" {
		s.backlog.Schema = "./schema/backlog.schema.json"
	}
	return loopkit.WriteJSON(s.root, "loop/backlog.json", s.backlog)
}

func saveIteration(s *state, it *loopkit.Iteration) error {
	return loopkit.WriteJSON(s.root, "loop/iterations/"+it.ID+".json", it)
}

// appendEvidence assigns an id, validates the entry and appends it to the iteration's ledger.
func appendEvidence(s *state, e loopkit.Entry, artifact []byte, artifactName string, now time.Time) (loopkit.Entry, error) {
	it := s.iterations[e.Iteration]
	if it == nil {
		return e, fmt.Errorf("unknown iteration %s", e.Iteration)
	}
	if it.State != "running" {
		return e, fmt.Errorf("iteration %s is %s; evidence can only be added while running", it.ID, it.State)
	}
	e.ID = fmt.Sprintf("E-%s-%03d", e.Iteration[:15], len(s.ledger[e.Iteration])+1)
	e.At = now.UTC().Format(time.RFC3339)
	if artifact != nil {
		dir := runsDir(s.root, e.Iteration)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return e, err
		}
		name := fmt.Sprintf("%03d-%s", len(s.ledger[e.Iteration])+1, artifactName)
		if err := os.WriteFile(filepath.Join(dir, name), artifact, 0o644); err != nil {
			return e, err
		}
		sum := sha256.Sum256(artifact)
		e.Artifact = &loopkit.Artifact{Path: "loop/.runs/" + e.Iteration + "/" + name, SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(artifact))}
	}
	if err := redactCheck(s.policy, e.Claim+" "+e.Ref.Value); err != nil {
		return e, err
	}
	line, err := json.Marshal(e)
	if err != nil {
		return e, err
	}
	if err := loopkit.NewValidator(s.root).ValidateBytes("loop/schema/ledger.schema.json", line); err != nil {
		return e, fmt.Errorf("evidence rejected by schema: %w", err)
	}
	p := filepath.Join(s.root, "loop", "ledger", e.Iteration+".jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return e, err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return e, err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return e, err
	}
	s.ledger[e.Iteration] = append(s.ledger[e.Iteration], e)
	s.entries[e.ID] = e
	return e, nil
}

func redactCheck(p loopkit.Policy, text string) error {
	for _, pat := range p.Storage.RedactPatterns {
		if re, err := regexp.Compile(pat); err == nil && re.MatchString(text) {
			return fmt.Errorf("refusing to record text matching redact pattern %q", pat)
		}
	}
	return nil
}

var (
	digits  = regexp.MustCompile(`[0-9]+`)
	pathish = regexp.MustCompile(`(/[^\s:]+)+`)
)

// signature identifies "the same failure" across iterations: claim + normalized last lines.
func signature(claim string, out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var keep []string
	for i := len(lines) - 1; i >= 0 && len(keep) < 3; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		l = pathish.ReplaceAllString(l, "<path>")
		l = digits.ReplaceAllString(l, "#")
		keep = append(keep, l)
	}
	sum := sha256.Sum256([]byte(claim + "\n" + strings.Join(keep, "\n")))
	return "sig:" + hex.EncodeToString(sum[:6])
}

// runCommand executes argv, stores the output as an artifact and records pass/fail evidence.
func runCommand(ctx context.Context, s *state, iter, kind, level, claim, dir string, argv []string, now func() time.Time) (loopkit.Entry, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = filepath.Join(s.root, filepath.FromSlash(dir))
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	t0 := time.Now()
	err := cmd.Run()
	dur := time.Since(t0)
	exit := 0
	if err != nil {
		exit = -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exit = ee.ExitCode()
		}
	}
	verdict := "pass"
	if err != nil {
		verdict = "fail"
	}
	e := loopkit.Entry{Iteration: iter, Kind: kind, Level: level, Claim: claim, Verdict: verdict,
		Ref:        loopkit.Ref{Type: "command", Value: strings.TrimSpace((dir + " $ ") + strings.Join(argv, " "))},
		Values:     map[string]any{"exit": exit},
		DurationMs: float64(dur.Milliseconds())}
	e, aerr := appendEvidence(s, e, out.Bytes(), logSlug(argv)+".log", now())
	if aerr != nil {
		return e, aerr
	}
	if verdict == "fail" {
		it := s.iterations[iter]
		it.FailureSignatures = appendUnique(it.FailureSignatures, signature(claim, out.Bytes()))
		if err := saveIteration(s, it); err != nil {
			return e, err
		}
	}
	return e, nil
}

func appendUnique(xs []string, x string) []string {
	for _, y := range xs {
		if y == x {
			return xs
		}
	}
	return append(xs, x)
}

// guardViolations lists protected files modified or deleted since base (committed or not).
func guardViolations(s *state, base string) ([]string, error) {
	var changed []string
	add := func(out string) {
		for _, l := range strings.Split(out, "\n") {
			f := strings.Fields(l)
			if len(f) < 2 {
				continue
			}
			st, path := f[0], f[len(f)-1]
			if strings.HasPrefix(st, "M") || strings.HasPrefix(st, "D") || strings.HasPrefix(st, "R") {
				changed = append(changed, path)
			}
		}
	}
	out, err := loopkit.Git(s.root, "diff", "--name-status", base)
	if err != nil {
		return nil, err
	}
	add(out)
	var v []string
	seen := map[string]bool{}
	for _, f := range changed {
		if !seen[f] && loopkit.MatchAny(s.policy.Protected.Paths, f) {
			v = append(v, f)
		}
		seen[f] = true
	}
	sort.Strings(v)
	return v, nil
}

func approved(it *loopkit.Iteration, action string) bool {
	for _, a := range it.Approvals {
		if a.Action == action {
			return true
		}
	}
	return false
}

type endOpts struct {
	tokens                    int
	usd                       float64
	state, summary            string
	next, checks, specs, less []string
	retroNone, passTask       bool
}

// end finalizes an iteration and, on success, may flip the task's passes flag.
func end(s *state, iter string, o endOpts, now time.Time) error {
	it := s.iterations[iter]
	if it == nil {
		return fmt.Errorf("unknown iteration %s", iter)
	}
	if it.State != "running" {
		return fmt.Errorf("iteration %s already ended (%s)", iter, it.State)
	}
	valid := false
	for _, t := range s.policy.TerminalStates {
		valid = valid || t == o.state
	}
	if !valid {
		return fmt.Errorf("state %q is not a terminal state %v", o.state, s.policy.TerminalStates)
	}
	it.State, it.Summary, it.Next = o.state, o.summary, o.next
	it.Retro = &loopkit.Retro{NewChecks: o.checks, SpecChanges: o.specs, Lessons: o.less, None: o.retroNone}
	if it.Retro.Empty() {
		it.Retro = nil
	}
	head := gitRef(s.root)
	it.Head = &head
	it.EndedAt = now.UTC().Format(time.RFC3339)
	m := &loopkit.Metrics{EvidenceByLevel: map[string]int{}}
	if st, err := time.Parse(time.RFC3339, it.StartedAt); err == nil {
		m.DurationMin = float64(int(now.Sub(st).Minutes()*10)) / 10
	}
	for _, e := range s.ledger[iter] {
		m.Evidence++
		m.EvidenceByLevel[e.Level]++
		switch e.Verdict {
		case "pass":
			m.Pass++
		case "fail":
			m.Fail++
		case "skip":
			m.Skip++
		}
	}
	m.Tokens, m.USD = o.tokens, o.usd
	it.Metrics = m

	var task *loopkit.Task
	for i := range s.backlog.Tasks {
		if s.backlog.Tasks[i].ID == it.Task {
			task = &s.backlog.Tasks[i]
		}
	}
	if o.state == "success" {
		for _, req := range s.policy.Evidence.SuccessRequires {
			if !satisfiedBy(s.ledger[iter], req) {
				return fmt.Errorf("success refused: no passing %s evidence at %s or stronger in this iteration", req.Kind, req.MaxLevel)
			}
		}
		if it.Retro.Empty() {
			return errors.New("success refused: record a retrospective (--check/--spec/--lesson or --retro-none)")
		}
		v, err := guardViolations(s, it.Base.Commit)
		if err != nil {
			return err
		}
		if len(v) > 0 && !approved(it, s.policy.Protected.Gate) {
			return fmt.Errorf("success refused: protected files changed without %s approval: %s", s.policy.Protected.Gate, strings.Join(v, ", "))
		}
	}
	if o.passTask && task != nil {
		if o.state != "success" {
			return errors.New("--pass-task requires --state success")
		}
		var ids []string
		for _, req := range task.Acceptance {
			found := ""
			for _, e := range s.ledger[iter] {
				if loopkit.Satisfies(e, req) {
					found = e.ID
					break
				}
			}
			if found == "" {
				return fmt.Errorf("task %s acceptance not met: %s %s (%s)", task.ID, req.Kind, req.MaxLevel, req.What)
			}
			ids = appendUnique(ids, found)
		}
		task.Passes, task.Status, task.Evidence = true, "done", ids
	} else if task != nil {
		switch o.state {
		case "blocked", "stalled", "exhausted":
			task.Status, task.BlockedBy = "blocked", o.state+": "+o.summary
		default:
			if task.Status == "doing" {
				task.Status = "open"
			}
		}
	}
	if err := saveIteration(s, it); err != nil {
		return err
	}
	if err := saveBacklog(s); err != nil {
		return err
	}
	cur := filepath.Join(s.root, "loop", ".runs", "CURRENT")
	if b, _ := os.ReadFile(cur); strings.TrimSpace(string(b)) == iter {
		_ = os.Remove(cur)
	}
	return nil
}

// maxSlug keeps artifact names well under file-name limits (255 bytes) with the sequence prefix.
const maxSlug = 60

// logSlug names a command's log from its first words, e.g. "go-test-count-1".
// sh -c "<script>" 같은 긴 인자도 파일 이름 한도를 넘지 않게 자른다.
func logSlug(argv []string) string {
	if len(argv) == 0 {
		return "run"
	}
	slug := strings.Trim(regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(filepath.Base(argv[0])+"-"+strings.Join(argv[1:min(len(argv), 3)], "-")), "-"), "-")
	if len(slug) > maxSlug {
		slug = strings.TrimRight(slug[:maxSlug], "-")
	}
	if slug == "" {
		return "run"
	}
	return slug
}
