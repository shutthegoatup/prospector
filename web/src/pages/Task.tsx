import { useState, type ReactNode } from "react";
import { keepPreviousData } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { defineMessage, defineMessages, FormattedMessage, type MessageDescriptor, useIntl } from "react-intl";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Empty, Eyebrow, Failed, Meta, Pill, Section, stop, usePageTitle, useRefresh } from "@/components/app";
import { Pager, Quote } from "@/components/app/more";
import { Loading, Placeholders, placeholders, Text } from "@/components/app/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { actor, ago, clock, stamp } from "@/lib/format";
import { useSession } from "@/session";
import { TaskStatus, taskFound, taskLast } from "@/pages/Tasks";

type Asked = components["schemas"]["Asked"];
type Run = components["schemas"]["Run"];
type Work = components["schemas"]["TaskWork"];
type Note = components["schemas"]["TaskNoteOut"];
type Say = ReturnType<typeof useIntl>["formatMessage"];

// Names are the same in every language.
const prospector = "Prospector";
const workTask = "work_task";
const dot = " · ";
const lead = "· ";
const comma = ", ";

const bold = (c: ReactNode[]) => <b>{c}</b>;

const m = defineMessages({
  noTask: { defaultMessage: "No such task", id: "2kHGa0", description: "Heading when a task's page is opened for a task that isn't there" },
  noTaskHint: {
    defaultMessage: "It may be in another organisation, or the link is wrong.",
    id: "UepY9r",
    description: "When a task's page is opened for a task that isn't there: why",
  },
  failed: { defaultMessage: "The task couldn't be read.", id: "v3fBNO", description: "Heading when a task failed to load" },
  reload: { defaultMessage: "Reload to try again.", id: "VjgnON", description: "Under the heading when a task failed to load" },
  found: { defaultMessage: "Found", id: "t9BUfG", description: "Heading of a task's section of the companies and people found for it" },
  foundHint: {
    defaultMessage: "What agents added for this task, or matched to it. Companies and people wait in Research until someone qualifies or picks them.",
    id: "l8EYoK",
    description: "Under the heading of a task's section of what was found. Research is the page of that name",
  },
  nothingYet: { defaultMessage: "Nothing yet.", id: "BjVmO+", description: "In a task's section of what was found, when nothing has been" },
  confirmClose: {
    defaultMessage: "Close this task? Agents stop seeing it; what was found stays.",
    id: "RjwDNP",
    description: "Asked before closing a task",
  },
  close: { defaultMessage: "Close task", id: "TcJWxF", description: "Button to close a task" },
  reopenTask: { defaultMessage: "Reopen task", id: "owGgv8", description: "Button to reopen a closed task" },
  asked: { defaultMessage: "Asked", id: "GF7hr+", description: "Heading of a task's section of what has been asked of its agents" },
  reopen: { defaultMessage: "Reopen", id: "nVpam5", description: "Button to mark a request on a task as not done after all" },
  markDone: { defaultMessage: "Mark done", id: "DAGA0t", description: "Button to mark a request on a task done" },
  askMore: { defaultMessage: "Ask for more", id: "2x46wj", description: "Label of the box for asking a task's agents for more" },
  askExample: {
    defaultMessage: "Find twenty more, in Scotland this time.",
    id: "8QeuLs",
    description: "Example in the empty box for asking a task's agents for more",
  },
  ask: { defaultMessage: "Ask", id: "qoAxkM", description: "Button sending a request for more to a task's agents" },
  askedDone: { defaultMessage: "Asked.", id: "PnIxmn", description: "Said once a request for more has been sent to a task's agents" },
  notes: { defaultMessage: "Agents' notes", id: "rO0a+B", description: "Heading of a task's section of the notes its agents left" },
  notesHint: {
    defaultMessage: "What they skipped or couldn't find, in their words. Not checked by anyone.",
    id: "YSzd1j",
    description: "Under the heading of a task's section of agents' notes",
  },
  none: { defaultMessage: "None.", id: "Jv5Qka", description: "When a task has no agents' notes" },
  confirmDelete: { defaultMessage: "Delete this note?", id: "kHdpMy", description: "Asked before deleting an agent's note on a task" },
  delete: { defaultMessage: "Delete", id: "6LR++K", description: "Button to delete an agent's note on a task" },
  agentsHint: {
    defaultMessage: "Work the task here, each with the model and tools set on the Agents page, each on its own request. What they find waits in Research.",
    id: "HLs2rn",
    description: "Under the heading of a task's section of the app's own agents. Agents and Research are the pages of those names",
  },
  handIt: { defaultMessage: "Hand it to your agent", id: "pYIaR/", description: "Heading of a task's section with the prompt to paste into an agent" },
  copied: { defaultMessage: "Copied ✓", id: "OVlUCm", description: "Button to copy a task's prompt, once it has been copied" },
  copy: { defaultMessage: "Copy prompt", id: "tRWcdn", description: "Button to copy a task's prompt" },
  work: { defaultMessage: "Work so far", id: "gYJYKx", description: "Heading of a task's section of the stretches of changes agents made" },
  workHint: {
    defaultMessage: "Each stretch of an agent's changes. An agent that is only reading doesn't show.",
    id: "3v16yg",
    description: "Under the heading of a task's section of agents' work so far",
  },
  noneYet: { defaultMessage: "None yet.", id: "HNgMyX", description: "When no agent has changed anything for a task" },
  pickedUp: { defaultMessage: "picked up, nothing added yet", id: "vEZYzf", description: "In a stretch of an agent's work on a task, when it changed nothing" },
  suggested: { defaultMessage: "suggested", id: "0Yl05V", description: "A person's stage: suggested by an agent, not yet accepted" },
  notContacted: { defaultMessage: "not contacted", id: "nzO1rC", description: "A person's stage: no one has contacted them yet" },
  inOutreach: { defaultMessage: "in outreach", id: "pHTf+E", description: "A person's stage: being contacted" },
  hold: { defaultMessage: "hold", id: "1ScrsB", description: "A person's stage: contact is on hold" },
  declined: { defaultMessage: "declined", id: "X7D4CM", description: "A person's stage: they said no" },
  replied: { defaultMessage: "replied", id: "Pk0UKs", description: "A person's stage: they replied" },
  meeting: { defaultMessage: "meeting", id: "wBVcnU", description: "A person's stage: a meeting is arranged" },
  proposal: { defaultMessage: "proposal", id: "6YRzhQ", description: "A person's stage: a proposal is with them" },
  won: { defaultMessage: "won", id: "jI0/xf", description: "A person's stage: the deal is won" },
  lost: { defaultMessage: "lost", id: "cS1f1R", description: "A person's stage: the deal is lost" },
});

