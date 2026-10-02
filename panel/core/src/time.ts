// Time entry for ranges: "83.5", "1:23.5", "1:02:03", "1m23s", "90s".
// 편집자는 타임코드처럼 쓰고, YouTube 링크는 t=1m30s 형식을 쓴다.

/** Parses a time string to seconds; undefined when it is not a time. */
export function parseTime(input: string): number | undefined {
  const s = input.trim().toLowerCase();
  if (!s) return undefined;
  if (/^\d+(\.\d+)?$/.test(s)) return Number(s);
  const colon = /^(?:(\d+):)?(\d{1,2}):(\d{1,2}(?:\.\d+)?)$/.exec(s);
  if (colon) {
    const [, h, m, sec] = colon;
    if (Number(sec) >= 60 || (h !== undefined && Number(m) >= 60)) return undefined;
    return Number(h ?? 0) * 3600 + Number(m) * 60 + Number(sec);
  }
  const units = /^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+(?:\.\d+)?)s)?$/.exec(s);
  if (units && (units[1] || units[2] || units[3])) {
    return Number(units[1] ?? 0) * 3600 + Number(units[2] ?? 0) * 60 + Number(units[3] ?? 0);
  }
  return undefined;
}

/** "0:05.3", "1:02:03.0" — tenths of a second (In/Out are refined in Premiere). */
export function formatTime(sec: number): string {
  const t = Math.max(0, Math.round(sec * 10) / 10);
  const h = Math.floor(t / 3600);
  const m = Math.floor((t % 3600) / 60);
  const s = (t % 60).toFixed(1).padStart(4, "0");
  return h > 0 ? `${h}:${String(m).padStart(2, "0")}:${s}` : `${m}:${s}`;
}

/** Start time encoded in a share link (YouTube t=/start=, #t=); undefined when absent. */
export function startFromURL(url: string): number | undefined {
  let u: URL;
  try {
    u = new URL(url.trim());
  } catch {
    return undefined;
  }
  const raw = u.searchParams.get("t") ?? u.searchParams.get("start") ?? /(?:^|&)t=([^&]+)/.exec(u.hash.replace(/^#/, ""))?.[1];
  if (!raw) return undefined;
  const v = parseTime(raw);
  return v !== undefined && v > 0 ? v : undefined;
}
