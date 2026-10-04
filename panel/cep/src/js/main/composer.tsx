// 새 다운로드 준비: 링크 → 미리보기(큰 플레이어·타임라인에서 In/Out) → 다운로드.
// 미리보기가 중심이고(docs/decisions/0011), 영상 정보는 한 줄, 받기 옵션은 접어 둔다.
// 카드(반경 2px)는 쓰지 않는다: 미리보기 틀과 행·구분선으로만 묶는다(docs/decisions/0010).
import { FormEvent, ReactNode, useRef, useState } from "react";
import {
  Draft,
  JobView,
  SequenceInfo,
  State,
  TERMINAL_STATES,
  Variant,
  bytes,
  codecLabel,
  duration,
  fpsLabel,
  fpsMismatch,
  isLink,
  isVertical,
  platformLabel,
  rangeToSend,
  variantLabel,
} from "@birdcage/core";
import type { PanelController } from "./main";
import { DraftJobs, phaseOf } from "./job-progress";
import { PreviewPanel } from "./preview-panel";

export const Composer = ({ s, ctl, focusMode = false, settings }: { s: State; ctl: PanelController; focusMode?: boolean; settings?: ReactNode }) => {
  const [submitError, setSubmitError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [pendingSignature, setPendingSignature] = useState("");
  const submitLock = useRef(false);
  const d = s.draft;
  const connected = s.connection === "connected";
  const signature = draftSignature(d);
  const draftJobs = (d.jobIds ?? []).map((id) => s.jobs.find((view) => view.snap.jobId === id)).filter((view): view is JobView => !!view);
  const lastJobID = d.jobIds?.[d.jobIds.length - 1];
  const latestJob = lastJobID ? s.jobs.find((view) => view.snap.jobId === lastJobID) : undefined;
  const waitingForJob = !!lastJobID && !latestJob;
  const matchingActive = draftJobs.some((view) => !TERMINAL_STATES.includes(view.snap.state) && matchesDraft(view, d));
  const awaitingSameDownload = pendingSignature === signature && (submitting || waitingForJob);
  const downloadDisabled = !connected || d.checking || submitting || matchingActive || awaitingSameDownload;
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!isLink(d.url) || downloadDisabled || submitLock.current) return;
    submitLock.current = true;
    setSubmitting(true);
    setPendingSignature(signature);
    setSubmitError("");
    try {
      await ctl.submitDraft();
    } catch (err) {
      setPendingSignature("");
      setSubmitError((err as Error).message);
    } finally {
      submitLock.current = false;
      setSubmitting(false);
    }
  };
  const ranged = d.range.on && !!rangeToSend(d.range, d.info?.durationSec).range;
  return (
    <form className="composer" onSubmit={submit}>
      <div className={focusMode ? "row link-row focus" : "row"}>
        {s.history.length > 0 && (
          <button type="button" className="icon back" onClick={() => ctl.back()} title={`이전 영상: ${s.history[s.history.length - 1].draft.info?.title ?? s.history[s.history.length - 1].draft.url}`}>
            ←
          </button>
        )}
        <input
          className="url"
          type="text"
          placeholder="YouTube · TikTok 링크"
          value={d.url}
          onChange={(e) => ctl.setUrl(e.target.value)}
          onKeyDown={(e) => {
            // 링크가 아닌 글자 + Enter는 유튜브 검색으로 넘긴다(상단 검색창과 같다).
            if (e.key === "Enter" && !isLink(d.url) && d.url.trim()) {
              e.preventDefault();
              ctl.search(d.url);
              ctl.clearDraft();
            }
          }}
          disabled={!connected}
        />
        {d.url && (
          <button type="button" className="icon" onClick={() => ctl.clearDraft()} title="지우기">
            ×
          </button>
        )}
        {!focusMode && (
          <button type="submit" className="primary" disabled={!connected || !isLink(d.url) || d.status === "probing" || d.checking}>
            {ranged ? "구간 받기" : "받기"}
          </button>
        )}
      </div>
      {d.status === "probing" && (
        <div className="player">
          <div className="frame">
            {/* 목록에서 연 영상은 아는 썸네일·제목을 먼저 보여 준다 */}
            {d.hint?.thumbnail ? <img className="still" src={d.hint.thumbnail} alt="" /> : <div className="still empty" />}
            <div className="overlay chip">링크 확인 중…</div>
          </div>
          {d.hint && <div className="hint-title">{d.hint.title}</div>}
        </div>
      )}
      {d.status === "failed" && (
        <div className="row status">
          <span className="error small">{d.failure?.message ?? "링크를 확인하지 못했습니다."}</span>
          <button type="button" disabled={d.checking} onClick={() => void ctl.probeDraft()}>
            {d.checking ? "확인 중…" : "다시 확인"}
          </button>
        </div>
      )}
      {submitError && <div className="error small">{submitError}</div>}
      {d.status === "ready" && d.info && (
        <>
          <PreviewPanel d={d} ctl={ctl} />
          {!focusMode && <DraftJobs s={s} d={d} ctl={ctl} />}
          <InfoLine d={d} seq={s.sequence} />
          <Options d={d} seq={s.sequence} ctl={ctl} />
          {settings}
          {focusMode && (
            <div className="row download-actions">
              {submitting || waitingForJob || latestJob ? (
                <InlineDownloadProgress view={latestJob} pending={submitting ? "다운로드 요청 중" : waitingForJob ? "다운로드 시작 중" : undefined} />
              ) : <span className="spacer" />}
              <button type="submit" className="primary download-submit" disabled={downloadDisabled}>
                {submitting ? "요청 중…" : "다운로드"}
              </button>
            </div>
          )}
        </>
      )}
    </form>
  );
};

