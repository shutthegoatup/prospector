import { type PointerEvent as ReactPointerEvent, type ReactNode, useEffect, useRef, useState } from "react";
import { Link, useParams } from "@tanstack/react-router";
import type { components } from "@/api";
import { $api, fetchClient } from "@/client";
import { ActionButton, Eyebrow, Failed, Meta, PageHead, useRefresh, Whose } from "@/components/app";
import { Bar, Loading, Placeholders, Text } from "@/components/app/skeleton";
import { cn } from "@/lib/utils";
import { useSession } from "@/session";

type Field = components["schemas"]["AgreementFieldIn"];
type Kind = Field["kind"];
type Recipient = components["schemas"]["AgreementRecipient"];

// A signer's colour tells their fields from another's, by their place among the signers.
const colours = ["#ff7452", "#ffc233", "#35c58a", "#8f7bff", "#56c5f4", "#ff9f43"];

const kinds: { kind: Kind; label: string; size: [number, number] }[] = [
  { kind: "signature", label: "Signature", size: [0.3, 0.06] },
  { kind: "initials", label: "Initials", size: [0.09, 0.04] },
  { kind: "name", label: "Name", size: [0.26, 0.035] },
  { kind: "date", label: "Date signed", size: [0.18, 0.035] },
  { kind: "text", label: "Text", size: [0.26, 0.035] },
  { kind: "tick", label: "Tick box", size: [0.03, 0.022] },
];
const labelOf = (k: Kind) => kinds.find((x) => x.kind === k)?.label ?? k;
const clamp = (v: number, lo: number, hi: number) => Math.min(Math.max(v, lo), hi);
const field = "h-9 rounded-lg border border-input bg-muted px-2 text-sm";

type Signer = { id: number; name: string; colour: string };

