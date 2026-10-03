import { describe, expect, it } from "vitest";
import { Controller } from "../src/controller";
import type { HostAdapter } from "../src/host";
import { decodePeaks, fileUrl, peakLevel, pickStoryboard, tileAt, waveLine } from "../src/preview";
import type { Peaks, Storyboard } from "../src/protocol";
import { FakeHelper, flush, mediaInfo } from "./fake";

// aqz-KE-bpKQ sb2 모양: 80×45, 10×10, 4.96s, 시트 2장(496s + 139s)
const sb: Storyboard = {
  width: 80,
  height: 45,
  cols: 10,
  rows: 10,
  intervalSec: 4.96,
  sprites: [
    { url: "s0", startSec: 0, durationSec: 496 },
    { url: "s1", startSec: 496, durationSec: 139 },
  ],
};

describe("storyboard tiles", () => {
  it("maps time to sprite and tile box", () => {
    expect(tileAt(sb, 0)).toMatchObject({ url: "s0", x: 0, y: 0, w: 80, h: 45, sheetW: 800, sheetH: 450 });
    expect(tileAt(sb, 4.96 * 11 + 1)).toMatchObject({ url: "s0", x: 80, y: 45 }); // 12번째 타일 = 2행 2열
    expect(tileAt(sb, 500)).toMatchObject({ url: "s1", x: 0, y: 0 });
    expect(tileAt(sb, 99999)).toMatchObject({ url: "s1" }); // 끝을 넘으면 마지막 타일
    expect(tileAt({ ...sb, sprites: [] }, 1)).toBeUndefined();
  });
  it("picks the smallest storyboard tall enough", () => {
    const list = [{ ...sb, height: 27 }, sb, { ...sb, height: 90 }, { ...sb, height: 180 }];
    expect(pickStoryboard(list, 40)?.height).toBe(45);
    expect(pickStoryboard(list, 120)?.height).toBe(180);
    expect(pickStoryboard(list, 999)?.height).toBe(180);
    expect(pickStoryboard(undefined, 40)).toBeUndefined();
  });
});

describe("waveform line", () => {
  const p: Peaks = { bucketSec: 0.01, count: 4, durationSec: 0.04, data: btoa(String.fromCharCode(0, 10, 63, 255)) };
  it("decodes and maps peaks to a dB line", () => {
    expect(Array.from(decodePeaks(p))).toEqual([0, 10, 63, 255]);
    expect(peakLevel(255)).toBe(1);
    expect(peakLevel(0)).toBe(0);
    expect(peakLevel(64)).toBeCloseTo(1 - 12 / 48, 1); // −12dB → 0.75
    const line = waveLine(p, decodePeaks(p), 0, 0.04, 2); // 열 2개: 앞 두 버킷 최대, 뒤 두 버킷 최대
    expect(line).toHaveLength(2);
    expect(line[0]).toBeCloseTo(peakLevel(10));
    expect(line[1]).toBe(1);
    expect(waveLine(p, decodePeaks(p), 0, 0, 10)).toEqual([]);
  });
});

describe("fileUrl", () => {
  it("builds file URLs for CEP", () => {
    expect(fileUrl("/Users/me/Library/Application Support/Birdcage/preview/a#1/p.mp4")).toBe(
      "file:///Users/me/Library/Application%20Support/Birdcage/preview/a%231/p.mp4",
    );
    expect(fileUrl("C:\\Users\\me\\p.mp4")).toBe("file:///C:/Users/me/p.mp4");
    expect(fileUrl("https://i.ytimg.com/sb/x.jpg")).toBe("https://i.ytimg.com/sb/x.jpg");
  });
});

describe("controller preview flow", () => {
  it("opens the preview on probe, keeps states for this probe only, closes on link change", async () => {
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
    expect(fh.last.sent.find((e) => e.type === "preview.open")?.data).toEqual({ probeId: "p1" });

    fh.last.push("preview.state", { probeId: "other", stage: "ready", progress: 1 });
    expect(ctl.state.draft.preview).toBeUndefined(); // 다른 조회의 상태는 무시
    fh.last.push("preview.state", { probeId: "p1", stage: "video", progress: -1, peaks: { bucketSec: 0.01, count: 1, durationSec: 0.01, data: "AA==" } });
    expect(ctl.state.draft.preview?.stage).toBe("video");

    ctl.setUrl("https://youtu.be/y");
    expect(fh.last.sent.filter((e) => e.type === "preview.close").map((e) => e.data)).toEqual([{ probeId: "p1" }]);
    expect(ctl.state.draft.preview).toBeUndefined();
  });
});
