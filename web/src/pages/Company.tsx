import { type ComponentProps, type FormEvent, type ReactNode, useEffect, useRef, useState } from "react";
import { Link, useParams } from "@tanstack/react-router";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Eyebrow, Failed, Meta, Pill, type Tone, useRefresh } from "@/components/app";
import { Input } from "@/components/ui/input";
import { actor, plural, stamp } from "@/lib/format";
import { cn } from "@/lib/utils";
import { Pager, StageTag, usePages } from "@/pages/Companies";
import { OnboardingCard, today } from "@/pages/Onboarding";
import { safeHref } from "@/lib/links";
import { useOfferings } from "@/lib/offerings";
import { useSession } from "@/session";

type Overview = components["schemas"]["CompanyOverview"];
type Person = components["schemas"]["CompanyPerson"];
type Pending = components["schemas"]["PendingDetail"];
type Criterion = components["schemas"]["Criterion"];
type Subscription = components["schemas"]["Subscription"];
type SubscriptionChange = components["schemas"]["SubscriptionChange"];
type Agreement = components["schemas"]["Agreement"];

const historySize = 50;
const notesSize = 20;
const field = "h-9 rounded-lg border border-input bg-muted px-3 text-sm";
const select = "h-9 rounded-lg border border-input bg-muted px-2 text-sm";


function save(data: unknown, name: string) {
  const a = document.createElement("a");
  a.href = URL.createObjectURL(new Blob([JSON.stringify(data, null, 2)], { type: "application/json" }));
  a.download = name;
  a.click();
  URL.revokeObjectURL(a.href);
}

// formBody reads a form as the API takes it: number inputs as numbers, empty ones left out.
function formBody(form: HTMLFormElement) {
  const out: Record<string, string | number> = {};
  for (const el of Array.from(form.elements) as HTMLInputElement[]) {
    if (!el.name || el.value === "") continue;
    out[el.name] = el.type === "number" ? Number(el.value) : el.value;
  }
  return out;
}

export function CompanyPage() {
  const { org } = useSession();
  const { id } = useParams({ from: "/o/$org/companies/$id" });
  const overview = $api.useQuery("get", "/o/{org}/api/v1/companies/{id}/overview", { params: { path: { org, id } } });
  const historyPages = usePages(`history:${id}`);
  const notesPages = usePages(`notes:${id}`);
  const history = $api.useQuery("get", "/o/{org}/api/v1/companies/{id}/history", {
    params: { path: { org, id }, query: { limit: historySize, cursor: historyPages.cursor } },
  });
  const notes = $api.useQuery("get", "/o/{org}/api/v1/companies/{id}/notes", {
    params: { path: { org, id }, query: { limit: notesSize, cursor: notesPages.cursor } },
  });

  const back = (
    <Link to="/o/$org/companies" params={{ org }} className="font-mono text-xs">
      ← Companies
    </Link>
  );
  if (overview.error?.status === 404) {
    return (
      <>
        {back}
        <p className="mt-3 text-sm">That company isn't here.</p>
      </>
    );
  }
  // Half a company's page would read as the whole of it, so any part failing fails the page.
  if (overview.error || history.error || notes.error) {
    return (
      <>
        {back}
        <p className="mt-3 text-sm text-[var(--stop)]" role="alert">
          The company couldn't be read. Reload to try again.
        </p>
      </>
    );
  }
  const c = overview.data;
  if (!c || !history.data || !notes.data) return back;
  return (
    <>
      {back}
      <div className="mt-3 flex flex-col gap-5 panel p-6">
        <Head c={c} />
        {c.why && <p className="text-sm text-muted-foreground">{c.why}</p>}
        <WhereItStands c={c} />
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <div className="rounded-lg bg-muted p-3">
            <Eyebrow>Looking for</Eyebrow>
            <LookingFor found={c.looking_for} />
          </div>
          <div className="rounded-lg bg-muted p-3">
            <Eyebrow>Found by</Eyebrow>
            <div className="mt-1 text-sm">{actor(c.found_by)}</div>
          </div>
          <div className="rounded-lg bg-muted p-3">
            <Eyebrow>Touches</Eyebrow>
            <div className="mt-1 font-mono text-lg">{c.touches}</div>
          </div>
        </div>
        <People c={c} />
        <Onboardings c={c} />
        <Subscriptions c={c} />
        <Agreements c={c} />
        {c.facts.length > 0 && (
          <div>
            <Eyebrow>Facts</Eyebrow>
            {c.facts.map((f) => (
              <div key={f.id} className="grid grid-cols-[130px_1fr_auto] items-baseline gap-3 border-b border-dashed border-border py-2 text-sm">
                <span className="font-mono text-xs text-muted-foreground">{f.field}</span>
                <span>{f.value}</span>
                {f.source_url ? (
                  <a href={safeHref(f.source_url)} target="_blank" rel="noopener" className="font-mono text-xs">
                    source ↗
                  </a>
                ) : (
                  <span />
                )}
              </div>
            ))}
          </div>
        )}
        {notes.data.total > 0 && (
          <div>
            <Eyebrow>Agents' notes, unchecked</Eyebrow>
            <Notes notes={notes.data.items} />
            <Pager
              n={notesPages.n}
              total={notes.data.total}
              size={notesSize}
              next={notes.data.next_cursor}
              onNext={notesPages.next}
              onPrevious={notesPages.previous}
            />
          </div>
        )}
        <div>
          <Eyebrow>History</Eyebrow>
          {history.data.items.length === 0 && <p className="py-2 text-sm text-muted-foreground">Nothing recorded yet.</p>}
          {history.data.items.map((h, i) => (
            <div key={`${h.at}-${i}`} className="grid grid-cols-[110px_1fr] gap-3 border-b border-dashed border-border py-2 text-sm">
              <span className="font-mono text-xs text-muted-foreground">{stamp(h.at)}</span>
              <span>
                <span className={cn("font-mono text-xs", h.actor.startsWith("agent:") ? "text-mark" : "text-muted-foreground")}>{actor(h.actor)}</span> {h.text}
              </span>
            </div>
          ))}
          <Pager
            n={historyPages.n}
            total={history.data.total}
            size={historySize}
            next={history.data.next_cursor}
            onNext={historyPages.next}
            onPrevious={historyPages.previous}
          />
        </div>
      </div>
    </>
  );
}

