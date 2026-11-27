import { keepPreviousData, useQueryClient } from "@tanstack/react-query";
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { defineMessage, defineMessages, FormattedMessage, useIntl, type MessageDescriptor } from "react-intl";
import { Link, useSearch } from "@tanstack/react-router";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Empty, Eyebrow, Failed, FilterChip, Meta, PageHead, Pill, RailCard, useRefresh, Whose } from "@/components/app";
import { Bar, Placeholders, placeholders, Settled, Text } from "@/components/app/skeleton";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { actor, onDay } from "@/lib/format";
import { intl } from "@/i18n";
import { safeHref } from "@/lib/links";
import { useSession } from "@/session";

type DueCard = components["schemas"]["DueCard"];
type Step = DueCard["step"];
type MarkStep = components["schemas"]["MarkNew"]["step"];
type ContactChange = components["schemas"]["ContactChange"];

const m = defineMessages({
  title: { defaultMessage: "Today", id: "gMzis5", description: "Title of the page listing the day's outreach" },
  whose: {
    defaultMessage: "Outreach is sales work, so here you read it.",
    id: "+650eH",
    description: "On the Today page, to a member without sales work: why they can read it but not act",
  },
  filterByStep: { defaultMessage: "Filter by step", id: "DXkbGZ", description: "What a screen reader calls the row of filters by step on the Today page" },
  all: { defaultMessage: "All", id: "tXCGGp", description: "Filter on the Today page showing every step" },
  nothingDue: { defaultMessage: "Nothing due here", id: "bOriAy", description: "Title when the Today page has nothing to do under the filter chosen" },
  nothingDueHint: {
    defaultMessage: "Add someone from LinkedIn, or check Research for companies waiting on you.",
    id: "C7e7R8",
    description: "Hint when the Today page has nothing to do; Research is the name of a page",
  },
  doNow: { defaultMessage: "Do now", id: "sgwP9f", description: "Heading of the Today page's panel of things to do now, each with a count" },
  nothingNow: { defaultMessage: "Nothing due right now.", id: "Gtr29S", description: "In the Do now panel when there is nothing to do" },
  later: { defaultMessage: "Later", id: "JnJ6Mx", description: "Heading of the Today page's panel of calls to make once it is office hours" },
  lastWeek: { defaultMessage: "Last 7 days", id: "1MET8Y", description: "Heading of the Today page's panel of counts for the past week" },
  touches: { defaultMessage: "touches", id: "fAYYkN", description: "Under a count in the Last 7 days panel: notes, messages, emails and calls made" },
  replies: { defaultMessage: "replies", id: "Hpunxd", description: "Under a count in the Last 7 days panel: replies received" },
  meetings: { defaultMessage: "meetings", id: "e9fTe/", description: "Under a count in the Last 7 days panel: meetings booked" },
  researched: { defaultMessage: "researched", id: "NHrASo", description: "Under a count in the Last 7 days panel: companies researched" },
  answerEnquiries: { defaultMessage: "Answer enquiries", id: "Z1lVj5", description: "In the Do now panel, beside a count of unanswered enquiries" },
  setNextSteps: { defaultMessage: "Set next steps", id: "mfLMho", description: "In the Do now panel, beside a count of conversations needing a next step" },
  moveDeals: {
    defaultMessage: "Move renewals and other deals on",
    id: "0BmVcB",
    description: "In the Do now panel, beside a count of further deals with no next step or one that is due",
  },
  lookAtSubscriptions: {
    defaultMessage: "Look at subscriptions coming due",
    id: "Wwk3F6",
    description: "In the Do now panel, beside a count of subscriptions coming due",
  },
  moveOnboarding: {
    defaultMessage: "Move onboarding steps on",
    id: "9MCyQA",
    description: "In the Do now panel, beside a count of customers' onboarding steps waiting",
  },
  makeCalls: { defaultMessage: "Make calls now", id: "xmDFHI", description: "In the Do now panel, beside a count of calls where it is office hours" },
  sendMessages: { defaultMessage: "Send LinkedIn messages", id: "2qC45m", description: "In the Do now panel, beside a count of LinkedIn messages to send" },
  sendEmails: { defaultMessage: "Send emails", id: "nzC5dc", description: "In the Do now panel, beside a count of emails to send" },
  sendFollowups: { defaultMessage: "Send follow-ups", id: "E9oEyL", description: "In the Do now panel, beside a count of follow-ups to send" },
  addOnLinkedIn: { defaultMessage: "Add on LinkedIn", id: "T7yEjP", description: "In the Do now panel, beside a count of people to add on LinkedIn" },
  reviewResearched: {
    defaultMessage: "Review researched companies",
    id: "WIXyfe",
    description: "In the Do now panel, beside a count of researched companies to review",
  },
  copied: { defaultMessage: "Copied ✓", id: "MNh7Fx", description: "A copy button's label once it has copied the text" },
  copy: { defaultMessage: "Copy", id: "zaqdVr", description: "Button copying an email or follow-up, signed, to paste elsewhere" },
  note: { defaultMessage: "Note", id: "Vx2kvl", description: "What a screen reader calls the box with a LinkedIn connection note to edit" },
  message: { defaultMessage: "Message", id: "MkdeMg", description: "What a screen reader calls the box with a LinkedIn message to edit" },
  callScript: { defaultMessage: "Call script", id: "y4K/LS", description: "Opens what to say on a call" },
  notes: { defaultMessage: "Notes", id: "WqroO0", description: "Label of the field for notes from a call" },
  called: { defaultMessage: "Called", id: "DMp1f1", description: "Button marking a call made" },
  interested: { defaultMessage: "Interested", id: "wlw5/B", description: "Button marking that the person called was interested" },
  notInterested: { defaultMessage: "Not interested", id: "j8PtH5", description: "Button marking that the person called was not interested" },
  added: { defaultMessage: "Added", id: "EIULF9", description: "Button marking someone added on LinkedIn" },
  skip: { defaultMessage: "Skip", id: "cpHquh", description: "Button putting off adding someone on LinkedIn" },
  sent: { defaultMessage: "Sent", id: "ui6JIn", description: "Button marking a message, email or follow-up sent" },
  replied: { defaultMessage: "Replied", id: "pArKFD", description: "Button marking that the person replied" },
  acceptedInstead: {
    defaultMessage: "Accepted instead",
    id: "gmXCeT",
    description: "Button on an email due: the person accepted the LinkedIn connection instead, so a LinkedIn message goes next",
  },
  nextStep: { defaultMessage: "Next step", id: "Y1XZJo", description: "Label of the field for the next step in a conversation" },
  by: { defaultMessage: "By", id: "Q0Em+a", description: "Label of the date the next step is due by" },
  noDate: { defaultMessage: "No date", id: "ZQ5Y/A", description: "In place of a next step's date when it has none" },
  stage: { defaultMessage: "Stage", id: "kJ/AVh", description: "Label of where a conversation stands" },
  fit: { defaultMessage: "Fit", id: "gQL8tA", description: "Tooltip on a company's fit score" },
  sendTo: { defaultMessage: "Send to", id: "XxgWDg", description: "Label of the choice of whom at a company to send to" },
  email: { defaultMessage: "Email", id: "HYsRxB", description: "Label of a person's email address" },
  phone: { defaultMessage: "Phone", id: "xfJnWy", description: "Label of a person's phone number" },
  timeZone: { defaultMessage: "Time zone", id: "uHTjF9", description: "Label of a person's time zone" },
  sources: { defaultMessage: "Sources to check before sending", id: "VBs1xl", description: "Heading of the pages the research drew on" },
  flags: { defaultMessage: "Flags", id: "7fXu3t", description: "Heading of what the research flagged to look at" },
});

