package ytdlp

import (
	"strconv"
	"strings"

	"birdcage/helper/internal/engine"
)

// progressPrefix marks our --progress-template lines on stdout.
const progressPrefix = "[birdcage] "

// progressTemplate prints a compact, '|'-separated record per progress tick.
// %(progress)j 는 info_dict 전체를 매 틱마다 직렬화해 무거우므로 필요한 필드만 뽑는다.
// 값이 없으면 yt-dlp가 "NA"를 출력한다.
const progressTemplate = "download:" + progressPrefix +
	"%(progress.status)s|%(progress.downloaded_bytes)s|%(progress.total_bytes)s|%(progress.total_bytes_estimate)s|" +
	"%(progress.speed)s|%(progress.eta)s|%(progress.fragment_index)s|%(progress.fragment_count)s|%(info.format_id)s"

// ParseProgress parses one of our progress lines. ok is false for other output.
func ParseProgress(line string) (engine.Progress, bool) {
	rest, found := strings.CutPrefix(line, progressPrefix)
	if !found {
		return engine.Progress{}, false
	}
	var f [9]string
	n := 0
	for n < len(f)-1 {
		i := strings.IndexByte(rest, '|')
		if i < 0 {
			break
		}
		f[n] = rest[:i]
		rest = rest[i+1:]
		n++
	}
	f[n] = rest
	if n != len(f)-1 {
		return engine.Progress{}, false
	}
	p := engine.Progress{
		Status:          f[0],
		DownloadedBytes: parseInt(f[1]),
		TotalBytes:      parseInt(f[2]),
		SpeedBps:        parseFloat(f[4]),
		ETASec:          parseFloat(f[5]),
		FragIndex:       int(parseInt(f[6])),
		FragCount:       int(parseInt(f[7])),
		FormatID:        na(f[8]),
		Fraction:        -1,
	}
	if p.TotalBytes == 0 {
		if est := parseInt(f[3]); est > 0 {
			p.TotalBytes, p.TotalEstimated = est, true
		}
	}
	switch {
	case p.TotalBytes > 0:
		p.Fraction = clamp01(float64(p.DownloadedBytes) / float64(p.TotalBytes))
	case p.FragCount > 0:
		p.Fraction = clamp01(float64(p.FragIndex) / float64(p.FragCount))
	}
	if p.Status == "finished" {
		p.Fraction = 1
	}
	return p, true
}

func na(s string) string {
	if s == "NA" || s == "None" {
		return ""
	}
	return s
}

func parseInt(s string) int64 {
	s = na(s)
	if s == "" {
		return 0
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	// yt-dlp는 일부 값을 float로 출력한다(예: "1234.0").
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return int64(f)
	}
	return 0
}

func parseFloat(s string) float64 {
	s = na(s)
	if s == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}
