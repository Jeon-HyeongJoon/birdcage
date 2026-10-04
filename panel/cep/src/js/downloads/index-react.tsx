import React, { useEffect, useState } from "react";
import ReactDOM from "react-dom/client";
import { clearDownloadHistory, loadDownloadHistory, withDownloadHistory } from "../download-history";
import { csi, initBolt, subscribeBackgroundColor } from "../lib/utils/bolt";
import { JobsAction, JobsSnapshot, onJobsSnapshot, requestJobs, sendJobsAction } from "../jobs-channel";
import "../index.scss";
import "../main/main.scss";
import { DownloadsApp } from "./downloads";

initBolt(false);

const historySnapshot = (): JobsSnapshot => ({ jobs: loadDownloadHistory(), liveJobIds: [] });

const refreshHistory = (snapshot: JobsSnapshot): JobsSnapshot => {
  const liveJobIds = snapshot.liveJobIds ?? snapshot.jobs.map((view) => view.snap.jobId);
  const live = new Set(liveJobIds);
  const liveJobs = snapshot.jobs.filter((view) => live.has(view.snap.jobId));
  return { ...snapshot, jobs: withDownloadHistory(liveJobs, loadDownloadHistory()), liveJobIds };
};

const Root = () => {
  const [snapshot, setSnapshot] = useState<JobsSnapshot>(historySnapshot);
  const [bg, setBg] = useState("#232323");
  useEffect(() => {
    if (window.cep) subscribeBackgroundColor(setBg);
    const off = onJobsSnapshot((next) => setSnapshot(refreshHistory(next)));
    requestJobs();
    const poll = window.setInterval(() => setSnapshot((current) => refreshHistory(current)), 1000);
    return () => {
      window.clearInterval(poll);
      off();
    };
  }, []);
  const openMain = () => {
    csi.requestOpenExtension("com.birdcage.panel.main", "");
    setTimeout(requestJobs, 500);
  };
  const action = (value: JobsAction) => {
    if (value.type === "clearFinished") {
      clearDownloadHistory();
      setSnapshot((current) => ({ ...current, jobs: current.jobs.filter((view) => view.snap.state !== "done") }));
    }
    sendJobsAction(value);
  };
  return <div style={{ backgroundColor: bg, minHeight: "100vh" }}><DownloadsApp snapshot={snapshot} action={action} openMain={openMain} /></div>;
};

ReactDOM.createRoot(document.getElementById("app") as HTMLElement).render(<Root />);
