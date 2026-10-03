import { describe, expect, it } from "vitest";
import { Controller, isLink } from "../src/controller";
import type { HostAdapter } from "../src/host";
import { FakeHelper, flush, mediaInfo } from "./fake";

function setup(sequence: Awaited<ReturnType<NonNullable<HostAdapter["sequenceInfo"]>>> = null) {
  const fh = new FakeHelper();
  const timers: Array<() => void> = [];
  const host: HostAdapter = {
    info: () => ({ app: "PPRO", version: "15.2.0", api: "cep", os: "mac" }),
    startHelper: async () => ({ port: 1, token: "t" }),
    stopHelper: () => {},
    importFile: async (_p, bin) => ({ ok: true, bin }),
    sequenceInfo: async () => sequence,
  };
  const ctl = new Controller({ host, clientName: "t", clientVersion: "0", ws: fh.factory, setTimer: (fn) => timers.push(fn) });
  return { fh, ctl, fireTimers: () => timers.splice(0).forEach((fn) => fn()) };
}

const URL = "https://www.youtube.com/watch?v=x";

describe("draft: paste → probe → choose → download", () => {
  it("probes a pasted link after the debounce and recommends by the sequence", async () => {
    const { fh, ctl, fireTimers } = setup({ name: "Seq 01", width: 1920, height: 1080, fps: 29.97 });
    fh.probeReply = (url) =>
      mediaInfo(url, [
        { formatId: "313+140", res: 2160, transcode: true, vcodec: "vp09.00.51.08" },
        { formatId: "137+140", res: 1080, sizeBytes: 48_000_000 },
        { formatId: "18", res: 360 },
      ]);
    await ctl.start();
    ctl.setUrl(URL);
    expect(ctl.state.draft.status).toBe("idle");
    expect(fh.last.sent.some((e) => e.type === "media.probe")).toBe(false); // 입력 중에는 조회하지 않는다
    fireTimers();
    expect(ctl.state.draft.status).toBe("probing");
    await flush();
    await flush();
    const d = ctl.state.draft;
    expect(d.status).toBe("ready");
    expect(d.info?.title).toBe("제목");
    expect(d.recommendedId).toBe("137+140");
    expect(d.formatId).toBe("137+140");
    expect(ctl.state.sequence?.fps).toBe(29.97);

    ctl.selectVariant("313+140");
    ctl.selectVariant("nope"); // 없는 변형은 무시
    expect(ctl.state.draft.formatId).toBe("313+140");

    await ctl.submitDraft();
    const create = fh.last.sent.find((e) => e.type === "job.create");
    expect(create?.data).toEqual({ url: URL, maxHeight: 2160, probeId: "p1", formatId: "313+140" });
    // 받은 뒤에도 미리보기·선택을 유지한다(같은 영상의 다른 부분을 이어서 고르기).
    expect(ctl.state.draft).toMatchObject({ url: URL, status: "ready", formatId: "313+140", submitted: 1 });
    ctl.clearDraft();
    expect(ctl.state.draft).toEqual({ url: "", status: "idle", range: { on: false } });
  });

  it("ignores a stale probe answer after the link changed", async () => {
    const { fh, ctl, fireTimers } = setup();
    const answers: Array<(v: unknown) => void> = [];
    fh.probeReply = () => undefined; // 수동으로 응답
    await ctl.start();
    ctl.setUrl(URL);
    fireTimers();
    const probeEnv = fh.last.sent.find((e) => e.type === "media.probe")!;
    ctl.setUrl("https://www.youtube.com/watch?v=other");
    answers.push((v) => fh.last.push("media.info", v, probeEnv.id));
    answers[0](mediaInfo(URL, [{ formatId: "18", res: 360 }]));
    await flush();
    expect(ctl.state.draft.url).toBe("https://www.youtube.com/watch?v=other");
    expect(ctl.state.draft.status).toBe("idle");
    expect(ctl.state.draft.info).toBeUndefined();
  });

  it("shows a classified failure and still allows a plain download", async () => {
    const { fh, ctl, fireTimers } = setup();
    fh.probeReply = (url) => ({ url, failure: { kind: "private", message: "비공개 영상입니다.", policy: "stop" } });
    await ctl.start();
    ctl.setUrl(URL);
    fireTimers();
    await flush();
    await flush();
    expect(ctl.state.draft.status).toBe("failed");
    expect(ctl.state.draft.failure?.message).toBe("비공개 영상입니다.");
    await ctl.submitDraft();
    expect(fh.last.sent.find((e) => e.type === "job.create")?.data).toEqual({ url: URL, maxHeight: 1080 });
  });

  it("does not probe text that is not a link", async () => {
    const { fh, ctl, fireTimers } = setup();
    await ctl.start();
    ctl.setUrl("그냥 텍스트");
    fireTimers();
    expect(fh.last.sent.some((e) => e.type === "media.probe")).toBe(false);
    expect(isLink("youtu.be/x")).toBe(false);
    expect(isLink("https://youtu.be/x")).toBe(true);
  });
});
