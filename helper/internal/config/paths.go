// Package config resolves per-OS default locations.
package config

import (
	"os"
	"path/filepath"
	"runtime"
)

// DataDir is where tools, logs and work files live.
//
//	macOS:   ~/Library/Application Support/Birdcage
//	Windows: %LOCALAPPDATA%\Birdcage
//	기타:    $XDG_DATA_HOME/birdcage 또는 ~/.local/share/birdcage
func DataDir() string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Birdcage")
	case "windows":
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "Birdcage")
		}
		return filepath.Join(home, "AppData", "Local", "Birdcage")
	default:
		if d := os.Getenv("XDG_DATA_HOME"); d != "" {
			return filepath.Join(d, "birdcage")
		}
		return filepath.Join(home, ".local", "share", "birdcage")
	}
}

// LibraryDir is where finished, Premiere-ready media is stored.
//
//	macOS: ~/Movies/Birdcage   Windows: %USERPROFILE%\Videos\Birdcage   기타: ~/Videos/Birdcage
func LibraryDir() string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Movies", "Birdcage")
	}
	return filepath.Join(home, "Videos", "Birdcage")
}

// ExeName appends .exe on Windows.
func ExeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
