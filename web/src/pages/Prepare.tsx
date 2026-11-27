import { type PointerEvent as ReactPointerEvent, type ReactNode, useEffect, useRef, useState } from "react";
import { defineMessage, defineMessages, useIntl } from "react-intl";
import { Link, useParams } from "@tanstack/react-router";
import type { components } from "@/api";
import { $api, fetchClient } from "@/client";
import { ActionButton, Eyebrow, Failed, Meta, PageHead, useRefresh, Whose } from "@/components/app";
import { Bar, Loading, Placeholders, Text } from "@/components/app/skeleton";
import { intl } from "@/i18n";
import { useKeyedState } from "@/lib/keyed";
import { cn } from "@/lib/utils";
import { useSession } from "@/session";

type Field = components["schemas"]["AgreementFieldIn"];
type Kind = Field["kind"];
type Recipient = components["schemas"]["AgreementRecipient"];

// A signer's colour tells their fields from another's, by their place among the signers.
const colours = ["#ff7452", "#ffc233", "#35c58a", "#8f7bff", "#56c5f4", "#ff9f43"];

const kinds: { kind: Kind; size: [number, number] }[] = [
  { kind: "signature", size: [0.3, 0.06] },
  { kind: "initials", size: [0.09, 0.04] },
  { kind: "name", size: [0.26, 0.035] },
  { kind: "date", size: [0.18, 0.035] },
  { kind: "text", size: [0.26, 0.035] },
  { kind: "tick", size: [0.03, 0.022] },
];

const kindNames = defineMessages({
  signature: { defaultMessage: "Signature", id: "yfmW86", description: "A kind of field placed on an agreement's page: where a signer signs" },
  initials: { defaultMessage: "Initials", id: "WacP+p", description: "A kind of field placed on an agreement's page: where a signer initials" },
  name: { defaultMessage: "Name", id: "QwzLyU", description: "A kind of field placed on an agreement's page: where the signer's name goes" },
  date: { defaultMessage: "Date signed", id: "/7/ivi", description: "A kind of field placed on an agreement's page: the date it is signed" },
  text: { defaultMessage: "Text", id: "1Nblfr", description: "A kind of field placed on an agreement's page: text the signer types" },
  tick: { defaultMessage: "Tick box", id: "KD99+8", description: "A kind of field placed on an agreement's page: a box the signer ticks" },
});