const v = {
  forOffering: defineMessage<{ offering: string }>({
    defaultMessage: "For {offering}",
    id: "m3WkmC",
    description: "Tag on a task saying which offering it is for. {offering} is its name",
  }),
  companyStage: defineMessage<{ stage: string }>({
    defaultMessage: "{stage, select, review {review} qualified {qualified} rejected {rejected} other {{stage}}}",
    id: "gMApmK",
    description: "Where a company found for a task stands: waiting for review, qualified or rejected. Shown small, in lower case",
  }),
  waiting: defineMessage<{ until: string }>({
    defaultMessage: "Waiting for the model endpoint's limit, until {until}",
    id: "a0FDXz",
    description: "An agent's run on a task, held by its model's rate limit. {until} is a date and time",
  }),
  working: defineMessage<{ model: string }>({
    defaultMessage: "Working, with {model}",
    id: "QYp1Gi",
    description: "An agent's run on a task, going. {model} is the model's name",
  }),
  ended: defineMessage<{ status: string; reason: string }>({
    defaultMessage: "{status, select, finished {finished} stopped {stopped} failed {failed} other {{status}}}: {reason}",
    id: "j941lb",
    description: "How an agent's run on a task ended: finished, stopped or failed. {reason} is why it stopped, in the run's own words",
  }),
  runCounts: defineMessage<{ turns: number; calls: number; added: number }>({
    defaultMessage:
      "{turns, plural, one {# turn} other {# turns}} · {calls, plural, one {# tool call} other {# tool calls}} · {added, plural, one {# thing} other {# things}} added",
    id: "BvukbC",
    description: "What an agent's run on a task did: its turns with the model, its tool calls and how many things it added",
  }),
  freshStarts: defineMessage<{ n: number }>({
    defaultMessage: "{n, plural, one {# fresh start} other {# fresh starts}}",
    id: "bJx9Nc",
    description: "After what an agent's run did: how many times it started afresh with a clean context",
  }),
  restarts: defineMessage<{ n: number }>({
    defaultMessage: "carried on after {n, plural, one {# restart} other {# restarts}}",
    id: "cHTUQq",
    description: "After what an agent's run did: how many times it carried on after the server restarted",
  }),
  spanMins: defineMessage<{ start: string; mins: number }>({
    defaultMessage: "{start}, {mins} min",
    id: "Y5Hmk9",
    description: "When a stretch of an agent's work began and how long it went on. {start} is a date and time; {mins} the minutes",
  }),
  lastChange: defineMessage<{ when: string }>({
    defaultMessage: "last change {when}",
    id: "enFDdr",
    description: "In a stretch of an agent's work still going: when it last changed something. {when} is how long ago, such as 5 min ago",
  }),
  changes: defineMessage<{ changes: number; companies: number; people: number; drafts: number }>({
    defaultMessage:
      "{changes, plural, one {# change} other {# changes}} at {companies, plural, one {# company} other {# companies}}{people, plural, =0 {} one {, # person suggested} other {, # people suggested}}{drafts, plural, =0 {} one {, # draft} other {, # drafts}}",
    id: "mExZKp",
    description: "What a stretch of an agent's work on a task did: its changes, at how many companies, and the people it suggested and drafts it wrote, if any",
  }),
  askedDone: defineMessage<{ by: string; at: string; who: string }>({
    defaultMessage: "{by} · {at} · done, says {who}",
    id: "I4318I",
    description: "Under a request on a task that is done. {by} is who asked; {at} when; {who} who said it was done",
  }),
  askedOpen: defineMessage<{ by: string; at: string; since: string; found: string }>({
    defaultMessage: "{by} · {at} · {since, select, first {found so far} other {added since}}: {found}",
    id: "tFxWT0",
    description:
      "Under a request on a task. {by} is who asked; {at} when; {since} says whether it is the task's first request, so what was found counts from the start; {found} how many companies and people, such as 3 companies, 5 people",
  }),
  held: defineMessage<{ who: string; until: string }>({
    defaultMessage: "{who} is on it until {until}",
    id: "69eqzS",
    description: "Under a request on a task, after what was found: who has taken it on. {who} is them, usually an agent; {until} a time of day",
  }),
  noteKind: defineMessage<{ kind: string; other: string }>({
    defaultMessage: "{kind, select, skipped {Skipped} dead_end {Dead end} next {Next} other {{other}}}",
    id: "PV/TDS",
    description: "What kind of note an agent left on a task. {other} is the kind as the server names it, for one not listed",
  }),
  stopAgent: defineMessage<{ agent: string }>({
    defaultMessage: "Stop {agent}",
    id: "E83dus",
    description: "Button to stop an agent's run on a task. {agent} is its name",
  }),
  runAgent: defineMessage<{ agent: string }>({
    defaultMessage: "Run {agent}",
    id: "cUBBoe",
    description: "Button to set an agent going on a task. {agent} is its name",
  }),
  agents: defineMessage<{ brand: string }>({
    defaultMessage: "{brand}'s agents",
    id: "EDfFch",
    description: "Heading of a task's section of the agents the app runs itself. {brand} is the app's name, Prospector",
  }),
  handItHint: defineMessage<{ command: string }>({
    defaultMessage: "Paste this into an agent connected to this organisation. Clients that show prompts as commands also have it as {command}.",
    id: "Mkbdfc",
    description: "Under the heading of a task's section with its prompt. {command} is the prompt's name as a command, work_task",
  }),
};

