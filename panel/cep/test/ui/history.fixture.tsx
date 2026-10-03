import React, { useState } from "react";
import ReactDOM from "react-dom/client";
import { Controller, type HostAdapter, type JobView } from "@birdcage/core";
import "../../src/js/index.scss";
import { clearDownloadHistory, loadDownloadHistory, recordDownloadHistory, withDownloadHistory } from "../../src/js/download-history";
import { DownloadsApp } from "../../src/js/downloads/downloads";
import type { JobsAction } from "../../src/js/jobs-channel";
import { App, type PanelHost } from "../../src/js/main/main";
import { FakeHelper } from "./fake-helper";
import { hello, scenarios } from "./scenarios";

const completed = (title: string, updatedAt: string): JobView => ({
  snap: {
    jobId: "remembered",
    url: "https://www.youtube.com/watch?v=remembered",
    state: "done",
    options: { maxHeight: 1080, profile: "compat" },
    progress: 1,
    file: "/Users/editor/Movies/Birdcage/Youtube/remembered.mp4",
    info: { id: "remembered", title, extractor: "Youtube", webpageUrl: "https://www.youtube.com/watch?v=remembered" },
    media: { container: "mov,mp4", durationSec: 30, sizeBytes: 12_000_000, video: { index: 0, codec: "h264", pixFmt: "yuv420p", width: 1920, height: 1080, fps: 30 } },
    createdAt: "2026-10-03T12:00:00Z",
    updatedAt,
  },
});

if (new URLSearchParams(location.search).has("seed") && loadDownloadHistory().length === 0) {
  recordDownloadHistory([completed("이전 제목", "2026-10-03T12:00:10Z")]);
  recordDownloadHistory([completed("저장된 완료 영상", "2026-10-03T12:00:20Z")]);
}

const scenario = scenarios["preview-downloading"];
const helper = new FakeHelper({ hello, probe: scenario.probe, preview: scenario.preview });
const host: HostAdapter = {
  info: () => ({ app: "PPRO", version: "15.2.0", api: "cep", os: "mac" }),
  startHelper: async () => ({ port: 31415, token: "fixture" }),
  stopHelper: () => undefined,
  importFile: async (_path, bin) => ({ ok: true, bin }),
  sequenceInfo: async () => scenario.sequence ?? null,
};
const ctl = new Controller({ host, clientName: "fixture", clientVersion: "0", ws: helper.factory, backoff: [], probeDebounceMs: 0 });
const panelHost: PanelHost = { revealFile: () => undefined, openDownloads: () => undefined };

const Harness = () => {
  const [history, setHistory] = useState(loadDownloadHistory());
  const jobs = withDownloadHistory(ctl.state.jobs, history);
  const action = (value: JobsAction) => {
    if (value.type === "clearFinished") {
      clearDownloadHistory();
      setHistory([]);
      ctl.clearFinished();
    }
  };
  return (
    <>
      <div id="main-panel"><App controller={ctl} host={panelHost} downloadHistory={() => history} /></div>
      <div id="downloads-panel"><DownloadsApp snapshot={{ jobs, liveJobIds: ctl.state.jobs.map((view) => view.snap.jobId), libraryDir: hello.libraryDir }} action={action} openMain={() => undefined} /></div>
    </>
  );
};

(window as unknown as { __birdcageHistory: { count: () => number } }).__birdcageHistory = { count: () => loadDownloadHistory().length };

async function main() {
  await ctl.start();
  await scenario.run?.(ctl, helper);
  ReactDOM.createRoot(document.getElementById("fixture") as HTMLElement).render(<Harness />);
  for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  document.getElementById("fixture")!.dataset.ready = "1";
}

void main();
