// Command loopctl operates the evidence-driven development loop defined in loop/policy.json.
//
//	loopctl check                         # 스키마 + 정책 규칙 검사(L2 게이트)
//	loopctl orient                        # 새 세션 시작 루틴: 상태 요약
//	loopctl begin  --task T-001 --kind worker --hypothesis "..."
//	loopctl run    --kind test --level L1 --claim "..." [--dir helper] -- go test ./...
//	loopctl evidence --kind web --level L3 --claim "..." --verdict pass --ref-type url --ref URL --source primary --observed-at 2026-10-02
//	loopctl premises [--record]           # ADR 전제 검사(실행 가능한 ADR)
//	loopctl guard                         # 보호 경로(검증기·명세) 변경 감지
//	loopctl approve --action verifier.change --by <사람> [--note ...]
//	loopctl end    --state success --summary "..." [--pass-task] [--lesson ...] [--retro-none]
//	loopctl gates  [--cost free] [--upto G-x] [--include G-e2e] [--only G-a --only G-b]  # 게이트 사다리(싼 것부터, 실패 시 중단)
//	loopctl brief  [--evidence E-…]       # 모델에 넘길 압축 실패 피드백
//	loopctl readiness --task T-001 --kind worker   # 자율 루프 준비 조건
//	loopctl rules                         # 원칙이 참조하는 강제 규칙 목록
//	loopctl gc     [--dry-run]            # 보존 기간이 지난 loop/.runs 정리
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"birdcage/tools/internal/loopkit"
)

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	root, err := loopkit.Root(".")
	if err != nil {
		die(err)
	}
	cmd, args := os.Args[1], os.Args[2:]
	now := time.Now
	switch cmd {
	case "check":
		os.Exit(cmdCheck(root, now()))
	case "orient":
		cmdOrient(root, now())
	case "begin":
		fs := flag.NewFlagSet(cmd, flag.ExitOnError)
		task := fs.String("task", "", "task id (T-NNN)")
		kind := fs.String("kind", "worker", "session kind")
		hyp := fs.String("hypothesis", "", "what this iteration expects to change and how it will be verified")
		sid := fs.String("session-id", os.Getenv("CLAUDE_SESSION_ID"), "agent session id")
		model := fs.String("model", "", "model id")
		_ = fs.Parse(args)
		if *task == "" || *hyp == "" {
			die(fmt.Errorf("--task and --hypothesis are required"))
		}
		s := mustLoad(root)
		id, err := begin(s, *task, *kind, *hyp, *sid, *model, now())
		if err != nil {
			die(err)
		}
		fmt.Println(id)
	case "run":
		fs := flag.NewFlagSet(cmd, flag.ExitOnError)
		iter := fs.String("iter", "", "iteration id (default: loop/.runs/CURRENT)")
		kind := fs.String("kind", "test", "evidence kind")
		level := fs.String("level", "L1", "evidence level")
		claim := fs.String("claim", "", "what passing this command proves")
		dir := fs.String("dir", "", "working directory relative to the repo root")
		_ = fs.Parse(args)
		argv := fs.Args()
		if len(argv) == 0 || *claim == "" {
			die(fmt.Errorf("usage: loopctl run --claim ... [--dir d] -- cmd args"))
		}
		s := mustLoad(root)
		id, err := currentIter(root, *iter)
		if err != nil {
			die(err)
		}
		e, err := runCommand(context.Background(), s, id, *kind, *level, *claim, *dir, argv, now)
		if err != nil {
			die(err)
		}
		fmt.Printf("%s %s %s (%.0fms) %s\n", e.ID, strings.ToUpper(e.Verdict), e.Level, e.DurationMs, e.Claim)
		if e.Verdict != "pass" {
			os.Exit(1)
		}
	case "evidence":
		fs := flag.NewFlagSet(cmd, flag.ExitOnError)
		iter := fs.String("iter", "", "iteration id")
		var e loopkit.Entry
		var values, cites multi
		fs.StringVar(&e.Kind, "kind", "", "evidence kind")
		fs.StringVar(&e.Level, "level", "", "L1..L5")
		fs.StringVar(&e.Claim, "claim", "", "claim")
		fs.StringVar(&e.Verdict, "verdict", "info", "pass|fail|skip|info")
		fs.StringVar(&e.Ref.Type, "ref-type", "file", "command|file|url|run|commit|premise|session")
		fs.StringVar(&e.Ref.Value, "ref", "", "reference")
		fs.StringVar(&e.Source, "source", "", "primary|secondary (web)")
		fs.StringVar(&e.ObservedAt, "observed-at", "", "when the source was read (web)")
		fs.IntVar(&e.Reproduced, "reproduced", 0, "times reproduced (perf)")
		fs.Var(&values, "value", "key=value (repeatable)")
		fs.Var(&cites, "cites", "evidence id (repeatable)")
		_ = fs.Parse(args)
		s := mustLoad(root)
		id, err := currentIter(root, *iter)
		if err != nil {
			die(err)
		}
		e.Iteration, e.Cites = id, cites
		if len(values) > 0 {
			e.Values = map[string]any{}
			for _, kv := range values {
				k, v, _ := strings.Cut(kv, "=")
				e.Values[k] = v
			}
		}
		e, err = appendEvidence(s, e, nil, "", now())
		if err != nil {
			die(err)
		}
		fmt.Println(e.ID)
	case "premises":
		fs := flag.NewFlagSet(cmd, flag.ExitOnError)
		record := fs.Bool("record", false, "record results as evidence in the current iteration")
		iter := fs.String("iter", "", "iteration id")
		_ = fs.Parse(args)
		os.Exit(cmdPremises(root, *record, *iter, now))
	case "guard":
		fs := flag.NewFlagSet(cmd, flag.ExitOnError)
		iter := fs.String("iter", "", "iteration id")
		base := fs.String("base", "", "base revision (default: iteration base commit)")
		_ = fs.Parse(args)
		s := mustLoad(root)
		b := *base
		var it *loopkit.Iteration
		if b == "" {
			id, err := currentIter(root, *iter)
			if err != nil {
				die(err)
			}
			it = s.iterations[id]
			if it == nil {
				die(fmt.Errorf("unknown iteration %s", id))
			}
			b = it.Base.Commit
		}
		v, err := guardViolations(s, b)
		if err != nil {
			die(err)
		}
		if len(v) == 0 {
			fmt.Println("guard: no protected files modified")
			return
		}
		ok := it != nil && approved(it, s.policy.Protected.Gate)
		for _, f := range v {
			fmt.Printf("protected: %s\n", f)
		}
		if !ok {
			fmt.Printf("guard: %d protected file(s) changed without %s approval\n", len(v), s.policy.Protected.Gate)
			os.Exit(1)
		}
		fmt.Println("guard: approved by", s.policy.Protected.Gate)
	case "approve":
		fs := flag.NewFlagSet(cmd, flag.ExitOnError)
		iter := fs.String("iter", "", "iteration id")
		action := fs.String("action", "", "human gate action")
		by := fs.String("by", "", "who approved (a person)")
		note := fs.String("note", "", "note")
		_ = fs.Parse(args)
		s := mustLoad(root)
		id, err := currentIter(root, *iter)
		if err != nil {
			die(err)
		}
		it := s.iterations[id]
		if it == nil || *action == "" || *by == "" {
			die(fmt.Errorf("need a running iteration, --action and --by"))
		}
		it.Approvals = append(it.Approvals, loopkit.Approval{Action: *action, By: *by, At: now().UTC().Format(time.RFC3339), Note: *note})
		if err := saveIteration(s, it); err != nil {
			die(err)
		}
		fmt.Println("approved", *action, "by", *by)
	case "end":
		fs := flag.NewFlagSet(cmd, flag.ExitOnError)
		iter := fs.String("iter", "", "iteration id")
		var o endOpts
		var next, checks, specs, lessons multi
		fs.StringVar(&o.state, "state", "", "success|no-op|blocked|stalled|exhausted")
		fs.StringVar(&o.summary, "summary", "", "what happened")
		fs.Var(&next, "next", "next action for the following session (repeatable)")
		fs.Var(&checks, "check", "retro: new check added (repeatable)")
		fs.Var(&specs, "spec", "retro: spec change (repeatable)")
		fs.Var(&lessons, "lesson", "retro: lesson (repeatable)")
		fs.BoolVar(&o.retroNone, "retro-none", false, "retro: nothing to record (explicit)")
		fs.BoolVar(&o.passTask, "pass-task", false, "mark the task done if its acceptance is met by this iteration's evidence")
		fs.IntVar(&o.tokens, "tokens", 0, "tokens used by the session for this iteration (cost evidence)")
		fs.Float64Var(&o.usd, "usd", 0, "cost in USD for this iteration (cost evidence)")
		_ = fs.Parse(args)
		o.next, o.checks, o.specs, o.less = next, checks, specs, lessons
		s := mustLoad(root)
		id, err := currentIter(root, *iter)
		if err != nil {
			die(err)
		}
		if err := end(s, id, o, now()); err != nil {
			die(err)
		}
		fmt.Println("ended", id, o.state)
	case "rules":
		ids := make([]string, 0, len(rules))
		for id := range rules {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			fmt.Printf("%-20s %s\n", id, rules[id])
		}
	case "readiness":
		fs := flag.NewFlagSet(cmd, flag.ExitOnError)
		task := fs.String("task", "", "task id")
		kind := fs.String("kind", "worker", "session kind")
		_ = fs.Parse(args)
		s := mustLoad(root)
		var t *loopkit.Task
		for i := range s.backlog.Tasks {
			if s.backlog.Tasks[i].ID == *task {
				t = &s.backlog.Tasks[i]
			}
		}
		unmet := readiness(s, t, *kind, now())
		if len(unmet) == 0 {
			fmt.Printf("ready: %s loop may start (%s)\n", *kind, *task)
			return
		}
		fmt.Printf("not ready for a %s loop:\n  - %s\n", *kind, strings.Join(unmet, "\n  - "))
		os.Exit(1)
	case "gates":
		fs := flag.NewFlagSet(cmd, flag.ExitOnError)
		iter := fs.String("iter", "", "record into this iteration (default: current, if any)")
		cost := fs.String("cost", "", "run gates up to this cost class: free|slow|network")
		upto := fs.String("upto", "", "stop after this gate id")
		var inc, onl multi
		fs.Var(&inc, "include", "optional gate id to include (repeatable)")
		fs.Var(&onl, "only", "run only these gate ids, in ladder order (repeatable; target verification)")
		noRecord := fs.Bool("no-record", false, "do not record evidence")
		_ = fs.Parse(args)
		s := mustLoad(root)
		id := ""
		if !*noRecord {
			id, _ = currentIter(root, *iter)
			if id != "" && (s.iterations[id] == nil || s.iterations[id].State != "running") {
				id = ""
			}
		}
		include := map[string]bool{}
		for _, g := range inc {
			include[g] = true
		}
		only := map[string]bool{}
		for _, g := range onl {
			only[g] = true
		}
		code, err := runGates(context.Background(), s, id, include, only, *upto, *cost, now)
		if err != nil {
			die(err)
		}
		if id == "" && !*noRecord {
			fmt.Println("(진행 중인 반복이 없어 증적은 기록하지 않았다)")
		}
		os.Exit(code)
	case "brief":
		fs := flag.NewFlagSet(cmd, flag.ExitOnError)
		iter := fs.String("iter", "", "iteration id")
		ev := fs.String("evidence", "", "evidence id (default: latest failure)")
		_ = fs.Parse(args)
		s := mustLoad(root)
		id, err := currentIter(root, *iter)
		if err != nil {
			die(err)
		}
		out, err := brief(s, id, *ev)
		if err != nil {
			die(err)
		}
		fmt.Print(out)
	case "gc":
		fs := flag.NewFlagSet(cmd, flag.ExitOnError)
		dry := fs.Bool("dry-run", false, "only list")
		_ = fs.Parse(args)
		cmdGC(root, *dry, now())
	default:
		usage()
	}
}

