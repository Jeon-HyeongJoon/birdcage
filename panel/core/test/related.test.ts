import { describe, expect, it } from "vitest";
import { Controller, supportsRelated } from "../src/controller";
import type { HostAdapter } from "../src/host";
import type { Envelope, RelatedItem } from "../src/protocol";
import { FakeHelper, flush, mediaInfo } from "./fake";

// 탐색 목록(T-036): 목록 탭 → 항목 열기 → [← 이전 영상]으로 구간·화질·목록을 되살린다.
function setup() {
  const fh = new FakeHelper();
  let probes = 0;
  fh.probeReply = (url) => {
    const m = mediaInfo(url, [{ formatId: "137+140", res: 1080 }, { formatId: "18", res: 360 }]);
    return { ...m, probeId: `p${++probes}`, info: { ...m.info, id: new URL(url).searchParams.get("v") ?? "x" } };
  };
  let now = 1_000_000;
  const host: HostAdapter = {
    info: () => ({ app: "PPRO", version: "15.2.0", api: "cep", os: "mac" }),
    startHelper: async () => ({ port: 1, token: "t" }),
    stopHelper: () => {},
    importFile: async (_p, bin) => ({ ok: true, bin }),
  };
  const ctl = new Controller({ host, clientName: "t", clientVersion: "0", ws: fh.factory, setTimer: (fn) => fn(), now: () => now });
  const sent = (type: string) => fh.last.sent.filter((e) => e.type === type);
  const answer = (env: Envelope, items: RelatedItem[], extra: Record<string, unknown> = {}) => {
    const d = env.data as { probeId: string; source: string };
    fh.last.push("related.list", { probeId: d.probeId, requested: d.source, source: d.source, items, ...extra }, env.id);
  };
  return { fh, ctl, sent, answer, advance: (ms: number) => (now += ms) };
}

const A = "https://www.youtube.com/watch?v=a";
const item = (id: string): RelatedItem => ({ id, url: `https://www.youtube.com/watch?v=${id}`, title: `영상 ${id}`, channel: "채널", durationSec: 61, thumbnail: `https://i.ytimg.com/vi/${id}/mqdefault.jpg` });

async function ready(ctl: Controller) {
  for (let i = 0; i < 5; i++) await flush();
  expect(ctl.state.draft.status).toBe("ready");
}

