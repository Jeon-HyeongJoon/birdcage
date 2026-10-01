// Controller: panel logic shared by every host.
// Helper 수명(시작·재연결), 작업 목록 상태, 완료 시 자동 임포트를 담당한다. UI는 state만 그린다.
import { HelperClient, HelperEndpoint, WebSocketFactory } from "./client";
import type { HostAdapter } from "./host";
import type { HelloOK, JobSnapshot, ToolStatus } from "./protocol";
import { TERMINAL_STATES } from "./protocol";

export type ConnectionState = "idle" | "starting" | "connected" | "reconnecting" | "failed";

export interface ImportState {
  state: "pending" | "importing" | "done" | "failed";
  error?: string;
  bin?: string[];
}

export interface JobView {
  snap: JobSnapshot;
  import?: ImportState;
}

export interface Settings {
  autoImport: boolean;
  maxHeight: number;
}

export interface State {
  connection: ConnectionState;
  error?: string;
  helper?: Pick<HelloOK, "helperVersion" | "profile" | "libraryDir">;
  endpoint?: HelperEndpoint;
  tools: ToolStatus[];
  toolsInstalling: boolean;
  toolsError?: string;
  jobs: JobView[]; // 최신이 앞
  settings: Settings;
}

export interface ControllerOptions {
  host: HostAdapter;
  clientName: string;
  clientVersion: string;
  ws?: WebSocketFactory;
  settings?: Partial<Settings>;
  /** Reconnect delays in ms; after the last one the controller gives up (state "failed"). */
  backoff?: number[];
  setTimer?: (fn: () => void, ms: number) => unknown;
}

const REQUIRED_TOOLS = ["yt-dlp", "ffmpeg", "ffprobe"];

export class Controller {
  private _state: State;
  private subs = new Set<(s: State) => void>();
  private client: HelperClient | null = null;
  private attempt = 0;
  private disposed = false;
  private starting = false;

  constructor(private opts: ControllerOptions) {
    this._state = {
      connection: "idle",
      tools: [],
      toolsInstalling: false,
      jobs: [],
      settings: { autoImport: true, maxHeight: 1080, ...opts.settings },
    };
  }

  get state(): State {
    return this._state;
  }

  subscribe(fn: (s: State) => void): () => void {
    this.subs.add(fn);
    return () => this.subs.delete(fn);
  }

  /** Tools the helper needs that are missing. */
  missingTools(): string[] {
    const have = new Set(this._state.tools.filter((t) => !t.error && t.path).map((t) => t.name as string));
    const missing = REQUIRED_TOOLS.filter((n) => !have.has(n));
    if (!have.has("deno") && !have.has("node")) missing.push("deno");
    return missing;
  }

  async start(): Promise<void> {
    // 중복 호출(React StrictMode 이중 effect, 재연결 타이머 겹침)에도 Helper/연결을 하나만 둔다.
    if (this.disposed || this.starting || this._state.connection === "connected") return;
    this.starting = true;
    this.set({ connection: this.attempt === 0 ? "starting" : "reconnecting", error: undefined });
    try {
      const endpoint = await this.opts.host.startHelper();
      const client = new HelperClient({
        endpoint,
        ws: this.opts.ws,
        hello: { client: this.opts.clientName, clientVersion: this.opts.clientVersion, host: this.opts.host.info() },
        onClose: (reason) => this.onClose(client, reason),
      });
      this.wire(client);
      const ok = await client.connect();
      this.client = client;
      this.attempt = 0;
      const known = new Map(this._state.jobs.map((j) => [j.snap.jobId, j]));
      this.set({
        connection: "connected",
        endpoint,
        helper: { helperVersion: ok.helperVersion, profile: ok.profile, libraryDir: ok.libraryDir },
        tools: ok.tools,
        // Helper가 다시 떴으면 그쪽 목록이 기준. 이전 세션 작업(임포트 상태 포함)은 뒤에 유지.
        jobs: mergeJobs(ok.jobs.map((s) => ({ snap: s, import: known.get(s.jobId)?.import })), this._state.jobs),
      });
    } catch (e) {
      this.starting = false;
      this.scheduleReconnect((e as Error).message);
      return;
    }
    this.starting = false;
  }

  async submit(url: string): Promise<void> {
    const c = this.requireClient();
    await c.createJob(url.trim(), this._state.settings.maxHeight);
  }