function Head({ c }: { c: Overview }) {
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const review = $api.useMutation("patch", "/o/{org}/api/v1/companies/{id}", { onSuccess: refresh });
  return (
    <div className="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h1 className="font-heading text-3xl font-bold tracking-tight">{c.name}</h1>
        <Meta>{[c.domain, c.country, c.segment, c.grade ? "fit " + c.grade : ""].filter((p) => p.trim()).join(" · ")}</Meta>
      </div>
      <div className="flex items-center gap-2">
        <StageTag stage={c.pipeline} />
        {c.stage !== "qualified" && sales && (
          <ActionButton size="sm" onClick={() => review.mutate({ params: { path: { org, id: c.id } }, body: { stage: "qualified" } })}>
            Qualify
          </ActionButton>
        )}
      </div>
      <Failed errors={[review.error]} />
    </div>
  );
}

const verdictPill: Record<string, [string, Tone]> = {
  review: ["To review", "warn"],
  qualified: ["Qualified", "ok"],
  rejected: ["Junked", "stop"],
};

// WhereItStands is the company's verdict for each offering, shown once there is more
// than the organisation's own: ruled out for one, it can still be right for another.
function WhereItStands({ c }: { c: Overview }) {
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const offerings = useOfferings();
  const settings = $api.useQuery("get", "/o/{org}/api/v1/settings", { params: { path: { org } } });
  const verdicts = $api.useQuery(
    "get",
    "/o/{org}/api/v1/companies/{id}/verdicts",
    { params: { path: { org, id: c.id }, query: { limit: 200 } } },
    { enabled: offerings.length > 0 },
  );
  const review = $api.useMutation("patch", "/o/{org}/api/v1/companies/{id}", { onSuccess: refresh });
  if (offerings.length === 0) return null;
  const judge = (stage: "review" | "qualified" | "rejected", offering?: number) =>
    review.mutate({ params: { path: { org, id: c.id } }, body: { stage, offering } });
  const by = new Map((verdicts.data?.items ?? []).map((v) => [v.offering_id, v]));
  const ownPill: [string, Tone] = c.stage === "rejected" ? ["Rejected", "stop"] : (verdictPill[c.stage] ?? ["", ""]);
  return (
    <div>
      <Eyebrow>Where it stands</Eyebrow>
      <div className="mt-1 flex flex-col">
        <StandingRow name={settings.data?.name ?? "Your own"} pill={c.found_for ? null : ownPill}>
          {sales && c.found_for !== 0 && (
            <ActionButton quiet size="xs" onClick={() => judge("review")}>
              Review for this
            </ActionButton>
          )}
          {sales && !c.found_for && c.stage === "rejected" && (
            <ActionButton quiet size="xs" onClick={() => judge("review")}>
              Put back
            </ActionButton>
          )}
        </StandingRow>
        {offerings.map((o) => {
          const v = by.get(o.id);
          return (
            <StandingRow key={o.id} name={o.name} pill={v ? (verdictPill[v.stage] ?? null) : null} said={v && (v.reason || v.by) ? `${v.reason ? `“${v.reason}” ` : ""}${actor(v.by)}, ${stamp(v.updated_at)}` : ""}>
              {sales && !v && (
                <ActionButton quiet size="xs" onClick={() => judge("review", o.id)}>
                  Review for this
                </ActionButton>
              )}
              {sales && v?.stage === "review" && (
                <ActionButton size="xs" onClick={() => judge("qualified", o.id)}>
                  Qualify
                </ActionButton>
              )}
              {sales && v && v.stage !== "rejected" && (
                <ActionButton quiet size="xs" onClick={() => judge("rejected", o.id)}>
                  Junk
                </ActionButton>
              )}
              {sales && v?.stage === "rejected" && (
                <ActionButton quiet size="xs" onClick={() => judge("review", o.id)}>
                  Undo
                </ActionButton>
              )}
            </StandingRow>
          );
        })}
      </div>
      <Failed errors={[verdicts.error, review.error]} />
    </div>
  );
}

