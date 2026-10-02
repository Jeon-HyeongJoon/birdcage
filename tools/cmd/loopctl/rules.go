package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"birdcage/tools/internal/loopkit"
)

// rules is the registry of enforcement mechanisms that principles may reference
// (specs/principles.json enforcedBy {type: loopctl-rule}). R-principle-refs fails if a
// principle points at a rule that is not here.
var rules = map[string]string{
	"R-schema":           "모든 루프 파일이 스키마를 통과하고 참조(작업·반복)가 존재한다",
	"R-cites":            "증적의 cites는 존재하는 증적을 가리킨다",
	"R-web-source":       "웹 증적은 source·observedAt을 갖고, 2차는 1차를 인용한다",
	"R-success-evidence": "success 반복은 policy.evidence.successRequires를 만족하는 증적을 가진다",
	"R-passes-evidence":  "passes=true 작업은 acceptance를 만족하는 증적을 인용한다(done ⇔ passes)",
	"R-retro":            "success·stalled·exhausted 반복은 회고를 가진다",
	"R-terminal":         "반복은 policy.terminalStates 중 하나로 끝나고 endedAt·head를 가진다",
	"R-budget":           "작업당 반복 수와 반복 시간 예산",
	"R-stall":            "같은 실패 서명이 연속으로 반복되면 정체로 본다",
	"R-one-task-session": "세션 하나는 작업 하나만 다룬다(bootstrap 제외)",
	"R-adr-premise":      "모든 ADR은 실행 가능한 전제를 가진다",
	"R-storage-local":    "storage.local 경로(원본 산출물)는 git에 올라가지 않는다",
	"R-storage-size":     "커밋 대상 루프 파일은 크기 상한 이하",
	"R-redact":           "커밋 대상과 증적 기록에 비밀 패턴이 없다",
	"R-guard-protected":  "보호 경로의 수정·삭제는 verifier.change 승인이 필요하다(loopctl guard, end)",
	"R-gate-order":       "게이트 사다리는 비용 순(free→slow→network→tokens→human)이고 readiness 게이트가 존재한다",
	"R-readiness":        "worker·watch 반복은 readiness 조건을 만족해야 시작된다(loopctl begin)",
	"R-brief":            "모델에 넘기는 실패 피드백은 policy.feedback 형식으로 압축한다(loopctl brief)",
	"R-principle-refs":   "원칙이 가리키는 규칙·전제·게이트·정책 키가 실제로 존재하고 상태가 일관된다",
}

var costRank = map[string]int{"free": 0, "slow": 1, "network": 2, "tokens": 3, "human": 4}

type ladderGate struct {
	ID       string   `json:"id"`
	Level    string   `json:"level"`
	Kind     string   `json:"kind"`
	Cost     string   `json:"cost"`
	What     string   `json:"what"`
	Argv     []string `json:"argv"`
	Dir      string   `json:"dir"`
	Optional bool     `json:"optional"`
	Requires []string `json:"requires"`
}

type ladderPolicy struct {
	GateLadder []ladderGate `json:"gateLadder"`
	Readiness  struct {
		AppliesTo                 []string `json:"appliesTo"`
		RequireGates              []string `json:"requireGates"`
		CompileAcceptanceMaxLevel string   `json:"compileAcceptanceMaxLevel"`
		SpecChurn                 struct {
			Paths      []string `json:"paths"`
			WindowDays int      `json:"windowDays"`
			MaxCommits int      `json:"maxCommits"`
		} `json:"specChurn"`
	} `json:"readiness"`
	Feedback struct {
		TailLines int `json:"tailLines"`
	} `json:"feedback"`
}

func readLadder(root string) (ladderPolicy, error) {
	var lp ladderPolicy
	return lp, loopkit.ReadJSON(root, "loop/policy.json", &lp)
}

