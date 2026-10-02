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
  speed,
  title,
} from "@birdcage/core";
import { subscribeBackgroundColor } from "../lib/utils/bolt";
import { Composer } from "./composer";
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
  | "submitDraft"
  | "retryConnection"
  | "installTools"
  | "updateSettings"
  | "clearFinished"
  | "cancel"
  | "importJob"
>;

export type PanelHost = Pick<HostAdapter, "revealFile">;

const HEIGHTS = [360, 480, 720, 1080, 1440, 2160];

const CONNECTION_LABEL = {
  idle: "대기",
  starting: "Helper 시작 중…",
  connected: "연결됨",
  reconnecting: "다시 연결 중…",
  failed: "연결 실패",
} as const;

export const App = ({ controller: ctl, host }: { controller: PanelController; host: PanelHost }) => {
  const s = useSyncExternalStore(
    (cb) => ctl.subscribe(cb),
    () => ctl.state,
  );
  const [bg, setBg] = useState("#232323");

  useEffect(() => {
    if (window.cep) subscribeBackgroundColor(setBg);
    void ctl.start();
  }, [ctl]);

  const missing = s.connection === "connected" ? ctl.missingTools() : [];

  return (
    <div className="app" style={{ backgroundColor: bg }}>
      <header className="row header">
        <strong>Birdcage</strong>
        <span className={`badge ${s.connection}`}>{CONNECTION_LABEL[s.connection]}</span>
        {s.helper && (
          <span className="muted small" data-visual-dynamic>
            helper {s.helper.helperVersion} · {s.helper.profile === "modern" ? "Premiere 26+" : "호환 모드"}
          </span>
        )}
        {s.connection === "failed" && (
          <button onClick={() => ctl.retryConnection()}>다시 시도</button>
        )}
      </header>
      {s.error && s.connection !== "connected" && <pre className="error">{s.error}</pre>}

      {missing.length > 0 && (
        <div className="notice">
          <div>필요한 도구가 없습니다: {missing.join(", ")}</div>
          <button disabled={s.toolsInstalling} onClick={() => ctl.installTools()}>
            {s.toolsInstalling ? "설치 중…" : "도구 설치"}
          </button>
          {s.toolsError && <pre className="error">{s.toolsError}</pre>}
        </div>
      )}

      <Composer s={s} ctl={ctl} />

      <div className="row options muted small">
        <label>
          기본 화질
          <select
            value={s.settings.maxHeight}
            onChange={(e) => ctl.updateSettings({ maxHeight: Number(e.target.value) })}
          >
            {HEIGHTS.map((h) => (
              <option key={h} value={h}>
                {h}p
              </option>
            ))}
          </select>
        </label>
        <label>
          <input
            type="checkbox"
            checked={s.settings.autoImport}
            onChange={(e) => ctl.updateSettings({ autoImport: e.target.checked })}
          />
          완료 시 프로젝트로 가져오기
        </label>
      </div>

      <ul className="jobs">
        {s.jobs.map((j) => (
          <JobItem key={j.snap.jobId} view={j} controller={ctl} host={host} />
        ))}
      </ul>

      <footer className="row footer muted small">
        <span title={s.helper?.libraryDir} data-visual-dynamic>
          저장 위치: {s.helper?.libraryDir ?? "-"}
        </span>
        {s.jobs.some((j) => TERMINAL_STATES.includes(j.snap.state)) && (
          <button onClick={() => ctl.clearFinished()}>완료 항목 지우기</button>
        )}
      </footer>
    </div>
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
      {active && (
        <div className="bar">
          <div style={{ width: `${pct}%` }} />
        </div>
      )}
      {detail && (
        <div className="muted small" data-visual-dynamic>
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
            {view.import?.state === "importing" ? "가져오는 중…" : "프로젝트로 가져오기"}
          </button>
        )}
        {view.import?.state === "done" && <span className="ok small">✓ {view.import.bin?.join(" / ")}</span>}
        {j.file && host.revealFile && <button onClick={() => host.revealFile!(j.file!)}>폴더에서 보기</button>}
      </div>
    </li>
  );
};
