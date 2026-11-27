import { keepPreviousData, useQueryClient } from "@tanstack/react-query";
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { Link, useSearch } from "@tanstack/react-router";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Empty, Eyebrow, Failed, FilterChip, Meta, PageHead, Pill, RailCard, useRefresh, Whose } from "@/components/app";
import { Bar, Placeholders, placeholders, Settled, Text } from "@/components/app/skeleton";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { actor, plural } from "@/lib/format";
import { safeHref } from "@/lib/links";
import { useSession } from "@/session";

type DueCard = components["schemas"]["DueCard"];
type Step = DueCard["step"];
type MarkStep = components["schemas"]["MarkNew"]["step"];
type ContactChange = components["schemas"]["ContactChange"];

const steps: { key: Step; label: string; card: string }[] = [
  { key: "call", label: "Calls", card: "Call" },
  { key: "accepted", label: "Messages", card: "Message" },
  { key: "email", label: "Emails", card: "Email" },
  { key: "followup", label: "Follow-ups", card: "Follow-up" },
  { key: "linkedin", label: "LinkedIn adds", card: "LinkedIn add" },
  { key: "next", label: "Next steps", card: "Next step" },
];

const stages = [
  { key: "replied", label: "Replied" },
  { key: "meeting", label: "Meeting" },
  { key: "proposal", label: "Proposal" },
  { key: "won", label: "Won" },
  { key: "lost", label: "Lost" },
];

// The server's page shows this many cards; the rest wait, best first.
const pageSize = 60;

const days = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

