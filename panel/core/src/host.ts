// HostAdapter: everything that differs between CEP (Premiere ≤25, 26 for now) and UXP (26+).
// 코어는 이 인터페이스만 알고, 호스트별 구현은 panel/cep(·이후 panel/uxp)에 둔다.
import type { HelperEndpoint } from "./client";
import type { Host } from "./protocol";

export interface ImportResult {
  ok: boolean;
  error?: string;
  /** Bin path the item was placed in, e.g. ["Birdcage", "Youtube"]. */
  bin: string[];
}

export interface HostAdapter {
  info(): Host;
  /** Starts (or reuses) the helper process and returns its endpoint. */
  startHelper(): Promise<HelperEndpoint>;
  /** Stops the helper this panel started (no-op if not owned). */
  stopHelper(): void;
  /** Imports a file into the open project under the given bin path. */
  importFile(path: string, bin: string[]): Promise<ImportResult>;
  /** Shows the file in Finder/Explorer (optional). */
  revealFile?(path: string): void;
}