const sameRange = (a?: { startSec: number; endSec: number }, b?: { startSec: number; endSec: number }): boolean =>
  (!a && !b) || (!!a && !!b && Math.abs(a.startSec - b.startSec) < 0.01 && Math.abs(a.endSec - b.endSec) < 0.01);

const subtitleOf = (d: Draft) => d.subtitle ? { lang: d.subtitle.replace(/:auto$/, ""), auto: d.subtitle.endsWith(":auto") } : undefined;

const matchesDraft = (view: JobView, d: Draft): boolean => {
  const expectedRange = rangeToSend(d.range, d.info?.durationSec).range;
  const actual = view.snap.options;
  const expectedSubtitle = subtitleOf(d);
  const actualSubtitle = actual.subtitles;
  return sameRange(actual.range, expectedRange)
    && !!actual.audioOnly === !!d.audioOnly
    && (actual.formatId === undefined || actual.formatId === (d.audioOnly ? undefined : d.formatId))
    && ((!actualSubtitle && !expectedSubtitle) || (!!actualSubtitle && !!expectedSubtitle && actualSubtitle.lang === expectedSubtitle.lang && !!actualSubtitle.auto === expectedSubtitle.auto));
};

const draftSignature = (d: Draft): string => {
  const range = rangeToSend(d.range, d.info?.durationSec).range;
  return JSON.stringify({ url: d.url.trim(), range, formatId: d.audioOnly ? undefined : d.formatId, audioOnly: !!d.audioOnly, subtitle: subtitleOf(d) });
};

const InlineDownloadProgress = ({ view, pending }: { view?: JobView; pending?: string }) => {
  const phase = view ? phaseOf(view) : undefined;
  const label = pending ?? phase?.label ?? "다운로드 시작 중";
  const fraction = pending ? undefined : phase?.fraction;
  const tone = phase?.tone ?? "busy";
  return (
    <span className={`inline-download-progress ${tone}`} aria-live="polite">
      <span className="row inline-status">
        <span className={tone === "bad" ? "error small" : tone === "done" ? "ok small" : "small"}>{label}</span>
        {!pending && phase?.detail && <span className="muted small inline-detail">{phase.detail}</span>}
      </span>
      <span className={`bar${fraction === undefined ? " unknown" : ""}`}>
        {fraction !== undefined && <span style={{ width: `${Math.round(fraction * 100)}%` }} />}
      </span>
    </span>
  );
};

const selected = (d: Draft): Variant | undefined => d.info?.variants?.find((v) => v.formatId === d.formatId);

const InfoLine = ({ d, seq }: { d: Draft; seq?: SequenceInfo | null }) => {
  const info = d.info!;
  const v = d.audioOnly ? undefined : selected(d);
  const meta = [info.uploader, platformLabel(info.extractor), duration(info.durationSec)].filter(Boolean).join(" · ");
  return (
    <div className="info-line">
      <div className="info-title" title={info.title}>
        {info.title}
      </div>
      <div className="muted small nowrap meta">{meta}</div>
      <div className="badges">
          {v && (v.transcode ? <span className="tag warn">변환 필요 · {codecLabel(v.vcodec)}→H.264</span> : <span className="tag ok">원본 그대로</span>)}
          {v?.hdr && <span className="tag warn">HDR</span>}
          {v && isVertical(v) && <span className="tag">세로</span>}
          {v && seq && fpsMismatch(v.fps, seq.fps) && (
            <span className="tag warn" title={`시퀀스 "${seq.name}"`}>
              시퀀스 {fpsLabel(seq.fps)}와 다름
            </span>
          )}
      </div>
    </div>
  );
};