  cancel(jobId: string): void {
    this.client?.cancelJob(jobId);
  }

  installTools(channel?: "stable" | "nightly"): void {
    this.requireClient().ensureTools(channel);
    this.set({ toolsInstalling: true, toolsError: undefined });
  }

  /** Imports a finished job's file (manual button or auto-import). */
  async importJob(jobId: string): Promise<void> {
    const view = this._state.jobs.find((j) => j.snap.jobId === jobId);
    if (!view || view.snap.state !== "done" || !view.snap.file) return;
    if (view.import?.state === "importing") return;
    const bin = ["Birdcage", view.snap.info?.extractor || "Other"];
    this.patchJob(jobId, { import: { state: "importing", bin } });
    try {
      const res = await this.opts.host.importFile(view.snap.file, bin);
      this.patchJob(jobId, { import: res.ok ? { state: "done", bin: res.bin } : { state: "failed", error: res.error, bin } });
    } catch (e) {
      this.patchJob(jobId, { import: { state: "failed", error: (e as Error).message, bin } });
    }
  }

  updateSettings(s: Partial<Settings>): void {
    this.set({ settings: { ...this._state.settings, ...s } });
  }

  /** Clears finished jobs from the list (panel-side only). */
  clearFinished(): void {
    this.set({ jobs: this._state.jobs.filter((j) => !TERMINAL_STATES.includes(j.snap.state)) });
  }

  retryConnection(): void {
    this.attempt = 0;
    void this.start();
  }

  dispose(): void {
    this.disposed = true;
    this.client?.close();
    this.opts.host.stopHelper();
    this.subs.clear();
  }

  private wire(client: HelperClient): void {
    const onJob = (job: JobSnapshot) => this.upsertJob(job);
    client.on("job.update", (d) => onJob(d.job));
    client.on("job.failed", (d) => onJob(d.job));
    client.on("job.done", (d) => {
      const before = this._state.jobs.find((j) => j.snap.jobId === d.job.jobId);
      onJob(d.job);
      if (this._state.settings.autoImport && !before?.import && d.job.file) {
        this.patchJob(d.job.jobId, { import: { state: "pending" } });
        void this.importJob(d.job.jobId);
      }
    });
    client.on("tools.status", (d) => this.set({ tools: d.tools, toolsInstalling: d.installing, toolsError: d.error }));
  }

  private onClose(client: HelperClient, reason: string): void {
    if (this.disposed || (this.client && this.client !== client)) return;
    this.client = null;
    this.scheduleReconnect(reason);
  }

  private scheduleReconnect(reason: string): void {
    if (this.disposed) return;
    const delays = this.opts.backoff ?? [500, 1000, 2000, 5000, 10000];
    if (this.attempt >= delays.length) {
      this.set({ connection: "failed", error: reason });
      return;
    }
    const delay = delays[this.attempt++];
    this.set({ connection: "reconnecting", error: reason });
    const timer = this.opts.setTimer ?? ((fn, ms) => setTimeout(fn, ms));
    timer(() => void this.start(), delay);
  }

  private requireClient(): HelperClient {
    if (!this.client || !this.client.connected) throw new Error("Helper에 연결되지 않았습니다.");
    return this.client;
  }

  private upsertJob(job: JobSnapshot): void {
    const jobs = this._state.jobs.slice();
    const i = jobs.findIndex((j) => j.snap.jobId === job.jobId);
    if (i >= 0) jobs[i] = { ...jobs[i], snap: job };
    else jobs.unshift({ snap: job });
    this.set({ jobs });
  }

  private patchJob(jobId: string, patch: Partial<JobView>): void {
    this.set({ jobs: this._state.jobs.map((j) => (j.snap.jobId === jobId ? { ...j, ...patch } : j)) });
  }

  private set(patch: Partial<State>): void {
    this._state = { ...this._state, ...patch };
    for (const fn of this.subs) fn(this._state);
  }
}

function mergeJobs(fromHelper: JobView[], previous: JobView[]): JobView[] {
  const ids = new Set(fromHelper.map((j) => j.snap.jobId));
  const sorted = fromHelper.slice().sort((a, b) => b.snap.createdAt.localeCompare(a.snap.createdAt));
  return sorted.concat(previous.filter((j) => !ids.has(j.snap.jobId)));
}
