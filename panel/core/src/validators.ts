import { FAILURE_KINDS, FAILURE_POLICY, FailureKind } from "./failures.gen";

// Minimal runtime shape checks for protocol payloads. Used by the fixture contract test
// (Go 필드명이 바뀌면 여기서 잡힌다) and, in dev builds, to warn about unexpected messages.

type Kind = "string" | "number" | "boolean" | "object" | "array";
type Shape = Record<string, Kind | `${Kind}?`>;

const tool: Shape = { name: "string", path: "string", version: "string", source: "string", error: "string?" };
const job: Shape = {
  jobId: "string",
  url: "string",
  state: "string",
  options: "object",
  progress: "number",
  download: "object?",
  info: "object?",
  media: "object?",
  plan: "object?",
  file: "string?",
  failure: "object?",
  warnings: "array?",
  timings: "array?",
  createdAt: "string",
  updatedAt: "string",
};

const nested: Record<string, Shape> = {
  "job.options": { maxHeight: "number", profile: "string" },
  "job.download": { status: "string", downloadedBytes: "number", fraction: "number", totalBytes: "number?", speedBps: "number?" },
  "job.info": { id: "string", title: "string", extractor: "string", webpageUrl: "string", selected: "array?" },
  "job.media": { container: "string", durationSec: "number", sizeBytes: "number", video: "object?", audio: "object?" },
  "job.plan": { profile: "string", video: "string", audio: "string", passthrough: "boolean", reasons: "array" },
  "job.failure": { kind: "string", message: "string", policy: "string", detail: "string?" },
};

const payloads: Record<string, Shape> = {
  hello: { client: "string", clientVersion: "string", host: "object" },
  "hello.ok": { helperVersion: "string", protocol: "number", profile: "string", libraryDir: "string", tools: "array", jobs: "array" },
  "job.create": { url: "string", maxHeight: "number?" },
  "job.accepted": { jobId: "string" },
  "job.cancel": { jobId: "string" },
  "job.update": { job: "object" },
  "job.done": { job: "object" },
  "job.failed": { job: "object" },
  "job.list.ok": { jobs: "array" },
  "tools.status": { installing: "boolean", tools: "array", error: "string?" },
  error: { code: "string", message: "string" },
};

function kindOf(v: unknown): Kind {
  if (Array.isArray(v)) return "array";
  return typeof v as Kind;
}

function check(obj: unknown, shape: Shape, where: string, errs: string[]): void {
  if (!obj || typeof obj !== "object") {
    errs.push(`${where}: not an object`);
    return;
  }
  const o = obj as Record<string, unknown>;
  for (const [key, spec] of Object.entries(shape)) {
    const optional = spec.endsWith("?");
    const want = (optional ? spec.slice(0, -1) : spec) as Kind;
    if (!(key in o) || o[key] === undefined) {
      if (!optional) errs.push(`${where}.${key}: missing`);
      continue;
    }
    if (kindOf(o[key]) !== want) errs.push(`${where}.${key}: want ${want}, got ${kindOf(o[key])}`);
  }
}

function checkJob(j: unknown, where: string, errs: string[]): void {
  check(j, job, where, errs);
  const o = (j ?? {}) as Record<string, unknown>;
  for (const k of ["options", "download", "info", "media", "plan", "failure"]) {
    if (o[k] !== undefined) check(o[k], nested["job." + k], `${where}.${k}`, errs);
  }
  // 실패 종류·정책은 SSOT(specs/failures.json)와 일치해야 한다.
  const f = o.failure as { kind?: string; policy?: string } | undefined;
  if (f && typeof f.kind === "string") {
    if (!(FAILURE_KINDS as readonly string[]).includes(f.kind)) errs.push(`${where}.failure.kind: unknown kind ${f.kind}`);
    else if (f.policy !== FAILURE_POLICY[f.kind as FailureKind]) errs.push(`${where}.failure.policy: ${f.policy} != spec ${FAILURE_POLICY[f.kind as FailureKind]}`);
  }
}

/** Returns a list of shape errors (empty when the payload looks right). */
export function validatePayload(type: string, data: unknown): string[] {
  const shape = payloads[type];
  if (!shape) return [`unknown message type ${type}`];
  const errs: string[] = [];
  check(data, shape, type, errs);
  const d = (data ?? {}) as Record<string, unknown>;
  if (type === "job.update" || type === "job.done" || type === "job.failed") checkJob(d.job, `${type}.job`, errs);
  if (Array.isArray(d.jobs)) d.jobs.forEach((j, i) => checkJob(j, `${type}.jobs[${i}]`, errs));
  if (Array.isArray(d.tools)) d.tools.forEach((t, i) => check(t, tool, `${type}.tools[${i}]`, errs));
  return errs;
}
