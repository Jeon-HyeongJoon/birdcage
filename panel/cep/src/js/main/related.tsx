// 탐색 목록: 미리보기 옆(넓은 패널) 또는 아래(좁은 패널)에서 다음 레퍼런스를 고른다(docs/decisions/0012).
// 카드 없이 행·구분선만 쓴다. 목록은 보이거나 미리보기 준비가 끝난 뒤에 받는다(미리보기와 대역폭을 다투지 않게).
import { useEffect, useState } from "react";
import { RelatedItem, RelatedListState, RelatedSource, State, duration, supportsRelated } from "@birdcage/core";
import type { PanelController } from "./main";

const TABS: Array<{ source: RelatedSource; label: string; about: string }> = [
  { source: "youtube", label: "추천", about: "유튜브가 이 영상 옆에 보여 주는 '다음 동영상'" },
  { source: "channel", label: "같은 채널", about: "이 채널의 최근 영상" },
  { source: "search", label: "비슷한 제목", about: "제목 핵심어로 검색한 영상" },
];

const SOURCE_LABEL: Record<RelatedSource, string> = { youtube: "유튜브 추천", channel: "같은 채널", search: "제목 검색" };

export const RelatedPanel = ({ s, ctl, downloaded }: { s: State; ctl: PanelController; downloaded: Set<string> }) => {
  const d = s.draft;
  const [el, setEl] = useState<HTMLElement | null>(null);
  const visible = useVisible(el);
  const ready = d.status === "ready" && supportsRelated(d.info);
  const previewDone = d.preview?.stage === "ready" || d.preview?.stage === "failed";
  useEffect(() => {
    if (ready && (visible || previewDone)) ctl.loadRelated();
  }, [ready, visible, previewDone, d.probeId, ctl]);

  // 목록에서 연 영상을 조회하는 동안에도 자리를 지켜 배치가 흔들리지 않게 한다.
  if (!ready && !(d.status === "probing" && d.hint)) return null;
  const tab = d.related?.tab ?? "youtube";
  const list = d.related?.lists[tab];
  return (
    <aside className="related" ref={setEl} aria-label="탐색 목록">
      <div className="related-tabs" role="tablist">
        {TABS.map((t) => (
          <button key={t.source} type="button" role="tab" aria-selected={t.source === tab} className={t.source === tab ? "tab on" : "tab"} title={t.about} disabled={!ready} onClick={() => ctl.loadRelated(t.source)}>
            {t.label}
          </button>
        ))}
      </div>
      {!ready || !list || list.status === "loading" ? <Skeleton /> : <Rows list={list} tab={tab} ctl={ctl} downloaded={downloaded} />}
    </aside>
  );
};

const Rows = ({ list, tab, ctl, downloaded }: { list: RelatedListState; tab: RelatedSource; ctl: PanelController; downloaded: Set<string> }) => {
  if (list.status === "failed") {
    return (
      <div className="related-note">
        <span className="muted small">{list.error ?? "목록을 가져오지 못했습니다."}</span>
        <button type="button" onClick={() => ctl.loadRelated(tab)}>
          다시 시도
        </button>
      </div>
    );
  }
  if (list.items.length === 0) return <div className="related-note muted small">영상이 없습니다.</div>;
  const open = (it: RelatedItem) => {
    ctl.openRelated(it);
    // 좁은 패널에서는 목록이 미리보기 아래에 있다: 새 영상의 미리보기로 올라간다.
    const top = document.querySelector(".composer");
    if (top && top.getBoundingClientRect().top < 0) top.scrollIntoView();
  };
  return (
    <>
      {list.source && list.source !== tab && <div className="related-note muted small">{SOURCE_LABEL[tab]}을 가져오지 못해 {SOURCE_LABEL[list.source]} 결과를 보여 줍니다.</div>}
      <ul className="related-list">
        {list.items.map((it) => (
          <li key={it.id}>
            <button type="button" className="related-item" title={it.title} onClick={() => open(it)}>
              <span className="thumb">
                {it.thumbnail && <img src={it.thumbnail} alt="" />}
                {!!it.durationSec && <span className="dur">{duration(it.durationSec)}</span>}
              </span>
              <span className="text">
                <span className="title">{it.title}</span>
                <span className="sub muted">{it.channel}</span>
                <span className="sub muted">
                  {downloaded.has(it.id) && <span className="ok">✓ 받음 · </span>}
                  {it.meta ?? (it.views ? `조회수 ${views(it.views)}회` : "")}
                </span>
              </span>
            </button>
          </li>
        ))}
      </ul>
    </>
  );
};

const Skeleton = () => (
  <ul className="related-list skeleton" aria-busy="true">
    {[0, 1, 2, 3, 4].map((i) => (
      <li key={i}>
        <span className="related-item">
          <span className="thumb" />
          <span className="text">
            <span className="line" />
            <span className="line short" />
          </span>
        </span>
      </li>
    ))}
  </ul>
);

/** 1234 → "1,234", 12345 → "1.2만", 123456789 → "1.2억" (YouTube 한국어 표기). */
export function views(n: number): string {
  if (n >= 1e8) return `${short(n / 1e8)}억`;
  if (n >= 1e4) return `${short(n / 1e4)}만`;
  return n.toLocaleString("ko-KR");
}

const short = (x: number) => (x >= 10 ? Math.floor(x).toString() : (Math.floor(x * 10) / 10).toString());

/** Whether the element is in the viewport now (IntersectionObserver: Chrome 51+, CEP 10 OK). */
function useVisible(el: HTMLElement | null): boolean {
  const [visible, setVisible] = useState(false);
  useEffect(() => {
    if (!el) return;
    if (typeof IntersectionObserver === "undefined") {
      setVisible(true);
      return;
    }
    const io = new IntersectionObserver((entries) => setVisible(entries.some((e) => e.isIntersecting)));
    io.observe(el);
    return () => io.disconnect();
  }, [el]);
  return visible;
}
