import type { JobView } from "@birdcage/core";
import { fs, node_process, os, path } from "./lib/cep/node";

const HISTORY_KEY = "birdcage.downloadHistory.v1";
const HISTORY_LIMIT = 100;
const HISTORY_FILE = "download-history.json";

type HistoryStore = Pick<Storage, "getItem" | "setItem" | "removeItem">;

const browserStorage = (): HistoryStore | undefined => {
  try {
    return localStorage;
  } catch {
    return undefined;
  }
};

const birdcageDataDir = (): string => {
  const home = os.homedir();
  const platform = os.platform();
  if (platform === "darwin") return path.join(home, "Library", "Application Support", "Birdcage");
  if (platform === "win32") {
    const localAppData = node_process.env?.LOCALAPPDATA;
    return path.join(localAppData || path.join(home, "AppData", "Local"), "Birdcage");
  }
  const xdgDataHome = node_process.env?.XDG_DATA_HOME;
  return xdgDataHome ? path.join(xdgDataHome, "birdcage") : path.join(home, ".local", "share", "birdcage");
};

/** 두 CEP 패널이 함께 쓰는 원자적 파일 저장소. 경로 인자는 실제 파일 I/O 검증에도 사용한다. */
export const createFileHistoryStore = (file: string): HistoryStore => {
  return {
    getItem: () => {
      try {
        return fs.readFileSync(file, "utf8");
      } catch {
        return null;
      }
    },
    setItem: (_key, value) => {
      const dir = path.dirname(file);
      const temporary = `${file}.${node_process.pid || "cep"}.${Date.now()}.${Math.random().toString(16).slice(2)}.tmp`;
      fs.mkdirSync(dir, { recursive: true });
      fs.writeFileSync(temporary, value, "utf8");
      try {
        fs.renameSync(temporary, file);
      } catch {
        // 일부 Windows 파일시스템은 기존 파일 위 rename을 거부하므로 덮어쓰기로 폴백한다.
        try {
          fs.copyFileSync(temporary, file);
          fs.unlinkSync(temporary);
        } catch (error) {
          try {
            fs.unlinkSync(temporary);
          } catch {
            /* 임시 파일 정리 실패보다 원래 쓰기 오류를 우선한다. */
          }
          throw error;
        }
      }
    },
    // 빈 파일을 남겨 다른 CEP 패널의 과거 localStorage가 다시 이관되지 않게 한다.
    removeItem: () => {
      fs.mkdirSync(path.dirname(file), { recursive: true });
      fs.writeFileSync(file, "[]", "utf8");
    },
  };
};

/** CEP 확장 ID가 달라도 같은 완료 기록을 읽도록 Helper 데이터 폴더의 파일을 사용한다. */
const sharedFileStorage = (): HistoryStore | undefined => {
  if (typeof window === "undefined" || typeof window.cep === "undefined" || typeof fs.readFileSync !== "function") return undefined;
  return createFileHistoryStore(path.join(birdcageDataDir(), HISTORY_FILE));
};

const storage = (store?: HistoryStore): HistoryStore | undefined => {
  if (store) return store;
  const shared = sharedFileStorage();
  if (shared) {
    // 0.1.1까지의 메인 패널 기록을 최초 한 번 공용 파일로 옮긴다.
    if (shared.getItem(HISTORY_KEY) === null) {
      const legacy = browserStorage();
      const value = legacy?.getItem(HISTORY_KEY);
      if (value) {
        try {
          shared.setItem(HISTORY_KEY, value);
          legacy?.removeItem(HISTORY_KEY);
        } catch {
          return legacy;
        }
      }
    }
    return shared;
  }
  return browserStorage();
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
