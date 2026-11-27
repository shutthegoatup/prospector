import { type FormEvent, useState } from "react";
import { defineMessage, defineMessages, useIntl, type MessageDescriptor } from "react-intl";
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

const m = defineMessages({
  title: { defaultMessage: "Enquiries", id: "Gsjaem", description: "Title of the page listing enquiries from the website" },
  intro: {
    defaultMessage:
      "What people sent through your sites' forms and your products, newest first. The words are theirs, exactly as sent. Each new one waits to be answered: give it an owner, and say when you're waiting on them or it's closed. Reply from here and it is emailed to them; their answer comes to your own mailbox, and you record it here.",
    id: "tqu/z4",
    description: "Under the title of the Enquiries page: what it shows and how it is used",
  },
  whose: {
    defaultMessage: "Answering enquiries is support work, so here you read it.",
    id: "8UoIiK",
    description: "On the Enquiries page, to a member without support work: why they can read it but not act",
  },
  all: { defaultMessage: "All", id: "zec65k", description: "Filter on the Enquiries page showing every enquiry" },
  spam: { defaultMessage: "Spam", id: "DLHvp2", description: "Filter on the Enquiries page showing the enquiries taken for spam" },
  unread: { defaultMessage: "The enquiries couldn't be read. Reload to try again.", id: "Friped", description: "When the list of enquiries failed to load" },
  nothing: { defaultMessage: "Nothing yet", id: "CG1A6M", description: "Title when there are no enquiries under the filter chosen" },
  nothingHint: {
    defaultMessage: "Enquiries appear here once a site listed in Settings posts one, or a key with the enquiries:write scope does.",
    id: "DvtELE",
    description: "Hint when there are no enquiries; Settings is the name of a page, enquiries:write the name of an API key scope",
  },
  pages: { defaultMessage: "Pages", id: "aSiD4J", description: "What a screen reader calls the links between pages of a long list" },
  previous: { defaultMessage: "← Previous", id: "p98CtA", description: "Button back to the previous page of a list" },
  next: { defaultMessage: "Next →", id: "uzH+V4", description: "Button on to the next page of a list" },
  answering: { defaultMessage: "Answering", id: "SCe5HK", description: "Small heading on an enquiry, before where answering it stands and who has it" },
  notTracked: { defaultMessage: "Not tracked", id: "P2V68J", description: "Where answering an enquiry stands, when nobody has said" },
  nobodyHasIt: { defaultMessage: "nobody has it yet", id: "Yqa7Gd", description: "On an enquiry, after where answering it stands: no member is answering it" },
  standing: {
    defaultMessage: "Where answering it stands",
    id: "BPPNDa",
    description: "What a screen reader calls the choice of where answering an enquiry stands",
  },
  whoAnswers: { defaultMessage: "Who is answering it", id: "HXFdEQ", description: "What a screen reader calls the choice of the member answering an enquiry" },
  nobodyYet: { defaultMessage: "Nobody yet", id: "8a8HM5", description: "In the choice of who is answering an enquiry: no member" },
  notePlaceholder: {
    defaultMessage: "Our note: not emailed, but theirs to see if they ask",
    id: "BPlmGS",
    description: "Placeholder of the field for the members' note on an enquiry",
  },
  note: { defaultMessage: "Our note", id: "yZSQsb", description: "What a screen reader calls the field for the members' note on an enquiry" },
  inConversations: {
    defaultMessage: "In Conversations",
    id: "LmHyI8",
    description: "Link on an enquiry already made a conversation; Conversations is the name of a page",
  },
  converse: { defaultMessage: "Start a conversation", id: "WJbOhK", description: "Button making an enquiry a conversation" },
  remove: { defaultMessage: "Delete", id: "L3fhRh", description: "Button deleting an enquiry" },
  reply: {
    defaultMessage: "Reply, or record their answer",
    id: "UCu3U6",
    description: "Opens the fields under an enquiry to reply to it or record what they said back",
  },
  sendReply: { defaultMessage: "Email this reply", id: "SfIQnz", description: "Button emailing a reply to an enquiry" },
  noMail: {
    defaultMessage: "To reply from here an owner sets a mail server in Settings. Until then, answer from your own mail.",
    id: "xAPIJu",
    description: "Under an enquiry, in place of the reply field, when no mail server is set; Settings is the name of a page",
  },
  answerPlaceholder: {
    defaultMessage: "What they said back, pasted from your mail. Nothing is sent.",
    id: "cHEmjo",
    description: "Placeholder of the field recording an enquirer's answer",
  },
  record: { defaultMessage: "Record their answer", id: "RHztYC", description: "Button recording an enquirer's answer pasted from mail" },
  saved: { defaultMessage: "Saved ✓", id: "ZbHsgv", description: "After a reply was sent or an answer recorded" },
  failed: {
    defaultMessage: "That didn't work. Reload and try again.",
    id: "+cNnh1",
    description: "When sending a reply or recording an answer failed with nothing from the server to say why",
  },
});

