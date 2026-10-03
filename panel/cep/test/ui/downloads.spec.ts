import { expect, test } from "@playwright/test";

test("다운로드 패널: 진행·완료 작업과 관리 명령", async ({ page }) => {
  await page.goto("/downloads.fixture.html");
  await expect(page.locator("#app")).toHaveAttribute("data-ready", "1");
  await expect(page.getByText("현재 받는 영상")).toBeVisible();
  await expect(page.getByText("받는 중 42%")).toBeVisible();
  await expect(page.getByText("완료된 영상")).toBeVisible();
  await expect(page.getByText("가져오기 실패: 열린 시퀀스가 없습니다")).toBeVisible();

  await page.getByRole("button", { name: "취소" }).click();
  await page.getByRole("button", { name: "다시 넣기" }).click();
  await page.getByRole("button", { name: "폴더에서 보기" }).click();
  await page.getByRole("button", { name: "완료 지우기" }).click();
  expect(await page.evaluate(() => (window as unknown as { __birdcageDownloads: { actions: unknown[] } }).__birdcageDownloads.actions)).toEqual([
    { type: "cancel", jobId: "active" },
    { type: "import", jobId: "done" },
    { type: "reveal", jobId: "done" },
    { type: "clearFinished" },
  ]);
});
