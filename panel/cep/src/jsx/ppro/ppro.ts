// ExtendScript (ES3) side of the CEP panel. bolt-cep transpiles this file to ES3.
// Keep logic minimal here (ES3, hard to debug); decisions live in the TS panel/core.
// ASCII ONLY (strings and comments): ExtendScript may read BOM-less files in the system code page
// (e.g. Windows CP949), garbling non-ASCII text. The panel builds user-facing (Korean) messages from `code`.

export type ImportCode = "no_project" | "file_missing" | "import_failed";
type ImportResult = { ok: boolean; code?: ImportCode; detail?: string; bin: string[]; added?: number };

const BIN = 2; // ProjectItemType.BIN

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

/** Imports one file into the bin path (created as needed), e.g. ["Birdcage", "Youtube"]. */
export const birdcageImport = (path: string, binPath: string[]): ImportResult => {
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
  return { ok: true, bin: binPath, added: added };
};

/** Host information the panel cannot read from CEP alone. */
export const birdcageHostInfo = () => {
  return { version: app.version, build: app.build, projectOpen: !!(app.project && app.project.path) };
};
