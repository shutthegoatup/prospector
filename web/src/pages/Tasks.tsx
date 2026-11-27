import { useState } from "react";
import { keepPreviousData } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { defineMessage, defineMessages, useIntl } from "react-intl";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Empty, Eyebrow, Failed, FilterChip, Meta, PageHead, Pill, type Tone, Whose } from "@/components/app";
import { Pager } from "@/components/app/more";
import { Bar, Placeholders, placeholders, Text } from "@/components/app/skeleton";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { intl } from "@/i18n";
import { actor, ago } from "@/lib/format";
import { useKeyedState } from "@/lib/keyed";
import { useOfferings } from "@/lib/offerings";
import { useSession } from "@/session";

type TaskBrief = components["schemas"]["TaskBrief"];

const m = defineMessages({
  tasks: { defaultMessage: "Tasks", id: "yA+4cV", description: "Title and heading of the page listing tasks for agents" },
  intro: {
    defaultMessage:
      "Write what you want found. An agent picks it up, and what it finds waits in Research for you. Anyone can ask for more later, and a different agent can carry on from what is already here.",
    id: "gKQAzy",
    description: "Paragraph under the tasks page's heading saying what tasks are for. Research is the page of that name",
  },
  whose: {
    defaultMessage: "Tasks for agents are sales work, so here you read it.",
    id: "3batB0",
    description: "On the tasks page, to a member who doesn't do sales: why they can only read it",
  },
  open: { defaultMessage: "Open", id: "RRstyD", description: "Filter on the tasks page: only tasks still open" },
  withClosed: { defaultMessage: "With closed", id: "H+qZnw", description: "Filter on the tasks page: open tasks and closed ones too" },
  failed: { defaultMessage: "The tasks couldn't be read. Reload to try again.", id: "20GPXH", description: "When the list of tasks failed to load" },
  noTasks: { defaultMessage: "No tasks yet", id: "PY2dfR", description: "Heading on the tasks page when there are none" },
  noTasksHint: {
    defaultMessage: "Write a brief and hand it to your agent with the prompt on its page.",
    id: "h+YFUf",
    description: "On the tasks page when there are none: how to start one",
  },
  noAgentYet: { defaultMessage: "No agent has picked this up yet", id: "LN41oo", description: "On a task, when no agent has worked on it" },
  whatFound: { defaultMessage: "What should be found?", id: "hiTB5H", description: "Label of the box where a new task's brief is written" },
  briefExample: {
    defaultMessage: "Heads of procurement at UK kitchen makers with their own showrooms. About twenty, with the page that shows each one's role.",
    id: "6q5dsX",
    description: "Example brief in the empty box for a new task",
  },
  for: { defaultMessage: "For", id: "e3uNMz", description: "Label of the field picking which offering a new task is for" },
  ownOffering: {
    defaultMessage: "Your own offering",
    id: "C6/+rt",
    description: "In the field picking which offering a new task is for: the organisation's own",
  },
  forWhat: {
    defaultMessage: "Agents get its look-for fields, and what they find waits in its review.",
    id: "YRZuQR",
    description: "Under the field picking which offering a new task is for: what picking one does",
  },
  shortName: { defaultMessage: "Short name (optional)", id: "FukJLV", description: "Label of the field giving a new task a short name" },
  create: { defaultMessage: "Create task", id: "ITXTKL", description: "Button to create a new task" },
  privacy: {
    defaultMessage: "Agents can read the brief and everyone's requests. Don't put anything in them you wouldn't put in a note to a colleague.",
    id: "aspFMC",
    description: "Under the form for a new task: a warning that agents read what is written",
  },
});

