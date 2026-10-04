package related

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// ErrFormat means the page no longer has the structure the parser knows (플랫폼 형식 변경).
var ErrFormat = errors.New("related: unknown page format")

// 시청 페이지가 초기 데이터를 넣는 방식(현재는 첫 번째, 나머지는 과거·대체 형식).
var initialDataMarkers = [][]byte{[]byte("var ytInitialData = "), []byte(`window["ytInitialData"] = `), []byte("ytInitialData = ")}

// ParseWatchPage reads the "up next" list from a YouTube watch page's ytInitialData.
//
// 공식 API가 없다(YouTube Data API relatedToVideoId는 2023-08-07 폐지). 항목 형식은 2025년에
// compactVideoRenderer → lockupViewModel로 바뀌었고(NewPipeExtractor PR #1320) 둘 다 읽는다.
// 모르는 형식이면 ErrFormat을 돌려 호출자가 다른 출처로 넘어가게 한다.
func ParseWatchPage(page []byte, seedID string) ([]Item, error) {
	root, err := initialData(page)
	if err != nil {
		return nil, err
	}
	sec := dig(root, "contents", "twoColumnWatchNextResults", "secondaryResults")
	if sec == nil {
		return nil, fmt.Errorf("%w: no secondaryResults", ErrFormat)
	}
	return clean(collect(sec), seedID), nil
}

// initialData decodes the page's ytInitialData object.
func initialData(page []byte) (map[string]any, error) {
	for _, m := range initialDataMarkers {
		if i := bytes.Index(page, m); i >= 0 {
			var root map[string]any
			// 디코더는 첫 JSON 값에서 멈추므로 뒤따르는 ";</script>…"는 읽지 않는다.
			if err := json.NewDecoder(bytes.NewReader(page[i+len(m):])).Decode(&root); err != nil {
				return nil, fmt.Errorf("%w: ytInitialData: %v", ErrFormat, err)
			}
			return root, nil
		}
	}
	return nil, fmt.Errorf("%w: no ytInitialData", ErrFormat)
}

// collect gathers video items in document order: lockupViewModel(2025~), compactVideoRenderer(시청 페이지 과거 형식),
// videoRenderer(검색 결과). 쇼츠(shortsLockupViewModel)·재생목록은 모으지 않는다.
func collect(v any) []Item {
	var items []Item
	walk(v, func(key string, obj map[string]any) bool {
		switch key {
		case "lockupViewModel":
			if it, ok := parseLockup(obj); ok {
				items = append(items, it)
			}
			return false
		case "compactVideoRenderer", "videoRenderer":
			if it, ok := parseCompact(obj); ok {
				items = append(items, it)
			}
			return false
		}
		return true
	})
	return items
}

// parseLockup reads the current (2025~) item shape.
func parseLockup(v map[string]any) (Item, bool) {
	if str(v, "contentType") != "LOCKUP_CONTENT_TYPE_VIDEO" { // 재생목록·믹스
		return Item{}, false
	}
	md, _ := dig(v, "metadata", "lockupMetadataViewModel").(map[string]any)
	it := Item{ID: str(v, "contentId"), Title: str(dig(md, "title"), "content")}
	rows, _ := dig(md, "metadata", "contentMetadataViewModel", "metadataRows").([]any)
	for i, r := range rows {
		parts, _ := dig(r, "metadataParts").([]any)
		var texts []string
		for _, p := range parts {
			// 접근성 문구가 더 완전하다("7.8만" → "조회수 7.8만회").
			t := str(p, "accessibilityLabel")
			if t == "" {
				t = str(dig(p, "text"), "content")
			}
			if t != "" {
				texts = append(texts, t)
			}
		}
		if i == 0 && len(texts) > 0 {
			it.Channel = texts[0]
		} else if len(texts) > 0 {
			it.Meta = strings.Join(append(splitMeta(it.Meta), texts...), " · ")
		}
	}
	img := dig(v, "contentImage", "thumbnailViewModel")
	it.Thumbnail = pickSource(dig(img, "image", "sources"))
	live := false
	walk(img, func(key string, b map[string]any) bool {
		if key != "thumbnailBadgeViewModel" {
			return true
		}
		if strings.Contains(str(b, "badgeStyle"), "LIVE") {
			live = true
		}
		if d, ok := parseDuration(str(b, "text")); ok && it.DurationSec == 0 {
			it.DurationSec = d
		}
		return false
	})
	return it, !live
}

