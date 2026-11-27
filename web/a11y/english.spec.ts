import { expect, test, type Page, type Route } from "@playwright/test";
import { readFileSync } from "node:fs";

import { serve, served, servedWords } from "./served.ts";
import { base, member, org } from "./world.ts";

// In en-XA every word the app says is accented and bracketed, so plain letters left on a
// page are words that never went through react-intl. A page is checked once its file is
// off .oxlintrc.json's list of those still to be translated.
const untranslated = new Set<string>(
  (JSON.parse(readFileSync(new URL("../.oxlintrc.json", import.meta.url), "utf8")) as { overrides: { files: string[] }[] }).overrides.flatMap((o) => o.files),
);
const translated = (file: string) => !untranslated.has(file);

// Dates come from Intl, which has no data of its own for a pseudo-locale, so en-XA's
// months are English: they say nothing about what react-intl was given.
const months = Array.from({ length: 12 }, (_, i) =>
  new Intl.DateTimeFormat("en-XA", { month: "short", timeZone: "UTC" }).format(new Date(Date.UTC(2026, i, 1))),
);

// What a page may show as it is: the data the test put there, the name, and Intl's months.
const allowed = ["Prospector", member.name, member.email, org.name, ...servedWords, ...months];

// said is everything a person sees or hears inside the first element matching scope:
// text, screen-reader-only text included and hidden text not, the attributes read out
// or shown, and the tab's title when the whole page is checked.
async function said(tab: Page, scope: string): Promise<string[]> {
  return tab.evaluate((within) => {
    const root = document.querySelector(within);
    if (!root) throw new Error(`nothing matches ${within}`);
    const out: string[] = within === "body" ? [document.title] : [];
    // oxlint-disable-next-line unicorn/consistent-function-scoping -- it runs in the page, so it can't be hoisted out of it
    const shown = (el: Element) => !el.closest("[aria-hidden=true]") && el.checkVisibility({ visibilityProperty: true });
    const walk = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    for (let n = walk.nextNode(); n; n = walk.nextNode()) {
      const el = n.parentElement;
      if (el && !el.closest("script, style") && (el.closest("option") || shown(el)) && n.textContent?.trim()) out.push(n.textContent.trim());
    }
    for (const el of [root, ...root.querySelectorAll("*")]) {
      if (!shown(el)) continue;
      for (const a of ["aria-label", "title", "placeholder", "alt"]) {
        const v = el.getAttribute(a)?.trim();
        if (v) out.push(v);
      }
    }
    return out;
  }, scope);
}

// english are the strings in which plain letters are left once the allowed words are taken out.
function english(strings: string[]) {
  return strings.filter((s) => /[A-Za-z]{2,}/.test(allowed.reduce((left, w) => left.replaceAll(w, ""), s)));
}

async function inPseudo(tab: Page, scope: string) {
  const all = await said(tab, scope);
  expect(
    all.some((s) => s.includes("[")),
    `nothing in en-XA was seen in ${scope}: ${JSON.stringify(all)}`,
  ).toBe(true);
  expect(english(all), `left in English in ${scope}`).toEqual([]);
}

test.beforeEach(async ({ context }) => {
  await context.addCookies([{ name: "lang", value: "en-XA", url: base }]);
});

test("the check sees English", async ({ page: tab }) => {
  await tab.goto("/login");
  await expect(tab.locator("form")).toBeVisible();
  await tab.evaluate(() => document.querySelector("main")?.append("Plain words"));
  expect(english(await said(tab, "body"))).toEqual(["Plain words"]);
});

test.describe("signed out", () => {
  test.skip(!translated("src/pages/SignIn.tsx"), "SignIn.tsx is still to be translated");
  for (const [name, path] of [
    ["sign-in", "/login"],
    ["a forgotten password", "/forgot"],
    ["single sign-on", "/login/sso"],
    ["a reset link that has gone", "/reset/not-a-token"],
  ] as const) {
    test(name, async ({ page: tab }) => {
      await tab.goto(path);
      await tab.waitForLoadState("networkidle");
      await inPseudo(tab, "body");
    });
  }
});

test.describe("served", () => {
  test.skip(!translated("src/pages/Served.tsx"), "Served.tsx is still to be translated");
  for (const [name, payload] of served) {
    test(name, async ({ page: tab }) => {
      await serve(tab, payload);
      await inPseudo(tab, "body");
    });
  }
});

const nav = (route: Route) => route.request().url().endsWith("/api/v1/nav");

test.describe("signed in", () => {
  test.use({ storageState: "a11y/.signed-in.json" });
  test.skip(!translated("src/components/app/layout.tsx"), "layout.tsx is still to be translated");
  const at = `/o/${org.slug}/today`;

  test("the sidebar", async ({ page: tab }) => {
    await tab.goto(at);
    await expect(tab.getByRole("navigation").first()).toBeVisible();
    await tab.waitForLoadState("networkidle");
    await inPseudo(tab, "aside");
  });

  test("the sidebar, with the organisation switcher open", async ({ page: tab }) => {
    await tab.goto(at);
    await tab.locator("aside summary").click();
    await inPseudo(tab, "aside");
  });

  test("the sidebar while it loads", async ({ page: tab }) => {
    await tab.route(`**/o/${org.slug}/api/v1/**`, (route) => (nav(route) ? undefined : route.fallback()));
    await tab.goto(at);
    await expect(tab.getByRole("status")).not.toHaveText("");
    await inPseudo(tab, "body");
  });

  test("an organisation that can't be read", async ({ page: tab }) => {
    await tab.route(`**/o/${org.slug}/api/v1/nav`, (route) =>
      route.fulfill({ status: 429, contentType: "application/problem+json", json: { type: "/api/problems/throttled", title: "", status: 429 } }),
    );
    await tab.goto(at);
    await expect(tab.getByRole("alert")).toBeVisible();
    await inPseudo(tab, "main");
  });
});
