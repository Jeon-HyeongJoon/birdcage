import { existsSync } from "fs";
import path from "path";
import { fileURLToPath } from "url";
import { defineConfig } from "@playwright/test";
import { pinnedProject } from "./test/ui/environment";

const here = path.dirname(fileURLToPath(import.meta.url));

const chromeByPlatform: Partial<Record<NodeJS.Platform, string>> = {
  darwin: "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
  win32: "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
  linux: "/usr/bin/google-chrome",
};

const chrome = process.env.BIRDCAGE_CHROME_PATH || chromeByPlatform[process.platform];
if (!chrome || !existsSync(chrome)) {
  throw new Error("Chrome을 찾을 수 없습니다. BIRDCAGE_CHROME_PATH에 실행 파일 경로를 지정하세요.");
}

export default defineConfig({
  testDir: path.resolve(here, "test", "ui"),
  testMatch: "*.spec.ts",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: "line",
  // DOM 골든(toMatchSnapshot)은 환경 무관(golden/dom), 픽셀 골든은 고정 환경 폴더(golden/<고정 환경>).
  snapshotPathTemplate: "{testDir}/../golden/dom/{arg}{ext}",
  expect: {
    timeout: 5_000,
    toHaveScreenshot: {
      pathTemplate: "{testDir}/../golden/{projectName}/{arg}{ext}",
      animations: "disabled",
      caret: "hide",
      scale: "css",
    },
  },
  use: {
    baseURL: "http://127.0.0.1:4173",
    browserName: "chromium",
    launchOptions: { executablePath: chrome },
    viewport: { width: 420, height: 560 },
    deviceScaleFactor: 1,
    colorScheme: "dark",
    locale: "ko-KR",
    timezoneId: "Asia/Seoul",
    reducedMotion: "reduce",
  },
  projects: [{ name: pinnedProject }],
  webServer: {
    command: "npx vite --config vite.ui.config.ts",
    cwd: here,
    url: "http://127.0.0.1:4173/fixture.html",
    reuseExistingServer: false,
    timeout: 30_000,
  },
});
