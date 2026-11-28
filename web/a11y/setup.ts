import { expect, test as setup } from "@playwright/test";
import createClient from "openapi-fetch";

import type { paths } from "../src/api";
import { base, member, org, reader, seed } from "./world.ts";

function ok<T>({ data, error, response }: { data?: T; error?: unknown; response: Response }): T {
  expect(error, `${response.url} answered ${response.status}: ${JSON.stringify(error)}`).toBeUndefined();
  return data as T;
}

setup("a member who only reads", async ({ request }) => {
  const signedIn = await request.post("/api/v1/session", { data: { email: reader.email, password: reader.password } });
  expect(signedIn.status()).toBe(200);
  await request.storageState({ path: "a11y/.reader.json" });
});

// Signs in, then gives the organisation something on every page, through the API as the
// app itself would.
setup("an organisation with something on every page", async ({ request }) => {
  const signedIn = await request.post("/api/v1/session", { data: { email: member.email, password: member.password } });
  expect(signedIn.status()).toBe(200);
  const state = await request.storageState({ path: "a11y/.signed-in.json" });
  const cookie = state.cookies.map((c) => `${c.name}=${c.value}`).join("; ");
  const api = createClient<paths>({ baseUrl: base, headers: { "Content-Type": "application/json", cookie } });
  const params = { path: { org: org.slug } };

  const contacts = [];
  for (const body of seed.people) {
    contacts.push(ok(await api.POST("/o/{org}/api/v1/people", { params, body })).id);
  }
  const companies = ok(await api.GET("/o/{org}/api/v1/companies", { params })).items;
  const company = companies.find((c) => c.name === seed.people[0]!.company)!.id;

  ok(await api.POST("/o/{org}/api/v1/campaigns", { params, body: seed.campaign }));
  ok(await api.POST("/o/{org}/api/v1/tasks", { params, body: seed.task }));
  ok(await api.POST("/o/{org}/api/v1/subscriptions", { params, body: { company, ...seed.subscription } }));
  ok(await api.PATCH("/o/{org}/api/v1/settings", { params, body: { onboarding_steps: seed.onboardingSteps } }));
  ok(await api.POST("/o/{org}/api/v1/onboardings", { params, body: { company } }));
  ok(await api.POST("/o/{org}/api/v1/agents", { params, body: seed.agent }));
  ok(await api.POST("/o/{org}/api/v1/agreements", { params, body: { company, ...seed.agreement } }));

  // Postmark connected, and an address it has reported as bouncing for good.
  const hook = ok(await api.POST("/o/{org}/api/v1/mail-server/bounce-hook", { params }));
  const bounced = await request.post("/hooks/postmark", {
    headers: { Authorization: `Basic ${btoa(`${hook.username}:${hook.password}`)}` },
    data: { RecordType: "Bounce", Type: "HardBounce", Email: "ines@harbourline.example" },
  });
  expect(bounced.status(), await bounced.text()).toBe(204);
});
