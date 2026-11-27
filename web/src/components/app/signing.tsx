// Signing an agreement: the document with the signer's own places marked on it, what
// they fill in, their typed name, and their agreement to sign electronically. The
// outsider's page (sign.tsx) and a member's (pages/Sign.tsx) both use it.
import { useEffect, useRef, useState } from "react";
import type { PDFPageProxy } from "pdfjs-dist";

export type Place = {
  id: number;
  page: number;
  kind: "signature" | "initials" | "name" | "date" | "text" | "tick";
  label: string;
  x: number;
  y: number;
  w: number;
  h: number;
};

const label = { signature: "Your signature", initials: "Your initials", name: "Your name", date: "The date you sign", text: "", tick: "" };

type Sheet = { page: PDFPageProxy; w: number; h: number };

// Document draws a PDF's pages with pdf.js, loaded on the page that needs it alone, and
// whatever goes over each page.
export function Document({ load, over }: { load: () => Promise<ArrayBuffer>; over?: (page: number) => React.ReactNode }) {
  const here = useRef<HTMLDivElement>(null);
  const [sheets, setSheets] = useState<Sheet[]>([]);
  const [problem, setProblem] = useState("");
  useEffect(() => {
    let gone = false;
    (async () => {
      const pdfjs = await import("pdfjs-dist");
      pdfjs.GlobalWorkerOptions.workerSrc = "/assets/js/pdf.worker.min.mjs";
      const pdf = await pdfjs.getDocument({ data: new Uint8Array(await load()) }).promise;
      const width = Math.min(here.current?.clientWidth || 640, 760);
      const out: Sheet[] = [];
      for (let n = 1; n <= pdf.numPages; n++) {
        const page = await pdf.getPage(n);
        const view = page.getViewport({ scale: 1 });
        out.push({ page, w: width, h: (width * view.height) / view.width });
      }
      if (!gone) setSheets(out);
    })().catch((err: unknown) => setProblem(err instanceof Error ? err.message : String(err)));
    return () => {
      gone = true;
    };
  }, [load]);
  if (problem) return <p className="font-mono text-xs text-[var(--stop)]">{problem}</p>;
  return (
    <div ref={here} className="flex flex-col items-center gap-6">
      {sheets.length === 0 && <p className="font-mono text-xs text-muted-foreground">The pages appear here.</p>}
      {sheets.map((s, i) => (
        <div
          key={i}
          data-page={i + 1}
          aria-label={`Page ${i + 1} of ${sheets.length}`}
          className="relative w-full overflow-hidden rounded-sm border border-border bg-card"
          style={{ maxWidth: s.w, aspectRatio: `${s.w} / ${s.h}`, boxShadow: "6px 6px 0 var(--ink)" }}
        >
          <Canvas sheet={s} />
          {over?.(i + 1)}
        </div>
      ))}
    </div>
  );
}

function Canvas({ sheet }: { sheet: Sheet }) {
  const ref = useRef<HTMLCanvasElement>(null);
  useEffect(() => {
    const canvas = ref.current;
    if (!canvas) return;
    const viewport = sheet.page.getViewport({ scale: (sheet.w / sheet.page.getViewport({ scale: 1 }).width) * devicePixelRatio });
    canvas.width = viewport.width;
    canvas.height = viewport.height;
    const task = sheet.page.render({ canvas, viewport });
    return () => task.cancel();
  }, [sheet]);
  return <canvas ref={ref} className="block w-full" />;
}

export type Signed = { typed: string; values: { field: number; value: string }[] };