function clock(d: Date) {
  const two = (n: number) => String(n).padStart(2, "0");
  return `${days[d.getDay()]} ${d.getDate()} ${months[d.getMonth()]} · ${two(d.getHours())}:${two(d.getMinutes())}`;
}

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
  const add = (label: string, count: number, link: LinkTo) => count > 0 && todo.push({ label, count, link });
  const step =
    (step: Step): LinkTo =>
    (p) => <Link to="/o/$org/today" params={{ org }} search={{ step }} {...p} />;
  const page =
    (to: "/o/$org/conversations" | "/o/$org/subscriptions" | "/o/$org/onboarding" | "/o/$org/research"): LinkTo =>
    (p) => <Link to={to} params={{ org }} {...p} />;
  if (list.data && s) {
    add("Answer enquiries", s.enquiries, (p) => <Link to="/o/$org/enquiries" params={{ org }} search={{ state: "open" }} {...p} />);
    add("Set next steps", counts.next ?? 0, step("next"));
    add("Move renewals and other deals on", s.deals, page("/o/$org/conversations"));
    add("Look at subscriptions coming due", s.subscriptions, page("/o/$org/subscriptions"));
    add("Move onboarding steps on", s.onboarding_steps, page("/o/$org/onboarding"));
    add("Make calls now", (counts.call ?? 0) - later, step("call"));
    add("Send LinkedIn messages", counts.accepted ?? 0, step("accepted"));
    add("Send emails", counts.email ?? 0, step("email"));
    add("Send follow-ups", counts.followup ?? 0, step("followup"));
    add("Add on LinkedIn", counts.linkedin ?? 0, step("linkedin"));
    add("Review researched companies", s.review, page("/o/$org/research"));
  }

  const marked = async (next: number) => {
    await client.invalidateQueries();
    if (next) setFocus(next);
  };

  return (
    <>
      <PageHead title="Today">
        <span className="font-mono text-xs text-muted-foreground">{clock(now)}</span>
      </PageHead>
      <Whose area="sales" work="Outreach is" can={sales} />
      <div className="flex flex-wrap items-start gap-8">
        <section className="flex min-w-0 flex-[999_1_560px] flex-col gap-4">
          <p className="text-sm text-muted-foreground">
            <span className="tag mr-1.5 text-sm">
              <Text value={list.error ? total : list.data?.total} w="2ch" />
            </span>
            things due, best first: next steps, calls where it's office hours, messages, emails, follow-ups, then LinkedIn adds.
          </p>
          <nav className="flex flex-wrap gap-2" aria-label="Filter by step">
            <FilterChip to="/o/$org/today" params={{ org }} label="All" count={total} on={!filter} />
            {steps
              .filter((st) => (counts[st.key] ?? 0) > 0 || filter === st.key)
              .map((st) => (
                <FilterChip
                  key={st.key}
                  to="/o/$org/today"
                  params={{ org }}
                  search={{ step: st.key }}
                  label={st.label}
                  count={counts[st.key] ?? 0}
                  on={filter === st.key}
                />
              ))}
          </nav>
          {filter === "linkedin" && sales && (
            <p className="font-mono text-xs text-muted-foreground">
              Keys: <kbd className="text-foreground">o</kbd> open profile and copy the note · <kbd className="text-foreground">a</kbd> added ·{" "}
              <kbd className="text-foreground">s</kbd> skip
            </p>
          )}
          <Failed errors={[list.error, summary.error]} />
          {list.data && cards.length === 0 && (
            <Empty title="Nothing due here" hint="Add someone from LinkedIn, or check Research for companies waiting on you." />
          )}
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
          {shown > cards.length && <Meta>{`Showing the first ${cards.length} of ${shown}. Clear some, or filter by step above.`}</Meta>}
        </section>
        <aside className="min-w-0 flex-[1_1_260px]">
          {/* The Do now list's length decides where the panels under it start, so the column shows once it is all in. */}
          <Settled className="flex flex-col gap-4">
            <div className="panel flex flex-col gap-1 p-5">
              <Eyebrow>Do now</Eyebrow>
              {list.data && s && todo.length === 0 && <p className="pt-1.5 text-sm text-muted-foreground">Nothing due right now.</p>}
              {todo.map((t) => (
                <DoNow key={t.label} t={t} />
              ))}
            </div>
            {/* With the summary too, so it comes in with the Do now list rather than after it. */}
            {(s || summary.error) && later > 0 && (
              <div className="panel flex flex-col gap-2.5 p-5">
                <Eyebrow>Later</Eyebrow>
                <p className="text-sm text-muted-foreground">{plural(later, "call")} outside office hours right now</p>
              </div>
            )}
            {(s || !summary.error) && (
              <div className="panel flex flex-col gap-3 p-5">
                <Eyebrow>Last 7 days</Eyebrow>
                <div className="grid grid-cols-2 gap-3">
                  <Stat n={s?.week.touches} label="touches" />
                  <Stat n={s?.week.replies} label="replies" />
                  <Stat n={s?.week.meetings} label="meetings" />
                  <Stat n={s?.week.researched} label="researched" />
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
        <span className="text-muted-foreground group-hover:text-mark">→</span>
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
  const save = (v: string) => v !== value && onSave(v);
  return (
    <>
      <Eyebrow>{label}</Eyebrow>
      {!sales ? (
        <Shown value={value} none="—" />
      ) : multi ? (
        <Textarea key={value} defaultValue={value} placeholder={label} rows={3} aria-label={label} className="bg-muted" onBlur={(e) => save(e.target.value)} />
      ) : (
        <Input key={value} defaultValue={value} placeholder="—" aria-label={label} className="h-8 bg-muted" onBlur={(e) => save(e.target.value)} />
      )}
    </>
  );
}

function CopyButton({ label, text, open, shortcut }: { label: string; text: () => string; open?: string; shortcut?: string }) {
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
      {copied ? "Copied ✓" : label}
    </ActionButton>
  );
}

// TodayCard is one thing due, or its outline while the list loads: the name and company,
// and the details bar. What it asks of you depends on its step, and comes with it.
function TodayCard({ c, index, focused = false, onMarked }: { c?: DueCard; index: number; focused?: boolean; onMarked?: (next: number) => void }) {
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
    const shortcut = (step: MarkStep) => (c.step === "linkedin" ? ({ linkedin: "a", hold: "s" } as Partial<Record<MarkStep, string>>)[step] : undefined);
    const markButton = (step: MarkStep, label: string, primary = false) =>
      sales && (
        <ActionButton
          key={step}
          size="sm"
          quiet={!primary}
          data-shortcut={shortcut(step)}
          disabled={mark.isPending}
          onClick={() => mark.mutate({ params: { path: { org } }, body: { contact_id: c.id, step } })}
        >
          {label}
        </ActionButton>
      );

    const draft = (field: "note" | "message", rows: number) =>
      sales ? (
        <Textarea
          ref={text}
          key={c.text}
          defaultValue={c.text}
          rows={rows}
          aria-label={field === "note" ? "Note" : "Message"}
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
              <summary className="cursor-pointer font-mono text-xs uppercase tracking-wider text-muted-foreground">Call script</summary>
              {c.script.map((l) => (
                <div key={l.cue} className="mt-3">
                  <div className="font-mono text-[11px] uppercase tracking-wider text-muted-foreground">{l.cue}</div>
                  <p className="mt-0.5">{l.say}</p>
                </div>
              ))}
            </details>
            <div className="grid grid-cols-[5.5rem_1fr] items-start gap-3 text-sm">
              <Field label="Notes" value={c.call_notes} multi onSave={(v) => save("call_notes", v)} />
            </div>
            <Actions>
              {markButton("call", "Called", true)}
              {markButton("replied", "Interested")}
              {markButton("declined", "Not interested")}
            </Actions>
          </>
        );
        break;
      case "linkedin":
        body = (
          <>
            {draft("note", 5)}
            <Actions>
              <CopyButton label={c.open_label} text={drafted} open={c.open_url} shortcut="o" />
              {markButton("linkedin", "Added")}
              {markButton("hold", "Skip")}
              <span className="ml-auto">
                <Meta>{`${[...c.text].length} / 300 chars`}</Meta>
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
              {markButton("email", "Sent")}
              {markButton("replied", "Replied")}
            </Actions>
          </>
        );
        break;
      case "next":
        body = (
          <>
            <Meta>{c.meta}</Meta>
            <div className="grid grid-cols-[5.5rem_1fr] items-center gap-x-3 gap-y-2 text-sm">
              <Field label="Next step" value={c.next_step} onSave={(v) => save("next_step", v)} />
              <Eyebrow>By</Eyebrow>
              {sales ? (
                <Input
                  type="date"
                  key={c.next_date}
                  defaultValue={c.next_date}
                  aria-label="By"
                  className="h-8 w-44 bg-muted"
                  onChange={(e) => save("next_date", e.target.value)}
                />
              ) : (
                <Shown value={c.next_date} none="No date" />
              )}
            </div>
            <Actions>
              {sales ? (
                <label className="flex items-center gap-2">
                  <Eyebrow>Stage</Eyebrow>
                  <select
                    value={c.stage || "replied"}
                    onChange={(e) => save("stage", e.target.value, true)}
                    className="h-8 rounded-lg border border-input bg-muted px-2 text-sm"
                  >
                    {stages.map((st) => (
                      <option key={st.key} value={st.key}>
                        {st.label}
                      </option>
                    ))}
                  </select>
                </label>
              ) : (
                <span className="flex items-center gap-2">
                  <Eyebrow>Stage</Eyebrow>
                  <Shown value={stages.find((st) => st.key === c.stage)?.label ?? "Replied"} none="" />
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
              <CopyButton label="Copy" text={() => c.copy} />
              {markButton(c.step, "Sent")}
              {markButton("replied", "Replied")}
              {c.step === "email" && markButton("accepted", "Accepted instead")}
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
                <Text value={c && (steps.find((st) => st.key === c.step)?.card ?? c.step)} w="7ch" />
              </span>
              {c?.draft_by && <span className="text-muted-foreground"> · drafted by {actor(c.draft_by)}</span>}
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
                  <span className="mx-1.5 text-border">/</span>
                  {c.title}
                </>
              )}
            </p>
          </div>
          {c?.fit && (
            <span
              className="shrink-0 rounded-md border-2 border-foreground bg-primary px-2 py-0.5 font-mono text-xs font-medium text-primary-foreground"
              title="Fit"
            >
              {c.fit}
            </span>
          )}
        </div>
        {c && c.people.length > 1 && sales && (
          <label className="flex items-center gap-3">
            <Eyebrow>Send to</Eyebrow>
            <select
              value={c.name}
              onChange={(e) => save("send_to", e.target.value, true)}
              className="h-8 flex-1 rounded-lg border border-input bg-muted px-2 text-sm"
            >
              {c.people.map((p) => (
                <option key={p.name} value={p.name}>
                  {p.name}
                  {p.title && " · " + p.title}
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
          <span className="transition-transform group-open:rotate-90">›</span>
          <Text value={c && "Details · " + plural(c.sources.length, "source")} w="16ch" />
          {c?.flags && " · flags"}
        </summary>
        {c && (
          <div className="flex flex-col gap-4 px-5 pb-5 sm:px-6">
            <div className="grid grid-cols-[5.5rem_1fr] items-center gap-x-3 gap-y-2 text-sm">
              <Field label="Email" value={c.email} onSave={(v) => save("email", v)} />
              <Field label="Phone" value={c.phone} onSave={(v) => save("phone", v, true)} />
              <Field label="LinkedIn" value={c.linkedin} onSave={(v) => save("linkedin", v)} />
              <Field label="Time zone" value={c.tz} onSave={(v) => save("tz", v)} />
            </div>
            {c.sources.length > 0 && (
              <div>
                <Eyebrow>Sources to check before sending</Eyebrow>
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
                <Eyebrow>Flags</Eyebrow>
                <div className="mt-1.5 whitespace-pre-wrap text-sm text-muted-foreground">{c.flags}</div>
              </div>
            )}
          </div>
        )}
      </details>
    </RailCard>
  );
}