const m = defineMessages({
  title: { defaultMessage: "Prepare for signing", id: "iDuNyR", description: "Title of the page preparing an agreement to be signed" },
  whose: {
    defaultMessage: "Preparing agreements is sales work, so here you read it.",
    id: "KGKrVx",
    description: "On the page preparing an agreement, to a member without sales work: why they can read it but not act",
  },
  upload: { defaultMessage: "Upload the PDF.", id: "zFcu5E", description: "In the list of what an agreement needs before it can be sent" },
  addSigner: { defaultMessage: "Add someone to sign it.", id: "epxAxw", description: "In the list of what an agreement needs before it can be sent" },
  signers: { defaultMessage: "Signers", id: "YQSb3c", description: "Heading of the people who sign an agreement" },
  nobodyYet: { defaultMessage: "Nobody yet.", id: "NwGjP1", description: "Under Signers when nobody signs the agreement yet" },
  copyOnly: { defaultMessage: "Copy only", id: "+UBn4I", description: "Button making a signer someone who only gets a copy of the agreement" },
  remove: { defaultMessage: "Remove", id: "qMSVO6", description: "Button taking someone off an agreement" },
  signInOrder: { defaultMessage: "Sign in this order", id: "KHhvY5", description: "Checkbox: the signers sign one after another, in the order listed" },
  inOrder: {
    defaultMessage: "They sign in this order.",
    id: "ktFVs8",
    description: "Under Signers, read only: they sign one after another, in the order listed",
  },
  anyOrder: { defaultMessage: "They can sign in any order.", id: "jS3GJQ", description: "Under Signers, read only: they need not sign in the order listed" },
  getsCopy: { defaultMessage: "Gets a copy", id: "s/p1B1", description: "Heading of the people who get a copy of an agreement without signing it" },
  noCopies: {
    defaultMessage: "Nobody. Everyone who signs gets one anyway.",
    id: "yWoXW0",
    description: "Under Gets a copy when nobody only gets a copy",
  },
  signs: { defaultMessage: "Signs", id: "4vytQM", description: "Button making someone who gets a copy of an agreement one of its signers" },
  addSomeone: { defaultMessage: "Add someone", id: "uzmeyM", description: "Heading of the forms adding a signer or someone to get a copy" },
  inYourOrg: { defaultMessage: "Someone in your organisation", id: "FQQ+lf", description: "Choice of a member of the organisation to add to an agreement" },
  whoCanBeAdded: {
    defaultMessage: "People need one email address and can't be on the never-contact list. Nobody is emailed yet.",
    id: "KAgz+u",
    description: "Under the forms adding someone to an agreement",
  },
  beforeSent: { defaultMessage: "Before it can be sent", id: "OdIgb0", description: "Heading of the list of what an agreement needs before it can be sent" },
  ready: { defaultMessage: "✓ Every signer has a signature field.", id: "peu3qk", description: "When an agreement has all it needs before it can be sent" },
  saving: { defaultMessage: "Saving…", id: "wjb+Jf", description: "While the fields placed on an agreement are being saved" },
  saved: { defaultMessage: "Saved ✓", id: "zByJ0E", description: "Once the fields placed on an agreement are saved" },
  howToPlace: {
    defaultMessage: "Drag one onto a page, or click it to put it on the page in view. Drag a field to move it, its corner to size it.",
    id: "AEtvaQ",
    description: "Under the buttons for the kinds of field to place on an agreement's pages",
  },
  addThenPlace: {
    defaultMessage: "Add someone to sign it, then place their fields on the pages.",
    id: "YBb7M+",
    description: "Where the fields are placed, when nobody signs the agreement yet",
  },
  whatTheyDo: {
    defaultMessage: "What they do",
    id: "xR+kpe",
    description: "What a screen reader calls the choice of whether someone added signs or gets a copy",
  },
  roleSigner: { defaultMessage: "signs", id: "2jQ/sM", description: "Choice when adding someone to an agreement: they sign it" },
  roleCopy: { defaultMessage: "gets a copy", id: "q45QDW", description: "Choice when adding someone to an agreement: they only get a copy" },
  add: { defaultMessage: "Add", id: "EYmnLm", description: "Button adding someone to an agreement" },
  notFetched: {
    defaultMessage: "The document couldn't be fetched.",
    id: "OM6iz1",
    description: "When an agreement's PDF couldn't be loaded to show its pages",
  },
  pagesHere: { defaultMessage: "The pages appear here.", id: "1gF6jj", description: "Where an agreement's pages are drawn, while they load" },
});

