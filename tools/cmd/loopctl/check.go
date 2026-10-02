package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"birdcage/tools/internal/loopkit"
)

// state is everything loopctl knows about the loop on disk.
type state struct {
	root       string
	policy     loopkit.Policy
	backlog    loopkit.Backlog
	iterations map[string]*loopkit.Iteration
	ledger     map[string][]loopkit.Entry // iteration id → entries (file order)
	entries    map[string]loopkit.Entry   // evidence id → entry
}

func load(root string) (*state, []string) {
	var errs []string
	add := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }
	s := &state{root: root, iterations: map[string]*loopkit.Iteration{}, ledger: map[string][]loopkit.Entry{}, entries: map[string]loopkit.Entry{}}
	v := loopkit.NewValidator(root)

	if err := v.ValidateFile("loop/schema/policy.schema.json", "loop/policy.json"); err != nil {
		add("R-schema: %v", err)
	}
	if err := loopkit.ReadJSON(root, "loop/policy.json", &s.policy); err != nil {
		add("policy: %v", err)
	}
	if err := v.ValidateFile("loop/schema/backlog.schema.json", "loop/backlog.json"); err != nil {
		add("R-schema: %v", err)
	}
	if err := loopkit.ReadJSON(root, "loop/backlog.json", &s.backlog); err != nil {
		add("backlog: %v", err)
	}

	its, _ := filepath.Glob(filepath.Join(root, "loop", "iterations", "*.json"))
	for _, p := range its {
		rel := "loop/iterations/" + filepath.Base(p)
		if err := v.ValidateFile("loop/schema/iteration.schema.json", rel); err != nil {
			add("R-schema: %v", err)
			continue
		}
		var it loopkit.Iteration
		if err := loopkit.ReadJSON(root, rel, &it); err != nil {
			add("%s: %v", rel, err)
			continue
		}
		if stem := strings.TrimSuffix(filepath.Base(p), ".json"); stem != it.ID {
			add("%s: id %s does not match file name", rel, it.ID)
		}
		s.iterations[it.ID] = &it
	}

	ls, _ := filepath.Glob(filepath.Join(root, "loop", "ledger", "*.jsonl"))
	for _, p := range ls {
		rel := "loop/ledger/" + filepath.Base(p)
		if err := v.ValidateLines("loop/schema/ledger.schema.json", rel); err != nil {
			add("R-schema: %v", err)
			continue
		}
		stem := strings.TrimSuffix(filepath.Base(p), ".jsonl")
		entries, err := readLedger(p)
		if err != nil {
			add("%s: %v", rel, err)
			continue
		}
		for _, e := range entries {
			if e.Iteration != stem {
				add("%s: entry %s belongs to iteration %s", rel, e.ID, e.Iteration)
			}
			if _, dup := s.entries[e.ID]; dup {
				add("%s: duplicate evidence id %s", rel, e.ID)
			}
			s.entries[e.ID] = e
		}
		s.ledger[stem] = entries
	}
	if err := v.ValidateFile("docs/decisions/premises.schema.json", "docs/decisions/premises.json"); err != nil {
		add("R-schema: %v", err)
	}
	return s, errs
}

