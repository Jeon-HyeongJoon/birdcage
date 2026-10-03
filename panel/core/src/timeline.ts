// Timeline math for the preview: view window, zoom, handles. 순수 함수(테스트: test/timeline.test.ts).
import type { DraftRange } from "./controller";

/** Visible part of the source, in seconds. */
export interface View {
  start: number;
  end: number;
}

/** Narrowest view: 2 s (프레임 단위로 고를 수 있을 만큼 확대). */
export const MIN_SPAN = 2;
/** Shortest selection. */
export const MIN_CLIP = 0.1;

export function fullView(duration: number): View {
  return { start: 0, end: Math.max(duration, MIN_SPAN) };
}

/** Keeps the view inside [0, duration] with at least MIN_SPAN (or the whole duration if shorter). */
export function clampView(v: View, duration: number): View {
  const total = Math.max(duration, MIN_SPAN);
  let span = Math.min(Math.max(v.end - v.start, Math.min(MIN_SPAN, total)), total);
  let start = Math.min(Math.max(0, v.start), total - span);
  if (!isFinite(start)) start = 0;
  if (!isFinite(span)) span = total;
  return { start, end: start + span };
}

/** Zooms by factor (>1 = in) keeping the time at `anchor` under the same pixel. */
export function zoomView(v: View, factor: number, anchor: number, duration: number): View {
  const span = (v.end - v.start) / factor;
  const ratio = v.end > v.start ? (anchor - v.start) / (v.end - v.start) : 0.5;
  return clampView({ start: anchor - ratio * span, end: anchor - ratio * span + span }, duration);
}

export function panView(v: View, dt: number, duration: number): View {
  return clampView({ start: v.start + dt, end: v.end + dt }, duration);
}

/** Fits the selection with 15% margin on each side. */
export function fitView(r: DraftRange, duration: number): View {
  if (!r.on || r.start === undefined || r.end === undefined) return fullView(duration);
  const pad = (r.end - r.start) * 0.15;
  return clampView({ start: r.start - pad, end: r.end + pad }, duration);
}

/** Keeps t visible (pans when playback runs past the edge). */
export function follow(v: View, t: number, duration: number): View {
  if (t >= v.start && t <= v.end) return v;
  const span = v.end - v.start;
  return clampView({ start: t - span * 0.1, end: t - span * 0.1 + span }, duration);
}

export function xToTime(x: number, width: number, v: View): number {
  return v.start + (Math.max(0, Math.min(width, x)) / Math.max(1, width)) * (v.end - v.start);
}

export function timeToX(t: number, width: number, v: View): number {
  return ((t - v.start) / Math.max(1e-9, v.end - v.start)) * width;
}

export type Handle = "in" | "out";

/** In/Out to draw: the selection, or the whole video when no range is set (손잡이는 항상 보인다). */
export function handlesOf(r: DraftRange, duration: number): { start: number; end: number } {
  return r.on && r.start !== undefined && r.end !== undefined ? { start: r.start, end: r.end } : { start: 0, end: duration };
}

/** Which handle is under x (within tol px), preferring the nearer one. */
export function hitHandle(x: number, width: number, v: View, r: DraftRange, tolPx = 6, duration = 0): Handle | null {
  const h = r.on && r.start !== undefined && r.end !== undefined ? { start: r.start, end: r.end } : duration > 0 ? { start: 0, end: duration } : null;
  if (!h) return null;
  const dIn = Math.abs(x - timeToX(h.start, width, v));
  const dOut = Math.abs(x - timeToX(h.end, width, v));
  if (Math.min(dIn, dOut) > tolPx) return null;
  return dIn <= dOut ? "in" : "out";
}

/** Moves one handle to t, keeping In < Out by MIN_CLIP inside [0, duration]. */
export function moveHandle(r: DraftRange, which: Handle, t: number, duration: number): DraftRange {
  const start = r.on && r.start !== undefined ? r.start : 0;
  const end = r.on && r.end !== undefined ? r.end : duration;
  if (which === "in") return { on: true, start: round(Math.max(0, Math.min(t, end - MIN_CLIP))), end };
  return { on: true, start, end: round(Math.min(duration, Math.max(t, start + MIN_CLIP))) };
}

/** Sets In (or Out) at t like Premiere's I/O keys: the other point stays unless it would cross. */
export function markAt(r: DraftRange, which: Handle, t: number, duration: number): DraftRange {
  const start = r.on && r.start !== undefined ? r.start : 0;
  const end = r.on && r.end !== undefined ? r.end : duration;
  if (which === "in") return { on: true, start: round(Math.max(0, Math.min(t, duration - MIN_CLIP))), end: t >= end ? duration : end };
  return { on: true, start: t <= start ? 0 : start, end: round(Math.min(duration, Math.max(t, MIN_CLIP))) };
}

/** Tick step (s) for labels about every `px` pixels. */
export function tickStep(v: View, width: number, px = 80): number {
  const raw = ((v.end - v.start) / Math.max(1, width)) * px;
  const steps = [0.1, 0.2, 0.5, 1, 2, 5, 10, 15, 30, 60, 120, 300, 600, 1800, 3600];
  return steps.find((s) => s >= raw) ?? 3600;
}

const round = (t: number) => Math.round(t * 1000) / 1000;
