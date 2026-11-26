// Built from web/src/app.ts by `npm run build` in web/. Don't edit by hand.
"use strict";
(() => {
  // src/app.ts
  async function send(path, data) {
    const res = await fetch(path, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(data) });
    const type = res.headers.get("content-type") || "";
    if (!res.ok) throw new Error(type.includes("problem+json") ? (await res.json()).detail || res.statusText : (await res.text()).trim());
    return type.includes("json") ? await res.json() : {};
  }
  function formJSON(form) {
    const out = {};
    for (const el of Array.from(form.elements)) {
      if (el.name && !el.disabled) out[el.name] = el.value;
    }
    return out;
  }
  function next(form, res) {
    if (typeof res.go === "string") return res.go;
    const go = form.dataset.go;
    if (!go) return void 0;
    const to = go.replace(/\{(\w+)\}/g, (_, k) => encodeURIComponent(String(res[k] ?? "")));
    return to.includes("//") ? void 0 : to;
  }
  document.addEventListener("submit", async (e) => {
    const form = e.target.closest("form[data-form]");
    if (!form) return;
    e.preventDefault();
    const result = form.querySelector("[data-result]");
    const button = form.querySelector("[type=submit]");
    if (button) button.disabled = true;
    try {
      const res = await send(form.dataset.form || "", formJSON(form));
      const to = next(form, res);
      if (to) return void (location.href = to);
      if (result) result.textContent = typeof res.message === "string" ? res.message : "Saved \u2713";
    } catch (err) {
      const text = err instanceof Error ? err.message : String(err);
      if (result) result.textContent = text;
      else alert(text);
    } finally {
      if (button) button.disabled = false;
    }
  });
})();
