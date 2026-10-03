// 미리보기 패널: 큰 16:9 플레이어 + 재생 조작 + 타임라인(필름스트립·선 파형·In/Out 핸들·재생헤드) + In/Out 입력.
// 미리보기에서 사용자가 가장 많은 정보를 얻는다(docs/decisions/0011): 플레이어는 패널 폭 전체를 쓴다.
// 좌표·확대·핸들 규칙은 @birdcage/core timeline(테스트됨)을 쓰고, 여기서는 그리기와 입력만 한다.
import { MouseEvent as ReactMouseEvent, WheelEvent, useEffect, useMemo, useRef, useState } from "react";
import {
  Draft,
  Handle,
  Storyboard,
  View,
  decodePeaks,
  fileUrl,
  fitView,
  follow,
  formatTime,
  fullView,
  handlesOf,
  hitHandle,
  markAt,
  moveHandle,
  panView,
  pickStoryboard,
  tickStep,
  tileAt,
  timeToX,
  waveLine,
  xToTime,
  zoomView,
} from "@birdcage/core";
import type { PanelController } from "./main";
import { TimeField } from "./time-field";

const STRIP_H = 36;
const WAVE_H = 30;

/**
 * Drag with window-level mouse events: works the same in CEF 74 (Premiere 15) and new Chrome, and keeps
 * following the mouse outside the element. 포인터 캡처 대신 창 전체에서 끝까지 따라간다.
 */
function startDrag(e: ReactMouseEvent, onMove: (clientX: number) => void, onEnd?: () => void): void {
  if (e.button !== 0) return;
  e.preventDefault();
  onMove(e.clientX);
  const move = (ev: MouseEvent) => onMove(ev.clientX);
  const up = () => {
    window.removeEventListener("mousemove", move);
    window.removeEventListener("mouseup", up);
    onEnd?.();
  };
  window.addEventListener("mousemove", move);
  window.addEventListener("mouseup", up);
}

/** Buttons that never take keyboard focus (Space stays play/pause instead of re-pressing the button). */
const noFocus = (e: ReactMouseEvent) => e.preventDefault();