export function PreparePage() {
  const { org, sales } = useSession();
  const { id } = useParams({ from: "/o/$org/agreements/$id/prepare" });
  const refresh = useRefresh();
  const path = { params: { path: { org, id } } };
  const agreement = $api.useQuery("get", "/o/{org}/api/v1/agreements/{id}", path);
  const recipients = $api.useQuery("get", "/o/{org}/api/v1/agreements/{id}/recipients", { params: { path: { org, id }, query: { limit: 200 } } });
  const stored = $api.useQuery("get", "/o/{org}/api/v1/agreements/{id}/fields", { params: { path: { org, id }, query: { limit: 200 } } });
  const add = $api.useMutation("post", "/o/{org}/api/v1/agreements/{id}/recipients", { onSuccess: refresh });
  const change = $api.useMutation("patch", "/o/{org}/api/v1/agreements/{id}/recipients/{recipient}", { onSuccess: refresh });
  const remove = $api.useMutation("delete", "/o/{org}/api/v1/agreements/{id}/recipients/{recipient}", { onSuccess: refresh });
  const order = $api.useMutation("patch", "/o/{org}/api/v1/agreements/{id}", { onSuccess: refresh });
  const [fields, setFields] = useState<Field[] | null>(null);
  const [picked, setPicked] = useState<number | null>(null);
  const [saved, setSaved] = useState("");
  const [saveError, setSaveError] = useState<{ detail?: string } | null>(null);
  const saving = useRef(Promise.resolve());

  useEffect(() => {
    if (stored.data) setFields(stored.data.items.map(({ recipient_id, page, kind, x, y, w, h }) => ({ recipient_id, page, kind, x, y, w, h })));
  }, [stored.data]);

  const a = agreement.data;
  const all = recipients.data?.items ?? [];
  const signers: Signer[] = all
    .filter((r) => r.role === "signer")
    .map((r, i) => ({ id: r.id, name: r.name, colour: colours[i % colours.length] ?? "#ffc233" }));
  const copies = all.filter((r) => r.role === "copy");
  const edit = sales && a?.status === "draft";
  const placing = signers.find((s) => s.id === picked) ?? signers[0];

  // save sends every field at once, one save after another, as the editor changes them.
  const save = (next: Field[]) => {
    setFields(next);
    setSaved("Saving…");
    saving.current = saving.current.then(async () => {
      const r = await fetchClient.PUT("/o/{org}/api/v1/agreements/{id}/fields", { params: { path: { org, id } }, body: { items: next } });
      if (r.error) {
        setSaved("");
        setSaveError(r.error);
        stored.refetch();
      } else {
        setSaved("Saved ✓");
        setSaveError(null);
      }
    });
  };

  const head = (
    <>
      <Link to="/o/$org/companies/$id" params={{ org, id: a?.company_id ?? 0 }} disabled={!a} className="font-mono text-xs text-mark">
        ← <Text value={a?.company} w="14ch" />
      </Link>
      <PageHead title="Prepare for signing">
        <Meta>
          <Text value={a && `${a.name} · ${a.file_name} · ${a.page_count} pages`} w="32ch" />
        </Meta>
      </PageHead>
      <Whose area="sales" work="Preparing agreements is" can={sales} />
    </>
  );
  if (agreement.error)
    return (
      <>
        {head}
        <Failed errors={[agreement.error]} />
      </>
    );
  // What follows depends on the agreement, so it appears below rather than pushing the head down.
  if (!a) return <Loading busy>{head}</Loading>;
  const known = recipients.data !== undefined || Boolean(recipients.error);
  const signed = new Set((fields ?? []).filter((f) => f.kind === "signature").map((f) => f.recipient_id));
  const missing = [
    ...(a.file_hash ? [] : ["Upload the PDF."]),
    ...(signers.length === 0 ? ["Add someone to sign it."] : []),
    ...signers.filter((s) => !signed.has(s.id)).map((s) => `Place a signature for ${s.name}.`),
  ];

  return (
    <>
      {head}
      {a.problem && (
        <p className="mb-4 text-sm text-[var(--stop)]" role="alert">
          {a.problem}
        </p>
      )}
      {(recipients.error || stored.error) && (
        <div className="mb-4">
          <Failed errors={[recipients.error, stored.error]} />
        </div>
      )}
      <div className="flex flex-wrap items-start gap-5">
        <div className="flex max-w-full flex-[1_1_240px] flex-col gap-4 md:max-w-[260px]">
          <section className="flex flex-col gap-2 panel p-3.5">
            <Eyebrow>Signers</Eyebrow>
            {known && signers.length === 0 && <Meta>Nobody yet.</Meta>}
            {!known && (
              <Placeholders className="flex flex-col gap-2">
                {[0, 1].map((i) => (
                  <SignerRow key={`loading-${i}`} edit={edit} />
                ))}
              </Placeholders>
            )}
            {signers.map((s, i) => {
              const r = all.find((x) => x.id === s.id) as Recipient;
              return (
                <SignerRow key={s.id} edit={edit} s={s} n={i + 1} r={r} placing={placing?.id === s.id} onPlace={() => setPicked(s.id)}>
                  {edit && (
                    <div className="flex flex-wrap gap-1.5">
                      {i > 0 && (
                        <ActionButton
                          quiet
                          size="xs"
                          aria-label={`Move ${s.name} up`}
                          onClick={() =>
                            change.mutate({
                              params: { path: { org, id, recipient: s.id } },
                              body: { position: all.find((x) => x.id === signers[i - 1]?.id)?.position },
                            })
                          }
                        >
                          ↑
                        </ActionButton>
                      )}
                      <ActionButton quiet size="xs" onClick={() => change.mutate({ params: { path: { org, id, recipient: s.id } }, body: { role: "copy" } })}>
                        Copy only
                      </ActionButton>
                      <ActionButton
                        quiet
                        size="xs"
                        onClick={() =>
                          window.confirm(`Take ${s.name} off, with their fields?`) && remove.mutate({ params: { path: { org, id, recipient: s.id } } })
                        }
                      >
                        Remove
                      </ActionButton>
                    </div>
                  )}
                </SignerRow>
              );
            })}
            {edit ? (
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={a.in_order} onChange={(e) => order.mutate({ ...path, body: { in_order: e.currentTarget.checked } })} />
                Sign in this order
              </label>
            ) : (
              <Meta>{a.in_order ? "They sign in this order." : "They can sign in any order."}</Meta>
            )}
          </section>
          <section className="flex flex-col gap-2 panel p-3.5">
            <Eyebrow>Gets a copy</Eyebrow>
            {known && copies.length === 0 && <Meta>Nobody. Everyone who signs gets one anyway.</Meta>}
            {!known && (
              <Placeholders>
                <CopyRow />
              </Placeholders>
            )}
            {copies.map((r) => (
              <CopyRow key={r.id} r={r}>
                {edit && (
                  <span className="flex gap-1.5">
                    <ActionButton quiet size="xs" onClick={() => change.mutate({ params: { path: { org, id, recipient: r.id } }, body: { role: "signer" } })}>
                      Signs
                    </ActionButton>
                    <ActionButton quiet size="xs" onClick={() => remove.mutate({ params: { path: { org, id, recipient: r.id } } })}>
                      Remove
                    </ActionButton>
                  </span>
                )}
              </CopyRow>
            ))}
          </section>
          {edit && (
            <section className="flex flex-col gap-2 panel p-3.5">
              <Eyebrow>Add someone</Eyebrow>
              <AddForm label={`Someone at ${a.company}`} options={a.people} onAdd={(who, role) => add.mutate({ ...path, body: { role, contact: who } })} />
              <AddForm label="Someone in your organisation" options={a.members} onAdd={(who, role) => add.mutate({ ...path, body: { role, member: who } })} />
              <Failed errors={[add.error, change.error, remove.error, order.error]} />
              <Meta>People need one email address and can't be on the never-contact list. Nobody is emailed yet.</Meta>
            </section>
          )}
          <section className="flex flex-col gap-1.5 panel p-3.5">
            <Eyebrow>Before it can be sent</Eyebrow>
            {known && fields && missing.length === 0 && <p className="font-mono text-xs text-[var(--ok)]">✓ Every signer has a signature field.</p>}
            {known && fields && missing.map((m) => <Meta key={m}>· {m}</Meta>)}
            <Meta>Sending it for signature comes in the next release. Until then nothing here leaves Prospector.</Meta>
          </section>
        </div>
        <div className="flex min-w-0 flex-[999_1_560px] flex-col gap-3">
          {known && edit && signers.length > 0 && (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-mono text-xs text-muted-foreground">Place for {placing?.name}:</span>
                {kinds.map((k) => (
                  <ActionButton
                    key={k.kind}
                    quiet
                    size="xs"
                    draggable
                    onDragStart={(e) => e.dataTransfer.setData("text/x-field", k.kind)}
                    onClick={() => fields && placing && save([...fields, put(k.kind, placing.id, pageInView(), 0.5, 0.5)])}
                  >
                    {k.label}
                  </ActionButton>
                ))}
                <span className="font-mono text-xs text-muted-foreground">{saved}</span>
              </div>
              <Meta>Drag one onto a page, or click it to put it on the page in view. Drag a field to move it, its corner to size it.</Meta>
              <Failed errors={[saveError]} />
            </>
          )}
          {known && edit && signers.length === 0 && <Meta>Add someone to sign it, then place their fields on the pages.</Meta>}
          {known && a.file_hash && fields && (
            <Pages
              id={id}
              hash={a.file_hash}
              fields={fields}
              signers={signers}
              edit={edit}
              onChange={save}
              onDrop={(kind, page, x, y) => placing && save([...fields, put(kind, placing.id, page, x, y)])}
            />
          )}
        </div>
      </div>
    </>
  );
}

