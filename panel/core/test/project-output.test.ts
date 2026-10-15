import { describe, expect, it } from "vitest";
import { Controller } from "../src/controller";
import type { HostAdapter } from "../src/host";
import { FakeHelper, flush } from "./fake";

function host(projectOutputDir: () => Promise<string | null>): HostAdapter {
  return {
    info: () => ({ app: "PPRO", version: "15.2.0", api: "cep", os: "mac" }),
    startHelper: async () => ({ port: 1234, token: "t" }),
    stopHelper: () => undefined,
    importFile: async (_path, bin) => ({ ok: true, bin }),
    projectOutputDir,
  };
}

describe("project-local downloads", () => {
  it("reads the active project directory for every download request", async () => {
    const helper = new FakeHelper();
    let destination = "/projects/one/Birdcage";
    const ctl = new Controller({
      host: host(async () => destination),
      clientName: "test",
      clientVersion: "0",
      ws: helper.factory,
    });

    await ctl.start();
    await flush();
    expect(ctl.state.outputDir).toBe(destination);

    await ctl.submit("https://youtu.be/one");
    expect(helper.last.sent[1].data).toEqual({ url: "https://youtu.be/one", maxHeight: 1080, outputDir: "/projects/one/Birdcage" });

    destination = "/projects/two/Birdcage";
    await ctl.submit("https://youtu.be/two");
    expect(helper.last.sent[2].data).toEqual({ url: "https://youtu.be/two", maxHeight: 1080, outputDir: "/projects/two/Birdcage" });
    expect(ctl.state.outputDir).toBe(destination);
  });

  it("omits the preference and shows the helper library for an unsaved project", async () => {
    const helper = new FakeHelper();
    const ctl = new Controller({
      host: host(async () => null),
      clientName: "test",
      clientVersion: "0",
      ws: helper.factory,
    });

    await ctl.start();
    await flush();
    await ctl.submit("https://youtu.be/fallback");

    expect(helper.last.sent[1].data).toEqual({ url: "https://youtu.be/fallback", maxHeight: 1080 });
    expect(ctl.state.outputDir).toBe("/lib");
  });
});