// checkGateLadder: R-gate-order.
func checkGateLadder(root string) []string {
	lp, err := readLadder(root)
	if err != nil {
		return []string{"R-gate-order: " + err.Error()}
	}
	var errs []string
	ids := map[string]bool{}
	prev := -1
	for _, g := range lp.GateLadder {
		if ids[g.ID] {
			errs = append(errs, "R-gate-order: duplicate gate "+g.ID)
		}
		ids[g.ID] = true
		r := costRank[g.Cost]
		if r < prev {
			errs = append(errs, fmt.Sprintf("R-gate-order: %s (%s) comes after a more expensive gate", g.ID, g.Cost))
		}
		prev = max(prev, r)
		manual := g.Cost == "tokens" || g.Cost == "human"
		if manual && len(g.Argv) > 0 {
			errs = append(errs, "R-gate-order: "+g.ID+": tokens/human gates are manual (no argv)")
		}
		if !manual && len(g.Argv) == 0 {
			errs = append(errs, "R-gate-order: "+g.ID+": automatic gate needs argv")
		}
		if (g.Level == "L4" || g.Level == "L5") && !manual {
			errs = append(errs, "R-gate-order: "+g.ID+": L4/L5 gates must be tokens/human cost")
		}
	}
	for _, id := range lp.Readiness.RequireGates {
		if !ids[id] {
			errs = append(errs, "R-gate-order: readiness requires unknown gate "+id)
		}
	}
	return errs
}

type principleDoc struct {
	Principles []struct {
		ID         string `json:"id"`
		Status     string `json:"status"`
		EnforcedBy []struct {
			Type string `json:"type"`
			Ref  string `json:"ref"`
		} `json:"enforcedBy"`
	} `json:"principles"`
}

// checkPrinciples: R-principle-refs — every enforcement reference resolves and the
// declared status matches the references (enforced: no honor; honor: only honor).
func checkPrinciples(root string) []string {
	v := loopkit.NewValidator(root)
	if err := v.ValidateFile("specs/principles.schema.json", "specs/principles.json"); err != nil {
		return []string{"R-principle-refs: " + err.Error()}
	}
	var pd principleDoc
	if err := loopkit.ReadJSON(root, "specs/principles.json", &pd); err != nil {
		return []string{"R-principle-refs: " + err.Error()}
	}
	lp, _ := readLadder(root)
	gates := map[string]bool{}
	for _, g := range lp.GateLadder {
		gates[g.ID] = true
	}
	ps, _ := loadPremises(root)
	premises := map[string]bool{}
	for _, p := range ps {
		premises[p.ID] = true
	}
	var policy map[string]any
	_ = loopkit.ReadJSON(root, "loop/policy.json", &policy)

	var errs []string
	seen := map[string]bool{}
	for _, p := range pd.Principles {
		if seen[p.ID] {
			errs = append(errs, "R-principle-refs: duplicate principle "+p.ID)
		}
		seen[p.ID] = true
		honor, other := 0, 0
		for _, r := range p.EnforcedBy {
			switch r.Type {
			case "honor":
				honor++
				continue
			case "loopctl-rule":
				if _, ok := rules[r.Ref]; !ok {
					errs = append(errs, fmt.Sprintf("R-principle-refs: %s → unknown loopctl rule %s", p.ID, r.Ref))
				}
			case "premise":
				if !premises[r.Ref] {
					errs = append(errs, fmt.Sprintf("R-principle-refs: %s → unknown premise %s", p.ID, r.Ref))
				}
			case "gate":
				if !gates[r.Ref] {
					errs = append(errs, fmt.Sprintf("R-principle-refs: %s → unknown gate %s", p.ID, r.Ref))
				}
			case "policy":
				if !hasPath(policy, r.Ref) {
					errs = append(errs, fmt.Sprintf("R-principle-refs: %s → loop/policy.json has no key %s", p.ID, r.Ref))
				}
			case "generated":
				if r.Ref == "" {
					errs = append(errs, fmt.Sprintf("R-principle-refs: %s → generated ref needs a description", p.ID))
				}
			}
			other++
		}
		switch {
		case p.Status == "enforced" && honor > 0:
			errs = append(errs, fmt.Sprintf("R-principle-refs: %s is 'enforced' but relies on honor", p.ID))
		case p.Status == "honor" && other > 0:
			errs = append(errs, fmt.Sprintf("R-principle-refs: %s is 'honor' but lists mechanisms (use partial)", p.ID))
		case p.Status == "partial" && (honor == 0 || other == 0):
			errs = append(errs, fmt.Sprintf("R-principle-refs: %s is 'partial' but needs both mechanisms and honor", p.ID))
		}
	}
	return errs
}