const v = {
  meta: defineMessage<{ name: string; file: string; n: number }>({
    defaultMessage: "{name} · {file} · {n, plural, one {# page} other {# pages}}",
    id: "oO4acB",
    description: "Under the title of the page preparing an agreement. {name} is the agreement's name, {file} its PDF's file name, {n} its number of pages",
  }),
  placeSignature: defineMessage<{ name: string }>({
    defaultMessage: "Place a signature for {name}.",
    id: "ybCtLn",
    description: "In the list of what an agreement needs before it can be sent. {name} is a signer's name",
  }),
  moveUp: defineMessage<{ name: string }>({
    defaultMessage: "Move {name} up",
    id: "QdYH9f",
    description: "What a screen reader calls the button moving a signer up the order. {name} is the signer's name",
  }),
  takeOff: defineMessage<{ name: string }>({
    defaultMessage: "Take {name} off, with their fields?",
    id: "yNSaJj",
    description: "Asked before taking a signer off an agreement. {name} is the signer's name",
  }),
  someoneAt: defineMessage<{ company: string }>({
    defaultMessage: "Someone at {company}",
    id: "WLHhzC",
    description: "Choice of a person at the company to add to an agreement. {company} is the company's name",
  }),
  placeFor: defineMessage<{ name: string }>({
    defaultMessage: "Place for {name}:",
    id: "+xZN/W",
    description: "Before the buttons for the kinds of field to place on an agreement. {name} is the signer they are for",
  }),
  numbered: defineMessage<{ n: number; name: string }>({
    defaultMessage: "{n} · {name}",
    id: "6TbXnv",
    description: "A signer of an agreement. {n} is their place in the order, {name} their name",
  }),
  member: defineMessage<{ email: string }>({
    defaultMessage: "{email} · member",
    id: "zH/xCa",
    description: "Under someone on an agreement who is a member of the organisation. {email} is their email address",
  }),
  person: defineMessage<{ name: string; email: string }>({
    defaultMessage: "{name} · {email}",
    id: "OFMRAO",
    description: "A person to choose to add to an agreement. {name} is their name, {email} their email address",
  }),
  pageOf: defineMessage<{ n: number; total: number }>({
    defaultMessage: "Page {n} of {total}",
    id: "ka0pPD",
    description: "What a screen reader calls one of an agreement's pages. {n} is the page's number, {total} how many pages there are",
  }),
  fieldFor: defineMessage<{ kind: string; name: string }>({
    defaultMessage: "{kind} for {name}",
    id: "no4k0a",
    description: "Tooltip on a field placed on an agreement's page. {kind} is the kind of field, such as Signature; {name} the signer it is for",
  }),
  fieldForGone: defineMessage<{ kind: string }>({
    defaultMessage: "{kind} for someone no longer signing",
    id: "WTQZoW",
    description: "Tooltip on a field placed on an agreement's page whose signer was taken off. {kind} is the kind of field, such as Signature",
  }),
  fieldTag: defineMessage<{ kind: string; name: string }>({
    defaultMessage: "{kind} · {name}",
    id: "1C5aX2",
    description: "Written in a field placed on an agreement's page. {kind} is the kind of field, such as Signature; {name} the signer's first name",
  }),
  removeField: defineMessage<{ kind: Kind }>({
    defaultMessage:
      "Remove this {kind, select, signature {signature} initials {initials} name {name} date {date signed} text {text} tick {tick box} other {}} field",
    id: "DR1R9k",
    description: "What a screen reader calls the button taking a field off an agreement's page; the choices are the kinds of field",
  }),
  sendingLater: defineMessage<{ brand: string }>({
    defaultMessage: "Sending it for signature comes in the next release. Until then nothing here leaves {brand}.",
    id: "QIll+I",
    description: "Under the list of what an agreement needs before it can be sent. {brand} is the product's name, Prospector",
  }),
};

// Names and marks: the same in every language.
const brand = "Prospector";
const back = "← ";
const raise = "↑";
const bullet = "· ";
const cross = "×";
const space = " ";
const clamp = (n: number, lo: number, hi: number) => Math.min(Math.max(n, lo), hi);
const field = "h-9 rounded-lg border border-input bg-muted px-2 text-sm";

type Signer = { id: number; name: string; colour: string };

