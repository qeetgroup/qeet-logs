import { expect, test } from "@playwright/test";

import { mockApi, seedKey } from "./fixtures";

// Client-side navigation via the sidebar keeps the layout mounted and swaps the
// content region. Each target asserts both the URL and the page's <h1>.

test.describe("navigation", () => {
  test.beforeEach(async ({ page }) => {
    await seedKey(page);
    await mockApi(page);
    await page.goto("/");
    await expect(page.getByRole("navigation", { name: "Primary" })).toBeVisible();
  });

  const targets = [
    { link: "Log Search", path: /\/search$/, heading: "Log Search" },
    { link: "Incidents", path: /\/incidents$/, heading: "Incidents" },
    { link: "Settings", path: /\/settings$/, heading: "Settings" },
  ] as const;

  for (const target of targets) {
    test(`navigates to ${target.link}`, async ({ page }) => {
      const nav = page.getByRole("navigation", { name: "Primary" });
      await nav.getByRole("link", { name: target.link, exact: true }).click();

      await expect(page).toHaveURL(target.path);
      await expect(page.getByRole("heading", { level: 1, name: target.heading })).toBeVisible();
    });
  }
});
