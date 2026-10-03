// Package rangefetch downloads only part of a fragmented MP4 (YouTube DASH) by byte ranges.
//
// ffmpeg로 구간을 받으면 연속 스트림이라 YouTube가 재생 속도(약 1.8배)로 제한한다(실측: 1080p 32s 구간 19.1s).
// 같은 서버에 범위 요청을 나눠 보내면 제한이 없다(10MB 1.1s, 9.1MB/s). DASH mp4에는 시간→바이트 색인(sidx)이
// 앞쪽에 있으므로 필요한 조각(moof+mdat)만 받아 초기화 부분(ftyp+moov) 뒤에 붙이면 그대로 재생 가능한 파일이 되고,
// 조각의 tfdt가 원래 타임라인 시각을 담아 구간 정렬(media.Plan.AlignTo)이 그대로 맞는다.
package rangefetch

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// ErrNoIndex means the file is not a fragmented MP4 with a usable sidx (caller falls back).
var ErrNoIndex = errors.New("rangefetch: no usable sidx index")

// ErrExpired means the server refused the URL (signed URLs expire): probe again.
var ErrExpired = errors.New("rangefetch: URL rejected (expired?)")

// Ref is one indexed subsegment (a fragment: moof+mdat), in source seconds and file bytes.
type Ref struct {
	Offset, Size int64
	Start, Dur   float64
}

// Index is the layout of a fragmented MP4: init bytes [0, InitEnd) and indexed fragments.
type Index struct {
	InitEnd int64
	Refs    []Ref
}

// ParseIndex reads top-level boxes from the head of a file and returns the init end and sidx refs.
// head must contain ftyp, moov and sidx (YouTube: 64KB is plenty).
func ParseIndex(head []byte) (*Index, error) {
	var off int64
	var initEnd int64 = -1
	for off+8 <= int64(len(head)) {
		size := int64(binary.BigEndian.Uint32(head[off:]))
		typ := string(head[off+4 : off+8])
		hdr := int64(8)
		if size == 1 {
			if off+16 > int64(len(head)) {
				break
			}
			size, hdr = int64(binary.BigEndian.Uint64(head[off+8:])), 16
		}
		if size < hdr {
			return nil, ErrNoIndex
		}
		if typ == "sidx" {
			if off+size > int64(len(head)) {
				return nil, fmt.Errorf("%w: sidx beyond head", ErrNoIndex)
			}
			if initEnd < 0 {
				initEnd = off
			}
			refs, err := parseSidx(head[off+hdr:off+size], off+size)
			if err != nil {
				return nil, err
			}
			return &Index{InitEnd: initEnd, Refs: refs}, nil
		}
		if typ == "moof" || typ == "mdat" {
			break // 색인 없이 조각이 시작된다
		}
		if typ == "moov" && off+size <= int64(len(head)) && countTraks(head[off+hdr:off+size]) != 1 {
			// 여러 트랙이 한 조각에 섞인 파일은 조각 시각(tfdt)과 처음부터 읽은 시각이 어긋난다(실측 21ms).
			// YouTube·Instagram DASH는 스트림마다 파일이 따로라(트랙 1개) 이 경로를 쓰고, 그 밖은 대체 경로로.
			return nil, fmt.Errorf("%w: not single-track", ErrNoIndex)
		}
		if typ != "ftyp" && typ != "moov" && typ != "styp" && typ != "free" && typ != "uuid" {
			initEnd = off
		}
		off += size
	}
	return nil, ErrNoIndex
}

func countTraks(moov []byte) int {
	n := 0
	for off := 0; off+8 <= len(moov); {
		size := int(binary.BigEndian.Uint32(moov[off:]))
		if size < 8 {
			break
		}
		if string(moov[off+4:off+8]) == "trak" {
			n++
		}
		off += size
	}
	return n
}

