// Link probe end to end (no Premiere, no network): Controller → real helper → yt-dlp generic extractor
// on a local file server. 조회 결과를 다운로드가 재사용하는지(probe 단계가 재추출 없이 끝남) 확인한다.
// 필요: scripts/build-helper.sh 산출물, ffmpeg, yt-dlp(BIRDCAGE_YTDLP). 없으면 건너뛴다.
import { ChildProcess, execFileSync, spawn } from "node:child_process";
import { createReadStream, existsSync, mkdtempSync, rmSync } from "node:fs";
import { createServer, Server } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import type { HelperEndpoint, WebSocketLike } from "../src/client";
import { Controller } from "../src/controller";
import type { HostAdapter, ImportResult } from "../src/host";

const helperBin = join(__dirname, "..", "..", "cep", "src", "bin", process.platform === "win32" ? "win32-x64/birdcage-helper.exe" : "darwin/birdcage-helper");
const ytdlp = process.env.BIRDCAGE_YTDLP;
const ready = existsSync(helperBin) && !!ytdlp;

class NodeHost implements HostAdapter {
  child: ChildProcess | null = null;
  constructor(private dataDir: string) {}
  info() {
    return { app: "PPRO", version: "15.2.0", api: "cep" as const, os: (process.platform === "win32" ? "win" : "mac") as "win" | "mac" };
  }
  startHelper(): Promise<HelperEndpoint> {
    return new Promise((resolve, reject) => {
      const child = spawn(helperBin, ["--handshake", "stdout", "--exit-on-stdin-close", "--data-dir", this.dataDir,
        "--library-dir", join(this.dataDir, "library"), "--log-level", "warn", "--yt-dlp", ytdlp!], { stdio: ["pipe", "pipe", "inherit"] });
      this.child = child;
      let buf = "";
      child.stdout!.setEncoding("utf8");
      child.stdout!.on("data", (c: string) => {
        buf += c;
        const nl = buf.indexOf("\n");
        if (nl >= 0) {
          const r = JSON.parse(buf.slice(0, nl));
          resolve({ port: r.port, token: r.token, version: r.version });
        }
      });
      child.on("error", reject);
    });
  }
  stopHelper() {
    this.child?.stdin?.end();
  }
  async importFile(_path: string, bin: string[]): Promise<ImportResult> {
    return { ok: true, bin };
  }
  async sequenceInfo() {
    return { name: "Seq", width: 1920, height: 1080, fps: 30 };
  }
}

const until = <T>(ctl: Controller, pick: (s: Controller["state"]) => T | undefined, ms: number) =>
  new Promise<T>((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error("timeout: " + JSON.stringify(ctl.state.draft) + JSON.stringify(ctl.state.jobs.map((j) => j.snap.state)))), ms);
    const check = (s: Controller["state"]) => {
      const v = pick(s);
      if (v !== undefined) {
        clearTimeout(timer);
        un();
        resolve(v);
      }
    };
    const un = ctl.subscribe(check);
    check(ctl.state);
  });

describe.skipIf(!ready)("integration: probe → reuse → download", () => {
  let dir: string;
  let server: Server;
  let base: string;

  beforeAll(async () => {
    dir = mkdtempSync(join(tmpdir(), "birdcage-probe-"));
    execFileSync("ffmpeg", ["-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "testsrc2=s=320x240:r=30:d=3",
      "-f", "lavfi", "-i", "sine=r=48000:d=3", "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest",
      "-movflags", "+faststart", join(dir, "clip.mp4")]);
    server = createServer((_req, res) => {
      res.setHeader("Content-Type", "video/mp4");
      createReadStream(join(dir, "clip.mp4")).pipe(res);
    });
    await new Promise<void>((r) => server.listen(0, "127.0.0.1", () => r()));
    base = `http://127.0.0.1:${(server.address() as { port: number }).port}`;
  });

  afterAll(() => {
    server?.close();
    rmSync(dir, { recursive: true, force: true });
  });

  it("probes a pasted link and the job reuses the probe", async () => {
    const ctl = new Controller({
      host: new NodeHost(join(dir, "data")),
      clientName: "integration",
      clientVersion: "0",
      ws: (url) => new WebSocket(url) as unknown as WebSocketLike,
      probeDebounceMs: 0,
      settings: { autoImport: false },
    });
    await ctl.start();
    const t0 = Date.now();
    ctl.setUrl(`${base}/clip.mp4`);
    const draft = await until(ctl, (s) => (s.draft.status === "ready" || s.draft.status === "failed" ? s.draft : undefined), 60000);
    const probeMs = Date.now() - t0;
    expect(draft.failure).toBeUndefined();
    expect(draft.info?.extractor).toBe("Generic");
    expect(draft.probeId).toMatch(/^p[0-9a-f]+$/);
    expect(ctl.state.sequence?.width).toBe(1920);

    await ctl.submitDraft();
    const job = await until(ctl, (s) => (s.jobs[0] && ["done", "failed"].includes(s.jobs[0].snap.state) ? s.jobs[0].snap : undefined), 60000);
    expect(job.failure).toBeUndefined();
    expect(job.options.probeId).toBe(draft.probeId);
    const probeStage = job.timings?.find((t) => t.stage === "probe");
    // 재사용이면 probe 단계는 파일 복사뿐이다(yt-dlp 실행 없음). 조회 자체는 수백 ms 걸린다.
    expect(probeStage && probeStage.ms).toBeLessThan(100);
    console.log(`probe (yt-dlp -J) ${probeMs}ms, job probe stage ${probeStage?.ms}ms (reused), timings`, job.timings);
    ctl.dispose();
  }, 120000);
});
