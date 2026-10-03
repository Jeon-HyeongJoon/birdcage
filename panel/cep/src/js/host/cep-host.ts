// HostAdapter for CEP panels (Premiere 15.0–26).
// CEP는 Node.js를 쓸 수 있어 Helper를 자식 프로세스로 직접 띄운다(UXP는 v2에서 다른 방식).
import type { HelperEndpoint, Host, HostAdapter, ImportOptions, ImportResult, SequenceInfo } from "@birdcage/core";
import { child_process, fs, os, path } from "../lib/cep/node";
import { csi, evalTS } from "../lib/utils/bolt";

type ChildProcess = ReturnType<typeof child_process.spawn>;

const READY_TIMEOUT_MS = 20000;

// ExtendScript는 ASCII 코드만 돌려주고(인코딩 문제 회피) 메시지는 여기서 만든다.
const IMPORT_ERRORS = {
  no_project: "열린 프로젝트가 없습니다.",
  file_missing: "파일을 찾을 수 없습니다",
  import_failed: "Premiere가 파일을 가져오지 못했습니다",
} as const;

// 배치 결과 중 사용자에게 알릴 것(가져오기 자체는 성공). ok는 알리지 않는다.
const PLACE_WARNINGS: Partial<Record<NonNullable<ImportResult["placed"]>, string>> = {
  unverified: "타임라인에 놓았지만 위치를 확인하지 못했습니다.",
  no_sequence: "열린 시퀀스가 없어 프로젝트에만 가져왔습니다.",
  no_free_track: "재생 헤드 위치에 빈 트랙이 없어 프로젝트에만 가져왔습니다. 트랙을 추가하거나 삽입·덮어쓰기를 쓰세요.",
  failed: "타임라인에 놓지 못했습니다(프로젝트에는 가져옴).",
};

export class CepHost implements HostAdapter {
  private child: ChildProcess | null = null;
  private endpoint: HelperEndpoint | null = null;
  private starting: Promise<HelperEndpoint> | null = null;
  /** Last helper stderr lines, shown when startup fails. */
  readonly stderr: string[] = [];

  info(): Host {
    const env = csi.getHostEnvironment();
    return {
      app: "PPRO",
      version: env?.appVersion ?? "0",
      api: "cep",
      os: os.platform() === "win32" ? "win" : "mac",
    };
  }

  /** Path of the bundled helper inside the extension folder. */
  helperPath(): string {
    const root = csi.getSystemPath("extension");
    if (os.platform() === "win32") {
      return path.join(root, "bin", `win32-${process.arch}`, "birdcage-helper.exe");
    }
    return path.join(root, "bin", "darwin", "birdcage-helper"); // universal2 (arm64 + x86_64)
  }

  startHelper(): Promise<HelperEndpoint> {
    if (this.endpoint && this.child && this.child.exitCode === null) return Promise.resolve(this.endpoint);
    if (this.starting) return this.starting;
    this.starting = this.spawn().finally(() => {
      this.starting = null;
    });
    return this.starting;
  }