func parseSidx(b []byte, boxEnd int64) ([]Ref, error) {
	if len(b) < 12 {
		return nil, ErrNoIndex
	}
	version := b[0]
	p := 4
	p += 4 // reference_ID
	timescale := binary.BigEndian.Uint32(b[p:])
	p += 4
	var ept, first uint64
	if version == 0 {
		if len(b) < p+8 {
			return nil, ErrNoIndex
		}
		ept, first = uint64(binary.BigEndian.Uint32(b[p:])), uint64(binary.BigEndian.Uint32(b[p+4:]))
		p += 8
	} else {
		if len(b) < p+16 {
			return nil, ErrNoIndex
		}
		ept, first = binary.BigEndian.Uint64(b[p:]), binary.BigEndian.Uint64(b[p+8:])
		p += 16
	}
	p += 2 // reserved
	if len(b) < p+2 || timescale == 0 {
		return nil, ErrNoIndex
	}
	count := int(binary.BigEndian.Uint16(b[p:]))
	p += 2
	if len(b) < p+12*count {
		return nil, ErrNoIndex
	}
	offset := boxEnd + int64(first)
	t := float64(ept) / float64(timescale)
	refs := make([]Ref, 0, count)
	for i := 0; i < count; i++ {
		v := binary.BigEndian.Uint32(b[p:])
		if v&0x80000000 != 0 {
			return nil, fmt.Errorf("%w: hierarchical sidx", ErrNoIndex)
		}
		size := int64(v & 0x7fffffff)
		dur := float64(binary.BigEndian.Uint32(b[p+4:])) / float64(timescale)
		refs = append(refs, Ref{Offset: offset, Size: size, Start: t, Dur: dur})
		offset += size
		t += dur
		p += 12
	}
	return refs, nil
}

// Span returns the fragments overlapping [start, end) (at least one).
func (ix *Index) Span(start, end float64) (first, last int) {
	first, last = -1, -1
	for i, r := range ix.Refs {
		if r.Start+r.Dur > start && r.Start < end {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 { // 범위가 끝을 넘으면 마지막 조각
		first, last = len(ix.Refs)-1, len(ix.Refs)-1
	}
	return first, last
}

// Part is one stream to fetch (video or audio).
type Part struct {
	URL     string
	Headers map[string]string
}

// Client fetches parts. Zero value works (http.DefaultClient, 8MB chunks, 4 at a time).
type Client struct {
	HTTP      *http.Client
	ChunkSize int64
	Parallel  int
	Retries   int
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// get fetches [a, b) of url into dst at dstOff (WriteAt), retrying network errors.
func (c *Client) get(ctx context.Context, p Part, a, b int64, dst io.WriterAt, dstOff int64, onBytes func(int64)) error {
	retries := c.Retries
	if retries == 0 {
		retries = 3
	}
	var last error
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			}
		}
		n, err := c.getOnce(ctx, p, a, b, dst, dstOff, onBytes)
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrExpired) || ctx.Err() != nil {
			return err
		}
		last = err
		// 받은 만큼은 두고 나머지를 이어 받는다.
		a += n
		dstOff += n
	}
	return last
}

func (c *Client) getOnce(ctx context.Context, p Part, a, b int64, dst io.WriterAt, dstOff int64, onBytes func(int64)) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	if err != nil {
		return 0, err
	}
	for k, v := range p.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Range", "bytes="+strconv.FormatInt(a, 10)+"-"+strconv.FormatInt(b-1, 10))
	resp, err := c.http().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound:
		return 0, fmt.Errorf("%w: HTTP %d", ErrExpired, resp.StatusCode)
	case resp.StatusCode != http.StatusPartialContent:
		return 0, fmt.Errorf("rangefetch: HTTP %d (want 206)", resp.StatusCode)
	}
	buf := make([]byte, 256<<10)
	var n int64
	for n < b-a {
		k, err := resp.Body.Read(buf[:min(int64(len(buf)), b-a-n)])
		if k > 0 {
			if _, werr := dst.WriteAt(buf[:k], dstOff+n); werr != nil {
				return n, werr
			}
			n += int64(k)
			onBytes(int64(k))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return n, err
		}
	}
	if n != b-a {
		return n, fmt.Errorf("rangefetch: short read %d of %d", n, b-a)
	}
	return n, nil
}