// SignerRow is one signer, or their outline while the recipients load.
function SignerRow({
  edit,
  s,
  n,
  r,
  placing,
  onPlace,
  children,
}: {
  edit: boolean;
  s?: Signer;
  n?: number;
  r?: Recipient;
  placing?: boolean;
  onPlace?: () => void;
  children?: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1.5 rounded-lg border border-border p-2 text-sm">
      <label className="flex items-center gap-2">
        {edit && <input type="radio" name="placing" checked={placing ?? false} disabled={!s} onChange={onPlace} />}
        <span className={cn("shrink-0 rounded-sm", !s && "motion-safe:animate-pulse bg-accent")} style={{ width: 10, height: 10, background: s?.colour }} />
        <b>{s ? `${n} · ${s.name}` : <Bar w="12ch" />}</b>
      </label>
      <Meta>
        <Text value={r && partyLine(r)} w="20ch" />
      </Meta>
      {children}
    </div>
  );
}

function CopyRow({ r, children }: { r?: Recipient; children?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
      <span>
        <Text value={r?.name} w="10ch" />{" "}
        <span className="font-mono text-xs text-muted-foreground">
          <Text value={r && partyLine(r)} w="18ch" />
        </span>
      </span>
      {children}
    </div>
  );
}

function partyLine(r: Recipient) {
  return r.member_id ? `${r.email} · member` : r.email;
}

