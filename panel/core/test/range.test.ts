import { describe, expect, it } from "vitest";
import { Controller, rangeToSend } from "../src/controller";
import type { HostAdapter, ImportOptions } from "../src/host";
import { formatTime, parseTime, startFromURL } from "../src/time";
import { FakeHelper, flush, job, mediaInfo } from "./fake";

describe("time entry", () => {
  it("parses editor and link formats", () => {
    expect(parseTime("83.5")).toBe(83.5);
    expect(parseTime("1:23.5")).toBe(83.5);
    expect(parseTime("1:02:03")).toBe(3723);
    expect(parseTime("1m30s")).toBe(90);
    expect(parseTime("90s")).toBe(90);
    expect(parseTime("1h2m")).toBe(3720);
    expect(parseTime("1:75")).toBeUndefined();
    expect(parseTime("abc")).toBeUndefined();
    expect(parseTime("")).toBeUndefined();
  });
  it("formats tenths", () => {
    expect(formatTime(5.3)).toBe("0:05.3");
    expect(formatTime(83.46)).toBe("1:23.5");
    expect(formatTime(3723)).toBe("1:02:03.0");
  });
  it("reads the start time of share links", () => {
    expect(startFromURL("https://youtu.be/x?t=90")).toBe(90);
    expect(startFromURL("https://www.youtube.com/watch?v=x&t=1m30s")).toBe(90);
    expect(startFromURL("https://www.youtube.com/watch?v=x#t=45")).toBe(45);
    expect(startFromURL("https://www.youtube.com/embed/x?start=12")).toBe(12);
    expect(startFromURL("https://www.youtube.com/watch?v=x")).toBeUndefined();
    expect(startFromURL("not a url")).toBeUndefined();
  });
});

describe("rangeToSend", () => {
  it("validates against the duration", () => {
    expect(rangeToSend({ on: false, start: 1, end: 2 }, 10)).toEqual({});
    expect(rangeToSend({ on: true, start: 5.3, end: 8.1 }, 10)).toEqual({ range: { startSec: 5.3, endSec: 8.1 } });
    expect(rangeToSend({ on: true, start: 5, end: 99 }, 10)).toEqual({ range: { startSec: 5, endSec: 10 } });
    expect(rangeToSend({ on: true, start: 0, end: 10 }, 10)).toEqual({}); // 전체와 같으면 구간이 아니다
    expect(rangeToSend({ on: true, start: 8, end: 5 }, 10).error).toMatch(/뒤여야/);
    expect(rangeToSend({ on: true, start: 12, end: 15 }, 10).error).toMatch(/길이/);
    expect(rangeToSend({ on: true, start: 2 }, undefined).error).toMatch(/끝 시간/);
  });
});

describe("range through the controller", () => {
  it("prefills t= from the link, sends the range and passes clip In/Out to import", async () => {
    const fh = new FakeHelper();
    const timers: Array<() => void> = [];
    const imports: Array<{ path: string; opts?: ImportOptions }> = [];
    const host: HostAdapter = {
      info: () => ({ app: "PPRO", version: "15.2.0", api: "cep", os: "mac" }),
      startHelper: async () => ({ port: 1, token: "t" }),
      stopHelper: () => {},
      importFile: async (path, bin, opts) => {
        imports.push({ path, opts });
        return { ok: true, bin, inout: "set" };
      },
    };
    const ctl = new Controller({ host, clientName: "t", clientVersion: "0", ws: fh.factory, setTimer: (fn) => timers.push(fn) });
    fh.probeReply = (url) => mediaInfo(url, [{ formatId: "18", res: 360 }]);
    await ctl.start();
    const url = "https://www.youtube.com/watch?v=x&t=1m5s";
    ctl.setUrl(url);
    timers.splice(0).forEach((f) => f());
    await flush();
    await flush();
    expect(ctl.state.draft.range).toEqual({ on: true, start: 65, end: 125 });
    ctl.setRange({ end: 70.5 });
    await ctl.submitDraft();
    expect(fh.last.sent.find((e) => e.type === "job.create")?.data).toEqual({
      url,
      maxHeight: 360,
      probeId: "p1",
      formatId: "18",
      range: { startSec: 65, endSec: 70.5 },
    });

    // 끝난 구간 작업은 clip In/Out을 가져오기에 넘긴다.
    fh.last.push("job.done", { job: job("j1", "done", { file: "/lib/a.mp4", clip: { inSec: 1.3, outSec: 6.8 } }) });
    await flush();
    await flush();
    expect(imports).toEqual([{ path: "/lib/a.mp4", opts: { clip: { inSec: 1.3, outSec: 6.8 } } }]);
    expect(ctl.state.jobs[0].import).toMatchObject({ state: "done", inout: "set" });
  });

  it("refuses an invalid range before sending", async () => {
    const fh = new FakeHelper();
    const timers: Array<() => void> = [];
    const host: HostAdapter = {
      info: () => ({ app: "PPRO", version: "15.2.0", api: "cep", os: "mac" }),
      startHelper: async () => ({ port: 1, token: "t" }),
      stopHelper: () => {},
      importFile: async (_p, bin) => ({ ok: true, bin }),
    };
    const ctl = new Controller({ host, clientName: "t", clientVersion: "0", ws: fh.factory, setTimer: (fn) => timers.push(fn) });
    fh.probeReply = (url) => mediaInfo(url, [{ formatId: "18", res: 360 }]);
    await ctl.start();
    ctl.setUrl("https://youtu.be/x");
    timers.splice(0).forEach((f) => f());
    await flush();
    await flush();
    ctl.setRange({ on: true, start: 30, end: 10 });
    await expect(ctl.submitDraft()).rejects.toThrow(/뒤여야/);
    expect(fh.last.sent.some((e) => e.type === "job.create")).toBe(false);
  });
});
