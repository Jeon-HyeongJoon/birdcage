// UI 골든 시나리오: 실제 Controller를 가짜 Helper 메시지로 구동해 각 화면 상태를 만든다.
// 값은 모두 고정(시간·경로·버전·썸네일)이라 마스크 없이 결정적으로 렌더링된다.
import type {
  Controller,
  HelloOK,
  HostAdapter,
  ImportResult,
  JobSnapshot,
  MediaInfoMsg,
  Peaks,
  PreviewState,
  SequenceInfo,
  SourceInfo,
  Storyboard,
  ToolStatus,
} from "@birdcage/core";
import type { FakeHelper } from "./fake-helper";

export interface Scenario {
  /** What the golden shows (for reviewers). */
  about: string;
  hello?: Partial<HelloOK>;
  probe?: (url: string) => MediaInfoMsg | undefined;
  /** preview.state messages after preview.open. */
  preview?: (probeId: string) => Omit<PreviewState, "probeId">[];
  startHelper?: HostAdapter["startHelper"];
  sequence?: SequenceInfo | null;
  importFile?: (path: string) => ImportResult;
  /** Drives the controller after connecting (start() is awaited unless startHelper never resolves). */
  run?: (ctl: Controller, helper: FakeHelper) => Promise<void> | void;
}

const tools: ToolStatus[] = [
  { name: "yt-dlp", path: "/tools/yt-dlp", version: "2026.08.19", source: "installed" },
  { name: "ffmpeg", path: "/tools/ffmpeg", version: "8.1.1", source: "path" },
  { name: "ffprobe", path: "/tools/ffprobe", version: "8.1.1", source: "path" },
  { name: "deno", path: "/tools/deno", version: "2.9.7", source: "installed" },
];

export const hello: HelloOK = {
  helperVersion: "0.1.0-fixture",
  protocol: 0,
  profile: "compat",
  libraryDir: "/Users/editor/Movies/Birdcage",
  tools,
  jobs: [],
};

// 결정적인 썸네일(네트워크 없음).
const thumb = (fill: string) =>
  "data:image/svg+xml;utf8," +
  encodeURIComponent(
    `<svg xmlns="http://www.w3.org/2000/svg" width="192" height="108"><rect width="192" height="108" fill="${fill}"/><path d="M0 108L192 0" stroke="#a3b18a" stroke-width="6"/></svg>`,
  );

// 결정적인 스토리보드 시트: 10×10 타일(80×45), 타일마다 다른 색과 번호(네트워크 없음).
const sheet = (hueBase: number) => {
  let cells = "";
  for (let i = 0; i < 100; i++) {
    const x = (i % 10) * 80;
    const y = Math.floor(i / 10) * 45;
    cells += `<rect x="${x}" y="${y}" width="80" height="45" fill="hsl(${(hueBase + i * 7) % 360},35%,32%)"/>`;
    cells += `<rect x="${x + 6}" y="${y + 30}" width="${(i % 10) * 6 + 8}" height="6" fill="#cfd8c0"/>`;
    cells += `<text x="${x + 6}" y="${y + 22}" font-family="monospace" font-size="16" fill="#eef">${i}</text>`;
  }
  return "data:image/svg+xml;utf8," + encodeURIComponent(`<svg xmlns="http://www.w3.org/2000/svg" width="800" height="450">${cells}</svg>`);
};

const zooBoard: Storyboard = { width: 80, height: 45, cols: 10, rows: 10, intervalSec: 0.5, sprites: [{ url: sheet(90), startSec: 0, durationSec: 19 }] };

// 결정적인 파형: 말소리처럼 오르내리는 포락선(0–255), 10ms 버킷.
const peaks = (dur: number): Peaks => {
  const n = Math.round(dur * 100);
  let bin = "";
  for (let i = 0; i < n; i++) {
    const env = 0.5 + 0.5 * Math.sin(i / 23) * Math.cos(i / 57);
    const v = Math.max(0, Math.min(255, Math.round(255 * Math.pow(10, (-40 + 40 * env) / 20) * (0.7 + 0.3 * Math.abs(Math.sin(i * 1.7))))));
    bin += String.fromCharCode(v);
  }
  return { bucketSec: 0.01, count: n, durationSec: dur, data: btoa(bin) };
};

