// Package library decides where finished media is stored and how it is named.
//
// 경로: <root>/<플랫폼>/<제목> [<id>].mp4
// 파일명은 yt-dlp에 맡기지 않고 직접 만든다: Windows 금지 문자/예약어, 경로 길이, 유니코드 정규화(NFC).
package library

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// maxNameRunes keeps names well under Windows' 260-char MAX_PATH once joined with the root.
const maxNameRunes = 80

var reservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// Sanitize makes s safe as a file or folder name on Windows and macOS.
func Sanitize(s string) string {
	s = norm.NFC.String(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsSpace(r): // 탭·개행도 공백으로(제어문자 제거보다 먼저)
			b.WriteRune(' ')
		case r < 0x20 || r == 0x7f:
			continue
		case strings.ContainsRune(`<>:"/\|?*`, r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if utf8.RuneCountInString(out) > maxNameRunes {
		out = string([]rune(out)[:maxNameRunes])
	}
	// Windows는 끝의 점/공백을 허용하지 않는다.
	out = strings.TrimRight(out, ". ")
	if out == "" {
		out = "untitled"
	}
	if base, _, _ := strings.Cut(out, "."); reservedNames[strings.ToUpper(base)] {
		out = "_" + out
	}
	return out
}

var (
	resMu    sync.Mutex
	reserved = map[string]bool{}
)

// Reserve returns a destination path that neither exists on disk nor is held by
// another in-flight job, and holds it until release is called.
//
// 같은 영상을 동시에 두 번 받으면 두 작업이 같은 경로를 받아 서로 덮어쓰던 문제를 막는다
// (jobs 동시성 테스트에서 발견).
func Reserve(root, extractor, title, id, ext string) (path string, release func(), err error) {
	return ReserveTagged(root, extractor, title, id, "", ext)
}

// ReserveTagged is Reserve with a tag after the id, e.g. "<title> [<id>] 0m05.3s-0m08.1s.mp4".
func ReserveTagged(root, extractor, title, id, tag, ext string) (path string, release func(), err error) {
	dir := filepath.Join(root, Sanitize(orDefault(extractor, "Other")))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, err
	}
	name := Sanitize(title)
	if id != "" {
		name = fmt.Sprintf("%s [%s]", name, Sanitize(id))
	}
	if tag != "" {
		name += " " + Sanitize(tag)
	}
	resMu.Lock()
	defer resMu.Unlock()
	p := filepath.Join(dir, name+ext)
	for i := 2; ; i++ {
		if _, err := os.Stat(p); os.IsNotExist(err) && !reserved[p] {
			reserved[p] = true
			return p, func() {
				resMu.Lock()
				delete(reserved, p)
				resMu.Unlock()
			}, nil
		}
		if i > 999 {
			return "", nil, fmt.Errorf("too many files named %q in %s", name, dir)
		}
		p = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", name, i, ext))
	}
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}
