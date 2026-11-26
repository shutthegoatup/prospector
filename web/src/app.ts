// The app's page script. Built to assets/js/app.js by `npm run build` in web/; api.d.ts
// is generated from api/openapi.yaml, so a call the spec doesn't have fails to compile.
import createClient from "openapi-fetch";
import type { components, paths } from "./api";
import { problemText } from "./problem.ts";

type Schemas = components["schemas"];
type Json = Record<string, unknown>;

const base = document.body.dataset.base || "/";
const content = document.getElementById("content");
const org = base.match(/^\/o\/([^/]+)\//)?.[1] ?? "";

const api = createClient<paths>({ baseUrl: location.origin });

function url(path: string): string {
  return path.startsWith("/") ? path : base + path;
}

function signIn(): void {
  location.href = "/login?next=" + encodeURIComponent(location.pathname + location.search);
}

// send calls a path a page names in its markup: the API, or a page route not yet moved
// onto it.
async function send(path: string, data: unknown, method = "POST"): Promise<Json> {
  const init: RequestInit = { method, headers: { "content-type": "application/json" } };
  if (method !== "DELETE") init.body = JSON.stringify(data);
  const res = await fetch(url(path), init);
  if (res.status === 401) signIn();
  const type = res.headers.get("content-type") || "";
  // The API answers a failure as RFC 9457 problem JSON; older routes in plain text.
  if (!res.ok) throw new Error(type.includes("problem+json") ? ((await res.json()) as Schemas["Problem"]).detail || res.statusText : (await res.text()).trim());
  return type.includes("json") ? ((await res.json()) as Json) : {};
}

// answered turns a typed API call's outcome into its data, or the problem it reported.
function answered<T>(r: { data?: T; error?: unknown; response: Response }): T {
  if (r.response.status === 401) signIn();
  if (r.error !== undefined || !r.response.ok) throw new Error(problemText(r.error, r.response.statusText, r.response.status));
  return r.data as T;
}

function save(data: unknown, name: string): void {
  const a = document.createElement("a");
  a.href = URL.createObjectURL(new Blob([JSON.stringify(data, null, 2)], { type: "application/json" }));
  a.download = name;
  a.click();
  URL.revokeObjectURL(a.href);
}

function report(err: unknown): void {
  alert(err instanceof Error ? err.message : String(err));
}

// Re-renders the page's content from the server. The fragment comes from templ, which
// escapes everything; DOMParser never runs scripts in it.
async function refresh(focus?: string | false): Promise<void> {
  if (!content) return;
  const res = await fetch(location.href, { headers: { "X-Fragment": "1" } });
  if (!res.ok) return;
  const doc = new DOMParser().parseFromString(await res.text(), "text/html");
  content.classList.add("settled");
  content.replaceChildren(...doc.body.childNodes);
  const card = focus && content.querySelector(`[data-card="${focus}"]`);
  if (card) {
    card.classList.add("ring-2", "ring-primary");
    card.scrollIntoView({ block: "start", behavior: "smooth" });
  }
}

// The clipboard API exists only on HTTPS and localhost; on a plain-HTTP LAN address
// the older execCommand copy still works inside the click.
function writeClipboard(text: string): void {
  if (navigator.clipboard && window.isSecureContext) {
    navigator.clipboard.writeText(text).catch(() => {});
    return;
  }
  const area = document.createElement("textarea");
  area.value = text;
  area.setAttribute("readonly", "");
  area.style.position = "fixed";
  area.style.opacity = "0";
  document.body.append(area);
  area.select();
  document.execCommand("copy");
  area.remove();
}

function copy(button: HTMLElement, text: string, link?: string): void {
  writeClipboard(text);
  if (/^https?:\/\//.test(link || "")) window.open(link, "linkedin");
  button.textContent = "Copied ✓";
}

document.addEventListener("click", async (e) => {
  const target = e.target as Element;
  // The bookmarklet link is for dragging to the bookmarks bar, not for clicking here.
  if (target.closest("[data-bookmarklet]")) return void e.preventDefault();
  const b = target.closest("button");
  if (!b) return;
  const d = b.dataset;
  try {
    if (d.mark) {
      await send("ui/mark", { id: +(d.id || 0), step: d.mark });
      refresh(d.next !== "0" && d.next);
    } else if ("copy" in d) {
      copy(b, d.copy || "", d.open);
    } else if (d.copyFrom) {
      const from = document.getElementById(d.copyFrom) as HTMLInputElement | HTMLTextAreaElement | null;
      copy(b, from?.value || "", d.open);
    } else if (d.download) {
      save(await send(d.download, JSON.parse(d.body || "{}")), d.file || "download.json");
      refresh();
    } else if (d.post) {
      if (d.confirm && !confirm(d.confirm)) return;
      const body = JSON.parse(d.body || "{}") as Json;
      if (d.withFit) body.fit = (document.getElementById(d.withFit) as HTMLInputElement | null)?.value || "";
      const res = await send(d.post, body, d.method);
      if (typeof res.go === "string") return void (location.href = res.go);
      refresh();
    }
  } catch (err) {
    report(err);
  }
});

type Field = HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement;

// Pasting a profile link fills an empty name from it: /in/hannah-pryce-4a2b1c → Hannah Pryce.
document.addEventListener("input", (e) => {
  const input = e.target as Field;
  if (input.name !== "linkedin" || !input.form) return;
  const name = input.form.elements.namedItem("name") as HTMLInputElement | null;
  const slug = (input.value.match(/linkedin\.com\/in\/([^/?#]+)/) || [])[1];
  if (!name || name.value || !slug) return;
  const words = decodeURIComponent(slug).split("-").filter((w) => w && !/\d/.test(w));
  name.value = words.map((w) => w.charAt(0).toUpperCase() + w.slice(1)).join(" ");
});

// The body a PATCH operation takes, from the spec.
type PatchBody<P extends keyof paths> = paths[P] extends { patch: { requestBody: { content: { "application/json": infer B } } } } ? B : never;
type EnquiryChange = PatchBody<"/o/{org}/api/v1/enquiries/{id}">;

// The fields a page may change one at a time, checked against the spec: a field the spec
// drops or renames fails the build.
const subscriptionFields = ["name", "amount", "per", "start", "term_months", "then", "notice_days", "notice_on", "ended_on"] as const satisfies readonly (keyof Schemas["SubscriptionChange"])[];
const dealFields = ["kind", "amount", "close_date", "owner", "stage", "next_step", "next_date", "lost_reason"] as const satisfies readonly (keyof Schemas["DealChange"])[];
const enquiryFields = ["state", "owner", "note"] as const satisfies readonly (keyof EnquiryChange)[];
const contactFields = ["person", "title", "email", "linkedin", "phone", "tz", "sources", "call_notes", "send_to", "message", "note", "subject", "body", "followup", "stage", "next_step", "next_date", "lost_reason"] as const satisfies readonly (keyof Schemas["ContactChange"])[];
const campaignFields = ["name", "kind", "audience", "subject", "body"] as const satisfies readonly (keyof Schemas["CampaignChange"])[];

function oneOf<T extends string>(list: readonly T[], f: string, what: string): T {
  if (!(list as readonly string[]).includes(f)) throw new Error(`${what} has no ${f}.`);
  return f as T;
}

// A file chosen for an agreement goes up as the PDF it is, the body of a PUT.
document.addEventListener("change", async (e) => {
  const input = (e.target as Element).closest<HTMLInputElement>("input[type=file][data-upload]");
  const file = input?.files?.[0];
  if (!input || !file) return;
  try {
    answered(
      await api.PUT("/o/{org}/api/v1/agreements/{id}/document", {
        params: { path: { org, id: +(input.dataset.upload || 0) }, query: { name: file.name } },
        // The spec's binary body is typed as a string; the File is sent as it is.
        body: file as unknown as string,
        bodySerializer: (b) => b,
        headers: { "content-type": "application/pdf" },
      }),
    );
    refresh();
  } catch (err) {
    input.value = "";
    report(err);
  }
});

document.addEventListener("change", async (e) => {
  const f = (e.target as Element).closest<Field>("[data-field]");
  if (!f) return;
  const field = f.dataset.field || "";
  try {
    // A deal after the conversation's own is changed by its own id, not its person's.
    if (f.dataset.deal) {
      const change: Schemas["DealChange"] = { [oneOf(dealFields, field, "A deal")]: f.value };
      answered(await api.PATCH("/o/{org}/api/v1/deals/{id}", { params: { path: { org, id: +f.dataset.deal } }, body: change }));
    } else if (f.dataset.sub) {
      const key = oneOf(subscriptionFields, field, "A subscription");
      const number = f.type === "number" || !!f.dataset.number;
      // An emptied number box is not a nought: saving one would end a commitment.
      if (number && f.value.trim() === "") throw new Error("Enter a number, or 0 for none.");
      const change: Schemas["SubscriptionChange"] = { [key]: number ? +f.value : f.value };
      answered(await api.PATCH("/o/{org}/api/v1/subscriptions/{id}", { params: { path: { org, id: +f.dataset.sub } }, body: change }));
    } else if (f.dataset.enquiry) {
      const change: EnquiryChange = { [oneOf(enquiryFields, field, "An enquiry")]: f.value };
      answered(await api.PATCH("/o/{org}/api/v1/enquiries/{id}", { params: { path: { org, id: +f.dataset.enquiry } }, body: change }));
    } else if (f.dataset.campaign) {
      const change: Schemas["CampaignChange"] = { [oneOf(campaignFields, field, "A campaign")]: f.value };
      answered(await api.PATCH("/o/{org}/api/v1/campaigns/{id}", { params: { path: { org, id: +f.dataset.campaign } }, body: change }));
    } else if (f.dataset.step) {
      if (field !== "due") throw new Error(`A step's ${field} is changed another way.`);
      answered(await api.PATCH("/o/{org}/api/v1/onboarding-steps/{id}", { params: { path: { org, id: +f.dataset.step } }, body: { due: f.value } }));
    } else if (field.startsWith("deal_")) {
      // The deal a conversation carries, changed through its person.
      const change: Schemas["DealChange"] = { [oneOf(dealFields, field.slice(5), "A deal")]: f.value };
      answered(await api.PATCH("/o/{org}/api/v1/contacts/{id}/deal", { params: { path: { org, id: +(f.dataset.id || 0) } }, body: change }));
    } else {
      const change: Schemas["ContactChange"] = { [oneOf(contactFields, field, "A person")]: f.value };
      answered(await api.PATCH("/o/{org}/api/v1/contacts/{id}", { params: { path: { org, id: +(f.dataset.id || 0) } }, body: change }));
    }
    if (f.dataset.refresh || field === "phone") refresh();
  } catch (err) {
    report(err);
  }
});

// Forms post their fields as JSON: numbers as numbers, a lone checkbox as true/false,
// checkboxes marked data-multi as a list of the checked values, numbers when marked data-number.
function formJSON(form: HTMLFormElement): Json {
  const out: Json = {};
  for (const el of Array.from(form.elements) as Field[]) {
    if (!el.name || el.disabled) continue;
    if (el instanceof HTMLInputElement && el.type === "checkbox" && el.dataset.multi) {
      const list = (out[el.name] as (string | number)[] | undefined) || [];
      if (el.checked) list.push(el.dataset.number ? +el.value : el.value);
      out[el.name] = list;
    } else if (el instanceof HTMLInputElement && el.type === "checkbox") {
      out[el.name] = el.checked;
    } else if (el.type === "number" || el.dataset.number) {
      out[el.name] = el.value === "" ? 0 : +el.value;
    } else {
      out[el.name] = el.value;
    }
  }
  return out;
}

document.addEventListener("submit", async (e) => {
  const form = (e.target as Element).closest<HTMLFormElement>("form[data-form]");
  if (!form) return;
  e.preventDefault();
  const result = form.querySelector<HTMLElement>("[data-result]");
  const button = form.querySelector<HTMLButtonElement>("[type=submit]");
  if (button) button.disabled = true;
  try {
    const res = await send(form.dataset.form || "", formJSON(form), form.dataset.method);
    if (typeof res.go === "string") return void (location.href = res.go);
    // An API answer is the thing made; data-go is where its page is, by its id.
    if (form.dataset.go && (typeof res.id === "number" || typeof res.id === "string")) return void (location.href = url(form.dataset.go.replace("{id}", String(res.id))));
    if (result) result.textContent = typeof res.message === "string" ? res.message : "Saved ✓";
    if (form.dataset.reset) {
      for (const el of Array.from(form.elements) as Field[]) {
        if (el.type !== "checkbox" && el.type !== "submit" && el.tagName !== "SELECT" && !(el as HTMLInputElement).hidden && !el.closest("fieldset[data-keep]")) el.value = "";
      }
      form.querySelector<HTMLInputElement>("input:not([type=hidden])")?.focus();
    }
    if (form.dataset.close) setTimeout(() => window.close(), 1200);
    if (form.dataset.refresh) setTimeout(() => refresh(), 600);
  } catch (err) {
    if (result) {
      result.textContent = err instanceof Error ? err.message : String(err);
      result.style.color = "var(--stop)";
      setTimeout(() => (result.style.color = ""), 4000);
    } else report(err);
  } finally {
    if (button) button.disabled = false;
  }
});

const typing = () => /INPUT|TEXTAREA|SELECT/.test(document.activeElement?.tagName || "");

// On Today: o opens the first card's profile and copies its note, a marks it added, s skips.
document.addEventListener("keydown", (e) => {
  if (e.metaKey || e.ctrlKey || e.altKey || typing()) return;
  const first = content?.querySelector("[data-card]");
  const button = first?.querySelector<HTMLElement>(`[data-key="${e.key}"]`);
  if (button) {
    e.preventDefault();
    button.click();
  }
});

// A page with work in hand of its own, like the field editor, is left alone.
setInterval(() => {
  if (!typing() && !document.querySelector("details[open] form, [data-no-refresh]")) refresh();
}, 60000);