func hasPath(m map[string]any, dotted string) bool {
	var cur any = m
	for _, k := range strings.Split(dotted, ".") {
		mm, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		if cur, ok = mm[k]; !ok {
			return false
		}
	}
	return true
}

// readiness: R-readiness. Returns unmet conditions for starting an autonomous iteration.
func readiness(s *state, task *loopkit.Task, kind string, now time.Time) []string {
	lp, err := readLadder(s.root)
	if err != nil {
		return []string{err.Error()}
	}
	applies := false
	for _, k := range lp.Readiness.AppliesTo {
		applies = applies || k == kind
	}
	if !applies {
		return nil
	}
	var unmet []string
	gates := map[string]ladderGate{}
	for _, g := range lp.GateLadder {
		gates[g.ID] = g
	}
	for _, id := range lp.Readiness.RequireGates {
		g, ok := gates[id]
		if !ok {
			unmet = append(unmet, "gate "+id+" not defined")
			continue
		}
		for _, req := range g.Requires {
			if _, err := os.Stat(filepath.Join(s.root, filepath.FromSlash(req))); err != nil {
				unmet = append(unmet, fmt.Sprintf("gate %s needs %s (install dependencies)", id, req))
			}
		}
	}
	if task != nil && task.Mode == "compile" {
		ok := false
		for _, a := range task.Acceptance {
			ok = ok || loopkit.LevelRank(a.MaxLevel) <= loopkit.LevelRank(lp.Readiness.CompileAcceptanceMaxLevel)
		}
		if !ok {
			unmet = append(unmet, fmt.Sprintf("task %s has no machine-checkable acceptance (≤%s)", task.ID, lp.Readiness.CompileAcceptanceMaxLevel))
		}
	}
	if task != nil && task.Mode == "human" {
		unmet = append(unmet, fmt.Sprintf("task %s is a human task (not for autonomous loops)", task.ID))
	}
	sc := lp.Readiness.SpecChurn
	since := now.AddDate(0, 0, -sc.WindowDays).Format("2006-01-02")
	args := append([]string{"log", "--since=" + since, "--format=%H", "--"}, globsToPathspecs(sc.Paths)...)
	if out, err := loopkit.Git(s.root, args...); err == nil {
		n := 0
		if out != "" {
			n = len(strings.Split(out, "\n"))
		}
		if n > sc.MaxCommits {
			unmet = append(unmet, fmt.Sprintf("specs changed in %d commits in %d days (max %d): spec not stable yet", n, sc.WindowDays, sc.MaxCommits))
		}
	}
	return unmet
}

func globsToPathspecs(globs []string) []string {
	out := make([]string, len(globs))
	for i, g := range globs {
		out[i] = ":(glob)" + g
	}
	return out
}

