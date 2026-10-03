import { expect, test } from "@playwright/test";

test("완료 기록은 재시작 뒤 남고 메인에는 현재 다운로드 현황이 보인다", async ({ page }) => {
  await page.goto("/history.fixture.html?seed=1");
  await expect(page.locator("#fixture")).toHaveAttribute("data-ready", "1");

  const summary = page.locator("#main-panel .download-summary");
  await expect(summary).toContainText("Me at the zoo");
  await expect(summary).toContainText("받는 중 42%");
  await expect(summary).toContainText("진행 중 1");
  await expect(summary).toContainText("완료 기록 1");

  const downloads = page.locator("#downloads-panel");
  await expect(downloads.getByRole("heading", { name: "현재 다운로드" })).toBeVisible();
  await expect(downloads.getByRole("heading", { name: "완료 기록 1" })).toBeVisible();
  await expect(downloads.getByText("저장된 완료 영상")).toBeVisible();
  await expect(downloads.getByText("다운로드 완료")).toBeVisible();
  await expect(downloads.getByRole("button", { name: "가져오기" })).toHaveCount(0);
  expect(await page.evaluate(() => (window as unknown as { __birdcageHistory: { count: () => number } }).__birdcageHistory.count())).toBe(1);

  await page.reload();
  await expect(page.locator("#fixture")).toHaveAttribute("data-ready", "1");
  await expect(page.locator("#downloads-panel").getByText("저장된 완료 영상")).toBeVisible();
  expect(await page.evaluate(() => (window as unknown as { __birdcageHistory: { count: () => number } }).__birdcageHistory.count())).toBe(1);

  await page.locator("#downloads-panel").getByRole("button", { name: "완료 지우기" }).click();
  await expect(page.locator("#downloads-panel").getByRole("heading", { name: /완료 기록/ })).toHaveCount(0);
  expect(await page.evaluate(() => (window as unknown as { __birdcageHistory: { count: () => number } }).__birdcageHistory.count())).toBe(0);
});
