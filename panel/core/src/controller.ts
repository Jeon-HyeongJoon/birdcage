// Controller: panel logic shared by every host.
// Helper 수명(시작·재연결), 작업 목록 상태, 완료 시 자동 임포트를 담당한다. UI는 state만 그린다.
import { HelperClient, HelperEndpoint, WebSocketFactory } from "./client";
import { recommendVariant, SequenceInfo, targetRes } from "./choice";
import { startFromURL } from "./time";
import type { HostAdapter, ImportOptions } from "./host";
import type { Failure, HelloOK, JobSnapshot, PreviewState, RelatedItem, RelatedSource, SourceInfo, TimeRange, ToolStatus } from "./protocol";
import { TERMINAL_STATES } from "./protocol";

export type ConnectionState = "idle" | "starting" | "connected" | "reconnecting" | "failed";

export interface ImportState {
  state: "pending" | "importing" | "done" | "failed";
  error?: string;
  bin?: string[];
  inout?: "set" | "unverified" | "unsupported";
  placed?: "ok" | "source" | "unverified" | "no_sequence" | "no_free_track" | "failed";
  captions?: "imported" | "track" | "unsupported" | "before_start" | "failed";
  warnings?: string[];
}

export interface JobView {
  snap: JobSnapshot;
  import?: ImportState;
}

export interface Settings {
  autoImport: boolean;
  maxHeight: number;
}

/** Import options for a finished job: In/Out, subtitles, and always a new empty track at the playhead. */
export function importOptions(snap: JobSnapshot): ImportOptions {
  const o: ImportOptions = { place: "newTrack", hasVideo: !!snap.media?.video };
  if (snap.clip) o.clip = snap.clip;
  if (snap.subtitleFile) o.subtitle = snap.subtitleFile;
  return o;
}

export type ProbeStatus = "idle" | "probing" | "ready" | "failed";

/** The link being prepared for download (조회 → 선택 → 받기). */
export interface Draft {
  url: string;
  status: ProbeStatus;
  /** A failed link is being checked again; keep the failure UI until the answer arrives. */
  checking?: boolean;
  probeId?: string;
  info?: SourceInfo;
  failure?: Failure | { kind: "request"; message: string };
  /** Chosen variant; undefined until the info arrives (then the recommendation). */
  formatId?: string;
  recommendedId?: string;
  /** Part of the source to download (seconds); off = whole video. */
  range: DraftRange;
  /** Best audio only, as WAV. */
  audioOnly?: boolean;
  /** Subtitle track to fetch ("lang" or "lang:auto"); undefined = none. */
  subtitle?: string;
  /** Staged preview (waveform, playable video, generated storyboard) for this probe. */
  preview?: PreviewState;
  /** Downloads started from this draft (UI feedback). */
  submitted?: number;
  /** Jobs started from this draft, oldest first. */
  jobIds?: string[];
  /** Browse list next to the preview (YouTube links only). */
  related?: RelatedView;
  /** The list item this link was opened from (shown while probing). */
  hint?: RelatedItem;
  /** Browse list the link came from ("related:youtube"), sent with probe and download for the fitness review. */
  via?: string;
  /** Restored from history: if the helper already dropped the probe, the preview fails and we probe again. */
  restored?: boolean;
}

/** One browse list tab. */
export interface RelatedListState {
  status: "loading" | "ready" | "failed";
  items: RelatedItem[];
  /** Source that filled the list; differs from the tab when the helper fell back. */
  source?: RelatedSource;
  error?: string;
}

export interface RelatedView {
  tab: RelatedSource;
  lists: Partial<Record<RelatedSource, RelatedListState>>;
}

/** YouTube search results shown in the browse list's 검색 tab; they survive opening a result and back(). */
export interface SearchState {
  query: string;
  status: "loading" | "ready" | "failed";
  items: RelatedItem[];
  /** Last fetched page (0-based). */
  page: number;
  more: boolean;
  loadingMore?: boolean;
  moreError?: string;
  error?: string;
}

/** Which list the browse panel shows. */
export type ListTab = "search" | "related";

/** A link left for another one; back() restores it with its choices. */
export interface HistoryEntry {
  draft: Draft;
  at: number;
}

/** Links with a browse list (YouTube: 시청 페이지 '다음 동영상', 같은 채널, 제목 검색). */
export function supportsRelated(info?: SourceInfo): boolean {
  return info?.extractor === "Youtube";
}