// put is a new field for a signer, centred on a point of a page and kept on it.
function put(kind: Kind, recipient: number, page: number, cx: number, cy: number): Field {
  const [w, h] = kinds.find((k) => k.kind === kind)?.size ?? [0.2, 0.04];
  return { recipient_id: recipient, page, kind, x: clamp(cx - w / 2, 0, 1 - w), y: clamp(cy - h / 2, 0, 1 - h), w, h };
}

// pageInView is the page most of the window shows.
function pageInView() {
  let best = 1;
  let most = -1;
  for (const sheet of document.querySelectorAll<HTMLElement>("[data-page]")) {
    const r = sheet.getBoundingClientRect();
    const seen = Math.min(r.bottom, innerHeight) - Math.max(r.top, 0);
    if (seen > most) [best, most] = [Number(sheet.dataset.page), seen];
  }
  return best;
}

function AddForm({
  label,
  options,
  onAdd,
}: {
  label: string;
  options: components["schemas"]["Invitable"][];
  onAdd: (who: number, role: "signer" | "copy") => void;
}) {
  const [who, setWho] = useState("");
  const [role, setRole] = useState<"signer" | "copy">("signer");
  return (
    <form
      className="flex flex-col gap-2"
      onSubmit={(e) => {
        e.preventDefault();
        if (who) onAdd(Number(who), role);
        setWho("");
      }}
    >
      <select value={who} required aria-label={label} onChange={(e) => setWho(e.currentTarget.value)} className={field}>
        <option value="">{label}</option>
        {options.map((o) => (
          <option key={o.id} value={o.id}>
            {o.name} · {o.email}
          </option>
        ))}
      </select>
      <div className="flex items-center gap-2">
        <select value={role} aria-label="What they do" onChange={(e) => setRole(e.currentTarget.value as "signer" | "copy")} className={field}>
          <option value="signer">signs</option>
          <option value="copy">gets a copy</option>
        </select>
        <ActionButton quiet size="sm" type="submit">
          Add
        </ActionButton>
      </div>
    </form>
  );
}

// Pages draws the document's pages with pdf.js, loaded on this page alone, and each
// signer's fields over them.
function Pages({
  id,
  hash,
  fields,
  signers,
  edit,
  onChange,
  onDrop,
}: {
  id: number;
  hash: string;
  fields: Field[];
  signers: Signer[];
  edit: boolean;
  onChange: (f: Field[]) => void;
  onDrop: (kind: Kind, page: number, x: number, y: number) => void;
}) {
  const { org } = useSession();
  const here = useRef<HTMLDivElement>(null);
  const [sizes, setSizes] = useState<Sheet[]>([]);
  const [problem, setProblem] = useState("");

  useEffect(() => {
    let gone = false;
    (async () => {
      const pdfjs = await import("pdfjs-dist");
      pdfjs.GlobalWorkerOptions.workerSrc = "/assets/js/pdf.worker.min.mjs";
      const doc = await fetchClient.GET("/o/{org}/api/v1/agreements/{id}/document", { params: { path: { org, id } }, parseAs: "arrayBuffer" });
      if (doc.error || !doc.data) throw new Error("The document couldn't be fetched.");
      const pdf = await pdfjs.getDocument({ data: new Uint8Array(doc.data) }).promise;
      const width = Math.min(here.current?.clientWidth || 640, 760);
      const pages: Sheet[] = [];
      for (let n = 1; n <= pdf.numPages; n++) {
        const page = await pdf.getPage(n);
        const view = page.getViewport({ scale: 1 });
        pages.push({ page, w: width, h: (width * view.height) / view.width });
      }
      if (!gone) setSizes(pages);
    })().catch((err: unknown) => setProblem(err instanceof Error ? err.message : String(err)));
    return () => {
      gone = true;
    };
  }, [org, id, hash]);

  if (problem) return <Meta>{problem}</Meta>;
  return (
    <div ref={here} className="flex flex-col items-center gap-6">
      {sizes.length === 0 && <Meta>The pages appear here.</Meta>}
      {sizes.map((s, i) => (
        <div
          key={i}
          data-page={i + 1}
          aria-label={`Page ${i + 1} of ${sizes.length}`}
          className="relative w-full overflow-hidden rounded-sm border border-border bg-card"
          style={{ maxWidth: s.w, aspectRatio: `${s.w} / ${s.h}`, boxShadow: "8px 8px 0 var(--ink)" }}
          onDragOver={(e) => edit && e.preventDefault()}
          onDrop={(e) => {
            const kind = e.dataTransfer.getData("text/x-field") as Kind | "";
            if (!edit || !kind) return;
            e.preventDefault();
            const r = e.currentTarget.getBoundingClientRect();
            onDrop(kind, i + 1, (e.clientX - r.left) / r.width, (e.clientY - r.top) / r.height);
          }}
        >
          <Canvas sheet={s} />
          {fields.map((f, n) =>
            f.page === i + 1 ? (
              <FieldBox
                key={n}
                f={f}
                signer={signers.find((x) => x.id === f.recipient_id)}
                edit={edit}
                onMove={(moved) => onChange(fields.map((g, m) => (m === n ? moved : g)))}
                onRemove={() => onChange(fields.filter((_, m) => m !== n))}
              />
            ) : null,
          )}
        </div>
      ))}
    </div>
  );
}

