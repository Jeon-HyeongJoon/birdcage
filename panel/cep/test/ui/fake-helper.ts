// In-browser fake Helper for UI fixtures: answers the protocol like the Go helper so the real
// Controller (panel/core) produces the state the panel renders. 손으로 만든 State 대신 실제 상태 전이를 쓴다.
import type { Envelope, HelloOK, MediaInfoMsg, PreviewState, RelatedListMsg, RelatedSource, SearchListMsg, WebSocketLike } from "@birdcage/core";

export class FakeSocket implements WebSocketLike {
  readyState = 0;
  onopen: ((ev: unknown) => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  onclose: ((ev: { code?: number; reason?: string }) => void) | null = null;
  onerror: ((ev: unknown) => void) | null = null;

  constructor(private onSend: (s: FakeSocket, env: Envelope) => void) {
    queueMicrotask(() => {
      this.readyState = 1;
      this.onopen?.({});
    });
  }

  send(data: string): void {
    this.onSend(this, JSON.parse(data) as Envelope);
  }

  push(type: string, data: unknown, id?: string): void {
    this.onmessage?.({ data: JSON.stringify({ v: 0, type, id, data }) });
  }

  close(code = 1000, reason = ""): void {
    if (this.readyState === 3) return;
    this.readyState = 3;
    this.onclose?.({ code, reason });
  }
}

export interface FakeHelperOptions {
  hello: HelloOK;
  /** media.probe answer; undefined = never answers (probing state). */
  probe?: (url: string) => MediaInfoMsg | undefined;
  /** preview.state messages sent after preview.open (in order). */
  preview?: (probeId: string) => Omit<PreviewState, "probeId">[];
  /** related.list answer; undefined = never answers (loading). */
  related?: (probeId: string, source: RelatedSource) => Omit<RelatedListMsg, "probeId" | "requested"> | undefined;
  /** search.list answer; undefined = never answers (loading). */
  search?: (query: string, page: number) => Omit<SearchListMsg, "query" | "page"> | undefined;
}

export class FakeHelper {
  socket: FakeSocket | null = null;
  private nextJob = 1;
  constructor(private opts: FakeHelperOptions) {}

  factory = (): WebSocketLike => {
    this.socket = new FakeSocket((sock, env) => this.reply(sock, env));
    return this.socket;
  };

  push(type: string, data: unknown): void {
    this.socket?.push(type, data);
  }

  private reply(sock: FakeSocket, env: Envelope): void {
    queueMicrotask(() => {
      switch (env.type) {
        case "hello":
          sock.push("hello.ok", this.opts.hello, env.id);
          break;
        case "job.create":
          sock.push("job.accepted", { jobId: `j${this.nextJob++}` }, env.id);
          break;
        case "media.probe": {
          const reply = this.opts.probe?.((env.data as { url: string }).url);
          if (reply) sock.push("media.info", reply, env.id);
          break;
        }
        case "preview.open": {
          const id = (env.data as { probeId: string }).probeId;
          (this.opts.preview?.(id) ?? []).forEach((st, i) => sock.push("preview.state", { ...st, probeId: id }, i === 0 ? env.id : undefined));
          break;
        }
        case "related.fetch": {
          const d = env.data as { probeId: string; source: RelatedSource };
          const reply = this.opts.related?.(d.probeId, d.source);
          if (reply) sock.push("related.list", { probeId: d.probeId, requested: d.source, source: d.source, ...reply }, env.id);
          break;
        }
        case "search.fetch": {
          const d = env.data as { query: string; page?: number };
          const reply = this.opts.search?.(d.query, d.page ?? 0);
          if (reply) sock.push("search.list", { query: d.query, page: d.page ?? 0, ...reply }, env.id);
          break;
        }
        case "tools.ensure":
          sock.push("tools.status", { installing: true, tools: this.opts.hello.tools }, env.id);
          break;
      }
    });
  }
}
