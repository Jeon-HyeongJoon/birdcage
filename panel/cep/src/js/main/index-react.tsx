import React from "react";
import ReactDOM from "react-dom/client";
import { Controller } from "@birdcage/core";
import { initBolt } from "../lib/utils/bolt";
import { CepHost } from "../host/cep-host";
import { version } from "../../shared/shared";
import "../index.scss";
import { App } from "./main";
import { onJobsAction, onJobsRequest, publishJobs } from "../jobs-channel";

initBolt();

const SETTINGS_KEY = "birdcage.settings";

function loadSettings() {
  try {
    return JSON.parse(localStorage.getItem(SETTINGS_KEY) || "{}");
  } catch {
    return {};
  }
}

const host = new CepHost();
const ctl = new Controller({ host, clientName: "birdcage-cep", clientVersion: version, settings: loadSettings() });
const publish = () => publishJobs({ jobs: ctl.state.jobs, libraryDir: ctl.state.helper?.libraryDir });
ctl.subscribe((s) => {
  try {
    localStorage.setItem(SETTINGS_KEY, JSON.stringify(s.settings));
  } catch {
    /* 저장 실패해도 동작에는 지장 없음 */
  }
  publish();
});
const stopRequest = onJobsRequest(publish);
const stopActions = onJobsAction((action) => {
  if (action.type === "cancel") ctl.cancel(action.jobId);
  else if (action.type === "import") void ctl.importJob(action.jobId);
  else if (action.type === "clearFinished") ctl.clearFinished();
  else if (action.type === "reveal") {
    const file = ctl.state.jobs.find((j) => j.snap.jobId === action.jobId)?.snap.file;
    if (file) host.revealFile(file);
  }
});
window.addEventListener("beforeunload", () => {
  stopRequest();
  stopActions();
  ctl.dispose();
});

// StrictMode는 개발 중 effect를 두 번 실행한다. Controller.start()는 중복 호출에 안전하다.
ReactDOM.createRoot(document.getElementById("app") as HTMLElement).render(
  <React.StrictMode>
    <App controller={ctl} host={host} />
  </React.StrictMode>,
);
