import { expect, test } from "@playwright/test";

// 미리보기 상호작용: 골든(정지 화면)이 못 잡는 동작을 실제 브라우저 이벤트로 확인한다.
// preview-ready: 19s, 실제 H.264 클립(10fps), 구간 7.2–12.5, 보기 전체(0–19s).
test("타임라인: Out 핸들 드래그, 클릭 탐색, I 키, 프레임 이동, 확대", async ({ page }) => {
  // 이벤트 처리기 안의 예외(예: this가 풀린 메서드 전달)는 화면에 안 보이고 pageerror로만 나온다.
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(String(e)));
  await page.goto("/fixture.html?scenario=preview-ready");
  await expect(page.locator("#app")).toHaveAttribute("data-ready", "1");
  const tl = page.locator(".timeline");
  const box = (await tl.boundingBox())!;
  const xAt = (sec: number) => box.x + (sec / 19) * box.width;
  const y = box.y + box.height / 2;
  const inField = page.locator(".inout input").nth(0);
  const outField = page.locator(".inout input").nth(1);
  const time = page.locator(".overlay.time");
  const sec = (s: string) => {
    const [m, rest] = s.split(":");
    return Number(m) * 60 + Number(rest);
  };

  // Out 핸들(12.5s)을 15s로 끈다.
  await page.mouse.move(xAt(12.5), y);
  await page.mouse.down();
  await page.mouse.move(xAt(14), y, { steps: 4 });
  await page.mouse.move(xAt(15), y, { steps: 4 });
  await page.mouse.up();
  // 입력란은 렌더 뒤 effect로 갱신되므로 기다려서 읽는다.
  await expect.poll(async () => Math.abs(sec(await outField.inputValue()) - 15)).toBeLessThan(0.15);
  await expect(inField).toHaveValue("0:07.2");
  await expect.poll(() => page.evaluate(() => (window as unknown as { __birdcage: { ctl: { state: { draft: { range: { end?: number } } } } } }).__birdcage.ctl.state.draft.range.end)).toBeCloseTo(15, 0);

  // 빈 곳 클릭 → 재생헤드 이동(3s), I 키 → In = 재생헤드.
  await page.mouse.click(xAt(3), y);
  await expect.poll(async () => Math.abs(sec((await time.textContent())!.split(" / ")[0]) - 3)).toBeLessThan(0.15);
  await page.keyboard.press("i");
  await expect.poll(async () => Math.abs(sec(await inField.inputValue()) - 3)).toBeLessThan(0.15);

  // → 한 프레임(10fps = 0.1s), Shift+→ 1초.
  const before = sec((await time.textContent())!.split(" / ")[0]);
  await page.keyboard.press("ArrowRight");
  await expect.poll(async () => sec((await time.textContent())!.split(" / ")[0]) - before).toBeCloseTo(0.1, 1);
  await page.keyboard.press("Shift+ArrowRight");
  await expect.poll(async () => sec((await time.textContent())!.split(" / ")[0]) - before).toBeCloseTo(1.1, 1);

  // Ctrl+휠로 확대하면 눈금 간격이 좁아진다(라벨 값 차이가 작아짐).
  const tickGap = async () => {
    const labels = (await page.locator(".ticks span").allTextContents()).map(sec);
    return labels[1] - labels[0];
  };
  const gap0 = await tickGap();
  await page.mouse.move(xAt(5), y);
  for (let i = 0; i < 6; i++) {
    await page.keyboard.down("Control");
    await page.mouse.wheel(0, -100);
    await page.keyboard.up("Control");
  }
  await expect.poll(tickGap).toBeLessThan(gap0);

  // 맞춤: 구간(3–15s)에 맞춰 보기가 바뀐다 → 첫 눈금이 0보다 뒤.
  await page.getByRole("button", { name: "맞춤" }).click();
  await expect.poll(async () => sec((await page.locator(".ticks span").first().textContent())!)).toBeGreaterThan(1);
  expect(errors).toEqual([]);
});

test("스크러버·Space·양끝 손잡이: 프로그레스 바처럼 끌기, 패널 어디서나 Space, 구간 없이 처음 손잡이 끌기", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(String(e)));
  const sec = (s: string) => {
    const [m, rest] = s.split(":");
    return Number(m) * 60 + Number(rest);
  };
  await page.goto("/fixture.html?scenario=preview-ready");
  await expect(page.locator("#app")).toHaveAttribute("data-ready", "1");
  const time = page.locator(".overlay.time");
  const now = async () => sec((await time.textContent())!.split(" / ")[0]);

  // 진행 막대(영상 전체 길이 19s)를 끌어 15s로.
  const bar = (await page.locator(".scrub").boundingBox())!;
  const xBar = (s: number) => bar.x + (s / 19) * bar.width;
  await page.mouse.move(xBar(5), bar.y + bar.height / 2);
  await page.mouse.down();
  await page.mouse.move(xBar(10), bar.y + bar.height / 2, { steps: 3 });
  await page.mouse.move(xBar(15), bar.y + 40, { steps: 3 }); // 막대 밖으로 벗어나도 계속 따라간다
  await page.mouse.up();
  await expect.poll(async () => Math.abs((await now()) - 15)).toBeLessThan(0.2);

  // Space: 포커스가 패널 어디에 있어도(본문) 재생/정지.
  await page.locator(".info-title").click();
  const before = await page.evaluate(() => document.querySelector("video")!.currentTime);
  await page.keyboard.press(" ");
  await expect.poll(() => page.evaluate(() => !document.querySelector("video")!.paused)).toBe(true);
  await page.waitForTimeout(600);
  await page.keyboard.press(" ");
  await expect.poll(() => page.evaluate(() => document.querySelector("video")!.paused)).toBe(true);
  expect(await page.evaluate(() => document.querySelector("video")!.currentTime)).toBeGreaterThan(before + 0.2);
  // 버튼을 누른 뒤에도 Space는 버튼을 다시 누르지 않고 재생/정지다.
  await page.getByRole("button", { name: "In", exact: true }).click();
  await expect.poll(async () => page.locator(".inout input").nth(0).inputValue()).not.toBe("0:07.2"); // In = 재생헤드
  const inAfterClick = await page.locator(".inout input").nth(0).inputValue();
  await page.keyboard.press(" ");
  await expect.poll(() => page.evaluate(() => !document.querySelector("video")!.paused)).toBe(true);
  await page.keyboard.press(" ");
  expect(await page.locator(".inout input").nth(0).inputValue()).toBe(inAfterClick);
  expect(errors).toEqual([]);
});

test("구간이 없을 때 처음 손잡이를 끌어 구간을 만든다", async ({ page }) => {
  await page.goto("/fixture.html?scenario=probed-warnings"); // 47s, 구간 없음
  await expect(page.locator("#app")).toHaveAttribute("data-ready", "1");
  const tl = (await page.locator(".timeline").boundingBox())!;
  const y = tl.y + tl.height / 2;
  await expect(page.locator(".handle.in")).toBeVisible();
  await expect(page.locator(".handle.out")).toBeVisible();
  await page.mouse.move(tl.x + 2, y);
  await page.mouse.down();
  await page.mouse.move(tl.x + (10 / 47) * tl.width, y, { steps: 4 });
  await page.mouse.up();
  await expect.poll(async () => page.locator(".inout input").nth(0).inputValue()).toMatch(/^0:(09\.[89]|10\.[0-2])$/);
  await expect(page.locator(".inout input").nth(1)).toHaveValue("0:47.0");
});
