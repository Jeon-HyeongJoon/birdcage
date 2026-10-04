import { expect, test } from "@playwright/test";
import { osKey, pinned } from "./environment";

// 유튜브 검색(T-026): 상단 검색창 → 목록의 '검색' 탭 → 결과 열기 → 미리보기 → [←] 뒤에도 결과 유지.
type Win = { __birdcage: { ctl: { state: { draft: { url: string; status: string; via?: string }; listTab: string; search?: { query: string; items: unknown[]; status: string } } } } };
const state = (page: import("@playwright/test").Page) =>
  page.evaluate(() => {
    const s = (window as unknown as Win).__birdcage.ctl.state;
    return { url: s.draft.url, status: s.draft.status, via: s.draft.via, tab: s.listTab, query: s.search?.query, items: s.search?.items.length };
  });

test("넓은 패널: 미리보기 없이 검색하면 결과가 본문 폭(여러 열), 열면 사이드바로", async ({ browser, page }) => {
  await page.setViewportSize({ width: 1280, height: 720 });
  await page.goto("/fixture.html?scenario=empty");
  await expect(page.locator("#app")).toHaveAttribute("data-ready", "1");
  await page.locator("input.search").fill("인터뷰 b-roll");
  await page.locator("input.search").press("Enter");
  await expect(page.locator(".related-item")).toHaveCount(6);
  await expect(page.locator("aside.related")).toHaveClass(/full/);
  const rows = await page.locator(".related-list > li").evaluateAll((els) => els.map((e) => Math.round(e.getBoundingClientRect().top)));
  expect(new Set(rows).size).toBeLessThan(rows.length); // 같은 줄에 여러 개(여러 열)
  await expect(page.getByRole("tab", { name: "검색" })).toHaveAttribute("aria-selected", "true");
  await expect(page.getByRole("tab", { name: "추천" })).toHaveCount(0); // 열린 영상이 없으면 추천 탭은 없다

  await page.getByRole("button", { name: "더 보기" }).click();
  await expect(page.locator(".related-item")).toHaveCount(8);
  await expect(page.getByRole("button", { name: "더 보기" })).toHaveCount(0);

  const same = browser.version().split(".")[0] === pinned.chrome && osKey() === pinned.os && process.arch === pinned.arch;
  await page.mouse.move(0, 0); // 호버 배경 없이
  if (same) await expect(page.locator(".app")).toHaveScreenshot("search-wide.png");

  await page.locator(".related-item").nth(1).click();
  await expect.poll(() => state(page)).toMatchObject({ url: "https://www.youtube.com/watch?v=sr2", via: "search:youtube", tab: "search", items: 8 });
});

test("결과 열기 → 미리보기 → 추천 탭 전환 → [←] 뒤에도 검색 결과 유지", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 720 });
  await page.goto("/fixture.html?scenario=search-preview");
  await expect(page.locator("#app")).toHaveAttribute("data-ready", "1");
  await expect(page.locator(".frame video")).toHaveCount(1);
  await expect(page.locator("aside.related")).not.toHaveClass(/full/); // 미리보기가 있으면 사이드바
  await expect(page.getByRole("tab", { name: "검색" })).toHaveAttribute("aria-selected", "true");
  await page.getByRole("tab", { name: "추천" }).click();
  await expect(page.getByRole("tab", { name: "추천" })).toHaveAttribute("aria-selected", "true");
  await expect(page.locator(".related-item")).toHaveCount(5); // 이 영상의 추천 목록
  await page.getByRole("tab", { name: "검색" }).click();
  await expect(page.locator(".related-item")).toHaveCount(6);

  await page.locator(".related-item").first().click();
  await expect.poll(async () => (await state(page)).url).toBe("https://www.youtube.com/watch?v=sr1");
  await page.locator("button.back").click();
  await expect.poll(() => state(page)).toMatchObject({ url: "https://www.youtube.com/watch?v=sr6", tab: "search", query: "인터뷰 b-roll", items: 6 });
});

test("링크 칸에 글자 + Enter도 검색, 검색창에 검색어가 보인다", async ({ page }) => {
  await page.goto("/fixture.html?scenario=empty");
  await expect(page.locator("#app")).toHaveAttribute("data-ready", "1");
  await page.locator("input.url").fill("코끼리 브이로그");
  await page.locator("input.url").press("Enter");
  await expect.poll(() => state(page)).toMatchObject({ url: "", tab: "search", query: "코끼리 브이로그" });
  await expect(page.locator("input.search")).toHaveValue("코끼리 브이로그");
  await expect(page.locator(".related-item")).toHaveCount(6);
  // 링크는 그대로 링크로 확인한다(검색하지 않음).
  await page.locator("input.url").fill("https://www.youtube.com/watch?v=jNQXAC9IVRw");
  await page.locator("input.url").press("Enter");
  await expect.poll(async () => (await state(page)).query).toBe("코끼리 브이로그");
});