export function PreparePage() {
  const say = useIntl().formatMessage;
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
  // The fields as edited, from what was last fetched: a fresh fetch starts them again.
  const [fields, setFields] = useKeyedState<Field[] | null>([stored.data], () =>
    stored.data ? stored.data.items.map(({ recipient_id, page, kind, x, y, w, h }) => ({ recipient_id, page, kind, x, y, w, h })) : null,
  );
  const [picked, setPicked] = useState<number | null>(null);
  const [saved, setSaved] = useState("");
  const [saveError, setSaveError] = useState<{ detail?: string } | null>(null);
  const saving = useRef(Promise.resolve());

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
    setSaved(say(m.saving));
    saving.current = saving.current.then(async () => {
      const r = await fetchClient.PUT("/o/{org}/api/v1/agreements/{id}/fields", { params: { path: { org, id } }, body: { items: next } });
      if (r.error) {
        setSaved("");
        setSaveError(r.error);
        stored.refetch();
      } else {
        setSaved(say(m.saved));
        setSaveError(null);
      }
    });
  };

  const head = (
    <>
      <Link to="/o/$org/companies/$id" params={{ org, id: a?.company_id ?? 0 }} disabled={!a} className="font-mono text-xs text-mark">
        {back}
        <Text value={a?.company} w="14ch" />
      </Link>
      <PageHead title={say(m.title)}>
        <Meta>
          <Text value={a && say(v.meta, { name: a.name, file: a.file_name, n: a.page_count })} w="32ch" />
        </Meta>
      </PageHead>
      <Whose area="sales" said={say(m.whose)} can={sales} />
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
    ...(a.file_hash ? [] : [say(m.upload)]),
    ...(signers.length === 0 ? [say(m.addSigner)] : []),
    ...signers.filter((s) => !signed.has(s.id)).map((s) => say(v.placeSignature, { name: s.name })),
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
            <Eyebrow>{say(m.signers)}</Eyebrow>
            {known && signers.length === 0 && <Meta>{say(m.nobodyYet)}</Meta>}
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
                          aria-label={say(v.moveUp, { name: s.name })}
                          onClick={() =>
                            change.mutate({
                              params: { path: { org, id, recipient: s.id } },
                              body: { position: all.find((x) => x.id === signers[i - 1]?.id)?.position },
                            })
                          }
                        >
                          {raise}
                        </ActionButton>
                      )}
                      <ActionButton quiet size="xs" onClick={() => change.mutate({ params: { path: { org, id, recipient: s.id } }, body: { role: "copy" } })}>
                        {say(m.copyOnly)}
                      </ActionButton>
                      <ActionButton
                        quiet
                        size="xs"
                        onClick={() => window.confirm(say(v.takeOff, { name: s.name })) && remove.mutate({ params: { path: { org, id, recipient: s.id } } })}
                      >
                        {say(m.remove)}
                      </ActionButton>
                    </div>
                  )}
                </SignerRow>
              );
            })}
            {edit ? (
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={a.in_order} onChange={(e) => order.mutate({ ...path, body: { in_order: e.currentTarget.checked } })} />
                {say(m.signInOrder)}
              </label>
            ) : (
              <Meta>{say(a.in_order ? m.inOrder : m.anyOrder)}</Meta>
            )}
          </section>
          <section className="flex flex-col gap-2 panel p-3.5">
            <Eyebrow>{say(m.getsCopy)}</Eyebrow>
            {known && copies.length === 0 && <Meta>{say(m.noCopies)}</Meta>}
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
                      {say(m.signs)}
                    </ActionButton>
                    <ActionButton quiet size="xs" onClick={() => remove.mutate({ params: { path: { org, id, recipient: r.id } } })}>
                      {say(m.remove)}
                    </ActionButton>
                  </span>
                )}
              </CopyRow>
            ))}
          </section>
          {edit && (
            <section className="flex flex-col gap-2 panel p-3.5">
              <Eyebrow>{say(m.addSomeone)}</Eyebrow>
              <AddForm
                label={say(v.someoneAt, { company: a.company })}
                options={a.people}
                onAdd={(who, role) => add.mutate({ ...path, body: { role, contact: who } })}
              />
              <AddForm label={say(m.inYourOrg)} options={a.members} onAdd={(who, role) => add.mutate({ ...path, body: { role, member: who } })} />
              <Failed errors={[add.error, change.error, remove.error, order.error]} />
              <Meta>{say(m.whoCanBeAdded)}</Meta>
            </section>
          )}
          <section className="flex flex-col gap-1.5 panel p-3.5">
            <Eyebrow>{say(m.beforeSent)}</Eyebrow>
            {known && fields && missing.length === 0 && <p className="font-mono text-xs text-[var(--ok)]">{say(m.ready)}</p>}
            {known &&
              fields &&
              missing.map((line) => (
                <Meta key={line}>
                  {bullet}
                  {line}
                </Meta>
              ))}
            <Meta>{say(v.sendingLater, { brand })}</Meta>
          </section>
        </div>
        <div className="flex min-w-0 flex-[999_1_560px] flex-col gap-3">
          {known && edit && signers.length > 0 && (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-mono text-xs text-muted-foreground">{say(v.placeFor, { name: placing?.name ?? "" })}</span>
                {kinds.map((k) => (
                  <ActionButton
                    key={k.kind}
                    quiet
                    size="xs"
                    draggable
                    onDragStart={(e) => e.dataTransfer.setData("text/x-field", k.kind)}
                    onClick={() => fields && placing && save([...fields, put(k.kind, placing.id, pageInView(), 0.5, 0.5)])}
                  >
                    {say(kindNames[k.kind])}
                  </ActionButton>
                ))}
                <span className="font-mono text-xs text-muted-foreground">{saved}</span>
              </div>
              <Meta>{say(m.howToPlace)}</Meta>
              <Failed errors={[saveError]} />
            </>
          )}
          {known && edit && signers.length === 0 && <Meta>{say(m.addThenPlace)}</Meta>}
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
  const say = useIntl().formatMessage;
  return (
    <div className="flex flex-col gap-1.5 rounded-lg border border-border p-2 text-sm">
      <label className="flex items-center gap-2">
        {edit && <input type="radio" name="placing" checked={placing ?? false} disabled={!s} onChange={onPlace} />}
        <span className={cn("shrink-0 rounded-sm", !s && "motion-safe:animate-pulse bg-accent")} style={{ width: 10, height: 10, background: s?.colour }} />
        <b>{s ? say(v.numbered, { n: n ?? 0, name: s.name }) : <Bar w="12ch" />}</b>
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
        <Text value={r?.name} w="10ch" />
        {space}
        <span className="font-mono text-xs text-muted-foreground">
          <Text value={r && partyLine(r)} w="18ch" />
        </span>
      </span>
      {children}
    </div>
  );
}

