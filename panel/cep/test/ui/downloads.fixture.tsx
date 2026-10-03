import React from "react";
import ReactDOM from "react-dom/client";
import type { JobView } from "@birdcage/core";
import "../../src/js/index.scss";
import "../../src/js/main/main.scss";
import { DownloadsApp } from "../../src/js/downloads/downloads";
import type { JobsAction } from "../../src/js/jobs-channel";

const jobs: JobView[] = [
  {
    snap: {
      jobId: "active",
      url: "https://www.youtube.com/watch?v=active",
      state: "downloading",
      options: { maxHeight: 1080, profile: "compat", range: { startSec: 12, endSec: 42 } },
      progress: 0.42,
      download: { status: "downloading", downloadedBytes: 4_200_000, totalBytes: 10_000_000, speedBps: 5_000_000, etaSec: 2, fraction: 0.42 },
      info: { id: "active", title: "현재 받는 영상", extractor: "Youtube", webpageUrl: "https://www.youtube.com/watch?v=active" },
      createdAt: "2026-10-03T12:00:00Z",
      updatedAt: "2026-10-03T12:00:02Z",
    },
  },
  {
    snap: {
      jobId: "done",
      url: "https://www.youtube.com/watch?v=done",
      state: "done",
      options: { maxHeight: 720, profile: "compat" },
      progress: 1,
      file: "/Users/editor/Movies/Birdcage/Youtube/done.mp4",
      info: { id: "done", title: "완료된 영상", extractor: "Youtube", webpageUrl: "https://www.youtube.com/watch?v=done" },
      media: { container: "mov,mp4", durationSec: 30, sizeBytes: 12_000_000, video: { index: 0, codec: "h264", pixFmt: "yuv420p", width: 1280, height: 720, fps: 30 } },
      createdAt: "2026-10-03T11:00:00Z",
      updatedAt: "2026-10-03T11:00:10Z",
    },
    import: { state: "failed", error: "열린 시퀀스가 없습니다" },
  },
];
const actions: JobsAction[] = [];
(window as unknown as { __birdcageDownloads: { actions: JobsAction[] } }).__birdcageDownloads = { actions };

ReactDOM.createRoot(document.getElementById("app") as HTMLElement).render(
  <DownloadsApp snapshot={{ jobs, libraryDir: "/Users/editor/Movies/Birdcage" }} action={(action) => actions.push(action)} openMain={() => undefined} />,
);
document.getElementById("app")!.dataset.ready = "1";