/** YouTube video ids already downloaded (탐색 목록의 "✓ 받음"). */
export function downloadedIds(jobs: JobView[]): Set<string> {
  const ids = new Set<string>();
  for (const j of jobs) if (j.snap.state === "done" && j.snap.info?.extractor === "Youtube" && j.snap.info.id) ids.add(j.snap.info.id);
  return ids;
}

const MAX_HISTORY = 20;
/** Reuse a remembered probe for this long (helper keeps probes 20 min). */
const PROBE_REUSE_MS = 15 * 60 * 1000;

export interface DraftRange {
  on: boolean;
  start?: number;
  end?: number;
}

/** Validates a draft range against the duration; returns the range to send or an error message. */
export function rangeToSend(r: DraftRange, durationSec?: number): { range?: TimeRange; error?: string } {
  if (!r.on) return {};
  const start = r.start ?? 0;
  const end = r.end ?? durationSec;
  if (end === undefined) return { error: "끝 시간을 입력하세요." };
  if (!(end > start)) return { error: "끝 시간은 시작보다 뒤여야 합니다." };
  if (durationSec && start >= durationSec) return { error: "시작 시간이 영상 길이를 넘습니다." };
  if (durationSec && start <= 0 && end >= durationSec) return {}; // 전체와 같다
  return { range: { startSec: start, endSec: durationSec ? Math.min(end, durationSec) : end } };
}

const EMPTY_DRAFT: Draft = { url: "", status: "idle", range: { on: false } };

/** Looks like a downloadable link (the helper validates for real). */
export function isLink(s: string): boolean {
  return /^https?:\/\/[^\s/]+\.[^\s]+/.test(s.trim());
}

export interface State {
  connection: ConnectionState;
  error?: string;
  helper?: Pick<HelloOK, "helperVersion" | "profile" | "libraryDir">;
  /** Effective destination shown to the user. Project-local when available, otherwise helper libraryDir. */
  outputDir?: string;
  endpoint?: HelperEndpoint;
  tools: ToolStatus[];
  toolsInstalling: boolean;
  toolsError?: string;
  jobs: JobView[]; // 최신이 앞
  settings: Settings;
  draft: Draft;
  /** Active sequence (undefined = not asked yet, null = none open). */
  sequence?: SequenceInfo | null;
  /** Links left for another one, oldest first ([← 이전 영상]). */
  history: HistoryEntry[];
  search?: SearchState;
  listTab: ListTab;
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
  /** Delay between typing/pasting a link and probing it. */
  probeDebounceMs?: number;
  now?: () => number;
}

const REQUIRED_TOOLS = ["yt-dlp", "ffmpeg", "ffprobe"];

/** Tools the helper needs that are missing or broken (a JS runtime: deno or node). */
export function missingTools(tools: ToolStatus[]): string[] {
  const have = new Set(tools.filter((t) => !t.error && t.path).map((t) => t.name as string));
  const missing = REQUIRED_TOOLS.filter((n) => !have.has(n));
  if (!have.has("deno") && !have.has("node")) missing.push("deno");
  return missing;
}

export class Controller {
  private _state: State;
  private subs = new Set<(s: State) => void>();
  private client: HelperClient | null = null;
  private attempt = 0;
  private disposed = false;
  private starting = false;
  private draftSeq = 0;
  private searchSeq = 0;


  constructor(private opts: ControllerOptions) {
    this._state = {
      connection: "idle",
      tools: [],
      toolsInstalling: false,
      jobs: [],
      settings: { autoImport: true, maxHeight: 1080, ...opts.settings },
      draft: EMPTY_DRAFT,
      history: [],
      listTab: "related",
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
    return missingTools(this._state.tools);
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
      void this.refreshOutputDir();
    } catch (e) {
      this.starting = false;
      this.scheduleReconnect((e as Error).message);
      return;
    }
    this.starting = false;
  }

  async submit(url: string): Promise<void> {
    const c = this.requireClient();
    const outputDir = await this.refreshOutputDir();
    await c.createJob(url.trim(), this._state.settings.maxHeight, { outputDir });
  }