const personStages: Record<string, MessageDescriptor> = {
  suggested: m.suggested,
  "not contacted": m.notContacted,
  "in outreach": m.inOutreach,
  hold: m.hold,
  declined: m.declined,
  replied: m.replied,
  meeting: m.meeting,
  proposal: m.proposal,
  won: m.won,
  lost: m.lost,
};

const going = (r: Run) => r.status === "running" || r.status === "waiting";

function runState(say: Say, r: Run) {
  if (r.status === "waiting" && r.wait_until) return say(v.waiting, { until: stamp(r.wait_until) });
  if (going(r)) return say(v.working, { model: r.model });
  return say(v.ended, { status: r.status, reason: r.stop_reason });
}

function runText(say: Say, r: Run) {
  const parts = [say(v.runCounts, { turns: r.turns, calls: r.tool_calls, added: r.added })];
  if (r.episodes > 1) parts.push(say(v.freshStarts, { n: r.episodes }));
  if (r.resumes > 0) parts.push(say(v.restarts, { n: r.resumes }));
  return parts.join(dot);
}

function workText(say: Say, w: Work) {
  const mins = Math.floor((new Date(w.end).getTime() - new Date(w.start).getTime()) / 60_000);
  const parts = [mins > 0 ? say(v.spanMins, { start: stamp(w.start), mins }) : stamp(w.start)];
  if (w.going) parts.push(say(v.lastChange, { when: ago(w.end) }));
  parts.push(w.changes === 0 ? say(m.pickedUp) : say(v.changes, { changes: w.changes, companies: w.companies, people: w.people, drafts: w.drafts }));
  return parts.join(dot);
}