function StandingRow({ name, pill, said, children }: { name: string; pill: [string, Tone] | null; said?: string; children?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-dashed border-border py-2 text-sm">
      <span className="min-w-[140px] font-medium">{name}</span>
      {pill ? <Pill tone={pill[1]}>{pill[0]}</Pill> : <span className="text-muted-foreground">Not looked at</span>}
      {said && <span className="min-w-0 text-xs text-muted-foreground">{said}</span>}
      <span className="ml-auto flex gap-2">{children}</span>
    </div>
  );
}

function assetPill(a: Criterion): [string, Tone] {
  const name = a.field.replaceAll("_", " ");
  if (a.state === "met") return [name, "ok"];
  if (a.state === "not_met") return ["no " + name, "stop"];
  if (a.state === "not_found") return [name + " not found", ""];
  return [name + " unchecked", ""];
}

function LookingFor({ found }: { found: Criterion[] }) {
  const { org, owner } = useSession();
  if (found.length === 0) {
    return (
      <div className="mt-1 text-sm text-muted-foreground">
        {owner ? <Link to="/o/$org/settings" params={{ org }}>Name what you look for</Link> : "Nothing named yet"}
      </div>
    );
  }
  return (
    <div className="mt-1 flex flex-wrap gap-1.5">
      {found.map((a) => {
        const [text, tone] = assetPill(a);
        return (
          <Pill key={a.field} tone={tone}>
            {text}
          </Pill>
        );
      })}
    </div>
  );
}

function eraseConfirm(p: Person) {
  const who = p.several ? `everyone on this row (${p.several})` : p.name;
  return `Erase ${who}? Their details, drafts and notes are deleted for good, along with agents' notes about this company and its tasks, and they can't be added again. Facts about the company, and the briefs and requests on tasks, that mention them aren't found for you, and nor are your own lead files: take them out there too.`;
}

function People({ c }: { c: Overview }) {
  const { org, sales, owner } = useSession();
  const refresh = useRefresh();
  const exportPerson = $api.useMutation("post", "/o/{org}/api/v1/contacts/{id}/export");
  const erase = $api.useMutation("post", "/o/{org}/api/v1/contacts/{id}/erase", { onSuccess: refresh });
  return (
    <div>
      <div className="flex items-center justify-between">
        <Eyebrow>People</Eyebrow>
        {sales && (
          <Link to="/o/$org/add" params={{ org }} search={{ company: c.name }} className="font-mono text-xs">
            + add
          </Link>
        )}
      </div>
      {c.people.length === 0 && <p className="py-2 text-sm text-muted-foreground">No one yet.</p>}
      {c.people.map((p) => (
        <div key={p.id}>
          <div className="flex flex-wrap justify-between gap-2 border-b border-dashed border-border py-2.5 text-sm">
            <span>
              <b>{p.name}</b> <span className="text-muted-foreground">{p.title ? "· " + p.title : ""}</span>
            </span>
            <span className="flex items-center gap-2">
              {p.linkedin && (
                <a href={safeHref(p.linkedin)} target="_blank" rel="noopener" className="font-mono text-xs">
                  LinkedIn ↗
                </a>
              )}
              <Meta>{p.stage}</Meta>
              {owner && (
                <>
                  <ActionButton
                    quiet
                    size="xs"
                    disabled={exportPerson.isPending}
                    onClick={() =>
                      exportPerson.mutate({ params: { path: { org, id: p.id } } }, { onSuccess: (held) => save(held, `person-${p.id}.json`) })
                    }
                  >
                    Export
                  </ActionButton>
                  <ActionButton
                    quiet
                    size="xs"
                    disabled={erase.isPending}
                    onClick={() => window.confirm(eraseConfirm(p)) && erase.mutate({ params: { path: { org, id: p.id } } })}
                  >
                    Erase
                  </ActionButton>
                </>
              )}
            </span>
          </div>
          <SuggestedDetails list={p.pending} />
        </div>
      ))}
      <Failed errors={[exportPerson.error, erase.error]} />
    </div>
  );
}

function detailLabel(f: string) {
  if (f === "linkedin") return "LinkedIn profile";
  if (f === "phone") return "phone number";
  return f;
}

function suggestedNote(d: Pending) {
  const notes = [];
  if (d.email_status === "guess") notes.push("a guess");
  if (d.current) notes.push("in place of " + d.current);
  return notes.join(", ");
}