  /** Updates the link field; a valid link is probed after a short pause (paste → info). */
  setUrl(url: string): void {
    const seq = ++this.draftSeq;
    const trimmed = url.trim();
    if (trimmed === this._state.draft.url.trim() && this._state.draft.status !== "idle") {
      this.set({ draft: { ...this._state.draft, url } });
      return;
    }
    this.pushHistory();
    this.closePreview();
    this.set({ draft: { ...EMPTY_DRAFT, url } });
    if (!isLink(trimmed) || this._state.connection !== "connected") return;
    const timer = this.opts.setTimer ?? ((fn, ms) => setTimeout(fn, ms));
    timer(() => {
      if (seq === this.draftSeq) void this.probeDraft();
    }, this.opts.probeDebounceMs ?? 400);
  }

  /** Probes the current link now (also used by "다시 조회"); keep re-applies a remembered draft's choices. */
  async probeDraft(keep?: Draft): Promise<void> {
    const cur = this._state.draft;
    const url = cur.url.trim();
    if (!isLink(url)) return;
    const seq = ++this.draftSeq;
    const retrying = cur.status === "failed";
    this.set({
      draft: retrying
        ? { ...cur, checking: true }
        : { ...EMPTY_DRAFT, url: cur.url, hint: cur.hint, via: cur.via, status: "probing" },
    });
    try {
      // 시퀀스는 조회와 함께 읽는다(추천이 현재 시퀀스를 따른다).
      const [res] = await Promise.all([
        this.requireClient().probe(url, this._state.settings.maxHeight, cur.via),
        this.refreshSequence(),
        this.refreshOutputDir(),
      ]);
      if (seq !== this.draftSeq) return; // 사용자가 그새 링크를 바꿨다
      if (res.failure || !res.info) {
        this.set({ draft: { ...this._state.draft, status: "failed", checking: false, failure: res.failure } });
        return;
      }
      const rec = recommendVariant(res.info.variants, targetRes(this._state.sequence, this._state.settings.maxHeight));
      // 공유 링크의 시작 시각(t=)이 있으면 그 지점부터의 구간으로 채운다.
      const t = startFromURL(url);
      const range: DraftRange = t !== undefined && (!res.info.durationSec || t < res.info.durationSec) ? { on: true, start: t, end: res.info.durationSec } : { on: false };
      const kept = keep && res.info.variants?.some((v) => v.formatId === keep.formatId) ? keep.formatId : undefined;
      this.set({
        draft: {
          ...this._state.draft,
          status: "ready",
          checking: false,
          failure: undefined,
          probeId: res.probeId,
          info: res.info,
          formatId: kept ?? rec?.formatId,
          recommendedId: rec?.formatId,
          range,
          ...(keep && { range: keep.range, audioOnly: keep.audioOnly, subtitle: keep.subtitle, related: keep.related, jobIds: keep.jobIds, submitted: keep.submitted, via: keep.via }),
        },
      });
      // 미리보기: 스토리보드는 이미 info에 있고, 파형 → 재생 가능한 비디오가 preview.state로 이어진다.
      if (res.probeId) this.client?.openPreview(res.probeId);
    } catch (e) {
      if (seq !== this.draftSeq) return;
      this.set({ draft: { ...this._state.draft, status: "failed", checking: false, failure: { kind: "request", message: (e as Error).message } } });
    }
  }

  /** Audio only (WAV) and subtitle choice ("lang" / "lang:auto" / undefined). */
  setExtras(x: { audioOnly?: boolean; subtitle?: string | null }): void {
    const d = { ...this._state.draft };
    if (x.audioOnly !== undefined) d.audioOnly = x.audioOnly;
    if (x.subtitle !== undefined) d.subtitle = x.subtitle ?? undefined;
    this.set({ draft: d });
  }

  /** Updates the range (seconds); pass on=false to download the whole video. */
  setRange(r: Partial<DraftRange>): void {
    this.set({ draft: { ...this._state.draft, range: { ...this._state.draft.range, ...r } } });
  }

  selectVariant(formatId: string): void {
    if (!this._state.draft.info?.variants?.some((v) => v.formatId === formatId)) return;
    this.set({ draft: { ...this._state.draft, formatId } });
  }

