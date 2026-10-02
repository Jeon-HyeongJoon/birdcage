// Package loopkit holds shared pieces for the loop tools: repo root discovery,
// JSON Schema validation, glob matching and small git helpers.
package loopkit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Root walks up from dir to the directory containing loop/policy.json.
func Root(dir string) (string, error) {
	d, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(d, "loop", "policy.json")); err == nil {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", errors.New("repo root not found (no loop/policy.json above the working directory)")
		}
		d = parent
	}
}

// Validator validates JSON documents against schemas in the repo.
type Validator struct {
	root    string
	c       *jsonschema.Compiler
	schemas map[string]*jsonschema.Schema
}

// NewValidator compiles schemas lazily from repo-relative paths.
func NewValidator(root string) *Validator {
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	return &Validator{root: root, c: c, schemas: map[string]*jsonschema.Schema{}}
}

// Validate checks a JSON value (already decoded with UseNumber semantics) against a schema file.
func (v *Validator) Validate(schemaRel string, doc any) error {
	s, ok := v.schemas[schemaRel]
	if !ok {
		var err error
		s, err = v.c.Compile(filepath.Join(v.root, filepath.FromSlash(schemaRel)))
		if err != nil {
			return fmt.Errorf("compile %s: %w", schemaRel, err)
		}
		v.schemas[schemaRel] = s
	}
	return s.Validate(doc)
}

// ValidateBytes validates one JSON document given as bytes.
func (v *Validator) ValidateBytes(schemaRel string, b []byte) error {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		return err
	}
	return v.Validate(schemaRel, doc)
}

// ValidateFile validates a whole JSON file.
func (v *Validator) ValidateFile(schemaRel, fileRel string) error {
	b, err := os.ReadFile(filepath.Join(v.root, filepath.FromSlash(fileRel)))
	if err != nil {
		return err
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("%s: %w", fileRel, err)
	}
	if err := v.Validate(schemaRel, doc); err != nil {
		return fmt.Errorf("%s: %w", fileRel, err)
	}
	return nil
}

// ValidateLines validates every line of a JSONL file.
func (v *Validator) ValidateLines(schemaRel, fileRel string) error {
	b, err := os.ReadFile(filepath.Join(v.root, filepath.FromSlash(fileRel)))
	if err != nil {
		return err
	}
	for i, line := range bytes.Split(b, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(line))
		if err != nil {
			return fmt.Errorf("%s:%d: %w", fileRel, i+1, err)
		}
		if err := v.Validate(schemaRel, doc); err != nil {
			return fmt.Errorf("%s:%d: %w", fileRel, i+1, err)
		}
	}
	return nil
}

// ReadJSON decodes a repo-relative JSON file strictly into v.
func ReadJSON(root, rel string, v any) error {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// WriteJSON writes v as indented JSON with a trailing newline, atomically.
func WriteJSON(root, rel string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Match reports whether a slash-separated repo path matches a glob that may use
// "**" for any number of directories (e.g. "helper/**/*_test.go", "loop/schema/**").
func Match(glob, p string) bool {
	return matchSegs(strings.Split(glob, "/"), strings.Split(p, "/"))
}

func matchSegs(g, p []string) bool {
	for len(g) > 0 {
		if g[0] == "**" {
			if len(g) == 1 {
				return true
			}
			for i := 0; i <= len(p); i++ {
				if matchSegs(g[1:], p[i:]) {
					return true
				}
			}
			return false
		}
		if len(p) == 0 {
			return false
		}
		ok, err := path.Match(g[0], p[0])
		if err != nil || !ok {
			return false
		}
		g, p = g[1:], p[1:]
	}
	return len(p) == 0
}

// MatchAny reports whether p matches any glob.
func MatchAny(globs []string, p string) bool {
	for _, g := range globs {
		if Match(g, p) {
			return true
		}
	}
	return false
}

// Git runs git in root and returns trimmed stdout.
func Git(root string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}