const zoo: SourceInfo = {
  id: "jNQXAC9IVRw",
  title: "Me at the zoo",
  uploader: "jawed",
  extractor: "Youtube",
  webpageUrl: "https://www.youtube.com/watch?v=jNQXAC9IVRw",
  durationSec: 19,
  thumbnail: thumb("#3a5a40"),
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
  storyboards: [zooBoard],
};

const vertical: SourceInfo = {
  id: "7400000000000000001",
  title: "세로 영상 — 업로더가 붙인 아주 긴 제목이 두 줄을 넘으면 말줄임으로 잘리고 카드 높이는 그대로 유지된다 #편집 #레퍼런스",
  uploader: "creator.kr",
  extractor: "TikTok",
  webpageUrl: "https://www.tiktok.com/@creator.kr/video/7400000000000000001",
  durationSec: 47,
  thumbnail: thumb("#5a3a4a"),
  variants: [
    { formatId: "bytevc1_1080p", res: 1080, width: 1080, height: 1920, fps: 25, vcodec: "hev1.2.4.L120.90", hdr: true, sizeBytes: 18_400_000, transcode: true },
    { formatId: "h264_720p", res: 720, width: 720, height: 1280, fps: 25, vcodec: "h264", sizeBytes: 9_100_000, transcode: false },
  ],
  subtitles: [],
};

const baseJob = (jobId: string, state: JobSnapshot["state"], extra: Partial<JobSnapshot> = {}): JobSnapshot => ({
  jobId,
  url: `https://www.youtube.com/watch?v=${jobId}`,
  state,
  options: { maxHeight: 1080, profile: "compat" },
  progress: -1,
  createdAt: "2026-10-03T12:00:00Z",
  updatedAt: "2026-10-03T12:00:05Z",
  ...extra,
});

const info = (id: string, title: string, extractor = "Youtube") => ({ id, title, extractor, webpageUrl: `https://www.youtube.com/watch?v=${id}` });

const settle = () => new Promise((r) => setTimeout(r, 0));

