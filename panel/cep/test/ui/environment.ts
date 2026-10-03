// Pixel goldens are valid only in this pinned rendering environment (browser major, OS major, arch).
// DOM goldens are compared everywhere. A change here is a verifier change (docs/decisions, W7).
import { execFileSync } from "child_process";
import os from "os";

export const pinned = { chrome: "154", os: "darwin27", arch: "arm64" } as const;

/** "darwin27" (macOS product major) / "win32-10.0.26100" / "linux6". */
export function osKey(): string {
  if (process.platform === "darwin") {
    try {
      return "darwin" + execFileSync("sw_vers", ["-productVersion"], { encoding: "utf8" }).trim().split(".")[0];
    } catch {
      return "darwin";
    }
  }
  if (process.platform === "win32") return "win32-" + os.release();
  return process.platform + os.release().split(".")[0];
}

export const pinnedProject = `chrome-${pinned.chrome}-${pinned.os}-${pinned.arch}`;
