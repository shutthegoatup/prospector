import { type FormEvent, useState } from "react";
import { keepPreviousData } from "@tanstack/react-query";
import { Link, useSearch } from "@tanstack/react-router";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Empty, Eyebrow, Failed, FilterChip, Meta, PageHead, Pill, Quote, type Tone, useRefresh, Whose } from "@/components/app";
import { Bar, Placeholders, placeholders } from "@/components/app/skeleton";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { actor, dated, stamp } from "@/lib/format";
import { useKeyedState } from "@/lib/keyed";
import { useSession } from "@/session";

type Enquiry = components["schemas"]["Enquiry"];
type Message = components["schemas"]["EnquiryMessage"];

const pageSize = 50;

const states = [
  { key: "open", label: "To answer" },
  { key: "waiting", label: "Waiting on them" },
  { key: "closed", label: "Closed" },
] as const;

const kinds = [
  { key: "sales", label: "Sales" },
  { key: "demo", label: "Demo" },
  { key: "signup", label: "Sign-up" },
  { key: "support", label: "Help" },
  { key: "feature", label: "Feature request" },
  { key: "other", label: "Other" },
] as const;

const select = "h-8 rounded-lg border border-input bg-muted px-2 text-sm";
const area = "rounded-lg border border-input bg-muted px-3 py-2 text-sm leading-relaxed";

function stateLabel(state: string) {
  return states.find((s) => s.key === state)?.label ?? "";
}

function kindLabel(kind: string) {
  return kinds.find((k) => k.key === kind)?.label ?? kind;
}

function kindTone(kind: string): Tone {
  switch (kind) {
    case "sales":
    case "demo":
    case "signup":
      return "ok";
    case "support":
      return "warn";
    case "feature":
      return "agent";
  }
  return "";
}

function joined(...parts: string[]) {
  return parts.filter(Boolean).join(" · ");
}

function from(e: Enquiry) {
  let where = joined(e.site, e.phone);
  if (e.via !== "form") where = joined(actor(e.via), where);
  return joined(dated(e.created_at), where);
}

function threadMeta(m: Message) {
  if (m.ours) return actor(m.by) + " replied · " + stamp(m.created_at);
  return "they said · recorded by " + actor(m.by) + " · " + stamp(m.created_at);
}

export function EnquiriesPage() {
  const { org, support } = useSession();
  const { kind = "", state = "", spam = false } = useSearch({ from: "/o/$org/enquiries" });
  const here = { to: "/o/$org/enquiries", params: { org } } as const;
  // Each cursor is where a page after the first starts, so Previous is one pop.
  // Another filter is another list, which starts at its first page.
  const [cursors, setCursors] = useKeyedState<string[]>([kind, state, spam], () => []);
  const cursor = cursors[cursors.length - 1];

  const list = $api.useQuery(
    "get",
    "/o/{org}/api/v1/enquiries",
    {
      params: { path: { org }, query: { kind: kind || undefined, state: state || undefined, spam: spam || undefined, cursor, limit: pageSize } },
    },
    { placeholderData: keepPreviousData },
  );
  const counts = $api.useQuery("get", "/o/{org}/api/v1/enquiry-counts", { params: { path: { org } } }).data;
  const members = $api.useQuery("get", "/o/{org}/api/v1/members", { params: { path: { org }, query: { limit: 200 } } }).data?.items ?? [];

  const matching = !counts ? 0 : spam ? counts.spam : kind && state ? 0 : kind ? (counts.kinds[kind] ?? 0) : state ? (counts.states[state] ?? 0) : counts.all;
  const pages = Math.ceil(matching / pageSize);
  const page = cursors.length + 1;
  const next = list.data?.next_cursor;

  return (
    <>
      <PageHead title="Enquiries" />
      <p className="mb-5 max-w-[70ch] text-sm text-muted-foreground">
        What people sent through your sites' forms and your products, newest first. The words are theirs, exactly as sent. Each new one waits to be answered:
        give it an owner, and say when you're waiting on them or it's closed. Reply from here and it is emailed to them; their answer comes to your own mailbox,
        and you record it here.
      </p>
      <Whose area="support" work="Answering enquiries is" can={support} />
      {/* Only "All" is sure to be there, so it holds the row's place until the counts come. */}
      {!counts && (
        <div className="mb-5 flex flex-wrap gap-2">
          <FilterChip {...here} label="All" on={!kind && !state && !spam} />
        </div>
      )}
      {counts && (
        <div className="mb-5 flex flex-wrap gap-2">
          {states
            .filter((s) => s.key !== "closed" && (counts.states[s.key] ?? 0) > 0)
            .map((s) => (
              <FilterChip key={s.key} {...here} search={{ state: s.key }} label={s.label} count={counts.states[s.key] ?? 0} on={state === s.key} />
            ))}
          <FilterChip {...here} label="All" count={counts.all} on={!kind && !state && !spam} />
          {kinds
            .filter((k) => (counts.kinds[k.key] ?? 0) > 0)
            .map((k) => (
              <FilterChip key={k.key} {...here} search={{ kind: k.key }} label={k.label} count={counts.kinds[k.key] ?? 0} on={kind === k.key && !spam} />
            ))}
          {counts.spam > 0 && <FilterChip {...here} search={{ spam: true }} label="Spam" count={counts.spam} on={spam} />}
        </div>
      )}
      {list.error && (
        <p className="text-sm text-[var(--stop)]" role="alert">
          The enquiries couldn't be read. Reload to try again.
        </p>
      )}
      {list.data && list.data.items.length === 0 && (
        <Empty title="Nothing yet" hint="Enquiries appear here once a site listed in Settings posts one, or a key with the enquiries:write scope does." />
      )}
      {!list.data && !list.error && (
        <Placeholders className="flex max-w-3xl flex-col gap-3">
          {Array.from({ length: placeholders }, (_, i) => (
            <EnquiryCard key={i} members={[]} canReply={false} />
          ))}
        </Placeholders>
      )}
      <div className="flex max-w-3xl flex-col gap-3">
        {list.data?.items.map((e) => (
          <EnquiryCard key={e.id} e={e} members={members.map((m) => m.name)} canReply={list.data.can_reply} />
        ))}
      </div>
      {(cursors.length > 0 || next) && (
        <nav className="mt-3 flex max-w-3xl flex-wrap items-center justify-between gap-3" aria-label="Pages">
          <span>
            {cursors.length > 0 && (
              <ActionButton quiet size="sm" onClick={() => setCursors(cursors.slice(0, -1))}>
                ← Previous
              </ActionButton>
            )}
          </span>
          <Meta>{pages >= page ? `Page ${page} of ${pages}` : `Page ${page}`}</Meta>
          <span>
            {next && (
              <ActionButton quiet size="sm" onClick={() => setCursors([...cursors, next])}>
                Next →
              </ActionButton>
            )}
          </span>
        </nav>
      )}
    </>
  );
}

