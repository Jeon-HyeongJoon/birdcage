// ExtendScript (ES3) side of the CEP panel. bolt-cep transpiles this file to ES3.
import { firstFreeTrack, Span } from "./placement";
// Keep logic minimal here (ES3, hard to debug); decisions live in the TS panel/core.
// ASCII ONLY (strings and comments): ExtendScript may read BOM-less files in the system code page
// (e.g. Windows CP949), garbling non-ASCII text. The panel builds user-facing (Korean) messages from `code`.

export type ImportCode = "no_project" | "file_missing" | "import_failed";
export type InOutState = "set" | "unverified" | "unsupported";
export type PlaceMode = "newTrack";
export type PlaceState = "ok" | "source" | "unverified" | "no_sequence" | "no_free_track" | "failed";
export type CaptionState = "imported" | "track" | "unsupported" | "before_start" | "failed";
type ImportResult = {
  ok: boolean;
  code?: ImportCode;
  detail?: string;
  bin: string[];
  added?: number;
  inout?: InOutState;
  source?: boolean;
  placed?: PlaceState;
  track?: number;
  captions?: CaptionState;
};
/** Options from the panel: In/Out (seconds inside the file), Source Monitor, timeline placement. */
export type ImportOptions = {
  inSec?: number;
  outSec?: number;
  openSource?: boolean;
  place?: PlaceMode;
  hasVideo?: boolean;
  subtitle?: string;
};

const BIN = 2; // ProjectItemType.BIN
/** Ticks per second in Premiere time values. */
const TICKS = 254016000000;

const findOrCreateBin = (parent: ProjectItem, name: string): ProjectItem => {
  const n = parent.children.numItems;
  for (let i = 0; i < n; i++) {
    const child = parent.children[i];
    if (child && child.type === BIN && child.name === name) return child;
  }
  // createBin returns the new bin, but some versions return undefined: search again.
  const created = parent.createBin(name) as unknown as ProjectItem | undefined;
  if (created) return created;
  for (let i = 0; i < parent.children.numItems; i++) {
    const child = parent.children[i];
    if (child && child.type === BIN && child.name === name) return child;
  }
  throw new Error("cannot create bin " + name);
};

/** Finds the imported item in the bin by media path (importFiles does not return it). */
const findItem = (bin: ProjectItem, fsPath: string): ProjectItem | null => {
  for (let i = bin.children.numItems - 1; i >= 0; i--) {
    const c = bin.children[i];
    if (!c || c.type === BIN) continue;
    try {
      if (new File(c.getMediaPath()).fsName === fsPath) return c;
    } catch (e) {
      // offline or non-media item
    }
  }
  return null;
};

const near = (a: number, b: number): boolean => Math.abs(a - b) < 0.02; // 20 ms: under one frame at 50 fps

const pointSeconds = (t: unknown): number => {
  if (t && typeof t === "object" && "seconds" in (t as object)) return Number((t as Time).seconds);
  return Number(t);
};

/**
 * Sets In/Out on the item and reads them back. The scripting docs disagree on the unit
 * (seconds vs ticks), so each form is tried and only a read-back match counts as "set".
 */
const setInOut = (item: ProjectItem, inSec: number, outSec: number): InOutState => {
  if (typeof item.setInPoint !== "function" || typeof item.getInPoint !== "function") return "unsupported";
  const forms = [
    (x: number): number | string => x,
    (x: number): number | string => String(Math.round(x * TICKS)),
  ];
  for (let i = 0; i < forms.length; i++) {
    try {
      item.setInPoint(forms[i](inSec), 4); // 4 = video and audio
      item.setOutPoint(forms[i](outSec), 4);
      if (near(pointSeconds(item.getInPoint()), inSec) && near(pointSeconds(item.getOutPoint()), outSec)) return "set";
    } catch (e) {
      // try the next form
    }
  }
  return "unverified";
};

const spans = (tracks: TrackCollection): Span[][] => {
  const out: Span[][] = [];
  for (let i = 0; i < tracks.numTracks; i++) {
    const clips = tracks[i].clips;
    const list: Span[] = [];
    for (let j = 0; j < clips.numItems; j++) {
      list.push([Number(clips[j].start.seconds), Number(clips[j].end.seconds)]);
    }
    out.push(list);
  }
  return out;
};

const targeted = (tracks: TrackCollection): boolean[] => {
  const out: boolean[] = [];
  for (let i = 0; i < tracks.numTracks; i++) {
    let t = false;
    try {
      t = typeof tracks[i].isTargeted === "function" && tracks[i].isTargeted();
    } catch (e) {
      t = false;
    }
    out.push(t);
  }
  return out;
};

