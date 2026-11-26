// The script of the server-rendered pages that remain: choosing a first password and
// starting a first organisation. Built to assets/js/app.js by `npm run build` in web/.
import type { components } from "./api";

type Json = Record<string, unknown>;

// A form posts its fields as JSON, to the API or to a route of the server's own. The
// API answers a failure as RFC 9457 problem JSON; the server's routes in plain text.
async function send(path: string, data: Json): Promise<Json> {
  const res = await fetch(path, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(data) });
  const type = res.headers.get("content-type") || "";
  if (!res.ok) throw new Error(type.includes("problem+json") ? ((await res.json()) as components["schemas"]["Problem"]).detail || res.statusText : (await res.text()).trim());
  return type.includes("json") ? ((await res.json()) as Json) : {};
}

function formJSON(form: HTMLFormElement): Json {
  const out: Json = {};
  for (const el of Array.from(form.elements) as HTMLInputElement[]) {
    if (el.name && !el.disabled) out[el.name] = el.value;
  }
  return out;
}

// An answer that says where to go is followed; data-go names a page by the fields of
// what was made, as /o/{slug}/settings.
function next(form: HTMLFormElement, res: Json): string | undefined {
  if (typeof res.go === "string") return res.go;
  const go = form.dataset.go;
  if (!go) return undefined;
  const to = go.replace(/\{(\w+)\}/g, (_, k: string) => encodeURIComponent(String(res[k] ?? "")));
  return to.includes("//") ? undefined : to;
}

document.addEventListener("submit", async (e) => {
  const form = (e.target as Element).closest<HTMLFormElement>("form[data-form]");
  if (!form) return;
  e.preventDefault();
  const result = form.querySelector<HTMLElement>("[data-result]");
  const button = form.querySelector<HTMLButtonElement>("[type=submit]");
  if (button) button.disabled = true;
  try {
    const res = await send(form.dataset.form || "", formJSON(form));
    const to = next(form, res);
    if (to) return void (location.href = to);
    if (result) result.textContent = typeof res.message === "string" ? res.message : "Saved ✓";
  } catch (err) {
    const text = err instanceof Error ? err.message : String(err);
    if (result) result.textContent = text;
    else alert(text);
  } finally {
    if (button) button.disabled = false;
  }
});
