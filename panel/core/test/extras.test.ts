import { describe, expect, it } from "vitest";
import { Controller, importOptions } from "../src/controller";
import type { HostAdapter } from "../src/host";
import { FakeHelper, flush, job, mediaInfo } from "./fake";

describe("audio only and subtitles", () => {
  it("sends audioOnly without a video variant and the chosen subtitle track", async () => {
    const fh = new FakeHelper();
    const timers: Array<() => void> = [];
    const host: HostAdapter = {
      info: () => ({ app: "PPRO", version: "15.2.0", api: "cep", os: "mac" }),
      startHelper: async () => ({ port: 1, token: "t" }),
      stopHelper: () => {},
      importFile: async (_p, bin) => ({ ok: true, bin }),
    };
    const ctl = new Controller({ host, clientName: "t", clientVersion: "0", ws: fh.factory, setTimer: (fn) => timers.push(fn) });
    fh.probeReply = (url) => mediaInfo(url, [{ formatId: "137+140", res: 1080 }]);
    await ctl.start();
    ctl.setUrl("https://youtu.be/x");
    timers.splice(0).forEach((f) => f());
    await flush();
    await flush();
    ctl.setExtras({ audioOnly: true, subtitle: "en-orig:auto" });
    await ctl.submitDraft();
    expect(fh.last.sent.find((e) => e.type === "job.create")?.data).toEqual({
      url: "https://youtu.be/x",
      maxHeight: 1080,
      probeId: "p1",
      audioOnly: true,
      subtitles: { lang: "en-orig", auto: true },
    });
  });

  it("clears the subtitle with null and imports the sidecar with the media", () => {
    const snap = job("j", "done", { file: "/a.wav", subtitleFile: "/a.en.srt" });
    expect(importOptions(snap)).toEqual({ subtitle: "/a.en.srt", place: "newTrack", hasVideo: false });
  });
});