const v = {
  status: defineMessage<{ status: string }>({
    defaultMessage: "{status, select, working {Being worked on} done {Done} closed {Closed} other {Waiting for an agent}}",
    id: "xDZiTw",
    description: "Where a task stands: an agent is working on it, it is done, closed, or waiting for an agent to pick it up",
  }),
  last: defineMessage<{ who: string; did: string; when: string }>({
    defaultMessage: "{who} · {did, select, pickup {picked it up} other {last change}} {when}",
    id: "uwv0/U",
    description:
      "On a task: the agent that last worked on it, and when. {who} is the agent; {did} says whether it only picked the task up; {when} is how long ago, such as 5 min ago, or a date",
  }),
  found: defineMessage<{ companies: number; people: number }>({
    defaultMessage: "{companies, plural, one {# company} other {# companies}}, {people, plural, one {# person} other {# people}}",
    id: "y2Lucp",
    description: "How many companies and people agents found for a task",
  }),
  forOffering: defineMessage<{ offering: string | undefined }>({
    defaultMessage: "For {offering}",
    id: "m3WkmC",
    description: "Tag on a task saying which offering it is for. {offering} is its name",
  }),
};

const tones: Record<string, Tone> = { working: "agent", done: "ok", closed: "" };

// TaskStatus is where a task stands; without a status it is the same pill, hidden in a
// pulsing box, while the task loads.
export function TaskStatus({ status }: { status?: string }) {
  const say = useIntl().formatMessage;
  if (status === undefined) {
    return (
      <span aria-hidden className="inline-flex motion-safe:animate-pulse rounded-full bg-accent">
        <span className="invisible inline-flex">
          <Pill>{say(v.status, { status: "waiting" })}</Pill>
        </span>
      </span>
    );
  }
  return <Pill tone={tones[status] ?? "warn"}>{say(v.status, { status })}</Pill>;
}

export function taskLast(t: TaskBrief) {
  if (!t.last_agent || !t.last_agent_at) return intl.formatMessage(m.noAgentYet);
  const did = t.last_agent_action === "task:pickup" ? "pickup" : "change";
  return intl.formatMessage(v.last, { who: actor(t.last_agent), did, when: ago(t.last_agent_at) });
}

export function taskFound(companies: number, people: number) {
  return intl.formatMessage(v.found, { companies, people });
}

export function TasksPage() {
  const say = useIntl().formatMessage;
  const { org, sales } = useSession();
  const { closed = false } = useSearch({ from: "/o/$org/tasks" });
  // Open and closed tasks are two lists, each with its own cursor.
  const [cursor, setCursor] = useKeyedState<string | undefined>([closed], () => undefined);
  const tasks = $api.useQuery(
    "get",
    "/o/{org}/api/v1/tasks",
    { params: { path: { org }, query: { closed, limit: 30, cursor } } },
    { placeholderData: keepPreviousData },
  );

  return (
    <>
      <PageHead title={say(m.tasks)} />
      <p className="mb-5 max-w-[70ch] text-sm text-muted-foreground">{say(m.intro)}</p>
      <Whose area="sales" said={say(m.whose)} can={sales} />
      <div className="flex flex-wrap items-start gap-8">
        <div className="flex min-w-0 flex-[999_1_480px] flex-col gap-3">
          <div className="flex flex-wrap gap-2">
            <FilterChip to="/o/$org/tasks" params={{ org }} label={say(m.open)} count={tasks.data?.open} on={!closed} />
            <FilterChip to="/o/$org/tasks" params={{ org }} search={{ closed: true }} label={say(m.withClosed)} count={tasks.data?.all} on={closed} />
          </div>
          {tasks.error && (
            <p className="text-sm text-[var(--stop)]" role="alert">
              {say(m.failed)}
            </p>
          )}
          {tasks.data && tasks.data.items.length === 0 && <Empty title={say(m.noTasks)} hint={say(m.noTasksHint)} />}
          {!tasks.data && !tasks.error && (
            <Placeholders className="flex flex-col gap-3">
              {Array.from({ length: placeholders }, (_, i) => (
                <TaskCard key={i} />
              ))}
            </Placeholders>
          )}
          {tasks.data?.items.map((t) => (
            <TaskCard key={t.id} t={t} />
          ))}
          <Pager cursor={cursor} next={tasks.data?.next_cursor} go={setCursor} />
        </div>
        {sales && <TaskCreate />}
      </div>
    </>
  );
}

