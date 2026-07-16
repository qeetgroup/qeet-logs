import { expect, test } from "@playwright/test";

import { mockApi, signInWith } from "./fixtures";

// The sign-in screen is the only public route. It validates a pasted API key
// against GET /v1/admin/api-keys before storing it and landing on the overview.

test.describe("sign-in", () => {
  test.beforeEach(async ({ page }) => {
    // Fresh context => empty localStorage => the page does not auto-redirect.
    await mockApi(page);
  });

  test("renders the sign-in form", async ({ page }) => {
    await page.goto("/sign-in");

    // The card title is a styled <div>, not a heading — assert by text.
    await expect(page.getByText("Sign in", { exact: true })).toBeVisible();
    await expect(page.getByLabel("API key")).toBeVisible();
    await expect(page.getByRole("button", { name: "Continue" })).toBeVisible();
  });

  test("rejects an empty key (submit stays disabled)", async ({ page }) => {
    await page.goto("/sign-in");

    const submit = page.getByRole("button", { name: "Continue" });
    await expect(submit).toBeDisabled();

    // Whitespace-only is also treated as empty.
    await page.getByLabel("API key").fill("   ");
    await expect(submit).toBeDisabled();
  });

  test("rejects an invalid key with an inline error", async ({ page }) => {
    // The validation probe fails => the key is rejected, no navigation.
    await mockApi(page, {
      "/v1/admin/api-keys": { status: 401, json: { error: "invalid key" } },
    });

    await page.goto("/sign-in");
    await signInWith(page, "qlog_live_bogus");

    await expect(page.getByText(/api key was rejected/i)).toBeVisible();
    await expect(page).toHaveURL(/\/sign-in$/);
  });

  test("accepts a valid key and lands on the app shell", async ({ page }) => {
    // Default mock => GET /v1/admin/api-keys returns 200 => key accepted.
    await page.goto("/sign-in");
    await signInWith(page, "qlog_live_valid");

    // Redirected to the overview inside the authenticated layout.
    await expect(page).toHaveURL(/\/$/);
    await expect(page.getByRole("navigation", { name: "Primary" })).toBeVisible();
  });
});
