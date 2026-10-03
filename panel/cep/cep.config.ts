import type { CEP_Config } from "vite-cep-plugin";
import { version } from "./package.json";

// One CEP panel for Premiere 2021 (15.0) through 26.
// - RequiredRuntime CSXS 10: Premiere 15.0-15.3 ship CEP 10 (Chromium 74, Node 12.3.1); requiring 11/12 would not load there.
// - Build target is chrome74 (vite.config.ts).
// - --enable-nodejs --mixed-context: needed to spawn the helper with Node child_process.
// NOTE: keep comments in this file ASCII-only. It is bundled into the ExtendScript build (via shared.ts),
// and ExtendScript may read files in the system code page (e.g. CP949), garbling non-ASCII text.
const config: CEP_Config = {
  version,
  id: "com.birdcage.panel",
  displayName: "Birdcage",
  symlink: "local",
  port: 3000,
  servePort: 5000,
  startingDebugPort: 8860,
  extensionManifestVersion: 6.0,
  requiredRuntimeVersion: 10.0,
  hosts: [{ name: "PPRO", version: "[15.0,99.9]" }],

  type: "Panel",
  iconNormal: "./assets/icon-light.png",
  iconDarkNormal: "./assets/icon-dark.png",
  iconNormalRollOver: "./assets/icon-light.png",
  iconDarkNormalRollOver: "./assets/icon-dark.png",
  parameters: ["--v=0", "--enable-nodejs", "--mixed-context"],
  width: 420,
  height: 560,

  panels: [
    {
      mainPath: "./main/index.html",
      name: "main",
      panelDisplayName: "Birdcage",
      autoVisible: true,
      width: 420,
      height: 560,
      minWidth: 320,
      minHeight: 300,
    },
    {
      mainPath: "./downloads/index.html",
      name: "downloads",
      panelDisplayName: "Birdcage \ub2e4\uc6b4\ub85c\ub4dc",
      autoVisible: false,
      width: 420,
      height: 560,
      minWidth: 320,
      minHeight: 240,
    },
  ],
  build: {
    jsxBin: "off",
    sourceMap: true,
  },
  zxp: {
    country: "KR",
    province: "Seoul",
    org: "Birdcage",
    password: "password", // self-signed cert for personal use (not for distribution)
    tsa: ["http://timestamp.digicert.com/", "http://timestamp.apple.com/ts01"],
    allowSkipTSA: false,
    sourceMap: false,
    jsxBin: "off",
  },
  installModules: [],
  // src/bin -> dist/cep/bin: helper executables from scripts/build-helper.sh. src/assets -> panel icons.
  copyAssets: ["bin", "assets"],
  copyZipAssets: [],
};
export default config;