func mustLoad(root string) *state {
	s, errs := load(root)
	if len(errs) > 0 {
		die(fmt.Errorf("loop state invalid:\n  %s", strings.Join(errs, "\n  ")))
	}
	return s
}

func cmdCheck(root string, now time.Time) int {
	s, errs := load(root)
	e2, warns := checkRules(s, now)
	errs = append(errs, e2...)
	for _, w := range warns {
		fmt.Println("warn ", w)
	}
	for _, e := range errs {
		fmt.Println("ERROR", e)
	}
	fmt.Printf("loopctl check: %d iterations, %d evidence, %d tasks — %d error(s), %d warning(s)\n",
		len(s.iterations), len(s.entries), len(s.backlog.Tasks), len(errs), len(warns))
	if len(errs) > 0 {
		return 1
	}
	return 0
}

func cmdPremises(root string, record bool, iter string, now func() time.Time) int {
	ps, err := loadPremises(root)
	if err != nil {
		die(err)
	}
	var s *state
	var id string
	if record {
		s = mustLoad(root)
		if id, err = currentIter(root, iter); err != nil {
			die(err)
		}
	}
	failed := 0
	for _, p := range ps {
		r := runPremise(context.Background(), root, p, now())
		mark := map[string]string{"pass": "ok  ", "fail": "FAIL", "skip": "skip"}[r.Verdict]
		fmt.Printf("%s %s ADR %s %-4s %6.0fms  %s", mark, p.ID, p.ADR, p.Level, float64(r.Duration.Milliseconds()), p.Claim)
		if r.Detail != "" {
			fmt.Printf("\n       → %s", r.Detail)
		}
		fmt.Println()
		if r.Verdict == "fail" {
			failed++
		}
		if record {
			e := loopkit.Entry{Iteration: id, Kind: "premise", Level: p.Level, Claim: p.ID + ": " + p.Claim, Verdict: r.Verdict,
				Ref: loopkit.Ref{Type: "premise", Value: "docs/decisions/premises.json#" + p.ID}, Values: r.Values, DurationMs: float64(r.Duration.Milliseconds())}
			if r.Detail != "" {
				e.Values["detail"] = r.Detail
			}
			var art []byte
			if len(r.Output) > 0 {
				art = r.Output
			}
			if _, err := appendEvidence(s, e, art, p.ID+".log", now()); err != nil {
				die(err)
			}
		}
	}
	fmt.Printf("premises: %d checked, %d failed\n", len(ps), failed)
	if failed > 0 {
		return 1
	}
	return 0
}