// SignForm is the document, the signer's places on it, and the panel they sign from.
export function SignForm({
  places,
  load,
  sentName,
  consent,
  busy,
  error,
  onSign,
  onDecline,
}: {
  places: Place[];
  load: () => Promise<ArrayBuffer>;
  sentName: string;
  consent: string;
  busy: boolean;
  error: string;
  onSign: (s: Signed) => void;
  onDecline: (reason: string) => void;
}) {
  const [typed, setTyped] = useState(sentName);
  const [agreed, setAgreed] = useState(false);
  const [values, setValues] = useState<Record<number, string>>({});
  const [declining, setDeclining] = useState(false);
  const [reason, setReason] = useState("");
  const pages = [...new Set(places.map((p) => p.page))].sort((a, b) => a - b);
  const asked = places.filter((p) => p.kind === "text" || p.kind === "tick");
  return (
    <>
      <p className="text-sm text-muted-foreground">
        You have {places.length === 1 ? "1 place" : `${places.length} places`} to fill in{pages.length ? `, on page ${pages.join(", ")}` : ""}. Read every page;
        the whole document is below.
      </p>
      <Document
        load={load}
        over={(page) =>
          places
            .filter((p) => p.page === page)
            .map((p) => (
              <div
                key={p.id}
                title={p.label || label[p.kind]}
                className="absolute flex items-center overflow-hidden rounded px-1 text-[11px] font-semibold"
                style={{
                  left: `${p.x * 100}%`,
                  top: `${p.y * 100}%`,
                  width: `${p.w * 100}%`,
                  height: `${p.h * 100}%`,
                  border: "2px solid var(--coral)",
                  background: "#ff745214",
                }}
              >
                {p.kind === "signature" || p.kind === "name" ? (
                  typed ? (
                    <span className="font-heading italic">{typed}</span>
                  ) : (
                    label[p.kind]
                  )
                ) : p.kind === "tick" ? (
                  values[p.id] ? (
                    "✓"
                  ) : (
                    ""
                  )
                ) : p.kind === "text" ? (
                  values[p.id] || p.label
                ) : (
                  label[p.kind]
                )}
              </div>
            ))
        }
      />
      <section className="flex flex-col gap-3 panel p-4">
        {asked.map((p) => (
          <label key={p.id} className="flex flex-col gap-1 text-sm">
            {p.kind === "tick" ? (
              <span className="flex items-start gap-2">
                <input type="checkbox" checked={!!values[p.id]} onChange={(e) => setValues({ ...values, [p.id]: e.currentTarget.checked ? "yes" : "" })} />
                {p.label} <span className="font-mono text-xs text-muted-foreground">page {p.page}</span>
              </span>
            ) : (
              <>
                <span className="font-mono text-[11px] uppercase tracking-wider text-muted-foreground">
                  {p.label} · page {p.page}
                </span>
                <input
                  value={values[p.id] ?? ""}
                  maxLength={200}
                  required
                  onChange={(e) => setValues({ ...values, [p.id]: e.currentTarget.value })}
                  className="h-10 rounded-lg border border-input bg-muted px-3"
                />
              </>
            )}
          </label>
        ))}
        <label className="flex flex-col gap-1">
          <span className="font-mono text-[11px] uppercase tracking-wider text-muted-foreground">Type your full name</span>
          <input
            value={typed}
            maxLength={120}
            onChange={(e) => setTyped(e.currentTarget.value)}
            className="h-10 rounded-lg border border-input bg-muted px-3"
          />
        </label>
        <div className="flex flex-col gap-1">
          <span className="font-mono text-[11px] uppercase tracking-wider text-muted-foreground">How it will appear</span>
          <span className="border-b border-ink pb-1 font-heading text-3xl italic" style={{ maxWidth: 320 }}>
            {typed || " "}
          </span>
        </div>
        <label className="flex items-start gap-2 text-sm">
          <input type="checkbox" checked={agreed} onChange={(e) => setAgreed(e.currentTarget.checked)} className="mt-1" />
          <span>{consent}</span>
        </label>
        {error && (
          <p className="text-sm text-[var(--stop)]" role="alert">
            {error}
          </p>
        )}
        <div className="flex flex-wrap items-center gap-3">
          <button
            type="button"
            disabled={busy || !agreed || !typed.trim()}
            onClick={() => onSign({ typed, values: Object.entries(values).map(([field, value]) => ({ field: Number(field), value })) })}
            className="h-10 rounded-lg border-2 border-ink bg-primary px-4 font-semibold shadow-[2px_2px_0_var(--ink)] disabled:opacity-50"
          >
            Sign
          </button>
          <button type="button" onClick={() => setDeclining(!declining)} className="h-9 rounded-lg border-2 border-ink bg-card px-3 text-sm font-semibold">
            Decline…
          </button>
        </div>
        {declining && (
          <div className="flex flex-col gap-2">
            <textarea
              value={reason}
              maxLength={1000}
              onChange={(e) => setReason(e.currentTarget.value)}
              placeholder="Why, for whoever sent it (optional)"
              className="rounded-lg border border-input bg-muted p-2 text-sm"
              style={{ minHeight: 80 }}
            />
            <button
              type="button"
              disabled={busy}
              onClick={() => window.confirm("Decline to sign? It ends the agreement, and whoever sent it is told.") && onDecline(reason)}
              className="h-9 self-start rounded-lg border-2 bg-card px-3 text-sm font-semibold text-[var(--stop)]"
              style={{ borderColor: "var(--stop)" }}
            >
              Decline to sign
            </button>
          </div>
        )}
        <p className="font-mono text-xs text-muted-foreground">Nothing is signed until you press Sign. Declining ends it, and whoever sent it hears why.</p>
      </section>
    </>
  );
}
