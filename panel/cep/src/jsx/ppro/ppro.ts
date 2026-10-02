// ExtendScript (ES3) side of the CEP panel. bolt-cep transpiles this file to ES3.
// Keep logic minimal here (ES3, hard to debug); decisions live in the TS panel/core.
// ASCII ONLY (strings and comments): ExtendScript may read BOM-less files in the system code page
// (e.g. Windows CP949), garbling non-ASCII text. The panel builds user-facing (Korean) messages from `code`.

export type ImportCode = "no_project" | "file_missing" | "import_failed";
export type InOutState = "set" | "unverified" | "unsupported";
type ImportResult = {
  ok: boolean;
  code?: ImportCode;
  detail?: string;
  bin: string[];
  added?: number;
  inout?: InOutState;
  source?: boolean;
};
/** Options from the panel: In/Out (seconds inside the file) and Source Monitor. */
export type ImportOptions = { inSec?: number; outSec?: number; openSource?: boolean };

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
