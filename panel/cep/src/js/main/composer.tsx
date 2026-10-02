// 새 다운로드 준비: 링크 → 조회(정보 카드) → 화질 선택 → 받기.
// 카드는 영상 정보 하나만 쓴다(사각, 반경 2px). 선택지는 행·구분선으로 그린다(docs/decisions/0010).
import { FormEvent, useState } from "react";
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
  isLink,
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
