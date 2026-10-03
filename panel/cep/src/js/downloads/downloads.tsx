import { JobView, TERMINAL_STATES, bytes, formatTime, title } from "@birdcage/core";
import { JobsAction, JobsSnapshot } from "../jobs-channel";
import { phaseOf } from "../main/job-progress";

export const DownloadsApp = ({ snapshot, action, openMain }: { snapshot?: JobsSnapshot; action: (a: JobsAction) => void; openMain: () => void }) => {
  const jobs = snapshot?.jobs ?? [];
  const activeJobs = jobs.filter((j) => !TERMINAL_STATES.includes(j.snap.state) || j.import?.state === "pending" || j.import?.state === "importing");
  const completed = jobs.filter((j) => j.snap.state === "done" && j.import?.state !== "pending" && j.import?.state !== "importing");
  const stopped = jobs.filter((j) => (j.snap.state === "failed" || j.snap.state === "cancelled") && !activeJobs.includes(j));
  const live = new Set(snapshot?.liveJobIds ?? jobs.map((view) => view.snap.jobId));
  return (
    <div className="app downloads-app">
      <header className="row header">
        <strong>다운로드 목록</strong>
        {activeJobs.length > 0 && <span className="tag accent">진행 중 {activeJobs.length}</span>}
        <span className="spacer" />
        {completed.length > 0 && <button onClick={() => action({ type: "clearFinished" })}>완료 지우기</button>}
      </header>
      {!snapshot ? (
        <div className="notice downloads-empty">
          <div>Birdcage 작업 창에 연결하는 중입니다.</div>
          <button onClick={openMain}>Birdcage 열기</button>
        </div>
      ) : jobs.length === 0 ? (
        <div className="downloads-empty muted">요청한 다운로드가 없습니다.</div>
      ) : (
        <div className="download-list">
          {activeJobs.length > 0 && <DownloadSection title="현재 다운로드" count={activeJobs.length} jobs={activeJobs} live={live} action={action} />}
          {stopped.length > 0 && <DownloadSection title="최근 상태" count={stopped.length} jobs={stopped} live={live} action={action} />}
          {completed.length > 0 && <DownloadSection title="완료 기록" count={completed.length} jobs={completed} live={live} action={action} completed />}
        </div>
      )}
      <footer className="row footer muted small">
        <span title={snapshot?.libraryDir}>저장 위치: {snapshot?.libraryDir ?? "-"}</span>
      </footer>
    </div>
  );
};

const DownloadSection = ({ title: heading, count, jobs, live, action, completed = false }: { title: string; count: number; jobs: JobView[]; live: Set<string>; action: (a: JobsAction) => void; completed?: boolean }) => (
  <section className="download-section">
    <h2 className="download-section-title small">
      <span>{heading}</span>
      <span className="section-count">{count}</span>
    </h2>
    <ul className="jobs">
      {jobs.map((view) => <DownloadJob key={view.snap.jobId} view={view} live={live.has(view.snap.jobId)} action={action} completed={completed} />)}
    </ul>
  </section>
);

const DownloadJob = ({ view, live, action, completed }: { view: JobView; live: boolean; action: (a: JobsAction) => void; completed: boolean }) => {
  const j = view.snap;
  const p = phaseOf(view);
  const details = jobDetails(view, p.detail, completed);
  const statusLabel = completed ? "다운로드 완료" : j.state === "failed" ? "실패" : j.state === "cancelled" ? "취소됨" : p.label;
  return (
    <li className={`job download-job ${j.state}`}>
      <div className="row job-head">
        <span className="title" title={j.url}>{title(j)}</span>
        <span className={`state ${completed ? "done" : p.tone}`}>{statusLabel}</span>
      </div>
      {p.tone === "busy" && (
        <div className={`bar${p.fraction === undefined ? " unknown" : ""}`}>
          {p.fraction !== undefined && <div style={{ width: `${Math.round(p.fraction * 100)}%` }} />}
        </div>
      )}
      <div className="muted small job-detail">{details}</div>
      {[...(j.warnings ?? []), ...(view.import?.warnings ?? [])].map((warning, i) => <div key={i} className="warn small">{warning}</div>)}
      {j.failure && j.state !== "cancelled" && <div className="error small" title={j.failure.detail}>{j.failure.message}</div>}
      {view.import?.state === "failed" && <div className="error small">가져오기 실패: {view.import.error}</div>}
      <div className="row actions">
        {p.tone === "busy" && j.state !== "done" && <button onClick={() => action({ type: "cancel", jobId: j.jobId })}>취소</button>}
        {live && j.state === "done" && view.import?.state !== "done" && view.import?.state !== "importing" && <button onClick={() => action({ type: "import", jobId: j.jobId })}>{view.import?.state === "failed" ? "다시 넣기" : "가져오기"}</button>}
        {j.file && <button onClick={() => action({ type: "reveal", jobId: j.jobId })}>폴더에서 보기</button>}
      </div>
    </li>
  );
};

const jobDetails = (view: JobView, progressDetail: string | undefined, completed: boolean): string => {
  const j = view.snap;
  const range = j.options.range;
  const video = j.media?.video;
  return [
    range ? `구간 ${formatTime(range.startSec)}–${formatTime(range.endSec)}` : "",
    completed ? `완료 ${completedAt(j.updatedAt)}` : "",
    completed && video?.width && video?.height ? `${video.width}×${video.height}` : "",
    completed && j.media?.sizeBytes ? bytes(j.media.sizeBytes) : "",
    completed ? "" : progressDetail,
  ].filter(Boolean).join(" · ");
};

const completedAt = (raw: string): string => {
  const date = new Date(raw);
  if (!Number.isFinite(date.getTime())) return "";
  const two = (value: number) => String(value).padStart(2, "0");
  return `${date.getFullYear()}.${two(date.getMonth() + 1)}.${two(date.getDate())} ${two(date.getHours())}:${two(date.getMinutes())}`;
};