function askedMeta(say: Say, r: Asked, first: boolean) {
  const by = r.asked_by;
  const at = stamp(r.asked_at);
  if (r.done) return say(v.askedDone, { by, at, who: actor(r.done_by ?? "") });
  const found = say(v.askedOpen, { by, at, since: first ? "first" : "later", found: taskFound(r.added_since.companies, r.added_since.people) });
  if (!r.held_by || !r.held_until) return found;
  return found + dot + say(v.held, { who: actor(r.held_by), until: clock(r.held_until) });
}

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

export function TaskPage() {
  const say = useIntl().formatMessage;
  const { org, sales } = useSession();
  const { id } = useParams({ from: "/o/$org/tasks/$id" });
  const refresh = useRefresh();
  const [foundAt, setFoundAt] = useState<string | undefined>();
  // Another page of what was found keeps this task on screen, but never another task.
  const task = $api.useQuery(
    "get",
    "/o/{org}/api/v1/tasks/{id}",
    { params: { path: { org, id }, query: { limit: 200, cursor: foundAt } } },
    { placeholderData: (prev) => (prev?.id === id ? prev : undefined) },
  );
  const change = $api.useMutation("patch", "/o/{org}/api/v1/tasks/{id}", { onSuccess: refresh });
  // The queries the side column's first sections make, so it is drawn once they have come:
  // Agents may not be there at all and the prompt runs to any number of lines, so either
  // would otherwise push the rest down when it came.
  const runs = $api.useQuery("get", "/o/{org}/api/v1/tasks/{task}/runs", { params: { path: { org, task: id }, query: { limit: 10, cursor: undefined } } });
  const agents = $api.useQuery("get", "/o/{org}/api/v1/agents", { params: { path: { org } } });
  const open = task.data ? task.data.status !== "closed" : undefined;
  const prompt = $api.useQuery("get", "/o/{org}/api/v1/tasks/{id}/prompt", { params: { path: { org, id } } });
  const side = !runs.isPending && !agents.isPending && !prompt.isPending;

  usePageTitle(task.data?.title);
  if (task.error?.status === 404) return <Empty title={say(m.noTask)} hint={say(m.noTaskHint)} />;
  if (task.error)
    return (
      <div role="alert">
        <h1 className="font-heading text-4xl font-bold tracking-tight">{say(m.failed)}</h1>
        <p className="text-sm text-[var(--stop)]">{say(m.reload)}</p>
      </div>
    );
  const t = task.data;
  const personStage = (stage: string) => (personStages[stage] ? say(personStages[stage]) : stage);

  // While the task loads, only what every task's page starts with: the rest depends on
  // the task, and appears below rather than pushing this down.
  return (
    <Loading busy={!t}>
      <header className="mb-5 flex flex-wrap items-baseline justify-between gap-3">
        <h1 className="font-heading text-4xl font-bold tracking-tight">
          <Text value={t?.title} w="14ch" />
          <span className="text-coral">{stop}</span>
        </h1>
        {t?.offering && <Pill>{say(v.forOffering, { offering: t.offering.name })}</Pill>}
        <TaskStatus status={t?.status} />
      </header>
      <div className="flex flex-wrap items-start gap-8">
        <div className="flex min-w-0 flex-[999_1_480px] flex-col gap-5">
          <AskedSection id={id} requests={t?.requests} last={t && taskLast(t)} open={open} />
          {t && (
            <Section title={say(m.found)} hint={say(m.foundHint)}>
              {t.companies.length === 0 && t.people.length === 0 && <p className="text-sm text-muted-foreground">{say(m.nothingYet)}</p>}
              {t.companies.map((c) => (
                <div key={c.id} className="flex flex-wrap justify-between gap-2 border-b border-dashed border-border py-2.5 text-sm">
                  <Link to="/o/$org/companies/$id" params={{ org, id: c.id }} className="font-medium underline">
                    {c.name}
                  </Link>
                  <span className="font-mono text-xs text-muted-foreground">{say(v.companyStage, { stage: c.stage })}</span>
                </div>
              ))}
              {t.people.map((p) => {
                const about = [p.title, p.company].filter((s) => s?.trim()).join(comma);
                return (
                  <div key={p.id} className="flex flex-wrap justify-between gap-2 border-b border-dashed border-border py-2.5 text-sm">
                    <span>
                      <b>{p.name}</b> <span className="text-muted-foreground">{about && lead + about}</span>
                    </span>
                    <span className="font-mono text-xs text-muted-foreground">{personStage(p.stage)}</span>
                  </div>
                );
              })}
              <Pager cursor={foundAt} next={t.next_cursor} go={setFoundAt} />
            </Section>
          )}
          {t && <Notes key={id} id={id} />}
        </div>
        <aside className="flex min-w-0 flex-[1_1_280px] flex-col gap-5">
          {open !== undefined && side && (
            <>
              <Agents key={id} id={id} open={open} />
              {open && <Prompt id={id} />}
              <WorkSoFar key={id} id={id} />
              {sales && (
                <div>
                  {open ? (
                    <ActionButton
                      quiet
                      size="sm"
                      onClick={() => window.confirm(say(m.confirmClose)) && change.mutate({ params: { path: { org, id } }, body: { closed: true } })}
                    >
                      {say(m.close)}
                    </ActionButton>
                  ) : (
                    <ActionButton quiet size="sm" onClick={() => change.mutate({ params: { path: { org, id } }, body: { closed: false } })}>
                      {say(m.reopenTask)}
                    </ActionButton>
                  )}
                  <Failed errors={[change.error]} />
                </div>
              )}
            </>
          )}
        </aside>
      </div>
    </Loading>
  );
}