// runGates executes the ladder in order (fail-fast). Records evidence when iter != "".
func runGates(ctx context.Context, s *state, iter string, include, only map[string]bool, upto string, costMax string, now func() time.Time) (int, error) {
	lp, err := readLadder(s.root)
	if err != nil {
		return 1, err
	}
	limit := 99
	if costMax != "" {
		limit = costRank[costMax]
	}
	for _, g := range lp.GateLadder {
		if costRank[g.Cost] > limit {
			break
		}
		if len(only) > 0 && !only[g.ID] {
			continue // 표적 검증: 지정한 게이트만(사다리 순서는 유지)
		}
		if len(g.Argv) == 0 {
			fmt.Printf("manual %-11s %s %s — %s\n", g.ID, g.Level, g.Cost, g.What)
			if g.ID == upto {
				break
			}
			continue
		}
		if g.Optional && !include[g.ID] && !only[g.ID] {
			fmt.Printf("skip   %-11s %s %s (optional; --include %s)\n", g.ID, g.Level, g.Cost, g.ID)
			if g.ID == upto {
				break
			}
			continue
		}
		missing := ""
		for _, req := range g.Requires {
			if _, err := os.Stat(filepath.Join(s.root, filepath.FromSlash(req))); err != nil {
				missing = req
			}
		}
		claim := g.ID + ": " + g.What
		var verdict string
		var dur time.Duration
		if missing != "" {
			verdict = "fail"
			fmt.Printf("FAIL   %-11s missing %s\n", g.ID, missing)
		} else if iter != "" {
			e, err := runCommand(ctx, s, iter, g.Kind, g.Level, claim, g.Dir, g.Argv, now)
			if err != nil {
				return 1, err
			}
			verdict, dur = e.Verdict, time.Duration(e.DurationMs)*time.Millisecond
		} else {
			cmd := exec.CommandContext(ctx, g.Argv[0], g.Argv[1:]...)
			cmd.Dir = filepath.Join(s.root, filepath.FromSlash(g.Dir))
			var out bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &out
			t0 := time.Now()
			err := cmd.Run()
			dur = time.Since(t0)
			verdict = "pass"
			if err != nil {
				verdict = "fail"
				fmt.Println(tailLines(out.Bytes(), lp.Feedback.TailLines))
			}
		}
		fmt.Printf("%-6s %-11s %s %-7s %6.1fs  %s\n", map[string]string{"pass": "ok", "fail": "FAIL"}[verdict], g.ID, g.Level, g.Cost, dur.Seconds(), g.What)
		if verdict != "pass" {
			if iter != "" {
				fmt.Println("       → loopctl brief  (압축된 실패 피드백)")
			}
			return 1, nil
		}
		if g.ID == upto {
			break
		}
	}
	return 0, nil
}

func tailLines(b []byte, n int) string {
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// brief prints the compressed failure context for a model (R-brief, principle A6).
func brief(s *state, iter, evidenceID string) (string, error) {
	lp, _ := readLadder(s.root)
	var e *loopkit.Entry
	entries := s.ledger[iter]
	for i := len(entries) - 1; i >= 0; i-- {
		if (evidenceID == "" && entries[i].Verdict == "fail") || entries[i].ID == evidenceID {
			e = &entries[i]
			break
		}
	}
	if e == nil {
		return "", fmt.Errorf("no failing evidence in %s", iter)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "claim: %s\nverdict: %s (%s, %s)\nref: %s\n", e.Claim, e.Verdict, e.Kind, e.Level, e.Ref.Value)
	if it := s.iterations[iter]; it != nil && len(it.FailureSignatures) > 0 {
		fmt.Fprintf(&b, "signatures: %s\n", strings.Join(it.FailureSignatures, ", "))
	}
	if e.Artifact != nil {
		raw, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(e.Artifact.Path)))
		if err == nil {
			fmt.Fprintf(&b, "artifact: %s (%d bytes, sha256 %s…)\n--- last %d lines ---\n%s\n", e.Artifact.Path, e.Artifact.Bytes, e.Artifact.SHA256[:12], lp.Feedback.TailLines, tailLines(raw, lp.Feedback.TailLines))
		} else {
			fmt.Fprintf(&b, "artifact: %s (expired locally; sha256 %s)\n", e.Artifact.Path, e.Artifact.SHA256[:12])
		}
	}
	if len(e.Values) > 0 {
		vb, _ := json.Marshal(e.Values)
		fmt.Fprintf(&b, "values: %s\n", vb)
	}
	return b.String(), nil
}
