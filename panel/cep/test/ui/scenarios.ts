import type { JobSnapshot, State, ToolStatus } from "@birdcage/core";

export type Scenario = {
  state: State;
  missingTools: string[];
};

const tools: ToolStatus[] = [
  { name: "yt-dlp", path: "/tools/yt-dlp", version: "2026.10.01", source: "installed" },
  { name: "ffmpeg", path: "/tools/ffmpeg", version: "8.0", source: "path" },
  { name: "ffprobe", path: "/tools/ffprobe", version: "8.0", source: "path" },
  { name: "deno", path: "/tools/deno", version: "2.5.0", source: "installed" },
];

const baseJob = (jobId: string, state: JobSnapshot["state"]): JobSnapshot => ({
  jobId,
  url: `https://www.youtube.com/watch?v=${jobId}`,
  state,
  options: { maxHeight: 1080, profile: "compat" },
  progress: -1,
  createdAt: "2026-10-01T12:00:00Z",
  updatedAt: "2026-10-01T12:00:05Z",
});

const downloading: JobSnapshot = {
  ...baseJob("downloading", "downloading"),
  progress: 0.42,
  download: {
    status: "downloading",
    downloadedBytes: 42_000_000,
    totalBytes: 100_000_000,
    speedBps: 5_250_000,
    etaSec: 11,
    fraction: 0.42,
  },
  info: {
    id: "downloading",
    title: "편집용 인터뷰 클립",
    uploader: "Birdcage Fixture",
    extractor: "Youtube",
    webpageUrl: "https://www.youtube.com/watch?v=downloading",
  },
};

const done: JobSnapshot = {
  ...baseJob("done", "done"),
  progress: 1,
  info: {
    id: "done",
    title: "완료된 제품 영상",
    extractor: "Youtube",
    webpageUrl: "https://www.youtube.com/watch?v=done",
  },
  media: {
    container: "mov,mp4,m4a,3gp,3g2,mj2",
    durationSec: 19,
    sizeBytes: 2_150_000,
    video: { index: 0, codec: "h264", pixFmt: "yuv420p", width: 1920, height: 1080, fps: 30 },
    audio: { index: 1, codec: "aac", sampleRate: 48_000, channels: 2 },
  },
  file: "/fixtures/Birdcage/Youtube/done.mp4",
};

const failed: JobSnapshot = {
  ...baseJob("failed", "failed"),
  progress: 1,
  info: {
    id: "failed",
    title: "접근할 수 없는 영상",
    extractor: "Youtube",
    webpageUrl: "https://www.youtube.com/watch?v=failed",
  },
  failure: {
    kind: "private",
    message: "비공개 영상입니다.",
    detail: "fixture: private video",
    policy: "stop",
  },
};

const connected = (jobs: State["jobs"], availableTools = tools): State => ({
  connection: "connected",
  helper: { helperVersion: "0.1.0-fixture", profile: "compat", libraryDir: "/fixtures/Birdcage" },
  endpoint: { port: 31_415, token: "fixture-token" },
  tools: availableTools,
  toolsInstalling: false,
  jobs,
  settings: { autoImport: false, maxHeight: 1080 },
});

export const scenarios: Record<string, Scenario> = {
  workflow: {
    state: connected([{ snap: downloading }, { snap: done }, { snap: failed }]),
    missingTools: [],
  },
  "missing-tools": {
    state: connected([], []),
    missingTools: ["yt-dlp", "ffmpeg", "ffprobe", "deno"],
  },
  "connection-failed": {
    state: {
      connection: "failed",
      error: "Helper에 연결할 수 없습니다.",
      tools: [],
      toolsInstalling: false,
      jobs: [],
      settings: { autoImport: true, maxHeight: 1080 },
    },
    missingTools: [],
  },
};
