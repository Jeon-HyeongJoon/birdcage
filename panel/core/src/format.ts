// Display helpers (Korean UI).
import type { JobState, JobSnapshot } from "./protocol";

export const STATE_LABEL: Record<JobState, string> = {
  queued: "대기",
  probing: "분석 중",
  downloading: "다운로드",
  normalizing: "Premiere용 변환",
  done: "완료",
  failed: "실패",
  cancelled: "취소됨",
};

export function bytes(n?: number): string {
  if (!n || n <= 0) return "";
  const units = ["B", "KB", "MB", "GB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`;
}

export function speed(bps?: number): string {
  return bps && bps > 0 ? `${bytes(bps)}/s` : "";
}

export function eta(sec?: number): string {
  if (!sec || sec <= 0 || !isFinite(sec)) return "";
  const s = Math.round(sec);
  return s >= 60 ? `${Math.floor(s / 60)}분 ${s % 60}초 남음` : `${s}초 남음`;
}

/** Overall 0..1 progress across stages for a single progress bar. */
export function overallProgress(j: JobSnapshot): number {
  const p = j.progress >= 0 ? j.progress : 0;
  switch (j.state) {
    case "queued":
      return 0;
    case "probing":
      return 0.05;
    case "downloading":
      return 0.05 + 0.75 * p;
    case "normalizing":
      return 0.8 + 0.2 * p;
    default:
      return 1;
  }
}

export function title(j: JobSnapshot): string {
  return j.info?.title || j.url;
}

export function mediaSummary(j: JobSnapshot): string {
  const v = j.media?.video;
  const a = j.media?.audio;
  const parts: string[] = [];
  if (v) parts.push(`${v.width}×${v.height} ${v.codec.toUpperCase()} ${Math.round(v.fps * 100) / 100}fps`);
  if (a) parts.push(`${a.codec.toUpperCase()} ${a.sampleRate / 1000}kHz`);
  if (j.plan) parts.push(planLabel(j.plan));
  return parts.join(" · ");
}

/** What normalization did, in editor terms. */
export function planLabel(p: NonNullable<JobSnapshot["plan"]>): string {
  if (p.passthrough) return "원본 그대로";
  if (p.audioOnly) return "WAV";
  if (p.video === "transcode") return `변환(${p.encoder ?? ""})`;
  if (p.aligned) return "구간 정렬(오디오만 재인코딩)";
  if (p.audio === "transcode") return "오디오만 재인코딩";
  return "리먹스";
}

/** False while a download reports no measurable progress (구간 다운로드는 ffmpeg가 끝에만 알린다). */
export function progressKnown(j: JobSnapshot): boolean {
  return !(j.state === "downloading" && j.progress < 0);
}

/** "0:19", "3:25", "1:02:03". */
export function duration(sec?: number): string {
  if (sec === undefined || sec < 0 || !isFinite(sec)) return "";
  const s = Math.floor(sec);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const ss = String(s % 60).padStart(2, "0");
  return h > 0 ? `${h}:${String(m).padStart(2, "0")}:${ss}` : `${m}:${ss}`;
}

/** "30fps", "29.97fps". */
export function fpsLabel(fps?: number): string {
  if (!fps || fps <= 0) return "";
  return `${Math.round(fps * 100) / 100}fps`;
}

/** Platform name for display (yt-dlp extractor key). */
export function platformLabel(extractor?: string): string {
  switch ((extractor || "").toLowerCase()) {
    case "youtube":
      return "YouTube";
    case "instagram":
      return "Instagram";
    case "tiktok":
      return "TikTok";
  }
  return extractor || "";
}