/** 받기 옵션(화질·오디오만·자막): 요약 한 줄, 누르면 펼친다. */
const Options = ({ d, seq, ctl }: { d: Draft; seq?: SequenceInfo | null; ctl: PanelController }) => {
  const [open, setOpen] = useState(false);
  const v = selected(d);
  const summary = d.audioOnly
    ? "오디오만 · WAV 48kHz"
    : v
      ? [variantLabel(v), codecLabel(v.vcodec), bytes(v.sizeBytes)].filter(Boolean).join(" · ")
      : "자동 화질";
  const sub = d.subtitle ? ` · 자막 ${d.subtitle.replace(/:auto$/, " (자동)")}` : "";
  return (
    <div className="section options-box">
      <button type="button" className="disclosure" aria-expanded={open} onClick={() => setOpen(!open)}>
        <span className="caret">{open ? "▾" : "▸"}</span> 받기 옵션 <span className="muted">· {summary + sub}</span>
      </button>
      {open && (
        <>
          {!d.audioOnly && (d.info?.variants?.length ?? 0) > 0 && <Variants d={d} seq={seq} ctl={ctl} />}
          <Extras d={d} ctl={ctl} />
        </>
      )}
    </div>
  );
};

const Variants = ({ d, seq, ctl }: { d: Draft; seq?: SequenceInfo | null; ctl: PanelController }) => (
  <div className="section">
    <div className="section-label">
      화질
      {seq && (
        <span className="muted small">
          {" "}
          · 시퀀스 {seq.width}×{seq.height}
        </span>
      )}
    </div>
    <div className="choices" role="radiogroup" aria-label="화질">
      {d.info!.variants!.map((v) => (
        <label key={v.formatId} className={`choice${v.formatId === d.formatId ? " on" : ""}`}>
          <input type="radio" name="variant" checked={v.formatId === d.formatId} onChange={() => ctl.selectVariant(v.formatId)} />
          <span className="choice-main">{variantLabel(v)}</span>
          <span className="choice-meta muted">
            {[codecLabel(v.vcodec), fpsLabel(v.fps)].filter(Boolean).join(" · ")}
          </span>
          <span className="choice-size muted">{bytes(v.sizeBytes) || "크기 모름"}</span>
          <span className="choice-tags">
            {v.formatId === d.recommendedId && <span className="tag accent">추천</span>}
            {v.transcode && <span className="tag warn">변환</span>}
          </span>
        </label>
      ))}
    </div>
  </div>
);

/** WAV PCM 16-bit 48 kHz stereo: 192,000 bytes per second. */
const WAV_BPS = 48000 * 2 * 2;

const Extras = ({ d, ctl }: { d: Draft; ctl: PanelController }) => {
  const subs = d.info?.subtitles ?? [];
  const r = d.range.on ? rangeToSend(d.range, d.info?.durationSec).range : undefined;
  const seconds = r ? r.endSec - r.startSec + 2 : d.info?.durationSec;
  const ba = d.info?.bestAudio;
  return (
    <div className="section extras">
      <div className="row">
        <label className="check nowrap">
          <input type="checkbox" checked={!!d.audioOnly} onChange={(e) => ctl.setExtras({ audioOnly: e.target.checked })} />
          오디오만
          <span className="muted small after">
            · WAV 48kHz{seconds ? ` · 약 ${bytes(seconds * WAV_BPS)}` : ""}
            {ba?.acodec ? ` · 원본 ${codecLabel(ba.acodec)}` : ""}
          </span>
        </label>
      </div>
      <div className="row">
        <label className="subs nowrap">
          <span className="muted">자막</span>
          <select
            value={d.subtitle ?? ""}
            disabled={subs.length === 0}
            onChange={(e) => ctl.setExtras({ subtitle: e.target.value || null })}
          >
            <option value="">{subs.length === 0 ? "없음" : "받지 않음"}</option>
            {subs.map((t) => (
              <option key={t.lang + (t.auto ? ":auto" : "")} value={t.auto ? `${t.lang}:auto` : t.lang}>
                {(t.name || t.lang) + (t.auto ? " (자동)" : "")}
              </option>
            ))}
          </select>
        </label>
      </div>
    </div>
  );
};
