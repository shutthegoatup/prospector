import type { Page } from "@playwright/test";

import type { Served } from "../src/served.ts";
import { org } from "./world.ts";

// The pages the server decides, drawn from what it puts in the shell's data-page: each
// state it can send.
const steps = [
  { id: 1, name: "Kick-off call", due: "2026-12-01", done_on: null, mine: false },
  { id: 2, name: "Send the welcome pack", due: null, done_on: "2026-11-20", mine: true },
];
const customer = "ada@brookvale.example";

export const served: [string, Served][] = [
  ["a customer's steps", { page: "customer", state: "steps", token: "t", org: org.name, email: customer, day: "2026-11-27", steps }],
  ["a customer asking for a link", { page: "customer", state: "open", org: org.name, steps: [] }],
  ["a customer whose link has gone", { page: "customer", state: "again", org: org.name, steps: [] }],
  ["a customer sent a link", { page: "customer", state: "sent", email: customer, steps: [] }],
  ["a customer with nothing to do", { page: "customer", state: "none", org: org.name, steps: [] }],
  ["unsubscribing", { page: "unsubscribe", token: "t", done: false }],
  ["unsubscribed", { page: "unsubscribe", token: "t", done: true }],
  ["starting a first organisation", { page: "start" }],
];

// What those payloads put on a page as they are: names a page shows, never words of its own.
export const servedWords = [org.name, customer, ...steps.map((s) => s.name)];

// serve opens /c as the server would send it with payload, in the shell it really sends.
export async function serve(tab: Page, payload: Served) {
  await tab.route("**/c", async (route) => {
    const shell = await (await route.fetch()).text();
    const attribute = JSON.stringify(payload).replaceAll("&", "&amp;").replaceAll('"', "&quot;");
    await route.fulfill({ contentType: "text/html", body: shell.replace(/data-page="[^"]*"/, `data-page="${attribute}"`) });
  });
  await tab.goto("/c");
  await tab.waitForLoadState("networkidle");
}