  /** Downloads the draft with the chosen options and clears the field. */
  async submitDraft(): Promise<void> {
    const d = this._state.draft;
    const url = d.url.trim();
    if (!isLink(url)) return;
    const c = this.requireClient();
    const { range, error } = rangeToSend(d.range, d.info?.durationSec);
    if (error) throw new Error(error);
    const subtitles = d.subtitle ? { lang: d.subtitle.replace(/:auto$/, ""), auto: d.subtitle.endsWith(":auto") || undefined } : undefined;
    const outputDir = await this.refreshOutputDir();
    const common = { range, audioOnly: d.audioOnly || undefined, subtitles, via: d.via, outputDir };
    const extra = d.status === "ready" ? { probeId: d.probeId, formatId: d.audioOnly ? undefined : d.formatId, ...common } : common;
    const variant = d.info?.variants?.find((v) => v.formatId === d.formatId);
    const jobId = await c.createJob(url, variant?.res ?? this._state.settings.maxHeight, JSON.parse(JSON.stringify(extra)));
    // 찾아보기 흐름: 받은 뒤에도 미리보기를 유지해 같은 영상의 다른 부분을 이어서 고를 수 있다.
    // 이 링크에서 시작한 작업은 미리보기 아래에 크게 보여 준다(받는 중인지 바로 알 수 있게).
    const d2 = this._state.draft;
    this.set({ draft: { ...d2, submitted: (d2.submitted ?? 0) + 1, jobIds: [...(d2.jobIds ?? []), jobId] } });
  }

  /** Clears the link field and closes the preview. */
  clearDraft(): void {
    this.draftSeq++;
    this.pushHistory();
    this.closePreview();
    this.set({ draft: EMPTY_DRAFT });
  }

  /** Loads a browse list tab for the current link (once per tab; a failed tab is fetched again). */
  loadRelated(tab?: RelatedSource): void {
    const d = this._state.draft;
    if (d.status !== "ready" || !d.probeId || !supportsRelated(d.info)) return;
    const view: RelatedView = d.related ?? { tab: "youtube", lists: {} };
    const t = tab ?? view.tab;
    if (tab && this._state.listTab !== "related") this.set({ listTab: "related" }); // 사용자가 추천 탭을 눌렀다
    const existing = view.lists[t];
    if (t !== view.tab) this.set({ draft: { ...d, related: { ...view, tab: t } } });
    if (existing && existing.status !== "failed") return;
    const probeId = d.probeId;
    const put = (list: RelatedListState) => {
      const cur = this._state.draft;
      if (cur.probeId !== probeId) return; // 그새 다른 링크로 옮겼다
      const v = cur.related ?? { tab: t, lists: {} };
      this.set({ draft: { ...cur, related: { ...v, lists: { ...v.lists, [t]: list } } } });
    };
    if (!this.client?.connected) {
      put({ status: "failed", items: [], error: "Helper에 연결되지 않았습니다." });
      return;
    }
    put({ status: "loading", items: [] });
    this.client.related(probeId, t).then(
      (res) => put(res.error ? { status: "failed", items: [], error: res.error } : { status: "ready", items: res.items, source: res.source }),
      (e) => put({ status: "failed", items: [], error: (e as Error).message }),
    );
  }

  /** Opens a browse-list or search item; the current link goes to history so back() can return to it. */
  openRelated(item: RelatedItem, via?: string): void {
    const from = this._state.draft.related?.tab ?? "youtube";
    this.draftSeq++;
    this.pushHistory();
    this.closePreview();
    this.set({ draft: { ...EMPTY_DRAFT, url: item.url, hint: item, via: via ?? `related:${from}` } });
    if (this._state.connection === "connected") void this.probeDraft();
  }

  /** Returns to the previous link with its range, quality, extras and browse list. */
  back(): void {
    const h = this._state.history;
    const prev = h[h.length - 1];
    if (!prev) return;
    this.draftSeq++;
    this.closePreview();
    this.set({ history: h.slice(0, -1) });
    const p = prev.draft;
    // 조회 결과가 Helper에 남아 있을 시간이면 그대로 되살린다(미리보기는 디스크 캐시라 바로 뜬다).
    if (p.status === "ready" && p.probeId && this.now() - prev.at < PROBE_REUSE_MS) {
      this.set({ draft: { ...p, preview: undefined, restored: true } });
      this.client?.openPreview(p.probeId);
      return;
    }
    const hint = p.info ? { id: p.info.id, url: p.url, title: p.info.title, thumbnail: p.info.thumbnail } : undefined;
    this.set({ draft: { ...EMPTY_DRAFT, url: p.url, via: p.via, hint } });
    if (this._state.connection === "connected") void this.probeDraft(p);
  }

