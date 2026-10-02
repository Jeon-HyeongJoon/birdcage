// Choosing what to download: pure functions over media.info + the active sequence.
// UI·컨트롤러가 같은 규칙을 쓰도록 여기 둔다(테스트: test/choice.test.ts).
import type { Variant } from "./protocol";

/** The active Premiere sequence (ExtendScript birdcageSequenceInfo). */
export interface SequenceInfo {
  name: string;
  width: number;
  height: number;
  fps: number;
}

/** Short side of a frame (yt-dlp "res"): vertical 1080×1920 → 1080. */
export function shortSide(w: number, h: number): number {
  return Math.min(w, h);
}

/**
 * Recommends a variant: the highest resolution not above the target (the sequence's short side,
 * else the user's max quality), preferring ones that need no transcode at the same resolution.
 * Nothing at or below the target → the smallest available.
 */
export function recommendVariant(variants: Variant[] | undefined, target: number): Variant | undefined {
  if (!variants || variants.length === 0) return undefined;
  const fit = variants.filter((v) => v.res <= target);
  const pool = fit.length > 0 ? fit : [variants.reduce((a, b) => (b.res < a.res ? b : a))];
  return pool.slice().sort((a, b) => b.res - a.res || Number(a.transcode) - Number(b.transcode))[0];
}

/** Target resolution for recommendations. */
export function targetRes(sequence: SequenceInfo | null | undefined, maxHeight: number): number {
  return sequence && sequence.width > 0 && sequence.height > 0 ? shortSide(sequence.width, sequence.height) : maxHeight;
}

/**
 * True when a source frame rate does not map cleanly onto the sequence: not equal (±0.5%) and
 * not an integer multiple/fraction (60→30 drops every other frame evenly; 25→29.97 judders).
 */
export function fpsMismatch(source: number | undefined, sequence: number | undefined): boolean {
  if (!source || !sequence || source <= 0 || sequence <= 0) return false;
  const r = source >= sequence ? source / sequence : sequence / source;
  return Math.abs(r - Math.round(r)) / r > 0.005;
}

const CODEC_LABEL: Record<string, string> = { h264: "H.264", hevc: "HEVC", vp9: "VP9", av1: "AV1", aac: "AAC", opus: "Opus" };

/** Codec family of a yt-dlp tag (mirror of helper engine.CodecFamily). */
export function codecFamily(tag: string | undefined): string {
  const t = (tag || "").toLowerCase();
  if (t.startsWith("avc") || t.startsWith("h264")) return "h264";
  if (t.startsWith("hvc1") || t.startsWith("hev1") || t.startsWith("hevc") || t.startsWith("h265") || t === "bytevc1") return "hevc";
  if (t.startsWith("vp09") || t.startsWith("vp9")) return "vp9";
  if (t.startsWith("av01") || t === "av1") return "av1";
  if (t.startsWith("mp4a") || t === "aac") return "aac";
  return t;
}

export function codecLabel(tag: string | undefined): string {
  const f = codecFamily(tag);
  return CODEC_LABEL[f] ?? f.toUpperCase();
}

/** "1080p", "1080p60", "2160p (4K)". */
export function variantLabel(v: Pick<Variant, "res" | "fps">): string {
  let s = `${v.res}p`;
  if (v.fps && v.fps > 30.5) s += Math.round(v.fps);
  return s;
}

/** True for portrait sources (width < height). */
export function isVertical(v: Pick<Variant, "width" | "height">): boolean {
  return !!v.width && !!v.height && v.width < v.height;
}
