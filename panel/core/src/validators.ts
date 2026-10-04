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
  clip: "object?",
  subtitleFile: "string?",
  failure: "object?",
  warnings: "array?",
  timings: "array?",
  createdAt: "string",
  updatedAt: "string",
};

const nested: Record<string, Shape> = {
  "job.options": { maxHeight: "number", profile: "string", probeId: "string?", formatId: "string?", range: "object?", audioOnly: "boolean?", subtitles: "object?" },
  "job.clip": { inSec: "number", outSec: "number" },
  "job.download": { status: "string", downloadedBytes: "number", fraction: "number", totalBytes: "number?", speedBps: "number?" },
  "job.info": { id: "string", title: "string", extractor: "string", webpageUrl: "string", selected: "array?", thumbnail: "string?", variants: "array?", subtitles: "array?", storyboards: "array?" },
  "job.media": { container: "string", durationSec: "number", sizeBytes: "number", video: "object?", audio: "object?" },
  "job.plan": { profile: "string", video: "string", audio: "string", passthrough: "boolean", reasons: "array" },
  "job.failure": { kind: "string", message: "string", policy: "string", detail: "string?" },
};

const variant: Shape = { formatId: "string", res: "number", vcodec: "string", transcode: "boolean", sizeBytes: "number?", fps: "number?", hdr: "boolean?" };
const subtitle: Shape = { lang: "string", name: "string?", auto: "boolean?" };
const range: Shape = { startSec: "number", endSec: "number" };
const storyboard: Shape = { width: "number", height: "number", cols: "number", rows: "number", intervalSec: "number", sprites: "array" };
const sprite: Shape = { url: "string", startSec: "number", durationSec: "number" };
const relatedItem: Shape = { id: "string", url: "string", title: "string", channel: "string?", durationSec: "number?", thumbnail: "string?", meta: "string?", views: "number?" };
const RELATED_SOURCES = ["youtube", "channel", "search"];
const peaks: Shape = { bucketSec: "number", count: "number", durationSec: "number", data: "string" };

const payloads: Record<string, Shape> = {
  hello: { client: "string", clientVersion: "string", host: "object" },
  "hello.ok": { helperVersion: "string", protocol: "number", profile: "string", libraryDir: "string", tools: "array", jobs: "array" },
  "job.create": { url: "string", maxHeight: "number?", probeId: "string?", formatId: "string?", range: "object?", audioOnly: "boolean?", subtitles: "object?", via: "string?" },
  "media.probe": { url: "string", maxHeight: "number?", via: "string?" },
  "related.fetch": { probeId: "string", source: "string?" },
  "related.list": { probeId: "string", requested: "string", source: "string?", items: "array", error: "string?" },
  "media.info": { url: "string", probeId: "string?", info: "object?", failure: "object?" },
  "preview.open": { probeId: "string" },
  "preview.close": { probeId: "string" },
  "preview.state": { probeId: "string", stage: "string", progress: "number", peaks: "object?", video: "object?", storyboard: "object?", cached: "boolean?", error: "string?" },
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

function checkFailure(f: unknown, where: string, errs: string[]): void {
  check(f, nested["job.failure"], where, errs);
  // 실패 종류·정책은 SSOT(specs/failures.json)와 일치해야 한다.
  const o = f as { kind?: string; policy?: string };
  if (o && typeof o.kind === "string") {
    if (!(FAILURE_KINDS as readonly string[]).includes(o.kind)) errs.push(`${where}.kind: unknown kind ${o.kind}`);
    else if (o.policy !== FAILURE_POLICY[o.kind as FailureKind]) errs.push(`${where}.policy: ${o.policy} != spec ${FAILURE_POLICY[o.kind as FailureKind]}`);
  }
}

function checkStoryboard(sb: unknown, where: string, errs: string[]): void {
  check(sb, storyboard, where, errs);
  const o = (sb ?? {}) as Record<string, unknown>;
  if (Array.isArray(o.sprites)) o.sprites.forEach((x, i) => check(x, sprite, `${where}.sprites[${i}]`, errs));
}

function checkInfo(info: unknown, where: string, errs: string[]): void {
  check(info, nested["job.info"], where, errs);
  const o = (info ?? {}) as Record<string, unknown>;
  if (Array.isArray(o.storyboards)) o.storyboards.forEach((v, i) => checkStoryboard(v, `${where}.storyboards[${i}]`, errs));
  if (Array.isArray(o.variants)) o.variants.forEach((v, i) => check(v, variant, `${where}.variants[${i}]`, errs));
  if (Array.isArray(o.subtitles)) o.subtitles.forEach((v, i) => check(v, subtitle, `${where}.subtitles[${i}]`, errs));
}

function checkJob(j: unknown, where: string, errs: string[]): void {
  check(j, job, where, errs);
  const o = (j ?? {}) as Record<string, unknown>;
  for (const k of ["options", "download", "media", "plan", "clip"]) {
    if (o[k] !== undefined) check(o[k], nested["job." + k], `${where}.${k}`, errs);
  }
  if (o.info !== undefined) checkInfo(o.info, `${where}.info`, errs);
  if (o.failure !== undefined) checkFailure(o.failure, `${where}.failure`, errs);
}

/** Returns a list of shape errors (empty when the payload looks right). */
export function validatePayload(type: string, data: unknown): string[] {
  const shape = payloads[type];
  if (!shape) return [`unknown message type ${type}`];
  const errs: string[] = [];
  check(data, shape, type, errs);
  const d = (data ?? {}) as Record<string, unknown>;
  if (type === "job.update" || type === "job.done" || type === "job.failed") checkJob(d.job, `${type}.job`, errs);
  if (type === "job.create" && d.range !== undefined) check(d.range, range, `${type}.range`, errs);
  if (type === "job.create" && d.subtitles !== undefined) check(d.subtitles, subtitle, `${type}.subtitles`, errs);
  if (type === "preview.state") {
    if (d.peaks !== undefined) check(d.peaks, peaks, `${type}.peaks`, errs);
    if (d.storyboard !== undefined) checkStoryboard(d.storyboard, `${type}.storyboard`, errs);
    if (!["audio", "video", "ready", "failed"].includes(String(d.stage))) errs.push(`${type}.stage: unknown ${d.stage}`);
  }
  if (type === "media.info") {
    if (d.info !== undefined) checkInfo(d.info, `${type}.info`, errs);
    if (d.failure !== undefined) checkFailure(d.failure, `${type}.failure`, errs);
    if ((d.info === undefined) === (d.failure === undefined)) errs.push(`${type}: exactly one of info/failure`);
  }
  if (type === "related.list" || type === "related.fetch") {
    for (const k of ["requested", "source"]) if (d[k] !== undefined && !RELATED_SOURCES.includes(String(d[k]))) errs.push(`${type}.${k}: unknown ${d[k]}`);
    if (Array.isArray(d.items)) d.items.forEach((it, i) => check(it, relatedItem, `${type}.items[${i}]`, errs));
  }
  if (Array.isArray(d.jobs)) d.jobs.forEach((j, i) => checkJob(j, `${type}.jobs[${i}]`, errs));
  if (Array.isArray(d.tools)) d.tools.forEach((t, i) => check(t, tool, `${type}.tools[${i}]`, errs));
  return errs;
}
