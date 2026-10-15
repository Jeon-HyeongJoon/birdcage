package jobs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReserveDestinationPrefersProjectFolder(t *testing.T) {
	fallback := t.TempDir()
	project := filepath.Join(t.TempDir(), "Project", "Birdcage")
	m := &Manager{deps: Deps{LibraryDir: fallback}}

	dest, release, warning, err := m.reserveDestination(project, "Youtube", "Clip", "abc", "", ".mp4")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if warning != "" {
		t.Fatalf("unexpected warning: %s", warning)
	}
	if got, want := filepath.Dir(dest), filepath.Join(project, "Youtube"); got != want {
		t.Fatalf("destination dir %q, want %q", got, want)
	}
}

func TestReserveDestinationFallsBack(t *testing.T) {
	for _, tc := range []struct {
		name      string
		preferred func(t *testing.T) string
	}{
		{
			name: "relative path",
			preferred: func(_ *testing.T) string {
				return filepath.Join("relative", "Birdcage")
			},
		},
		{
			name: "unwritable path",
			preferred: func(t *testing.T) string {
				blocked := filepath.Join(t.TempDir(), "not-a-directory")
				if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(blocked, "Birdcage")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fallback := t.TempDir()
			m := &Manager{deps: Deps{LibraryDir: fallback}}
			dest, release, warning, err := m.reserveDestination(tc.preferred(t), "Youtube", "Clip", "abc", "", ".mp4")
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			if got, want := filepath.Dir(dest), filepath.Join(fallback, "Youtube"); got != want {
				t.Fatalf("destination dir %q, want fallback %q", got, want)
			}
			if !strings.Contains(warning, fallback) {
				t.Fatalf("warning %q does not identify fallback %q", warning, fallback)
			}
		})
	}
}
