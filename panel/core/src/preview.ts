// Preview helpers: storyboard tiles, waveform samples, local file URLs. 순수 함수(테스트: test/preview.test.ts).
import type { Peaks, Storyboard } from "./protocol";

/** One storyboard tile: the sprite image and the tile's pixel box inside it. */
export interface Tile {
  url: string;
  x: number;
  y: number;
  w: number;
  h: number;
  /** Sprite size in pixels (for CSS background-size). */
  sheetW: number;
  sheetH: number;
}

/** The tile showing time t (clamped to the storyboard). */
export function tileAt(sb: Storyboard, t: number): Tile | undefined {
  if (!sb.sprites.length || sb.intervalSec <= 0) return undefined;
  const per = sb.cols * sb.rows;
  let i = Math.max(0, Math.floor(t / sb.intervalSec));
  const total = sb.sprites.reduce((n, s) => n + Math.max(1, Math.ceil(s.durationSec / sb.intervalSec - 1e-6)), 0);
  i = Math.min(i, Math.max(0, Math.min(total, per * sb.sprites.length) - 1));
  const sprite = Math.min(sb.sprites.length - 1, Math.floor(i / per));
  const k = i - sprite * per;
  return {
    url: sb.sprites[sprite].url,
    x: (k % sb.cols) * sb.width,
    y: Math.floor(k / sb.cols) * sb.height,
    w: sb.width,
    h: sb.height,
    sheetW: sb.cols * sb.width,
    sheetH: sb.rows * sb.height,
  };
}

/** Picks the storyboard for a target tile height: the smallest one at least that tall, else the largest. */
export function pickStoryboard(list: Storyboard[] | undefined, minHeight: number): Storyboard | undefined {
  if (!list || list.length === 0) return undefined;
  const sorted = list.slice().sort((a, b) => a.height - b.height);
  return sorted.find((s) => s.height >= minHeight) ?? sorted[sorted.length - 1];
}

/** Decodes waveform bytes (0–255 linear peaks). */
export function decodePeaks(p: Peaks): Uint8Array {
  const bin = atob(p.data);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

/** Linear peak 0–255 → display height 0..1 on a dB scale (−48 dB → 0, 0 dB → 1). 조용한 구간도 보이게. */
export function peakLevel(v: number, floorDb = -48): number {
  if (v <= 0) return 0;
  const db = 20 * Math.log10(v / 255);
  return Math.max(0, Math.min(1, 1 - db / floorDb));
}

/**
 * Line points for the waveform between [t0, t1] at `width` columns: the max level of the buckets in
 * each column (선그래프: 막대 대신 열마다 한 점).
 */
export function waveLine(p: Peaks, bytes: Uint8Array, t0: number, t1: number, width: number): number[] {
  const out: number[] = [];
  if (width <= 0 || t1 <= t0 || p.bucketSec <= 0) return out;
  const span = (t1 - t0) / width;
  for (let x = 0; x < width; x++) {
    const a = Math.floor((t0 + x * span) / p.bucketSec);
    const b = Math.max(a + 1, Math.floor((t0 + (x + 1) * span) / p.bucketSec));
    let m = 0;
    for (let i = Math.max(0, a); i < Math.min(bytes.length, b); i++) m = Math.max(m, bytes[i]);
    out.push(peakLevel(m));
  }
  return out;
}

/** file:// URL for a local path (macOS /a/b → file:///a/b, Windows C:\a → file:///C:/a). */
export function fileUrl(path: string): string {
  if (/^(https?|data|file):/.test(path)) return path;
  const p = path.replace(/\\/g, "/");
  return "file://" + (p.startsWith("/") ? "" : "/") + encodeURI(p).replace(/#/g, "%23").replace(/\?/g, "%3F");
}
