// 새 다운로드 준비: 링크 → 조회(정보 카드) → 화질 선택 → 받기.
// 카드는 영상 정보 하나만 쓴다(사각, 반경 2px). 선택지는 행·구분선으로 그린다(docs/decisions/0010).
import { FormEvent, useEffect, useState } from "react";
import {
  Draft,
  SequenceInfo,
  State,
  Variant,
  bytes,
  codecLabel,
  duration,
  fpsLabel,
  fpsMismatch,
  formatTime,
  isLink,
  parseTime,
  rangeToSend,
  isVertical,
  platformLabel,
  variantLabel,
} from "@birdcage/core";
import type { PanelController } from "./main";

export const Composer = ({ s, ctl }: { s: State; ctl: PanelController }) => {
  const [submitError, setSubmitError] = useState("");
  const d = s.draft;
  const connected = s.connection === "connected";
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!isLink(d.url)) return;
    setSubmitError("");
    try {
      await ctl.submitDraft();
    } catch (err) {
      setSubmitError((err as Error).message);
    }
  };
  return (
    <form className="composer" onSubmit={submit}>
      <div className="row">
        <input
          className="url"
          type="text"
          placeholder="YouTube · Instagram · TikTok 링크"
          value={d.url}
          onChange={(e) => ctl.setUrl(e.target.value)}
          disabled={!connected}
        />
        <button type="submit" className="primary" disabled={!connected || !isLink(d.url) || d.status === "probing"}>
          받기
        </button>
      </div>
      {d.status === "probing" && <div className="muted small status">링크 확인 중…</div>}
      {d.status === "failed" && (
        <div className="row status">
          <span className="error small">{d.failure?.message ?? "링크를 확인하지 못했습니다."}</span>
          <button type="button" onClick={() => void ctl.probeDraft()}>
            다시 확인
          </button>
        </div>
      )}
      {submitError && <div className="error small">{submitError}</div>}
      {d.status === "ready" && d.info && <InfoCard d={d} seq={s.sequence} />}
      {d.status === "ready" && (d.info?.variants?.length ?? 0) > 0 && <Variants d={d} seq={s.sequence} ctl={ctl} />}
      {d.status === "ready" && <RangeSection d={d} ctl={ctl} />}
    </form>
  );
};

const selected = (d: Draft): Variant | undefined => d.info?.variants?.find((v) => v.formatId === d.formatId);

const InfoCard = ({ d, seq }: { d: Draft; seq?: SequenceInfo | null }) => {
  const info = d.info!;
  const v = selected(d);
  const [thumbOk, setThumbOk] = useState(true);
  const meta = [info.uploader, platformLabel(info.extractor), duration(info.durationSec)].filter(Boolean).join(" · ");
  return (
    <div className="card info">
      {info.thumbnail && thumbOk ? (
        <img className="thumb" src={info.thumbnail} width={96} height={54} alt="" onError={() => setThumbOk(false)} />
      ) : (
        <div className="thumb empty" />
      )}
      <div className="info-body">
        <div className="info-title" title={info.title}>
          {info.title}
        </div>
        <div className="muted small">{meta}</div>
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

/** Time input that keeps the typed text until it parses (blur/Enter commits). */
const TimeField = ({ label, value, onCommit }: { label: string; value?: number; onCommit: (v?: number) => void }) => {
  const [text, setText] = useState(value === undefined ? "" : formatTime(value));
  const [bad, setBad] = useState(false);
  useEffect(() => {
    setText(value === undefined ? "" : formatTime(value));
    setBad(false);
  }, [value]);
  const commit = () => {
    if (text.trim() === "") {
      onCommit(undefined);
      return;
    }
    const v = parseTime(text);
    setBad(v === undefined);
    if (v !== undefined) onCommit(v);
  };
  return (
    <label className="time">
      <span className="muted">{label}</span>
      <input
        type="text"
        className={bad ? "bad" : ""}
        value={text}
        placeholder="0:00.0"
        onChange={(e) => setText(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            commit();
          }
        }}
      />
    </label>
  );
};

const RangeSection = ({ d, ctl }: { d: Draft; ctl: PanelController }) => {
  const dur = d.info?.durationSec;
  const r = d.range;
  const { range, error } = rangeToSend(r, dur);
  const start = r.start ?? 0;
  const end = r.end ?? dur ?? 0;
  const pct = (x: number) => (dur ? `${Math.max(0, Math.min(100, (x / dur) * 100))}%` : "0%");
  return (
    <div className="section range">
      <label className="section-label check">
        <input type="checkbox" checked={r.on} onChange={(e) => ctl.setRange({ on: e.target.checked })} />
        구간만 받기
        {dur ? <span className="muted small after">· 전체 {duration(dur)}</span> : null}
      </label>
      {r.on && (
        <>
          <div className="row times">
            <TimeField label="시작" value={r.start} onCommit={(v) => ctl.setRange({ start: v })} />
            <TimeField label="끝" value={r.end ?? dur} onCommit={(v) => ctl.setRange({ end: v })} />
            <span className="muted small">{range ? `${formatTime(range.endSec - range.startSec)} 길이` : ""}</span>
          </div>
          {dur ? (
            <div className="rangebar" aria-hidden="true">
              <div className="sel" style={{ left: pct(start), width: `calc(${pct(end)} - ${pct(start)})` }} />
            </div>
          ) : null}
          {error ? (
            <div className="error small">{error}</div>
          ) : (
            <div className="muted small">앞뒤 1초 여유를 붙여 받고, 프로젝트 항목에 In/Out을 설정합니다.</div>
          )}
        </>
      )}
    </div>
  );
};
