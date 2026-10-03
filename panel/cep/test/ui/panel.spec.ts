import { expect, test } from "@playwright/test";
import { osKey, pinned } from "./environment";
import { scenarios } from "./scenarios";

// 구조(DOM) 골든은 어느 환경에서나 비교하고, 픽셀 골든은 고정 환경에서만 비교한다(W7).
// 픽스처는 실제 Controller를 가짜 Helper로 구동하고 값이 모두 고정이라 마스크가 없다.
for (const [name, scenario] of Object.entries(scenarios)) {
  test(`${name}: ${scenario.about}`, async ({ browser, page }) => {
    const errors: string[] = [];
    page.on("pageerror", (e) => errors.push(String(e)));
    await page.goto(`/fixture.html?scenario=${name}`);
    await expect(page.locator("#app")).toHaveAttribute("data-ready", "1");
    await page.evaluate(() => document.fonts.ready);
    expect(errors).toEqual([]);

    const structure = await page.locator("#app").evaluate((root) => {
      const attrs = ["class", "type", "role", "aria-checked", "aria-label", "title", "style", "placeholder"];
      const visit = (node: Node): unknown => {
        if (node.nodeType === Node.TEXT_NODE) {
          const text = node.textContent?.replace(/\s+/g, " ").trim();
          return text || null;
        }
        if (!(node instanceof HTMLElement)) return null;
        const a: Record<string, string | boolean> = {};
        for (const n of attrs) if (node.hasAttribute(n)) a[n] = node.getAttribute(n) || true;
        if (node instanceof HTMLButtonElement || node instanceof HTMLInputElement || node instanceof HTMLSelectElement) a.disabled = node.disabled;
        if (node instanceof HTMLInputElement && (node.type === "checkbox" || node.type === "radio")) a.checked = node.checked;
        if (node instanceof HTMLInputElement && node.type === "text") a.value = node.value;
        if (node instanceof HTMLSelectElement) a.value = node.value;
        if (node instanceof HTMLImageElement) a.src = node.src.startsWith("data:") ? "data:" + node.src.length : node.src;
        return { tag: node.tagName.toLowerCase(), attributes: a, children: Array.from(node.childNodes).map(visit).filter((c) => c !== null) };
      };
      return visit(root);
    });
    expect(JSON.stringify(structure, null, 2) + "\n").toMatchSnapshot(`${name}.dom.json`);

    const env = { chrome: browser.version().split(".")[0], os: osKey(), arch: process.arch };
    const same = env.chrome === pinned.chrome && env.os === pinned.os && env.arch === pinned.arch;
    test.info().annotations.push({ type: "environment", description: `${JSON.stringify(env)} pinned=${JSON.stringify(pinned)}` });
    if (!same) {
      test.info().annotations.push({ type: "pixel-skipped", description: "환경이 고정 환경과 달라 픽셀 골든은 건너뛴다(DOM 골든은 비교함)" });
      return;
    }
    await expect(page.locator(".app")).toHaveScreenshot(`${name}.png`);
  });
}
