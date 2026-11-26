import { useEffect, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Eyebrow, Failed, FilterChip, Meta, Pill, Section, type Tone, useRefresh, Whose } from "@/components/app";
import { actor, day, stamp } from "@/lib/format";
import { Checkbox } from "@/components/ui/checkbox";
import { Textarea } from "@/components/ui/textarea";
import { useSession } from "@/session";

type Company = components["schemas"]["ResearchCompany"];
type Criterion = components["schemas"]["Criterion"];
type Run = components["schemas"]["Run"];
type Agent = components["schemas"]["Agent"];

const filters = [
  { key: "review", label: "To review" },
  { key: "contact", label: "Needs a contact" },
  { key: "verify", label: "Needs verifying" },
  { key: "details", label: "Details to check" },
  { key: "rejected", label: "Rejected" },
] as const;
type Filter = (typeof filters)[number]["key"];

const empty: Record<Filter, [string, string]> = {
  review: ["Nothing to review", "Companies your agents and discovery jobs add will wait here."],
  contact: ["Every qualified company has a contact", "Add someone with + Add person, or let an agent suggest people."],
  verify: ["No emails to verify", "Contacts whose email is a pattern guess show up here."],
  details: ["No details to check", "An email, phone or profile an agent finds for someone already here waits for you to accept it."],
  rejected: ["Nothing here", ""],
};


