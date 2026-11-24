const base = document.body.dataset.base || "/";
const content = document.getElementById("content");

function url(path) {
  return path.startsWith("/") ? path : base + path;
}

async function post(path, data) {
  const res = await fetch(url(path), { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(data) });
  if (res.status === 401) location.href = "/login?next=" + encodeURIComponent(location.pathname + location.search);
  if (!res.ok) throw new Error((await res.text()).trim());
  return res.headers.get("content-type")?.includes("json") ? res.json() : {};
}

function report(err) {
  alert(err.message || err);
}

// Re-renders the page's content from the server. The fragment comes from templ, which
// escapes everything; DOMParser never runs scripts in it.
async function refresh(focus) {
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
function writeClipboard(text) {
  if (navigator.clipboard && window.isSecureContext) return navigator.clipboard.writeText(text).catch(() => {});
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

function copy(button, text, link) {
  writeClipboard(text);
  if (/^https?:\/\//.test(link || "")) window.open(link, "linkedin");
  button.textContent = "Copied ✓";
}

document.addEventListener("click", async (e) => {
  const b = e.target.closest("button");
  if (!b) return;
  try {
    if (b.dataset.mark) {
      await post("ui/mark", { id: +b.dataset.id, step: b.dataset.mark });
      refresh(b.dataset.next !== "0" && b.dataset.next);
    } else if ("copy" in b.dataset) {
      copy(b, b.dataset.copy, b.dataset.open);
    } else if (b.dataset.copyFrom) {
      copy(b, document.getElementById(b.dataset.copyFrom).value, b.dataset.open);
    } else if (b.dataset.post) {
      if (b.dataset.confirm && !confirm(b.dataset.confirm)) return;
      const body = JSON.parse(b.dataset.body || "{}");
      if (b.dataset.withFit) body.fit = document.getElementById(b.dataset.withFit)?.value || "";
      const res = await post(b.dataset.post, body);
      if (res.go) return void (location.href = res.go);
      refresh();
    }
  } catch (err) {
    report(err);
  }
});

// Pasting a profile link fills an empty name from it: /in/hannah-pryce-4a2b1c → Hannah Pryce.
document.addEventListener("input", (e) => {
  if (e.target.name !== "linkedin" || !e.target.form) return;
  const name = e.target.form.elements.name;
  const slug = (e.target.value.match(/linkedin\.com\/in\/([^/?#]+)/) || [])[1];
  if (!name || name.value || !slug) return;
  const words = decodeURIComponent(slug).split("-").filter((w) => w && !/\d/.test(w));
  name.value = words.map((w) => w[0].toUpperCase() + w.slice(1)).join(" ");
});

document.addEventListener("change", async (e) => {
  const f = e.target.closest("[data-field]");
  if (!f) return;
  try {
    await post("ui/update", { id: +f.dataset.id, field: f.dataset.field, value: f.value });
    if (f.dataset.refresh || f.dataset.field === "phone") refresh();
  } catch (err) {
    report(err);
  }
});

// Forms post their fields as JSON: numbers as numbers, a lone checkbox as true/false,
// checkboxes marked data-multi as a list of the checked values.
function formJSON(form) {
  const out = {};
  for (const el of form.elements) {
    if (!el.name || el.disabled) continue;
    if (el.type === "checkbox" && el.dataset.multi) {
      out[el.name] = out[el.name] || [];
      if (el.checked) out[el.name].push(el.value);
    } else if (el.type === "checkbox") {
      out[el.name] = el.checked;
    } else if (el.type === "number") {
      out[el.name] = el.value === "" ? 0 : +el.value;
    } else {
      out[el.name] = el.value;
    }
  }
  return out;
}

document.addEventListener("submit", async (e) => {
  const form = e.target.closest("form[data-form]");
  if (!form) return;
  e.preventDefault();
  const result = form.querySelector("[data-result]");
  const button = form.querySelector("[type=submit]");
  if (button) button.disabled = true;
  try {
    const res = await post(form.dataset.form, formJSON(form));
    if (res.go) return void (location.href = res.go);
    if (result) result.textContent = res.message || "Saved ✓";
    if (form.dataset.reset) {
      for (const el of form.elements) if (el.type !== "checkbox" && el.type !== "submit" && el.tagName !== "SELECT" && !el.closest("fieldset[data-keep]")) el.value = "";
      form.querySelector("input:not([type=hidden])")?.focus();
    }
    if (form.dataset.close) setTimeout(() => window.close(), 1200);
    if (form.dataset.refresh) setTimeout(() => refresh(), 600);
  } catch (err) {
    if (result) {
      result.textContent = err.message;
      result.style.color = "var(--stop)";
      setTimeout(() => (result.style.color = ""), 4000);
    } else report(err);
  } finally {
    if (button) button.disabled = false;
  }
});

// On Today: o opens the first card's profile and copies its note, a marks it added, s skips.
document.addEventListener("keydown", (e) => {
  if (e.metaKey || e.ctrlKey || e.altKey || /INPUT|TEXTAREA|SELECT/.test(document.activeElement?.tagName || "")) return;
  const first = content?.querySelector("[data-card]");
  const button = first?.querySelector(`[data-key="${e.key}"]`);
  if (button) {
    e.preventDefault();
    button.click();
  }
});

setInterval(() => {
  if (!/INPUT|TEXTAREA|SELECT/.test(document.activeElement?.tagName || "") && !document.querySelector("details[open] form")) refresh();
}, 60000);
