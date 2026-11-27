import { type FormEvent, type ReactNode } from "react";
import { Link, useParams } from "@tanstack/react-router";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Eyebrow, Failed, Meta, Pill, type Tone, useRefresh } from "@/components/app";
import { standingText, SubscriptionUpdate, termText, thens } from "@/components/app/subscription";
import { Placeholders, Text } from "@/components/app/skeleton";
import { actor, stamp } from "@/lib/format";
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
  if (!c || !history.data || !notes.data) {
    // Only what every company's page starts with, where it will be: what follows depends
    // on the company, and appears below rather than pushing this down.
    return (
      <>
        {back}
        <Placeholders className="mt-3 flex flex-col gap-5 panel p-6">
          <Head />
        </Placeholders>
      </>
    );
  }
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

// Head is the company's name and where it stands, or their outline while the page loads.
function Head({ c }: { c?: Overview }) {
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const review = $api.useMutation("patch", "/o/{org}/api/v1/companies/{id}", { onSuccess: refresh });
  return (
    <div className="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h1 className="font-heading text-3xl font-bold tracking-tight">
          <Text value={c?.name} w="12ch" />
        </h1>
        <Meta>
          <Text value={c && [c.domain, c.country, c.segment, c.grade ? "fit " + c.grade : ""].filter((p) => p.trim()).join(" · ")} w="30ch" />
        </Meta>
      </div>
      <div className="flex min-h-8 items-center gap-2">
        <StageTag stage={c?.pipeline} />
        {c && c.stage !== "qualified" && sales && (
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
            <StandingRow
              key={o.id}
              name={o.name}
              pill={v ? (verdictPill[v.stage] ?? null) : null}
              said={v && (v.reason || v.by) ? `${v.reason ? `“${v.reason}” ` : ""}${actor(v.by)}, ${stamp(v.updated_at)}` : ""}
            >
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
        {owner ? (
          <Link to="/o/$org/settings" params={{ org }}>
            Name what you look for
          </Link>
        ) : (
          "Nothing named yet"
        )}
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
                    onClick={() => exportPerson.mutate({ params: { path: { org, id: p.id } } }, { onSuccess: (held) => save(held, `person-${p.id}.json`) })}
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
          // oxlint-disable-next-line unicorn/no-thenable -- the API's name for what happens when a term ends
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
            <span className="font-mono text-xs text-muted-foreground">
              {a.status} · by {actor(a.created_by)}
            </span>
          </div>
          <Meta>{fileLine(a)}</Meta>
          <div className="flex flex-wrap items-center gap-2">
            {a.file_hash && sales && a.status === "draft" ? (
              <ActionButton size="xs" asChild>
                <Link to="/o/$org/agreements/$id/prepare" params={{ org, id: a.id }}>
                  Prepare for signing
                </Link>
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
            Prospector keeps the PDF as uploaded, with its SHA-256, and doesn't read what it says. It is not for deeds, or anything that must be witnessed or
            notarised. You can set out who signs it and where; sending it for signature comes later.
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
