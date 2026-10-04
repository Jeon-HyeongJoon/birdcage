import { expect, test } from "@playwright/test";
import { osKey, pinned } from "./environment";

// 탐색 목록(T-036): 넓은 패널은 오른쪽 사이드바(미리보기는 왼쪽에 남는다), 항목 → 미리보기 전환 → [←]로 복원.
type Win = { __birdcage: { ctl: { state: { draft: { url: string; status: string; probeId?: string; range: unknown; formatId?: string }; history: unknown[] }; setRange: (r: unknown) => void; selectVariant: (id: string) => void } } };

test("넓은 패널: 오른쪽 사이드바, 목록만 따로 스크롤", async ({ browser, page }) => {
  await page.setViewportSize({ width: 1280, height: 720 });
  await page.goto("/fixture.html?scenario=related-loaded");
  await expect(page.locator("#app")).toHaveAttribute("data-ready", "1");
  await page.evaluate(() => document.fonts.ready);

  const main = (await page.locator(".composer").boundingBox())!;
  const aside = (await page.locator("aside.related").boundingBox())!;
  expect(aside.width).toBe(340);
  expect(aside.x).toBeGreaterThan(main.x + main.width); // 미리보기 오른쪽
  expect(aside.y).toBeLessThan(main.y + 40); // 위쪽 정렬
  expect(main.width).toBeGreaterThan(880);
  await expect(page.locator(".related-item")).toHaveCount(5);
  await expect(page.locator(".related-item").nth(2)).toContainText("✓ 받음");
  await expect(page.locator(".related-item .dur").first()).toHaveText("12:34");
  await expect(page.locator(".related-item .dur").nth(3)).toHaveText("1:02:05");

  const same = browser.version().split(".")[0] === pinned.chrome && osKey() === pinned.os && process.arch === pinned.arch;
  if (same) await expect(page.locator(".app")).toHaveScreenshot("related-wide.png");
});

test("좁은 패널: 목록은 받기 아래, 보일 때 받는다", async ({ page }) => {
  await page.setViewportSize({ width: 420, height: 400 }); // 목록이 첫 화면 밖에 있게
  await page.goto("/fixture.html?scenario=probed");
  await expect(page.locator("#app")).toHaveAttribute("data-ready", "1");
  const composer = (await page.locator(".composer").boundingBox())!;
  const aside = (await page.locator("aside.related").boundingBox())!;
  expect(aside.y).toBeGreaterThanOrEqual(composer.y + composer.height);
  // 미리보기가 받는 중이고 목록이 화면 밖이면 아직 요청하지 않는다(미리보기와 대역폭을 다투지 않게).
  await expect(page.locator("aside.related .skeleton")).toHaveCount(1);
  expect(await page.evaluate(() => (window as unknown as { __birdcage: { ctl: { state: { draft: { related?: unknown } } } } }).__birdcage.ctl.state.draft.related)).toBeUndefined();
  await page.locator("aside.related").scrollIntoViewIfNeeded();
  await expect(page.locator(".related-item")).toHaveCount(5);
});

test("항목 열기 → 조회 중 썸네일·제목 → [←]로 구간·화질 복원", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 720 });
  await page.goto("/fixture.html?scenario=related-loaded");
  await expect(page.locator("#app")).toHaveAttribute("data-ready", "1");
  const state = () => page.evaluate(() => {
    const s = (window as unknown as Win).__birdcage.ctl.state;
    return { url: s.draft.url, status: s.draft.status, probeId: s.draft.probeId, range: s.draft.range, formatId: s.draft.formatId, history: s.history.length };
  });
  await page.evaluate(() => {
    const c = (window as unknown as Win).__birdcage.ctl;
    c.setRange({ on: true, start: 3, end: 8 });
    c.selectVariant("136+140");
  });
  await expect(page.getByRole("button", { name: "←" })).toHaveCount(0);

  await page.locator(".related-item").nth(1).click();
  await expect.poll(async () => (await state()).url).toBe("https://www.youtube.com/watch?v=rel2");
  await expect(page.locator(".info-title")).toHaveText("Me at the zoo"); // 픽스처의 조회 결과(같은 정보)
  await expect(page.locator(".frame video")).toHaveCount(1);
  const back = page.locator("button.back");
  await expect(back).toHaveAttribute("title", "이전 영상: Me at the zoo");

  await back.click();
  await expect.poll(state).toEqual({ url: "https://www.youtube.com/watch?v=jNQXAC9IVRw", status: "ready", probeId: "p8", range: { on: true, start: 3, end: 8 }, formatId: "136+140", history: 0 });
  await expect(page.locator(".inout input").first()).toHaveValue("0:03.0");
  await expect(page.locator(".related-item")).toHaveCount(5); // 목록도 그대로(다시 받지 않음)
  await expect(back).toHaveCount(0);
});

test("탭: 같은 채널·비슷한 제목, 실패하면 다시 시도", async ({ page }) => {
  await page.goto("/fixture.html?scenario=related-loaded");
  await expect(page.locator("#app")).toHaveAttribute("data-ready", "1");
  await page.getByRole("tab", { name: "같은 채널" }).click();
  await expect(page.getByRole("tab", { name: "같은 채널" })).toHaveAttribute("aria-selected", "true");
  await expect(page.locator(".related-item")).toHaveCount(1);
  await page.getByRole("tab", { name: "비슷한 제목" }).click();
  await expect(page.locator(".related-item")).toHaveCount(2);
  await page.getByRole("tab", { name: "추천" }).click();
  await expect(page.locator(".related-item")).toHaveCount(5);

  await page.goto("/fixture.html?scenario=related-failed");
  await expect(page.locator("#app")).toHaveAttribute("data-ready", "1");
  await expect(page.locator(".related-note")).toContainText("목록을 가져오지 못했습니다");
  await expect(page.getByRole("button", { name: "다시 시도" })).toBeVisible();
});