const v = {
  page: defineMessage<{ n: number; pages: number }>({
    defaultMessage: "Page {n} of {pages}",
    id: "7v4UCq",
    description: "Between the buttons of a long list: which page this is",
  }),
  onlyPage: defineMessage<{ n: number }>({
    defaultMessage: "Page {n}",
    id: "ItNsS4",
    description: "Between the buttons of a long list whose length isn't known: which page this is",
  }),
  hasIt: defineMessage<{ name: string }>({
    defaultMessage: "{name} has it",
    id: "E+o1ZU",
    description: "On an enquiry, after where answering it stands. {name} is the member answering it",
  }),
  left: defineMessage<{ name: string }>({
    defaultMessage: "{name} (left)",
    id: "ViIW8d",
    description: "In the choice of who is answering an enquiry: a member who has since left. {name} is their name",
  }),
  confirmRemove: defineMessage<{ email: string }>({
    defaultMessage: "Delete this enquiry from {email} for good?",
    id: "3S7eks",
    description: "Asked before deleting an enquiry. {email} is the enquirer's address",
  }),
  replied: defineMessage<{ by: string; at: string }>({
    defaultMessage: "{by} replied · {at}",
    id: "QTmUBQ",
    description: "Above a reply in an enquiry's thread. {by} is who replied, {at} the date and time",
  }),
  theySaid: defineMessage<{ by: string; at: string }>({
    defaultMessage: "they said · recorded by {by} · {at}",
    id: "sOH1iG",
    description: "Above an enquirer's answer in the thread. {by} is the member who recorded it, {at} the date and time",
  }),
  replyPlaceholder: defineMessage<{ email: string }>({
    defaultMessage: "Your reply to {email}. Only what you write here is sent; their answer comes to your own mailbox.",
    id: "fTUmeI",
    description: "Placeholder of the field for a reply to an enquiry. {email} is the enquirer's address",
  }),
};

const stateNames: Record<string, MessageDescriptor> = defineMessages({
  open: {
    defaultMessage: "To answer",
    id: "PgG+wU",
    description: "Where answering an enquiry stands: not answered yet. A filter, a choice and a label on each enquiry",
  },
  waiting: {
    defaultMessage: "Waiting on them",
    id: "N5m6RU",
    description: "Where answering an enquiry stands: answered, waiting on the enquirer. A filter, a choice and a label on each enquiry",
  },
  closed: { defaultMessage: "Closed", id: "x+lq22", description: "Where answering an enquiry stands: done with. A choice and a label on each enquiry" },
});

const kindNames: Record<string, MessageDescriptor> = defineMessages({
  sales: { defaultMessage: "Sales", id: "tAkbqW", description: "What an enquiry is about: buying. A filter and a label on each enquiry" },
  demo: { defaultMessage: "Demo", id: "YYWBQh", description: "What an enquiry is about: asking for a demo. A filter and a label on each enquiry" },
  signup: { defaultMessage: "Sign-up", id: "10Hi0u", description: "What an enquiry is about: signing up. A filter and a label on each enquiry" },
  support: { defaultMessage: "Help", id: "vhpYOv", description: "What an enquiry is about: asking for help. A filter and a label on each enquiry" },
  feature: {
    defaultMessage: "Feature request",
    id: "35/Dia",
    description: "What an enquiry is about: asking for a feature. A filter and a label on each enquiry",
  },
  other: { defaultMessage: "Other", id: "WkxVFJ", description: "What an enquiry is about: anything else. A filter and a label on each enquiry" },
});

