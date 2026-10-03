package preview

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"birdcage/helper/internal/library"
)

// Cache keeps preview folders on disk (not memory), one per video, evicting the least recently
// used ones beyond the byte cap. 같은 영상을 다시 열면 받지 않고 바로 쓴다.
type Cache struct {
	Root     string
	CapBytes int64
}

// Key names a video's folder, e.g. "Youtube-aqz-KE-bpKQ".
func Key(extractor, id string) string {
	return library.Sanitize(strings.TrimSpace(extractor) + "-" + strings.TrimSpace(id))
}

// Dir returns (and creates) the folder for key and marks it as just used.
func (c *Cache) Dir(key string) (string, error) {
	d := filepath.Join(c.Root, key)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	now := time.Now()
	_ = os.Chtimes(d, now, now)
	return d, nil
}

type entry struct {
	path string
	used time.Time
	size int64
}

// Evict removes the least recently used folders until the total fits CapBytes; keep is never removed.
// It returns the removed folders.
func (c *Cache) Evict(keep string) []string {
	if c.CapBytes <= 0 {
		return nil
	}
	ents, err := os.ReadDir(c.Root)
	if err != nil {
		return nil
	}
	var list []entry
	var total int64
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(c.Root, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		size := dirSize(p)
		total += size
		list = append(list, entry{p, info.ModTime(), size})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].used.Before(list[j].used) })
	var removed []string
	for _, e := range list {
		if total <= c.CapBytes {
			break
		}
		if filepath.Base(e.path) == keep {
			continue
		}
		if os.RemoveAll(e.path) == nil {
			total -= e.size
			removed = append(removed, e.path)
		}
	}
	return removed
}

func dirSize(p string) int64 {
	var n int64
	_ = filepath.WalkDir(p, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}
