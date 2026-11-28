import { expect, test, type Page, type Route } from "@playwright/test";
import { readFileSync } from "node:fs";

import { serve, served, servedWords } from "./served.ts";
import { base, member, org, reader, seed } from "./world.ts";

// In en-XA every word the app says is accented and bracketed, so plain letters left on a
// page are words that never went through react-intl. A page is checked once its file is
// off .oxlintrc.json's list of those still to be translated (or kept English, as /api is).
const untranslated = new Set<string>(
  (JSON.parse(readFileSync(new URL("../.oxlintrc.json", import.meta.url), "utf8")) as { overrides: { files: string[] }[] }).overrides.flatMap((o) => o.files),
);
const translated = (file: string) => !untranslated.has(file);

// Dates come from Intl, which has no data of its own for a pseudo-locale, so en-XA's
// months, weekdays and AM and PM are English: they say nothing about what react-intl was given.
const dateWords = (options: Intl.DateTimeFormatOptions, count: number, at: (i: number) => Date) =>
  Array.from({ length: count }, (_, i) => new Intl.DateTimeFormat("en-XA", { ...options, timeZone: "UTC" }).format(at(i)));
const months = [
  ...dateWords({ month: "short" }, 12, (i) => new Date(Date.UTC(2026, i, 1))),
  ...dateWords({ month: "long" }, 12, (i) => new Date(Date.UTC(2026, i, 1))),
  ...dateWords({ weekday: "short" }, 7, (i) => new Date(Date.UTC(2026, 10, 22 + i))),
  ...dateWords({ weekday: "long" }, 7, (i) => new Date(Date.UTC(2026, 10, 22 + i))),
  "AM",
  "PM",
];

// What a page may show as it is: the data the test put there, the name, and Intl's months.
const strings = (v: unknown): string[] => (typeof v === "string" ? [v, ...v.split("\n")] : v && typeof v === "object" ? Object.values(v).flatMap(strings) : []);
// What a LinkedIn profile's address looks like, as Add's empty field shows it.
const addresses = ["https://www.linkedin.com/in/…"];
// Names of products, and of languages as each names itself (Intl.DisplayNames), are names.
const products = [
  "Hunter",
  "Prospeo",
  "Claude Code",
  "Anthropic (Claude)",
  "OpenAI-compatible",
  "Okta",
  "Entra ID",
  "Google Workspace",
  "OpenID Connect",
  "SearXNG",
  "LinkedIn",
  "GitHub",
];
const locales = JSON.parse(readFileSync(new URL("../locales.json", import.meta.url), "utf8")) as { source: string; translated: string[]; pseudo: string[] };
const languages = [locales.source, ...locales.translated, ...locales.pseudo].map((l) => new Intl.DisplayNames([l], { type: "language" }).of(l) ?? l);

const allowed = [
  "Prospector",
  // The front page's example agent.
  "research-agent",
  ...products,
  ...languages,
  ...addresses,
  member.name,
  member.email,
  reader.name,
  reader.email,
  org.name,
  ...servedWords,
  ...months,
  ...strings(seed),
];

// The organisation's own wording (its note template, pitch and the rest, defaults
// included) is the member's to write, so a page showing it shows data.
async function orgWords(tab: Page) {
  const get = async (path: string) => (await tab.request.get(`/o/${org.slug}/api/v1/${path}`)).json();
  const settings = await get("settings");
  // What an agent's last check found is that check's own words, and the names of the API's
  // scopes and an agent's tools are identifiers.
  const agents = (await get("agents")) as { items: { problem: string }[] };
  const connect = (await get("connect")) as { scopes: { name: string }[]; tools: { name: string }[] };
  const keys = (await get("keys")) as { items: { name: string }[] };
  const words = [
    ...strings(settings),
    ...agents.items.map((a) => a.problem),
    ...connect.scopes.map((x) => x.name),
    ...connect.tools.map((x) => x.name),
    ...keys.items.map((k) => k.name),
  ];
  for (const w of words) if (w && !allowed.includes(w)) allowed.push(w);
}

// Identifiers and addresses a sentence mentions are left as they are in every language:
// URLs, hosts, email addresses, an API call, scope names like settings:write, tool names
// like add_fact, and the keys the organisation's own text is read by ({first}, them:, (must)).
const identifiers = [
  /\S+:\/\/\S+/g,
  /\S+@\S+\.\S+/g,
  /\b[\w-]+(?:\.[\w-]+)*\.(?:example|com)\b(?:\/\S*)?/g,
  /\b(?:GET|POST|PUT|PATCH|DELETE) \S+/g,
  /\b[a-z]+(?:[:_][a-z]+)+\b/g,
  // A key's first characters, which tell keys apart.
  /\bpk_\w+/g,
  /\{\w+\}/g,
  /\((?:must)\)/g,
  /\b(?:them|us):/g,
];