// parseCompact reads compactVideoRenderer (시청 페이지 과거 형식) and videoRenderer (검색 결과): 필드가 같다.
func parseCompact(v map[string]any) (Item, bool) {
	it := Item{ID: str(v, "videoId"), Title: text(dig(v, "title"))}
	for _, k := range []string{"longBylineText", "ownerText", "shortBylineText"} {
		if it.Channel = text(dig(v, k)); it.Channel != "" {
			break
		}
	}
	if dig(v, "upcomingEventData") != nil {
		return it, false // 예정된 생방송·프리미어는 아직 받을 수 없다
	}
	it.DurationSec, _ = parseDuration(text(dig(v, "lengthText")))
	it.Meta = strings.Join(nonEmpty(text(dig(v, "viewCountText")), text(dig(v, "publishedTimeText"))), " · ")
	it.Thumbnail = pickSource(dig(v, "thumbnail", "thumbnails"))
	badges, _ := dig(v, "badges").([]any)
	for _, b := range badges {
		if strings.Contains(str(dig(b, "metadataBadgeRenderer"), "style"), "LIVE") {
			return it, false
		}
	}
	return it, true
}

// clean drops the seed, duplicates and incomplete items and fills missing thumbnails.
func clean(items []Item, seedID string) []Item {
	seen := map[string]bool{seedID: true}
	out := items[:0]
	for _, it := range items {
		if it.ID == "" || it.Title == "" || seen[it.ID] {
			continue
		}
		seen[it.ID] = true
		it.URL = "https://www.youtube.com/watch?v=" + it.ID
		if it.Thumbnail == "" {
			it.Thumbnail = "https://i.ytimg.com/vi/" + it.ID + "/mqdefault.jpg"
		}
		out = append(out, it)
	}
	return out
}

// pickSource returns the smallest image at least 320px wide (2× the 128px row thumbnail), else the largest.
func pickSource(v any) string {
	srcs, _ := v.([]any)
	best, bestW, largest, largestW := "", 0, "", 0
	for _, s := range srcs {
		u, w := str(s, "url"), num(s, "width")
		if u == "" {
			continue
		}
		if w >= 320 && (best == "" || w < bestW) {
			best, bestW = u, w
		}
		if w > largestW || largest == "" {
			largest, largestW = u, w
		}
	}
	if best != "" {
		return best
	}
	return largest
}

// parseDuration reads "14:31" or "1:02:03".
func parseDuration(s string) (float64, bool) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	total := 0
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return 0, false
		}
		total = total*60 + n
	}
	return float64(total), total > 0
}

// walk visits every object under v in document order; fn gets the key the object sits under
// and returns false to skip its children.
func walk(v any, fn func(key string, obj map[string]any) bool) {
	switch t := v.(type) {
	case map[string]any:
		// 키 순서를 고정해 결과를 결정적으로 만든다(목록 순서 자체는 배열이 정한다).
		for _, k := range slices.Sorted(maps.Keys(t)) {
			child := t[k]
			if m, ok := child.(map[string]any); ok && !fn(k, m) {
				continue
			}
			walk(child, fn)
		}
	case []any:
		for _, x := range t {
			walk(x, fn)
		}
	}
}

func dig(v any, path ...string) any {
	for _, k := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func str(v any, key string) string {
	s, _ := dig(v, key).(string)
	return s
}

func num(v any, key string) int {
	f, _ := dig(v, key).(float64)
	return int(f)
}

// text reads YouTube's {simpleText} or {runs:[{text}]} strings.
func text(v any) string {
	if s := str(v, "simpleText"); s != "" {
		return s
	}
	runs, _ := dig(v, "runs").([]any)
	var b strings.Builder
	for _, r := range runs {
		b.WriteString(str(r, "text"))
	}
	return b.String()
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func splitMeta(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}