func readLedger(p string) ([]loopkit.Entry, error) {
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []loopkit.Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	for sc.Scan() {
		if len(bytes.TrimSpace(sc.Bytes())) == 0 {
			continue
		}
		var e loopkit.Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

// checkRules applies the cross-file rules of loop/policy.json. Returns errors and warnings.
func checkRules(s *state, now time.Time) (errs, warns []string) {
	add := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }
	warn := func(format string, a ...any) { warns = append(warns, fmt.Sprintf(format, a...)) }
	p := s.policy

	tasks := map[string]*loopkit.Task{}
	for i := range s.backlog.Tasks {
		t := &s.backlog.Tasks[i]
		if tasks[t.ID] != nil {
			add("R-schema: backlog: duplicate task %s", t.ID)
		}
		tasks[t.ID] = t
	}

	// 증적: 인용 대상 존재, 웹 근거 규칙.
	for _, e := range s.entries {
		for _, c := range e.Cites {
			if _, ok := s.entries[c]; !ok {
				add("R-cites: ledger %s: cites unknown evidence %s", e.ID, c)
			}
		}
		if e.Kind == "web" {
			if e.Source == "" || e.ObservedAt == "" {
				add("R-web-source: ledger %s: web evidence needs source (primary|secondary) and observedAt", e.ID)
			}
			if e.Source == "secondary" && !citesPrimaryWeb(s, e) {
				add("R-web-source: ledger %s: secondary web evidence must cite a primary source entry", e.ID)
			}
		}
	}
	for id := range s.ledger {
		if s.iterations[id] == nil {
			add("R-schema: ledger %s: no iteration record loop/iterations/%s.json", id, id)
		}
	}

	// 반복 기록: 종료 상태, 성공 조건, 회고, 예산, 정체.
	perTask := map[string][]*loopkit.Iteration{}
	for _, it := range s.iterations {
		perTask[it.Task] = append(perTask[it.Task], it)
		if tasks[it.Task] == nil {
			add("R-schema: iteration %s: unknown task %s", it.ID, it.Task)
		}
		if it.State == "running" {
			if t, err := time.Parse(time.RFC3339, it.StartedAt); err == nil && now.Sub(t) > time.Duration(p.Budgets.MaxIterationMinutes)*time.Minute {
				warn("R-budget: iteration %s: running for %s (budget %dm) — end it as exhausted or stalled", it.ID, now.Sub(t).Round(time.Minute), p.Budgets.MaxIterationMinutes)
			}
			continue
		}
		if it.EndedAt == "" || it.Head == nil {
			add("R-terminal: iteration %s: terminal state %s needs endedAt and head", it.ID, it.State)
		}
		if it.State == "success" {
			for _, req := range p.Evidence.SuccessRequires {
				if !satisfiedBy(s.ledger[it.ID], req) {
					add("R-success-evidence: iteration %s: success needs passing %s evidence at %s or stronger", it.ID, req.Kind, req.MaxLevel)
				}
			}
		}
		if (it.State == "success" || it.State == "stalled" || it.State == "exhausted") && it.Retro.Empty() {
			add("R-retro: iteration %s: %s needs a retrospective (new check, spec change, lesson, or explicit none)", it.ID, it.State)
		}
	}
	// 세션 하나에 작업 하나: 같은 세션 id가 여러 작업에 쓰이면 위반(bootstrap 세션만 예외).
	if p.Sessions.OneTaskPerSession {
		bySession := map[string]map[string]bool{}
		for _, it := range s.iterations {
			if it.Session.ID == "" || it.Session.Kind == "bootstrap" {
				continue
			}
			if bySession[it.Session.ID] == nil {
				bySession[it.Session.ID] = map[string]bool{}
			}
			bySession[it.Session.ID][it.Task] = true
		}
		for sid, ts := range bySession {
			if len(ts) > 1 {
				add("R-one-task-session: session %s worked on %d tasks; policy is one task per session (start a fresh session)", sid, len(ts))
			}
		}
	}
	for id, its := range perTask {
		t := tasks[id]
		if t == nil {
			continue
		}
		if len(its) > p.Budgets.MaxIterationsPerTask && t.Status != "blocked" && t.Status != "done" {
			add("R-budget: task %s: %d iterations exceed budget %d — mark blocked/exhausted and escalate", id, len(its), p.Budgets.MaxIterationsPerTask)
		}
		if sig := repeatedSignature(its, p.Budgets.StallAfterSameSignature); sig != "" && t.Status != "blocked" && t.Status != "done" {
			add("R-stall: task %s: failure signature %s repeated %d times — stalled, change strategy or escalate", id, sig, p.Budgets.StallAfterSameSignature)
		}
	}

	// 백로그: passes는 증적으로만.
	for _, t := range s.backlog.Tasks {
		for _, d := range t.DependsOn {
			if tasks[d] == nil {
				add("R-schema: task %s: depends on unknown task %s", t.ID, d)
			}
		}
		if t.Passes != (t.Status == "done") {
			add("R-passes-evidence: task %s: passes=%v but status=%s (done ⇔ passes)", t.ID, t.Passes, t.Status)
		}
		if !t.Passes {
			continue
		}
		var cited []loopkit.Entry
		for _, id := range t.Evidence {
			e, ok := s.entries[id]
			if !ok {
				add("R-passes-evidence: task %s: evidence %s not in ledger", t.ID, id)
				continue
			}
			cited = append(cited, e)
		}
		for _, req := range t.Acceptance {
			if !satisfiedBy(cited, req) {
				add("R-passes-evidence: task %s: passes=true but no cited passing %s evidence at %s or stronger (%s)", t.ID, req.Kind, req.MaxLevel, req.What)
			}
		}
	}

	errs = append(errs, checkADRCoverage(s.root)...)
	errs = append(errs, checkGateLadder(s.root)...)
	errs = append(errs, checkPrinciples(s.root)...)
	e2, w2 := checkStorage(s.root, p)
	errs, warns = append(errs, e2...), append(warns, w2...)
	sort.Strings(errs)
	sort.Strings(warns)
	return errs, warns
}

func satisfiedBy(entries []loopkit.Entry, req loopkit.Requirement) bool {
	for _, e := range entries {
		if loopkit.Satisfies(e, req) {
			return true
		}
	}
	return false
}

func citesPrimaryWeb(s *state, e loopkit.Entry) bool {
	for _, c := range e.Cites {
		if x, ok := s.entries[c]; ok && x.Kind == "web" && x.Source == "primary" {
			return true
		}
	}
	return false
}

// repeatedSignature returns a failure signature present in the last n iterations (by start time).
func repeatedSignature(its []*loopkit.Iteration, n int) string {
	if len(its) < n {
		return ""
	}
	sorted := append([]*loopkit.Iteration(nil), its...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].StartedAt < sorted[j].StartedAt })
	last := sorted[len(sorted)-n:]
	for _, sig := range last[0].FailureSignatures {
		all := true
		for _, it := range last[1:] {
			found := false
			for _, s := range it.FailureSignatures {
				if s == sig {
					found = true
					break
				}
			}
			all = all && found
		}
		if all {
			return sig
		}
	}
	return ""
}

