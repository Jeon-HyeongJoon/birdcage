package tools

import (
	"context"
	"errors"
	"fmt"
)

// Manager combines lookup and installation.
type Manager struct {
	Loc  *Locator
	Inst *Installer
}

// Status reports every tool.
func (m *Manager) Status(ctx context.Context) []Status { return m.Loc.StatusAll(ctx) }

// Ensure installs yt-dlp and a JS runtime when missing. ffmpeg/ffprobe는 V0에서 자동 설치하지 않는다
// (macOS는 Homebrew, Windows는 수동 배치 — 설치기는 V1).
func (m *Manager) Ensure(ctx context.Context, ch Channel) ([]Status, error) {
	var errs []error
	if _, err := m.Loc.Find(ctx, YtDlp); err != nil {
		if _, err := m.Inst.InstallYtDlp(ctx, ch); err != nil {
			errs = append(errs, err)
		}
		m.Loc.Invalidate(YtDlp)
	}
	// node가 있어도 deno를 설치한다: yt-dlp 권장 런타임이고, PATH의 node는 버전 관리자(fnm/nvm)의
	// 셸별 임시 경로인 경우가 많아 Premiere가 띄운 Helper에서는 사라질 수 있다(실측: fnm_multishells 경로).
	if _, err := m.Loc.Find(ctx, Deno); err != nil {
		if _, err := m.Inst.InstallDeno(ctx); err != nil {
			errs = append(errs, err)
		}
		m.Loc.Invalidate(Deno)
	}
	for _, n := range []Name{FFmpeg, FFprobe} {
		if _, err := m.Loc.Find(ctx, n); err != nil {
			errs = append(errs, fmt.Errorf("%s 없음: macOS는 `brew install ffmpeg`, Windows는 ffmpeg.exe/ffprobe.exe를 PATH나 --%s로 지정", n, n))
		}
	}
	return m.Loc.StatusAll(ctx), errors.Join(errs...)
}

// Update installs the latest yt-dlp of the channel even if one exists (L2 폴백의 기반).
func (m *Manager) Update(ctx context.Context, ch Channel) (Tool, error) {
	t, err := m.Inst.InstallYtDlp(ctx, ch)
	m.Loc.Invalidate(YtDlp)
	return t, err
}
