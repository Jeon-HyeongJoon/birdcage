package tools

import (
	"slices"
	"testing"
)

func TestSystemToolDirsCoversMacPackageManagers(t *testing.T) {
	dirs := systemToolDirs("darwin")
	for _, want := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin"} {
		if !slices.Contains(dirs, want) {
			t.Fatalf("systemToolDirs(darwin) = %v, missing %q", dirs, want)
		}
	}
	if got := systemToolDirs("windows"); len(got) != 0 {
		t.Fatalf("systemToolDirs(windows) = %v, want none", got)
	}
}
