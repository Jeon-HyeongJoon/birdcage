import { describe, expect, it } from "vitest";
import { Controller } from "../src/controller";
import type { HostAdapter } from "../src/host";
import type { Envelope, RelatedItem } from "../src/protocol";
import { FakeHelper, flush, mediaInfo } from "./fake";

// 유튜브 검색(T-026): 검색 → 결과(검색 탭) → 더 보기 → 결과 열기(via search:youtube) → [←] 뒤에도 결과 유지.
function setup() {
  const fh = new FakeHelper();
  let probes = 0;
  fh.probeReply = (url) => {
    const m = mediaInfo(url, [{ formatId: "18", res: 360 }]);
    return { ...m, probeId: `p${++probes}`, info: { ...m.info, id: new URL(url).searchParams.get("v") ?? "x" } };
  };
  const host: HostAdapter = {
    info: () => ({ app: "PPRO", version: "15.2.0", api: "cep", os: "mac" }),
    startHelper: async () => ({ port: 1, token: "t" }),
    stopHelper: () => {},
    importFile: async (_p, bin) => ({ ok: true, bin }),
  };
  const ctl = new Controller({ host, clientName: "t", clientVersion: "0", ws: fh.factory, setTimer: (fn) => fn() });
  const sent = (type: string) => fh.last.sent.filter((e) => e.type === type);
  const answer = (env: Envelope, items: RelatedItem[], extra: Record<string, unknown> = {}) => {
    const d = env.data as { query: string; page?: number };
    fh.last.push("search.list", { query: d.query, page: d.page ?? 0, items, ...extra }, env.id);
  };
  return { ctl, sent, answer };
}

const item = (id: string): RelatedItem => ({ id, url: `https://www.youtube.com/watch?v=${id}`, title: `검색 ${id}`, channel: "채널", durationSec: 30 });
const ready = async (ctl: Controller) => {
  for (let i = 0; i < 5; i++) await flush();
};

describe("search", () => {
  it("searches, pages without duplicates, ignores stale answers", async () => {
    const { ctl, sent, answer } = setup();
    await ctl.start();
    ctl.search("  인터뷰   b-roll ");
    expect(ctl.state.listTab).toBe("search");
    expect(ctl.state.search).toMatchObject({ query: "인터뷰 b-roll", status: "loading" });
    expect(sent("search.fetch")[0].data).toEqual({ query: "인터뷰 b-roll" });
    answer(sent("search.fetch")[0], [item("a"), item("b")], { more: true });
    await flush();
    expect(ctl.state.search).toMatchObject({ status: "ready", page: 0, more: true });

    ctl.searchMore();
    ctl.searchMore(); // 받는 중에는 한 번만
    expect(sent("search.fetch")).toHaveLength(2);
    expect(sent("search.fetch")[1].data).toEqual({ query: "인터뷰 b-roll", page: 1 });
    expect(ctl.state.search?.loadingMore).toBe(true);
    answer(sent("search.fetch")[1], [item("b"), item("c")], { page: 1, more: false });
    await flush();
    expect(ctl.state.search?.items.map((i) => i.id)).toEqual(["a", "b", "c"]);
    expect(ctl.state.search).toMatchObject({ page: 1, more: false, loadingMore: false });
    ctl.searchMore(); // 더 없음
    expect(sent("search.fetch")).toHaveLength(2);

    ctl.search("첫 검색어");
    ctl.search("둘째 검색어");
    answer(sent("search.fetch")[2], [item("old")]);
    await flush();
    expect(ctl.state.search).toMatchObject({ query: "둘째 검색어", status: "loading" });
    answer(sent("search.fetch")[3], [], { error: "검색 결과를 가져오지 못했습니다" });
    await flush();
    expect(ctl.state.search).toMatchObject({ status: "failed", error: "검색 결과를 가져오지 못했습니다" });
    ctl.search("   ");
    expect(sent("search.fetch")).toHaveLength(4);
  });

  it("opening a result keeps the results and records via; back keeps them too", async () => {
    const { ctl, sent, answer } = setup();
    await ctl.start();
    ctl.setUrl("https://www.youtube.com/watch?v=cur");
    await ready(ctl);
    ctl.search("코끼리");
    answer(sent("search.fetch")[0], [item("e1"), item("e2")]);
    await flush();

    ctl.openRelated(item("e2"), "search:youtube");
    expect(sent("media.probe").at(-1)?.data).toMatchObject({ url: item("e2").url, via: "search:youtube" });
    await ready(ctl);
    expect(ctl.state.draft.info?.id).toBe("e2");
    expect(ctl.state.listTab).toBe("search");
    expect(ctl.state.search?.items).toHaveLength(2);

    ctl.loadRelated("channel"); // 추천 탭을 누르면 목록 쪽으로
    expect(ctl.state.listTab).toBe("related");
    ctl.showList("search");
    ctl.back();
    expect(ctl.state.draft.url).toBe("https://www.youtube.com/watch?v=cur");
    expect(ctl.state.listTab).toBe("search");
    expect(ctl.state.search?.items.map((i) => i.id)).toEqual(["e1", "e2"]);
  });
});
