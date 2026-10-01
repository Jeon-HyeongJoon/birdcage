package ytdlp

import (
	"regexp"
	"strings"

	"birdcage/helper/internal/engine"
)

type rule struct {
	kind engine.Kind
	re   *regexp.Regexp
}

// rules are checked in order against yt-dlp's stderr (ERROR lines first).
// 문구는 yt-dlp 소스와 실제 이슈 보고에서 수집. 순서가 중요하다(구체적인 것 먼저).
var rules = []rule{
	{engine.KindDRM, regexp.MustCompile(`(?i)\bDRM\b`)},
	{engine.KindPrivate, regexp.MustCompile(`(?i)private video|this video is private|is a private`)},
	{engine.KindAgeRestricted, regexp.MustCompile(`(?i)confirm your age|age[- ]restricted|inappropriate for some users`)},
	// Instagram은 rate limit와 로그인 필요를 같은 문구로 보고한다 → 일시 제한으로 우선 분류(조사 결과 대부분 일시 차단).
	{engine.KindRateLimited, regexp.MustCompile(`(?i)rate[- ]limit|HTTP Error 429|Too Many Requests|confirm you.?re not a bot|try again later`)},
	{engine.KindLoginRequired, regexp.MustCompile(`(?i)login required|sign in to|log in to|use --cookies|cookies-from-browser|members[- ]only|join this channel`)},
	{engine.KindGeoBlocked, regexp.MustCompile(`(?i)available in your country|geo[- ]?restrict|blocked it in your country|not available from your location`)},
	{engine.KindRemoved, regexp.MustCompile(`(?i)video (?:is )?unavailable|has been removed|no longer available|does not exist|HTTP Error 404|account .*terminated|this post is unavailable`)},
	{engine.KindLive, regexp.MustCompile(`(?i)is a live (?:event|stream)|premieres in|live event will begin|this live event`)},
	{engine.KindUnsupportedURL, regexp.MustCompile(`(?i)unsupported url|is not a valid url|no video formats found|no video could be found`)},
	{engine.KindFormat, regexp.MustCompile(`(?i)requested format is not available|requested format not available`)},
	{engine.KindNetwork, regexp.MustCompile(`(?i)timed out|connection (?:reset|refused|aborted)|network is unreachable|name or service not known|nodename nor servname|temporary failure in name resolution|getaddrinfo failed|urlopen error|ssl:|remote end closed connection|incomplete ?read`)},
	{engine.KindExtractorBroken, regexp.MustCompile(`(?i)unable to extract|failed to extract|signature extraction failed|n challenge|nsig|please report this issue|unable to decode|HTTP Error 403`)},
}

// Classify turns yt-dlp stderr lines into a Failure. ERROR lines take precedence.
func Classify(lines []string) *engine.Failure {
	var errLines, other []string
	for _, l := range lines {
		if strings.HasPrefix(l, "ERROR:") {
			errLines = append(errLines, l)
		} else {
			other = append(other, l)
		}
	}
	detail := lastNonEmpty(errLines)
	if detail == "" {
		detail = lastNonEmpty(other)
	}
	for _, group := range [][]string{errLines, other} {
		for _, r := range rules {
			for _, l := range group {
				if r.re.MatchString(l) {
					return engine.Fail(r.kind, detail)
				}
			}
		}
	}
	return engine.Fail(engine.KindExtractorBroken, detail)
}

func lastNonEmpty(ls []string) string {
	for i := len(ls) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(ls[i]); s != "" {
			return s
		}
	}
	return ""
}
