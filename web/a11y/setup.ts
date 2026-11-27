import { expect, test as setup } from "@playwright/test";
import createClient from "openapi-fetch";

import type { paths } from "../src/api";
import { base, member, org } from "./world.ts";

function ok<T>({ data, error, response }: { data?: T; error?: unknown; response: Response }): T {
  expect(error, `${response.url} answered ${response.status}: ${JSON.stringify(error)}`).toBeUndefined();
  return data as T;
}

// Signs in, then gives the organisation something on every page, through the API as the
// app itself would.
setup("an organisation with something on every page", async ({ request }) => {
  const signedIn = await request.post("/api/v1/session", { data: { email: member.email, password: member.password } });
  expect(signedIn.status()).toBe(200);
  const state = await request.storageState({ path: "a11y/.signed-in.json" });
  const cookie = state.cookies.map((c) => `${c.name}=${c.value}`).join("; ");
  const api = createClient<paths>({ baseUrl: base, headers: { "Content-Type": "application/json", cookie } });
  const params = { path: { org: org.slug } };

  const people = [
    { name: "Ada Brook", title: "Head of Operations", company: "Brook & Vale", email: "ada@brookvale.example" },
    { name: "Kit Moreau", title: "Founder", company: "Moreau Freight", email: "kit@moreau.example", added: true },
    { name: "Sol Nakamura", title: "Buyer", company: "Northwind Parts", email: "sol@northwind.example" },
  ];
  const contacts = [];
  for (const body of people) {
    contacts.push(ok(await api.POST("/o/{org}/api/v1/people", { params, body })).id);
  }
  const companies = ok(await api.GET("/o/{org}/api/v1/companies", { params })).items;
  const company = companies.find((c) => c.name === "Brook & Vale")!.id;

  ok(await api.POST("/o/{org}/api/v1/campaigns", { params, body: { name: "Spring update", subject: "What's new", body: "Hello,\n\nA short note." } }));
  ok(
    await api.POST("/o/{org}/api/v1/tasks", {
      params,
      body: { title: "Find freight firms", brief: "Freight firms in the north with a fleet of ten or more.", want: 20 },
    }),
  );
  ok(await api.POST("/o/{org}/api/v1/subscriptions", { params, body: { company, name: "Standard", amount: "£5,000", start: "2026-01-01", term_months: 12 } }));
  ok(await api.PATCH("/o/{org}/api/v1/settings", { params, body: { onboarding_steps: "Kick-off call\nSend the welcome pack\nFirst review" } }));
  ok(await api.POST("/o/{org}/api/v1/onboardings", { params, body: { company } }));
  ok(
    await api.POST("/o/{org}/api/v1/agents", {
      params,
      body: { name: "Scout", base_url: "https://llm.example/v1", model: "local", api_key: "a11y-not-a-key" },
    }),
  );
  ok(await api.POST("/o/{org}/api/v1/agreements", { params, body: { company, name: "Master services" } }));
});
