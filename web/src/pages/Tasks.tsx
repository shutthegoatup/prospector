import { useEffect, useState } from "react";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Empty, Eyebrow, Failed, FilterChip, Meta, PageHead, Pill, type Tone, Whose } from "@/components/app";
import { Pager } from "@/components/app/more";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { actor, ago, plural } from "@/lib/format";
import { useSession } from "@/session";

type TaskBrief = components["schemas"]["TaskBrief"];

const statuses: Record<string, [string, Tone]> = {
  working: ["Being worked on", "agent"],
  done: ["Done", "ok"],
  closed: ["Closed", ""],
};

export function TaskStatus({ status }: { status: string }) {
  const [label, tone] = statuses[status] ?? ["Waiting for an agent", "warn"];
  return <Pill tone={tone}>{label}</Pill>;
}

export function taskLast(t: TaskBrief) {
  if (!t.last_agent || !t.last_agent_at) return "No agent has picked this up yet";
  const did = t.last_agent_action === "task:pickup" ? "picked it up" : "last change";
  return `${actor(t.last_agent)} · ${did} ${ago(t.last_agent_at)}`;
}

export function taskFound(companies: number, people: number) {
  return `${plural(companies, "company", "companies")}, ${plural(people, "person", "people")}`;
}

export function TasksPage() {
  const { org, sales } = useSession();
  const { closed = false } = useSearch({ from: "/o/$org/tasks" });
  const [cursor, setCursor] = useState<string | undefined>();
  useEffect(() => setCursor(undefined), [closed]);
  const tasks = $api.useQuery("get", "/o/{org}/api/v1/tasks", { params: { path: { org }, query: { closed, limit: 30, cursor } } });

  return (
    <>
      <PageHead title="Tasks" />
      <p className="mb-5 max-w-[70ch] text-sm text-muted-foreground">
        Write what you want found. An agent picks it up, and what it finds waits in Research for you. Anyone can ask for more later, and a
        different agent can carry on from what is already here.
      </p>
      <Whose area="sales" work="Tasks for agents are" can={sales} />
      <div className="flex flex-wrap items-start gap-8">
        <div className="flex min-w-0 flex-[999_1_480px] flex-col gap-3">
          <div className="flex flex-wrap gap-2">
            <FilterChip to="/o/$org/tasks" params={{ org }} label="Open" count={tasks.data?.open ?? 0} on={!closed} />
            <FilterChip to="/o/$org/tasks" params={{ org }} search={{ closed: true }} label="With closed" count={tasks.data?.all ?? 0} on={closed} />
          </div>
          {tasks.error && <p className="text-sm text-[var(--stop)]">The tasks couldn't be read. Reload to try again.</p>}
          {tasks.data && tasks.data.items.length === 0 && (
            <Empty title="No tasks yet" hint="Write a brief and hand it to your agent with the prompt on its page." />
          )}
          {tasks.data?.items.map((t) => {
            const latest = t.open_requests[t.open_requests.length - 1];
            return (
              <article key={t.id} className="flex flex-col gap-2.5 rounded-xl border border-border bg-card p-3.5 text-sm">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <Link to="/o/$org/tasks/$id" params={{ org, id: t.id }} className="font-medium underline">
                    {t.title}
                  </Link>
                  <TaskStatus status={t.status} />
                </div>
                {latest && <p className="text-muted-foreground">{latest.text.split("\n")[0]}</p>}
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <Meta>{taskLast(t)}</Meta>
                  <Meta>{taskFound(t.found.companies, t.found.people)}</Meta>
                </div>
              </article>
            );
          })}
          <Pager cursor={cursor} next={tasks.data?.next_cursor} go={setCursor} />
        </div>
        {sales && <TaskCreate />}
      </div>
    </>
  );
}

function TaskCreate() {
  const { org } = useSession();
  const navigate = useNavigate();
  const [brief, setBrief] = useState("");
  const [title, setTitle] = useState("");
  const create = $api.useMutation("post", "/o/{org}/api/v1/tasks", { onSuccess: (t) => navigate({ to: "/o/$org/tasks/$id", params: { org, id: t.id } }) });

  return (
    <aside className="panel flex min-w-0 flex-[1_1_280px] flex-col gap-3 p-5 text-sm">
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate({ params: { path: { org } }, body: { title, brief } });
        }}
      >
        <label className="flex flex-col gap-1.5">
          <Eyebrow>What should be found?</Eyebrow>
          <Textarea
            required
            maxLength={4000}
            rows={7}
            value={brief}
            onChange={(e) => setBrief(e.target.value)}
            placeholder="Heads of procurement at UK kitchen makers with their own showrooms. About twenty, with the page that shows each one's role."
            className="rounded-lg border-input bg-muted text-sm leading-relaxed"
          />
        </label>
        <label className="flex flex-col gap-1.5">
          <Eyebrow>Short name (optional)</Eyebrow>
          <Input maxLength={60} value={title} onChange={(e) => setTitle(e.target.value)} className="h-10 rounded-lg border-input bg-muted text-sm" />
        </label>
        <div className="flex flex-wrap items-center gap-3">
          <ActionButton type="submit" disabled={create.isPending}>
            Create task
          </ActionButton>
          <Failed errors={[create.error]} />
        </div>
      </form>
      <p className="text-muted-foreground">
        Agents can read the brief and everyone's requests. Don't put anything in them you wouldn't put in a note to a colleague.
      </p>
    </aside>
  );
}