const v = {
  showing: defineMessage<{ shown: number; total: number }>({
    defaultMessage: "Showing the first {shown} of {total}. Clear some, or filter by step above.",
    id: "/rdoJK",
    description: "Under the Today page's list when only some of what is due is shown",
  }),
  laterCalls: defineMessage<{ n: number }>({
    defaultMessage: "{n, plural, one {# call} other {# calls}} outside office hours right now",
    id: "KzAALb",
    description: "In the Later panel: calls to make once it is office hours where the person is",
  }),
  now: defineMessage<{ day: string; time: string }>({
    defaultMessage: "{day} · {time}",
    id: "j9HJpA",
    description: "Beside the Today page's title: the day, such as Fri 27 Nov, and the time",
  }),
  chars: defineMessage<{ n: number; max: number }>({
    defaultMessage: "{n} / {max} chars",
    id: "QYg3/2",
    description: "Beside a LinkedIn connection note: how many characters it has of the most LinkedIn allows",
  }),
  draftedBy: defineMessage<{ who: string }>({
    defaultMessage: " · drafted by {who}",
    id: "LmNULy",
    description: "After the step on a card, when someone wrote the text. {who} is a member or an agent",
  }),
  details: defineMessage<{ n: number }>({
    defaultMessage: "Details · {n, plural, one {# source} other {# sources}}",
    id: "deTA2z",
    description: "Opens a card's contact details and the pages its research drew on",
  }),
  withFlags: defineMessage({
    defaultMessage: " · flags",
    id: "X6CYIK",
    description: "After Details on a card when the research flagged something",
  }),
};

