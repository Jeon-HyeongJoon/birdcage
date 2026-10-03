// Package subs shifts SRT subtitles onto a downloaded file's timeline.
//
// 구간 다운로드·오디오 자르기는 파일의 0초가 원본의 S초다. 자막(원본 타임라인)을 S만큼 앞으로 옮기고
// 파일 범위를 벗어난 큐는 버리거나 경계에 맞춘다.
package subs

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

// Cue is one subtitle entry; times are in seconds.
type Cue struct {
	Start, End float64
	Text       string
}

// Parse reads SRT (index line optional, CRLF tolerated). Malformed blocks are skipped.
func Parse(src string) []Cue {
	var cues []Cue
	sc := bufio.NewScanner(strings.NewReader(strings.ReplaceAll(strings.TrimPrefix(src, "\ufeff"), "\r\n", "\n")))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	var cur *Cue
	var text []string
	flush := func() {
		if cur != nil {
			cur.Text = strings.TrimSpace(strings.Join(text, "\n"))
			if cur.Text != "" && cur.End > cur.Start {
				cues = append(cues, *cur)
			}
		}
		cur, text = nil, nil
	}
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		if cur == nil {
			if a, b, ok := parseTiming(line); ok {
				cur = &Cue{Start: a, End: b}
			}
			continue // 번호 줄 또는 잘못된 줄
		}
		text = append(text, line)
	}
	flush()
	return cues
}

func parseTiming(line string) (float64, float64, bool) {
	a, b, ok := strings.Cut(line, "-->")
	if !ok {
		return 0, 0, false
	}
	start, ok1 := parseStamp(strings.TrimSpace(a))
	end, ok2 := parseStamp(strings.Fields(strings.TrimSpace(b) + " ")[0])
	return start, end, ok1 && ok2
}

// parseStamp reads "HH:MM:SS,mmm" (also "." and "MM:SS,mmm").
func parseStamp(s string) (float64, bool) {
	s = strings.ReplaceAll(s, ",", ".")
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	var total float64
	for _, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil || v < 0 {
			return 0, false
		}
		total = total*60 + v
	}
	return total, true
}

// Shift moves cues by -offset and keeps those overlapping [0, length) (length <= 0 = no limit),
// clamping the edges to the file.
func Shift(cues []Cue, offset, length float64) []Cue {
	var out []Cue
	for _, c := range cues {
		c.Start -= offset
		c.End -= offset
		if c.End <= 0 || (length > 0 && c.Start >= length) {
			continue
		}
		c.Start = max(0, c.Start)
		if length > 0 {
			c.End = min(c.End, length)
		}
		out = append(out, c)
	}
	return out
}

// Format writes SRT with renumbered cues.
func Format(cues []Cue) string {
	var b strings.Builder
	for i, c := range cues {
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", i+1, stamp(c.Start), stamp(c.End), c.Text)
	}
	return b.String()
}

func stamp(sec float64) string {
	ms := int64(sec*1000 + 0.5)
	h := ms / 3600000
	m := ms / 60000 % 60
	s := ms / 1000 % 60
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, ms%1000)
}
