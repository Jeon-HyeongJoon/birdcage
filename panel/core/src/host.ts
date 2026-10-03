// HostAdapter: everything that differs between CEP (Premiere ≤25, 26 for now) and UXP (26+).
// 코어는 이 인터페이스만 알고, 호스트별 구현은 panel/cep(·이후 panel/uxp)에 둔다.
import type { SequenceInfo } from "./choice";
import type { HelperEndpoint } from "./client";
import type { Clip } from "./protocol";
import type { Host } from "./protocol";

export interface ImportResult {
  ok: boolean;
  error?: string;
  /** Bin path the item was placed in, e.g. ["Birdcage", "Youtube"]. */
  bin: string[];
  /** In/Out on the item: set and read back equal, could not be verified, or not supported by this version. */
  inout?: "set" | "unverified" | "unsupported";
  /** Non-fatal problems to show (e.g. In/Out not verified). */
  warnings?: string[];
  /** Timeline placement outcome: ok (new empty track at the playhead), source (no sequence → Source Monitor), … */
  placed?: "ok" | "source" | "unverified" | "no_sequence" | "no_free_track" | "failed";
  /** Subtitle sidecar: imported into the bin, and as a caption track when placed on the timeline. */
  captions?: "imported" | "track" | "unsupported" | "before_start" | "failed";
}


export interface ImportOptions {
  /** Requested range inside the file → In/Out on the imported item. */
  clip?: Clip;
  /** Open the item in the Source Monitor after import. */
  openSource?: boolean;
  /**
   * Put the item at the playhead on a new empty track (existing clips are never touched); with no
   * open sequence it opens in the Source Monitor. 사용자 지시(2026-10-03): 배치 선택 없이 항상 이렇게.
   */
  place?: "newTrack";
  /** The file has video (audio-only items only need a free audio track). */
  hasVideo?: boolean;
  /** SRT sidecar to import next to the media (caption track if placed on the timeline). */
  subtitle?: string;
}

export interface HostAdapter {
  info(): Host;
  /** Starts (or reuses) the helper process and returns its endpoint. */
  startHelper(): Promise<HelperEndpoint>;
  /** Stops the helper this panel started (no-op if not owned). */
  stopHelper(): void;
  /** Imports a file into the open project under the given bin path. */
  importFile(path: string, bin: string[], opts?: ImportOptions): Promise<ImportResult>;
  /** Shows the file in Finder/Explorer (optional). */
  revealFile?(path: string): void;
  /** Keeps the preview shortcuts in the panel instead of the host app (optional). */
  captureKeys?(): void;
  /** The active sequence, or null when none is open (optional: recommendations fall back to settings). */
  sequenceInfo?(): Promise<SequenceInfo | null>;
}
