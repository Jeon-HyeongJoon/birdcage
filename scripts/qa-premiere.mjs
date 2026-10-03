#!/usr/bin/env node
// Premiere 안의 Birdcage 패널을 원격 디버깅(CDP)으로 실제처럼 조작해 받기 흐름 전체를 시험한다.
// 링크 입력 → 미리보기 준비 → (선택) In/Out 입력 → 받기 → 받기·변환·넣기 완료까지 단계별 시간과 화면을 남긴다.
//
//   node scripts/qa-premiere.mjs <링크> [시작초 끝초]
//   예) node scripts/qa-premiere.mjs "https://www.youtube.com/watch?v=jNQXAC9IVRw" 7 12
//
// 필요: Premiere에서 Birdcage 패널이 열려 있을 것(디버그 포트는 panel/cep/dist/cep/.debug, 기본 8860).
// 주의: 완료되면 열린 시퀀스의 재생헤드 위치, 새 빈 트랙에 클립이 들어간다(프로젝트가 바뀐다).
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const [url, startArg, endArg] = process.argv.slice(2);
if (!url) {
  console.error("사용법: node scripts/qa-premiere.mjs <링크> [시작초 끝초]");
  process.exit(2);
}
const root = join(dirname(fileURLToPath(import.meta.url)), "..");
let port = 8860;
try {
  port = Number(/Port="(\d+)"/.exec(readFileSync(join(root, "panel/cep/dist/cep/.debug"), "utf8"))[1]);
} catch {
  /* 기본값 */
}

const targets = await (await fetch(`http://localhost:${port}/json`).catch(() => {
  console.error(`패널 디버그 포트(${port})에 연결할 수 없습니다. Premiere에서 Birdcage 패널을 여세요.`);
  process.exit(1);
})).json();
const page = targets.find((t) => t.title === "Birdcage") ?? targets[0];
const ws = new WebSocket(page.webSocketDebuggerUrl);
let seq = 0;
const pending = new Map();
const errors = [];
ws.onmessage = (m) => {
  const d = JSON.parse(m.data);
  if (d.id && pending.has(d.id)) {
    pending.get(d.id)(d);
    pending.delete(d.id);
  } else if (d.method === "Runtime.exceptionThrown") errors.push(d.params.exceptionDetails.exception?.description ?? d.params.exceptionDetails.text);
};
const send = (method, params = {}) =>
  new Promise((r) => {
    const id = ++seq;
    pending.set(id, r);
    ws.send(JSON.stringify({ id, method, params }));
  });
const evaluate = async (expression) => (await send("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true })).result?.result?.value;
await new Promise((r) => (ws.onopen = r));
await send("Runtime.enable");

const t0 = Date.now();
const since = () => ((Date.now() - t0) / 1000).toFixed(1) + "s";
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const until = async (label, expr, timeoutMs) => {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    const v = await evaluate(expr);
    if (v) {
      console.log(`  ${since().padStart(6)}  ${label}${typeof v === "string" ? ": " + v : ""}`);
      return v;
    }
    await sleep(250);
  }
  throw new Error(`시간 초과: ${label}`);
};
const setInput = (selector, value, blur) => `(() => {
  const el = document.querySelector(${JSON.stringify(selector)}); if (!el) return false;
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set.call(el, ${JSON.stringify(value)});
  el.dispatchEvent(new Event("input", { bubbles: true }));
  ${blur ? "el.dispatchEvent(new FocusEvent('blur')); el.dispatchEvent(new FocusEvent('focusout', { bubbles: true }));" : ""}
  return true; })()`;

console.log(`QA: ${url}${startArg ? ` 구간 ${startArg}–${endArg}s` : " 전체"}`);
try {
  await until("패널 사용 가능(초록)", `!!document.querySelector('.header .dot.ok')`, 30000);
  await evaluate(`(() => { const b = document.querySelector('button[title="지우기"]'); if (b) b.click(); return true; })()`);
  await evaluate(setInput("input.url", url));
  await until("링크 확인(정보·스토리보드)", `document.querySelector('.info-title')?.textContent`, 60000);
  await until("파형", `!document.querySelector('.wave-pending') && !!document.querySelector('.timeline canvas')`, 60000);
  await until("미리보기 재생 가능", `(() => { const v = document.querySelector('.frame video'); return !!v && v.readyState >= 2; })()`, 120000);
  if (startArg) {
    await evaluate(setInput(".inout input", String(startArg), true));
    await sleep(100);
    await evaluate(`(() => { const el = document.querySelectorAll('.inout input')[1]; Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set.call(el, ${JSON.stringify(String(endArg))}); el.dispatchEvent(new Event("input", { bubbles: true })); el.dispatchEvent(new FocusEvent('focusout', { bubbles: true })); return true; })()`);
    await until("In/Out 입력", `Array.from(document.querySelectorAll('.inout input')).map(i => i.value).join(' – ')`, 5000);
  }
  await evaluate(`document.querySelector('form.composer button[type=submit]').click()`);
  console.log(`  ${since().padStart(6)}  받기 누름`);
  let last = "";
  const start = Date.now();
  while (Date.now() - start < 10 * 60000) {
    const s = await evaluate(`(() => { const p = document.querySelector('.job-progress'); if (!p) return null;
      return { tone: p.className, line: p.querySelector('.status-line')?.innerText.replace(/\\s+/g, ' ').trim(), steps: p.querySelector('.steps')?.innerText.replace(/\\s+/g, ' ') }; })()`);
    if (s) {
      const text = `${s.steps} | ${s.line}`;
      if (text !== last) {
        console.log(`  ${since().padStart(6)}  ${text}`);
        last = text;
      }
      if (/\b(done|bad)\b/.test(s.tone)) break;
    }
    await sleep(300);
  }
} catch (e) {
  console.log(`  ${since().padStart(6)}  실패: ${e.message}`);
  process.exitCode = 1;
} finally {
  const shot = await send("Page.captureScreenshot", { format: "png" });
  const out = join(root, "loop/.runs", `qa-premiere-${new Date().toISOString().replace(/[:.]/g, "-")}.png`);
  try {
    writeFileSync(out, Buffer.from(shot.result.data, "base64"));
    console.log(`화면: ${out}`);
  } catch {
    /* loop/.runs 없음 */
  }
  if (errors.length) console.log("패널 예외:\n  " + errors.join("\n  "));
  ws.close();
}
