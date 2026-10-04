package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"birdcage/helper/internal/related"
)

// 탐색 목록(T-036) 메시지의 픽스처. protocol_test.go의 TestFixtures와 같은 규칙(-update로 다시 쓴다).
func TestRelatedFixtures(t *testing.T) {
	items := []related.Item{
		{ID: "eoisNyKSBic", URL: "https://www.youtube.com/watch?v=eoisNyKSBic", Title: "'우사기' 는 사실 모든 것을 알고 있었습니다", Channel: "김채호의 필름찢기",
			DurationSec: 871, Thumbnail: "https://i.ytimg.com/vi/eoisNyKSBic/hqdefault.jpg", Meta: "조회수 7.8만회 · 4시간 전"},
		{ID: "s1", URL: "https://www.youtube.com/watch?v=s1", Title: "검색 결과", Channel: "B", DurationSec: 10, Thumbnail: "https://i.ytimg.com/vi/s1/mqdefault.jpg", Views: 1200},
	}
	for name, f := range map[string]struct {
		typ, id string
		data    any
	}{
		"related.fetch": {TypeRelatedFetch, "9", RelatedFetch{ProbeID: "p0a1b2c3d4e5f", Source: related.SourceYouTube}},
		"related.list":  {TypeRelatedList, "9", RelatedList{ProbeID: "p0a1b2c3d4e5f", Requested: related.SourceYouTube, Source: related.SourceYouTube, Items: items}},
	} {
		b, err := Encode(f.typ, f.id, f.data)
		if err != nil {
			t.Fatal(err)
		}
		var pretty bytes.Buffer
		_ = json.Indent(&pretty, b, "", "  ")
		pretty.WriteByte('\n')
		path := filepath.Join(fixtureDir(), name+".json")
		if *update {
			if err := os.WriteFile(path, pretty.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v (run with -update)", name, err)
		}
		if !bytes.Equal(want, pretty.Bytes()) {
			t.Errorf("%s: fixture differs from Go encoding — run `go test ./internal/protocol -update` and update panel/core types", name)
		}
	}
}
