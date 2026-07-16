import { expect, test } from "@playwright/test";

import { mockApi, seedKey } from "./fixtures";

// With a seeded key the authenticated `_app` layout renders: sidebar (with the
// primary navigation + brand) and the header (account menu).

test.describe("app shell", () => {
  test.beforeEach(async ({ page }) => {
    await seedKey(page);
    await mockApi(page);
  });

  test("renders the sidebar, header and overview", async ({ page }) => {
    await page.goto("/");

    // Sidebar: primary nav landmark + brand + representative links.
    const nav = page.getByRole("navigation", { name: "Primary" });
    await expect(nav).toBeVisible();
    await expect(nav.getByRole("link", { name: "Log Search", exact: true })).toBeVisible();
    await expect(nav.getByRole("link", { name: "Incidents", exact: true })).toBeVisible();
    await expect(nav.getByRole("link", { name: "Settings", exact: true })).toBeVisible();

    // Header: account menu button.
    await expect(page.getByRole("button", { name: "Account menu" })).toBeVisible();

    // Content: overview page header.
    await expect(page.getByRole("heading", { level: 1, name: "Overview" })).toBeVisible();
  });
});