function SuggestedDetails({ list }: { list: Pending[] }) {
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const decide = $api.useMutation("patch", "/o/{org}/api/v1/suggestions/{id}", { onSuccess: refresh });
  return (
    <>
      {list.map((d) => {
        const note = suggestedNote(d);
        return (
          <div key={d.id} className="flex flex-wrap items-center gap-2 rounded-lg bg-muted p-3 text-sm">
            <span className="min-w-0 flex-[1_1_220px]">
              <span className="font-mono text-xs text-muted-foreground">suggested {detailLabel(d.field)}</span>
              <span className="block break-all">{d.value}</span>
              {note && <span className="block text-muted-foreground">{note}</span>}
              <span className="flex flex-wrap items-center gap-2">
                <span className="font-mono text-xs text-mark">{actor(d.by)}</span>
                {d.source_url && (
                  <a href={safeHref(d.source_url)} target="_blank" rel="noopener" className="font-mono text-xs">
                    source ↗
                  </a>
                )}
              </span>
            </span>
            {sales && (
              <>
                <ActionButton size="xs" onClick={() => decide.mutate({ params: { path: { org, id: d.id } }, body: { decision: "accepted" } })}>
                  Accept
                </ActionButton>
                <ActionButton quiet size="xs" onClick={() => decide.mutate({ params: { path: { org, id: d.id } }, body: { decision: "dismissed" } })}>
                  Dismiss
                </ActionButton>
              </>
            )}
          </div>
        );
      })}
      <Failed errors={[decide.error]} />
    </>
  );
}

// Disclosure is a form tucked under a quiet line, as the server pages tuck theirs.
function Disclosure({ label, children }: { label: string; children: ReactNode }) {
  return (
    <details className="mt-2 text-sm">
      <summary className="cursor-pointer font-mono text-xs text-muted-foreground">{label}</summary>
      {children}
    </details>
  );
}

function Onboardings({ c }: { c: Overview }) {
  const { org, sales, support, owner } = useSession();
  const refresh = useRefresh();
  const start = $api.useMutation("post", "/o/{org}/api/v1/onboardings", { onSuccess: refresh });
  const day = today();
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    const b = formBody(form);
    start.mutate(
      { params: { path: { org } }, body: { company: c.id, name: (b.name as string) ?? "", start: (b.start as string) ?? "" } },
      { onSuccess: () => form.reset() },
    );
  };
  return (
    <div>
      <Eyebrow>Onboarding</Eyebrow>
      {c.onboardings.length === 0 ? (
        <p className="py-2 text-sm text-muted-foreground">Not being onboarded.</p>
      ) : (
        <div className="mt-1 flex flex-col gap-3">
          {c.onboardings.map((o) => (
            <OnboardingCard key={o.id} o={o} day={day} named={false} />
          ))}
        </div>
      )}
      {c.planned_steps > 0 && (owner || sales || support) ? (
        <Disclosure label="Start an onboarding">
          <form onSubmit={submit} className="mt-2 flex flex-wrap items-start gap-2">
            <label className="flex min-w-[220px] flex-1 flex-col gap-1">
              <Eyebrow>For, if they have several</Eyebrow>
              <input name="name" placeholder="e.g. the Leith site" className={field} />
            </label>
            <label className="flex flex-col gap-1">
              <Eyebrow>Starts</Eyebrow>
              <input name="start" type="date" required defaultValue={day} className={field} />
            </label>
            <span className="flex flex-col gap-1">
              <Eyebrow>From your steps</Eyebrow>
              <ActionButton type="submit" size="sm" className="h-9" disabled={start.isPending}>
                Start
              </ActionButton>
            </span>
            <Failed errors={[start.error]} />
          </form>
        </Disclosure>
      ) : (
        c.planned_steps === 0 && <Meta>An owner writes the onboarding steps under Settings; then one can be started here.</Meta>
      )}
    </div>
  );
}

const thens = [
  { key: "renews", label: "renews for the same term" },
  { key: "rolls", label: "rolls on, a period at a time" },
  { key: "ends", label: "ends" },
];

function termText(s: Subscription) {
  if (s.term_months === 0) return `no commitment, a ${s.per} at a time`;
  const then = thens.find((t) => t.key === s.then);
  return plural(s.term_months, "month") + (then ? ", then " + then.label : "");
}

function standingText(s: Subscription) {
  if (s.status === "ended") return "ended " + s.ends;
  if (s.status === "scheduled") return "starts " + s.start;
  if (s.ends) return "ends " + s.ends;
  if (s.cancel_ends && s.cancel_ends !== s.term_end) {
    return `term ends ${s.term_end} and the next is committed to · cancel by ${s.cancel_by} to end ${s.cancel_ends}`;
  }
  return `term ends ${s.term_end} · cancel by ${s.cancel_by}`;
}

function subscriptionTone(s: Subscription): Tone {
  if (s.status === "active") return "ok";
  if (s.status === "notice given" || s.status === "ending") return "warn";
  return "";
}