const stepLabels = defineMessages({
  call: { defaultMessage: "Calls", id: "9EV7uU", description: "Filter on the Today page: calls to make" },
  accepted: { defaultMessage: "Messages", id: "waRVnL", description: "Filter on the Today page: LinkedIn messages to send" },
  email: { defaultMessage: "Emails", id: "OsvY74", description: "Filter on the Today page: emails to send" },
  followup: { defaultMessage: "Follow-ups", id: "yJOtz6", description: "Filter on the Today page: follow-ups to send" },
  linkedin: { defaultMessage: "LinkedIn adds", id: "4dHiRC", description: "Filter on the Today page: people to add on LinkedIn" },
  next: { defaultMessage: "Next steps", id: "0+Wiez", description: "Filter on the Today page: conversations needing a next step" },
});

const stepCards = defineMessages({
  call: { defaultMessage: "Call", id: "FZDrq9", description: "Top of a card on the Today page: a call to make" },
  accepted: { defaultMessage: "Message", id: "loW2RH", description: "Top of a card on the Today page: a LinkedIn message to send" },
  email: { defaultMessage: "Email", id: "6/S/4s", description: "Top of a card on the Today page: an email to send" },
  followup: { defaultMessage: "Follow-up", id: "XbHScK", description: "Top of a card on the Today page: a follow-up to send" },
  linkedin: { defaultMessage: "LinkedIn add", id: "T6AkIy", description: "Top of a card on the Today page: someone to add on LinkedIn" },
  next: { defaultMessage: "Next step", id: "SNTPLC", description: "Top of a card on the Today page: a conversation needing a next step" },
});

const steps: Step[] = ["call", "accepted", "email", "followup", "linkedin", "next"];

const stageNames = defineMessages({
  replied: { defaultMessage: "Replied", id: "blZ73x", description: "A conversation's stage: they replied" },
  meeting: { defaultMessage: "Meeting", id: "PYDd6K", description: "A conversation's stage: a meeting is booked or held" },
  proposal: { defaultMessage: "Proposal", id: "h9vXAR", description: "A conversation's stage: a proposal is out" },
  won: { defaultMessage: "Won", id: "5c9azL", description: "A conversation's stage: won" },
  lost: { defaultMessage: "Lost", id: "vDbQhP", description: "A conversation's stage: lost" },
});

type Stage = keyof typeof stageNames;
const stages = Object.keys(stageNames) as Stage[];
const stageOf = (stage: string): Stage => (stages.includes(stage as Stage) ? (stage as Stage) : "replied");

// The server's page shows this many cards; the rest wait, best first.
const pageSize = 60;

// The name is a name: the same in every language.
const linkedIn = "LinkedIn";

// Marks between words, not words themselves.
const dash = "—";
const slash = "/";
const dot = " · ";
const arrow = "→";
const chevron = "›";

// The keys on LinkedIn adds, the same in every language: open the profile and copy the note, mark it added, skip it.
const keys = { open: "o", added: "a", skip: "s" };

const two = (n: number) => String(n).padStart(2, "0");

function clock(d: Date) {
  return intl.formatMessage(v.now, {
    day: intl.formatDate(d, { weekday: "short", day: "numeric", month: "short" }),
    time: intl.formatTime(d, { hour: "2-digit", minute: "2-digit" }),
  });
}

// today is the reader's own date, as a calendar date (YYYY-MM-DD).
const today = (d = new Date()) => `${d.getFullYear()}-${two(d.getMonth() + 1)}-${two(d.getDate())}`;

const typing = () => /INPUT|TEXTAREA|SELECT/.test(document.activeElement?.tagName || "");

