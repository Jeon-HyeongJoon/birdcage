import { expect, test } from "@playwright/test";

test("집중 모드: CTA 옆 진행 막대, 중복 방지, 목록 분리, 다음 링크 병렬 준비", async ({ page }) => {
  await page.goto("/focus.fixture.html");
  await expect(page.locator("#app")).toHaveAttribute("data-ready", "1");

  await expect(page.getByRole("button", { name: "다운로드 목록 1" })).toBeVisible();
  await expect(page.locator(".download-actions")).not.toContainText("백그라운드에서 진행");
  await expect(page.locator(".inline-download-progress")).toContainText("받는 중 42%");
  await expect(page.locator(".inline-download-progress .bar > span")).toHaveAttribute("style", "width: 42%;");
  const download = page.getByRole("button", { name: "다운로드", exact: true });
  await expect(download).toBeDisabled();
  await expect(page.locator(".job-progress")).toHaveCount(0);
  await expect(page.locator("ul.jobs")).toHaveCount(0);
  await expect(page.locator("footer")).toHaveCount(0);

  const options = await page.locator(".options-box").boundingBox();
  const submit = await page.locator(".download-submit").boundingBox();
  expect(options).not.toBeNull();
  expect(submit).not.toBeNull();
  expect(submit!.y).toBeGreaterThan(options!.y);

  await page.evaluate(() => (window as unknown as { __birdcageFocus: { ctl: { setRange: (range: { start: number; end: number }) => void } } }).__birdcageFocus.ctl.setRange({ start: 8, end: 13 }));
  await expect(download).toBeEnabled();
  await download.click();
  await expect(download).toBeDisabled();
  await expect(page.locator(".inline-download-progress")).toContainText("다운로드 시작 중");
  await expect.poll(() => page.evaluate(() => (window as unknown as { __birdcageFocus: { ctl: { state: { draft: { jobIds?: string[] } } } } }).__birdcageFocus.ctl.state.draft.jobIds?.length)).toBe(2);

  await page.getByRole("button", { name: "다운로드 목록 1" }).click();
  expect(await page.evaluate(() => (window as unknown as { __birdcageFocus: { opened: () => number } }).__birdcageFocus.opened())).toBe(1);

  await page.locator("input.url").fill("https://www.youtube.com/watch?v=next");
  await expect.poll(() => page.evaluate(() => {
    const state = (window as unknown as { __birdcageFocus: { ctl: { state: { draft: { url: string }; jobs: unknown[] } } } }).__birdcageFocus.ctl.state;
    return { url: state.draft.url, jobs: state.jobs.length };
  })).toEqual({ url: "https://www.youtube.com/watch?v=next", jobs: 1 });
});
