import { describe, expect, it } from "vitest";
import { clampView, fitView, follow, fullView, handlesOf, hitHandle, markAt, MIN_SPAN, moveHandle, panView, tickStep, timeToX, xToTime, zoomView } from "../src/timeline";

const D = 600; // 10분

describe("view", () => {
  it("zooms around the anchor and clamps", () => {
    const v = zoomView(fullView(D), 4, 300, D);
    expect(v).toEqual({ start: 225, end: 375 }); // 300이 같은 위치(가운데)에 남는다
    expect(zoomView(fullView(D), 1e6, 0, D).end - zoomView(fullView(D), 1e6, 0, D).start).toBe(MIN_SPAN);
    expect(zoomView({ start: 0, end: 10 }, 0.001, 5, D)).toEqual({ start: 0, end: 600 });
    expect(panView({ start: 590, end: 600 }, 20, D)).toEqual({ start: 590, end: 600 });
    expect(clampView({ start: -5, end: 5 }, D)).toEqual({ start: 0, end: 10 });
    expect(fullView(1)).toEqual({ start: 0, end: MIN_SPAN }); // 짧은 영상도 최소 폭
  });
  it("fits the selection and follows playback", () => {
    expect(fitView({ on: true, start: 100, end: 120 }, D)).toEqual({ start: 97, end: 123 });
    expect(fitView({ on: false }, D)).toEqual({ start: 0, end: 600 });
    expect(follow({ start: 0, end: 100 }, 150, D)).toEqual({ start: 140, end: 240 });
    expect(follow({ start: 0, end: 100 }, 50, D)).toEqual({ start: 0, end: 100 });
  });
  it("maps pixels and times", () => {
    const v = { start: 100, end: 200 };
    expect(xToTime(200, 400, v)).toBe(150);
    expect(xToTime(-10, 400, v)).toBe(100);
    expect(timeToX(150, 400, v)).toBe(200);
    expect(tickStep({ start: 0, end: 600 }, 400)).toBe(120);
    expect(tickStep({ start: 0, end: 4 }, 400)).toBe(1);
  });
});

describe("handles", () => {
  const r = { on: true, start: 100, end: 120 };
  const v = { start: 90, end: 130 }; // 400px → 10px/s
  it("hits the nearer handle within tolerance", () => {
    expect(hitHandle(100, 400, v, r)).toBe("in");
    expect(hitHandle(305, 400, v, r)).toBe("out");
    expect(hitHandle(200, 400, v, r)).toBeNull();
    expect(hitHandle(100, 400, v, { on: false })).toBeNull(); // 길이를 모르면 손잡이 없음
    // 구간이 없어도 손잡이는 처음과 끝에 있다(양끝을 끌어 구간을 만든다).
    const whole = { start: 0, end: 600 };
    expect(hitHandle(3, 400, whole, { on: false }, 6, 600)).toBe("in");
    expect(hitHandle(398, 400, whole, { on: false }, 6, 600)).toBe("out");
  });
  it("moves a handle without crossing the other", () => {
    expect(moveHandle(r, "in", 110, D)).toEqual({ on: true, start: 110, end: 120 });
    expect(moveHandle(r, "in", 130, D)).toEqual({ on: true, start: 119.9, end: 120 });
    expect(moveHandle(r, "out", 50, D)).toEqual({ on: true, start: 100, end: 100.1 });
    expect(moveHandle({ on: false }, "out", 30, D)).toEqual({ on: true, start: 0, end: 30 }); // 핸들을 끌면 구간이 켜진다
  });
  it("marks In/Out like Premiere's I/O keys", () => {
    expect(markAt({ on: false }, "in", 42, D)).toEqual({ on: true, start: 42, end: 600 });
    expect(markAt(r, "out", 150, D)).toEqual({ on: true, start: 100, end: 150 });
    expect(markAt(r, "in", 130, D)).toEqual({ on: true, start: 130, end: 600 }); // Out을 넘으면 Out은 끝으로
    expect(markAt(r, "out", 90, D)).toEqual({ on: true, start: 0, end: 90 }); // In보다 앞이면 In은 처음으로
  });
});

describe("handles without a range", () => {
  it("sit at the start and end", () => {
    expect(handlesOf({ on: false }, 600)).toEqual({ start: 0, end: 600 });
    expect(handlesOf({ on: true, start: 5, end: 9 }, 600)).toEqual({ start: 5, end: 9 });
  });
});