function Subscriptions({ c }: { c: Overview }) {
  const { org, support } = useSession();
  const refresh = useRefresh();
  const add = $api.useMutation("post", "/o/{org}/api/v1/subscriptions", { onSuccess: refresh });
  const day = today();
  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    const b = formBody(form);
    add.mutate(
      {
        params: { path: { org } },
        body: {
          company: c.id,
          name: String(b.name ?? ""),
          amount: b.amount as string | undefined,
          per: b.per as string | undefined,
          start: String(b.start ?? ""),
          term_months: b.term_months as number | undefined,
          then: b.then as string | undefined,
          notice_days: b.notice_days as number | undefined,
        },
      },
      { onSuccess: () => form.reset() },
    );
  };
  return (
    <div>
      <Eyebrow>Subscriptions</Eyebrow>
      {c.subscriptions.length === 0 ? (
        <p className="py-2 text-sm text-muted-foreground">
          {support ? "Nothing recorded. Once they are a customer, record what they are on." : "Nothing recorded."}
        </p>
      ) : (
        <div className="mt-1 overflow-hidden rounded-lg border border-border">
          {c.subscriptions.map((s) => (
            <SubscriptionLine key={s.id} s={s} />
          ))}
        </div>
      )}
      {support && (
        <Disclosure label="Record a subscription">
          <form onSubmit={submit} className="mt-2 flex flex-wrap items-start gap-2">
            <label className="flex min-w-[220px] flex-1 flex-col gap-1">
              <Eyebrow>What they're on</Eyebrow>
              <input name="name" required placeholder="e.g. Standard plan, 2 sites" className={field} />
            </label>
            <label className="flex flex-col gap-1">
              <Eyebrow>Price</Eyebrow>
              <input name="amount" placeholder="e.g. 14,000" className={cn(field, "w-44")} />
            </label>
            <label className="flex flex-col gap-1">
              <Eyebrow>For</Eyebrow>
              <select name="per" className={select}>
                <option value="year">a year</option>
                <option value="month">a month</option>
              </select>
            </label>
            <label className="flex flex-col gap-1">
              <Eyebrow>Starts</Eyebrow>
              <input name="start" type="date" required defaultValue={day} className={field} />
            </label>
            <label className="flex flex-col gap-1">
              <Eyebrow>Months committed</Eyebrow>
              <input name="term_months" type="number" min={0} max={120} defaultValue={12} className={cn(field, "w-24")} />
            </label>
            <label className="flex flex-col gap-1">
              <Eyebrow>Then</Eyebrow>
              <select name="then" className={select}>
                {thens.map((t) => (
                  <option key={t.key} value={t.key}>
                    {t.label}
                  </option>
                ))}
              </select>
            </label>
            <label className="flex flex-col gap-1">
              <Eyebrow>Days' notice</Eyebrow>
              <input name="notice_days" type="number" min={0} max={730} defaultValue={30} className={cn(field, "w-24")} />
            </label>
            <span className="flex flex-col gap-1">
              <Eyebrow>When it's right</Eyebrow>
              <ActionButton type="submit" size="sm" className="h-9" disabled={add.isPending}>
                Record
              </ActionButton>
            </span>
            <Failed errors={[add.error]} />
          </form>
        </Disclosure>
      )}
    </div>
  );
}

function SubscriptionLine({ s }: { s: Subscription }) {
  const { support } = useSession();
  const price = s.figure ? `${s.figure} a ${s.per}` : "";
  return (
    <div className="border-b border-border px-4 py-3 text-sm last:border-0">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <span className="min-w-0">
          <b>{s.name}</b>
          <span className="block font-mono text-xs text-muted-foreground">{[price, termText(s), standingText(s)].filter((p) => p.trim()).join(" · ")}</span>
        </span>
        <span className="flex items-center gap-2">
          {s.soon && <Pill tone="warn">within 30 days</Pill>}
          <Pill tone={subscriptionTone(s)}>{s.status}</Pill>
        </span>
      </div>
      {support && <SubscriptionUpdate s={s} />}
    </div>
  );
}

// useCommitted calls back on the DOM's change event, when an edit is done, as the
// server-rendered page saves: React's onChange fires on every keystroke.
function useCommitted<T extends HTMLInputElement | HTMLSelectElement>(commit: (value: string) => void) {
  const ref = useRef<T>(null);
  const latest = useRef(commit);
  latest.current = commit;
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const changed = () => latest.current(el.value);
    el.addEventListener("change", changed);
    return () => el.removeEventListener("change", changed);
  }, []);
  return ref;
}

type SaveField = (key: keyof SubscriptionChange, value: string, number?: boolean) => void;

function SubField({ name, onSave, number, ...props }: ComponentProps<typeof Input> & { name: keyof SubscriptionChange; onSave: SaveField; number?: boolean }) {
  const ref = useCommitted<HTMLInputElement>((value) => onSave(name, value, number));
  return <Input ref={ref} {...props} type={number ? "number" : props.type} />;
}

function SubChoice({ name, onSave, value, label, children }: { name: keyof SubscriptionChange; onSave: SaveField; value: string; label: string; children: ReactNode }) {
  const ref = useCommitted<HTMLSelectElement>((v) => onSave(name, v));
  return (
    <select ref={ref} defaultValue={value} aria-label={label} className="h-8 rounded-lg border border-input bg-muted px-2 text-sm">
      {children}
    </select>
  );
}