// AskedSection is what has been asked of the task's agents; without the task, its heading.
function AskedSection({ id, requests = [], last, open = false }: { id: number; requests?: Asked[]; last?: string; open?: boolean }) {
  const say = useIntl().formatMessage;
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const [text, setText] = useState("");
  const mark = $api.useMutation("patch", "/o/{org}/api/v1/task-requests/{id}", { onSuccess: refresh });
  const ask = $api.useMutation("post", "/o/{org}/api/v1/tasks/{id}/requests", {
    onSuccess: () => {
      setText("");
      refresh();
    },
  });

  return (
    <Section title={say(m.asked)} hint={last}>
      {requests.map((r, i) => (
        <div key={r.id} className="flex flex-col gap-2 border-b border-dashed border-border py-2.5 text-sm last:border-0">
          <Quote>{r.text}</Quote>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <Meta>{askedMeta(say, r, i === 0)}</Meta>
            {open && sales && (
              <ActionButton quiet size="xs" onClick={() => mark.mutate({ params: { path: { org, id: r.id } }, body: { done: !r.done } })}>
                {r.done ? say(m.reopen) : say(m.markDone)}
              </ActionButton>
            )}
          </div>
        </div>
      ))}
      <Failed errors={[mark.error]} />
      {open && sales && (
        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            ask.mutate({ params: { path: { org, id } }, body: { text } });
          }}
        >
          <label className="flex flex-col gap-1.5">
            <Eyebrow>{say(m.askMore)}</Eyebrow>
            <Textarea
              required
              maxLength={2000}
              rows={3}
              value={text}
              onChange={(e) => setText(e.target.value)}
              placeholder={say(m.askExample)}
              className="rounded-lg border-input bg-muted text-sm leading-relaxed"
            />
          </label>
          <div className="flex flex-wrap items-center gap-3">
            <ActionButton type="submit" size="sm" disabled={ask.isPending}>
              {say(m.ask)}
            </ActionButton>
            {ask.isSuccess && (
              <p className="text-sm text-[var(--ok)]" role="status">
                {say(m.askedDone)}
              </p>
            )}
            <Failed errors={[ask.error]} />
          </div>
        </form>
      )}
    </Section>
  );
}