type Sheet = { page: import("pdfjs-dist").PDFPageProxy; w: number; h: number };

// Canvas draws one page, at the width it is shown and the screen's pixel density.
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

// FieldBox is one field over its page: dragged to move, by its corner to size, saved on release.
function FieldBox({ f, signer, edit, onMove, onRemove }: { f: Field; signer?: Signer; edit: boolean; onMove: (f: Field) => void; onRemove: () => void }) {
  const [live, setLive] = useState(f);
  useEffect(() => setLive(f), [f]);
  const colour = signer?.colour ?? "#b3a78e";
  const start = (e: ReactPointerEvent<HTMLElement>, sizing: boolean) => {
    if (!edit) return;
    e.stopPropagation();
    const area = (e.currentTarget.closest("[data-page]") as HTMLElement).getBoundingClientRect();
    const from = { x: e.clientX, y: e.clientY, f: live };
    let now = live;
    const move = (m: PointerEvent) => {
      const dx = (m.clientX - from.x) / area.width;
      const dy = (m.clientY - from.y) / area.height;
      now = sizing
        ? { ...from.f, w: clamp(from.f.w + dx, 0.02, 1 - from.f.x), h: clamp(from.f.h + dy, 0.015, 1 - from.f.y) }
        : { ...from.f, x: clamp(from.f.x + dx, 0, 1 - from.f.w), y: clamp(from.f.y + dy, 0, 1 - from.f.h) };
      setLive(now);
    };
    const up = () => {
      removeEventListener("pointermove", move);
      removeEventListener("pointerup", up);
      if (now !== from.f) onMove(now);
    };
    addEventListener("pointermove", move);
    addEventListener("pointerup", up);
  };
  return (
    <div
      title={`${labelOf(f.kind)} for ${signer?.name ?? "someone no longer signing"}`}
      onPointerDown={(e) => start(e, false)}
      className="absolute overflow-hidden whitespace-nowrap rounded px-1 text-[11px] font-semibold"
      style={{
        left: `${live.x * 100}%`,
        top: `${live.y * 100}%`,
        width: `${live.w * 100}%`,
        height: `${live.h * 100}%`,
        border: `2px dashed ${colour}`,
        background: colour + "22",
        cursor: edit ? "move" : "default",
        touchAction: "none",
      }}
    >
      {f.kind === "tick" ? "" : `${labelOf(f.kind)} · ${(signer?.name ?? "").split(" ")[0]}`}
      {edit && (
        <>
          <button
            type="button"
            aria-label={`Remove this ${labelOf(f.kind).toLowerCase()} field`}
            onPointerDown={(e) => e.stopPropagation()}
            onClick={onRemove}
            className="absolute cursor-pointer bg-transparent text-[13px]"
            style={{ right: 1, top: -2, border: 0 }}
          >
            ×
          </button>
          <span
            onPointerDown={(e) => start(e, true)}
            className="absolute"
            style={{ right: 0, bottom: 0, width: 10, height: 10, background: colour, cursor: "nwse-resize" }}
          />
        </>
      )}
    </div>
  );
}