/** Seconds between the item's In and Out (its whole length when none are set). */
const itemSeconds = (item: ProjectItem): number => {
  try {
    const d = pointSeconds(item.getOutPoint()) - pointSeconds(item.getInPoint());
    return d > 0 ? d : 0;
  } catch (e) {
    return 0;
  }
};

/** True when a clip of this item now starts at t on the video (or audio) track. */
const placedAt = (tracks: TrackCollection, index: number, item: ProjectItem, t: number): boolean => {
  if (index < 0 || index >= tracks.numTracks) return false;
  const clips = tracks[index].clips;
  for (let j = 0; j < clips.numItems; j++) {
    const c = clips[j];
    try {
      if (near(Number(c.start.seconds), t) && c.projectItem && c.projectItem.nodeId === item.nodeId) return true;
    } catch (e) {
      // ignore unreadable clips
    }
  }
  return false;
};

/** First track with no clips at all, or -1. */
const emptyTrack = (tracks: TrackCollection): number => {
  for (let i = 0; i < tracks.numTracks; i++) {
    if (tracks[i].clips.numItems === 0) return i;
  }
  return -1;
};

/**
 * Appends tracks with the QE DOM (unsupported by Adobe, but the only scripting way to add tracks).
 * Returns true only when the track counts actually grew.
 */
const addTracks = (seq: Sequence, video: number, audio: number): boolean => {
  const v0 = seq.videoTracks.numTracks;
  const a0 = seq.audioTracks.numTracks;
  try {
    app.enableQE();
    const qs = qe ? qe.project.getActiveSequence() : undefined;
    if (!qs) return false;
    // addTracks(numVideo, videoIndex, numAudio, audioChannelType(1 = stereo), audioIndex, numSubmix, submixType, submixIndex)
    qs.addTracks(video, v0, audio, 1, a0, 0, 1, 0);
  } catch (e) {
    return false;
  }
  return seq.videoTracks.numTracks >= v0 + video && seq.audioTracks.numTracks >= a0 + audio;
};

/**
 * Places the item at the playhead on empty tracks so existing work is never touched (user request
 * 2026-10-03): a track with no clips at all, else a new track (QE). If adding fails, the lowest track
 * that is empty over the clip's length. With no open sequence the item opens in the Source Monitor.
 */
const place = (item: ProjectItem, hasVideo: boolean): { placed: PlaceState; track?: number; at?: number } => {
  const seq = app.project.activeSequence;
  if (!seq) {
    try {
      app.sourceMonitor.openProjectItem(item);
      return { placed: "source" };
    } catch (e) {
      return { placed: "no_sequence" };
    }
  }
  const pos = seq.getPlayerPosition();
  const t = Number(pos.seconds);
  let v = hasVideo ? emptyTrack(seq.videoTracks) : 0;
  let a = emptyTrack(seq.audioTracks);
  const needV = hasVideo && v < 0;
  const needA = a < 0;
  if (needV || needA) {
    const v0 = seq.videoTracks.numTracks;
    const a0 = seq.audioTracks.numTracks;
    if (addTracks(seq, needV ? 1 : 0, needA ? 1 : 0)) {
      if (needV) v = v0;
      if (needA) a = a0;
    } else {
      const len = itemSeconds(item);
      v = hasVideo ? firstFreeTrack(spans(seq.videoTracks), t, t + len) : 0;
      a = firstFreeTrack(spans(seq.audioTracks), t, t + len);
      if (v < 0 || a < 0) return { placed: "no_free_track" };
    }
  }
  // Sequence.overwriteClip(item, time, vIndex, aIndex) on empty tracks touches nothing; older builds only have Track methods.
  type Place4 = (item: ProjectItem, time: Time, vIndex: number, aIndex: number) => unknown;
  const s = seq as unknown as { overwriteClip?: Place4 };
  try {
    if (typeof s.overwriteClip === "function") s.overwriteClip(item, pos, v, a);
    else seq.videoTracks[v].overwriteClip(item, pos);
  } catch (e) {
    return { placed: "failed", track: v };
  }
  const ok = hasVideo ? placedAt(seq.videoTracks, v, item, t) : placedAt(seq.audioTracks, a, item, t);
  return { placed: ok ? "ok" : "unverified", track: hasVideo ? v : a, at: t };
};

