import { JobView, TERMINAL_STATES, formatTime, mediaSummary, title } from "@birdcage/core";
import { JobsAction, JobsSnapshot } from "../jobs-channel";
import { phaseOf } from "../main/job-progress";

export const DownloadsApp = ({ snapshot, action, openMain }: { snapshot?: JobsSnapshot; action: (a: JobsAction) => void; openMain: () => void }) => {
  const jobs = snapshot?.jobs ?? [];
  const active = jobs.filter((j) => !TERMINAL_STATES.includes(j.snap.state) || j.import?.state === "pending" || j.import?.state === "importing").length;
  return (
    <div className="app downloads-app">
      <header className="row header">
        <strong>다운로드</strong>
        {active > 0 && <span className="tag accent">진행 중 {active}</span>}
        <span className="spacer" />
        {jobs.some((j) => TERMINAL_STATES.includes(j.snap.state)) && <button onClick={() => action({ type: "clearFinished" })}>완료 지우기</button>}
      </header>
      {!snapshot ? (
        <div className="notice downloads-empty">
          <div>Birdcage 작업 창에 연결하는 중입니다.</div>
          <button onClick={openMain}>Birdcage 열기</button>
        </div>
      ) : jobs.length === 0 ? (
        <div className="downloads-empty muted">요청한 다운로드가 없습니다.</div>
      ) : (
        <ul className="jobs download-list">
          {jobs.map((view) => (
            <DownloadJob key={view.snap.jobId} view={view} action={action} />
          ))}
        </ul>
      )}
      <footer className="row footer muted small">
        <span title={snapshot?.libraryDir}>저장 위치: {snapshot?.libraryDir ?? "-"}</span>
      </footer>
    </div>
  );
};

const DownloadJob = ({ view, action }: { view: JobView; action: (a: JobsAction) => void }) => {
  const j = view.snap;
  const p = phaseOf(view);
  const range = j.options.range;
  return (
    <li className={`job download-job ${j.state}`}>
      <div className="row">
        <span className="title" title={j.url}>{title(j)}</span>
        <span className={`state ${p.tone}`}>{p.label}</span>
      </div>
      {p.tone === "busy" && (
        <div className={`bar${p.fraction === undefined ? " unknown" : ""}`}>
          {p.fraction !== undefined && <div style={{ width: `${Math.round(p.fraction * 100)}%` }} />}
        </div>
      )}
      <div className="muted small job-detail">
        {range && <>구간 {formatTime(range.startSec)}–{formatTime(range.endSec)}</>}
        {j.state === "done" && <>{range ? " · " : ""}{mediaSummary(j)}</>}
        {p.detail && <>{range || j.state === "done" ? " · " : ""}{p.detail}</>}
      </div>
      {[...(j.warnings ?? []), ...(view.import?.warnings ?? [])].map((warning, i) => <div key={i} className="warn small">{warning}</div>)}
      {j.failure && <div className="error small" title={j.failure.detail}>{j.failure.message}</div>}
      {view.import?.state === "failed" && <div className="error small">가져오기 실패: {view.import.error}</div>}
      <div className="row actions">
        {p.tone === "busy" && j.state !== "done" && <button onClick={() => action({ type: "cancel", jobId: j.jobId })}>취소</button>}
        {j.state === "done" && view.import?.state !== "done" && view.import?.state !== "importing" && <button onClick={() => action({ type: "import", jobId: j.jobId })}>{view.import?.state === "failed" ? "다시 넣기" : "가져오기"}</button>}
        {j.file && <button onClick={() => action({ type: "reveal", jobId: j.jobId })}>폴더에서 보기</button>}
      </div>
    </li>
  );
};