const states = ["open", "waiting", "closed"] as const;
const kinds = ["sales", "demo", "signup", "support", "feature", "other"] as const;

const dot = " · ";
const space = " ";

const select = "h-8 rounded-lg border border-input bg-muted px-2 text-sm";
const area = "rounded-lg border border-input bg-muted px-3 py-2 text-sm leading-relaxed";

// An unknown state shows nothing; an unknown kind is the server's own word for it.
function useLabels() {
  const say = useIntl().formatMessage;
  return {
    stateLabel: (state: string) => (stateNames[state] ? say(stateNames[state]) : ""),
    kindLabel: (kind: string) => (kindNames[kind] ? say(kindNames[kind]) : kind),
  };
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
  return parts.filter(Boolean).join(dot);
}

function from(e: Enquiry) {
  let where = joined(e.site, e.phone);
  if (e.via !== "form") where = joined(actor(e.via), where);
  return joined(dated(e.created_at), where);
}

function ThreadMeta({ said }: { said: Message }) {
  const say = useIntl().formatMessage;
  const values = { by: actor(said.by), at: stamp(said.created_at) };
  return <Meta>{say(said.ours ? v.replied : v.theySaid, values)}</Meta>;
}

export function EnquiriesPage() {
  const say = useIntl().formatMessage;
  const { stateLabel, kindLabel } = useLabels();
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
      <PageHead title={say(m.title)} />
      <p className="mb-5 max-w-[70ch] text-sm text-muted-foreground">{say(m.intro)}</p>
      <Whose area="support" said={say(m.whose)} can={support} />
      {/* Only "All" is sure to be there, so it holds the row's place until the counts come. */}
      {!counts && (
        <div className="mb-5 flex flex-wrap gap-2">
          <FilterChip {...here} label={say(m.all)} on={!kind && !state && !spam} />
        </div>
      )}
      {counts && (
        <div className="mb-5 flex flex-wrap gap-2">
          {states
            .filter((s) => s !== "closed" && (counts.states[s] ?? 0) > 0)
            .map((s) => (
              <FilterChip key={s} {...here} search={{ state: s }} label={stateLabel(s)} count={counts.states[s] ?? 0} on={state === s} />
            ))}
          <FilterChip {...here} label={say(m.all)} count={counts.all} on={!kind && !state && !spam} />
          {kinds
            .filter((k) => (counts.kinds[k] ?? 0) > 0)
            .map((k) => (
              <FilterChip key={k} {...here} search={{ kind: k }} label={kindLabel(k)} count={counts.kinds[k] ?? 0} on={kind === k && !spam} />
            ))}
          {counts.spam > 0 && <FilterChip {...here} search={{ spam: true }} label={say(m.spam)} count={counts.spam} on={spam} />}
        </div>
      )}
      {list.error && (
        <p className="text-sm text-[var(--stop)]" role="alert">
          {say(m.unread)}
        </p>
      )}
      {list.data && list.data.items.length === 0 && <Empty title={say(m.nothing)} hint={say(m.nothingHint)} />}
      {!list.data && !list.error && (
        <Placeholders className="flex max-w-3xl flex-col gap-3">
          {Array.from({ length: placeholders }, (_, i) => (
            <EnquiryCard key={i} members={[]} canReply={false} />
          ))}
        </Placeholders>
      )}
      <div className="flex max-w-3xl flex-col gap-3">
        {list.data?.items.map((e) => (
          <EnquiryCard key={e.id} e={e} members={members.map((p) => p.name)} canReply={list.data.can_reply} />
        ))}
      </div>
      {(cursors.length > 0 || next) && (
        <nav className="mt-3 flex max-w-3xl flex-wrap items-center justify-between gap-3" aria-label={say(m.pages)}>
          <span>
            {cursors.length > 0 && (
              <ActionButton quiet size="sm" onClick={() => setCursors(cursors.slice(0, -1))}>
                {say(m.previous)}
              </ActionButton>
            )}
          </span>
          <Meta>{pages >= page ? say(v.page, { n: page, pages }) : say(v.onlyPage, { n: page })}</Meta>
          <span>
            {next && (
              <ActionButton quiet size="sm" onClick={() => setCursors([...cursors, next])}>
                {say(m.next)}
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
  const say = useIntl().formatMessage;
  const { stateLabel, kindLabel } = useLabels();
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
            </b>
            {space}
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
          <b>{e.name}</b>
          {space}
          <a href={`mailto:${e.email}`} className="text-muted-foreground underline">
            {e.email}
          </a>
          {e.company && (
            <span className="text-muted-foreground">
              {dot}
              {e.company}
            </span>
          )}
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
          <Eyebrow>{say(m.answering)}</Eyebrow>
          <span>
            <b className="font-semibold">{stateLabel(e.state) || say(m.notTracked)}</b>
            {dot + (e.owner ? say(v.hasIt, { name: e.owner }) : say(m.nobodyHasIt))}
          </span>
          {e.note && <span className="text-muted-foreground">{e.note}</span>}
        </div>
      )}
      {!e.spam && support && (
        <div className="flex flex-wrap items-center gap-2 border-t border-border pt-2">
          <Eyebrow>{say(m.answering)}</Eyebrow>
          <select aria-label={say(m.standing)} defaultValue={e.state} onChange={(ev) => set({ state: ev.target.value })} className={select}>
            {e.state === "" && <option value="">{say(m.notTracked)}</option>}
            {states.map((s) => (
              <option key={s} value={s}>
                {stateLabel(s)}
              </option>
            ))}
          </select>
          <select aria-label={say(m.whoAnswers)} defaultValue={e.owner} onChange={(ev) => set({ owner: ev.target.value })} className={select}>
            <option value="">{say(m.nobodyYet)}</option>
            {ownerLeft && <option value={e.owner}>{say(v.left, { name: e.owner })}</option>}
            {members.map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
          </select>
          <Input
            defaultValue={e.note}
            placeholder={say(m.notePlaceholder)}
            aria-label={say(m.note)}
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
                {say(m.inConversations)}
              </Link>
            ) : (
              (sales || support) && (
                <ActionButton quiet size="xs" onClick={() => converse.mutate(converseOn)} disabled={converse.isPending}>
                  {say(m.converse)}
                </ActionButton>
              )
            ))}
          {owner && (
            <ActionButton quiet size="xs" onClick={() => window.confirm(say(v.confirmRemove, { email: e.email })) && remove.mutate(path)}>
              {say(m.remove)}
            </ActionButton>
          )}
        </span>
      </div>
      {!e.spam && (e.thread.length > 0 || support) && (
        <div className="flex flex-col gap-2 border-t border-border pt-2">
          {e.thread.map((said) => (
            <div key={said.id} className="rounded-lg bg-muted p-3">
              <ThreadMeta said={said} />
              <p className="mt-1 whitespace-pre-wrap text-sm">{said.text}</p>
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
  const say = useIntl().formatMessage;
  return (
    <details>
      <summary className="cursor-pointer font-mono text-xs text-muted-foreground">{say(m.reply)}</summary>
      <div className="mt-2 flex flex-col gap-3">
        {canReply ? (
          <Said
            e={e}
            to="/o/{org}/api/v1/enquiries/{id}/replies"
            rows={4}
            placeholder={say(v.replyPlaceholder, { email: e.email })}
            button={say(m.sendReply)}
          />
        ) : (
          <Meta>{say(m.noMail)}</Meta>
        )}
        <Said e={e} to="/o/{org}/api/v1/enquiries/{id}/answers" rows={3} placeholder={say(m.answerPlaceholder)} button={say(m.record)} quiet />
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
  const say = useIntl().formatMessage;
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
        {said.isSuccess && <span className="font-mono text-xs text-muted-foreground">{say(m.saved)}</span>}
        {said.error && (
          <span className="font-mono text-xs text-[var(--stop)]" role="alert">
            {said.error.detail ?? say(m.failed)}
          </span>
        )}
      </div>
    </form>
  );
}