func cmdOrient(root string, now time.Time) {
	s, errs := load(root)
	ref := gitRef(root)
	fmt.Printf("== Birdcage loop — %s\n", now.Format("2006-01-02 15:04"))
	fmt.Printf("git: %s @ %s dirty=%v\n", ref.Branch, ref.Commit, ref.Dirty)
	if len(errs) > 0 {
		fmt.Printf("!! loop state invalid (%d) — run `make verify`\n", len(errs))
	}
	fmt.Printf("rules: one task per session=%v, fresh context for %v, budget %d iterations/task, stall after %d same signatures\n",
		s.policy.Sessions.OneTaskPerSession, s.policy.Sessions.FreshContextFor, s.policy.Budgets.MaxIterationsPerTask, s.policy.Budgets.StallAfterSameSignature)

	its := make([]*loopkit.Iteration, 0, len(s.iterations))
	for _, it := range s.iterations {
		its = append(its, it)
	}
	sort.Slice(its, func(i, j int) bool { return its[i].StartedAt > its[j].StartedAt })
	fmt.Println("\n-- running")
	for _, it := range its {
		if it.State == "running" {
			fmt.Printf("  %s (%s) %s\n", it.ID, it.Session.Kind, it.Hypothesis)
		}
	}
	fmt.Println("\n-- recent iterations (handoff)")
	shown := 0
	for _, it := range its {
		if it.State == "running" || shown >= 3 {
			continue
		}
		shown++
		fmt.Printf("  %s %-9s %s\n", it.ID, it.State, it.Summary)
		for _, n := range it.Next {
			fmt.Printf("      next: %s\n", n)
		}
	}
	fmt.Println("\n-- open tasks (ready first)")
	done := map[string]bool{}
	for _, t := range s.backlog.Tasks {
		done[t.ID] = t.Passes
	}
	tasks := append([]loopkit.Task(nil), s.backlog.Tasks...)
	sort.SliceStable(tasks, func(i, j int) bool { return prio(tasks[i]) < prio(tasks[j]) })
	for _, t := range tasks {
		if t.Passes {
			continue
		}
		ready := true
		for _, d := range t.DependsOn {
			ready = ready && done[d]
		}
		tag := "ready"
		switch {
		case t.Status == "blocked":
			tag = "blocked"
		case !ready:
			tag = "waits " + strings.Join(t.DependsOn, ",")
		case t.Mode == "human":
			tag = "human"
		}
		fmt.Printf("  %s [%s] p%d %-8s %s\n", t.ID, t.Mode, prio(t), tag, t.Title)
	}
	cmdGC(root, true, now)
}