// TaskCard is one task in the list, or its outline while the list loads.
function TaskCard({ t }: { t?: TaskBrief }) {
  const say = useIntl().formatMessage;
  const { org } = useSession();
  const offerings = useOfferings();
  const latest = t?.open_requests[t.open_requests.length - 1];
  const offering = t && offerings.find((o) => o.id === t.offering_id);
  return (
    <article className="flex flex-col gap-2.5 rounded-xl border border-border bg-card p-3.5 text-sm">
      <div className="flex flex-wrap items-center justify-between gap-2">
        {t ? (
          <Link to="/o/$org/tasks/$id" params={{ org, id: t.id }} className="font-medium underline">
            {t.title}
          </Link>
        ) : (
          <Bar w="22ch" />
        )}
        <span className="flex items-center gap-2">
          {offering && <Pill>{say(v.forOffering, { offering: offering.name })}</Pill>}
          <TaskStatus status={t?.status} />
        </span>
      </div>
      {latest && <p className="text-muted-foreground">{latest.text.split("\n")[0]}</p>}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Meta>
          <Text value={t && taskLast(t)} w="30ch" />
        </Meta>
        <Meta>
          <Text value={t && taskFound(t.found.companies, t.found.people)} w="18ch" />
        </Meta>
      </div>
    </article>
  );
}

function TaskCreate() {
  const say = useIntl().formatMessage;
  const { org } = useSession();
  const navigate = useNavigate();
  const [brief, setBrief] = useState("");
  const [title, setTitle] = useState("");
  const [offering, setOffering] = useState(0);
  const offerings = useOfferings();
  // The same query as useOfferings, to know whether the "For" field will be there: what
  // follows it waits rather than being pushed down when it comes.
  const offered = $api.useQuery("get", "/o/{org}/api/v1/offerings", { params: { path: { org }, query: { limit: 200 } } });
  const known = !offered.isPending;
  const create = $api.useMutation("post", "/o/{org}/api/v1/tasks", { onSuccess: (t) => navigate({ to: "/o/$org/tasks/$id", params: { org, id: t.id } }) });

  return (
    <aside className="panel flex min-w-0 flex-[1_1_280px] flex-col gap-3 p-5 text-sm">
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate({ params: { path: { org } }, body: { title, brief, offering: offering || undefined } });
        }}
      >
        <label className="flex flex-col gap-1.5">
          <Eyebrow>{say(m.whatFound)}</Eyebrow>
          <Textarea
            required
            maxLength={4000}
            rows={7}
            value={brief}
            onChange={(e) => setBrief(e.target.value)}
            placeholder={say(m.briefExample)}
            className="rounded-lg border-input bg-muted text-sm leading-relaxed"
          />
        </label>
        {known && offerings.length > 0 && (
          <label className="flex flex-col gap-1.5">
            <Eyebrow>{say(m.for)}</Eyebrow>
            <select
              value={offering}
              onChange={(e) => setOffering(Number(e.target.value))}
              className="h-10 rounded-lg border border-input bg-muted px-3 text-sm"
            >
              <option value={0}>{say(m.ownOffering)}</option>
              {offerings.map((o) => (
                <option key={o.id} value={o.id}>
                  {o.name}
                </option>
              ))}
            </select>
            <Meta>{say(m.forWhat)}</Meta>
          </label>
        )}
        {known && (
          <>
            <label className="flex flex-col gap-1.5">
              <Eyebrow>{say(m.shortName)}</Eyebrow>
              <Input maxLength={60} value={title} onChange={(e) => setTitle(e.target.value)} className="h-10 rounded-lg border-input bg-muted text-sm" />
            </label>
            <div className="flex flex-wrap items-center gap-3">
              <ActionButton type="submit" disabled={create.isPending}>
                {say(m.create)}
              </ActionButton>
              <Failed errors={[create.error]} />
            </div>
          </>
        )}
      </form>
      {known && <p className="text-muted-foreground">{say(m.privacy)}</p>}
    </aside>
  );
}
