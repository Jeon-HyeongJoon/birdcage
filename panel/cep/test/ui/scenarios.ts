import type { JobSnapshot, State, ToolStatus } from "@birdcage/core";

// 결정적인 썸네일(네트워크 없음): 16:9 단색 + 대각선 SVG.
const thumb =
  "data:image/svg+xml;utf8," +
  encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="192" height="108"><rect width="192" height="108" fill="#3a5a40"/><path d="M0 108L192 0" stroke="#a3b18a" stroke-width="6"/></svg>');

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
  settings: { autoImport: false, maxHeight: 1080, placement: "track" },
  draft: { url: "", status: "idle", range: { on: false } },
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
      settings: { autoImport: true, maxHeight: 1080, placement: "project" },
      draft: { url: "", status: "idle", range: { on: false } },
    },
    missingTools: [],
  },
  probed: {
    state: {
      ...connected([{ snap: done }]),
      sequence: { name: "Seq 01", width: 1920, height: 1080, fps: 29.97 },
      draft: {
        url: "https://www.youtube.com/watch?v=jNQXAC9IVRw&t=5",
        status: "ready",
        range: { on: true, start: 5.3, end: 12.1 },
        subtitle: "en",
        probeId: "p1",
        formatId: "137+140",
        recommendedId: "137+140",
        info: {
          id: "jNQXAC9IVRw",
          title: "Me at the zoo — 첫 번째 업로드 영상, 제목이 길면 두 줄까지 보이고 나머지는 잘린다",
          uploader: "jawed",
          extractor: "Youtube",
          webpageUrl: "https://www.youtube.com/watch?v=jNQXAC9IVRw",
          durationSec: 19,
          thumbnail: thumb,
          variants: [
            { formatId: "313+140", res: 2160, width: 3840, height: 2160, fps: 30, vcodec: "vp09.00.51.08", sizeBytes: 210_000_000, transcode: true },
            { formatId: "137+140", res: 1080, width: 1920, height: 1080, fps: 30, vcodec: "avc1.640028", sizeBytes: 48_000_000, transcode: false },
            { formatId: "136+140", res: 720, width: 1280, height: 720, fps: 30, vcodec: "avc1.4d401f", sizeBytes: 21_500_000, transcode: false },
            { formatId: "18", res: 360, width: 640, height: 360, fps: 30, vcodec: "avc1.42001E", transcode: false },
          ],
          bestAudio: { id: "140", ext: "m4a", acodec: "mp4a.40.2", sizeBytes: 309_288 },
          subtitles: [
            { lang: "de", name: "German" },
            { lang: "en", name: "English" },
            { lang: "en-orig", name: "English (Original)", auto: true },
          ],
        },
      },
    },
    missingTools: [],
  },
};