export const PreviewPanel = ({ d, ctl }: { d: Draft; ctl: Pick<PanelController, "setRange"> }) => {
  const info = d.info!;
  const pv = d.preview;
  const duration = info.durationSec || pv?.video?.durationSec || pv?.peaks?.durationSec || 0;
  const boards = pv?.storyboard ? [pv.storyboard] : info.storyboards;
  const bigBoard = useMemo(() => pickStoryboard(boards, 180), [boards]);
  const stripBoard = useMemo(() => pickStoryboard(boards, STRIP_H), [boards]);
  const videoSrc = pv?.stage === "ready" && pv.video ? fileUrl(pv.video.path) : undefined;
  const fps = pv?.video?.fps || info.variants?.[0]?.fps || 30;

  const videoRef = useRef<HTMLVideoElement>(null);
  const [t, setT] = useState(d.range.on && d.range.start !== undefined ? d.range.start : 0);
  const [playing, setPlaying] = useState(false);
  const [view, setView] = useState<View>(() => fullView(duration));
  const [videoOk, setVideoOk] = useState(true);

  useEffect(() => setView(fullView(duration)), [duration, info.id]);
  useEffect(() => setVideoOk(true), [videoSrc]);

  const seek = (to: number) => {
    const c = Math.max(0, Math.min(duration, to));
    setT(c);
    if (videoRef.current && videoSrc) videoRef.current.currentTime = c;
  };
  const togglePlay = () => {
    const v = videoRef.current;
    if (!v || !videoSrc) return;
    if (v.paused) void v.play().catch(() => setPlaying(false));
    else v.pause();
  };
  const mark = (which: Handle) => ctl.setRange(markAt(d.range, which, t, duration));
  const step = (dt: number) => seek(t + dt);
  const zoom = (f: number) => setView((v) => zoomView(v, f, t, duration));

  // 단축키는 패널 어디서나(시간·링크 입력 중 제외). Space 재생/정지가 기본 동작.
  const keyRef = useRef<(e: KeyboardEvent) => void>(() => undefined);
  keyRef.current = (e: KeyboardEvent) => {
    const tag = (e.target as HTMLElement | null)?.tagName;
    if (tag === "INPUT" || tag === "SELECT" || tag === "TEXTAREA" || e.metaKey || e.ctrlKey || e.altKey) return;
    const frame = 1 / fps;
    const k = e.key;
    if (k === " ") togglePlay();
    else if (k === "i" || k === "I") mark("in");
    else if (k === "o" || k === "O") mark("out");
    else if (k === "ArrowLeft") step(e.shiftKey ? -1 : -frame);
    else if (k === "ArrowRight") step(e.shiftKey ? 1 : frame);
    else if (k === "Home") seek(0);
    else if (k === "End") seek(duration);
    else return;
    e.preventDefault();
  };
  useEffect(() => {
    const h = (e: KeyboardEvent) => keyRef.current(e);
    window.addEventListener("keydown", h);
    return () => window.removeEventListener("keydown", h);
  }, []);

  const r = d.range;
  const selLen = r.on && r.start !== undefined && r.end !== undefined ? r.end - r.start : undefined;
  return (
    <div className="preview-panel">
      <div className="player">
        <div className="frame" onClick={togglePlay}>
          {videoSrc && videoOk ? (
            <video
              ref={videoRef}
              src={videoSrc}
              preload="auto"
              playsInline
              onTimeUpdate={(e) => {
                const ct = e.currentTarget.currentTime;
                setT(ct);
                if (!e.currentTarget.paused) setView((v) => follow(v, ct, duration));
              }}
              onLoadedMetadata={(e) => {
                e.currentTarget.currentTime = t; // 열 때 In(또는 재생헤드) 위치를 보여 준다
              }}
              onPlay={() => setPlaying(true)}
              onPause={() => setPlaying(false)}
              onError={() => setVideoOk(false)}
            />
          ) : (
            <StillFrame board={bigBoard} t={t} fallback={info.thumbnail} />
          )}
          <div className="overlay time">
            {formatTime(t)} / {formatTime(duration)}
          </div>
          <StageChip d={d} />
        </div>
        <ScrubBar t={t} duration={duration} range={handlesOf(r, duration)} ranged={r.on} seek={seek} />
      </div>
      <div className="transport row">
        <button type="button" className="icon" disabled={!videoSrc || !videoOk} onMouseDown={noFocus} onClick={togglePlay} title="재생/정지 (Space)">
          {playing ? "❚❚" : "▶"}
        </button>
        <button type="button" className="icon" onMouseDown={noFocus} onClick={() => step(-1 / fps)} title="이전 프레임 (←)">
          ◀|
        </button>
        <button type="button" className="icon" onMouseDown={noFocus} onClick={() => step(1 / fps)} title="다음 프레임 (→)">
          |▶
        </button>
        <button type="button" onMouseDown={noFocus} onClick={() => mark("in")} title="재생헤드를 In으로 (I)">
          In
        </button>
        <button type="button" onMouseDown={noFocus} onClick={() => mark("out")} title="재생헤드를 Out으로 (O)">
          Out
        </button>
        <span className="spacer" />
        <button type="button" className="icon" onMouseDown={noFocus} onClick={() => zoom(0.5)} title="축소">
          −
        </button>
        <button type="button" className="icon" onMouseDown={noFocus} onClick={() => zoom(2)} title="확대">
          +
        </button>
        <button type="button" onMouseDown={noFocus} onClick={() => setView(fitView(r, duration))} title="구간에 맞춤">
          맞춤
        </button>
      </div>
      <Timeline d={d} duration={duration} t={t} view={view} setView={setView} seek={seek} board={stripBoard} setRange={(r) => ctl.setRange(r)} />
      <div className="row inout">
        <TimeField label="In" placeholder={formatTime(0)} value={r.on ? r.start : undefined} onCommit={(v) => ctl.setRange({ on: true, start: v ?? 0, end: r.on ? r.end : duration })} />
        <TimeField label="Out" placeholder={formatTime(duration)} value={r.on ? r.end : undefined} onCommit={(v) => ctl.setRange({ on: true, start: r.on ? r.start : 0, end: v ?? duration })} />
        <span className="muted small">{selLen !== undefined ? `${formatTime(selLen)} 길이` : "전체"}</span>
        <span className="spacer" />
        {r.on && (
          <button type="button" onClick={() => ctl.setRange({ on: false })}>
            구간 해제
          </button>
        )}
      </div>
    </div>
  );
};

