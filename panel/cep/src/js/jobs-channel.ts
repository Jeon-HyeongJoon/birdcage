import type { JobView } from "@birdcage/core";
import CSInterface, { CSEvent } from "./lib/cep/csinterface";

const STATE_EVENT = "com.birdcage.jobs.state";
const REQUEST_EVENT = "com.birdcage.jobs.request";
const ACTION_EVENT = "com.birdcage.jobs.action";

export interface JobsSnapshot {
  jobs: JobView[];
  /** 현재 Controller가 소유한 작업. 없으면(테스트·구버전) 모든 jobs를 현재 작업으로 본다. */
  liveJobIds?: string[];
  libraryDir?: string;
  outputDir?: string;
}

export type JobsAction =
  | { type: "cancel"; jobId: string }
  | { type: "import"; jobId: string }
  | { type: "reveal"; jobId: string }
  | { type: "clearFinished" };

type CEPEvent = { data?: unknown };

const client = () => new CSInterface();

function dispatch(type: string, data: unknown = {}) {
  if (!window.cep) return;
  const csi = client();
  const event = new CSEvent(type, "APPLICATION", csi.getApplicationID(), csi.getExtensionID());
  event.data = data;
  csi.dispatchEvent(event);
}

function decode<T>(event: CEPEvent): T | undefined {
  if (typeof event.data === "string") {
    try {
      return JSON.parse(event.data) as T;
    } catch {
      return undefined;
    }
  }
  return event.data as T | undefined;
}

function listen<T>(type: string, fn: (data: T) => void): () => void {
  if (!window.cep) return () => undefined;
  const csi = client();
  const handler = (event: CEPEvent) => {
    const data = decode<T>(event);
    if (data !== undefined) fn(data);
  };
  csi.addEventListener(type, handler);
  return () => csi.removeEventListener(type, handler, null);
}

export const publishJobs = (snapshot: JobsSnapshot) => dispatch(STATE_EVENT, snapshot);
export const requestJobs = () => dispatch(REQUEST_EVENT);
export const sendJobsAction = (action: JobsAction) => dispatch(ACTION_EVENT, action);
export const onJobsSnapshot = (fn: (snapshot: JobsSnapshot) => void) => listen(STATE_EVENT, fn);
export const onJobsRequest = (fn: () => void) => listen<Record<string, never>>(REQUEST_EVENT, fn);
export const onJobsAction = (fn: (action: JobsAction) => void) => listen(ACTION_EVENT, fn);