// EnquiryCard is one enquiry, or its outline while the list loads.
function EnquiryCard({ e, members, canReply }: { e?: Enquiry; members: string[]; canReply: boolean }) {
  const { org, sales, support, owner } = useSession();
  const refresh = useRefresh();
  const change = $api.useMutation("patch", "/o/{org}/api/v1/enquiries/{id}", { onSuccess: refresh });
  const converse = $api.useMutation("post", "/o/{org}/api/v1/enquiries/{id}/conversation", { onSuccess: refresh });
  const remove = $api.useMutation("delete", "/o/{org}/api/v1/enquiries/{id}", { onSuccess: refresh });
  if (!e) {
    return (
      <article className="flex flex-col gap-2.5 rounded-xl border border-border bg-card p-3.5 text-sm">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <span>
            <b>
              <Bar w="12ch" />
            </b>{" "}
            <Bar w="20ch" />
          </span>
          <Pill>
            <Bar w="6ch" />
          </Pill>
        </div>
        <Meta>
          <Bar w="18ch" />
        </Meta>
      </article>
    );
  }
  const path = { params: { path: { org, id: e.id } } };
  const set = (body: { state?: string; owner?: string; note?: string }) => change.mutate({ ...path, body });
  // The server takes a browser's POST only as JSON, even one with no body.
  const converseOn = { ...path, headers: { "content-type": "application/json" } };
  const ownerLeft = e.owner !== "" && !members.includes(e.owner);

  return (
    <article className="flex flex-col gap-2.5 rounded-xl border border-border bg-card p-3.5 text-sm">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span>
          <b>{e.name}</b>{" "}
          <a href={`mailto:${e.email}`} className="text-muted-foreground underline">
            {e.email}
          </a>
          {e.company && <span className="text-muted-foreground"> · {e.company}</span>}
        </span>
        <span className="flex items-center gap-2">
          {e.state && <Pill tone={e.state === "open" ? "warn" : ""}>{stateLabel(e.state)}</Pill>}
          <Pill tone={kindTone(e.kind)}>{kindLabel(e.kind)}</Pill>
        </span>
      </div>
      {e.message && <Quote>{e.message}</Quote>}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Meta>{from(e)}</Meta>
        {e.company_id !== 0 && (
          <Link to="/o/$org/companies/$id" params={{ org, id: e.company_id }} className="font-mono text-xs text-mark underline">
            {e.company_name}
          </Link>
        )}
      </div>
      {!e.spam && !support && (
        <div className="flex flex-wrap items-center gap-2 border-t border-border pt-2">
          <Eyebrow>Answering</Eyebrow>
          <span>
            <b className="font-semibold">{stateLabel(e.state) || "Not tracked"}</b>
            {" · " + (e.owner ? e.owner + " has it" : "nobody has it yet")}
          </span>
          {e.note && <span className="text-muted-foreground">{e.note}</span>}
        </div>
      )}
      {!e.spam && support && (
        <div className="flex flex-wrap items-center gap-2 border-t border-border pt-2">
          <Eyebrow>Answering</Eyebrow>
          <select aria-label="Where answering it stands" defaultValue={e.state} onChange={(ev) => set({ state: ev.target.value })} className={select}>
            {e.state === "" && <option value="">Not tracked</option>}
            {states.map((s) => (
              <option key={s.key} value={s.key}>
                {s.label}
              </option>
            ))}
          </select>
          <select aria-label="Who is answering it" defaultValue={e.owner} onChange={(ev) => set({ owner: ev.target.value })} className={select}>
            <option value="">Nobody yet</option>
            {ownerLeft && <option value={e.owner}>{e.owner} (left)</option>}
            {members.map((m) => (
              <option key={m} value={m}>
                {m}
              </option>
            ))}
          </select>
          <Input
            defaultValue={e.note}
            placeholder="Our note: not emailed, but theirs to see if they ask"
            aria-label="Our note"
            onBlur={(ev) => ev.target.value !== e.note && set({ note: ev.target.value })}
            onKeyDown={(ev) => ev.key === "Enter" && ev.currentTarget.blur()}
            className="h-8 min-w-[220px] flex-1 bg-muted"
          />
        </div>
      )}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Meta>{e.page}</Meta>
        <span className="flex items-center gap-2">
          {!e.spam &&
            (e.in_conversation ? (
              <Link to="/o/$org/conversations" params={{ org }} className="font-mono text-xs text-mark underline">
                In Conversations
              </Link>
            ) : (
              (sales || support) && (
                <ActionButton quiet size="xs" onClick={() => converse.mutate(converseOn)} disabled={converse.isPending}>
                  Start a conversation
                </ActionButton>
              )
            ))}
          {owner && (
            <ActionButton quiet size="xs" onClick={() => window.confirm(`Delete this enquiry from ${e.email} for good?`) && remove.mutate(path)}>
              Delete
            </ActionButton>
          )}
        </span>
      </div>
      {!e.spam && (e.thread.length > 0 || support) && (
        <div className="flex flex-col gap-2 border-t border-border pt-2">
          {e.thread.map((m) => (
            <div key={m.id} className="rounded-lg bg-muted p-3">
              <Meta>{threadMeta(m)}</Meta>
              <p className="mt-1 whitespace-pre-wrap text-sm">{m.text}</p>
            </div>
          ))}
          {support && <Reply e={e} canReply={canReply} />}
        </div>
      )}
      <Failed errors={[change.error, converse.error, remove.error]} />
    </article>
  );
}

