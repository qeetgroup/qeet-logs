import { expect, test } from "@playwright/test";

import { mockApi, seedKey } from "./fixtures";

// The console's production error strategy: a failing API call surfaces a
// contained error state (role="alert") while the surrounding shell survives —
// a failure never collapses to a blank screen. Unmatched routes fall through to
// the root not-found boundary.

test.describe("error boundaries", () => {
  test.beforeEach(async ({ page }) => {
    await seedKey(page);
  });

  test("shows an error state when an API call fails, keeping the shell", async ({ page }) => {
    // Settings loads its config from GET /v1/admin/retention — fail it.
    await mockApi(page, {
      "/v1/admin/retention": { status: 500, json: { error: "retention backend unavailable" } },
    });

    await page.goto("/settings");

    // The failure is surfaced as a role="alert" region with the error message...
    const alert = page.getByRole("alert");
    await expect(alert).toBeVisible();
    await expect(alert).toContainText("retention backend unavailable");

    // ...but the shell and page header remain (no blank screen).
    await expect(page.getByRole("navigation", { name: "Primary" })).toBeVisible();
    await expect(page.getByRole("heading", { level: 1, name: "Settings" })).toBeVisible();
  });

  test("renders the not-found boundary for an unknown route", async ({ page }) => {
    await mockApi(page);

    await page.goto("/this-route-does-not-exist");

    await expect(page.getByRole("heading", { name: "Page not found" })).toBeVisible();
    await expect(page.getByRole("link", { name: /back to overview/i })).toBeVisible();
  });
});