func prio(t loopkit.Task) int {
	if t.Priority == 0 {
		return 3
	}
	return t.Priority
}

func cmdGC(root string, dry bool, now time.Time) {
	var p loopkit.Policy
	_ = loopkit.ReadJSON(root, "loop/policy.json", &p)
	dirs, _ := filepath.Glob(filepath.Join(root, "loop", ".runs", "*-T-*"))
	var total int64
	var expired []string
	for _, d := range dirs {
		st, err := os.Stat(d)
		if err != nil {
			continue
		}
		_ = filepath.Walk(d, func(_ string, fi os.FileInfo, _ error) error {
			if fi != nil && !fi.IsDir() {
				total += fi.Size()
			}
			return nil
		})
		if now.Sub(st.ModTime()) > time.Duration(p.Storage.LocalRetentionDays)*24*time.Hour {
			expired = append(expired, d)
		}
	}
	fmt.Printf("\n-- local artifacts: %d iteration dirs, %.1f MiB, %d past %d-day retention\n", len(dirs), float64(total)/(1<<20), len(expired), p.Storage.LocalRetentionDays)
	if dry {
		return
	}
	for _, d := range expired {
		if err := os.RemoveAll(d); err == nil {
			fmt.Println("removed", d)
		}
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: loopctl check|orient|begin|run|gates|brief|evidence|premises|readiness|guard|approve|end|rules|gc")
	os.Exit(2)
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "loopctl:", err)
	os.Exit(1)
}
