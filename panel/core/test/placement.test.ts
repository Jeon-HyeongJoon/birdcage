import { describe, expect, it } from "vitest";
// ExtendScript 번들과 같은 순수 함수(ES3로 빌드되는 파일)를 Premiere 없이 검증한다.
import { firstFreeTrack, firstTargeted } from "../../cep/src/jsx/ppro/placement";
import { Controller, importOptions } from "../src/controller";
import type { HostAdapter, ImportOptions } from "../src/host";
import { FakeHelper, flush, job } from "./fake";

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
  const video = { container: "mp4", durationSec: 3, sizeBytes: 1, video: { index: 0, codec: "h264", pixFmt: "yuv420p", width: 1, height: 1, fps: 30 } };
  it("always places on a new empty track at the playhead (사용자 지시: 배치 선택 없음)", () => {
    expect(importOptions(job("j", "done"))).toEqual({ place: "newTrack", hasVideo: false });
    expect(importOptions(job("j", "done", { media: video, clip: { inSec: 1, outSec: 2 } }))).toEqual({
      place: "newTrack",
      hasVideo: true,
      clip: { inSec: 1, outSec: 2 },
    });
  });
});

describe("auto-import after download", () => {
  it("imports the finished file onto a new track with its In/Out", async () => {
    const fh = new FakeHelper();
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
    const ctl = new Controller({ host, clientName: "t", clientVersion: "0", ws: fh.factory });
    await ctl.start();
    fh.last.push("job.done", { job: job("j1", "done", { file: "/lib/a.mp4", clip: { inSec: 1, outSec: 3 } }) });
    await flush();
    await flush();
    expect(calls).toEqual([{ place: "newTrack", hasVideo: false, clip: { inSec: 1, outSec: 3 } }]);
    expect(ctl.state.jobs[0]).toMatchObject({ import: { state: "done", placed: "ok" } });
  });
});