export const scenarios: Record<string, Scenario> = {
  empty: { about: "연결됨, 빈 화면(링크 입력 대기)" },
  starting: { about: "Helper 시작 중", startHelper: () => new Promise(() => undefined) },
  "connection-failed": {
    about: "Helper를 띄우지 못함 → 오류와 다시 시도",
    startHelper: () => Promise.reject(new Error("Helper 실행 파일이 없습니다: /Applications/Birdcage/bin/darwin/birdcage-helper")),
  },
  "missing-tools": { about: "도구 없음 → 설치 안내", hello: { tools: [] } },
  "tools-installing": {
    about: "도구 설치 중",
    hello: { tools: [] },
    run: (ctl) => ctl.installTools(),
  },
  probing: {
    about: "링크 붙여넣기 → 확인 중",
    probe: () => undefined,
    run: async (ctl) => {
      ctl.setUrl("https://www.youtube.com/watch?v=jNQXAC9IVRw");
      await waitFor(() => ctl.state.draft.status === "probing");
    },
  },
  "probe-failed": {
    about: "조회 실패(분류된 메시지) + 다시 확인",
    probe: (url) => ({ url, failure: { kind: "private", message: "비공개 영상입니다.", policy: "stop" } }),
    run: async (ctl) => {
      ctl.setUrl("https://www.youtube.com/watch?v=private");
      await waitFor(() => ctl.state.draft.status === "failed");
    },
  },
  probed: {
    about: "조회 완료 → 미리보기 받는 중: 스토리보드 정지 화면·필름스트립·선 파형, t= 링크로 In/Out 채움, 받기 옵션 요약",
    probe: (url) => ({ url, probeId: "p1", info: zoo }),
    preview: () => [
      { stage: "audio", progress: 0.4 },
      { stage: "video", progress: 0.35, peaks: peaks(19) },
    ],
    sequence: { name: "Seq 01", width: 1920, height: 1080, fps: 29.97 },
    run: async (ctl) => {
      ctl.setUrl("https://www.youtube.com/watch?v=jNQXAC9IVRw&t=5");
      await waitFor(() => ctl.state.draft.status === "ready");
    },
  },
  "preview-ready": {
    about: "미리보기 재생 준비 완료(실제 H.264 클립): In 위치 프레임, 선택 구간 강조·핸들·재생헤드",
    probe: (url) => ({ url, probeId: "p4", info: zoo }),
    sequence: { name: "Seq 01", width: 1920, height: 1080, fps: 29.97 },
    preview: () => [
      {
        stage: "ready",
        progress: 1,
        cached: true,
        peaks: peaks(19),
        video: { path: "http://127.0.0.1:4173/assets/preview.mp4", width: 320, height: 180, fps: 10, durationSec: 19 },
      },
    ],
    run: async (ctl) => {
      ctl.setUrl("https://www.youtube.com/watch?v=jNQXAC9IVRw");
      await waitFor(() => ctl.state.draft.preview?.stage === "ready");
      ctl.setRange({ on: true, start: 7.2, end: 12.5 });
    },
  },
  "preview-failed": {
    about: "미리보기 실패(분류된 메시지): 스토리보드·정보는 그대로 쓴다",
    probe: (url) => ({ url, probeId: "p5", info: zoo }),
    preview: () => [{ stage: "failed", progress: -1, error: "플랫폼이 요청을 일시적으로 제한했습니다. 잠시 후 다시 시도합니다." }],
    run: async (ctl) => {
      ctl.setUrl("https://www.youtube.com/watch?v=jNQXAC9IVRw");
      await waitFor(() => ctl.state.draft.preview?.stage === "failed");
    },
  },
  "probed-warnings": {
    about: "경고 배지: 변환 필요(HEVC 10bit)·HDR·세로·시퀀스 fps 다름, 긴 제목 말줄임, 스토리보드 없음(썸네일), 파형 받는 중",
    probe: (url) => ({ url, probeId: "p2", info: vertical }),
    preview: () => [{ stage: "audio", progress: 0.4 }],
    sequence: { name: "Vertical 1080", width: 1080, height: 1920, fps: 29.97 },
    run: async (ctl) => {
      ctl.updateSettings({ placement: "track" });
      ctl.setUrl("https://www.tiktok.com/@creator.kr/video/7400000000000000001");
      await waitFor(() => ctl.state.draft.status === "ready");
      // 세로 시퀀스(짧은 변 1080)라 1080p HEVC가 추천된다: 추천과 변환 배지가 함께 보인다.
    },
  },
  "audio-only": {
    about: "오디오만(WAV) + 자동 자막 + 구간 + 소스 모니터: 받기 옵션 요약",
    probe: (url) => ({ url, probeId: "p3", info: zoo }),
    preview: () => [{ stage: "video", progress: 0.8, peaks: peaks(19) }],
    run: async (ctl) => {
      ctl.updateSettings({ placement: "source" });
      ctl.setUrl("https://www.youtube.com/watch?v=jNQXAC9IVRw");
      await waitFor(() => ctl.state.draft.status === "ready");
      ctl.setExtras({ audioOnly: true, subtitle: "en-orig:auto" });
      ctl.setRange({ on: true, start: 3, end: 12 });
    },
  },
  jobs: {
    about: "작업 목록: 받는 중(속도·남은 시간), 구간 받는 중(진행률 모름), 변환 중, 완료(In/Out·타임라인·캡션), 가져오기 실패, 실패, 취소",
    importFile: (path) =>
      path.includes("fail")
        ? { ok: false, error: "열린 프로젝트가 없습니다.", bin: ["Birdcage", "Youtube"] }
        : { ok: true, bin: ["Birdcage", "Youtube"], inout: "set", placed: "ok", captions: "track" },
    run: async (ctl, helper) => {
      ctl.updateSettings({ autoImport: true, placement: "track" });
      const push = (t: string, job: JobSnapshot) => helper.push(t, { job });
      push("job.failed", baseJob("j7", "cancelled", { info: info("j7", "취소한 영상"), failure: { kind: "cancelled", message: "취소되었습니다.", policy: "stop" } }));
      push("job.failed", baseJob("j6", "failed", { info: info("j6", "접근할 수 없는 영상"), failure: { kind: "private", message: "비공개 영상입니다.", detail: "ERROR: Private video", policy: "stop" } }));
      push(
        "job.done",
        baseJob("j5", "done", {
          info: info("j5", "가져오기 실패한 완료 영상"),
          file: "/Users/editor/Movies/Birdcage/Youtube/fail [j5].mp4",
          media: { container: "mov,mp4", durationSec: 30, sizeBytes: 5_000_000, video: { index: 0, codec: "h264", pixFmt: "yuv420p", width: 1280, height: 720, fps: 30 }, audio: { index: 1, codec: "aac", sampleRate: 48000, channels: 2 } },
        }),
      );
      push(
        "job.done",
        baseJob("j4", "done", {
          info: info("j4", "Me at the zoo"),
          options: { maxHeight: 1080, profile: "compat", range: { startSec: 7.25, endSec: 12.5 } },
          file: "/Users/editor/Movies/Birdcage/Youtube/Me at the zoo [j4] 0m07.2s-0m12.5s.mp4",
          clip: { inSec: 1.85, outSec: 7.1 },
          subtitleFile: "/Users/editor/Movies/Birdcage/Youtube/Me at the zoo [j4] 0m07.2s-0m12.5s.en.srt",
          plan: { profile: "compat", video: "copy", audio: "transcode", passthrough: false, reasons: [], aligned: true, alignShiftSec: 5.4 },
          media: { container: "mov,mp4", durationSec: 8.2, sizeBytes: 261_000, video: { index: 0, codec: "h264", pixFmt: "yuv420p", width: 320, height: 240, fps: 15 }, audio: { index: 1, codec: "aac", sampleRate: 48000, channels: 2 } },
        }),
      );
      push("job.update", baseJob("j3", "normalizing", { progress: 0.62, info: info("j3", "VP9 원본을 H.264로 변환 중"), plan: { profile: "compat", video: "transcode", audio: "copy", encoder: "h264_videotoolbox", passthrough: false, reasons: [] } }));
      push("job.update", baseJob("j2", "downloading", { progress: -1, info: info("j2", "구간만 받는 중"), options: { maxHeight: 720, profile: "compat", range: { startSec: 60, endSec: 75 } }, download: { status: "downloading", downloadedBytes: 0, fraction: -1 } }));
      push(
        "job.update",
        baseJob("j1", "downloading", {
          progress: 0.42,
          info: info("j1", "편집용 인터뷰 클립"),
          download: { status: "downloading", downloadedBytes: 42_000_000, totalBytes: 100_000_000, speedBps: 5_250_000, etaSec: 11, fraction: 0.42 },
          warnings: ["JS 런타임이 없어 YouTube 화질이 낮을 수 있습니다."],
        }),
      );
      await settle();
      await waitFor(() => ctl.state.jobs.filter((j) => j.import?.state === "done" || j.import?.state === "failed").length === 2);
    },
  },
};

export async function waitFor(cond: () => boolean, ms = 2000): Promise<void> {
  const t0 = Date.now();
  while (!cond()) {
    if (Date.now() - t0 > ms) throw new Error("fixture: condition not reached");
    await settle();
  }
}
