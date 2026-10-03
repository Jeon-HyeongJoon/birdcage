import React from "react";
import ReactDOM from "react-dom/client";
import { Controller, type HostAdapter } from "@birdcage/core";
import "../../src/js/index.scss";
import { App, type PanelHost } from "../../src/js/main/main";
import { FakeHelper } from "./fake-helper";
import { hello, scenarios } from "./scenarios";

const scenario = scenarios["preview-downloading"];
const helper = new FakeHelper({ hello, probe: scenario.probe, preview: scenario.preview });
const host: HostAdapter = {
  info: () => ({ app: "PPRO", version: "15.2.0", api: "cep", os: "mac" }),
  startHelper: async () => ({ port: 31415, token: "fixture" }),
  stopHelper: () => undefined,
  importFile: async (_path, bin) => ({ ok: true, bin }),
  sequenceInfo: async () => scenario.sequence ?? null,
};
const ctl = new Controller({ host, clientName: "fixture", clientVersion: "0", ws: helper.factory, backoff: [], probeDebounceMs: 0 });
let opened = 0;
const panelHost: PanelHost = { revealFile: () => undefined, openDownloads: () => opened++ };

(window as unknown as { __birdcageFocus: { ctl: Controller; opened: () => number } }).__birdcageFocus = { ctl, opened: () => opened };

async function main() {
  await ctl.start();
  await scenario.run?.(ctl, helper);
  ReactDOM.createRoot(document.getElementById("app") as HTMLElement).render(<App controller={ctl} host={panelHost} />);
  for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
  document.getElementById("app")!.dataset.ready = "1";
}

void main();
