import React from "react";
import ReactDOM from "react-dom/client";
import type { Settings, State } from "@birdcage/core";
import "../../src/js/index.scss";
import { App, type PanelController, type PanelHost } from "../../src/js/main/main";
import { scenarios } from "./scenarios";

const scenarioName = new URLSearchParams(window.location.search).get("scenario") || "workflow";
const scenario = scenarios[scenarioName];
if (!scenario) throw new Error(`알 수 없는 UI 픽스처: ${scenarioName}`);

class FixtureController implements PanelController {
  readonly state: State;

  constructor() {
    this.state = scenario.state;
  }

  subscribe(): () => void {
    return () => undefined;
  }

  async start(): Promise<void> {}
  missingTools(): string[] {
    return scenario.missingTools;
  }
  setUrl(_url: string): void {}
  async probeDraft(): Promise<void> {}
  selectVariant(_formatId: string): void {}
  setRange(): void {}
  setExtras(): void {}
  async submitDraft(): Promise<void> {}
  retryConnection(): void {}
  installTools(): void {}
  updateSettings(_settings: Partial<Settings>): void {}
  clearFinished(): void {}
  cancel(_jobId: string): void {}
  async importJob(_jobId: string): Promise<void> {}
}

const host: PanelHost = { revealFile: () => undefined };

ReactDOM.createRoot(document.getElementById("app") as HTMLElement).render(
  <React.StrictMode>
    <App controller={new FixtureController()} host={host} />
  </React.StrictMode>,
);