var adrFile = regexp.MustCompile(`^([0-9]{4})-.*\.md$`)

// checkADRCoverage: 모든 ADR은 실행 가능한 전제를 최소 하나 가진다.
func checkADRCoverage(root string) []string {
	ps, err := loadPremises(root)
	if err != nil {
		return []string{"premises: " + err.Error()}
	}
	has := map[string]bool{}
	for _, p := range ps {
		has[p.ADR] = true
	}
	var errs []string
	files, _ := os.ReadDir(filepath.Join(root, "docs", "decisions"))
	adrs := map[string]bool{}
	for _, f := range files {
		if m := adrFile.FindStringSubmatch(f.Name()); m != nil {
			adrs[m[1]] = true
			if !has[m[1]] {
				errs = append(errs, fmt.Sprintf("R-adr-premise: ADR %s has no executable premise in docs/decisions/premises.json", m[1]))
			}
		}
	}
	for _, p := range ps {
		if !adrs[p.ADR] {
			errs = append(errs, fmt.Sprintf("R-adr-premise: premise %s refers to missing ADR %s", p.ID, p.ADR))
		}
	}
	return errs
}

// checkStorage: 원본 산출물은 커밋 금지, 커밋 대상은 작고 비밀이 없어야 한다.
func checkStorage(root string, p loopkit.Policy) (errs, warns []string) {
	tracked, err := loopkit.TrackedFiles(root)
	if err != nil {
		warns = append(warns, "git ls-files: "+err.Error())
	}
	for _, f := range tracked {
		if loopkit.MatchAny(p.Storage.Local, f) {
			errs = append(errs, fmt.Sprintf("R-storage-local: %s is local-only (storage.local) but tracked by git", f))
		}
	}
	files, _ := loopkit.Files(root, p.Storage.Commit...)
	var res []*regexp.Regexp
	for _, pat := range p.Storage.RedactPatterns {
		if re, err := regexp.Compile(pat); err == nil {
			res = append(res, re)
		} else {
			errs = append(errs, "R-redact: bad redact pattern "+pat)
		}
	}
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			continue
		}
		if int64(len(b)) > p.Storage.MaxCommittedFileBytes {
			errs = append(errs, fmt.Sprintf("R-storage-size: %s is %d bytes > %d (move raw data to loop/.runs and cite it)", f, len(b), p.Storage.MaxCommittedFileBytes))
		}
		if f == "loop/policy.json" {
			continue // 패턴 정의 파일 자체는 검사하지 않는다
		}
		for _, re := range res {
			if loc := re.FindIndex(b); loc != nil {
				errs = append(errs, fmt.Sprintf("R-redact: %s matches redact pattern %q", f, re.String()))
			}
		}
	}
	return errs, warns
}
