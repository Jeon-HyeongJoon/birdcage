import { useEffect, useState, useSyncExternalStore } from "react";
import {
  Controller,
  HostAdapter,
  JobView,
  STATE_LABEL,
  TERMINAL_STATES,
  eta,
  mediaSummary,
  formatTime,
  overallProgress,
  progressKnown,
  speed,
  title,
} from "@birdcage/core";
import { subscribeBackgroundColor } from "../lib/utils/bolt";
import { withDownloadHistory } from "../download-history";
import { Composer } from "./composer";
import { phaseOf } from "./job-progress";
import { SettingsRow, StatusDot } from "./status";
import "./main.scss";

export type PanelController = Pick<
  Controller,
  | "state"
  | "subscribe"
  | "start"
  | "missingTools"
  | "setUrl"
  | "probeDraft"
  | "selectVariant"
  | "setRange"
  | "setExtras"
  | "submitDraft"
  | "clearDraft"
  | "retryConnection"
  | "installTools"
  | "updateSettings"
  | "clearFinished"
  | "cancel"
  | "importJob"
>;

export type PanelHost = Pick<HostAdapter, "revealFile" | "captureKeys"> & { openDownloads?: () => void };

export const App = ({ controller: ctl, host, downloadHistory = () => [] }: { controller: PanelController; host: PanelHost; downloadHistory?: () => JobView[] }) => {
  const s = useSyncExternalStore(
    (cb) => ctl.subscribe(cb),
    () => ctl.state,
  );
  const [bg, setBg] = useState("#232323");
  const focusMode = !!host.openDownloads;
  const visibleJobs = focusMode ? withDownloadHistory(s.jobs, downloadHistory()) : s.jobs;
  const activeJobs = visibleJobs.filter(isActiveJob);

  useEffect(() => {
    if (window.cep) subscribeBackgroundColor(setBg);
    host.captureKeys?.();
    void ctl.start();
  }, [ctl, host]);

  const missing = s.connection === "connected" ? ctl.missingTools() : [];

  return (
    <div className="app" style={{ backgroundColor: bg }}>
      <header className="row header">
        <strong>Birdcage</strong>
        <StatusDot s={s} missing={missing} retry={() => ctl.retryConnection()} />
        {focusMode && (
          <>
            <span className="spacer" />
            <button className="downloads-window" onClick={() => host.openDownloads?.()}>
              다운로드{activeJobs.length > 0 ? ` ${activeJobs.length}` : ""}
            </button>
          </>
        )}
      </header>
      {focusMode && visibleJobs.length > 0 && <DownloadSummary jobs={visibleJobs} open={() => host.openDownloads?.()} />}
      {s.connection === "failed" && (
        <div className="row status-error">
          <pre className="error">{s.error}</pre>
          <button onClick={() => ctl.retryConnection()}>다시 시도</button>
        </div>
      )}

      {missing.length > 0 && (
        <div className="notice">
          <div>필요한 도구가 없습니다: {missing.join(", ")}</div>
          <button disabled={s.toolsInstalling} onClick={() => ctl.installTools()}>
            {s.toolsInstalling ? "설치 중…" : "도구 설치"}
          </button>
          {s.toolsError && <pre className="error">{s.toolsError}</pre>}
        </div>
      )}

      <Composer s={s} ctl={ctl} focusMode={focusMode} settings={focusMode ? <SettingsRow s={s} ctl={ctl} /> : undefined} />

      {!focusMode && <SettingsRow s={s} ctl={ctl} />}

      {!focusMode && (
        <ul className="jobs">
          {s.jobs.map((j) => (
            <JobItem key={j.snap.jobId} view={j} controller={ctl} host={host} />
          ))}
        </ul>
      )}

      {!focusMode && (
        <footer className="row footer muted small">
          <span title={s.helper?.libraryDir}>
            저장 위치: {s.helper?.libraryDir ?? "-"}
          </span>
          {s.jobs.some((j) => TERMINAL_STATES.includes(j.snap.state)) && (
            <button onClick={() => ctl.clearFinished()}>완료 항목 지우기</button>
          )}
        </footer>
      )}
    </div>
  );
};

