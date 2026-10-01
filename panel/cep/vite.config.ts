import { defineConfig, Plugin } from "vite";

import react from "@vitejs/plugin-react";

import { cep, CepOptions, runAction } from "vite-cep-plugin";
import cepConfig from "./cep.config";
import fs from "fs";
import os from "os";
import path from "path";
import { extendscriptConfig } from "./vite.es.config";

const extensions = [".js", ".ts", ".tsx"];

const devDist = "dist";
const cepDist = "cep";

const src = path.resolve(__dirname, "src");
const root = path.resolve(src, "js");
const outDir = path.resolve(__dirname, "dist", cepDist);

const debugReact = process.env.DEBUG_REACT === "true";
const isProduction = process.env.NODE_ENV === "production";
const isMetaPackage = process.env.ZIP_PACKAGE === "true";
const isPackage = process.env.ZXP_PACKAGE === "true" || isMetaPackage;
const isServe = process.env.SERVE_PANEL === "true";
const action = process.env.BOLT_ACTION;

let input: { [key: string]: string } = {};
cepConfig.panels.map((panel) => {
  input[panel.name] = path.resolve(root, panel.mainPath);
});

const config: CepOptions = {
  cepConfig,
  isProduction,
  isPackage,
  isMetaPackage,
  isServe,
  debugReact,
  dir: `${__dirname}/${devDist}`,
  cepDist: cepDist,
  zxpOutput: `${__dirname}/${devDist}/zxp/${cepConfig.id}`,
  zipOutput: `${__dirname}/${devDist}/zip/${cepConfig.displayName}_${cepConfig.version}`,
  packages: cepConfig.installModules || [],
};

if (action) runAction(config, action);

// bolt-cep 플러그인은 빌드할 때마다 dist/cep를 CEP 확장 폴더에 링크한다(끄는 옵션 없음,
// vite-cep-plugin lib/index.js generateBundle). 빌드가 시스템을 건드리지 않도록, 이번 빌드가 새로 만든
// 링크와 그때 함께 생긴 빈 폴더만 지운다. 이미 있던 링크(scripts/cep-dev-install.sh → npm run symlink)는 둔다.
const cepExtDir =
  os.platform() === "win32"
    ? path.join(os.homedir(), "AppData", "Roaming", "Adobe", "CEP", "extensions") // bolt와 같은 경로
    : path.join(os.homedir(), "Library", "Application Support", "Adobe", "CEP", "extensions");
const cepLink = path.join(cepExtDir, cepConfig.id);
const lexists = (p: string) => {
  try {
    fs.lstatSync(p);
    return true;
  } catch {
    return false;
  }
};
const preexisting = new Set([cepLink, cepExtDir, path.dirname(cepExtDir)].filter(lexists));

const noImplicitCepLink = (): Plugin => ({
  name: "birdcage:no-implicit-cep-link",
  apply: "build",
  closeBundle() {
    if (preexisting.has(cepLink)) return;
    try {
      if (fs.lstatSync(cepLink).isSymbolicLink()) fs.unlinkSync(cepLink);
    } catch {
      return; // 링크가 만들어지지 않았다
    }
    for (const dir of [cepExtDir, path.dirname(cepExtDir)]) {
      if (preexisting.has(dir)) break;
      try {
        fs.rmdirSync(dir); // 비어 있을 때만 지워진다
      } catch {
        break;
      }
    }
    console.log("birdcage: bolt가 만든 CEP 확장 링크를 지웠습니다 (Premiere에 연결: npm run symlink 또는 scripts/cep-dev-install.sh)");
  },
});

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react(), cep(config), noImplicitCepLink()],
  resolve: {
    alias: [
      { find: "@esTypes", replacement: path.resolve(__dirname, "src") },
      // 호스트 무관 코어(UXP 패널과 공유). 소스를 직접 번들해 chrome74로 함께 변환한다.
      { find: "@birdcage/core", replacement: path.resolve(__dirname, "..", "core", "src") },
    ],
  },
  root,
  clearScreen: false,
  server: {
    port: cepConfig.port,
    fs: { allow: [path.resolve(__dirname, "..")] },
  },
  preview: {
    port: cepConfig.servePort,
  },

  build: {
    sourcemap: isPackage ? cepConfig.zxp.sourceMap : cepConfig.build?.sourceMap,
    watch: {
      include: "src/jsx/**",
    },
    // commonjsOptions: {
    //   transformMixedEsModules: true,
    // },
    rollupOptions: {
      input,
      output: {
        manualChunks: {},
        // esModule: false,
        preserveModules: false,
        format: "cjs",
        entryFileNames: "assets/[name]-[hash].cjs",
        chunkFileNames: "assets/[name]-[hash].cjs",
      },
    },
    target: "chrome74",
    outDir,
  },
});

// rollup es3 build
const outPathExtendscript = path.join("dist", cepDist, "jsx", "index.js");
extendscriptConfig(
  `src/jsx/index.ts`,
  outPathExtendscript,
  cepConfig,
  extensions,
  isProduction,
  isPackage,
);
