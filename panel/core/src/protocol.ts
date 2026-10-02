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
  };
  audio?: {
    index: number;
    codec: string;
    profile?: string;
    sampleRate: number;
    channels: number;
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
  options: { maxHeight: number; profile: Profile | "" };
  progress: number; // stage progress 0..1, -1 unknown
  download?: DownloadProgress;
  info?: SourceInfo;
  media?: MediaInfo;
  plan?: Plan;
  file?: string;
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