/**
 * Imports the SRT sidecar into the same bin. When the media was placed at sequence time `at`
 * starting from its In point, media time 0 sits at at - inSec: the caption track starts there.
 * createCaptionTrack has no documented first version, so it is feature-checked.
 */
const importCaptions = (bin: ProjectItem, srtPath: string, at: number | undefined, inSec: number): CaptionState => {
  const f = new File(srtPath);
  if (!f.exists) return "failed";
  if (!app.project.importFiles([f.fsName], true, bin, false) && !findItem(bin, f.fsName)) return "failed";
  const srt = findItem(bin, f.fsName);
  if (!srt) return "failed";
  if (at === undefined) return "imported";
  const seq = app.project.activeSequence;
  const s = seq as unknown as { createCaptionTrack?: (item: ProjectItem, startAtTime: number) => unknown };
  if (!seq || typeof s.createCaptionTrack !== "function") return "unsupported";
  const start = at - inSec;
  if (start < 0) return "before_start";
  try {
    s.createCaptionTrack(srt, start);
    return "track";
  } catch (e) {
    return "failed";
  }
};

/** Imports one file into the bin path (created as needed), e.g. ["Birdcage", "Youtube"]. */
export const birdcageImport = (path: string, binPath: string[], opts?: ImportOptions): ImportResult => {
  if (!app.project || !app.project.rootItem) {
    return { ok: false, code: "no_project", bin: binPath };
  }
  const f = new File(path);
  if (!f.exists) {
    return { ok: false, code: "file_missing", detail: path, bin: binPath };
  }
  let bin = app.project.rootItem;
  for (let i = 0; i < binPath.length; i++) {
    bin = findOrCreateBin(bin, binPath[i]);
  }
  const before = bin.children.numItems;
  // importFiles(paths, suppressUI, targetBin, importAsNumberedStills)
  const ok = app.project.importFiles([f.fsName], true, bin, false);
  const added = bin.children.numItems - before;
  if (!ok && added <= 0) {
    return { ok: false, code: "import_failed", detail: f.fsName, bin: binPath };
  }
  const res: ImportResult = { ok: true, bin: binPath, added: added };
  if (!opts) return res;
  const item = findItem(bin, f.fsName);
  if (opts.inSec !== undefined && opts.outSec !== undefined) {
    res.inout = item ? setInOut(item, opts.inSec, opts.outSec) : "unverified";
  }
  let at: number | undefined;
  if (opts.place) {
    if (!item) {
      res.placed = "failed";
    } else {
      const p = place(item, opts.hasVideo !== false);
      res.placed = p.placed;
      res.track = p.track;
      if (p.placed === "ok") at = p.at;
    }
  }
  if (opts.subtitle) {
    res.captions = importCaptions(bin, opts.subtitle, at, opts.inSec || 0);
  }
  if (opts.openSource && item) {
    try {
      app.sourceMonitor.openProjectItem(item);
      res.source = true;
    } catch (e) {
      res.source = false;
    }
  }
  return res;
};

/**
 * Active sequence frame size and rate (recommendation and fps warning), or ok=false when none.
 * frameSize* exist on older versions; getSettings is tried when they are missing.
 */
export const birdcageSequenceInfo = () => {
  const seq = app.project ? app.project.activeSequence : null;
  if (!seq) return { ok: false };
  let w = 0;
  let h = 0;
  try {
    w = Number(seq.frameSizeHorizontal);
    h = Number(seq.frameSizeVertical);
  } catch (e) {
    w = 0;
  }
  if (!w || !h) {
    try {
      const st = seq.getSettings();
      w = Number(st.videoFrameWidth);
      h = Number(st.videoFrameHeight);
    } catch (e) {
      w = 0;
    }
  }
  let fps = 0;
  try {
    const tb = Number(seq.timebase); // ticks per frame
    if (tb > 0) fps = TICKS / tb;
  } catch (e) {
    fps = 0;
  }
  return { ok: true, name: String(seq.name), width: w || 0, height: h || 0, fps: fps };
};

/** Host information the panel cannot read from CEP alone. */
export const birdcageHostInfo = () => {
  return { version: app.version, build: app.build, projectOpen: !!(app.project && app.project.path) };
};

/** Local folder containing the active saved .prproj. Unsaved/cloud projects have no usable path. */
export const birdcageProjectDir = () => {
  if (!app.project || !app.project.path) return { ok: false };
  try {
    const project = new File(app.project.path);
    const parent = project.parent;
    if (!parent || !parent.fsName) return { ok: false };
    return { ok: true, path: parent.fsName };
  } catch (e) {
    return { ok: false };
  }
};
