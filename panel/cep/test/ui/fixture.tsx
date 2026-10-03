import React from "react";
import ReactDOM from "react-dom/client";
import { Controller, type HostAdapter } from "@birdcage/core";
import "../../src/js/index.scss";
import { App, type PanelHost } from "../../src/js/main/main";
import { FakeHelper } from "./fake-helper";
import { hello, scenarios } from "./scenarios";

const name = new URLSearchParams(window.location.search).get("scenario") || "empty";
const scenario = scenarios[name];
if (!scenario) throw new Error(`알 수 없는 UI 픽스처: ${name}`);

const helper = new FakeHelper({ hello: { ...hello, ...scenario.hello }, probe: scenario.probe, preview: scenario.preview });

const host: HostAdapter = {
  info: () => ({ app: "PPRO", version: "15.2.0", api: "cep", os: "mac" }),
  startHelper: scenario.startHelper ?? (async () => ({ port: 31415, token: "fixture" })),
  stopHelper: () => undefined,
  importFile: async (path, bin) => scenario.importFile?.(path) ?? { ok: true, bin },
  sequenceInfo: async () => scenario.sequence ?? null,
};

// 실제 Controller: 재연결 없음(backoff []), 조회는 지연 없이.
const ctl = new Controller({ host, clientName: "fixture", clientVersion: "0", ws: helper.factory, backoff: [], probeDebounceMs: 0 });
const panelHost: PanelHost = { revealFile: () => undefined };
// 상호작용 테스트가 실제 상태를 읽을 수 있게(픽스처 페이지 전용).
(window as unknown as { __birdcage: { ctl: Controller } }).__birdcage = { ctl };

async function main() {
  const started = ctl.start();
  if (!scenario.startHelper || name === "connection-failed") await started;
  await scenario.run?.(ctl, helper);
  ReactDOM.createRoot(document.getElementById("app") as HTMLElement).render(
    <React.StrictMode>
      <App controller={ctl} host={panelHost} />
    </React.StrictMode>,
  );
  // 렌더와 effect(ctl.start 재호출 등)가 가라앉은 뒤 준비 표시.
  for (let i = 0; i < 5; i++) await new Promise((r) => setTimeout(r, 0));
  // 미리보기 비디오가 있으면 첫 탐색(In 위치)이 끝나 프레임이 그려질 때까지 기다린다.
  const video = document.querySelector("video");
  if (video) {
    const t0 = Date.now();
    while ((video.readyState < 2 || video.seeking) && Date.now() - t0 < 5000) await new Promise((r) => setTimeout(r, 20));
    await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
  }
  document.getElementById("app")!.dataset.ready = "1";
}

main().catch((e) => {
  document.getElementById("app")!.dataset.ready = "error";
  document.getElementById("app")!.textContent = String(e);
});
