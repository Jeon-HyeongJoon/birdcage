// Panel ↔ Helper protocol v0 — TS mirror of helper/internal/protocol.
// 계약의 기준은 /protocol/fixtures/*.json 이다. Go 쪽 필드를 바꾸면 픽스처가 바뀌고
// test/fixtures.test.ts 가 실패하므로 여기 타입과 validators.ts를 함께 고친다.

import type { FailureKind, FailurePolicy } from "./failures.gen";

export const PROTOCOL_VERSION = 0;

export interface Envelope<T = unknown> {
  v: number;
  type: string;
  id?: string;
  data?: T;
}

export type HostAPI = "cep" | "uxp";
export type HostOS = "mac" | "win";

export interface Host {
  app: string; // "PPRO"
  version: string; // "15.2.0"
  api: HostAPI;
  os: HostOS;
}

export interface Hello {
  client: string;
  clientVersion: string;
  host: Host;
}

export type ToolName = "yt-dlp" | "ffmpeg" | "ffprobe" | "deno" | "node";

export interface ToolStatus {
  name: ToolName;
  path: string;
  version: string;
  source: string; // override | installed | bundled | path | ""
  error?: string;
}

export type Profile = "compat" | "modern";

export interface HelloOK {
  helperVersion: string;
  protocol: number;
  profile: Profile;
  libraryDir: string;
  tools: ToolStatus[];
  jobs: JobSnapshot[];
}

export interface JobCreate {
  url: string;
  maxHeight?: number;
  /** media.info probeId: the helper downloads from the cached probe without re-extraction. */
  probeId?: string;
  /** Chosen variant (Variant.formatId). */
  formatId?: string;
  /** Download only this part of the source (the helper adds 1s handles). */
  range?: TimeRange;
  /** Best audio only, written as WAV (PCM 16-bit 48 kHz). */
  audioOnly?: boolean;
  /** One subtitle track as an SRT sidecar (failure is only a warning). */
  subtitles?: SubtitleRequest;
  /** Browse list the link came from ("related:youtube"); the helper records it for the fitness review. */
  via?: string;
}

export interface SubtitleRequest {
  lang: string;
  auto?: boolean;
}

/** A span of the source timeline in seconds. */
export interface TimeRange {
  startSec: number;
  endSec: number;
}

/** The requested range inside the output file (set as In/Out on the Premiere item). */
export interface Clip {
  inSec: number;
  outSec: number;
}

export interface MediaProbe {
  url: string;
  maxHeight?: number;
  via?: string;
}

/** Where a browse list comes from: YouTube's own "up next", the same channel, or a title search. */
export type RelatedSource = "youtube" | "channel" | "search";

export interface RelatedFetch {
  probeId: string;
  source?: RelatedSource;
}

/** One video in a browse list (platform text such as views/age is in meta, as is). */
export interface RelatedItem {
  id: string;
  url: string;
  title: string;
  channel?: string;
  durationSec?: number;
  thumbnail?: string;
  meta?: string;
  views?: number;
}

/** Answer to related.fetch. source differs from requested when the helper fell back. */
export interface RelatedListMsg {
  probeId: string;
  requested: RelatedSource;
  source?: RelatedSource;
  items: RelatedItem[];
  error?: string;
}

/** Answer to media.probe: info on success, a classified failure otherwise. */
export interface MediaInfoMsg {
  url: string;
  probeId?: string;
  info?: SourceInfo;
  failure?: Failure;
}

export interface JobAccepted {
  jobId: string;
}

export interface JobCancel {
  jobId: string;
}

export type JobState = "queued" | "probing" | "downloading" | "normalizing" | "done" | "failed" | "cancelled";

export const TERMINAL_STATES: readonly JobState[] = ["done", "failed", "cancelled"];

export interface DownloadProgress {
  status: string;
  downloadedBytes: number;
  totalBytes?: number;
  totalEstimated?: boolean;
  speedBps?: number;
  etaSec?: number;
  fragIndex?: number;
  fragCount?: number;
  formatId?: string;
  fraction: number; // 0..1, -1 = unknown
}

export interface MediaFormat {
  id: string;
  ext?: string;
  vcodec?: string;
  acodec?: string;
  width?: number;
  height?: number;
  fps?: number;
  sizeBytes?: number;
}

/** One selectable quality (video + the audio it merges with). */
export interface Variant {
  formatId: string; // yt-dlp -f value, e.g. "137+140"
  res: number; // short side: vertical 1080×1920 → 1080
  width?: number;
  height?: number;
  fps?: number;
  vcodec: string;
  acodec?: string;
  hdr?: boolean;
  sizeBytes?: number; // 0/absent = unknown
  transcode: boolean; // video codec not Premiere-ready for this session's profile
}

export interface Subtitle {
  lang: string;
  name?: string;
  auto?: boolean;
}

/** Thumbnail sprite sheets: each sprite holds cols×rows tiles, one every intervalSec. */
export interface Storyboard {
  width: number;
  height: number;
  cols: number;
  rows: number;
  intervalSec: number;
  sprites: { url: string; startSec: number; durationSec: number }[];
}

/** Waveform: one linear peak per bucket, 0–255, base64 bytes. */
export interface Peaks {
  bucketSec: number;
  count: number;
  durationSec: number;
  data: string;
}

export type PreviewStage = "audio" | "video" | "ready" | "failed";