function SubscriptionUpdate({ s }: { s: Subscription }) {
  const { org } = useSession();
  const refresh = useRefresh();
  const path = { params: { path: { org, id: s.id } } };
  const [problem, setProblem] = useState<{ detail: string } | null>(null);
  const change = $api.useMutation("patch", "/o/{org}/api/v1/subscriptions/{id}", { onSuccess: refresh });
  const renew = $api.useMutation("post", "/o/{org}/api/v1/subscriptions/{id}/renewals", { onSuccess: refresh });
  const remove = $api.useMutation("delete", "/o/{org}/api/v1/subscriptions/{id}", { onSuccess: refresh });
  const [who, setWho] = useState<number | undefined>(s.people[0]?.id);

  const onSave: SaveField = (key, value, number) => {
    setProblem(null);
    change.reset();
    // An emptied number box is not a nought: saving one would end a commitment.
    if (number && value.trim() === "") {
      setProblem({ detail: "Enter a number, or 0 for none." });
      return;
    }
    change.mutate({ ...path, body: { [key]: number ? +value : value } });
  };
  // Each box is keyed on what the server holds, so one it rewrote shows the rewrite after the refresh.
  const box = (key: keyof SubscriptionChange, value: string | number) => `${key}:${value}`;

  return (
    <details className="mt-1 text-sm">
      <summary className="cursor-pointer font-mono text-xs text-muted-foreground">Update</summary>
      <div className="mt-2 grid grid-cols-1 gap-2 sm:grid-cols-[170px_1fr]">
        <Eyebrow>What they're on</Eyebrow>
        <SubField key={box("name", s.name)} name="name" onSave={onSave} defaultValue={s.name} aria-label="What they're on" className="h-8 bg-muted" />
        <Eyebrow>Price</Eyebrow>
        <span className="flex items-center gap-2">
          <SubField key={box("amount", s.figure)} name="amount" onSave={onSave} defaultValue={s.figure} placeholder="e.g. 14,000" aria-label="Price" className="h-8 w-44 bg-muted" />
          <SubChoice key={box("per", s.per)} name="per" onSave={onSave} value={s.per === "month" ? "month" : "year"} label="For">
            <option value="year">a year</option>
            <option value="month">a month</option>
          </SubChoice>
        </span>
        <Eyebrow>Started</Eyebrow>
        <SubField key={box("start", s.start)} name="start" onSave={onSave} type="date" defaultValue={s.start} aria-label="Started" className="h-8 w-44 bg-muted" />
        <Eyebrow>Committed for, months</Eyebrow>
        <SubField
          key={box("term_months", s.term_months)}
          name="term_months"
          onSave={onSave}
          number
          defaultValue={s.term_months}
          aria-label="Committed for, months"
          className="h-8 w-24 bg-muted"
        />
        <Eyebrow>At the end of the term</Eyebrow>
        <SubChoice key={box("then", s.then)} name="then" onSave={onSave} value={s.then} label="At the end of the term">
          {thens.map((t) => (
            <option key={t.key} value={t.key}>
              {t.label}
            </option>
          ))}
        </SubChoice>
        <Eyebrow>Notice to end, days</Eyebrow>
        <SubField
          key={box("notice_days", s.notice_days)}
          name="notice_days"
          onSave={onSave}
          number
          defaultValue={s.notice_days}
          aria-label="Notice to end, days"
          className="h-8 w-24 bg-muted"
        />
        <Eyebrow>Notice given on</Eyebrow>
        <SubField key={box("notice_on", s.notice_on)} name="notice_on" onSave={onSave} type="date" defaultValue={s.notice_on} aria-label="Notice given on" className="h-8 w-44 bg-muted" />
        <Eyebrow>Ended early on</Eyebrow>
        <SubField key={box("ended_on", s.ended_on)} name="ended_on" onSave={onSave} type="date" defaultValue={s.ended_on} aria-label="Ended early on" className="h-8 w-44 bg-muted" />
      </div>
      <Failed errors={[problem, change.error]} />
      {s.renewing ? (
        <p className="mt-2 border-t border-border pt-2">
          <Link to="/o/$org/conversations" params={{ org }} className="font-mono text-xs text-mark underline">
            Renewal under way in Conversations
          </Link>
        </p>
      ) : s.status !== "ended" && s.people.length > 0 ? (
        <form
          className="mt-2 flex flex-wrap items-center gap-2 border-t border-border pt-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (who) renew.mutate({ ...path, body: { contact: who } });
          }}
        >
          <Eyebrow>Renewal with</Eyebrow>
          <select aria-label="Who the renewal is with" value={who} onChange={(e) => setWho(Number(e.target.value))} className={select}>
            {s.people.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
          <ActionButton quiet size="sm" type="submit" disabled={renew.isPending}>
            Start the renewal deal
          </ActionButton>
          <Failed errors={[renew.error]} />
        </form>
      ) : (
        s.status !== "ended" && (
          <p className="mt-2 border-t border-border pt-2 font-mono text-xs text-muted-foreground">
            To start a renewal deal, someone at this company needs to be on the Conversations board first.
          </p>
        )
      )}
      <div className="mt-2 flex flex-wrap items-center gap-2 border-t border-border pt-2">
        <ActionButton quiet size="sm" disabled={remove.isPending} onClick={() => remove.mutate(path)}>
          Remove: recorded by mistake
        </ActionButton>
        <Failed errors={[remove.error]} />
      </div>
    </details>
  );
}

