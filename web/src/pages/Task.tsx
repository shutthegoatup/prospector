import { useState } from "react";
import { useParams } from "react-router";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Empty, Eyebrow, Failed, Meta, PageHead, Pill, Section, useRefresh } from "@/components/app";
import { Pager, Quote } from "@/components/app/more";
import { Textarea } from "@/components/ui/textarea";
import { actor, ago, clock, plural, stamp } from "@/lib/format";
import { useSession } from "@/session";
import { TaskStatus, taskFound, taskLast } from "@/pages/Tasks";

type Asked = components["schemas"]["Asked"];
type Run = components["schemas"]["Run"];
type Work = components["schemas"]["TaskWork"];

const noteKinds: Record<string, string> = { skipped: "Skipped", dead_end: "Dead end", next: "Next" };

const going = (r: Run) => r.status === "running" || r.status === "waiting";

function runState(r: Run) {
  if (r.status === "waiting" && r.wait_until) return "Waiting for the model endpoint's limit, until " + stamp(r.wait_until);
  if (going(r)) return "Working, with " + r.model;
  return r.status + ": " + r.stop_reason;
}

function runText(r: Run) {
  let text = `${plural(r.turns, "turn")} · ${plural(r.tool_calls, "tool call")} · ${plural(r.added, "thing")} added`;
  if (r.episodes > 1) text += " · " + plural(r.episodes, "fresh start");
  if (r.resumes > 0) text += " · carried on after " + plural(r.resumes, "restart");
  return text;
}

function workText(w: Work) {
  let span = stamp(w.start);
  const mins = Math.floor((new Date(w.end).getTime() - new Date(w.start).getTime()) / 60_000);
  if (mins > 0) span += `, ${mins} min`;
  if (w.going) span += " · last change " + ago(w.end);
  if (w.changes === 0) return span + " · picked up, nothing added yet";
  return (
    span +
    " · " +
    [
      `${plural(w.changes, "change")} at ${plural(w.companies, "company", "companies")}`,
      w.people > 0 ? plural(w.people, "person", "people") + " suggested" : "",
      w.drafts > 0 ? plural(w.drafts, "draft") : "",
    ]
      .filter(Boolean)
      .join(", ")
  );
}

function askedMeta(r: Asked, first: boolean) {
  const head = `${r.asked_by} · ${stamp(r.asked_at)} · `;
  if (r.done) return head + "done, says " + actor(r.done_by ?? "");
  const held = r.held_by && r.held_until ? ` · ${actor(r.held_by)} is on it until ${clock(r.held_until)}` : "";
  return head + (first ? "found so far: " : "added since: ") + taskFound(r.added_since.companies, r.added_since.people) + held;
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
  const { org, sales } = useSession();
  const id = Number(useParams().id);
  const refresh = useRefresh();
  const [foundAt, setFoundAt] = useState<string | undefined>();
  const task = $api.useQuery("get", "/o/{org}/api/v1/tasks/{id}", { params: { path: { org, id }, query: { limit: 200, cursor: foundAt } } });
  const change = $api.useMutation("patch", "/o/{org}/api/v1/tasks/{id}", { onSuccess: refresh });

  if (task.error) return <Empty title="No such task" hint="It may be in another organisation, or the link is wrong." />;
  const t = task.data;
  if (!t) return null;
  const open = t.status !== "closed";

  return (
    <>
      <PageHead title={t.title}>
        <TaskStatus status={t.status} />
      </PageHead>
      <div className="flex flex-wrap items-start gap-8">
        <div className="flex min-w-0 flex-[999_1_480px] flex-col gap-5">
          <AskedSection id={id} requests={t.requests} last={taskLast(t)} open={open} />
          <Section title="Found" hint="What agents added for this task, or matched to it. Companies and people wait in Research until someone qualifies or picks them.">
            {t.companies.length === 0 && t.people.length === 0 && <p className="text-sm text-muted-foreground">Nothing yet.</p>}
            {t.companies.map((c) => (
              <div key={c.id} className="flex flex-wrap justify-between gap-2 border-b border-dashed border-border py-2.5 text-sm">
                <a href={`/o/${org}/companies/${c.id}`} className="font-medium underline">
                  {c.name}
                </a>
                <span className="font-mono text-xs text-muted-foreground">{c.stage}</span>
              </div>
            ))}
            {t.people.map((p) => {
              const about = [p.title, p.company].filter((s) => s?.trim()).join(", ");
              return (
                <div key={p.id} className="flex flex-wrap justify-between gap-2 border-b border-dashed border-border py-2.5 text-sm">
                  <span>
                    <b>{p.name}</b> <span className="text-muted-foreground">{about && "· " + about}</span>
                  </span>
                  <span className="font-mono text-xs text-muted-foreground">{p.stage}</span>
                </div>
              );
            })}
            <Pager cursor={foundAt} next={t.next_cursor} go={setFoundAt} />
          </Section>
          <Notes id={id} />
        </div>
        <aside className="flex min-w-0 flex-[1_1_280px] flex-col gap-5">
          <Agents id={id} open={open} />
          {open && <Prompt id={id} />}
          <WorkSoFar id={id} />
          {sales && (
            <div>
              {open ? (
                <ActionButton
                  quiet
                  size="sm"
                  onClick={() =>
                    window.confirm("Close this task? Agents stop seeing it; what was found stays.") &&
                    change.mutate({ params: { path: { org, id } }, body: { closed: true } })
                  }
                >
                  Close task
                </ActionButton>
              ) : (
                <ActionButton quiet size="sm" onClick={() => change.mutate({ params: { path: { org, id } }, body: { closed: false } })}>
                  Reopen task
                </ActionButton>
              )}
              <Failed errors={[change.error]} />
            </div>
          )}
        </aside>
      </div>
    </>
  );
}

