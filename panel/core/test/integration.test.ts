// TS core ↔ real Go helper, end to end (no Premiere): the same Controller the CEP panel uses,
// with a Node HostAdapter that spawns the built helper exactly like CepHost does.
// 필요: scripts/build-helper.sh 산출물, ffmpeg, yt-dlp(BIRDCAGE_YTDLP 또는 Helper가 설치한 것). 없으면 건너뛴다.
import { execFileSync, spawn, ChildProcess } from "node:child_process";
import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { createServer, Server } from "node:http";
import { createReadStream } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Controller } from "../src/controller";
import type { HelperEndpoint, WebSocketLike } from "../src/client";
import type { HostAdapter, ImportResult } from "../src/host";

const helperBin = join(__dirname, "..", "..", "cep", "src", "bin", process.platform === "win32" ? "win32-x64/birdcage-helper.exe" : "darwin/birdcage-helper");

function has(cmd: string): boolean {
  try {
    execFileSync(cmd, ["-version"], { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
}

const ready = existsSync(helperBin) && has("ffmpeg") && (!!process.env.BIRDCAGE_YTDLP || process.env.BIRDCAGE_INTEGRATION === "1");

class NodeHost implements HostAdapter {
  child: ChildProcess | null = null;
  imports: Array<{ path: string; bin: string[] }> = [];
  constructor(private dataDir: string) {}
  info() {
    return { app: "PPRO", version: "15.2.0", api: "cep" as const, os: (process.platform === "win32" ? "win" : "mac") as "win" | "mac" };
  }
  startHelper(): Promise<HelperEndpoint> {
    return new Promise((resolve, reject) => {
      const child = spawn(helperBin, ["--handshake", "stdout", "--exit-on-stdin-close", "--data-dir", this.dataDir,
        "--library-dir", join(this.dataDir, "library"), "--log-level", "warn"], { stdio: ["pipe", "pipe", "inherit"] });
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
  async importFile(path: string, bin: string[]): Promise<ImportResult> {
    this.imports.push({ path, bin });
    return { ok: true, bin };
  }
}

describe.skipIf(!ready)("integration: Controller + real helper", () => {
  let dir: string;
  let server: Server;
  let base: string;

  beforeAll(async () => {
    dir = mkdtempSync(join(tmpdir(), "birdcage-int-"));
    execFileSync("ffmpeg", ["-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "testsrc2=s=320x240:r=30:d=3",
      "-f", "lavfi", "-i", "sine=r=48000:d=3", "-c:v", "libvpx-vp9", "-deadline", "realtime", "-cpu-used", "8", "-b:v", "200k",
      "-c:a", "libopus", "-shortest", join(dir, "clip.webm")]);
    server = createServer((req, res) => {
      res.setHeader("Content-Type", "video/webm");
      createReadStream(join(dir, "clip.webm")).pipe(res);
    });
    await new Promise<void>((r) => server.listen(0, "127.0.0.1", () => r()));
    const addr = server.address() as { port: number };
    base = `http://127.0.0.1:${addr.port}`;
  });

  afterAll(() => {
    server?.close();
    rmSync(dir, { recursive: true, force: true });
  });

  it("downloads, normalizes and auto-imports through the real helper", async () => {
    const host = new NodeHost(join(dir, "data"));
    const ctl = new Controller({
      host,
      clientName: "integration",
      clientVersion: "0",
      ws: (url) => new WebSocket(url) as unknown as WebSocketLike,
    });
    const t0 = Date.now();
    await ctl.start();
    expect(ctl.state.connection).toBe("connected");
    const startupMs = Date.now() - t0;

    await ctl.submit(`${base}/clip.webm`);
    const done = await new Promise<typeof ctl.state.jobs[number]>((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error("timeout: " + JSON.stringify(ctl.state.jobs.map((j) => j.snap.state)))), 90000);
      const un = ctl.subscribe((s) => {
        const j = s.jobs[0];
        if (j?.import?.state === "done" || j?.snap.state === "failed") {
          clearTimeout(timer);
          un();
          resolve(j);
        }
      });
    });
    expect(done.snap.failure).toBeUndefined();
    expect(done.snap.media?.video?.codec).toBe("h264");
    expect(done.snap.media?.audio?.codec).toBe("aac");
    expect(host.imports).toHaveLength(1);
    expect(host.imports[0].bin).toEqual(["Birdcage", "Generic"]);
    expect(existsSync(host.imports[0].path)).toBe(true);
    console.log(`helper startup→connected ${startupMs}ms, job timings`, done.snap.timings);
    ctl.dispose();
  }, 120000);
});