function Reply({ e, canReply }: { e: Enquiry; canReply: boolean }) {
  return (
    <details>
      <summary className="cursor-pointer font-mono text-xs text-muted-foreground">Reply, or record their answer</summary>
      <div className="mt-2 flex flex-col gap-3">
        {canReply ? (
          <Said
            e={e}
            to="/o/{org}/api/v1/enquiries/{id}/replies"
            rows={4}
            placeholder={`Your reply to ${e.email}. Only what you write here is sent; their answer comes to your own mailbox.`}
            button="Email this reply"
          />
        ) : (
          <Meta>To reply from here an owner sets a mail server in Settings. Until then, answer from your own mail.</Meta>
        )}
        <Said
          e={e}
          to="/o/{org}/api/v1/enquiries/{id}/answers"
          rows={3}
          placeholder="What they said back, pasted from your mail. Nothing is sent."
          button="Record their answer"
          quiet
        />
      </div>
    </details>
  );
}

// Said is one way of adding to the thread: a reply that is emailed, or their answer recorded.
function Said({
  e,
  to,
  rows,
  placeholder,
  button,
  quiet,
}: {
  e: Enquiry;
  to: "/o/{org}/api/v1/enquiries/{id}/replies" | "/o/{org}/api/v1/enquiries/{id}/answers";
  rows: number;
  placeholder: string;
  button: string;
  quiet?: boolean;
}) {
  const { org } = useSession();
  const refresh = useRefresh();
  const [text, setText] = useState("");
  const said = $api.useMutation("post", to, {
    onSuccess: () => {
      setText("");
      refresh();
    },
  });
  const submit = (ev: FormEvent) => {
    ev.preventDefault();
    said.mutate({ params: { path: { org, id: e.id } }, body: { text } });
  };
  return (
    <form onSubmit={submit} className="flex flex-col gap-2">
      <Textarea
        name="text"
        rows={rows}
        required
        maxLength={10000}
        value={text}
        onChange={(ev) => setText(ev.target.value)}
        placeholder={placeholder}
        className={area}
      />
      <div className="flex items-center gap-3">
        <ActionButton type="submit" size="sm" quiet={quiet} disabled={said.isPending}>
          {button}
        </ActionButton>
        {said.isSuccess && <span className="font-mono text-xs text-muted-foreground">Saved ✓</span>}
        {said.error && (
          <span className="font-mono text-xs text-[var(--stop)]" role="alert">
            {said.error.detail ?? "That didn't work. Reload and try again."}
          </span>
        )}
      </div>
    </form>
  );
}