/**
 * 재생 진행 막대형 스크러버(영상 전체 길이): 누른 곳으로 이동하고 손잡이를 끌면 따라간다.
 * 선택 구간(In–Out)은 막대 위에 띠로 보인다.
 */
const ScrubBar = ({
  t,
  duration,
  range,
  ranged,
  seek,
}: {
  t: number;
  duration: number;
  range: { start: number; end: number };
  ranged: boolean;
  seek: (t: number) => void;
}) => {
  const ref = useRef<HTMLDivElement>(null);
  const pct = (x: number) => `${duration > 0 ? Math.max(0, Math.min(100, (x / duration) * 100)) : 0}%`;
  const toTime = (clientX: number) => {
    const el = ref.current;
    if (!el || duration <= 0) return 0;
    const r = el.getBoundingClientRect();
    return (Math.max(0, Math.min(r.width, clientX - r.left)) / Math.max(1, r.width)) * duration;
  };
  return (
    <div className="scrub" ref={ref} onMouseDown={(e) => startDrag(e, (x) => seek(toTime(x)))} title="끌어서 이동">
      <div className="bar-bg">
        {ranged && <div className="band" style={{ left: pct(range.start), width: `calc(${pct(range.end)} - ${pct(range.start)})` }} />}
        <div className="played" style={{ width: pct(t) }} />
      </div>
      <div className="knob" style={{ left: pct(t) }} />
    </div>
  );
};

/** A storyboard tile scaled to fill the frame (before the playable video arrives). */
const StillFrame = ({ board, t, fallback }: { board?: Storyboard; t: number; fallback?: string }) => {
  const tile = board ? tileAt(board, t) : undefined;
  if (!tile) return fallback ? <img className="still" src={fallback} alt="" /> : <div className="still empty" />;
  // 프레임 폭을 100%로 두고 시트를 같은 배율로 키운다(백분율 위치: CSS만으로 반응형).
  const cols = tile.sheetW / tile.w;
  const rows = tile.sheetH / tile.h;
  const px = cols > 1 ? (tile.x / tile.w / (cols - 1)) * 100 : 0;
  const py = rows > 1 ? (tile.y / tile.h / (rows - 1)) * 100 : 0;
  return (
    <div
      className="still"
      style={{ backgroundImage: `url("${fileUrl(tile.url)}")`, backgroundSize: `${cols * 100}% ${rows * 100}%`, backgroundPosition: `${px}% ${py}%` }}
    />
  );
};

const StageChip = ({ d }: { d: Draft }) => {
  const pv = d.preview;
  const pct = pv && pv.progress >= 0 ? ` ${Math.round(pv.progress * 100)}%` : "";
  let text = "";
  if (!pv) text = "미리보기 준비 중";
  else if (pv.stage === "audio") text = `파형 받는 중${pct}`;
  else if (pv.stage === "video") text = `미리보기 받는 중${pct}`;
  else if (pv.stage === "failed") text = `미리보기 실패: ${pv.error ?? ""}`;
  if (!text) return null;
  return <div className={`overlay chip${pv?.stage === "failed" ? " bad" : ""}`}>{text}</div>;
};

