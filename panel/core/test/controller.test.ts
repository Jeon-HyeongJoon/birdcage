import { describe, expect, it, vi } from "vitest";
import { HelperClient, RequestError } from "../src/client";
import { Controller } from "../src/controller";
import type { HostAdapter } from "../src/host";
import { overallProgress, bytes, eta } from "../src/format";
import { FakeHelper, flush, job } from "./fake";

function host(overrides: Partial<HostAdapter> = {}): HostAdapter & { imports: string[][] } {
  const imports: string[][] = [];
  return {
    imports,
    info: () => ({ app: "PPRO", version: "15.2.0", api: "cep", os: "mac" }),
    startHelper: async () => ({ port: 1234, token: "t" }),
    stopHelper: () => {},
    importFile: async (path, bin) => {
      imports.push([path, ...bin]);
      return { ok: true, bin };
    },
    ...overrides,
  };
}

describe("HelperClient", () => {
  it("sends hello with the token and matches replies by id", async () => {
    const fh = new FakeHelper();
    const c = new HelperClient({ endpoint: { port: 1, token: "a b" }, hello: { client: "t", clientVersion: "0", host: host().info() }, ws: fh.factory });
    const ok = await c.connect();
    expect(fh.last.url).toBe("ws://127.0.0.1:1/ws?token=a%20b");
    expect(ok.profile).toBe("compat");
    expect(fh.last.sent[0]).toMatchObject({ v: 0, type: "hello", id: "r1" });
    await expect(c.createJob("https://youtu.be/x", 720)).resolves.toBe("j1");
    expect(fh.last.sent[1].data).toEqual({ url: "https://youtu.be/x", maxHeight: 720 });
    await expect(c.createJob("ftp://x")).rejects.toBeInstanceOf(RequestError);
  });

  it("rejects pending requests when the helper disconnects", async () => {
    const fh = new FakeHelper();
    const onClose = vi.fn();
    const c = new HelperClient({ endpoint: { port: 1, token: "t" }, hello: { client: "t", clientVersion: "0", host: host().info() }, ws: fh.factory, onClose });
    await c.connect();
    const p = c.listJobs(); // fake helper never answers job.list
    fh.last.close(1006, "helper died");
    await expect(p).rejects.toThrow(/disconnected/);
    expect(onClose).toHaveBeenCalledWith("helper died");
  });
});

describe("Controller", () => {
  it("connects, tracks jobs and auto-imports finished files into Birdcage/<platform>", async () => {
    const fh = new FakeHelper();
    const h = host();
    const ctl = new Controller({ host: h, clientName: "test", clientVersion: "0", ws: fh.factory });
    await ctl.start();
    expect(ctl.state.connection).toBe("connected");
    expect(ctl.missingTools()).toEqual(["yt-dlp", "ffmpeg", "ffprobe", "deno"]);

    await ctl.submit(" https://youtu.be/x ");
    expect(fh.last.sent[1].data).toEqual({ url: "https://youtu.be/x", maxHeight: 1080 });

    fh.last.push("job.update", { job: job("j1", "downloading", { progress: 0.5 }) });
    expect(ctl.state.jobs[0].snap.state).toBe("downloading");
    expect(overallProgress(ctl.state.jobs[0].snap)).toBeCloseTo(0.425);

    fh.last.push("job.done", { job: job("j1", "done", { file: "/lib/Youtube/a.mp4", info: { id: "x", title: "a", extractor: "Youtube", webpageUrl: "u" } }) });
    await flush();
    expect(h.imports).toEqual([["/lib/Youtube/a.mp4", "Birdcage", "Youtube"]]);
    expect(ctl.state.jobs[0].import).toEqual({ state: "done", bin: ["Birdcage", "Youtube"] });

    // 같은 done 이벤트가 다시 와도 두 번 임포트하지 않는다.
    fh.last.push("job.done", { job: job("j1", "done", { file: "/lib/Youtube/a.mp4" }) });
    await flush();
    expect(h.imports.length).toBe(1);
  });

  it("reports import failures without throwing", async () => {
    const fh = new FakeHelper();
    const ctl = new Controller({
      host: host({ importFile: async (_p, bin) => ({ ok: false, error: "열린 프로젝트가 없습니다", bin }) }),
      clientName: "t", clientVersion: "0", ws: fh.factory,
    });
    await ctl.start();
    fh.last.push("job.done", { job: job("j9", "done", { file: "/f.mp4" }) });
    await flush();
    expect(ctl.state.jobs[0].import).toMatchObject({ state: "failed", error: "열린 프로젝트가 없습니다" });
  });

  it("restarts the helper after it dies and gives up after the backoff list", async () => {
    const fh = new FakeHelper();
    const timers: Array<() => void> = [];
    let starts = 0;
    const ctl = new Controller({
      host: host({ startHelper: async () => { starts++; if (starts > 2) throw new Error("spawn failed"); return { port: 1, token: "t" }; } }),
      clientName: "t", clientVersion: "0", ws: fh.factory, backoff: [10, 10],
      setTimer: (fn) => timers.push(fn),
    });
    await ctl.start();
    fh.last.close(1006, "helper crashed");
    expect(ctl.state.connection).toBe("reconnecting");
    timers.shift()!();
    await flush(); await flush();
    expect(ctl.state.connection).toBe("connected");
    expect(starts).toBe(2);

    fh.last.close(1006, "again");
    timers.shift()!(); // start #3 fails → 다음 백오프
    await flush();
    expect(ctl.state.connection).toBe("reconnecting");
    timers.shift()!(); // start #4 fails → 백오프 목록 소진
    await flush();
    expect(ctl.state.connection).toBe("failed");
    expect(ctl.state.error).toBe("spawn failed");
  });
});

describe("format", () => {
  it("formats sizes and eta", () => {
    expect(bytes(1536)).toBe("1.5 KB");
    expect(bytes(0)).toBe("");
    expect(eta(75)).toBe("1분 15초 남음");
  });
});
