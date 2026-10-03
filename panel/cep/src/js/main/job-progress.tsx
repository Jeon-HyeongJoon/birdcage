// 이 미리보기에서 시작한 받기의 진행 상황을 미리보기 바로 아래에 크게: 지금 기다려야 하는지 한눈에.
// 단계(받기 → 변환 → 넣기), 굵은 진행 막대(진행률을 모르면 움직이는 빗금), 속도·남은 시간.
import { Draft, JobView, State, bytes, eta, formatTime, speed } from "@birdcage/core";
import type { PanelController } from "./main";

const STEPS = ["받기", "변환", "넣기"] as const;

type Phase = { step: number; label: string; fraction?: number; detail?: string; tone: "busy" | "done" | "bad" };

export function phaseOf(v: JobView): Phase {
  const j = v.snap;
  const imp = v.import?.state;
  switch (j.state) {
    case "queued":
      return { step: 0, label: "곧 받기 시작", tone: "busy" };
    case "probing":
      return { step: 0, label: "링크 확인 중", tone: "busy" };
    case "downloading": {
      const d = j.download;
      const f = d && d.fraction >= 0 ? d.fraction : undefined;
      const detail = d ? [bytes(d.downloadedBytes) + (d.totalBytes ? ` / ${bytes(d.totalBytes)}` : ""), speed(d.speedBps), eta(d.etaSec)].filter(Boolean).join(" · ") : "";
      return { step: 0, label: f !== undefined ? `받는 중 ${Math.round(f * 100)}%` : "받는 중", fraction: f, detail, tone: "busy" };
    }
    case "normalizing":
      return { step: 1, label: j.progress >= 0 ? `Premiere용 변환 중 ${Math.round(j.progress * 100)}%` : "Premiere용 변환 중", fraction: j.progress >= 0 ? j.progress : undefined, tone: "busy" };
    case "done":
      if (imp === "pending" || imp === "importing") return { step: 2, label: "타임라인에 넣는 중", tone: "busy" };
      if (imp === "failed") return { step: 2, label: `넣기 실패: ${v.import?.error ?? ""}`, fraction: 1, tone: "bad" };
      if (imp === "done") {
        const where = v.import?.placed === "ok" ? "새 트랙에 넣었습니다" : v.import?.placed === "source" ? "소스 모니터로 열었습니다" : "프로젝트에 넣었습니다";
        return { step: 3, label: `완료 · ${where}`, fraction: 1, tone: "done" };
      }
      return { step: 3, label: "받기 완료", fraction: 1, tone: "done" };
    case "cancelled":
      return { step: 0, label: "취소됨", fraction: 0, tone: "bad" };
    default:
      return { step: 0, label: j.failure?.message ?? "실패", fraction: 0, tone: "bad" };
  }
}

export const DraftJobs = ({ s, d, ctl }: { s: State; d: Draft; ctl: Pick<PanelController, "cancel" | "importJob"> }) => {
  const views = (d.jobIds ?? []).map((id) => s.jobs.find((j) => j.snap.jobId === id)).filter((v): v is JobView => !!v);
  if (views.length === 0) return null;
  const v = views[views.length - 1];
  const p = phaseOf(v);
  const j = v.snap;
  const active = p.tone === "busy";
  const r = j.options.range;
  return (
    <div className={`job-progress ${p.tone}`} aria-live="polite">
      <div className="steps">
        {STEPS.map((name, i) => (
          <span key={name} className={i < p.step ? "step done" : i === p.step && p.tone !== "done" ? `step now ${p.tone}` : "step"}>
            {i < p.step ? "✓" : i + 1} {name}
          </span>
        ))}
        {r && <span className="muted small range-tag">구간 {formatTime(r.startSec)}–{formatTime(r.endSec)}</span>}
      </div>
      <div className={`big-bar${p.fraction === undefined ? " unknown" : ""}`}>
        {p.fraction !== undefined && <div style={{ width: `${Math.round(p.fraction * 100)}%` }} />}
      </div>
      <div className="row status-line">
        <span className={p.tone === "bad" ? "error small" : p.tone === "done" ? "ok small" : "small"}>{p.label}</span>
        {p.detail && <span className="muted small">{p.detail}</span>}
        <span className="spacer" />
        {active && j.state !== "done" && (
          <button type="button" onClick={() => ctl.cancel(j.jobId)}>
            취소
          </button>
        )}
        {j.state === "done" && v.import?.state === "failed" && (
          <button type="button" onClick={() => void ctl.importJob(j.jobId)}>
            다시 넣기
          </button>
        )}
      </div>
      {views.length > 1 && <div className="muted small">이 영상에서 받은 구간 {views.length}개 · 아래 목록에서 모두 볼 수 있습니다</div>}
    </div>
  );
};
