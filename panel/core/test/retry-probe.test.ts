import { describe, expect, it } from "vitest";
import { Controller } from "../src/controller";
import type { HostAdapter } from "../src/host";
import { FakeHelper, flush, mediaInfo } from "./fake";

const URL = "https://www.youtube.com/watch?v=x";

describe("probe retry presentation", () => {
  it("keeps the failure state while checking and switches only after success", async () => {
    const helper = new FakeHelper();
    const timers: Array<() => void> = [];
    const host: HostAdapter = {
      info: () => ({ app: "PPRO", version: "15.2.0", api: "cep", os: "mac" }),
      startHelper: async () => ({ port: 1, token: "t" }),
      stopHelper: () => undefined,
      importFile: async (_path, bin) => ({ ok: true, bin }),
    };
    const ctl = new Controller({ host, clientName: "test", clientVersion: "0", ws: helper.factory, setTimer: (fn) => timers.push(fn) });
    helper.probeReply = (url) => ({ url, failure: { kind: "tool_missing", message: "필요한 도구가 설치되지 않았습니다.", policy: "ask_user" } });

    await ctl.start();
    ctl.setUrl(URL);
    timers.shift()!();
    await flush();
    await flush();
    expect(ctl.state.draft).toMatchObject({ status: "failed", failure: { kind: "tool_missing" } });

    helper.probeReply = () => undefined; // keep the retry request pending
    const retry = ctl.probeDraft();
    expect(ctl.state.draft).toMatchObject({ status: "failed", checking: true, failure: { kind: "tool_missing" } });
    expect(ctl.state.draft.info).toBeUndefined();

    const probes = helper.last.sent.filter((e) => e.type === "media.probe");
    const request = probes[probes.length - 1];
    helper.last.push("media.info", mediaInfo(URL, [{ formatId: "18", res: 360 }]), request.id);
    await retry;

    expect(ctl.state.draft).toMatchObject({ status: "ready", checking: false, info: { title: "제목" } });
    expect(ctl.state.draft.failure).toBeUndefined();
  });
});
