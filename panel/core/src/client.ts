// HelperClient: one WebSocket session with the helper.
// 요청에는 id를 붙이고 같은 id의 응답(또는 error)으로 Promise를 끝낸다. 나머지 메시지는 이벤트로 흘린다.
import {
  ClientMessages,
  decode,
  encode,
  Envelope,
  Hello,
  HelloOK,
  JobAccepted,
  JobCreate,
  JobList,
  MediaInfoMsg,
  ProtocolError,
  RelatedListMsg,
  RelatedSource,
  ServerMessages,
  ServerType,
} from "./protocol";

/** Subset of the browser WebSocket API we rely on (CEP Chromium 74, UXP, tests). */
export interface WebSocketLike {
  readonly readyState: number;
  send(data: string): void;
  close(code?: number, reason?: string): void;
  onopen: ((ev: unknown) => void) | null;
  onmessage: ((ev: { data: unknown }) => void) | null;
  onclose: ((ev: { code?: number; reason?: string }) => void) | null;
  onerror: ((ev: unknown) => void) | null;
}

export type WebSocketFactory = (url: string) => WebSocketLike;

export interface HelperEndpoint {
  port: number;
  token: string;
  version?: string;
  pprof?: string;
}

export class RequestError extends Error {
  constructor(public code: string, message: string) {
    super(message);
  }
}

type Listener<K extends ServerType> = (data: ServerMessages[K], env: Envelope) => void;

interface Pending {
  expect: ServerType[];
  resolve: (env: Envelope) => void;
  reject: (err: Error) => void;
  timer: ReturnType<typeof setTimeout>;
}

export interface ClientOptions {
  endpoint: HelperEndpoint;
  hello: Hello;
  ws?: WebSocketFactory;
  requestTimeoutMs?: number;
  /** Called once when the socket closes (helper died, network error, close()). */
  onClose?: (reason: string) => void;
}

export class HelperClient {
  private sock: WebSocketLike | null = null;
  private seq = 0;
  private pending = new Map<string, Pending>();
  private listeners = new Map<string, Set<Listener<any>>>();
  private closed = false;

  constructor(private opts: ClientOptions) {}

  /** Opens the socket, sends hello and resolves with hello.ok. */
  connect(): Promise<HelloOK> {
    const { port, token } = this.opts.endpoint;
    const url = `ws://127.0.0.1:${port}/ws?token=${encodeURIComponent(token)}`;
    const factory = this.opts.ws ?? ((u: string) => new WebSocket(u) as unknown as WebSocketLike);
    return new Promise<HelloOK>((resolve, reject) => {
      let opened = false;
      const sock = factory(url);
      this.sock = sock;
      sock.onopen = () => {
        opened = true;
        this.request("hello", this.opts.hello, ["hello.ok"])
          .then((env) => resolve(env.data as HelloOK))
          .catch(reject);
      };
      sock.onmessage = (ev) => this.handle(String(ev.data));
      sock.onerror = () => {
        if (!opened) reject(new Error("helper connection failed"));
      };
      sock.onclose = (ev) => {
        const reason = ev?.reason || `closed (${ev?.code ?? "?"})`;
        if (!opened) reject(new Error("helper connection closed: " + reason));
        this.teardown(reason);
      };
    });
  }

  on<K extends ServerType>(type: K, fn: Listener<K>): () => void {
    let set = this.listeners.get(type);
    if (!set) this.listeners.set(type, (set = new Set()));
    set.add(fn);
    return () => set!.delete(fn);
  }

  async createJob(url: string, maxHeight?: number, extra: Omit<JobCreate, "url" | "maxHeight"> = {}): Promise<string> {
    const env = await this.request("job.create", { url, maxHeight, ...extra }, ["job.accepted"]);
    return (env.data as JobAccepted).jobId;
  }

  /** Probes a link (yt-dlp -J takes seconds; the helper answers with media.info). */
  async probe(url: string, maxHeight?: number, via?: string): Promise<MediaInfoMsg> {
    const env = await this.request("media.probe", { url, maxHeight, via }, ["media.info"], 90000);
    return env.data as MediaInfoMsg;
  }

  /** Browse list for a probed link (watch page ~1s, yt-dlp fallbacks up to a few seconds). */
  async related(probeId: string, source: RelatedSource): Promise<RelatedListMsg> {
    const env = await this.request("related.fetch", { probeId, source }, ["related.list"], 45000);
    return env.data as RelatedListMsg;
  }

  cancelJob(jobId: string): void {
    this.send("job.cancel", undefined, { jobId });
  }

  async listJobs(): Promise<JobList["jobs"]> {
    const env = await this.request("job.list", undefined, ["job.list.ok"]);
    return (env.data as JobList).jobs;
  }

  /** Starts the staged preview of a probe; states arrive as preview.state events. */
  openPreview(probeId: string): void {
    this.send("preview.open", undefined, { probeId });
  }

  closePreview(probeId: string): void {
    if (this.connected) this.send("preview.close", undefined, { probeId });
  }

  /** Starts tool installation; progress arrives as tools.status events. */
  ensureTools(channel?: "stable" | "nightly"): void {
    this.send("tools.ensure", undefined, channel ? { channel } : undefined);
  }

  close(): void {
    this.closed = true;
    this.sock?.close(1000, "panel closing");
    this.teardown("closed by panel");
  }

  get connected(): boolean {
    return !!this.sock && this.sock.readyState === 1 && !this.closed;
  }

  private send<K extends keyof ClientMessages>(type: K, id: string | undefined, data: ClientMessages[K]): void {
    if (!this.sock || this.sock.readyState !== 1) throw new Error("helper not connected");
    this.sock.send(encode(type, id, data));
  }

  private request<K extends keyof ClientMessages>(type: K, data: ClientMessages[K], expect: ServerType[], timeoutMs?: number): Promise<Envelope> {
    const id = `r${++this.seq}`;
    return new Promise<Envelope>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`${type}: no reply from helper`));
      }, timeoutMs ?? this.opts.requestTimeoutMs ?? 15000);
      this.pending.set(id, { expect, resolve, reject, timer });
      try {
        this.send(type, id, data);
      } catch (e) {
        clearTimeout(timer);
        this.pending.delete(id);
        reject(e as Error);
      }
    });
  }

  private handle(raw: string): void {
    let env: Envelope;
    try {
      env = decode(raw);
    } catch (e) {
      console.warn("[birdcage] bad message from helper", e);
      return;
    }
    const p = env.id ? this.pending.get(env.id) : undefined;
    if (p && (env.type === "error" || p.expect.includes(env.type as ServerType))) {
      clearTimeout(p.timer);
      this.pending.delete(env.id!);
      if (env.type === "error") {
        const e = env.data as ProtocolError;
        p.reject(new RequestError(e.code, e.message));
      } else {
        p.resolve(env);
      }
    }
    const set = this.listeners.get(env.type);
    if (set) for (const fn of set) fn(env.data, env);
  }

  private teardown(reason: string): void {
    if (!this.sock) return;
    this.sock = null;
    for (const [id, p] of this.pending) {
      clearTimeout(p.timer);
      p.reject(new Error("helper disconnected: " + reason));
      this.pending.delete(id);
    }
    this.opts.onClose?.(reason);
  }
}