const signerTone: Record<string, Tone> = { signed: "ok", due: "warn", declined: "stop", waiting: "", "": "" };

// SignStatus is an agreement that went out: who signs and where each stands, and what
// can be done about it now.
function SignStatus({ a }: { a: Agreement }) {
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const path = { params: { path: { org, id: a.id } } };
  const recipients = $api.useQuery("get", "/o/{org}/api/v1/agreements/{id}/recipients", { params: { path: { org, id: a.id }, query: { limit: 200 } } });
  const overview = $api.useQuery("get", "/o/{org}/api/v1/agreements/{id}", path);
  const voiding = $api.useMutation("post", "/o/{org}/api/v1/agreements/{id}/void", { onSuccess: refresh });
  const resend = $api.useMutation("post", "/o/{org}/api/v1/agreements/{id}/recipients/{recipient}/resend");
  const [resent, setResent] = useState(0);
  const items = recipients.data?.items ?? [];
  const until = a.expires_at ? new Date(a.expires_at).toLocaleDateString() : "";
  return (
    <div className="flex flex-col gap-2">
      <Meta>
        {a.status === "sent" && `Sent ${a.sent_at ? new Date(a.sent_at).toLocaleDateString() : ""} by ${a.sent_by} · open until ${until}`}
        {a.status === "sealing" && (a.seal_problem ? `Everyone has signed, but sealing failed and is being tried again: ${a.seal_problem}` : "Everyone has signed: it's being sealed.")}
        {a.status === "completed" && `Signed by everyone and sealed ${a.completed_at ? new Date(a.completed_at).toLocaleDateString() : ""} · SHA-256 ${a.sealed_hash.slice(0, 12)}…`}
        {a.status === "declined" && "Declined: it can't be signed now."}
        {a.status === "voided" && "Withdrawn: it can't be signed now."}
        {a.status === "expired" && `Its time ran out on ${until}.`}
      </Meta>
      <ol className="flex list-none flex-col overflow-hidden rounded-lg border border-border p-0">
        {items.map((r, i) => (
          <li key={r.id} className="flex flex-wrap items-center justify-between gap-2 border-b border-border px-3 py-2 last:border-0">
            <span>
              {r.role === "signer" ? (
                <b>
                  {i + 1} · {r.name}
                </b>
              ) : (
                <span className="font-mono text-xs text-muted-foreground">gets a copy · {r.name}</span>
              )}{" "}
              <span className="font-mono text-xs text-muted-foreground">{r.email}</span>
            </span>
            <span className="flex items-center gap-2">
              {r.role === "signer" && (
                <Pill tone={signerTone[r.status ?? ""] ?? ""}>
                  {r.status === "signed" && r.signed_at ? `signed ${new Date(r.signed_at).toLocaleDateString()}` : r.status === "due" ? "their turn" : r.status || "—"}
                </Pill>
              )}
              {r.status === "declined" && r.decline_reason && <span className="font-mono text-xs text-muted-foreground">“{r.decline_reason}”</span>}
              {r.mail_problem && <span className="font-mono text-xs text-[var(--stop)]">Their mail {r.mail_problem}</span>}
              {sales && a.status === "sent" && r.status === "due" && !r.member_id && (
                <ActionButton
                  quiet
                  size="xs"
                  disabled={resent === r.id}
                  onClick={() => resend.mutate({ params: { path: { org, id: a.id, recipient: r.id } } }, { onSuccess: () => setResent(r.id) })}
                >
                  {resent === r.id ? "Sent again ✓" : "Send link again"}
                </ActionButton>
              )}
            </span>
          </li>
        ))}
      </ol>
      <div className="flex flex-wrap items-center gap-2">
        {overview.data?.my_turn && (
          <ActionButton size="xs" asChild>
            <Link to="/o/$org/agreements/$id/sign" params={{ org, id: a.id }}>
              Sign now
            </Link>
          </ActionButton>
        )}
        {a.status === "completed" && (
          <a href={`/o/${org}/api/v1/agreements/${a.id}/sealed`} className="font-mono text-xs text-mark underline">
            Download the signed copy
          </a>
        )}
        {sales && (a.status === "sent" || (a.status === "sealing" && a.seal_problem !== "")) && (
          <ActionButton
            quiet
            size="xs"
            className="text-[var(--stop)]"
            onClick={() => {
              const reason = window.prompt(`Withdraw ${a.name}? Nobody can sign it after, and the signers already sent it are told. Why, for them (optional):`);
              if (reason !== null) voiding.mutate({ ...path, body: { reason } });
            }}
          >
            Withdraw
          </ActionButton>
        )}
      </div>
      <Failed errors={[voiding.error, resend.error]} />
    </div>
  );
}

function fileLine(a: Agreement) {
  if (!a.file_hash) return "No document yet";
  return `${a.file_name} · ${Math.floor((a.file_size + 1023) / 1024)} KB · SHA-256 ${a.file_hash.slice(0, 12)}…`;
}

