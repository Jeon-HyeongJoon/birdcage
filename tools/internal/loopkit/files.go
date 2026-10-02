package loopkit

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// skipDirs are never walked: VCS, dependencies, build output and local-only artifacts.
var skipDirs = map[string]bool{".git": true, "node_modules": true, "dist": true, ".perf": true, ".runs": true}

// skipPaths are repo-relative directories excluded from walks (binary build outputs).
var skipPaths = map[string]bool{"panel/cep/src/bin": true}

// Files lists repo files (slash-separated, relative) matching any glob, including
// untracked files that would be committed. Results are sorted.
func Files(root string, globs ...string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && (skipDirs[d.Name()] || skipPaths[rel]) {
				return filepath.SkipDir
			}
			return nil
		}
		if len(globs) == 0 || MatchAny(globs, rel) {
			out = append(out, rel)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

// TrackedFiles lists files tracked by git (slash-separated).
func TrackedFiles(root string) ([]string, error) {
	out, err := Git(root, "ls-files")
	if err != nil || out == "" {
		return nil, err
	}
	return strings.Split(out, "\n"), nil
}
