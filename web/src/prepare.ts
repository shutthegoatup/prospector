// The field editor on an agreement's Prepare page: draws the PDF's pages and lets a
// member place each signer's fields on them. Built to assets/js/prepare.js, a bundle of
// its own so only this page loads pdf.js.
import createClient from "openapi-fetch";
import { GlobalWorkerOptions, getDocument, type PDFDocumentProxy } from "pdfjs-dist";
import type { components, paths } from "./api";
import { problemText } from "./problem.ts";

type Field = components["schemas"]["AgreementFieldIn"];
type Kind = Field["kind"];

GlobalWorkerOptions.workerSrc = "/assets/js/pdf.worker.min.mjs";

const base = document.body.dataset.base || "/";
const org = base.match(/^\/o\/([^/]+)\//)?.[1] ?? "";
const api = createClient<paths>({ baseUrl: location.origin });

// A field's size when it is first put down, as fractions of the page.
const sizes: Record<Kind, [number, number]> = {
  signature: [0.3, 0.06],
  initials: [0.09, 0.04],
  name: [0.26, 0.035],
  date: [0.18, 0.035],
  text: [0.26, 0.035],
  tick: [0.03, 0.022],
};
const labels: Record<Kind, string> = { signature: "Signature", initials: "Initials", name: "Name", date: "Date signed", text: "Text", tick: "Tick" };

function answered<T>(r: { data?: T; error?: unknown; response: Response }): T {
  if (r.response.status === 401) location.href = "/login?next=" + encodeURIComponent(location.pathname);
  if (r.error !== undefined || !r.response.ok) throw new Error(problemText(r.error, r.response.statusText, r.response.status));
  return r.data as T;
}

// The document is fetched once per page visit, and kept while the page re-renders.
let loaded: { hash: string; pdf: PDFDocumentProxy } | undefined;

async function documentFor(id: number, hash: string): Promise<PDFDocumentProxy> {
  if (loaded?.hash === hash) return loaded.pdf;
  const bytes = answered(await api.GET("/o/{org}/api/v1/agreements/{id}/document", { params: { path: { org, id } }, parseAs: "arrayBuffer" }));
  const pdf = await getDocument({ data: new Uint8Array(bytes), isEvalSupported: false }).promise;
  loaded = { hash, pdf };
  return pdf;
}

const clamp = (v: number, lo: number, hi: number) => Math.min(Math.max(v, lo), hi);

class Editor {
  fields: Field[] = [];
  layers: HTMLElement[] = [];
  saving = Promise.resolve();
  readonly id: number;
  readonly edit: boolean;

  constructor(readonly root: HTMLElement) {
    this.id = +(root.dataset.editor || 0);
    this.edit = "edit" in root.dataset;
  }

  signer(): number | undefined {
    const r = document.querySelector<HTMLInputElement>("input[name=placing]:checked");
    return r ? +r.value : undefined;
  }

  // colourOf is a signer's colour and name, from the swatch beside them, which every
  // reader of the page has; only editors get the radio that picks them.
  colourOf(recipient: number): { colour: string; name: string } {
    const s = document.querySelector<HTMLElement>(`[data-signer="${recipient}"]`);
    return { colour: s?.dataset.colour || "#b3a78e", name: s?.dataset.name || "" };
  }

  async start(): Promise<void> {
    const here = this.root.querySelector<HTMLElement>("[data-pages-here]");
    if (!here || !this.root.dataset.hash) return;
    const [pdf, fields] = await Promise.all([
      documentFor(this.id, this.root.dataset.hash),
      // 200 is as many as a draft holds, so one page is all of them.
      api.GET("/o/{org}/api/v1/agreements/{id}/fields", { params: { path: { org, id: this.id }, query: { limit: 200 } } }).then(answered),
    ]);
    this.fields = fields.items.map(({ recipient_id, page, kind, x, y, w, h }) => ({ recipient_id, page, kind, x, y, w, h }));
    here.replaceChildren();
    const width = Math.min(here.clientWidth || 640, 760);
    for (let n = 1; n <= pdf.numPages; n++) {
      const page = await pdf.getPage(n);
      const scale = width / page.getViewport({ scale: 1 }).width;
      const view = page.getViewport({ scale: scale * devicePixelRatio });
      const canvas = document.createElement("canvas");
      canvas.width = view.width;
      canvas.height = view.height;
      canvas.style.width = "100%";
      canvas.style.display = "block";
      const sheet = document.createElement("div");
      sheet.className = "relative w-full overflow-hidden rounded-sm border border-border bg-card";
      sheet.style.maxWidth = width + "px";
      sheet.style.boxShadow = "8px 8px 0 var(--ink)";
      sheet.dataset.page = String(n);
      sheet.setAttribute("aria-label", `Page ${n} of ${pdf.numPages}`);
      const layer = document.createElement("div");
      layer.style.position = "absolute";
      layer.style.inset = "0";
      sheet.append(canvas, layer);
      here.append(sheet);
      this.layers[n] = layer;
      await page.render({ canvasContext: canvas.getContext("2d")!, viewport: view }).promise;
      if (this.edit) this.dropOnto(layer, n);
    }
    this.draw();
  }

  draw(): void {
    for (const layer of this.layers) layer?.replaceChildren();
    this.fields.forEach((f, i) => {
      const layer = this.layers[f.page];
      if (!layer) return;
      const who = this.colourOf(f.recipient_id);
      const box = document.createElement("div");
      Object.assign(box.style, {
        position: "absolute",
        left: f.x * 100 + "%",
        top: f.y * 100 + "%",
        width: f.w * 100 + "%",
        height: f.h * 100 + "%",
        border: `2px dashed ${who.colour}`,
        background: who.colour + "22",
        borderRadius: "4px",
        fontSize: "11px",
        fontWeight: "600",
        padding: "0 4px",
        overflow: "hidden",
        whiteSpace: "nowrap",
        cursor: this.edit ? "move" : "default",
        touchAction: "none",
      });
      box.textContent = f.kind === "tick" ? "" : `${labels[f.kind]} · ${who.name.split(" ")[0]}`;
      box.title = `${labels[f.kind]} for ${who.name}`;
      if (this.edit) {
        const remove = document.createElement("button");
        remove.type = "button";
        remove.textContent = "×";
        remove.setAttribute("aria-label", `Remove this ${labels[f.kind].toLowerCase()} field`);
        Object.assign(remove.style, { position: "absolute", right: "1px", top: "-2px", background: "none", border: "0", cursor: "pointer", fontSize: "13px" });
        remove.addEventListener("pointerdown", (e) => e.stopPropagation());
        remove.addEventListener("click", () => {
          this.fields.splice(i, 1);
          this.changed();
        });
        const corner = document.createElement("span");
        Object.assign(corner.style, { position: "absolute", right: "0", bottom: "0", width: "10px", height: "10px", background: who.colour, cursor: "nwse-resize" });
        box.append(remove, corner);
        this.dragging(box, corner, f);
      }
      layer.append(box);
    });
  }

  // dragging moves a field with the pointer, or sizes it from its corner, and saves on release.
  dragging(box: HTMLElement, corner: HTMLElement, f: Field): void {
    box.addEventListener("pointerdown", (e) => {
      const layer = box.parentElement!;
      const area = layer.getBoundingClientRect();
      const sizing = e.target === corner;
      const start = { x: e.clientX, y: e.clientY, f: { ...f } };
      box.setPointerCapture(e.pointerId);
      const move = (m: PointerEvent) => {
        const dx = (m.clientX - start.x) / area.width;
        const dy = (m.clientY - start.y) / area.height;
        if (sizing) {
          f.w = clamp(start.f.w + dx, 0.02, 1 - f.x);
          f.h = clamp(start.f.h + dy, 0.015, 1 - f.y);
        } else {
          f.x = clamp(start.f.x + dx, 0, 1 - f.w);
          f.y = clamp(start.f.y + dy, 0, 1 - f.h);
        }
        Object.assign(box.style, { left: f.x * 100 + "%", top: f.y * 100 + "%", width: f.w * 100 + "%", height: f.h * 100 + "%" });
      };
      const up = () => {
        box.removeEventListener("pointermove", move);
        box.removeEventListener("pointerup", up);
        if (start.f.x !== f.x || start.f.y !== f.y || start.f.w !== f.w || start.f.h !== f.h) this.changed();
      };
      box.addEventListener("pointermove", move);
      box.addEventListener("pointerup", up);
    });
  }

  dropOnto(layer: HTMLElement, page: number): void {
    layer.addEventListener("dragover", (e) => e.preventDefault());
    layer.addEventListener("drop", (e) => {
      e.preventDefault();
      const kind = e.dataTransfer?.getData("text/x-field") as Kind | "";
      if (!kind) return;
      const area = layer.getBoundingClientRect();
      this.place(kind, page, (e.clientX - area.left) / area.width, (e.clientY - area.top) / area.height);
    });
  }

  // place puts a field for the signer picked down at a point on a page, kept on the page.
  place(kind: Kind, page: number, cx: number, cy: number): void {
    const who = this.signer();
    if (!who) return alert("Pick who it is for first.");
    const [w, h] = sizes[kind];
    this.fields.push({ recipient_id: who, page, kind, x: clamp(cx - w / 2, 0, 1 - w), y: clamp(cy - h / 2, 0, 1 - h), w, h });
    this.changed();
  }

  // inView is the page most of the window shows.
  inView(): number {
    let best = 1,
      most = -1;
    this.layers.forEach((layer, n) => {
      if (!layer) return;
      const r = layer.getBoundingClientRect();
      const seen = Math.min(r.bottom, innerHeight) - Math.max(r.top, 0);
      if (seen > most) [best, most] = [n, seen];
    });
    return best;
  }

  // missing says again, as fields come and go, which signers still have no signature.
  // The page drew it from the server; only the signature lines change here.
  missing(): void {
    const list = document.querySelector<HTMLElement>("[data-missing]");
    if (!list) return;
    const signed = new Set(this.fields.filter((f) => f.kind === "signature").map((f) => f.recipient_id));
    const lines = Array.from(document.querySelectorAll<HTMLElement>("[data-signer]"))
      .filter((s) => !signed.has(+(s.dataset.signer || 0)))
      .map((s) => `· Place a signature for ${s.dataset.name}.`);
    const line = (text: string, ok = false) => {
      const p = document.createElement("p");
      p.className = "font-mono text-xs " + (ok ? "text-[var(--ok)]" : "text-muted-foreground");
      p.textContent = text;
      return p;
    };
    list.replaceChildren(...(lines.length ? lines.map((l) => line(l)) : [line("✓ Every signer has a signature field.", true)]));
  }

  changed(): void {
    this.draw();
    this.missing();
    const shown = this.root.querySelector<HTMLElement>("[data-saved]");
    if (shown) shown.textContent = "Saving…";
    const items = this.fields.map((f) => ({ ...f }));
    this.saving = this.saving
      .then(async () => {
        answered(await api.PUT("/o/{org}/api/v1/agreements/{id}/fields", { params: { path: { org, id: this.id } }, body: { items } }));
        if (shown) shown.textContent = "Saved ✓";
      })
      .catch((err: unknown) => {
        if (shown) shown.textContent = "";
        alert(err instanceof Error ? err.message : String(err));
        return this.start();
      });
  }
}

let editor: Editor | undefined;

function mount(): void {
  const root = document.querySelector<HTMLElement>("[data-editor]");
  if (!root || root === editor?.root) return;
  editor = new Editor(root);
  editor.start().catch((err: unknown) => {
    const here = root.querySelector("[data-pages-here]");
    if (here) here.textContent = err instanceof Error ? err.message : String(err);
  });
}

document.addEventListener("click", (e) => {
  const b = (e.target as Element).closest<HTMLElement>("[data-palette] [data-kind]");
  if (b && editor) editor.place(b.dataset.kind as Kind, editor.inView(), 0.5, 0.5);
});

document.addEventListener("dragstart", (e) => {
  const b = (e.target as Element).closest<HTMLElement>("[data-palette] [data-kind]");
  if (b) e.dataTransfer?.setData("text/x-field", b.dataset.kind || "");
});

document.addEventListener("change", async (e) => {
  const box = (e.target as Element).closest<HTMLInputElement>("input[data-in-order]");
  if (!box) return;
  try {
    answered(await api.PATCH("/o/{org}/api/v1/agreements/{id}", { params: { path: { org, id: +(box.dataset.inOrder || 0) } }, body: { in_order: box.checked } }));
  } catch (err) {
    box.checked = !box.checked;
    alert(err instanceof Error ? err.message : String(err));
  }
});

// The page script re-renders the content after a signer is added or moved; the editor
// is drawn again on what replaced it.
new MutationObserver(mount).observe(document.getElementById("content") || document.body, { childList: true });
mount();