function partyLine(r: Recipient) {
  return r.member_id ? intl.formatMessage(v.member, { email: r.email }) : r.email;
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
  const say = useIntl().formatMessage;
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
            {say(v.person, { name: o.name, email: o.email })}
          </option>
        ))}
      </select>
      <div className="flex items-center gap-2">
        <select value={role} aria-label={say(m.whatTheyDo)} onChange={(e) => setRole(e.currentTarget.value as "signer" | "copy")} className={field}>
          <option value="signer">{say(m.roleSigner)}</option>
          <option value="copy">{say(m.roleCopy)}</option>
        </select>
        <ActionButton quiet size="sm" type="submit">
          {say(m.add)}
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
  const say = useIntl().formatMessage;
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
      if (doc.error || !doc.data) throw new Error(intl.formatMessage(m.notFetched));
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
    // oxlint-disable-next-line react/exhaustive-effect-dependencies -- hash is the document's: a new upload is drawn again, though only org and id are read
  }, [org, id, hash]);

  if (problem) return <Meta>{problem}</Meta>;
  return (
    <div ref={here} className="flex flex-col items-center gap-6">
      {sizes.length === 0 && <Meta>{say(m.pagesHere)}</Meta>}
      {sizes.map((s, i) => (
        <div
          key={i}
          data-page={i + 1}
          aria-label={say(v.pageOf, { n: i + 1, total: sizes.length })}
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
                onMove={(moved) => onChange(fields.map((g, at) => (at === n ? moved : g)))}
                onRemove={() => onChange(fields.filter((_, at) => at !== n))}
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
  // Where it is while dragged, from where it was saved: the saved field coming back starts it again.
  const say = useIntl().formatMessage;
  const [live, setLive] = useKeyedState([f], () => f);
  const colour = signer?.colour ?? "#b3a78e";
  const kind = say(kindNames[f.kind]);
  const start = (e: ReactPointerEvent<HTMLElement>, sizing: boolean) => {
    if (!edit) return;
    e.stopPropagation();
    const area = (e.currentTarget.closest("[data-page]") as HTMLElement).getBoundingClientRect();
    const from = { x: e.clientX, y: e.clientY, f: live };
    let now = live;
    const move = (p: PointerEvent) => {
      const dx = (p.clientX - from.x) / area.width;
      const dy = (p.clientY - from.y) / area.height;
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
      title={signer ? say(v.fieldFor, { kind, name: signer.name }) : say(v.fieldForGone, { kind })}
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
      {f.kind === "tick" ? "" : say(v.fieldTag, { kind, name: (signer?.name ?? "").split(" ")[0] ?? "" })}
      {edit && (
        <>
          <button
            type="button"
            aria-label={say(v.removeField, { kind: f.kind })}
            onPointerDown={(e) => e.stopPropagation()}
            onClick={onRemove}
            className="absolute cursor-pointer bg-transparent text-[13px]"
            style={{ right: 1, top: -2, border: 0 }}
          >
            {cross}
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