/** preview.state: staged preview of a probed link (storyboard → waveform → playable video). */
export interface PreviewState {
  probeId: string;
  stage: PreviewStage;
  progress: number; // current download 0..1, -1 unknown
  peaks?: Peaks;
  video?: { path: string; width: number; height: number; fps: number; durationSec: number };
  /** Generated locally when the platform has no storyboard (local file paths). */
  storyboard?: Storyboard;
  cached?: boolean;
  error?: string;
}

/** Source metadata from the engine (yt-dlp). */
export interface SourceInfo {
  id: string;
  title: string;
  uploader?: string;
  extractor: string; // Youtube | Instagram | TikTok | Generic …
  webpageUrl: string;
  durationSec?: number;
  isLive?: boolean;
  selected?: MediaFormat[];
  thumbnail?: string;
  /** Only in media.info (job snapshots omit the choice lists). */
  variants?: Variant[];
  bestAudio?: MediaFormat;
  subtitles?: Subtitle[];
  /** Platform storyboards (YouTube), smallest tiles first. */
  storyboards?: Storyboard[];
}

/** ffprobe summary of the final file. */
export interface MediaInfo {
  container: string;
  durationSec: number;
  sizeBytes: number;
  video?: {
    index: number;
    codec: string;
    profile?: string;
    pixFmt: string;
    width: number;
    height: number;
    fps: number;
    colorTransfer?: string;
    colorPrimaries?: string;
    bitRate?: number;
    startSec?: number;
  };
  audio?: {
    index: number;
    codec: string;
    profile?: string;
    sampleRate: number;
    channels: number;
    startSec?: number;
  };
}

export type StreamAction = "copy" | "transcode" | "none";

export interface Plan {
  profile: Profile;
  video: StreamAction;
  audio: StreamAction;
  encoder?: string;
  fps?: string;
  passthrough: boolean;
  reasons: string[];
  warnings?: string[];
  bitrateBps?: number;
  aligned?: boolean;
  alignShiftSec?: number;
  audioOnly?: boolean;
  trimStartSec?: number;
  trimDurSec?: number;
}

// 실패 분류표(FailureKind, FailurePolicy)는 specs/failures.json이 SSOT다(make gen → failures.gen.ts).
export type { FailureKind, FailurePolicy };

export interface Failure {
  kind: FailureKind;
  message: string; // 사용자용 한국어
  detail?: string;
  policy: FailurePolicy;
}

export interface StageTiming {
  stage: string;
  ms: number;
}

export interface JobSnapshot {
  jobId: string;
  url: string;
  state: JobState;
  options: {
    maxHeight: number;
    profile: Profile | "";
    probeId?: string;
    formatId?: string;
    range?: TimeRange;
    audioOnly?: boolean;
    subtitles?: SubtitleRequest;
  };
  progress: number; // stage progress 0..1, -1 unknown
  download?: DownloadProgress;
  info?: SourceInfo;
  media?: MediaInfo;
  plan?: Plan;
  file?: string;
  clip?: Clip;
  /** SRT sidecar next to file, on the file's timeline. */
  subtitleFile?: string;
  failure?: Failure;
  warnings?: string[];
  timings?: StageTiming[];
  createdAt: string;
  updatedAt: string;
}

export interface JobEvent {
  job: JobSnapshot;
}

export interface JobList {
  jobs: JobSnapshot[];
}

export interface ToolsEnsure {
  channel?: "stable" | "nightly";
}

export interface ToolsStatus {
  installing: boolean;
  tools: ToolStatus[];
  error?: string;
}

export interface ProtocolError {
  code: string; // bad_request | unknown_type | internal
  message: string;
}

/** Messages the panel sends. */
export interface ClientMessages {
  hello: Hello;
  "job.create": JobCreate;
  "job.cancel": JobCancel;
  "job.list": undefined;
  "tools.ensure": ToolsEnsure | undefined;
  "media.probe": MediaProbe;
  "preview.open": { probeId: string };
  "preview.close": { probeId: string };
  "related.fetch": RelatedFetch;
}

/** Messages the helper sends. */
export interface ServerMessages {
  "hello.ok": HelloOK;
  "job.accepted": JobAccepted;
  "job.update": JobEvent;
  "job.done": JobEvent;
  "job.failed": JobEvent;
  "job.list.ok": JobList;
  "tools.status": ToolsStatus;
  "media.info": MediaInfoMsg;
  "preview.state": PreviewState;
  "related.list": RelatedListMsg;
  error: ProtocolError;
}

export type ServerType = keyof ServerMessages;

export function encode<K extends keyof ClientMessages>(type: K, id: string | undefined, data: ClientMessages[K]): string {
  const env: Envelope<ClientMessages[K]> = { v: PROTOCOL_VERSION, type };
  if (id) env.id = id;
  if (data !== undefined) env.data = data;
  return JSON.stringify(env);
}

export class ProtocolDecodeError extends Error {}

export function decode(raw: string): Envelope {
  let env: Envelope;
  try {
    env = JSON.parse(raw);
  } catch (e) {
    throw new ProtocolDecodeError("invalid JSON: " + (e as Error).message);
  }
  if (!env || typeof env !== "object" || env.v !== PROTOCOL_VERSION || typeof env.type !== "string") {
    throw new ProtocolDecodeError("unsupported envelope: " + raw.slice(0, 120));
  }
  return env;
}
