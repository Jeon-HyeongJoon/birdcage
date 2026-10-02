import { expect, test } from "@playwright/test";
import { chromeMajor } from "./environment";

const scenarioNames = ["workflow", "missing-tools", "connection-failed"];

for (const scenario of scenarioNames) {
  test(`${scenario}: DOM 구조와 픽셀 골든`, async ({ browser, page }) => {
    expect(browser.version()).toMatch(new RegExp(`^${chromeMajor}\\.`));

    await page.goto(`/fixture.html?scenario=${scenario}`);
    await expect(page.locator(".app")).toBeVisible();
    await page.evaluate(() => document.fonts.ready);

    const structure = await page.locator("#app").evaluate((root) => {
      const visit = (node: Node): unknown => {
        if (node.nodeType === Node.TEXT_NODE) {
          const text = node.textContent?.replace(/\s+/g, " ").trim();
          return text || null;
        }
        if (!(node instanceof HTMLElement)) return null;

        const attributes: Record<string, string | boolean> = {};
        for (const name of ["class", "type", "data-visual-dynamic"]) {
          if (node.hasAttribute(name)) attributes[name] = node.getAttribute(name) || true;
        }
        if (node instanceof HTMLButtonElement || node instanceof HTMLInputElement) {
          attributes.disabled = node.disabled;
        }
        if (node instanceof HTMLInputElement && node.type === "checkbox") attributes.checked = node.checked;
        if (node instanceof HTMLSelectElement) attributes.value = node.value;
        const children = node.hasAttribute("data-visual-dynamic")
          ? ["<dynamic>"]
          : Array.from(node.childNodes).map(visit).filter((child) => child !== null);
        return { tag: node.tagName.toLowerCase(), attributes, children };
      };
      return visit(root);
    });

    expect(JSON.stringify(structure, null, 2)).toMatchSnapshot(`${scenario}.dom.json`);
    await expect(page.locator(".app")).toHaveScreenshot(`${scenario}.png`, {
      mask: [page.locator("[data-visual-dynamic]")],
      maskColor: "#555555",
    });
  });
}