function Agreements({ c }: { c: Overview }) {
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const create = $api.useMutation("post", "/o/{org}/api/v1/agreements", { onSuccess: refresh });
  const upload = $api.useMutation("put", "/o/{org}/api/v1/agreements/{id}/document", { onSuccess: refresh });
  const remove = $api.useMutation("delete", "/o/{org}/api/v1/agreements/{id}", { onSuccess: refresh });
  return (
    <div>
      <Eyebrow>Agreements</Eyebrow>
      {c.agreements.length === 0 && <p className="py-2 text-sm text-muted-foreground">None yet. Start one, then upload the contract as a PDF.</p>}
      {c.agreements.map((a) => (
        <div key={a.id} className="mt-1 flex flex-col gap-2 rounded-lg border border-border p-3 text-sm">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <b>{a.name}</b>
            <span className="font-mono text-xs text-muted-foreground">{a.status} · by {actor(a.created_by)}</span>
          </div>
          <Meta>{fileLine(a)}</Meta>
          {a.status !== "draft" && <SignStatus a={a} />}
          <div className="flex flex-wrap items-center gap-2">
            {a.file_hash && sales && a.status === "draft" ? (
              <ActionButton size="xs" asChild>
                <Link to="/o/$org/agreements/$id/prepare" params={{ org, id: a.id }}>Prepare for signing</Link>
              </ActionButton>
            ) : (
              a.file_hash && (
                <Link to="/o/$org/agreements/$id/prepare" params={{ org, id: a.id }} className="font-mono text-xs text-mark underline">
                  Who signs it
                </Link>
              )
            )}
            {a.file_hash && (
              <a href={`/o/${org}/api/v1/agreements/${a.id}/document`} className="font-mono text-xs text-mark underline">
                Download
              </a>
            )}
            {sales && a.status === "draft" && (
              <>
                <label className="font-mono text-xs text-muted-foreground">
                  {a.file_hash ? "Replace the PDF" : "Upload the PDF"}
                  <input
                    type="file"
                    accept="application/pdf,.pdf"
                    className="text-xs"
                    onChange={(e) => {
                      const input = e.currentTarget;
                      const file = input.files?.[0];
                      if (!file) return;
                      upload.mutate(
                        {
                          params: { path: { org, id: a.id }, query: { name: file.name } },
                          // The spec's binary body is typed as a string; the File is sent as it is.
                          body: file as unknown as string,
                          bodySerializer: (b: unknown) => b as BodyInit,
                          headers: { "Content-Type": "application/pdf" },
                        },
                        { onError: () => (input.value = "") },
                      );
                    }}
                  />
                </label>
                <ActionButton
                  quiet
                  size="xs"
                  onClick={() => window.confirm(`Remove the draft ${a.name} and its document?`) && remove.mutate({ params: { path: { org, id: a.id } } })}
                >
                  Remove
                </ActionButton>
              </>
            )}
          </div>
        </div>
      ))}
      <Failed errors={[upload.error, remove.error]} />
      {sales && (
        <Disclosure label="Start an agreement">
          <form
            className="mt-2 flex flex-wrap items-center gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              const form = e.currentTarget;
              create.mutate({ params: { path: { org } }, body: { company: c.id, name: String(formBody(form).name ?? "") } }, { onSuccess: () => form.reset() });
            }}
          >
            <input
              name="name"
              required
              maxLength={120}
              placeholder="e.g. Annual service agreement 2027"
              aria-label="Name of the agreement"
              className={cn(field, "min-w-[220px] flex-1")}
            />
            <ActionButton quiet size="sm" type="submit" disabled={create.isPending}>
              Start
            </ActionButton>
            <Failed errors={[create.error]} />
          </form>
          <Meta>
            Prospector keeps the PDF as uploaded, with its SHA-256, and doesn't read what it says. It is not for deeds, or anything that must be
            witnessed or notarised. You can set out who signs it and where; sending it for signature comes later.
          </Meta>
        </Disclosure>
      )}
    </div>
  );
}

const noteKinds: Record<string, string> = { skipped: "Skipped", dead_end: "Dead end", next: "Next" };

function Notes({ notes }: { notes: components["schemas"]["Note"][] }) {
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const remove = $api.useMutation("delete", "/o/{org}/api/v1/notes/{id}", { onSuccess: refresh });
  return (
    <>
      {notes.length === 0 && <p className="text-sm text-muted-foreground">None.</p>}
      {notes.map((n) => (
        <div key={n.id} className="flex flex-col gap-1.5 border-b border-dashed border-border py-2.5 text-sm last:border-0">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="flex flex-wrap items-center gap-2">
              <Pill tone="agent">{noteKinds[n.kind] ?? n.kind}</Pill>
            </span>
            {sales && (
              <ActionButton quiet size="xs" onClick={() => window.confirm("Delete this note?") && remove.mutate({ params: { path: { org, id: n.id } } })}>
                Delete
              </ActionButton>
            )}
          </div>
          <p className="whitespace-pre-wrap">{n.text}</p>
          <Meta>
            {actor(n.actor)} · {stamp(n.created_at)}
          </Meta>
        </div>
      ))}
      <Failed errors={[remove.error]} />
    </>
  );
}
