#!/usr/bin/env node
// 탐색 목록(T-036) 적합성 집계: Helper 성능 로그에서 출처별로 보여 준 목록·연 영상·받은 영상을 센다.
// 자동 기준(커버리지·지연·대체)과 함께 사람 판단(L5)의 근거로 쓴다(docs/decisions/0012).
//
//   node scripts/related-report.mjs [로그 파일 또는 폴더 ...]   # 기본: Helper 데이터 폴더 logs/perf-*.jsonl
//   node scripts/related-report.mjs --since 2026-10-04
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { homedir, platform } from "node:os";
import { join } from "node:path";

const args = process.argv.slice(2);
let since = "";
const paths = [];
for (let i = 0; i < args.length; i++) {
  if (args[i] === "--since") since = args[++i] ?? "";
  else paths.push(args[i]);
}
if (paths.length === 0) {
  const data = platform() === "win32" ? join(process.env.LOCALAPPDATA ?? join(homedir(), "AppData", "Local"), "Birdcage") : join(homedir(), "Library", "Application Support", "Birdcage");
  paths.push(join(data, "logs"));
}
const files = paths.flatMap((p) => (existsSync(p) && statSync(p).isDirectory() ? readdirSync(p).filter((f) => /^perf-.*\.jsonl$/.test(f)).map((f) => join(p, f)) : [p]));

const sources = ["youtube", "channel", "search"];
const row = () => ({ lists: 0, cached: 0, failed: 0, items: 0, ms: [], opened: 0, downloads: 0, ranged: 0 });
const by = Object.fromEntries(sources.map((s) => [s, row()]));
let first = "", last = "";
for (const f of files) {
  for (const line of readFileSync(f, "utf8").split("\n")) {
    if (!line.includes('"related.')) continue;
    let r;
    try {
      r = JSON.parse(line);
    } catch {
      continue;
    }
    if (r.kind !== "span" || (since && r.ts < since)) continue;
    first = !first || r.ts < first ? r.ts : first;
    last = r.ts > last ? r.ts : last;
    const a = r.attrs ?? {};
    if (r.name === "related.list") {
      // 출처가 직접 채운 목록만 센다. 요청 출처가 실패해 다른 출처로 넘어간 구간은 그 출처의 실패로 남는다.
      const req = by[a.requested];
      if (!req || a.source !== a.requested) continue;
      if (r.err || !a.items) req.failed++;
      else {
        req.lists++;
        req.items += a.items;
        if (a.cached) req.cached++;
        else req.ms.push(r.dur_ms);
      }
    } else if (r.name === "related.open" || r.name === "related.download") {
      const src = String(a.via ?? "").replace(/^related:/, "");
      if (!by[src]) continue;
      if (r.name === "related.open") by[src].opened++;
      else {
        by[src].downloads++;
        if (a.range) by[src].ranged++;
      }
    }
  }
}
const median = (xs) => {
  if (!xs.length) return "-";
  const v = xs.slice().sort((a, b) => a - b);
  const m = v.length >> 1;
  return ((v.length % 2 ? v[m] : (v[m - 1] + v[m]) / 2) / 1000).toFixed(2) + "s";
};
const pct = (a, b) => (b ? `${Math.round((a / b) * 100)}%` : "-");

console.log(`기간: ${first || "-"} ~ ${last || "-"} (${files.length}개 로그)`);
console.log("출처        목록  실패  항목(평균)  지연(중앙)  연 영상  받은 영상(구간)  열기/목록  받기/열기");
for (const s of sources) {
  const b = by[s];
  const shown = b.lists;
  console.log(
    [
      s.padEnd(10),
      String(shown).padStart(4),
      String(b.failed).padStart(5),
      (shown ? (b.items / shown).toFixed(1) : "-").padStart(10),
      median(b.ms).padStart(10),
      String(b.opened).padStart(7),
      `${b.downloads}(${b.ranged})`.padStart(14),
      pct(b.opened, shown).padStart(9),
      pct(b.downloads, b.opened).padStart(9),
    ].join("  "),
  );
}