function Notes({ id }: { id: number }) {
  const say = useIntl().formatMessage;
  const { org } = useSession();
  const refresh = useRefresh();
  const [cursor, setCursor] = useState<string | undefined>();
  const notes = $api.useQuery(
    "get",
    "/o/{org}/api/v1/tasks/{id}/notes",
    { params: { path: { org, id }, query: { limit: 20, cursor } } },
    { placeholderData: keepPreviousData },
  );
  const remove = $api.useMutation("delete", "/o/{org}/api/v1/notes/{id}", { onSuccess: refresh });
  const items = notes.data?.items ?? [];

  return (
    <Section title={say(m.notes)} hint={say(m.notesHint)}>
      {notes.data && items.length === 0 && <p className="text-sm text-muted-foreground">{say(m.none)}</p>}
      {!notes.data && !notes.error && (
        <Placeholders className="flex flex-col gap-3">
          {Array.from({ length: placeholders }, (_, i) => (
            <NoteRow key={i} />
          ))}
        </Placeholders>
      )}
      {items.map((n) => (
        <NoteRow key={n.id} n={n} onDelete={() => window.confirm(say(m.confirmDelete)) && remove.mutate({ params: { path: { org, id: n.id } } })} />
      ))}
      <Failed errors={[remove.error]} />
      <Pager cursor={cursor} next={notes.data?.next_cursor} go={setCursor} />
    </Section>
  );
}

// NoteRow is one agent's note, or its outline while the notes load: its text, which runs
// to any number of lines, comes with the note.
function NoteRow({ n, onDelete }: { n?: Note; onDelete?: () => void }) {
  const say = useIntl().formatMessage;
  const { org, sales } = useSession();
  return (
    <div className="flex flex-col gap-1.5 border-b border-dashed border-border py-2.5 text-sm last:border-0">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="flex flex-wrap items-center gap-2">
          <Pill tone="agent">
            <Text value={n && say(v.noteKind, { kind: n.kind, other: n.kind })} w="7ch" />
          </Pill>
          {n?.company && n.company_id !== undefined && (
            <Link to="/o/$org/companies/$id" params={{ org, id: n.company_id }} className="font-mono text-xs text-mark underline">
              {n.company}
            </Link>
          )}
        </span>
        {n && sales && (
          <ActionButton quiet size="xs" onClick={onDelete}>
            {say(m.delete)}
          </ActionButton>
        )}
      </div>
      {n && <p className="whitespace-pre-wrap">{n.text}</p>}
      <Meta>
        <Text value={n && [actor(n.actor), stamp(n.at)].join(dot)} w="24ch" />
      </Meta>
    </div>
  );
}

// AgentRun is an agent's name and where its run is.
function AgentRun({ r }: { r: Run }) {
  const say = useIntl().formatMessage;
  return (
    <FormattedMessage
      defaultMessage="<b>{agent}</b> · {state}"
      id="FfPs3R"
      description="An agent's run on a task. {agent} is its name; {state} where its run is, such as Working, with a model, or finished with why it stopped"
      values={{ agent: r.agent_name, state: runState(say, r), b: bold }}
    />
  );
}

