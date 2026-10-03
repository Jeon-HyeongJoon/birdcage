// Package preview prepares what the panel shows before anything is downloaded for editing:
// storyboard → waveform (audio first) → playable 360p H.264/AAC (docs/decisions/0011).
//
// 단계마다 상태를 내보내 패널이 바로 그릴 수 있게 한다. 결과는 디스크 캐시에 남아 다시 열면 즉시 준비된다.
package preview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"birdcage/helper/internal/engine"
	"birdcage/helper/internal/media"
	"birdcage/helper/internal/perf"
	"birdcage/helper/internal/proc"
)

// Stage of a preview.
type Stage string

const (
	StageAudio  Stage = "audio"  // 오디오를 받는 중(스토리보드는 media.info로 이미 있음)
	StageVideo  Stage = "video"  // 파형 준비, 미리보기 비디오를 받는 중
	StageReady  Stage = "ready"  // 재생 가능
	StageFailed Stage = "failed" // Error 참고(이미 준비된 부분은 그대로 쓴다)
)

// Video is the playable preview file (MP4 H.264 + AAC, faststart).
type Video struct {
	Path        string  `json:"path"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	FPS         float64 `json:"fps"`
	DurationSec float64 `json:"durationSec"`
}

// State is sent to the panel on every stage change (and on download progress).
type State struct {
	ProbeID  string  `json:"probeId"`
	Stage    Stage   `json:"stage"`
	Progress float64 `json:"progress"` // current download 0..1, -1 unknown
	Peaks    *Peaks  `json:"peaks,omitempty"`
	Video    *Video  `json:"video,omitempty"`
	// Storyboard is generated locally from the preview video when the platform has none.
	Storyboard *engine.Storyboard `json:"storyboard,omitempty"`
	Cached     bool               `json:"cached,omitempty"`
	Error      string             `json:"error,omitempty"`
}

// MaxHeight of the preview video: 패널 폭(400–800px)에 충분하고 10분에 28MB, 4.2s(실측).
const MaxHeight = 360

// Service runs previews.
type Service struct {
	Cache Cache
	Rec   *perf.Recorder
}

// Tools are what a preview run uses (resolved per run like jobs.Toolset).
type Tools struct {
	Engine engine.Engine
	FFmpeg string
	Prober *media.Prober
}

type meta struct {
	Peaks      *Peaks             `json:"peaks"`
	Video      *Video             `json:"video"`
	Storyboard *engine.Storyboard `json:"storyboard,omitempty"`
}

// Run prepares the preview of a probed item and calls emit for each state. It returns when the
// preview is ready, failed, or ctx is cancelled (패널이 다른 링크로 바꾸면 취소된다).
func (s *Service) Run(ctx context.Context, probeID string, info *engine.Info, t Tools, emit func(State)) {
	st := State{ProbeID: probeID, Stage: StageAudio, Progress: -1}
	fail := func(err error) {
		if ctx.Err() != nil {
			return // 취소: 알리지 않는다
		}
		st.Stage, st.Error = StageFailed, err.Error()
		var f *engine.Failure
		if errors.As(err, &f) {
			st.Error = f.Message
		}
		emit(st)
	}
	key := Key(info.Extractor, info.ID)
	dir, err := s.Cache.Dir(key)
	if err != nil {
		fail(err)
		return
	}
	span := s.Rec.Start("preview", "key", key)
	defer func() { span.End(nil) }()

	// 캐시에 완성본이 있으면 바로.
	if m, ok := loadMeta(dir); ok {
		st.Stage, st.Peaks, st.Video, st.Storyboard, st.Cached, st.Progress = StageReady, m.Peaks, m.Video, m.Storyboard, true, 1
		span.Set("cached", true)
		emit(st)
		return
	}
	emit(st)

	// 받을 때는 조회 결과(info.json)를 캐시 폴더로 복사해 쓴다(조회 캐시가 지워져도 계속).
	src := *info
	if b, err := os.ReadFile(info.InfoPath); err == nil {
		src.InfoPath = filepath.Join(dir, "info.json")
		if err := os.WriteFile(src.InfoPath, b, 0o644); err != nil {
			fail(err)
			return
		}
	}

	// 1) 오디오 먼저 → 파형. 오디오 전용 포맷이 없으면(직접 링크 등) 360p 이하 하나를 받아 둘 다에 쓴다.
	single := info.BestAudio == nil
	progress := func(p float64) {
		st.Progress = p
		emit(st)
	}
	t0 := time.Now()
	var audio, vid string
	if single {
		vid, err = s.fetch(ctx, t, &src, dir, "media", engine.DownloadRequest{MaxHeight: MaxHeight, FormatID: fmt.Sprintf("b[height<=%d]/b", MaxHeight)}, progress)
		audio = vid
	} else {
		audio, err = s.fetch(ctx, t, &src, dir, "audio", engine.DownloadRequest{AudioOnly: true, FormatID: "ba[ext=m4a]"}, progress)
	}
	if err != nil {
		fail(err)
		return
	}
	span.Set("audio_ms", perf.MS(time.Since(t0)), "single", single)
	if a, _, err := t.Prober.Probe(ctx, audio); err != nil || a.Audio == nil {
		audio = "" // 소리 없는 영상: 파형 없이 계속
	} else {
		t1 := time.Now()
		peaks, err := ComputePeaks(ctx, t.FFmpeg, audio)
		if err != nil {
			fail(fmt.Errorf("파형 계산 실패: %w", err))
			return
		}
		span.Set("peaks_ms", perf.MS(time.Since(t1)), "buckets", peaks.Count)
		st.Peaks = &peaks
	}
	st.Stage, st.Progress = StageVideo, -1
	emit(st)

	// 2) 미리보기 비디오(비디오 전용 360p H.264) → 받아 둔 오디오와 결합(복사)
	t2 := time.Now()
	if !single {
		vid, err = s.fetch(ctx, t, &src, dir, "video", engine.DownloadRequest{MaxHeight: MaxHeight, FormatID: fmt.Sprintf("bv*[height<=%d][vcodec^=avc1]", MaxHeight)}, progress)
		if err != nil {
			fail(err)
			return
		}
	}
	span.Set("video_ms", perf.MS(time.Since(t2)))
	out := filepath.Join(dir, "preview.mp4")
	v, err := s.mux(ctx, t, vid, audio, out)
	if err != nil {
		fail(fmt.Errorf("미리보기 결합 실패: %w", err))
		return
	}
	_ = os.Remove(vid)
	if audio != vid && audio != "" {
		// 오디오 원본은 지운다: 결합본에 들어 있고 캐시 용량을 아낀다.
		_ = os.Remove(audio)
	}

	// 3) 플랫폼 스토리보드가 없으면 미리보기에서 만든다(같은 형식이라 패널은 한 가지로 그린다).
	var sb *engine.Storyboard
	if len(info.Storyboards) == 0 {
		t3 := time.Now()
		if g, err := GenerateStoryboard(ctx, t.FFmpeg, out, dir, v.DurationSec); err == nil {
			sb = g
		}
		span.Set("storyboard_ms", perf.MS(time.Since(t3)))
	}
	m := meta{Peaks: st.Peaks, Video: v, Storyboard: sb}
	if b, err := json.Marshal(m); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o644)
	}
	st.Stage, st.Video, st.Storyboard, st.Progress = StageReady, v, sb, 1
	emit(st)
	if removed := s.Cache.Evict(key); len(removed) > 0 {
		span.Set("evicted", len(removed))
	}
}

func loadMeta(dir string) (meta, bool) {
	var m meta
	b, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil || json.Unmarshal(b, &m) != nil || m.Video == nil {
		return meta{}, false
	}
	if _, err := os.Stat(m.Video.Path); err != nil {
		return meta{}, false
	}
	return m, true
}

// fetch downloads one part with the engine into dir/<name>.<ext>, reusing an earlier complete file.
func (s *Service) fetch(ctx context.Context, t Tools, info *engine.Info, dir, name string, req engine.DownloadRequest, progress func(float64)) (string, error) {
	if done, _ := filepath.Glob(filepath.Join(dir, name+".*")); len(done) > 0 {
		return done[0], nil
	}
	work := filepath.Join(dir, "tmp-"+name)
	_ = os.RemoveAll(work)
	defer os.RemoveAll(work)
	req.Info, req.WorkDir = info, work
	var last time.Time
	res, err := t.Engine.Download(ctx, req, func(p engine.Progress) {
		if time.Since(last) < 200*time.Millisecond && p.Status != "finished" {
			return
		}
		last = time.Now()
		progress(p.Fraction)
	})
	if err != nil {
		return "", err
	}
	dst := filepath.Join(dir, name+strings.ToLower(filepath.Ext(res.Path)))
	if err := os.Rename(res.Path, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// mux joins the video stream with the separately fetched audio (copy). A non-H.264 fallback
// (플랫폼에 360p 이하 H.264가 없을 때)는 CEP가 재생을 보장하는 H.264로 바꾼다.
func (s *Service) mux(ctx context.Context, t Tools, video, audio, out string) (*Video, error) {
	in, _, err := t.Prober.Probe(ctx, video)
	if err != nil {
		return nil, err
	}
	if in.Video == nil {
		return nil, errors.New("미리보기 비디오 스트림이 없습니다")
	}
	vargs := []string{"-c:v", "copy", "-tag:v", "avc1"}
	if in.Video.Codec != "h264" || in.Video.BitDepth() != 8 {
		vargs = []string{"-c:v", "libx264", "-preset", "veryfast", "-crf", "26", "-vf", fmt.Sprintf("scale=-2:'min(%d,ih)'", MaxHeight), "-pix_fmt", "yuv420p"}
	}
	tmp := out + ".tmp.mp4"
	args := []string{"-hide_banner", "-nostdin", "-y", "-loglevel", "error", "-i", video}
	if audio != "" {
		args = append(args, "-i", audio, "-map", "0:v:0", "-map", "1:a:0")
	} else {
		args = append(args, "-map", "0:v:0")
	}
	args = append(args, vargs...)
	if audio != "" {
		acodec := []string{"-c:a", "copy"}
		if a, _, err := t.Prober.Probe(ctx, audio); err != nil || a.Audio == nil || a.Audio.Codec != "aac" {
			acodec = []string{"-c:a", "aac", "-b:a", "128k"} // MP4에 넣고 CEP가 재생할 수 있게
		}
		args = append(args, acodec...)
	}
	args = append(args, "-shortest", "-movflags", "+faststart", "-f", "mp4", tmp)
	if _, err := proc.Run(ctx, proc.Spec{Path: t.FFmpeg, Args: args, StderrTail: 10}); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	if err := os.Rename(tmp, out); err != nil {
		return nil, err
	}
	got, _, err := t.Prober.Probe(ctx, out)
	if err != nil || got.Video == nil {
		return nil, fmt.Errorf("미리보기 확인 실패: %v", err)
	}
	return &Video{Path: out, Width: got.Video.Width, Height: got.Video.Height, FPS: got.Video.FPS, DurationSec: got.DurationSec}, nil
}

// GenerateStoryboard builds 160×90 tiles (10×10 per sprite, center-cropped to fill: 세로·4:3도 필름스트립이 꽉 찬다) from a local video,
// at most ~100 tiles over the duration (최소 0.5s 간격).
func GenerateStoryboard(ctx context.Context, ffmpeg, video, dir string, dur float64) (*engine.Storyboard, error) {
	if dur <= 0 {
		return nil, errors.New("unknown duration")
	}
	interval := math.Max(0.5, dur/100)
	pattern := filepath.Join(dir, "sb_%03d.jpg")
	old, _ := filepath.Glob(filepath.Join(dir, "sb_*.jpg"))
	for _, o := range old {
		_ = os.Remove(o)
	}
	vf := fmt.Sprintf("fps=1/%.6f,scale=160:90:force_original_aspect_ratio=increase,crop=160:90,tile=10x10", interval)
	if _, err := proc.Run(ctx, proc.Spec{Path: ffmpeg, StderrTail: 10, Args: []string{"-hide_banner", "-nostdin", "-y", "-loglevel", "error",
		"-i", video, "-an", "-vf", vf, "-q:v", "5", "-start_number", "0", pattern}}); err != nil {
		return nil, err
	}
	files, _ := filepath.Glob(filepath.Join(dir, "sb_*.jpg"))
	if len(files) == 0 {
		return nil, errors.New("no storyboard sprites")
	}
	sb := &engine.Storyboard{Width: 160, Height: 90, Cols: 10, Rows: 10, IntervalSec: interval}
	per := interval * 100
	for i, f := range files {
		sb.Sprites = append(sb.Sprites, engine.Sprite{URL: f, StartSec: float64(i) * per, DurationSec: math.Min(per, dur-float64(i)*per)})
	}
	return sb, nil
}