describe("browse list", () => {
  it("loads a tab once, keeps the fallback source and ignores answers for an old link", async () => {
    const { ctl, sent, answer } = setup();
    await ctl.start();
    ctl.setUrl(A);
    await ready(ctl);
    expect(supportsRelated(ctl.state.draft.info)).toBe(true);

    ctl.loadRelated();
    ctl.loadRelated(); // 보이는 동안 여러 번 불려도 요청은 하나
    expect(sent("related.fetch")).toHaveLength(1);
    expect(sent("related.fetch")[0].data).toEqual({ probeId: "p1", source: "youtube" });
    expect(ctl.state.draft.related?.lists.youtube?.status).toBe("loading");
    answer(sent("related.fetch")[0], [item("b"), item("c")], { source: "channel" });
    await flush();
    const list = ctl.state.draft.related!.lists.youtube!;
    expect(list.status).toBe("ready");
    expect(list.items.map((i) => i.id)).toEqual(["b", "c"]);
    expect(list.source).toBe("channel"); // 시청 페이지 대신 같은 채널로 채웠다 → 그대로 표시

    ctl.loadRelated("search");
    expect(ctl.state.draft.related?.tab).toBe("search");
    expect(sent("related.fetch")[1].data).toEqual({ probeId: "p1", source: "search" });
    ctl.loadRelated("youtube"); // 이미 받은 탭은 다시 요청하지 않는다
    expect(sent("related.fetch")).toHaveLength(2);
    answer(sent("related.fetch")[1], [], { error: "목록을 가져오지 못했습니다", source: undefined });
    await flush();
    expect(ctl.state.draft.related?.lists.search).toEqual({ status: "failed", items: [], error: "목록을 가져오지 못했습니다" });
    ctl.loadRelated("search"); // 실패한 탭은 다시 시도
    expect(sent("related.fetch")).toHaveLength(3);

    ctl.setUrl("https://www.youtube.com/watch?v=z");
    answer(sent("related.fetch")[2], [item("late")]);
    await flush();
    expect(ctl.state.draft.related).toBeUndefined();
  });

  it("opens an item with a hint and via, then back restores range, quality and list", async () => {
    const { ctl, sent, answer, advance } = setup();
    await ctl.start();
    ctl.setUrl(A);
    await ready(ctl);
    ctl.selectVariant("18");
    ctl.setRange({ on: true, start: 5, end: 9 });
    ctl.loadRelated();
    answer(sent("related.fetch")[0], [item("b")]);
    await flush();

    ctl.openRelated(item("b"));
    expect(ctl.state.draft.status).toBe("probing");
    expect(ctl.state.draft.hint?.title).toBe("영상 b"); // 조회 중에도 썸네일·제목을 보여 준다
    expect(ctl.state.history).toHaveLength(1);
    expect(sent("preview.close").at(-1)?.data).toEqual({ probeId: "p1" });
    expect(sent("media.probe").at(-1)?.data).toEqual({ url: item("b").url, maxHeight: 1080, via: "related:youtube" });
    await ready(ctl);
    expect(ctl.state.draft.probeId).toBe("p2");
    expect(ctl.state.draft.related).toBeUndefined(); // 새 영상의 목록은 따로 받는다

    await ctl.submitDraft();
    expect(sent("job.create").at(-1)?.data).toMatchObject({ url: item("b").url, via: "related:youtube" });

    // 15분 안: 조회를 다시 하지 않고 그대로 되살린다.
    advance(60_000);
    const probesBefore = sent("media.probe").length;
    ctl.back();
    const d = ctl.state.draft;
    expect(sent("media.probe")).toHaveLength(probesBefore);
    expect(d.url).toBe(A);
    expect(d.probeId).toBe("p1");
    expect(d.formatId).toBe("18");
    expect(d.range).toEqual({ on: true, start: 5, end: 9 });
    expect(d.related?.lists.youtube?.items.map((i) => i.id)).toEqual(["b"]);
    expect(sent("preview.open").at(-1)?.data).toEqual({ probeId: "p1" });
    expect(ctl.state.history).toHaveLength(0);
  });

  it("re-probes an old entry or a dropped probe, keeping the choices", async () => {
    const { fh, ctl, sent, advance } = setup();
    await ctl.start();
    ctl.setUrl(A);
    await ready(ctl);
    ctl.selectVariant("18");
    ctl.setRange({ on: true, start: 5, end: 9 });
    ctl.openRelated(item("b"));
    await ready(ctl);

    advance(16 * 60_000); // Helper 조회 보존(20분)에 가까우면 다시 조회한다
    ctl.back();
    expect(ctl.state.draft.status).toBe("probing");
    expect(ctl.state.draft.hint?.title).toBe("제목");
    await ready(ctl);
    expect(ctl.state.draft.probeId).toBe("p3");
    expect(ctl.state.draft.formatId).toBe("18");
    expect(ctl.state.draft.range).toEqual({ on: true, start: 5, end: 9 });

    // 되살린 조회의 미리보기가 실패하면(조회가 버려짐) 한 번 다시 조회한다.
    ctl.openRelated(item("c"));
    await ready(ctl);
    ctl.back();
    expect(ctl.state.draft.restored).toBe(true);
    fh.last.push("preview.state", { probeId: "p3", stage: "failed", progress: -1, error: "링크 정보가 만료되었습니다. 다시 확인하세요." });
    expect(ctl.state.draft.status).toBe("probing");
    await ready(ctl);
    expect(ctl.state.draft.probeId).toBe("p5");
    expect(ctl.state.draft.range).toEqual({ on: true, start: 5, end: 9 });
    expect(ctl.state.draft.restored).toBeUndefined();
    expect(sent("media.probe")).toHaveLength(5);
  });

  it("only YouTube links have a list; leaving a ready link keeps at most 20 entries", async () => {
    const { ctl } = setup();
    expect(supportsRelated({ id: "x", title: "t", extractor: "TikTok", webpageUrl: "u" })).toBe(false);
    await ctl.start();
    for (let i = 0; i < 22; i++) {
      ctl.setUrl(`https://www.youtube.com/watch?v=v${i}`);
      await ready(ctl);
    }
    ctl.clearDraft();
    expect(ctl.state.history).toHaveLength(20);
    expect(ctl.state.history.at(-1)?.draft.url).toBe("https://www.youtube.com/watch?v=v21");
  });
});
