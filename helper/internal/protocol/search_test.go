package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"birdcage/helper/internal/related"
)

// 유튜브 검색(T-026) 메시지의 픽스처. protocol_test.go의 TestFixtures와 같은 규칙(-update로 다시 쓴다).
func TestSearchFixtures(t *testing.T) {
	items := []related.Item{{ID: "I0-g22cY3WY", URL: "https://www.youtube.com/watch?v=I0-g22cY3WY", Title: "인터뷰 B-roll 촬영 팁", Channel: "빅제임스 [Big James]",
		DurationSec: 346, Thumbnail: "https://i.ytimg.com/vi/I0-g22cY3WY/hqdefault.jpg", Meta: "조회수 6,938회 · 8년 전"}}
	for name, f := range map[string]struct {
		typ, id string
		data    any
	}{
		"search.fetch": {TypeSearchFetch, "11", SearchFetch{Query: "인터뷰 b-roll", Page: 1}},
		"search.list":  {TypeSearchList, "11", SearchList{Query: "인터뷰 b-roll", Page: 1, Items: items, More: true}},
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