  private spawn(): Promise<HelperEndpoint> {
    const exe = this.helperPath();
    if (!fs.existsSync(exe)) {
      return Promise.reject(new Error(`Helper 실행 파일이 없습니다: ${exe} (scripts/build-helper.sh 실행 필요)`));
    }
    let cmd = exe;
    const args = ["--handshake", "stdout", "--exit-on-stdin-close"];
    // Premiere 2021 같은 Intel 빌드는 Apple Silicon에서 Rosetta로 돈다. 이때 universal 바이너리를
    // 그냥 실행하면 x86_64로 떠서 느려지므로 arm64로 강제한다.
    if (os.platform() === "darwin" && isTranslated()) {
      cmd = "/usr/bin/arch";
      args.unshift("-arm64", exe);
    }
    return new Promise<HelperEndpoint>((resolve, reject) => {
      const child = child_process.spawn(cmd, args, { stdio: ["pipe", "pipe", "pipe"], windowsHide: true });
      this.child = child;
      let buf = "";
      let settled = false;
      const timer = setTimeout(() => fail(new Error("Helper가 제시간에 시작되지 않았습니다.")), READY_TIMEOUT_MS);
      const fail = (err: Error) => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        const tail = this.stderr.slice(-5).join("\n");
        reject(tail ? new Error(`${err.message}\n${tail}`) : err);
      };
      child.stdout!.setEncoding("utf8");
      child.stdout!.on("data", (chunk: string) => {
        if (settled) return;
        buf += chunk;
        const nl = buf.indexOf("\n");
        if (nl < 0) return;
        try {
          const ready = JSON.parse(buf.slice(0, nl));
          if (ready.type !== "ready") throw new Error("unexpected handshake");
          settled = true;
          clearTimeout(timer);
          this.endpoint = { port: ready.port, token: ready.token, version: ready.version, pprof: ready.pprof };
          resolve(this.endpoint);
        } catch (e) {
          fail(new Error("Helper 핸드셰이크 오류: " + (e as Error).message));
        }
      });
      child.stderr!.setEncoding("utf8");
      child.stderr!.on("data", (chunk: string) => {
        for (const line of chunk.split(/\r?\n/)) {
          if (!line) continue;
          this.stderr.push(line);
          if (this.stderr.length > 50) this.stderr.shift();
        }
      });
      child.on("error", (err: Error) => fail(err));
      child.on("exit", (code: number | null) => {
        if (this.child === child) {
          this.child = null;
          this.endpoint = null;
        }
        fail(new Error(`Helper가 종료되었습니다 (code ${code})`));
      });
    });
  }

  stopHelper(): void {
    const child = this.child;
    if (!child) return;
    this.child = null;
    this.endpoint = null;
    try {
      child.stdin?.end(); // Helper는 stdin이 닫히면 스스로 종료한다
    } catch {
      /* ignore */
    }
    setTimeout(() => {
      if (child.exitCode === null) child.kill();
    }, 3000);
  }

  async importFile(file: string, bin: string[], opts?: ImportOptions): Promise<ImportResult> {
    const es = opts
      ? { inSec: opts.clip?.inSec, outSec: opts.clip?.outSec, openSource: opts.openSource, place: opts.place, hasVideo: opts.hasVideo }
      : undefined;
    const res = (await evalTS("birdcageImport", file, bin, es)) as
      | {
          ok: boolean;
          code?: keyof typeof IMPORT_ERRORS;
          detail?: string;
          bin: string[];
          inout?: ImportResult["inout"];
          source?: boolean;
          placed?: ImportResult["placed"];
        }
      | undefined;
    if (!res) return { ok: false, error: "ExtendScript 응답이 없습니다.", bin };
    if (res.ok) {
      const warnings: string[] = [];
      if (res.inout === "unverified") warnings.push("In/Out을 설정했지만 확인하지 못했습니다. 소스 모니터에서 확인하세요.");
      if (res.inout === "unsupported") warnings.push("이 Premiere 버전은 스크립트로 In/Out을 설정할 수 없습니다.");
      if (opts?.openSource && res.source === false) warnings.push("소스 모니터에서 열지 못했습니다.");
      const placeMsg = res.placed ? PLACE_WARNINGS[res.placed] : undefined;
      if (placeMsg) warnings.push(placeMsg);
      return { ok: true, bin: res.bin, inout: res.inout, placed: res.placed, warnings: warnings.length ? warnings : undefined };
    }
    const msg = (res.code && IMPORT_ERRORS[res.code]) || "알 수 없는 오류";
    return { ok: false, error: res.detail ? `${msg} (${res.detail})` : msg, bin: res.bin };
  }

  async sequenceInfo(): Promise<SequenceInfo | null> {
    const res = (await evalTS("birdcageSequenceInfo")) as
      | { ok: boolean; name?: string; width?: number; height?: number; fps?: number }
      | undefined;
    if (!res || !res.ok || !res.width || !res.height) return null;
    return { name: res.name ?? "", width: res.width, height: res.height, fps: res.fps ?? 0 };
  }

  revealFile(file: string): void {
    if (os.platform() === "win32") {
      child_process.spawn("explorer.exe", ["/select,", file], { detached: true, stdio: "ignore" });
    } else {
      child_process.spawn("open", ["-R", file], { detached: true, stdio: "ignore" });
    }
  }
}

function isTranslated(): boolean {
  try {
    return child_process.execFileSync("sysctl", ["-n", "sysctl.proc_translated"], { encoding: "utf8" }).trim() === "1";
  } catch {
    return false; // Intel Mac에는 이 키가 없다
  }
}
