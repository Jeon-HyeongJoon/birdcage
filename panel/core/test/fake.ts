// Test doubles: an in-memory WebSocket and a scripted fake helper.
import type { WebSocketLike } from "../src/client";
import type { Envelope, JobSnapshot } from "../src/protocol";

export class FakeSocket implements WebSocketLike {
  readyState = 0;
  sent: Envelope[] = [];
  onopen: ((ev: unknown) => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  onclose: ((ev: { code?: number; reason?: string }) => void) | null = null;
  onerror: ((ev: unknown) => void) | null = null;

  constructor(public url: string, private onSend: (s: FakeSocket, env: Envelope) => void) {
    queueMicrotask(() => {
      this.readyState = 1;
      this.onopen?.({});
    });
  }

  send(data: string): void {
    const env = JSON.parse(data) as Envelope;
    this.sent.push(env);
    this.onSend(this, env);
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

export function job(id: string, state: JobSnapshot["state"], extra: Partial<JobSnapshot> = {}): JobSnapshot {
  return {
    jobId: id,
    url: "https://youtu.be/x",
    state,
    options: { maxHeight: 1080, profile: "compat" },
    progress: -1,
    createdAt: "2026-10-01T12:00:00Z",
    updatedAt: "2026-10-01T12:00:00Z",
    ...extra,
  };
}

/** A fake helper that answers hello/job.create and lets tests push events. */
export class FakeHelper {
  sockets: FakeSocket[] = [];
  nextJob = 1;
  factory = (url: string) => {
    const s = new FakeSocket(url, (sock, env) => this.reply(sock, env));
    this.sockets.push(s);
    return s;
  };

  get last(): FakeSocket {
    return this.sockets[this.sockets.length - 1];
  }

  private reply(sock: FakeSocket, env: Envelope): void {
    queueMicrotask(() => {
      switch (env.type) {
        case "hello":
          sock.push("hello.ok", { helperVersion: "test", protocol: 0, profile: "compat", libraryDir: "/lib", tools: [], jobs: [] }, env.id);
          break;
        case "job.create": {
          const url = (env.data as { url: string }).url;
          if (!url.startsWith("http")) {
            sock.push("error", { code: "bad_request", message: "invalid url" }, env.id);
            return;
          }
          sock.push("job.accepted", { jobId: `j${this.nextJob++}` }, env.id);
          break;
        }
      }
    });
  }
}

export const flush = () => new Promise((r) => setTimeout(r, 0));