function AskedSection({ id, requests, last, open }: { id: number; requests: Asked[]; last: string; open: boolean }) {
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
    <Section title="Asked" hint={last}>
      {requests.map((r, i) => (
        <div key={r.id} className="flex flex-col gap-2 border-b border-dashed border-border py-2.5 text-sm last:border-0">
          <Quote>{r.text}</Quote>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <Meta>{askedMeta(r, i === 0)}</Meta>
            {open && sales && (
              <ActionButton quiet size="xs" onClick={() => mark.mutate({ params: { path: { org, id: r.id } }, body: { done: !r.done } })}>
                {r.done ? "Reopen" : "Mark done"}
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
            <Eyebrow>Ask for more</Eyebrow>
            <Textarea
              required
              maxLength={2000}
              rows={3}
              value={text}
              onChange={(e) => setText(e.target.value)}
              placeholder="Find twenty more, in Scotland this time."
              className="rounded-lg border-input bg-muted text-sm leading-relaxed"
            />
          </label>
          <div className="flex flex-wrap items-center gap-3">
            <ActionButton type="submit" size="sm" disabled={ask.isPending}>
              Ask
            </ActionButton>
            {ask.isSuccess && (
              <p className="text-sm text-[var(--ok)]" role="status">
                Asked.
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
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const [cursor, setCursor] = useState<string | undefined>();
  const notes = $api.useQuery("get", "/o/{org}/api/v1/tasks/{id}/notes", { params: { path: { org, id }, query: { limit: 20, cursor } } });
  const remove = $api.useMutation("delete", "/o/{org}/api/v1/notes/{id}", { onSuccess: refresh });
  const items = notes.data?.items ?? [];

  return (
    <Section title="Agents' notes" hint="What they skipped or couldn't find, in their words. Not checked by anyone.">
      {notes.data && items.length === 0 && <p className="text-sm text-muted-foreground">None.</p>}
      {items.map((n) => (
        <div key={n.id} className="flex flex-col gap-1.5 border-b border-dashed border-border py-2.5 text-sm last:border-0">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="flex flex-wrap items-center gap-2">
              <Pill tone="agent">{noteKinds[n.kind] ?? n.kind}</Pill>
              {n.company && (
                <a href={`/o/${org}/companies/${n.company_id}`} className="font-mono text-xs text-mark underline">
                  {n.company}
                </a>
              )}
            </span>
            {sales && (
              <ActionButton
                quiet
                size="xs"
                onClick={() => window.confirm("Delete this note?") && remove.mutate({ params: { path: { org, id: n.id } } })}
              >
                Delete
              </ActionButton>
            )}
          </div>
          <p className="whitespace-pre-wrap">{n.text}</p>
          <Meta>
            {actor(n.actor)} · {stamp(n.at)}
          </Meta>
        </div>
      ))}
      <Failed errors={[remove.error]} />
      <Pager cursor={cursor} next={notes.data?.next_cursor} go={setCursor} />
    </Section>
  );
}

function Agents({ id, open }: { id: number; open: boolean }) {
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const [cursor, setCursor] = useState<string | undefined>();
  const runs = $api.useQuery("get", "/o/{org}/api/v1/tasks/{task}/runs", { params: { path: { org, task: id }, query: { limit: 10, cursor } } });
  const agents = $api.useQuery("get", "/o/{org}/api/v1/agents", { params: { path: { org } } });
  const start = $api.useMutation("post", "/o/{org}/api/v1/tasks/{task}/runs", { onSuccess: refresh });
  const stop = $api.useMutation("post", "/o/{org}/api/v1/runs/{id}/stop", { onSuccess: refresh });
  const ready = (agents.data?.items ?? []).filter((a) => a.ready);
  const all = runs.data?.items ?? [];
  const live = all.filter(going);
  if (ready.length === 0 && all.length === 0) return null;

  return (
    <Section
      title="Prospector's agents"
      hint="Work the task here, each with the model and tools set on the Agents page, each on its own request. What they find waits in Research."
    >
      {live.map((r) => (
        <div key={r.id} className="flex flex-col gap-1.5 border-b border-dashed border-border py-2 text-sm">
          <span>
            <b>{r.agent_name}</b> · {runState(r)}
          </span>
          <Meta>{runText(r)}</Meta>
          {sales && (
            <div>
              <ActionButton quiet size="sm" onClick={() => stop.mutate({ params: { path: { org, id: r.id } } })}>
                Stop {r.agent_name}
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
                Run {a.name}
              </ActionButton>
            ))}
        </div>
      )}
      <Failed errors={[start.error, stop.error]} />
      {all
        .filter((r) => !going(r))
        .map((r) => (
          <div key={r.id} className="border-b border-dashed border-border py-2 text-sm last:border-0">
            <span className="block">
              <b>{r.agent_name}</b> · {runState(r)}
            </span>
            <Meta>
              {r.started_by} · {stamp(r.started_at)} · {runText(r)}
            </Meta>
          </div>
        ))}
      <Pager cursor={cursor} next={runs.data?.next_cursor} go={setCursor} />
    </Section>
  );
}

function Prompt({ id }: { id: number }) {
  const { org } = useSession();
  const prompt = $api.useQuery("get", "/o/{org}/api/v1/tasks/{id}/prompt", { params: { path: { org, id } } });
  const [copied, setCopied] = useState(false);
  const text = prompt.data?.prompt ?? "";

  return (
    <Section
      title="Hand it to your agent"
      hint="Paste this into an agent connected to this organisation. Clients that show prompts as commands also have it as work_task."
    >
      <Quote>{text}</Quote>
      <div>
        <ActionButton
          size="sm"
          disabled={!text}
          onClick={() => {
            writeClipboard(text);
            setCopied(true);
          }}
        >
          {copied ? "Copied ✓" : "Copy prompt"}
        </ActionButton>
      </div>
    </Section>
  );
}

function WorkSoFar({ id }: { id: number }) {
  const { org } = useSession();
  const [cursor, setCursor] = useState<string | undefined>();
  const work = $api.useQuery("get", "/o/{org}/api/v1/tasks/{id}/work", { params: { path: { org, id }, query: { limit: 10, cursor } } });
  const items = work.data?.items ?? [];

  return (
    <Section title="Work so far" hint="Each stretch of an agent's changes. An agent that is only reading doesn't show.">
      {work.data && items.length === 0 && <p className="text-sm text-muted-foreground">None yet.</p>}
      {items.map((w) => (
        <div key={`${w.actor} ${w.start}`} className="border-b border-dashed border-border py-2 text-sm last:border-0">
          <b className="font-mono text-[13px] text-mark">{actor(w.actor)}</b>
          <span className="block text-muted-foreground">{workText(w)}</span>
        </div>
      ))}
      <Pager cursor={cursor} next={work.data?.next_cursor} go={setCursor} />
    </Section>
  );
}