// The clipboard API exists only on HTTPS and localhost; on a plain-HTTP LAN address
// the older execCommand copy still works inside the click.
function writeClipboard(text: string) {
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

type LinkTo = (props: { className: string; children: ReactNode }) => ReactNode;
type Todo = { label: string; count: number; link: LinkTo };

export function TodayPage() {
  const say = useIntl().formatMessage;
  const { org, sales } = useSession();
  const { step: filter } = useSearch({ from: "/o/$org/today" });
  const list = $api.useQuery(
    "get",
    "/o/{org}/api/v1/today",
    { params: { path: { org }, query: { step: filter, limit: pageSize } } },
    { placeholderData: keepPreviousData },
  );
  const summary = $api.useQuery("get", "/o/{org}/api/v1/today/summary", { params: { path: { org } } });
  const client = useQueryClient();
  const [focus, setFocus] = useState(0);
  const [now, setNow] = useState(() => new Date());

  // A page left open stays current, as the server's page does, unless someone is typing.
  useEffect(() => {
    const timer = setInterval(() => {
      if (typing()) return;
      setNow(new Date());
      client.invalidateQueries();
    }, 60000);
    return () => clearInterval(timer);
  }, [client]);

  // On LinkedIn adds: o opens the first card's profile and copies its note, a marks it added, s skips.
  // Until a changed filter's list comes, the first card is still the old list's.
  const stale = useRef(false);
  useLayoutEffect(() => {
    stale.current = list.isPlaceholderData;
  });
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || typing() || stale.current) return;
      const button = document.querySelector<HTMLElement>(`#today-cards > :first-child [data-shortcut="${e.key}"]`);
      if (button) {
        e.preventDefault();
        button.click();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);

  const counts = list.data?.counts ?? {};
  const total = list.data?.total ?? 0;
  const cards = list.data?.items ?? [];
  const shown = list.data?.shown ?? 0;
  const later = list.data?.later_calls ?? 0;
  const s = summary.data;

  const todo: Todo[] = [];
  const add = (label: MessageDescriptor, count: number, link: LinkTo) => count > 0 && todo.push({ label: say(label), count, link });
  const step =
    (which: Step): LinkTo =>
    (p) => <Link to="/o/$org/today" params={{ org }} search={{ step: which }} {...p} />;
  const page =
    (to: "/o/$org/conversations" | "/o/$org/subscriptions" | "/o/$org/onboarding" | "/o/$org/research"): LinkTo =>
    (p) => <Link to={to} params={{ org }} {...p} />;
  if (list.data && s) {
    add(m.answerEnquiries, s.enquiries, (p) => <Link to="/o/$org/enquiries" params={{ org }} search={{ state: "open" }} {...p} />);
    add(m.setNextSteps, counts.next ?? 0, step("next"));
    add(m.moveDeals, s.deals, page("/o/$org/conversations"));
    add(m.lookAtSubscriptions, s.subscriptions, page("/o/$org/subscriptions"));
    add(m.moveOnboarding, s.onboarding_steps, page("/o/$org/onboarding"));
    add(m.makeCalls, (counts.call ?? 0) - later, step("call"));
    add(m.sendMessages, counts.accepted ?? 0, step("accepted"));
    add(m.sendEmails, counts.email ?? 0, step("email"));
    add(m.sendFollowups, counts.followup ?? 0, step("followup"));
    add(m.addOnLinkedIn, counts.linkedin ?? 0, step("linkedin"));
    add(m.reviewResearched, s.review, page("/o/$org/research"));
  }

  const marked = async (next: number) => {
    await client.invalidateQueries();
    if (next) setFocus(next);
  };

  return (
    <>
      <PageHead title={say(m.title)}>
        <span className="font-mono text-xs text-muted-foreground">{clock(now)}</span>
      </PageHead>
      <Whose area="sales" said={say(m.whose)} can={sales} />
      <div className="flex flex-wrap items-start gap-8">
        <section className="flex min-w-0 flex-[999_1_560px] flex-col gap-4">
          <p className="text-sm text-muted-foreground">
            <FormattedMessage
              defaultMessage="{count}{n, plural, one {thing due} other {things due}}, best first: next steps, calls where it's office hours, messages, emails, follow-ups, then LinkedIn adds."
              id="+PizFY"
              description="Top of the Today page. {count} is the number of things due, drawn with its own space after it; {n} is the same number"
              values={{
                n: total,
                count: (
                  <span className="tag mr-1.5 text-sm">
                    <Text value={list.error ? total : list.data?.total} w="2ch" />
                  </span>
                ),
              }}
            />
          </p>
          <nav className="flex flex-wrap gap-2" aria-label={say(m.filterByStep)}>
            <FilterChip to="/o/$org/today" params={{ org }} label={say(m.all)} count={total} on={!filter} />
            {steps
              .filter((st) => (counts[st] ?? 0) > 0 || filter === st)
              .map((st) => (
                <FilterChip
                  key={st}
                  to="/o/$org/today"
                  params={{ org }}
                  search={{ step: st }}
                  label={say(stepLabels[st])}
                  count={counts[st] ?? 0}
                  on={filter === st}
                />
              ))}
          </nav>
          {filter === "linkedin" && sales && (
            <p className="font-mono text-xs text-muted-foreground">
              <FormattedMessage
                defaultMessage="Keys: {o} open profile and copy the note · {a} added · {s} skip"
                id="8a6W7E"
                description="Keyboard shortcuts on the LinkedIn adds. {o}, {a} and {s} are the keys themselves, which stay the same"
                values={{
                  o: <kbd className="text-foreground">{keys.open}</kbd>,
                  a: <kbd className="text-foreground">{keys.added}</kbd>,
                  s: <kbd className="text-foreground">{keys.skip}</kbd>,
                }}
              />
            </p>
          )}
          <Failed errors={[list.error, summary.error]} />
          {list.data && cards.length === 0 && <Empty title={say(m.nothingDue)} hint={say(m.nothingDueHint)} />}
          {!list.data && !list.error && (
            <Placeholders className="flex flex-col gap-4">
              {Array.from({ length: placeholders }, (_, i) => (
                <TodayCard key={i} index={i} />
              ))}
            </Placeholders>
          )}
          <div id="today-cards" className="flex flex-col gap-4">
            {cards.map((c, i) => (
              <TodayCard key={`${c.step}-${c.id}`} c={c} index={i} focused={focus === c.id} onMarked={marked} />
            ))}
          </div>
          {shown > cards.length && <Meta>{say(v.showing, { shown: cards.length, total: shown })}</Meta>}
        </section>
        <aside className="min-w-0 flex-[1_1_260px]">
          {/* The Do now list's length decides where the panels under it start, so the column shows once it is all in. */}
          <Settled className="flex flex-col gap-4">
            <div className="panel flex flex-col gap-1 p-5">
              <Eyebrow>{say(m.doNow)}</Eyebrow>
              {list.data && s && todo.length === 0 && <p className="pt-1.5 text-sm text-muted-foreground">{say(m.nothingNow)}</p>}
              {todo.map((t) => (
                <DoNow key={t.label} t={t} />
              ))}
            </div>
            {/* With the summary too, so it comes in with the Do now list rather than after it. */}
            {(s || summary.error) && later > 0 && (
              <div className="panel flex flex-col gap-2.5 p-5">
                <Eyebrow>{say(m.later)}</Eyebrow>
                <p className="text-sm text-muted-foreground">{say(v.laterCalls, { n: later })}</p>
              </div>
            )}
            {(s || !summary.error) && (
              <div className="panel flex flex-col gap-3 p-5">
                <Eyebrow>{say(m.lastWeek)}</Eyebrow>
                <div className="grid grid-cols-2 gap-3">
                  <Stat n={s?.week.touches} label={say(m.touches)} />
                  <Stat n={s?.week.replies} label={say(m.replies)} />
                  <Stat n={s?.week.meetings} label={say(m.meetings)} />
                  <Stat n={s?.week.researched} label={say(m.researched)} />
                </div>
              </div>
            )}
          </Settled>
        </aside>
      </div>
    </>
  );
}

