import { describe, expect, it } from "vitest";
import { codecLabel, fpsMismatch, recommendVariant, targetRes, variantLabel } from "../src/choice";
import { duration, fpsLabel, planLabel, platformLabel, progressKnown } from "../src/format";
import type { Variant } from "../src/protocol";

const v = (formatId: string, res: number, transcode = false, extra: Partial<Variant> = {}): Variant => ({
  formatId,
  res,
  vcodec: transcode ? "vp09.00.51.08" : "avc1.640028",
  transcode,
  ...extra,
});

describe("recommendVariant", () => {
  const vs = [v("313+140", 2160, true), v("271+140", 1440, true), v("137+140", 1080), v("136+140", 720), v("18", 360)];

  it("picks the highest resolution not above the sequence", () => {
    expect(recommendVariant(vs, 1080)?.formatId).toBe("137+140");
    expect(recommendVariant(vs, 2160)?.formatId).toBe("313+140"); // 4K 시퀀스면 변환이 필요해도 4K
    expect(recommendVariant(vs, 900)?.formatId).toBe("136+140");
  });

  it("prefers no-transcode at the same resolution and falls back to the smallest", () => {
    expect(recommendVariant([v("a", 1080, true), v("b", 1080)], 1080)?.formatId).toBe("b");
    expect(recommendVariant([v("big", 2160, true), v("mid", 1440, true)], 720)?.formatId).toBe("mid");
    expect(recommendVariant([], 1080)).toBeUndefined();
  });

  it("targets the sequence short side, else the max-quality setting", () => {
    expect(targetRes({ name: "s", width: 1080, height: 1920, fps: 30 }, 2160)).toBe(1080); // 세로 시퀀스
    expect(targetRes(null, 720)).toBe(720);
  });
});

describe("fpsMismatch", () => {
  it("accepts equal rates and clean multiples", () => {
    expect(fpsMismatch(30, 30)).toBe(false);
    expect(fpsMismatch(29.97, 29.97)).toBe(false);
    expect(fpsMismatch(60, 30)).toBe(false);
    expect(fpsMismatch(30, 60)).toBe(false);
    expect(fpsMismatch(59.94, 29.97)).toBe(false);
    expect(fpsMismatch(undefined, 30)).toBe(false);
  });
  it("flags rates that judder", () => {
    expect(fpsMismatch(25, 29.97)).toBe(true);
    expect(fpsMismatch(24, 30)).toBe(true);
    expect(fpsMismatch(30, 29.97)).toBe(false); // 0.1% 차이: 변환 없이 쓴다
    expect(fpsMismatch(23.976, 25)).toBe(true);
  });
});

describe("labels", () => {
  it("formats variants, codecs, durations and platforms", () => {
    expect(variantLabel({ res: 1080, fps: 60 })).toBe("1080p60");
    expect(variantLabel({ res: 720, fps: 30 })).toBe("720p");
    expect(codecLabel("avc1.640028")).toBe("H.264");
    expect(codecLabel("vp09.00.51.08")).toBe("VP9");
    expect(codecLabel("av01.0.08M.08")).toBe("AV1");
    expect(codecLabel("bytevc1")).toBe("HEVC");
    expect(duration(19)).toBe("0:19");
    expect(duration(3723)).toBe("1:02:03");
    expect(fpsLabel(29.97)).toBe("29.97fps");
    expect(platformLabel("Youtube")).toBe("YouTube");
  });
});

describe("job labels", () => {
  const plan = { profile: "compat" as const, video: "copy" as const, audio: "copy" as const, passthrough: false, reasons: [] };
  it("names what normalization did", () => {
    expect(planLabel({ ...plan, passthrough: true })).toBe("원본 그대로");
    expect(planLabel({ ...plan, audio: "transcode", aligned: true })).toBe("구간 정렬(오디오만 재인코딩)");
    expect(planLabel({ ...plan, video: "none", audio: "transcode", audioOnly: true })).toBe("WAV");
    expect(planLabel({ ...plan, video: "transcode", encoder: "h264_videotoolbox" })).toBe("변환(h264_videotoolbox)");
    expect(planLabel({ ...plan, audio: "transcode" })).toBe("오디오만 재인코딩");
    expect(planLabel(plan)).toBe("리먹스");
  });
  it("knows when download progress is unmeasurable", () => {
    const j = { jobId: "j", url: "u", options: { maxHeight: 1, profile: "" as const }, createdAt: "", updatedAt: "" };
    expect(progressKnown({ ...j, state: "downloading", progress: -1 })).toBe(false);
    expect(progressKnown({ ...j, state: "downloading", progress: 0.2 })).toBe(true);
    expect(progressKnown({ ...j, state: "probing", progress: -1 })).toBe(true);
  });
});
