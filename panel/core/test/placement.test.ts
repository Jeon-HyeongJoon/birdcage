import { describe, expect, it } from "vitest";
// ExtendScript 번들과 같은 순수 함수(ES3로 빌드되는 파일)를 Premiere 없이 검증한다.
import { firstFreeTrack, firstTargeted } from "../../cep/src/jsx/ppro/placement";
import { Controller, importOptions } from "../src/controller";
import type { HostAdapter, ImportOptions } from "../src/host";
import { FakeHelper, flush, job, mediaInfo } from "./fake";

describe("firstFreeTrack", () => {
  // V1: 0–10s, V2: 12–20s, V3: 비어 있음
  const tracks: Array<Array<[number, number]>> = [[[0, 10]], [[12, 20]], []];
  it("finds the lowest track empty over the clip length", () => {
    expect(firstFreeTrack(tracks, 10, 12)).toBe(0); // V1은 10s에 끝남·V2는 12s부터: 맞닿음은 겹침이 아니다
    expect(firstFreeTrack(tracks, 9.9, 12)).toBe(1); // V1과는 0.1s 겹치고 V2와는 맞닿기만 한다
    expect(firstFreeTrack(tracks, 5, 8)).toBe(1);
    expect(firstFreeTrack(tracks, 11, 15)).toBe(0); // V1은 10s에 끝났다
    expect(firstFreeTrack(tracks, 9, 13)).toBe(2); // V1·V2 모두 겹친다
    expect(firstFreeTrack(tracks, 21, 30)).toBe(0);
  });
  it("returns -1 when every track is busy", () => {
    expect(firstFreeTrack([[[0, 100]], [[50, 60]]], 55, 56)).toBe(-1);
    expect(firstFreeTrack([], 0, 1)).toBe(-1);
  });
  it("uses the first targeted track, else 0", () => {
    expect(firstTargeted([false, true, true])).toBe(1);
    expect(firstTargeted([false, false])).toBe(0);
  });
});

describe("importOptions", () => {
  const done = job("j", "done", { file: "/a.mp4", clip: { inSec: 1, outSec: 2 }, media: { container: "mp4", durationSec: 3, sizeBytes: 1, video: { index: 0, codec: "h264", pixFmt: "yuv420p", width: 1, height: 1, fps: 30 } } });
  it("maps each placement", () => {
    expect(importOptions(job("j", "done"), "project")).toBeUndefined();
    expect(importOptions(done, "project")).toEqual({ clip: { inSec: 1, outSec: 2 } });
    expect(importOptions(done, "source")).toEqual({ clip: { inSec: 1, outSec: 2 }, openSource: true });
    expect(importOptions(done, "track")).toEqual({ clip: { inSec: 1, outSec: 2 }, place: "track", hasVideo: true });
    expect(importOptions(job("j", "done"), "insert")).toEqual({ place: "insert", hasVideo: false });
  });
});

describe("placement is captured when the download starts", () => {
  it("auto-import uses the placement chosen at submit, not the current setting", async () => {
    const fh = new FakeHelper();
    const timers: Array<() => void> = [];
    const calls: Array<ImportOptions | undefined> = [];
    const host: HostAdapter = {
      info: () => ({ app: "PPRO", version: "15.2.0", api: "cep", os: "mac" }),
      startHelper: async () => ({ port: 1, token: "t" }),
      stopHelper: () => {},
      importFile: async (_p, bin, opts) => {
        calls.push(opts);
        return { ok: true, bin, placed: "ok" };
      },
    };
    const ctl = new Controller({ host, clientName: "t", clientVersion: "0", ws: fh.factory, setTimer: (fn) => timers.push(fn) });
    fh.probeReply = (url) => mediaInfo(url, [{ formatId: "18", res: 360 }]);
    await ctl.start();
    ctl.updateSettings({ placement: "insert" });
    ctl.setUrl("https://youtu.be/x");
    timers.splice(0).forEach((f) => f());
    await flush();
    await flush();
    await ctl.submitDraft(); // → j1, 삽입으로 받기 시작
    ctl.updateSettings({ placement: "project" }); // 받는 중에 설정을 바꿔도
    fh.last.push("job.done", { job: job("j1", "done", { file: "/lib/a.mp4" }) });
    await flush();
    await flush();
    expect(calls).toEqual([{ place: "insert", hasVideo: false }]);
    expect(ctl.state.jobs[0]).toMatchObject({ placement: "insert", import: { state: "done", placed: "ok" } });
  });
});
