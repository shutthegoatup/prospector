import { AxeBuilder } from "@axe-core/playwright";
import { expect, test, type Page, type Route } from "@playwright/test";
import { readFileSync } from "node:fs";

import { serve, served } from "./served.ts";
import { org } from "./world.ts";

// WCAG 2.2 A and AA, and axe's best practices (landmarks, headings, unique regions).
const rules = ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22a", "wcag22aa", "best-practice"];

async function accessible(tab: Page) {
  // Colours are read as drawn, so wait for whatever is fading in; a pulse never ends.
  await tab.evaluate(() =>
    Promise.all(
      document
        .getAnimations()
        .filter((a) => a.effect?.getTiming().iterations !== Infinity)
        .map((a) => a.finished),
    ),
  );
  const { violations } = await new AxeBuilder({ page: tab }).withTags(rules).analyze();
  await test.info().attach("violations", { body: JSON.stringify(violations, null, 2), contentType: "application/json" });
  expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`)).toEqual([]);
}

const at = (path: string) => `/o/${org.slug}/${path}`;

// The first of a list setup.ts filled, to open its own page.
async function first(tab: Page, list: string) {
  const { items } = (await (await tab.request.get(at(`api/v1/${list}`))).json()) as { items: { id: number }[] };
  expect(items.length, `${list} is empty`).toBeGreaterThan(0);
  return items[0]!.id;
}

// Every page inside an organisation: those the server lists in appPages, read from where
// it lists them so a new one is checked without being added here, and one of each kind
// of page about a single thing.
const listed = [...readFileSync(new URL("../../app_ui.go", import.meta.url), "utf8").matchAll(/^var appPages = \[\]string\{(.*)\}$/gm)]
  .flatMap((m) => (m[1] ?? "").split(", "))
  .map((p) => JSON.parse(p) as string);
const pages: { name: string; path: (tab: Page) => Promise<string> }[] = [
  ...listed.map((p) => ({ name: at(p), path: async () => at(p) })),
  { name: "a company", path: async (tab: Page) => at(`companies/${await first(tab, "companies")}`) },
  { name: "a task", path: async (tab: Page) => at(`tasks/${await first(tab, "tasks")}`) },
  { name: "an agreement's preparation", path: async (tab: Page) => at(`agreements/${await first(tab, "agreements")}/prepare`) },
];

const nav = (route: Route) => route.request().url().endsWith("/api/v1/nav");
const throttled = (route: Route) =>
  route.fulfill({ status: 429, contentType: "application/problem+json", json: { type: "/api/problems/throttled", title: "Too Many Requests", status: 429 } });

test("the server's pages are found", () => {
  expect(listed).toContain("today");
});

test.describe("signed in", () => {
  test.use({ storageState: "a11y/.signed-in.json" });

  for (const { name, path } of pages) {
    test(name, async ({ page: tab }) => {
      await tab.goto(await path(tab));
      await tab.waitForLoadState("networkidle");
      await accessible(tab);
    });

    // While its data loads: placeholders, said to be loading, and still for those who
    // asked for less motion.
    test(`${name} while it loads`, async ({ page: tab }) => {
      const address = await path(tab);
      await tab.emulateMedia({ reducedMotion: "reduce" });
      await tab.route(`**${at("api/v1/")}**`, (route) => (nav(route) ? route.fallback() : undefined));
      await tab.goto(address);
      await expect(tab.getByRole("status")).toHaveText("Loading…");
      await expect(tab.locator("main [aria-busy=true]").first()).toBeAttached();
      await accessible(tab);
      expect(await tab.evaluate(() => document.getAnimations().length)).toBe(0);
    });

    test(`${name} when it can't load`, async ({ page: tab }) => {
      const address = await path(tab);
      await tab.route(`**${at("api/v1/")}**`, (route) => (nav(route) ? route.fallback() : throttled(route)));
      await tab.goto(address);
      await expect(tab.getByRole("alert").first()).toBeVisible();
      await expect(tab.getByRole("status")).toHaveText("");
      await accessible(tab);
    });
  }

  test("placeholders pulse for those who don't mind motion", async ({ page: tab }) => {
    await tab.route(`**${at("api/v1/")}**`, (route) => (nav(route) ? route.fallback() : undefined));
    await tab.goto(at("companies"));
    await expect(tab.locator("main [aria-busy=true]").first()).toBeAttached();
    expect(await tab.evaluate(() => document.getAnimations().length)).toBeGreaterThan(0);
  });

  test("an organisation that can't be read", async ({ page: tab }) => {
    await tab.route(`**${at("api/v1/nav")}`, throttled);
    await tab.goto(at("today"));
    await expect(tab.getByRole("alert")).toBeVisible();
    await accessible(tab);
  });

  test("an organisation that isn't there", async ({ page: tab }) => {
    await tab.goto("/o/nowhere/today");
    await accessible(tab);
  });
});

test.describe("signed out", () => {
  for (const [name, path] of [
    ["the front page", "/"],
    ["sign-in", "/login"],
    ["a forgotten password", "/forgot"],
    ["single sign-on", "/login/sso"],
    ["a reset link that has gone", "/reset/not-a-token"],
    ["a customer without a link", "/c"],
    ["an unsubscribe link that has gone", "/u/not-a-token"],
  ] as const) {
    test(name, async ({ page: tab }) => {
      await tab.goto(path);
      await tab.waitForLoadState("networkidle");
      await accessible(tab);
    });
  }

  test("the API reference", async ({ page: tab }) => {
    // The longest page by far, and axe reads every node of it.
    test.slow();
    await tab.goto("/api");
    await accessible(tab);
  });

  // The pages the server decides, in the shell it sends them in.
  for (const [name, payload] of served) {
    test(name, async ({ page: tab }) => {
      await serve(tab, payload);
      if (payload.page === "customer" && payload.state === "steps") await expect(tab.getByText("Kick-off call")).toBeVisible();
      await accessible(tab);
    });
  }
});