// DoNow is one thing to do, linking to where it is done, or its outline while they load.
function DoNow({ t }: { t?: Todo }) {
  const className =
    "group flex items-baseline justify-between gap-3 border-b border-dashed border-border py-2 text-sm text-foreground no-underline last:border-0 hover:text-mark";
  const inner = (
    <>
      <span>
        <Text value={t?.label} w="20ch" />
      </span>
      <span className="flex items-baseline gap-2">
        <span className="font-mono text-lg text-mark">
          <Text value={t?.count} w="2ch" />
        </span>
        <span className="text-muted-foreground group-hover:text-mark">{arrow}</span>
      </span>
    </>
  );
  return t ? t.link({ className, children: inner }) : <div className={className}>{inner}</div>;
}

function Stat({ n, label }: { n?: number; label: string }) {
  return (
    <div>
      <div className="font-mono text-2xl">
        <Text value={n} w="2ch" />
      </div>
      <Meta>{label}</Meta>
    </div>
  );
}

// Shown is a value someone may read but not change.
function Shown({ value, none }: { value: string; none: string }) {
  return value ? <span className="whitespace-pre-wrap text-sm">{value}</span> : <span className="text-sm text-muted-foreground">{none}</span>;
}

function Quote({ children }: { children: ReactNode }) {
  return <div className="whitespace-pre-wrap rounded-lg border-l-2 border-primary bg-muted px-4 py-3 text-[15px] leading-relaxed">{children}</div>;
}

