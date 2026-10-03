import type { JobView } from "@birdcage/core";

const HISTORY_KEY = "birdcage.downloadHistory.v1";
const HISTORY_LIMIT = 100;

type HistoryStore = Pick<Storage, "getItem" | "setItem" | "removeItem">;

const storage = (store?: HistoryStore): HistoryStore | undefined => {
  if (store) return store;
  try {
    return localStorage;
  } catch {
    return undefined;
  }
};

const isCompleted = (view: unknown): view is JobView => {
  if (!view || typeof view !== "object") return false;
  const snap = (view as { snap?: unknown }).snap;
  if (!snap || typeof snap !== "object") return false;
  const value = snap as { jobId?: unknown; url?: unknown; state?: unknown; options?: unknown; createdAt?: unknown; updatedAt?: unknown };
  return value.state === "done" && typeof value.jobId === "string" && typeof value.url === "string" && !!value.options && typeof value.options === "object" && typeof value.createdAt === "string" && typeof value.updatedAt === "string";
};

/** 완료된 다운로드 기록. Helper가 종료돼도 남고, 손상된 값은 조용히 버린다. */
export function loadDownloadHistory(store?: HistoryStore): JobView[] {
  const target = storage(store);
  if (!target) return [];
  try {
    const parsed = JSON.parse(target.getItem(HISTORY_KEY) || "[]");
    return Array.isArray(parsed) ? parsed.filter(isCompleted).slice(0, HISTORY_LIMIT) : [];
  } catch {
    return [];
  }
}

/** 같은 작업은 최신 상태로 덮고 최근 100개만 남긴다. */
export function mergeDownloadHistory(previous: JobView[], jobs: JobView[]): JobView[] {
  const byID = new Map<string, JobView>();
  for (const view of previous) {
    if (isCompleted(view)) byID.set(view.snap.jobId, view);
  }
  for (const view of jobs) {
    if (isCompleted(view)) byID.set(view.snap.jobId, view);
  }
  return [...byID.values()]
    .sort((a, b) => b.snap.updatedAt.localeCompare(a.snap.updatedAt))
    .slice(0, HISTORY_LIMIT);
}

export function recordDownloadHistory(jobs: JobView[], store?: HistoryStore): JobView[] {
  const target = storage(store);
  const previous = loadDownloadHistory(target);
  const next = mergeDownloadHistory(previous, jobs);
  if (!target) return next;
  try {
    const encoded = JSON.stringify(next);
    if (target.getItem(HISTORY_KEY) !== encoded) target.setItem(HISTORY_KEY, encoded);
  } catch {
    /* 기록 실패가 다운로드를 방해하면 안 된다. */
  }
  return next;
}

export function clearDownloadHistory(store?: HistoryStore): void {
  try {
    storage(store)?.removeItem(HISTORY_KEY);
  } catch {
    /* 읽기 전용·용량 제한 환경에서는 현재 세션만 계속 쓴다. */
  }
}

/** 현재 Helper 작업이 우선이고, 거기에 없는 과거 완료 기록을 뒤에 붙인다. */
export function withDownloadHistory(jobs: JobView[], history: JobView[]): JobView[] {
  const live = new Set(jobs.map((view) => view.snap.jobId));
  return jobs.concat(history.filter((view) => !live.has(view.snap.jobId)));
}