export function ResearchPage() {
  const { org, sales } = useSession();
  const [params, setParams] = useSearchParams();
  const filter = (params.get("f") ?? "review") as Filter;
  const task = Number(params.get("task") ?? 0) || undefined;
  const [cursor, setCursor] = useState<string | undefined>();
  // A cursor is a place in one list: another filter or search starts at its top.
  useEffect(() => setCursor(undefined), [filter, task]);

  const research = $api.useQuery("get", "/o/{org}/api/v1/research", {
    params: { path: { org }, query: { filter, task, cursor } },
  });
  const settings = $api.useQuery("get", "/o/{org}/api/v1/settings", { params: { path: { org } } });
  const tasks = $api.useQuery("get", "/o/{org}/api/v1/tasks", { params: { path: { org }, query: { limit: 5 } } });
  const agents = $api.useQuery("get", "/o/{org}/api/v1/agents", { params: { path: { org } } });
  const ready = (agents.data?.items ?? []).filter((a) => a.ready);
  // The same query SearchPanel makes, so the search is read once; closed searches aren't in tasks.
  const chosen = $api.useQuery("get", "/o/{org}/api/v1/tasks/{id}", { params: { path: { org, id: task ?? 0 }, query: { limit: 3 } } }, { enabled: !!task }).data;

  const link = (f: string, t?: number) => {
    const q = new URLSearchParams();
    if (f !== "review") q.set("f", f);
    if (t) q.set("task", String(t));
    const s = q.toString();
    return s ? `?${s}` : ".";
  };

  return (
    <>
      <header className="mb-5 flex flex-wrap items-baseline justify-between gap-3">
        <h1 className="font-heading text-4xl font-bold tracking-tight">
          Research<span className="text-coral">.</span>
        </h1>
        <div className="flex flex-wrap gap-2">
          <ActionButton quiet size="sm" asChild>
            <a href={`/o/${org}/agents`}>Connect an agent</a>
          </ActionButton>
          {sales && (
            <ActionButton size="sm" asChild>
              <a href={`/o/${org}/app/add`}>+ Add person</a>
            </ActionButton>
          )}
        </div>
      </header>
      <p className="mb-4 max-w-[70ch] text-sm text-muted-foreground">
        Say who you're looking for and an agent goes to find them. What agents, jobs and imports find waits here: nothing reaches
        outreach until someone qualifies it and picks a contact. Every fact keeps the link it came from.
      </p>

      <Whose area="sales" work="Research is" can={sales} />
      {sales && <FindBox agents={ready} lookingFor={settings.data?.looking_for ?? []} />}

      <div className="mb-5 flex flex-col gap-2" aria-label="Searches">
        <div className="flex flex-wrap items-baseline justify-between gap-2">
          <Eyebrow>Searches</Eyebrow>
          <Link to="/tasks" className="text-[13px] text-mark underline">
            Every search, closed ones too
          </Link>
        </div>
        {(tasks.data?.items ?? []).map((t) => (
          <SearchStrip key={t.id} id={t.id} title={t.title} found={t.found.companies} agents={ready} chosen={t.id === task} />
        ))}
        {tasks.data && tasks.data.items.length === 0 && <p className="text-sm text-muted-foreground">No search open.</p>}
      </div>

      <div className="flex flex-wrap items-start gap-6">
        <div className="flex min-w-0 flex-[999_1_520px] flex-col gap-3">
          <nav className="flex flex-wrap items-center gap-2" aria-label="Filter">
            {filters.map((f) => (
              <FilterChip key={f.key} to={link(f.key, task)} label={f.label} count={research.data?.counts[f.key] ?? 0} on={f.key === filter} />
            ))}
            {task && (
              <span className="inline-flex items-center gap-1">
                <Pill tone="agent">From “{chosen?.title ?? `search ${task}`}”</Pill>
                <Link to={link(filter)} className="px-1 text-sm text-muted-foreground no-underline" aria-label="Show every search">
                  ×
                </Link>
              </span>
            )}
          </nav>
          {research.data?.ranked && research.data.items.length > 0 && (
            <Meta>Best fit first: no must missed, then most met, then fewest left unknown.</Meta>
          )}
          {research.data && research.data.items.length === 0 && (
            <div className="panel p-6 text-center">
              <p className="font-heading font-semibold">{empty[filter][0]}</p>
              <p className="text-sm text-muted-foreground">{empty[filter][1]}</p>
            </div>
          )}
          {research.error && <p className="text-sm text-[var(--stop)]">The queue couldn't be read. Reload to try again.</p>}
          <div className="flex flex-col gap-3">
            {research.data?.items.map((c, i) => <CompanyCard key={c.id} company={c} open={i === 0} />)}
          </div>
          <nav className="flex justify-between gap-3">
            <span>
              {cursor && (
                <ActionButton quiet size="sm" onClick={() => setCursor(undefined)}>
                  ← First
                </ActionButton>
              )}
            </span>
            <span>
              {research.data?.next_cursor && (
                <ActionButton quiet size="sm" onClick={() => setCursor(research.data?.next_cursor)}>
                  Older →
                </ActionButton>
              )}
            </span>
          </nav>
        </div>
        {task && (
          <aside className="flex min-w-0 flex-[1_1_280px] flex-col gap-4">
            <SearchPanel id={task} onGone={() => setParams({})} />
          </aside>
        )}
      </div>
    </>
  );
}

function FindBox({ agents, lookingFor }: { agents: Agent[]; lookingFor: components["schemas"]["LookFor"][] }) {
  const { org } = useSession();
  const navigate = useNavigate();
  const [brief, setBrief] = useState("");
  const [picked, setPicked] = useState<number[] | null>(null);
  const send = picked ?? agents.slice(0, 1).map((a) => a.id);
  const create = $api.useMutation("post", "/o/{org}/api/v1/tasks", {
    onSuccess: (t) => navigate(`?task=${t.id}`),
  });

  return (
    <form
      className="panel mb-5 overflow-hidden shadow-[4px_4px_0_var(--edge)]"
      onSubmit={(e) => {
        e.preventDefault();
        create.mutate({ params: { path: { org } }, body: { title: "", brief, agents: send } });
      }}
    >
      <div className="flex flex-col gap-2.5 px-5 py-4">
        <label htmlFor="find-brief">
          <Eyebrow>Who are you looking for?</Eyebrow>
        </label>
        <Textarea
          id="find-brief"
          required
          maxLength={4000}
          rows={2}
          value={brief}
          onChange={(e) => setBrief(e.target.value)}
          placeholder="Heads of procurement at UK kitchen makers with their own showrooms. About twenty, with the page that shows each one's role."
          className="rounded-lg border-input bg-muted text-base leading-relaxed"
        />
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
            {agents.length === 0 ? (
              <span>
                No agent of Prospector's own is ready, so this waits for one of yours.{" "}
                <a href={`/o/${org}/agents`} className="text-mark underline">
                  Set one up
                </a>
              </span>
            ) : (
              <>
                <span>Send</span>
                {agents.map((a) => (
                  <label
                    key={a.id}
                    className="inline-flex cursor-pointer items-center gap-1.5 rounded-full border-[1.5px] border-ink bg-card px-2.5 py-1 text-foreground"
                  >
                    <Checkbox
                      checked={send.includes(a.id)}
                      onCheckedChange={(on) => setPicked(on ? [...send, a.id] : send.filter((id) => id !== a.id))}
                    />
                    {a.name}
                  </label>
                ))}
              </>
            )}
          </div>
          <div className="flex items-center gap-3">
            {create.error && <p className="text-sm text-[var(--stop)]">{create.error.detail ?? "That didn't work."}</p>}
            <ActionButton type="submit" disabled={create.isPending}>
              Find
            </ActionButton>
          </div>
        </div>
      </div>
      <div className="flex flex-wrap gap-x-6 gap-y-2 border-t border-border bg-secondary px-5 py-3">
        {lookingFor.map((a) => (
          <div key={a.field} className="flex min-w-0 flex-[1_1_280px] items-baseline gap-2">
            <span className="w-[104px] shrink-0 font-mono text-xs">{a.field}</span>
            <span className="min-w-0 text-sm">{a.hint}</span>
            {a.must && <span className="font-mono text-[11px] text-mark">must</span>}
          </div>
        ))}
        <p className="w-full text-[13px] text-muted-foreground">
          {lookingFor.length === 0
            ? "Agents look for what you write. Settings › What you look for adds what they check at every company. "
            : "Agents check each of these, from Settings › What you look for, and record what they find as a fact with its source. "}
          <a href={`/o/${org}/settings`} className="text-mark underline">
            Change them
          </a>
        </p>
      </div>
    </form>
  );
}

function runState(r: Run) {
  if (r.stop_asked) return "stopping";
  if (r.status === "waiting") return "waiting for its endpoint";
  return r.status;
}

function SearchStrip({ id, title, found, agents, chosen }: { id: number; title: string; found: number; agents: Agent[]; chosen: boolean }) {
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const runs = $api.useQuery("get", "/o/{org}/api/v1/tasks/{task}/runs", { params: { path: { org, task: id }, query: { limit: 3 } } });
  const going = (runs.data?.items ?? []).filter((r) => r.status === "running" || r.status === "waiting");
  const start = $api.useMutation("post", "/o/{org}/api/v1/tasks/{task}/runs", { onSuccess: refresh });
  const stop = $api.useMutation("post", "/o/{org}/api/v1/runs/{id}/stop", { onSuccess: refresh });

  return (
    <div
      className={`flex flex-wrap items-center gap-x-4 gap-y-2 rounded-xl border-[1.5px] border-ink bg-card px-4 py-2.5 ${going.length > 0 ? "shadow-[4px_4px_0_#b9aef2]" : ""}`}
    >
      <b className="font-heading text-[15px]">{title}</b>
      {going.map((r) => (
        <Pill key={r.id} tone="agent">
          {r.agent_name} · {runState(r)}
        </Pill>
      ))}
      <span className="font-mono text-xs text-muted-foreground">
        {found} {found === 1 ? "company" : "companies"} found
      </span>
      <span className="ml-auto flex flex-wrap gap-2">
        {!chosen && (
          <ActionButton quiet size="sm" asChild>
            <Link to={`?task=${id}`}>Show these</Link>
          </ActionButton>
        )}
        {sales &&
          going.map((r) => (
            <ActionButton key={r.id} quiet size="sm" onClick={() => stop.mutate({ params: { path: { org, id: r.id } } })}>
              Stop {r.agent_name}
            </ActionButton>
          ))}
        {sales &&
          agents
            .filter((a) => !going.some((r) => r.agent === a.id))
            .map((a) => (
              <ActionButton key={a.id} quiet size="sm" onClick={() => start.mutate({ params: { path: { org, task: id } }, body: { agent: a.id } })}>
                Run {a.name}
              </ActionButton>
            ))}
      </span>
      <Failed errors={[start.error, stop.error]} />
    </div>
  );
}

function SearchPanel({ id, onGone }: { id: number; onGone: () => void }) {
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const task = $api.useQuery("get", "/o/{org}/api/v1/tasks/{id}", { params: { path: { org, id }, query: { limit: 3 } } });
  const runs = $api.useQuery("get", "/o/{org}/api/v1/tasks/{task}/runs", { params: { path: { org, task: id }, query: { limit: 3 } } });
  const [text, setText] = useState("");
  const ask = $api.useMutation("post", "/o/{org}/api/v1/tasks/{id}/requests", {
    onSuccess: () => {
      setText("");
      refresh();
    },
  });
  if (task.error) {
    return (
      <Section>
        <p className="text-sm">That search isn't here.</p>
        <ActionButton quiet size="sm" onClick={onGone}>
          Show every search
        </ActionButton>
      </Section>
    );
  }
  const t = task.data;
  return (
    <Section className="gap-2.5 p-5">
      <Eyebrow>{t?.title}</Eyebrow>
      <p className="text-sm">{t?.brief}</p>
      {(runs.data?.items ?? []).map((r) => (
        <div key={r.id} className="flex flex-col gap-0.5 border-t border-dashed border-border pt-2 text-sm">
          <span>
            <b>{r.agent_name}</b> · {runState(r)}
          </span>
          <Meta>
            {r.started_by} · {stamp(r.started_at)}
            {r.stop_reason ? ` · ${r.stop_reason}` : ""}
          </Meta>
        </div>
      ))}
      {t && t.notes.length > 0 && (
        <div className="flex flex-col gap-2 border-t border-dashed border-border pt-2">
          <Eyebrow>Agents' notes</Eyebrow>
          {t.notes.map((n) => (
            <p key={n.id} className="text-[13px]">
              {n.text}
              <span className="block font-mono text-xs text-muted-foreground">
                {n.kind.replace("_", " ")} · {actor(n.actor)} · {stamp(n.at)}
              </span>
            </p>
          ))}
        </div>
      )}
      <Link to={`/tasks/${id}`} className="text-[13px] text-mark underline">
        Every request, note and run
      </Link>
      {sales && t && t.status !== "closed" && (
        <form
          className="flex flex-col gap-2 border-t border-dashed border-border pt-3"
          onSubmit={(e) => {
            e.preventDefault();
            ask.mutate({ params: { path: { org, id } }, body: { text } });
          }}
        >
          <label htmlFor="ask-more">
            <Eyebrow>Ask for more</Eyebrow>
          </label>
          <Textarea
            id="ask-more"
            required
            maxLength={2000}
            rows={2}
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder="Twenty more, in Scotland this time."
            className="rounded-lg border-input bg-muted text-sm leading-relaxed"
          />
          <div className="flex items-center gap-3">
            <ActionButton type="submit" size="sm" disabled={ask.isPending}>
              Ask
            </ActionButton>
            {ask.isSuccess && <p className="text-sm text-[var(--ok)]">Asked.</p>}
            <Failed errors={[ask.error]} />
          </div>
        </form>
      )}
    </Section>
  );
}

const stateText: Record<Criterion["state"], [string, Tone]> = {
  met: ["met", "ok"],
  not_met: ["not met", "stop"],
  not_found: ["not found", ""],
  unchecked: ["unchecked", ""],
};

function CriterionRow({ c }: { c: Criterion }) {
  const [text, tone] = stateText[c.state];
  const how = c.state === "not_found" ? "looked" : c.inferred ? "inferred" : "seen on their site";
  return (
    <div className="grid grid-cols-1 items-baseline gap-1 border-t border-dashed border-border px-5 py-2 sm:grid-cols-[168px_96px_1fr] sm:gap-3">
      <span className="font-mono text-xs text-muted-foreground">
        {c.field}
        {c.must && <span className="text-mark"> · must</span>}
      </span>
      <span>
        <Pill tone={tone}>{text}</Pill>
      </span>
      <span className="min-w-0 text-sm">
        {c.at ? (
          <>
            {c.value}
            <span className="block font-mono text-[11px] text-muted-foreground">
              {how} · {actor(c.by ?? "")} · {day(c.at)}
              {c.source_url && (
                <>
                  {" · "}
                  <SourceLink href={c.source_url}>source ↗</SourceLink>
                </>
              )}
            </span>
          </>
        ) : (
          <span className="text-muted-foreground">No agent has checked this yet.</span>
        )}
      </span>
    </div>
  );
}

// SourceLink opens a fact's page; only web addresses are ever links.
function SourceLink({ href, children }: { href: string; children: React.ReactNode }) {
  if (!/^https?:\/\//i.test(href)) return <>{children}</>;
  return (
    <a href={href} target="_blank" rel="noopener noreferrer" className="font-mono text-xs text-mark underline">
      {children}
    </a>
  );
}

function CompanyCard({ company: c, open }: { company: Company; open: boolean }) {
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const [grade, setGrade] = useState(c.grade || "B");
  const review = $api.useMutation("patch", "/o/{org}/api/v1/companies/{id}", { onSuccess: refresh });
  const pick = $api.useMutation("post", "/o/{org}/api/v1/contacts/{id}/pick", { onSuccess: refresh });
  const decide = $api.useMutation("patch", "/o/{org}/api/v1/suggestions/{id}", { onSuccess: refresh });

  return (
    <article className="panel">
      <div className="flex flex-wrap items-start gap-x-6 gap-y-3 px-5 py-4">
        <div className="min-w-[200px] flex-[2_1_220px]">
          <a href={`/o/${org}/companies/${c.id}`} className="font-heading text-lg font-semibold text-foreground hover:text-mark">
            {c.name}
          </a>
          <p className="font-mono text-xs text-muted-foreground">{[c.country, c.segment, c.domain].filter(Boolean).join(" · ")}</p>
        </div>
        {c.criteria.length > 0 && (
          <div className="flex flex-[1_1_180px] flex-col gap-1">
            <Eyebrow>Fit</Eyebrow>
            <span className="font-mono text-[13px] font-medium">{c.fit.text}</span>
          </div>
        )}
        <div className="flex flex-[1_1_150px] flex-col gap-1">
          <Eyebrow>Found by</Eyebrow>
          <div>
            <Pill tone={c.found_by.startsWith("agent:") ? "agent" : ""}>{actor(c.found_by)}</Pill>
          </div>
          <Meta>{stamp(c.created_at)}</Meta>
        </div>
        {sales && (
          <div className="flex flex-[1_1_220px] flex-wrap items-center justify-end gap-2">
            {c.stage !== "qualified" && (
              <>
                <select
                  aria-label="Fit"
                  value={grade}
                  onChange={(e) => setGrade(e.target.value)}
                  className="h-8 rounded-md border border-input bg-muted px-2 font-mono text-xs"
                >
                  {["A", "B", "C"].map((g) => (
                    <option key={g} value={g}>
                      Fit {g}
                    </option>
                  ))}
                </select>
                <ActionButton size="sm" onClick={() => review.mutate({ params: { path: { org, id: c.id } }, body: { stage: "qualified", fit: grade } })}>
                  Qualify
                </ActionButton>
              </>
            )}
            {c.stage !== "rejected" && (
              <ActionButton quiet size="sm" onClick={() => review.mutate({ params: { path: { org, id: c.id } }, body: { stage: "rejected" } })}>
                Reject
              </ActionButton>
            )}
          </div>
        )}
      </div>
      {c.criteria.map((cr) => (
        <CriterionRow key={cr.field} c={cr} />
      ))}
      <details className="group border-t border-border/70 bg-muted" open={open}>
        <summary className="flex cursor-pointer list-none items-center gap-2 px-5 py-2.5 font-mono text-xs text-muted-foreground hover:text-foreground">
          <span className="transition-transform group-open:rotate-90">›</span>
          {c.facts.length} other {c.facts.length === 1 ? "fact" : "facts"} · {c.people.length} {c.people.length === 1 ? "person" : "people"}
        </summary>
        <div className="flex flex-wrap gap-6 px-5 pb-5">
          <div className="min-w-0 flex-[2_1_380px]">
            <Eyebrow>Facts</Eyebrow>
            {c.why && (
              <div className="grid grid-cols-[130px_1fr] gap-3 border-b border-dashed border-border py-2 text-sm">
                <span className="font-mono text-xs text-muted-foreground">why it fits</span>
                <span>{c.why}</span>
              </div>
            )}
            {c.facts.map((f) => (
              <div key={f.id} className="grid grid-cols-[130px_1fr_auto] gap-3 border-b border-dashed border-border py-2 text-sm">
                <span className="font-mono text-xs text-muted-foreground">{f.field}</span>
                <span>{f.value}</span>
                <SourceLink href={f.source_url}>source ↗</SourceLink>
              </div>
            ))}
          </div>
          <div className="min-w-0 flex-[1_1_260px]">
            <Eyebrow>People</Eyebrow>
            {c.people.length === 0 && (
              <p className="py-2 text-sm text-muted-foreground">
                None yet.{" "}
                {sales && <a href={`/o/${org}/app/add?company=${encodeURIComponent(c.name)}`}>Add someone</a>}
              </p>
            )}
            {c.people.map((p) => (
              <div key={p.id} className="flex flex-col gap-1.5 border-b border-dashed border-border py-2.5 text-sm">
                <div>
                  <b>{p.name}</b>
                  <span className="text-muted-foreground">{p.title ? `, ${p.title}` : ""}</span>
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  {p.email_status === "verified" && <Pill tone="ok">email verified</Pill>}
                  {p.email_status === "guess" && <Pill tone="warn">email is a guess</Pill>}
                  {p.linkedin && <SourceLink href={p.linkedin}>LinkedIn ↗</SourceLink>}
                  {p.source_url && <SourceLink href={p.source_url}>source ↗</SourceLink>}
                </div>
                {p.status === "suggested" && sales && (
                  <div>
                    <ActionButton size="xs" onClick={() => pick.mutate({ params: { path: { org, id: p.id } } })}>
                      Pick
                    </ActionButton>
                  </div>
                )}
                {p.status === "suggested" && !sales && <Meta>suggested</Meta>}
                {p.pending.map((d) => (
                  <div key={d.id} className="flex flex-wrap items-center gap-2 rounded-lg bg-card px-3 py-2">
                    <span className="font-mono text-xs text-muted-foreground">{d.field}</span>
                    <span>{d.value}</span>
                    {d.current && <Meta>now {d.current}</Meta>}
                    {d.source_url && <SourceLink href={d.source_url}>source ↗</SourceLink>}
                    {sales && (
                      <span className="ml-auto flex gap-2">
                        <ActionButton size="xs" onClick={() => decide.mutate({ params: { path: { org, id: d.id } }, body: { decision: "accepted" } })}>
                          Accept
                        </ActionButton>
                        <ActionButton quiet size="xs" onClick={() => decide.mutate({ params: { path: { org, id: d.id } }, body: { decision: "dismissed" } })}>
                          Dismiss
                        </ActionButton>
                      </span>
                    )}
                  </div>
                ))}
              </div>
            ))}
          </div>
        </div>
      </details>
    </article>
  );
}
