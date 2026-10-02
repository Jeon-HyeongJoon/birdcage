package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"birdcage/tools/internal/loopkit"
)

type premiseCheck struct {
	Type        string   `json:"type"`
	Path        string   `json:"path,omitempty"`
	Pattern     string   `json:"pattern,omitempty"`
	Paths       []string `json:"paths,omitempty"`
	Glob        string   `json:"glob,omitempty"`
	Argv        []string `json:"argv,omitempty"`
	Dir         string   `json:"dir,omitempty"`
	TimeoutSec  int      `json:"timeoutSec,omitempty"`
	MaxMs       int      `json:"maxMs,omitempty"`
	RequiresEnv []string `json:"requiresEnv,omitempty"`
	VerifiedAt  string   `json:"verifiedAt,omitempty"`
	TTLDays     int      `json:"ttlDays,omitempty"`
}

type premise struct {
	ID      string       `json:"id"`
	ADR     string       `json:"adr"`
	Claim   string       `json:"claim"`
	Level   string       `json:"level"`
	Sources []string     `json:"sources,omitempty"`
	Check   premiseCheck `json:"check"`
}

type premiseResult struct {
	P        premise
	Verdict  string // pass | fail | skip
	Detail   string
	Duration time.Duration
	Output   []byte // command output (for artifacts)
	Values   map[string]any
}

func loadPremises(root string) ([]premise, error) {
	v := loopkit.NewValidator(root)
	if err := v.ValidateFile("docs/decisions/premises.schema.json", "docs/decisions/premises.json"); err != nil {
		return nil, err
	}
	var doc struct {
		Premises []premise `json:"premises"`
	}
	err := loopkit.ReadJSON(root, "docs/decisions/premises.json", &doc)
	return doc.Premises, err
}

func runPremise(ctx context.Context, root string, p premise, now time.Time) (r premiseResult) {
	r = premiseResult{P: p, Values: map[string]any{}}
	t0 := time.Now()
	defer func() { r.Duration = time.Since(t0) }()
	c := p.Check
	switch c.Type {
	case "file-regex":
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(c.Path)))
		if err != nil {
			r.Verdict, r.Detail = "fail", err.Error()
			return r
		}
		re, err := regexp.Compile(c.Pattern)
		if err != nil {
			r.Verdict, r.Detail = "fail", "bad pattern: "+err.Error()
			return r
		}
		if re.Match(b) {
			r.Verdict = "pass"
		} else {
			r.Verdict, r.Detail = "fail", fmt.Sprintf("%s does not match %s", c.Path, c.Pattern)
		}
	case "file-ascii":
		files, err := loopkit.Files(root, c.Paths...)
		if err != nil || len(files) == 0 {
			r.Verdict, r.Detail = "fail", fmt.Sprintf("no files matched %v %v", c.Paths, err)
			return r
		}
		var bad []string
		for _, f := range files {
			b, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
			for i, line := range bytes.Split(b, []byte("\n")) {
				if hasNonASCII(line) {
					bad = append(bad, fmt.Sprintf("%s:%d", f, i+1))
					break
				}
			}
		}
		r.Values["files"] = len(files)
		if len(bad) == 0 {
			r.Verdict = "pass"
		} else {
			r.Verdict, r.Detail = "fail", "non-ASCII in "+strings.Join(bad, ", ")
		}
	case "per-dir-regex":
		files, err := loopkit.Files(root, c.Glob)
		if err != nil || len(files) == 0 {
			r.Verdict, r.Detail = "fail", fmt.Sprintf("no files matched %s %v", c.Glob, err)
			return r
		}
		re, err := regexp.Compile(c.Pattern)
		if err != nil {
			r.Verdict, r.Detail = "fail", "bad pattern: "+err.Error()
			return r
		}
		ok := map[string]bool{}
		for _, f := range files {
			dir := path.Dir(f)
			if _, seen := ok[dir]; !seen {
				ok[dir] = false
			}
			if b, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(f))); re.Match(b) {
				ok[dir] = true
			}
		}
		var missing []string
		for dir, good := range ok {
			if !good {
				missing = append(missing, dir)
			}
		}
		r.Values["dirs"] = len(ok)
		if len(missing) == 0 {
			r.Verdict = "pass"
		} else {
			r.Verdict, r.Detail = "fail", "missing in "+strings.Join(missing, ", ")
		}
	case "command":
		for _, env := range c.RequiresEnv {
			if os.Getenv(env) == "" {
				r.Verdict, r.Detail = "skip", "env "+env+" not set"
				return r
			}
		}
		argv := make([]string, len(c.Argv))
		for i, a := range c.Argv {
			argv[i] = os.ExpandEnv(a)
		}
		timeout := time.Duration(max(c.TimeoutSec, 60)) * time.Second
		cctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
		cmd.Dir = filepath.Join(root, filepath.FromSlash(c.Dir))
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		start := time.Now()
		err := cmd.Run()
		elapsed := time.Since(start)
		r.Output = out.Bytes()
		r.Values["ms"] = elapsed.Milliseconds()
		switch {
		case err != nil:
			r.Verdict, r.Detail = "fail", fmt.Sprintf("%v: %s", err, lastLine(out.Bytes()))
		case c.MaxMs > 0 && elapsed.Milliseconds() > int64(c.MaxMs):
			r.Verdict, r.Detail = "fail", fmt.Sprintf("took %dms > %dms", elapsed.Milliseconds(), c.MaxMs)
		default:
			r.Verdict = "pass"
			if bytes.Contains(out.Bytes(), []byte("[no tests to run]")) {
				r.Verdict, r.Detail = "fail", "no tests matched (premise points at a missing test)"
			}
		}
	case "expires":
		at, err := time.Parse("2006-01-02", c.VerifiedAt)
		if err != nil {
			r.Verdict, r.Detail = "fail", err.Error()
			return r
		}
		due := at.AddDate(0, 0, c.TTLDays)
		left := int(due.Sub(now).Hours() / 24)
		r.Values["daysLeft"] = left
		if now.After(due) {
			r.Verdict, r.Detail = "fail", fmt.Sprintf("stale since %s: re-verify sources (Discovery)", due.Format("2006-01-02"))
		} else {
			r.Verdict = "pass"
		}
	default:
		r.Verdict, r.Detail = "fail", "unknown check type "+c.Type
	}
	return r
}

func hasNonASCII(b []byte) bool {
	for _, c := range b {
		if c >= 0x80 {
			return true
		}
	}
	return false
}

func lastLine(b []byte) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
