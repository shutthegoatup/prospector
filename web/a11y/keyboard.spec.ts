import { expect, test } from "@playwright/test";

import { org } from "./world.ts";

// What axe can't see: where the keyboard goes, and what the tab is called.
test.use({ storageState: "a11y/.signed-in.json" });

test("the first Tab offers a way past the sidebar to the page", async ({ page }) => {
  await page.goto(`/o/${org.slug}/today`);
  await expect(page.getByRole("heading", { level: 1, name: /Today/ })).toBeVisible();
  await page.keyboard.press("Tab");
  const skip = page.getByRole("link", { name: "Skip to content" });
  await expect(skip).toBeFocused();
  await expect(skip).toBeInViewport();
  await page.keyboard.press("Enter");
  await expect(page.locator("main")).toBeFocused();
});

test("another page takes focus to its start, and names the tab", async ({ page }) => {
  await page.goto(`/o/${org.slug}/today`);
  await expect(page).toHaveTitle(`Today · ${org.name} · Prospector`);
  await page
    .getByRole("navigation", { name: "Main" })
    .getByRole("link", { name: /Companies/ })
    .click();
  await expect(page.locator("main")).toBeFocused();
  await expect(page).toHaveTitle(`Companies · ${org.name} · Prospector`);
  // And back to the first page, too.
  await page.getByRole("link", { name: "Skip to content" }).focus();
  await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: /Today/ }).click();
  await expect(page.locator("main")).toBeFocused();
  await expect(page).toHaveTitle(`Today · ${org.name} · Prospector`);
});

test("the bookmarklet's window is named for the page it adds from", async ({ page }) => {
  await page.goto(`/o/${org.slug}/add?popup=1`);
  await expect(page).toHaveTitle(`Add person · ${org.name} · Prospector`);
});

test("a filter on the same page leaves focus where it was", async ({ page }) => {
  await page.goto(`/o/${org.slug}/today`);
  const filter = page.getByRole("navigation", { name: "Filter by step" }).getByRole("link").nth(1);
  await filter.focus();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\?/);
  await expect(filter).toBeFocused();
});