// Head reads the first n bytes (index discovery).
func (c *Client) Head(ctx context.Context, p Part, n int64) ([]byte, error) {
	w := &memWriter{buf: make([]byte, n)}
	got := int64(0)
	err := c.get(ctx, p, 0, n, w, 0, func(k int64) { got += k })
	if err != nil {
		// 파일이 n보다 작으면 206 대신 짧게 끝날 수 있다: 받은 만큼으로 시도.
		if got > 0 && !errors.Is(err, ErrExpired) {
			return w.buf[:got], nil
		}
		return nil, err
	}
	return w.buf, nil
}

type memWriter struct{ buf []byte }

func (m *memWriter) WriteAt(p []byte, off int64) (int, error) {
	if off+int64(len(p)) > int64(len(m.buf)) {
		return 0, io.ErrShortBuffer
	}
	return copy(m.buf[off:], p), nil
}

// Progress reports fetched bytes of the total for all parts.
type Progress struct {
	Done, Total int64
	SpeedBps    float64
}

// Fetch writes, for each part, a playable fragmented MP4 with only the fragments covering
// [start, end) to paths[i]. It returns the source time where each output begins.
func (c *Client) Fetch(ctx context.Context, parts []Part, start, end float64, paths []string, progress func(Progress)) ([]float64, error) {
	type job struct {
		part   Part
		ix     *Index
		first  int
		last   int
		out    *os.File
		starts float64
	}
	jobs := make([]*job, len(parts))
	var total int64
	for i, p := range parts {
		head, err := c.Head(ctx, p, 64<<10)
		if err != nil {
			return nil, err
		}
		ix, err := ParseIndex(head)
		if err != nil {
			return nil, err
		}
		if ix.InitEnd <= 0 || ix.InitEnd > int64(len(head)) {
			return nil, fmt.Errorf("%w: init segment", ErrNoIndex)
		}
		f, l := ix.Span(start, end)
		out, err := os.Create(paths[i])
		if err != nil {
			return nil, err
		}
		defer out.Close()
		if _, err := out.WriteAt(head[:ix.InitEnd], 0); err != nil {
			return nil, err
		}
		jobs[i] = &job{part: p, ix: ix, first: f, last: l, out: out, starts: ix.Refs[f].Start}
		total += ix.Refs[l].Offset + ix.Refs[l].Size - ix.Refs[f].Offset
	}

	chunk := c.ChunkSize
	if chunk <= 0 {
		chunk = 8 << 20 // YouTube는 큰 단일 요청을 제한한다(yt-dlp도 10MB 단위로 나눠 받는다)
	}
	par := c.Parallel
	if par <= 0 {
		par = 4
	}
	type piece struct {
		j          *job
		a, b, dOff int64
	}
	var pieces []piece
	for _, j := range jobs {
		a := j.ix.Refs[j.first].Offset
		b := j.ix.Refs[j.last].Offset + j.ix.Refs[j.last].Size
		dOff := j.ix.InitEnd - a // 초기화 부분 바로 뒤에 조각들을 잇는다
		for s := a; s < b; s += chunk {
			pieces = append(pieces, piece{j, s, min(s+chunk, b), dOff})
		}
	}
	var done atomic.Int64
	t0 := time.Now()
	var lastReport atomic.Int64
	report := func(force bool) {
		now := time.Since(t0)
		if !force && now.Milliseconds()-lastReport.Load() < 200 {
			return
		}
		lastReport.Store(now.Milliseconds())
		if progress != nil {
			d := done.Load()
			progress(Progress{Done: d, Total: total, SpeedBps: float64(d) / max(now.Seconds(), 0.001)})
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	sem := make(chan struct{}, par)
	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error
	var mu sync.Mutex
	for _, pc := range pieces {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(pc piece) {
			defer wg.Done()
			defer func() { <-sem }()
			err := c.get(ctx, pc.j.part, pc.a, pc.b, pc.j.out, pc.a+pc.dOff, func(k int64) {
				done.Add(k)
				mu.Lock()
				report(false)
				mu.Unlock()
			})
			if err != nil {
				once.Do(func() { firstErr = err; cancel() })
			}
		}(pc)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	report(true)
	starts := make([]float64, len(jobs))
	for i, j := range jobs {
		starts[i] = j.starts
	}
	return starts, nil
}