function Actions({ children }: { children: ReactNode }) {
  return <div className="flex flex-wrap items-center gap-2">{children}</div>;
}

// Field saves when it is left changed, as a form field's change event does.
function Field({ label, value, multi, onSave }: { label: string; value: string; multi?: boolean; onSave: (v: string) => void }) {
  const { sales } = useSession();
  const save = (next: string) => next !== value && onSave(next);
  return (
    <>
      <Eyebrow>{label}</Eyebrow>
      {!sales ? (
        <Shown value={value} none={dash} />
      ) : multi ? (
        <Textarea key={value} defaultValue={value} placeholder={label} rows={3} aria-label={label} className="bg-muted" onBlur={(e) => save(e.target.value)} />
      ) : (
        <Input key={value} defaultValue={value} placeholder={dash} aria-label={label} className="h-8 bg-muted" onBlur={(e) => save(e.target.value)} />
      )}
    </>
  );
}

function CopyButton({ label, text, open, shortcut }: { label: string; text: () => string; open?: string; shortcut?: string }) {
  const say = useIntl().formatMessage;
  const [copied, setCopied] = useState(false);
  return (
    <ActionButton
      size="sm"
      data-shortcut={shortcut}
      onClick={() => {
        writeClipboard(text());
        if (open && /^https?:\/\//.test(open)) window.open(open, "linkedin");
        setCopied(true);
      }}
    >
      {copied ? say(m.copied) : label}
    </ActionButton>
  );
}

// TodayCard is one thing due, or its outline while the list loads: the name and company,
// and the details bar. What it asks of you depends on its step, and comes with it.
function TodayCard({ c, index, focused = false, onMarked }: { c?: DueCard; index: number; focused?: boolean; onMarked?: (next: number) => void }) {
  const say = useIntl().formatMessage;
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const self = useRef<HTMLDivElement>(null);
  const text = useRef<HTMLTextAreaElement>(null);
  const change = $api.useMutation("patch", "/o/{org}/api/v1/contacts/{id}");
  const mark = $api.useMutation("post", "/o/{org}/api/v1/marks", { onSuccess: () => onMarked?.(c?.next ?? 0) });

  useEffect(() => {
    if (focused) self.current?.scrollIntoView({ block: "start", behavior: "smooth" });
  }, [focused]);

  const save = (field: keyof ContactChange, value: string, again = false) =>
    c && change.mutate({ params: { path: { org, id: c.id } }, body: { [field]: value } as ContactChange }, { onSuccess: again ? () => refresh() : undefined });

  let body: ReactNode;
  if (c) {
    // On a LinkedIn add, a marks it added and s skips it.
    const shortcut = (step: MarkStep) =>
      c.step === "linkedin" ? ({ linkedin: keys.added, hold: keys.skip } as Partial<Record<MarkStep, string>>)[step] : undefined;
    const markButton = (step: MarkStep, label: MessageDescriptor, primary = false) =>
      sales && (
        <ActionButton
          key={step}
          size="sm"
          quiet={!primary}
          data-shortcut={shortcut(step)}
          disabled={mark.isPending}
          onClick={() => mark.mutate({ params: { path: { org } }, body: { contact_id: c.id, step } })}
        >
          {say(label)}
        </ActionButton>
      );

    const draft = (field: "note" | "message", rows: number) =>
      sales ? (
        <Textarea
          ref={text}
          key={c.text}
          defaultValue={c.text}
          rows={rows}
          aria-label={say(field === "note" ? m.note : m.message)}
          className="bg-muted text-[15px] leading-relaxed"
          onBlur={(e) => e.target.value !== c.text && save(field, e.target.value)}
        />
      ) : (
        <Quote>{c.text}</Quote>
      );
    const drafted = () => text.current?.value ?? c.text;

    switch (c.step) {
      case "call":
        body = (
          <>
            <div>
              <a
                className="font-mono text-2xl font-medium tracking-tight text-foreground underline decoration-primary decoration-4 underline-offset-4 hover:decoration-coral"
                href={"tel:" + c.tel}
              >
                {c.number}
              </a>
              {c.about && <p className="text-sm text-muted-foreground">{c.about}</p>}
              <div className="mt-1">
                <Meta>{c.meta}</Meta>
              </div>
            </div>
            <details className="rounded-lg border-l-2 border-primary bg-muted px-4 py-3">
              <summary className="cursor-pointer font-mono text-xs uppercase tracking-wider text-muted-foreground">{say(m.callScript)}</summary>
              {c.script.map((l) => (
                <div key={l.cue} className="mt-3">
                  <div className="font-mono text-[11px] uppercase tracking-wider text-muted-foreground">{l.cue}</div>
                  <p className="mt-0.5">{l.say}</p>
                </div>
              ))}
            </details>
            <div className="grid grid-cols-[5.5rem_1fr] items-start gap-3 text-sm">
              <Field label={say(m.notes)} value={c.call_notes} multi onSave={(next) => save("call_notes", next)} />
            </div>
            <Actions>
              {markButton("call", m.called, true)}
              {markButton("replied", m.interested)}
              {markButton("declined", m.notInterested)}
            </Actions>
          </>
        );
        break;
      case "linkedin":
        body = (
          <>
            {draft("note", 5)}
            <Actions>
              <CopyButton label={c.open_label} text={drafted} open={c.open_url} shortcut={keys.open} />
              {markButton("linkedin", m.added)}
              {markButton("hold", m.skip)}
              <span className="ml-auto">
                <Meta>{say(v.chars, { n: [...c.text].length, max: 300 })}</Meta>
              </span>
            </Actions>
          </>
        );
        break;
      case "accepted":
        body = (
          <>
            <Meta>{c.meta}</Meta>
            {draft("message", 10)}
            <Actions>
              <CopyButton label={c.open_label} text={drafted} open={c.open_url} />
              {markButton("email", m.sent)}
              {markButton("replied", m.replied)}
            </Actions>
          </>
        );
        break;
      case "next":
        body = (
          <>
            <Meta>{c.meta}</Meta>
            <div className="grid grid-cols-[5.5rem_1fr] items-center gap-x-3 gap-y-2 text-sm">
              <Field label={say(m.nextStep)} value={c.next_step} onSave={(next) => save("next_step", next)} />
              <Eyebrow>{say(m.by)}</Eyebrow>
              {sales ? (
                <Input
                  type="date"
                  key={c.next_date}
                  defaultValue={c.next_date}
                  aria-label={say(m.by)}
                  className="h-8 w-44 bg-muted"
                  onChange={(e) => save("next_date", e.target.value)}
                />
              ) : (
                <Shown value={c.next_date && onDay(c.next_date, today())} none={say(m.noDate)} />
              )}
            </div>
            <Actions>
              {sales ? (
                <label className="flex items-center gap-2">
                  <Eyebrow>{say(m.stage)}</Eyebrow>
                  <select
                    value={c.stage || "replied"}
                    onChange={(e) => save("stage", e.target.value, true)}
                    className="h-8 rounded-lg border border-input bg-muted px-2 text-sm"
                  >
                    {stages.map((st) => (
                      <option key={st} value={st}>
                        {say(stageNames[st])}
                      </option>
                    ))}
                  </select>
                </label>
              ) : (
                <span className="flex items-center gap-2">
                  <Eyebrow>{say(m.stage)}</Eyebrow>
                  <Shown value={say(stageNames[stageOf(c.stage)])} none="" />
                </span>
              )}
            </Actions>
          </>
        );
        break;
      default:
        body = (
          <>
            <Meta>{c.meta}</Meta>
            <Quote>{c.text}</Quote>
            <Actions>
              <CopyButton label={say(m.copy)} text={() => c.copy} />
              {markButton(c.step, m.sent)}
              {markButton("replied", m.replied)}
              {c.step === "email" && markButton("accepted", m.acceptedInstead)}
            </Actions>
          </>
        );
    }
  }

  return (
    <RailCard ref={self} state={c?.state ?? ""} held={c?.held} index={index} className={focused ? "ring-2 ring-primary" : undefined}>
      <div className="flex flex-col gap-4 px-5 py-5 sm:px-6">
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0">
            <div className="font-mono text-[11px] font-medium uppercase tracking-wider">
              <span className="tag">
                <Text value={c && say(stepCards[c.step])} w="7ch" />
              </span>
              {c?.draft_by && <span className="text-muted-foreground">{say(v.draftedBy, { who: actor(c.draft_by) })}</span>}
            </div>
            <h2 className="mt-2 font-heading text-2xl font-bold leading-tight tracking-tight">
              <Text value={c?.name} w="12ch" />
            </h2>
            <p className="mt-0.5 truncate text-sm text-muted-foreground">
              {!c ? (
                <Bar w="24ch" />
              ) : c.company_id ? (
                <Link className="text-foreground/90 underline underline-offset-2 hover:text-mark" to="/o/$org/companies/$id" params={{ org, id: c.company_id }}>
                  {c.company}
                </Link>
              ) : (
                <span className="text-foreground/90">{c.company}</span>
              )}
              {c?.title && (
                <>
                  <span className="mx-1.5 text-border">{slash}</span>
                  {c.title}
                </>
              )}
            </p>
          </div>
          {c?.fit && (
            <span
              className="shrink-0 rounded-md border-2 border-foreground bg-primary px-2 py-0.5 font-mono text-xs font-medium text-primary-foreground"
              title={say(m.fit)}
            >
              {c.fit}
            </span>
          )}
        </div>
        {c && c.people.length > 1 && sales && (
          <label className="flex items-center gap-3">
            <Eyebrow>{say(m.sendTo)}</Eyebrow>
            <select
              value={c.name}
              onChange={(e) => save("send_to", e.target.value, true)}
              className="h-8 flex-1 rounded-lg border border-input bg-muted px-2 text-sm"
            >
              {c.people.map((p) => (
                <option key={p.name} value={p.name}>
                  {p.name}
                  {p.title && dot + p.title}
                </option>
              ))}
            </select>
          </label>
        )}
        {c && c.step !== "next" && (
          <div className="flex flex-wrap gap-1.5">
            {c.badges.map((b) => (
              <Pill key={b.text} tone={b.state}>
                {b.text}
              </Pill>
            ))}
          </div>
        )}
        {body}
        <Failed errors={[change.error, mark.error]} />
      </div>
      <details className="group border-t border-border/70 bg-muted">
        <summary className="flex cursor-pointer list-none items-center gap-2 px-5 py-2.5 font-mono text-xs text-muted-foreground hover:text-foreground sm:px-6">
          <span className="transition-transform group-open:rotate-90">{chevron}</span>
          <Text value={c && say(v.details, { n: c.sources.length })} w="16ch" />
          {c?.flags && say(v.withFlags)}
        </summary>
        {c && (
          <div className="flex flex-col gap-4 px-5 pb-5 sm:px-6">
            <div className="grid grid-cols-[5.5rem_1fr] items-center gap-x-3 gap-y-2 text-sm">
              <Field label={say(m.email)} value={c.email} onSave={(next) => save("email", next)} />
              <Field label={say(m.phone)} value={c.phone} onSave={(next) => save("phone", next, true)} />
              <Field label={linkedIn} value={c.linkedin} onSave={(next) => save("linkedin", next)} />
              <Field label={say(m.timeZone)} value={c.tz} onSave={(next) => save("tz", next)} />
            </div>
            {c.sources.length > 0 && (
              <div>
                <Eyebrow>{say(m.sources)}</Eyebrow>
                <ol className="mt-1.5 flex list-decimal flex-col gap-1 pl-5 text-sm marker:font-mono marker:text-xs marker:text-muted-foreground">
                  {c.sources.map((u) => (
                    <li key={u}>
                      <a className="break-all text-mark underline-offset-4 hover:text-mark hover:underline" href={safeHref(u)} target="_blank" rel="noopener">
                        {u}
                      </a>
                    </li>
                  ))}
                </ol>
              </div>
            )}
            {c.flags && (
              <div>
                <Eyebrow>{say(m.flags)}</Eyebrow>
                <div className="mt-1.5 whitespace-pre-wrap text-sm text-muted-foreground">{c.flags}</div>
              </div>
            )}
          </div>
        )}
      </details>
    </RailCard>
  );
}