function Agents({ id, open }: { id: number; open: boolean }) {
  const say = useIntl().formatMessage;
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const [cursor, setCursor] = useState<string | undefined>();
  const runs = $api.useQuery(
    "get",
    "/o/{org}/api/v1/tasks/{task}/runs",
    { params: { path: { org, task: id }, query: { limit: 10, cursor } } },
    { placeholderData: keepPreviousData },
  );
  const agents = $api.useQuery("get", "/o/{org}/api/v1/agents", { params: { path: { org } } });
  const start = $api.useMutation("post", "/o/{org}/api/v1/tasks/{task}/runs", { onSuccess: refresh });
  const halt = $api.useMutation("post", "/o/{org}/api/v1/runs/{id}/stop", { onSuccess: refresh });
  const ready = (agents.data?.items ?? []).filter((a) => a.ready);
  const all = runs.data?.items ?? [];
  const live = all.filter(going);
  if (ready.length === 0 && all.length === 0) return null;

  return (
    <Section title={say(v.agents, { brand: prospector })} hint={say(m.agentsHint)}>
      {live.map((r) => (
        <div key={r.id} className="flex flex-col gap-1.5 border-b border-dashed border-border py-2 text-sm">
          <span>
            <AgentRun r={r} />
          </span>
          <Meta>{runText(say, r)}</Meta>
          {sales && (
            <div>
              <ActionButton quiet size="sm" onClick={() => halt.mutate({ params: { path: { org, id: r.id } } })}>
                {say(v.stopAgent, { agent: r.agent_name })}
              </ActionButton>
            </div>
          )}
        </div>
      ))}
      {open && sales && (
        <div className="flex flex-wrap gap-2">
          {ready
            .filter((a) => !live.some((r) => r.agent === a.id))
            .map((a) => (
              <ActionButton key={a.id} size="sm" onClick={() => start.mutate({ params: { path: { org, task: id } }, body: { agent: a.id } })}>
                {say(v.runAgent, { agent: a.name })}
              </ActionButton>
            ))}
        </div>
      )}
      <Failed errors={[start.error, halt.error]} />
      {all
        .filter((r) => !going(r))
        .map((r) => (
          <div key={r.id} className="border-b border-dashed border-border py-2 text-sm last:border-0">
            <span className="block">
              <AgentRun r={r} />
            </span>
            <Meta>{[r.started_by, stamp(r.started_at), runText(say, r)].join(dot)}</Meta>
          </div>
        ))}
      <Pager cursor={cursor} next={runs.data?.next_cursor} go={setCursor} />
    </Section>
  );
}

function Prompt({ id }: { id: number }) {
  const say = useIntl().formatMessage;
  const { org } = useSession();
  const prompt = $api.useQuery("get", "/o/{org}/api/v1/tasks/{id}/prompt", { params: { path: { org, id } } });
  const [copied, setCopied] = useState(false);
  const text = prompt.data?.prompt ?? "";

  return (
    <Section title={say(m.handIt)} hint={say(v.handItHint, { command: workTask })}>
      {/* For an agent, which reads English whatever the page is in (WCAG 3.1.2). */}
      <div lang="en">
        <Quote>{text}</Quote>
      </div>
      <div>
        <ActionButton
          size="sm"
          disabled={!text}
          onClick={() => {
            writeClipboard(text);
            setCopied(true);
          }}
        >
          {copied ? say(m.copied) : say(m.copy)}
        </ActionButton>
      </div>
    </Section>
  );
}

function WorkSoFar({ id }: { id: number }) {
  const say = useIntl().formatMessage;
  const { org } = useSession();
  const [cursor, setCursor] = useState<string | undefined>();
  const work = $api.useQuery(
    "get",
    "/o/{org}/api/v1/tasks/{id}/work",
    { params: { path: { org, id }, query: { limit: 10, cursor } } },
    { placeholderData: keepPreviousData },
  );
  const items = work.data?.items ?? [];

  return (
    <Section title={say(m.work)} hint={say(m.workHint)}>
      {work.data && items.length === 0 && <p className="text-sm text-muted-foreground">{say(m.noneYet)}</p>}
      {!work.data && !work.error && (
        <Placeholders className="flex flex-col gap-3">
          {Array.from({ length: placeholders }, (_, i) => (
            <WorkRow key={i} />
          ))}
        </Placeholders>
      )}
      {items.map((w) => (
        <WorkRow key={`${w.actor} ${w.start}`} w={w} />
      ))}
      <Pager cursor={cursor} next={work.data?.next_cursor} go={setCursor} />
    </Section>
  );
}

// WorkRow is one stretch of an agent's changes, or its outline while they load.
function WorkRow({ w }: { w?: Work }) {
  const say = useIntl().formatMessage;
  return (
    <div className="border-b border-dashed border-border py-2 text-sm last:border-0">
      <b className="font-mono text-[13px] text-mark">
        <Text value={w && actor(w.actor)} w="10ch" />
      </b>
      <span className="block text-muted-foreground">
        <Text value={w && workText(say, w)} w="28ch" />
      </span>
    </div>
  );
}