// said is everything a person sees or hears inside the first element matching scope:
// text, screen-reader-only text included and hidden text not, the attributes read out
// or shown, and the tab's title when the whole page is checked.
async function said(tab: Page, scope: string): Promise<string[]> {
  return tab.evaluate((within) => {
    const root = document.querySelector(within);
    if (!root) throw new Error(`nothing matches ${within}`);
    const out: string[] = within === "body" ? [document.title] : [];
    // oxlint-disable-next-line unicorn/consistent-function-scoping -- it runs in the page, so it can't be hoisted out of it
    // Text marked as in another language (WCAG 3.1.2), such as a prompt for an agent, is meant to be.
    const page = document.documentElement.lang;
    const shown = (el: Element) =>
      !el.closest("[aria-hidden=true]") && (el.closest("[lang]")?.getAttribute("lang") ?? page) === page && el.checkVisibility({ visibilityProperty: true });
    // Code, keys and sample output are machine text, not words to translate.
    const walk = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    for (let n = walk.nextNode(); n; n = walk.nextNode()) {
      const el = n.parentElement;
      if (el && !el.closest("script, style, code, kbd, samp") && (el.closest("option") || shown(el)) && n.textContent?.trim()) out.push(n.textContent.trim());
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
function english(seen: string[]) {
  // Longest first, so a name isn't cut into by a shorter one inside it. filter has made a
  // copy, and this tsconfig's lib has no toSorted.
  // oxlint-disable-next-line unicorn/no-array-sort
  const words = allowed.filter((w) => w.trim()).sort((a, b) => b.length - a.length);
  // A template of the organisation's (a note, a pitch) is shown filled in: its
  // placeholders, such as {first}, stand for whatever went in them.
  const filled = words.filter((w) => /\{\w+\}/.test(w)).map((w) => new RegExp(w.replace(/[.*+?^$()|[\]\\]/g, "\\$&").replace(/\\?\{\w+\\?\}/g, ".*?"), "g"));
  return seen.filter((s) => {
    // Whole allowed strings first, as an identifier taken out could break one.
    const left = words.reduce(
      (rest, w) => rest.replaceAll(w, ""),
      filled.reduce((rest, t) => rest.replace(t, ""), s),
    );
    return /[A-Za-z]{2,}/.test(identifiers.reduce((rest, t) => rest.replace(t, ""), left));
  });
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
  await tab.evaluate(() => {
    const main = document.querySelector("main")!;
    main.append("Plain words");
    main.querySelector("input")?.setAttribute("placeholder", "Plain placeholder");
    main.setAttribute("aria-label", "Plain label");
  });
  expect(new Set(english(await said(tab, "body")))).toEqual(new Set(["Plain label", "Plain placeholder", "Plain words"]));
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

  test("a wrong password", async ({ page: tab }) => {
    await tab.goto("/login");
    await tab.getByRole("textbox").first().fill(member.email);
    await tab.locator("input[type=password]").fill("not the password at all");
    await tab.locator("form button[type=submit]").click();
    await expect(tab.getByRole("alert")).toBeVisible();
    await inPseudo(tab, "body");
  });

  test("signing in after choosing a password", async ({ page: tab }) => {
    await tab.goto("/login?reset=1");
    await tab.waitForLoadState("networkidle");
    await inPseudo(tab, "body");
  });

  test("a reset link asked for", async ({ page: tab }) => {
    await tab.goto("/forgot");
    await tab.locator("input[type=email]").fill("nobody@prospector.example");
    await tab.locator("form button[type=submit]").click();
    await expect(tab.getByRole("status")).toBeVisible();
    await inPseudo(tab, "body");
  });

  test("single sign-on that covers no one", async ({ page: tab }) => {
    await tab.goto("/login/sso?email=nobody%40prospector.example");
    await expect(tab.getByRole("alert")).toBeVisible();
    await inPseudo(tab, "body");
  });
});

// The front page is built in each language; its form is drawn by its own script, in the
// page's. Its prices are marked English, and the API reference is all English like the spec.
test("the front page, with its form", async ({ page: tab }) => {
  test.skip(!translated("src/site/Landing.tsx"), "Landing.tsx is still to be translated");
  await tab.goto("/");
  await expect(tab.locator("#enquiry form")).toBeVisible();
  await inPseudo(tab, "body");
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
    await expect(tab.getByRole("status").first()).not.toHaveText("");
    await inPseudo(tab, "body");
  });

  test("an organisation that isn't there", async ({ page: tab }) => {
    await tab.goto("/o/nowhere/today");
    await tab.waitForLoadState("networkidle");
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

test.describe("signed in, only reading", () => {
  test.use({ storageState: "a11y/.reader.json" });
  test.skip(!translated("src/components/app/layout.tsx"), "layout.tsx is still to be translated");

  test("the sidebar", async ({ page: tab }) => {
    await tab.goto(`/o/${org.slug}/companies`);
    await expect(tab.locator("aside").getByText(reader.name)).toBeVisible();
    await inPseudo(tab, "aside");
  });
});

// The pages inside an organisation, each checked in main once its file is translated:
// loaded, as someone who only reads it, while its data loads, and when its data fails.
// files are every file the page draws with that is still to be translated in some group.
const appPages: { name: string; files: string[]; path: (tab: Page) => Promise<string> }[] = [
  { name: "today", files: ["src/pages/Today.tsx"], path: async () => "today" },
  { name: "companies", files: ["src/pages/Companies.tsx"], path: async () => "companies" },
  {
    name: "a company",
    files: ["src/pages/Company.tsx", "src/components/app/subscription.tsx", "src/pages/Onboarding.tsx"],
    path: async (tab) => `companies/${await first(tab, "companies")}`,
  },
  { name: "add", files: ["src/pages/Add.tsx"], path: async () => "add" },
  { name: "the bookmarklet's window", files: ["src/pages/Add.tsx"], path: async () => "add?popup=1" },
  { name: "research", files: ["src/pages/Research.tsx"], path: async () => "research" },
  { name: "conversations", files: ["src/pages/Conversations.tsx"], path: async () => "conversations" },
  { name: "enquiries", files: ["src/pages/Enquiries.tsx"], path: async () => "enquiries" },
  { name: "onboarding", files: ["src/pages/Onboarding.tsx"], path: async () => "onboarding" },
  { name: "subscriptions", files: ["src/pages/Subscriptions.tsx", "src/components/app/subscription.tsx"], path: async () => "subscriptions" },
  { name: "campaigns", files: ["src/pages/Campaigns.tsx"], path: async () => "campaigns" },
  { name: "agents", files: ["src/pages/Agents.tsx"], path: async () => "agents" },
  { name: "tasks", files: ["src/pages/Tasks.tsx"], path: async () => "tasks" },
  { name: "a task", files: ["src/pages/Task.tsx"], path: async (tab) => `tasks/${await first(tab, "tasks")}` },
  { name: "settings", files: ["src/pages/Settings.tsx"], path: async () => "settings" },
  {
    name: "an agreement's preparation",
    files: ["src/pages/Prepare.tsx"],
    path: async (tab) => `agreements/${await first(tab, "agreements")}/prepare`,
  },
];

async function first(tab: Page, list: string) {
  const { items } = (await (await tab.request.get(`/o/${org.slug}/api/v1/${list}`)).json()) as { items: { id: number }[] };
  return items[0]!.id;
}

const scope = (path: string) => (path.includes("popup=1") ? "body" : "main");
const throttled = (route: Route) =>
  route.fulfill({ status: 429, contentType: "application/problem+json", json: { type: "/api/problems/throttled", title: "", status: 429 } });

for (const { name, files, path } of appPages) {
  test.describe(name, () => {
    const left = files.filter((f) => !translated(f));
    test.skip(left.length > 0, `${left.join(", ")} still to be translated`);

    test.describe("as an owner", () => {
      test.use({ storageState: "a11y/.signed-in.json" });

      test("loaded", async ({ page: tab }) => {
        await orgWords(tab);
        const at = await path(tab);
        await tab.goto(`/o/${org.slug}/${at}`);
        await tab.waitForLoadState("networkidle");
        await inPseudo(tab, scope(at));
      });

      test("while it loads", async ({ page: tab }) => {
        const at = await path(tab);
        await tab.route(`**/o/${org.slug}/api/v1/**`, (route) => (nav(route) ? route.fallback() : undefined));
        await tab.goto(`/o/${org.slug}/${at}`);
        await expect(tab.getByRole("status").first()).not.toHaveText("");
        await inPseudo(tab, "body");
      });

      test("when its data fails", async ({ page: tab }) => {
        const at = await path(tab);
        await tab.route(`**/o/${org.slug}/api/v1/**`, (route) => (nav(route) ? route.fallback() : throttled(route)));
        await tab.goto(`/o/${org.slug}/${at}`);
        await expect(tab.getByRole("alert").first()).toBeVisible();
        await inPseudo(tab, scope(at));
      });
    });

    test.describe("as someone who only reads it", () => {
      test.use({ storageState: "a11y/.reader.json" });

      test("loaded", async ({ page: tab }) => {
        await orgWords(tab);
        const at = await path(tab);
        await tab.goto(`/o/${org.slug}/${at}`);
        await tab.waitForLoadState("networkidle");
        await inPseudo(tab, scope(at));
      });
    });
  });
}
