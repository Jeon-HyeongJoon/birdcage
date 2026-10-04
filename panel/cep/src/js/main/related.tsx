// 탐색 목록: 미리보기 옆(넓은 패널) 또는 아래(좁은 패널)에서 다음 레퍼런스를 고른다(docs/decisions/0012).
// 유튜브 검색 결과도 같은 목록의 '검색' 탭에 보여 미리보기를 유지한 채 고른다(docs/decisions/0013).
// 카드 없이 행·구분선만 쓴다. 추천 목록은 보이거나 미리보기 준비가 끝난 뒤에 받는다(미리보기와 대역폭을 다투지 않게).
import { FormEvent, useEffect, useState } from "react";
import { RelatedItem, RelatedSource, State, duration, supportsRelated } from "@birdcage/core";
import type { PanelController } from "./main";

const TABS: Array<{ source: RelatedSource; label: string; about: string }> = [
  { source: "youtube", label: "추천", about: "유튜브가 이 영상 옆에 보여 주는 '다음 동영상'" },
  { source: "channel", label: "같은 채널", about: "이 채널의 최근 영상" },
  { source: "search", label: "비슷한 제목", about: "제목 핵심어로 검색한 영상" },
];

const SOURCE_LABEL: Record<RelatedSource, string> = { youtube: "유튜브 추천", channel: "같은 채널", search: "제목 검색" };

/** Browse-list item opened from the search tab (recorded by the helper for the fitness review). */
export const SEARCH_VIA = "search:youtube";

export const RelatedPanel = ({ s, ctl, downloaded }: { s: State; ctl: PanelController; downloaded: Set<string> }) => {
  const d = s.draft;
  const search = s.search;
  const [el, setEl] = useState<HTMLElement | null>(null);
  const visible = useVisible(el);
  const ready = d.status === "ready" && supportsRelated(d.info);
  const probingFromList = d.status === "probing" && !!d.hint;
  const hasRelated = ready || probingFromList;
  const showSearch = !!search && (s.listTab === "search" || !hasRelated);
  const previewDone = d.preview?.stage === "ready" || d.preview?.stage === "failed";
  useEffect(() => {
    if (ready && !showSearch && (visible || previewDone)) ctl.loadRelated();
  }, [ready, showSearch, visible, previewDone, d.probeId, ctl]);
  useEffect(() => {
    // 좁은 패널에서 미리보기를 보다 검색하면 결과는 아래에 있다: 결과 머리로 내려간다.
    if (el && search?.status === "loading" && el.getBoundingClientRect().top > window.innerHeight * 0.6) el.scrollIntoView();
  }, [el, search?.query, search?.status]);

  if (!hasRelated && !search) return null;
  const tab = d.related?.tab ?? "youtube";
  const list = d.related?.lists[tab];
  // 미리보기가 없으면(검색만 했을 때) 넓은 패널에서도 목록이 본문 폭을 쓴다.
  const full = d.status !== "ready" && d.status !== "probing";
  const open = (it: RelatedItem, via?: string) => {
    ctl.openRelated(it, via);
    // 좁은 패널에서는 목록이 미리보기 아래에 있다: 새 영상의 미리보기로 올라간다.
    const top = document.querySelector(".composer");
    if (top && top.getBoundingClientRect().top < 0) top.scrollIntoView();
  };
  return (
    <aside className={full ? "related full" : "related"} ref={setEl} aria-label="탐색 목록">
      <div className="related-tabs" role="tablist">
        {search && (
          <button type="button" role="tab" aria-selected={showSearch} className={showSearch ? "tab on" : "tab"} title={`"${search.query}" 유튜브 검색 결과`} onClick={() => ctl.showList("search")}>
            검색
          </button>
        )}
        {hasRelated &&
          TABS.map((t) => (
            <button key={t.source} type="button" role="tab" aria-selected={!showSearch && t.source === tab} className={!showSearch && t.source === tab ? "tab on" : "tab"} title={t.about} disabled={!ready} onClick={() => ctl.loadRelated(t.source)}>
              {t.label}
            </button>
          ))}
      </div>
      {showSearch && search ? (
        search.status === "loading" ? (
          <Skeleton />
        ) : search.status === "failed" ? (
          <Note text={search.error ?? "검색 결과를 가져오지 못했습니다."} retry={() => ctl.search(search.query)} />
        ) : (
          <>
            <Rows items={search.items} empty="검색 결과가 없습니다." downloaded={downloaded} open={(it) => open(it, SEARCH_VIA)} />
            {search.moreError && <Note text={search.moreError} retry={() => ctl.searchMore()} />}
            {search.more && !search.moreError && (
              <button type="button" className="related-more" disabled={search.loadingMore} onClick={() => ctl.searchMore()}>
                {search.loadingMore ? "불러오는 중…" : "더 보기"}
              </button>
            )}
          </>
        )
      ) : !ready || !list || list.status === "loading" ? (
        <Skeleton />
      ) : list.status === "failed" ? (
        <Note text={list.error ?? "목록을 가져오지 못했습니다."} retry={() => ctl.loadRelated(tab)} />
      ) : (
        <>
          {list.source && list.source !== tab && <div className="related-note muted small">{SOURCE_LABEL[tab]}을 가져오지 못해 {SOURCE_LABEL[list.source]} 결과를 보여 줍니다.</div>}
          <Rows items={list.items} empty="영상이 없습니다." downloaded={downloaded} open={(it) => open(it)} />
        </>
      )}
    </aside>
  );
};

/** 상단 유튜브 검색창(Enter). TikTok 검색은 로그인 없이 결과가 없어 넣지 않았다(docs/decisions/0013). */
export const SearchBox = ({ s, ctl }: { s: State; ctl: PanelController }) => {
  const [q, setQ] = useState(s.search?.query ?? "");
  const current = s.search?.query;
  useEffect(() => {
    if (current !== undefined) setQ(current); // 링크 칸에서 시작한 검색도 여기에 보인다
  }, [current]);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    ctl.search(q);
  };
  return (
    <form className="search-box" role="search" onSubmit={submit}>
      <input className="search" type="text" placeholder="유튜브 검색" value={q} onChange={(e) => setQ(e.target.value)} />
    </form>
  );
};

const Note = ({ text, retry }: { text: string; retry: () => void }) => (
  <div className="related-note">
    <span className="muted small">{text}</span>
    <button type="button" onClick={retry}>
      다시 시도
    </button>
  </div>
);

const Rows = ({ items, empty, downloaded, open }: { items: RelatedItem[]; empty: string; downloaded: Set<string>; open: (it: RelatedItem) => void }) => {
  if (items.length === 0) return <div className="related-note muted small">{empty}</div>;
  return (
    <ul className="related-list">
      {items.map((it) => (
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