const isActiveJob = (view: JobView): boolean =>
  !TERMINAL_STATES.includes(view.snap.state) || view.import?.state === "pending" || view.import?.state === "importing";

const DownloadSummary = ({ jobs, open }: { jobs: JobView[]; open: () => void }) => {
  const active = jobs.filter(isActiveJob);
  const completed = jobs.filter((view) => view.snap.state === "done");
  const current = active[0] ?? completed[0] ?? jobs[0];
  const phase = phaseOf(current);
  const running = active.length > 0;
  return (
    <button type="button" className={`download-summary ${phase.tone}`} onClick={open} aria-live="polite">
      <span className="row summary-head">
        <span className="summary-title" title={current.snap.info?.title || current.snap.url}>{title(current.snap)}</span>
        <span className={phase.tone === "bad" ? "error small" : phase.tone === "done" ? "ok small" : "small"}>
          {running ? phase.label : current.snap.state === "done" ? "다운로드 완료" : phase.label}
        </span>
      </span>
      {running && (
        <span className={`bar${phase.fraction === undefined ? " unknown" : ""}`}>
          {phase.fraction !== undefined && <span style={{ width: `${Math.round(phase.fraction * 100)}%` }} />}
        </span>
      )}
      <span className="row summary-meta muted small">
        {phase.detail && <span>{phase.detail}</span>}
        <span className="spacer" />
        {active.length > 0 && <span>진행 중 {active.length}</span>}
        {completed.length > 0 && <span>완료 기록 {completed.length}</span>}
        <span>자세히 ›</span>
      </span>
    </button>
  );
};

const JobItem = ({ view, controller: ctl, host }: { view: JobView; controller: PanelController; host: PanelHost }) => {
  const j = view.snap;
  const active = !TERMINAL_STATES.includes(j.state);
  const pct = Math.round(overallProgress(j) * 100);
  const d = j.download;
  const detail =
    j.state === "downloading" && d
      ? [speed(d.speedBps), eta(d.etaSec)].filter(Boolean).join(" · ")
      : j.state === "done"
        ? mediaSummary(j)
        : "";
  return (
    <li className={`job ${j.state}`}>
      <div className="row">
        <span className="title" title={j.url}>
          {title(j)}
        </span>
        <span className="state">{STATE_LABEL[j.state]}</span>
      </div>
      {active &&
        (progressKnown(j) ? (
          <div className="bar">
            <div style={{ width: `${pct}%` }} />
          </div>
        ) : (
          <div className="bar unknown" title="진행률을 알 수 없습니다(구간 다운로드는 끝날 때 알려 줍니다)" />
        ))}
      {detail && (
        <div className="muted small">
          {detail}
        </div>
      )}
      {j.options.range && (
        <div className="muted small">
          구간 {formatTime(j.options.range.startSec)}–{formatTime(j.options.range.endSec)}
          {view.import?.inout === "set" ? " · In/Out 설정됨" : ""}
        </div>
      )}
      {[...(j.warnings ?? []), ...(view.import?.warnings ?? [])].map((w, i) => (
        <div key={i} className="warn small">
          {w}
        </div>
      ))}
      {j.failure && j.state !== "cancelled" && (
        <div className="error small" title={j.failure.detail}>
          {j.failure.message}
        </div>
      )}
      {view.import?.state === "failed" && <div className="error small">가져오기 실패: {view.import.error}</div>}
      <div className="row actions">
        {active && <button onClick={() => ctl.cancel(j.jobId)}>취소</button>}
        {j.state === "done" && view.import?.state !== "done" && (
          <button disabled={view.import?.state === "importing"} onClick={() => void ctl.importJob(j.jobId)}>
            {view.import?.state === "importing" ? "가져오는 중…" : "가져오기"}
          </button>
        )}
        {view.import?.state === "done" && (
          <span className="ok small">
            ✓ {view.import.bin?.join(" / ")}
            {view.import.placed === "ok" ? " · 타임라인에 놓음" : ""}
            {view.import.captions === "track" ? " · 캡션 트랙" : view.import.captions === "imported" ? " · 자막" : ""}
          </span>
        )}
        {j.file && host.revealFile && <button onClick={() => host.revealFile!(j.file!)}>폴더에서 보기</button>}
      </div>
    </li>
  );
};
