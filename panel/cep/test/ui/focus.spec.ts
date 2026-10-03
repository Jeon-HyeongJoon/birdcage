import { expect, test } from "@playwright/test";

test("집중 모드: 옵션 옆 받기, 작업 목록 분리, 다음 링크 병렬 준비", async ({ page }) => {
  await page.goto("/focus.fixture.html");
  await expect(page.locator("#app")).toHaveAttribute("data-ready", "1");

  await expect(page.getByRole("button", { name: "다운로드 1" })).toBeVisible();
  await expect(page.locator(".download-actions")).toContainText("백그라운드에서 진행");
  await expect(page.getByRole("button", { name: "구간 받기", exact: true })).toBeVisible();
  await expect(page.locator(".job-progress")).toHaveCount(0);
  await expect(page.locator("ul.jobs")).toHaveCount(0);
  await expect(page.locator("footer")).toHaveCount(0);

  const options = await page.locator(".options-box").boundingBox();
  const submit = await page.locator(".download-submit").boundingBox();
  expect(options).not.toBeNull();
  expect(submit).not.toBeNull();
  expect(submit!.y).toBeGreaterThan(options!.y);

  await page.getByRole("button", { name: "다운로드 1" }).click();
  expect(await page.evaluate(() => (window as unknown as { __birdcageFocus: { opened: () => number } }).__birdcageFocus.opened())).toBe(1);

  await page.locator("input.url").fill("https://www.youtube.com/watch?v=next");
  await expect.poll(() => page.evaluate(() => {
    const state = (window as unknown as { __birdcageFocus: { ctl: { state: { draft: { url: string }; jobs: unknown[] } } } }).__birdcageFocus.ctl.state;
    return { url: state.draft.url, jobs: state.jobs.length };
  })).toEqual({ url: "https://www.youtube.com/watch?v=next", jobs: 1 });
});