  /** Searches YouTube; results show in the browse list's 검색 tab (the preview stays). */
  search(query: string): void {
    const q = query.trim().replace(/\s+/g, " ");
    if (!q) return;
    const seq = ++this.searchSeq;
    this.set({ search: { query: q, status: "loading", items: [], page: 0, more: false }, listTab: "search" });
    const done = (patch: Partial<SearchState>) => {
      if (seq === this.searchSeq && this._state.search) this.set({ search: { ...this._state.search, ...patch } });
    };
    if (!this.client?.connected) {
      done({ status: "failed", error: "Helper에 연결되지 않았습니다." });
      return;
    }
    this.client.search(q, 0).then(
      (res) => done(res.error ? { status: "failed", error: res.error } : { status: "ready", query: res.query || q, items: res.items, more: !!res.more }),
      (e) => done({ status: "failed", error: (e as Error).message }),
    );
  }

  /** Fetches the next page of the current search ("더 보기"). */
  searchMore(): void {
    const s = this._state.search;
    if (!s || s.status !== "ready" || !s.more || s.loadingMore || !this.client?.connected) return;
    const seq = this.searchSeq;
    const page = s.page + 1;
    this.set({ search: { ...s, loadingMore: true, moreError: undefined } });
    const done = (patch: (cur: SearchState) => Partial<SearchState>) => {
      const cur = this._state.search;
      if (seq === this.searchSeq && cur) this.set({ search: { ...cur, loadingMore: false, ...patch(cur) } });
    };
    this.client.search(s.query, page).then(
      (res) =>
        done((cur) => {
          if (res.error) return { moreError: res.error };
          const seen = new Set(cur.items.map((i) => i.id));
          return { items: cur.items.concat(res.items.filter((i) => !seen.has(i.id))), page, more: !!res.more };
        }),
      (e) => done(() => ({ moreError: (e as Error).message })),
    );
  }

  /** Switches the browse panel between search results and the current link's lists. */
  showList(tab: ListTab): void {
    this.set({ listTab: tab });
  }

  private pushHistory(): void {
    const d = this._state.draft;
    if (d.status !== "ready") return;
    const entry: HistoryEntry = { draft: { ...d, preview: undefined, hint: undefined, restored: undefined }, at: this.now() };
    this.set({ history: [...this._state.history, entry].slice(-MAX_HISTORY) });
  }

  private now(): number {
    return (this.opts.now ?? Date.now)();
  }

  private closePreview(): void {
    const id = this._state.draft.probeId;
    if (id) this.client?.closePreview(id);
  }

  /** Reads the active sequence (for recommendations and fps warnings). */
  async refreshSequence(): Promise<void> {
    if (!this.opts.host.sequenceInfo) return;
    try {
      const seq = await this.opts.host.sequenceInfo();
      this.set({ sequence: seq });
    } catch {
      this.set({ sequence: null });
    }
  }

  /** Reads the active project for every request so Save As and project switches take effect immediately. */
  private async refreshOutputDir(): Promise<string | undefined> {
    let projectDir: string | null = null;
    try {
      projectDir = (await this.opts.host.projectOutputDir?.()) ?? null;
    } catch {
      projectDir = null;
    }
    const outputDir = projectDir || this._state.helper?.libraryDir;
    if (outputDir !== this._state.outputDir) {
      this.set({ outputDir });
    }
    // Only send a project-local preference. Omitting it makes the helper use its configured fallback.
    return projectDir || undefined;
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
      const res = await this.opts.host.importFile(view.snap.file, bin, importOptions(view.snap));
      this.patchJob(jobId, {
        import: res.ok
          ? { state: "done", bin: res.bin, inout: res.inout, placed: res.placed, captions: res.captions, warnings: res.warnings }
          : { state: "failed", error: res.error, bin },
      });
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
    client.on("preview.state", (d) => {
      const cur = this._state.draft;
      if (!d.probeId || d.probeId !== cur.probeId) return;
      if (cur.restored && d.stage === "failed") {
        // 되살린 조회를 Helper가 이미 버렸다(20분·16개 한도): 다시 조회하고 고른 값을 이어 쓴다.
        this.set({ draft: { ...EMPTY_DRAFT, url: cur.url, via: cur.via } });
        void this.probeDraft(cur);
        return;
      }
      this.set({ draft: { ...cur, preview: d, restored: undefined } });
    });
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
