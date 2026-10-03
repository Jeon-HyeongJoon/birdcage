import React, { useEffect, useState } from "react";
import ReactDOM from "react-dom/client";
import { csi, initBolt, subscribeBackgroundColor } from "../lib/utils/bolt";
import { JobsSnapshot, onJobsSnapshot, requestJobs, sendJobsAction } from "../jobs-channel";
import "../index.scss";
import "../main/main.scss";
import { DownloadsApp } from "./downloads";

initBolt(false);

const Root = () => {
  const [snapshot, setSnapshot] = useState<JobsSnapshot>();
  const [bg, setBg] = useState("#232323");
  useEffect(() => {
    if (window.cep) subscribeBackgroundColor(setBg);
    const off = onJobsSnapshot(setSnapshot);
    requestJobs();
    return off;
  }, []);
  const openMain = () => {
    csi.requestOpenExtension("com.birdcage.panel.main", "");
    setTimeout(requestJobs, 500);
  };
  return <div style={{ backgroundColor: bg, minHeight: "100vh" }}><DownloadsApp snapshot={snapshot} action={sendJobsAction} openMain={openMain} /></div>;
};

ReactDOM.createRoot(document.getElementById("app") as HTMLElement).render(<Root />);