const Timeline = ({
  d,
  duration,
  t,
  view,
  setView,
  seek,
  board,
  setRange,
}: {
  d: Draft;
  duration: number;
  t: number;
  view: View;
  setView: (f: (v: View) => View) => void;
  seek: (t: number) => void;
  board?: Storyboard;
  setRange: PanelController["setRange"];
}) => {
  const ref = useRef<HTMLDivElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);
  const [width, setWidth] = useState(0);
  const [hover, setHover] = useState<number | undefined>();
  const drag = useRef<Handle | "seek" | null>(null);
  const peaks = d.preview?.peaks;
  const bytes = useMemo(() => (peaks ? decodePeaks(peaks) : undefined), [peaks]);
  const r = d.range;

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const measure = () => setWidth(el.clientWidth);
    measure();
    const RO = (window as unknown as { ResizeObserver?: typeof ResizeObserver }).ResizeObserver;
    if (RO) {
      const ro = new RO(measure);
      ro.observe(el);
      return () => ro.disconnect();
    }
    window.addEventListener("resize", measure);
    return () => window.removeEventListener("resize", measure);
  }, []);

  // 선 파형: 막대·채움 없이 선 하나. 구간 밖은 흐리게, 안은 밝게(사용자 지시 2026-10-03).
  useEffect(() => {
    const c = canvas.current;
    if (!c || width <= 0) return;
    const dpr = window.devicePixelRatio || 1;
    c.width = Math.round(width * dpr);
    c.height = Math.round(WAVE_H * dpr);
    const g = c.getContext("2d");
    if (!g) return;
    g.setTransform(dpr, 0, 0, dpr, 0, 0);
    g.clearRect(0, 0, width, WAVE_H);
    if (!peaks || !bytes) return;
    const pts = waveLine(peaks, bytes, view.start, view.end, Math.max(1, Math.round(width)));
    const path = () => {
      g.beginPath();
      pts.forEach((lv, x) => {
        const y = WAVE_H - 2 - lv * (WAVE_H - 4);
        if (x === 0) g.moveTo(x + 0.5, y);
        else g.lineTo(x + 0.5, y);
      });
    };
    g.lineWidth = 1;
    g.lineJoin = "round";
    g.strokeStyle = "rgba(214, 214, 214, 0.4)";
    path();
    g.stroke();
    if (r.on && r.start !== undefined && r.end !== undefined) {
      const a = timeToX(r.start, width, view);
      const b = timeToX(r.end, width, view);
      g.save();
      g.beginPath();
      g.rect(a, 0, b - a, WAVE_H);
      g.clip();
      g.strokeStyle = "rgba(214, 214, 214, 0.95)";
      path();
      g.stroke();
      g.restore();
    }
  }, [peaks, bytes, view, width, r.on, r.start, r.end]);

  const xAt = (clientX: number) => clientX - (ref.current?.getBoundingClientRect().left ?? 0);
  const xOf = (e: WheelEvent) => xAt(e.clientX);
  // 손잡이를 잡으면 In/Out을, 그 밖은 재생헤드를 끈다(프로그레스 바처럼). 창 전체 마우스 이벤트로 끝까지 따라간다.
  const onDown = (e: ReactMouseEvent) => {
    const h = hitHandle(xAt(e.clientX), width, view, r, 6, duration);
    drag.current = h ?? "seek";
    let cur = r;
    startDrag(
      e,
      (clientX) => {
        const at = xToTime(xAt(clientX), width, view);
        if (h) {
          cur = moveHandle(cur, h, at, duration);
          setRange(cur);
          seek(h === "in" ? cur.start! : cur.end!); // 끄는 손잡이 위치의 프레임을 크게 보여 준다
        } else seek(at);
      },
      () => (drag.current = null),
    );
  };
  const onHover = (e: ReactMouseEvent) => setHover(xToTime(xAt(e.clientX), width, view));
  const onWheel = (e: WheelEvent) => {
    if (e.ctrlKey || e.metaKey) setView((v) => zoomView(v, e.deltaY < 0 ? 1.25 : 0.8, xToTime(xOf(e), width, v), duration));
    else setView((v) => panView(v, ((e.deltaX || e.deltaY) / Math.max(1, width)) * (v.end - v.start), duration));
  };

  const tiles = useMemo(() => {
    if (!board || width <= 0) return [];
    const tileW = (STRIP_H * board.width) / board.height;
    const n = Math.max(1, Math.ceil(width / tileW));
    return Array.from({ length: n }, (_, i) => {
      const tile = tileAt(board, xToTime((i + 0.5) * tileW, width, view));
      return { left: i * tileW, w: tileW, tile };
    });
  }, [board, width, view]);

  const hs = handlesOf(r, duration);
  const sel = duration > 0 ? { a: timeToX(hs.start, width, view), b: timeToX(hs.end, width, view) } : undefined;
  const step = tickStep(view, width);
  const ticks: number[] = [];
  for (let s = Math.ceil(view.start / step) * step; s <= view.end && ticks.length < 50; s += step) ticks.push(s);

  return (
    <div className="timeline-wrap">
      <div className="timeline" ref={ref} onMouseDown={onDown} onMouseMove={onHover} onMouseLeave={() => setHover(undefined)} onWheel={onWheel}>
        <div className="strip">
          {tiles.map(({ left, w, tile }, i) =>
            tile ? (
              <div
                key={i}
                className="tile"
                style={{
                  left,
                  width: w,
                  backgroundImage: `url("${fileUrl(tile.url)}")`,
                  backgroundSize: `${(tile.sheetW * STRIP_H) / tile.h}px ${(tile.sheetH * STRIP_H) / tile.h}px`,
                  backgroundPosition: `${(-tile.x * STRIP_H) / tile.h}px ${(-tile.y * STRIP_H) / tile.h}px`,
                }}
              />
            ) : null,
          )}
        </div>
        <canvas ref={canvas} className="wave" style={{ width: "100%", height: WAVE_H }} />
        {!d.preview?.peaks && <div className="wave-pending muted small">{d.preview?.stage === "failed" || d.preview?.stage === "ready" ? "파형 없음" : "파형 준비 중"}</div>}
        {sel && (
          <>
            {r.on && <div className="dim" style={{ left: 0, width: Math.max(0, sel.a) }} />}
            {r.on && <div className="dim" style={{ left: Math.min(width, sel.b), right: 0 }} />}
            {r.on && <div className="sel" style={{ left: sel.a, width: Math.max(0, sel.b - sel.a) }} />}
            {/* 손잡이는 구간이 없어도 처음·끝에 보인다(끌면 구간이 생긴다) */}
            <div className="handle in" style={{ left: Math.max(0, sel.a) }} title="In (끌어서 조정)" />
            <div className="handle out" style={{ left: Math.min(width - 6, sel.b - 6) }} title="Out (끌어서 조정)" />
          </>
        )}
        {hover !== undefined && <div className="hover" style={{ left: timeToX(hover, width, view) }} />}
        <div className="playhead" style={{ left: timeToX(t, width, view) }}>
          <div className="cap" />
        </div>
      </div>
      <div className="ticks muted">
        {ticks.map((s) => {
          const x = timeToX(s, width, view);
          // 양 끝 라벨은 안쪽으로 붙여 잘리지 않게 한다.
          const edge = x < 20 ? "first" : x > width - 20 ? "last" : "";
          return (
            <span key={s} className={edge} style={{ left: x }}>
              {formatTime(s).replace(/\.0$/, "")}
            </span>
          );
        })}
      </div>
    </div>
  );
};
