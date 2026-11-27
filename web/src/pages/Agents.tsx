import { Link } from "@tanstack/react-router";
import { type FormEvent, type ReactNode, useState } from "react";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Eyebrow, Failed, Meta, PageHead, Section, useRefresh } from "@/components/app";
import { Bar, Loading, Placeholders, placeholders, Settled, Text } from "@/components/app/skeleton";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { actor, plural, stamp } from "@/lib/format";
import { intl } from "@/i18n";
import { cn } from "@/lib/utils";
import { detailNames, month, vendorCosts, vendorKinds, vendorName, vendorTold } from "@/lib/vendors";
import { useSession } from "@/session";

type Agent = components["schemas"]["Agent"];
type Key = components["schemas"]["APIKey"];
type Connect = components["schemas"]["Connect"];
type Scope = components["schemas"]["Scope"];
type Rail = components["schemas"]["AgentState"]["rail"];
type Enricher = components["schemas"]["Enricher"];

const fieldClass = "h-10 rounded-lg border border-input bg-muted px-3 text-sm disabled:opacity-60";
const cardClass = "rail rounded-[14px] border-2 border-ink bg-card";
const runsShown = 12;
const activityShown = 20;

const stateText: Record<Rail, string> = { ok: "text-[var(--ok)]", warn: "text-[var(--warn)]", stop: "text-[var(--stop)]" };
const fill: Record<Rail, string> = { ok: "bg-[var(--ok-fill)]", warn: "bg-[var(--warn-fill)]", stop: "bg-[var(--stop-fill)]" };
const factText: Record<string, string> = { warn: "text-[var(--warn)]", stop: "text-[var(--stop)]", none: "text-muted-foreground" };

export function AgentsPage() {
  const { org } = useSession();
  const agents = $api.useQuery("get", "/o/{org}/api/v1/agents", { params: { path: { org }, query: { limit: 200 } } });
  const keys = $api.useQuery("get", "/o/{org}/api/v1/keys", { params: { path: { org }, query: { limit: 200 } } });
  const connect = $api.useQuery("get", "/o/{org}/api/v1/connect", { params: { path: { org } } });
  const settings = $api.useQuery("get", "/o/{org}/api/v1/settings", { params: { path: { org } } });

  return (
    <>
      <PageHead title="Agents & API" />
      <p className="mb-5 max-w-[72ch] text-sm text-muted-foreground">
        Agents research, add and draft for{" "}
        <b className="text-foreground">
          <Text value={settings.data?.name ?? (settings.error ? "" : undefined)} w="8ch" />
        </b>
        . Some Prospector runs itself; others are your own and connect with a key, which works for this organisation only. What any of them adds waits in
        Research for a person; agents can draft, but only people send.
      </p>
      {/* Its sections stack, and each list's length decides where the next one starts. */}
      <Settled className="flex flex-wrap items-start gap-5">
        <div className="flex min-w-0 flex-[3_1_560px] flex-col gap-5">
          <Roster agents={agents.data?.items} keys={keys.data?.items} connect={connect.data} errors={[agents.error, keys.error, connect.error]} />
          <DataVendors />
        </div>
        <div className="flex min-w-0 flex-[2_1_340px] flex-col gap-5">
          <Scores />
          <Work />
          <Section title="Tools agents get" hint="Writes need a source URL, and go through this organisation's never-contact list.">
            <Loading busy={!connect.data && !connect.error}>
              {!connect.data && !connect.error && (
                <Placeholders>
                  {Array.from({ length: placeholders }, (_, i) => (
                    <ToolRow key={`loading-${i}`} />
                  ))}
                </Placeholders>
              )}
              {connect.data?.tools.map((t) => (
                <ToolRow key={t.name} t={t} />
              ))}
            </Loading>
            <Failed errors={[connect.error]} />
          </Section>
          <Activity />
        </div>
      </Settled>
    </>
  );
}

function ToolRow({ t }: { t?: Connect["tools"][number] }) {
  return (
    <div className="grid grid-cols-[120px_1fr] gap-3 border-b border-dashed border-border py-2 text-sm last:border-0">
      <code className="font-mono text-[13px] text-mark">
        <Text value={t?.name} w="10ch" />
      </code>
      <span>
        {t ? (
          <>
            {t.what} <span className="font-mono text-[11px] text-muted-foreground">{t.scope}</span>
          </>
        ) : (
          <Bar w="24ch" />
        )}
      </span>
    </div>
  );
}

// Roster is every agent in one list, each card's offset colour its state; while they load,
// a tally and a couple of cards in outline.
function Roster({ agents, keys, connect, errors }: { agents?: Agent[]; keys?: Key[]; connect?: Connect; errors: ({ detail?: string } | null | undefined)[] }) {
  const { owner } = useSession();
  const loaded = agents && keys && connect ? { agents, keys, connect } : undefined;
  const loading = !loaded && !errors.some(Boolean);
  // Each of Prospector's agents has a built-in key of its own, shown on its card instead.
  const outside = loaded?.keys.filter((k) => !k.builtin) ?? [];
  const tally = (rail: Rail) => (loaded?.agents ?? []).filter((a) => a.state.rail === rail).length;
  const none = loaded !== undefined && loaded.agents.length === 0 && outside.length === 0;
  return (
    <Section
      title="Who works here"
      hint="Every agent in one list: those Prospector runs and those that connect with a key. The colour behind each card is its state."
    >
      {loaded && !loaded.connect.can_keep_keys && owner && (
        <p className="text-sm text-[var(--stop)]" role="alert">
          This server has no secret key, so it can't keep an API key: whoever runs it needs to set PROSPECTOR_SECRET_KEYS first.
        </p>
      )}
      {loading && (
        <Placeholders className="flex flex-wrap gap-2 font-mono text-xs">
          <TallyChip text="ready" />
        </Placeholders>
      )}
      {loaded && loaded.agents.length > 0 && (
        <div className="flex flex-wrap gap-2 font-mono text-xs" aria-label="Prospector's agents">
          <TallyChip rail="ok" n={tally("ok")} text="ready" />
          <TallyChip rail="warn" n={tally("warn")} text="missing something" />
          <TallyChip rail="stop" n={tally("stop")} text="can't run" />
        </div>
      )}
      <div className="flex flex-col gap-4">
        {none && <p className="text-sm text-muted-foreground">No agents yet.</p>}
        {loading && (
          <Placeholders className="flex flex-col gap-4">
            {[0, 1].map((i) => (
              <AgentCard key={`loading-${i}`} />
            ))}
          </Placeholders>
        )}
        {loaded?.agents.map((a) => (
          <AgentCard key={a.id} agent={a} agents={loaded.agents} connect={loaded.connect} />
        ))}
        {loaded && outside.map((k) => <KeyCard key={k.id} k={k} scopes={loaded.connect.scopes} />)}
        {(loaded || loading) && <AddAgent none={none} agents={loaded?.agents} connect={loaded?.connect} />}
      </div>
      <Failed errors={errors} />
      <p className="flex flex-wrap gap-x-4 gap-y-1 font-mono text-xs text-muted-foreground">
        <span>
          <i className="mr-1.5 inline-block h-2.5 w-3.5 rounded-sm border border-ink bg-[var(--ok-fill)] align-[-1px]" />
          ready
        </span>
        <span>
          <i className="mr-1.5 inline-block h-2.5 w-3.5 rounded-sm border border-ink bg-[var(--warn-fill)] align-[-1px]" />
          runs, missing something
        </span>
        <span>
          <i className="mr-1.5 inline-block h-2.5 w-3.5 rounded-sm border border-ink bg-[var(--stop-fill)] align-[-1px]" />
          can't run: no key, endpoint or model
        </span>
      </p>
    </Section>
  );
}

// TallyChip without a count is its outline while the roster loads.
function TallyChip({ rail, n, text }: { rail?: Rail; n?: number; text: string }) {
  if (n === 0) return null;
  return (
    <span className="rounded-full border-[1.5px] border-ink bg-card px-2.5 py-0.5">
      <i className={cn("mr-1.5 inline-block size-2 rounded-full", rail ? fill[rail] : "motion-safe:animate-pulse bg-accent")} />
      {n === undefined ? <Bar w="7ch" /> : `${n} ${text}`}
    </span>
  );
}

function Tag({ quiet, children }: { quiet?: boolean; children: ReactNode }) {
  return (
    <span className={cn("rounded-md bg-secondary px-1.5 py-0.5 font-mono text-[11px] font-normal", quiet ? "text-muted-foreground" : "text-[var(--agent)]")}>
      {children}
    </span>
  );
}

function AgentFact({ label, value, rail = "" }: { label: string; value?: string; rail?: string }) {
  return (
    <span className="flex min-w-0 flex-col gap-0.5 border-b border-dashed border-border px-4 py-2.5 sm:border-r sm:border-b-0 sm:last:border-r-0">
      <Eyebrow>{label}</Eyebrow>
      <span className={cn("font-mono text-[13px] break-words", factText[rail])}>
        <Text value={value} w="10ch" />
      </span>
    </span>
  );
}

// AgentCard is one of Prospector's agents, or its outline while the roster loads.
function AgentCard({ agent: v, agents, connect }: { agent?: Agent; agents?: Agent[]; connect?: Connect }) {
  const { owner } = useSession();
  const st = v?.state;
  return (
    <details className={cardClass} data-state={st?.rail} id={v && `agent-${v.id}`}>
      <summary className="cursor-pointer list-none [&::-webkit-details-marker]:hidden">
        <span className="flex flex-wrap items-start justify-between gap-3 px-4 pt-4 pb-3">
          <span className="flex min-w-0 flex-col gap-0.5">
            <span className="flex flex-wrap items-center gap-2 font-heading text-[17px] font-bold">
              <Text value={v?.name} w="10ch" />
              <Tag>Prospector runs it</Tag>
              {v?.thinking_off && <Tag quiet>thinking off</Tag>}
            </span>
            <span className={cn("font-mono text-xs", st && stateText[st.rail])}>
              <Text value={st?.line} w="22ch" />
            </span>
          </span>
          <span className="rounded-lg border-[1.5px] border-ink bg-card px-2.5 py-1 text-xs font-semibold">{owner ? "Setup" : "Details"}</span>
        </span>
        <span className="grid grid-cols-1 border-t border-dashed border-border sm:grid-cols-4">
          <AgentFact label="Model" value={v && (v.model || "none picked")} rail={!v || v.model ? "" : "stop"} />
          <AgentFact label="Search" value={v?.search_text} rail={!v || v.searches ? "" : "warn"} />
          <AgentFact label="Tools" value={v?.tools_text} />
          <AgentFact label="Falls back to" value={v?.fallback_text} rail={v?.fallback === 0 ? "none" : ""} />
        </span>
        {st?.todo && (
          <span className={cn("mx-4 mb-3 block rounded-lg border border-dashed border-current px-3 py-2 font-mono text-xs", stateText[st.rail])}>
            {st.todo}
          </span>
        )}
      </summary>
      {v && agents && connect && (
        <div className="rounded-b-[12px] border-t-2 border-ink bg-muted px-4 py-4">
          {owner ? (
            <AgentSetup agent={v} others={agents.filter((o) => o.id !== v.id)} connect={connect} />
          ) : (
            <Meta>Reaches {v.base_url || "Anthropic's own API"}. Only an owner changes its setup.</Meta>
          )}
        </div>
      )}
    </details>
  );
}

function ScopeBoxes({ scopes, ticked, onChange }: { scopes: Scope[]; ticked: string[]; onChange: (ticked: string[]) => void }) {
  return scopes.map((s) => (
    <label key={s.name} className="flex items-start gap-2.5 text-sm">
      <Checkbox
        className="mt-1"
        checked={ticked.includes(s.name)}
        onCheckedChange={(on) => onChange(on ? [...ticked, s.name] : ticked.filter((n) => n !== s.name))}
      />
      <span>
        <span className="font-mono text-xs text-mark">{s.name}</span> <span className="text-muted-foreground">{s.what}</span>
      </span>
    </label>
  ));
}

function KeyCard({ k, scopes }: { k: Key; scopes: Scope[] }) {
  const { org, owner } = useSession();
  const refresh = useRefresh();
  const [ticked, setTicked] = useState(k.scopes);
  const change = $api.useMutation("patch", "/o/{org}/api/v1/keys/{id}", { onSuccess: refresh });
  const revoke = $api.useMutation("delete", "/o/{org}/api/v1/keys/{id}", { onSuccess: refresh });
  return (
    <details className={cardClass} data-state={k.last_used_at ? "ok" : ""}>
      <summary className="cursor-pointer list-none [&::-webkit-details-marker]:hidden">
        <span className="flex flex-wrap items-start justify-between gap-3 px-4 pt-4 pb-3">
          <span className="flex min-w-0 flex-col gap-0.5">
            <span className="flex flex-wrap items-center gap-2 font-heading text-[17px] font-bold">
              {k.name}
              <Tag quiet>Connects with a key</Tag>
            </span>
            <span className={cn("font-mono text-xs", k.last_used_at ? "text-[var(--ok)]" : "text-muted-foreground")}>
              {k.last_used_at ? "used " + stamp(k.last_used_at) : "never used"} · key {k.prefix}… · made by {k.created_by}
            </span>
          </span>
          {owner && <span className="rounded-lg border-[1.5px] border-ink bg-card px-2.5 py-1 text-xs font-semibold">Change scopes</span>}
        </span>
        <span className="flex flex-wrap items-center gap-1.5 border-t border-dashed border-border px-4 py-2.5">
          <Eyebrow>Can</Eyebrow>
          {k.scopes.map((s) => (
            <span key={s} className="rounded bg-secondary px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground">
              {s}
            </span>
          ))}
        </span>
      </summary>
      {owner && (
        <div className="flex flex-col gap-3 rounded-b-[12px] border-t-2 border-ink bg-muted px-4 py-4">
          <form
            className="flex flex-col gap-1.5"
            onSubmit={(e) => {
              e.preventDefault();
              change.mutate({ params: { path: { org, id: k.id } }, body: { scopes: ticked } });
            }}
          >
            <ScopeBoxes scopes={scopes} ticked={ticked} onChange={setTicked} />
            <div className="flex flex-wrap items-center gap-3">
              <ActionButton size="sm" type="submit" disabled={change.isPending}>
                Save scopes
              </ActionButton>
              <ActionButton
                quiet
                size="sm"
                type="button"
                onClick={() => window.confirm(`Revoke ${k.name}? Agents using it stop working.`) && revoke.mutate({ params: { path: { org, id: k.id } } })}
              >
                Revoke
              </ActionButton>
              {change.data && !change.error && (
                <p className="text-sm text-[var(--ok)]" role="status">
                  {change.data.message}
                </p>
              )}
              <Failed errors={[change.error, revoke.error]} />
            </div>
          </form>
        </div>
      )}
    </details>
  );
}

function Code({ children }: { children: string }) {
  return (
    <pre className="overflow-x-auto whitespace-pre-wrap break-all rounded-lg night border-2 border-ink bg-background px-3.5 py-3 font-mono text-[13px] leading-relaxed text-foreground">
      {children}
    </pre>
  );
}

// AddAgent offers both ways in: an agent Prospector runs, or one of your own with a key.
// Its two ways in are drawn while the roster loads; what each holds waits for it.
function AddAgent({ none, agents, connect }: { none: boolean; agents?: Agent[]; connect?: Connect }) {
  const { owner } = useSession();
  const base = connect?.base_url;
  const rest = [
    `GET  ${base}api/v1/search?q=…`,
    `GET  ${base}api/v1/companies/{id or name}`,
    `GET  ${base}api/v1/due`,
    `GET  ${base}api/v1/tasks`,
    `GET  ${base}api/v1/tasks/{id}`,
    `POST ${base}api/v1/companies   {name, domain, facts: [{field, value, source_url}], task}`,
    `POST ${base}api/v1/facts       {company, field, value, source_url}`,
    `POST ${base}api/v1/contacts    {company, name, title, email, source_url}`,
    `POST ${base}api/v1/drafts      {contact_id, kind, text}`,
    `POST ${base}api/v1/notes       {task, company, kind, text}`,
    `POST ${base}api/v1/requests/finish {request}`,
  ].join("\n");
  return (
    <div className="flex flex-col gap-3 rounded-[14px] border-2 border-dashed border-input p-4">
      <span className="font-heading text-[15px] font-bold">Add an agent</span>
      {owner && (
        <details className="rounded-xl border-2 border-ink bg-card p-3.5" open={none}>
          <summary className="cursor-pointer">
            <b>Prospector runs it</b>
            <span className="block text-sm text-muted-foreground">
              An OpenAI-compatible endpoint or Anthropic, a model and a key. It works the tasks you hand it, with search and the tools you allow.
            </span>
          </summary>
          {agents && connect && (
            <div className="mt-3">
              <AgentSetup others={agents} connect={connect} />
            </div>
          )}
        </details>
      )}
      <details className="rounded-xl border-2 border-ink bg-card p-3.5">
        <summary className="cursor-pointer">
          <b>Your own agent connects</b>
          <span className="block text-sm text-muted-foreground">
            Claude Code or any MCP client, with a key that has only the scopes it needs. No key can mark anything sent or reach another organisation, and only
            one with settings:write can change settings.
          </span>
        </summary>
        {connect && (
          <div className="mt-3 flex flex-col gap-3">
            <div>
              <Eyebrow>MCP endpoint</Eyebrow>
              <Code>{connect.mcp_url}</Code>
            </div>
            <div>
              <Eyebrow>Claude Code</Eyebrow>
              <Code>{`claude mcp add --transport http prospector ${connect.mcp_url} \\\n  --header "Authorization: Bearer $PROSPECTOR_KEY"`}</Code>
            </div>
            <div>
              <Eyebrow>REST</Eyebrow>
              <Code>{rest}</Code>
            </div>
            {owner ? <NewKey scopes={connect.scopes} /> : <Meta>An owner makes keys.</Meta>}
          </div>
        )}
      </details>
    </div>
  );
}

function NewKey({ scopes }: { scopes: Scope[] }) {
  const { org } = useSession();
  const refresh = useRefresh();
  const [name, setName] = useState("");
  const [ticked, setTicked] = useState(["read", "research:write"]);
  const create = $api.useMutation("post", "/o/{org}/api/v1/keys", {
    onSuccess: () => {
      setName("");
      refresh();
    },
  });
  return (
    <form
      className="flex flex-col gap-3 rounded-lg bg-muted p-4"
      onSubmit={(e) => {
        e.preventDefault();
        create.mutate({ params: { path: { org } }, body: { name, scopes: ticked } });
      }}
    >
      <label className="flex flex-col gap-1.5">
        <Eyebrow>New key name</Eyebrow>
        <Input name="name" required placeholder="research-bot" value={name} onChange={(e) => setName(e.target.value)} className={fieldClass} />
      </label>
      <fieldset className="flex flex-col gap-1.5">
        <legend className="mb-1">
          <Eyebrow>Scopes</Eyebrow>
        </legend>
        <ScopeBoxes scopes={scopes} ticked={ticked} onChange={setTicked} />
      </fieldset>
      <div>
        <ActionButton size="sm" type="submit" disabled={create.isPending}>
          Create key
        </ActionButton>
      </div>
      {create.data && !create.error && (
        <p className="select-all break-all font-mono text-sm text-[var(--ok)]" role="status">
          {create.data.message}
        </p>
      )}
      <Failed errors={[create.error]} />
    </form>
  );
}

function Row({ label, group, children }: { label: string; group?: boolean; children: ReactNode }) {
  const Box = group ? "div" : "label";
  return (
    <Box className="grid grid-cols-1 items-start gap-1.5 sm:grid-cols-[170px_1fr] sm:gap-4">
      <span className="pt-2.5">
        <Eyebrow>{label}</Eyebrow>
      </span>
      <span className="flex flex-col gap-1">{children}</span>
    </Box>
  );
}

// AgentSetup keeps how the last save went while the form is drawn again from what was saved.
function AgentSetup({ agent, others, connect }: { agent?: Agent; others: Agent[]; connect: Connect }) {
  const { org } = useSession();
  const refresh = useRefresh();
  const [round, setRound] = useState(0);
  const saved = () => {
    setRound((n) => n + 1);
    refresh();
  };
  const create = $api.useMutation("post", "/o/{org}/api/v1/agents", { onSuccess: saved });
  const update = $api.useMutation("put", "/o/{org}/api/v1/agents/{id}", { onSuccess: saved });
  const remove = $api.useMutation("delete", "/o/{org}/api/v1/agents/{id}", { onSuccess: refresh });
  const save = agent ? update : create;
  const submit = (body: components["schemas"]["AgentSettings"]) =>
    agent ? update.mutate({ params: { path: { org, id: agent.id } }, body }) : create.mutate({ params: { path: { org } }, body });

  return (
    <AgentForm key={`${round}-${agent?.updated_at ?? ""}`} agent={agent} others={others} connect={connect} onSave={submit} pending={save.isPending}>
      {agent && (
        <ActionButton
          quiet
          size="sm"
          type="button"
          onClick={() =>
            window.confirm(`Delete ${agent.name}? Its runs stop and its key is revoked; what it found stays.`) &&
            remove.mutate({ params: { path: { org, id: agent.id } } })
          }
        >
          Delete
        </ActionButton>
      )}
      {save.data && !save.error && (
        <p className="text-sm text-[var(--ok)]" role="status">
          {save.data.message}
        </p>
      )}
      <Failed errors={[save.error, remove.error]} />
    </AgentForm>
  );
}

function AgentForm({
  agent: v,
  others,
  connect,
  onSave,
  pending,
  children,
}: {
  agent?: Agent;
  others: Agent[];
  connect: Connect;
  onSave: (body: components["schemas"]["AgentSettings"]) => void;
  pending: boolean;
  children: ReactNode;
}) {
  const [thinkingOff, setThinkingOff] = useState(v?.thinking_off ?? false);
  const [tools, setTools] = useState(v?.allowed_tools ?? []);
  const models = v?.models ?? [];
  const offered = v?.tools ?? [];
  const modelsID = v ? `agent-models-${v.id}` : "agent-models-new";

  const submit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const f = new FormData(e.currentTarget);
    const text = (name: string) => String(f.get(name) ?? "");
    onSave({
      name: text("name"),
      kind: text("kind"),
      base_url: text("base_url"),
      api_key: text("api_key"),
      model: text("model"),
      thinking_off: thinkingOff,
      search_url: text("search_url"),
      tools_url: text("tools_url"),
      fallback: Number(f.get("fallback") ?? 0),
      fallback_minutes: Number(f.get("fallback_minutes") ?? 0),
      tools: offered.length > 0 ? tools : undefined,
    });
  };

  return (
    <form className="flex flex-col gap-3" onSubmit={submit}>
      <Row label="Name">
        <Input name="name" defaultValue={v?.name} placeholder="Claude, ChatGPT, the gateway…" autoComplete="off" className={fieldClass} />
        <Meta>What it is called on the task page and in the record.</Meta>
      </Row>
      <Row label="Kind">
        <select name="kind" defaultValue={v?.kind || "openai"} className={fieldClass}>
          {connect.kinds.map((k) => (
            <option key={k.kind} value={k.kind}>
              {k.label}
            </option>
          ))}
        </select>
        <Meta>
          Anthropic reaches Claude through Anthropic's own API with its official SDK; OpenAI-compatible is ChatGPT, OpenRouter, a gateway of your own, or a
          model you run yourself behind llama.cpp or vLLM.
        </Meta>
      </Row>
      <Row label="Endpoint">
        <Input name="base_url" defaultValue={v?.base_url} placeholder="https://openrouter.ai/api/v1" autoComplete="off" className={fieldClass} />
        <Meta>
          For OpenAI-compatible, the address its /models and /chat/completions are under, such as https://api.openai.com/v1. For Anthropic, leave it empty for
          Anthropic's own.
        </Meta>
      </Row>
      <Row label="API key">
        <Input
          name="api_key"
          type="password"
          autoComplete="new-password"
          placeholder={v?.has_key ? "saved: leave empty to keep it" : ""}
          className={fieldClass}
        />
        <Meta>Never shown again. Changing the endpoint needs it entered again.</Meta>
      </Row>
      <Row label="Model">
        <Input name="model" defaultValue={v?.model} list={modelsID} autoComplete="off" placeholder="one the endpoint lists" className={fieldClass} />
        <datalist id={modelsID}>
          {models.map((m) => (
            // oxlint-disable-next-line jsx-a11y/control-has-associated-label -- a suggestion for the input, which has the label
            <option key={m} value={m} />
          ))}
        </datalist>
        <Meta>
          {models.length > 0
            ? `The endpoint lists ${plural(models.length, "model")}; a router model that picks one per request works too.`
            : "Saving fetches the endpoint's list to choose from."}
        </Meta>
      </Row>
      <Row label="Thinking">
        <span className="flex items-start gap-2.5 pt-2 text-sm">
          <Checkbox className="mt-0.5" checked={thinkingOff} onCheckedChange={(on) => setThinkingOff(on === true)} />
          <span>Off: answer without thinking first</span>
        </span>
        <Meta>For OpenAI-compatible only. A thinking model you run yourself, such as Qwen, can be too slow to think before every tool call.</Meta>
      </Row>
      <Row label="Search">
        <Input name="search_url" defaultValue={v?.search_url} placeholder="https://searx.example.com" autoComplete="off" className={fieldClass} />
        <Meta>
          A SearXNG instance you run: free, open-source search, with json among its search.formats. The agent searches through it, and reads pages itself, with
          no paid search service.
        </Meta>
      </Row>
      <Row label="Tools">
        <Input name="tools_url" defaultValue={v?.tools_url} placeholder="https://mcp.example.com/mcp" autoComplete="off" className={fieldClass} />
        <Meta>An MCP server, such as a gateway's federated /mcp. It's sent the endpoint's key only when it's on the same address.</Meta>
      </Row>
      {others.length > 0 && (
        <Row label="Fallback" group>
          <div className="flex flex-wrap items-center gap-2">
            <select name="fallback" aria-label="Fallback" defaultValue={String(v?.fallback ?? 0)} className={fieldClass}>
              <option value="0">None: wait however long it takes</option>
              {others.map((o) => (
                <option key={o.id} value={String(o.id)}>
                  {o.name}
                </option>
              ))}
            </select>
            <span className="text-sm text-muted-foreground">after</span>
            <Input
              type="number"
              name="fallback_minutes"
              aria-label="Minutes"
              min={1}
              defaultValue={v?.fallback_minutes || 15}
              className={cn(fieldClass, "w-24")}
            />
            <span className="text-sm text-muted-foreground">minutes</span>
          </div>
          <Meta>
            When its endpoint stays limited or away this long, the agent hands its task to the one chosen here, which carries on from the task's notes.
          </Meta>
        </Row>
      )}
      {offered.length > 0 && (
        <Row label="Allowed tools" group>
          <div className="flex flex-col gap-1.5 pt-1.5">
            {offered.map((t) => (
              <label key={t.name} className="flex items-start gap-2.5 text-sm">
                <Checkbox
                  className="mt-0.5"
                  checked={tools.includes(t.name)}
                  onCheckedChange={(on) => setTools(on ? [...tools, t.name] : tools.filter((n) => n !== t.name))}
                />
                <span>
                  <span className="font-mono text-xs">{t.name}</span> <span className="text-muted-foreground">{t.description}</span>
                </span>
              </label>
            ))}
          </div>
          <Meta>
            The agent uses only the tools ticked here. LinkedIn's terms forbid automated access, logged-in browsers included: prefer search and fetch tools to
            browser and captcha ones.
          </Meta>
        </Row>
      )}
      <div className="flex flex-wrap items-center gap-3">
        <ActionButton size="sm" type="submit" disabled={pending}>
          {v ? "Save and check" : "Add and check"}
        </ActionButton>
        {children}
      </div>
    </form>
  );
}

function Scores() {
  const { org } = useSession();
  const scores = $api.useQuery("get", "/o/{org}/api/v1/agent-scores", { params: { path: { org }, query: { limit: 200 } } });
  return (
    <Section title="How each agent is doing" hint="What people did with each agent's suggestions. Use one key per outside agent to compare them.">
      <Loading busy={!scores.data && !scores.error}>
        {scores.data?.items.length === 0 && <p className="text-sm text-muted-foreground">Nothing suggested yet.</p>}
        {!scores.data && !scores.error && (
          <Placeholders>
            {[0, 1].map((i) => (
              <ScoreRow key={`loading-${i}`} />
            ))}
          </Placeholders>
        )}
        {scores.data?.items.map((sc) => (
          <ScoreRow key={sc.agent} sc={sc} />
        ))}
      </Loading>
      <Failed errors={[scores.error]} />
    </Section>
  );
}

function ScoreRow({ sc }: { sc?: components["schemas"]["AgentScore"] }) {
  return (
    <div className="border-b border-dashed border-border py-2 text-sm last:border-0">
      <b className="font-mono text-[13px] text-mark">
        <Text value={sc?.agent} w="12ch" />
      </b>
      <span className="block text-muted-foreground">
        <Text value={sc?.text} w="30ch" />
      </span>
    </div>
  );
}

function Work() {
  const { org } = useSession();
  const [cursor, setCursor] = useState<string | undefined>();
  const work = $api.useQuery("get", "/o/{org}/api/v1/agent-work", { params: { path: { org }, query: { limit: runsShown, cursor } } });
  const older = work.data?.next_cursor;
  return (
    <Section title="Recent agent work" hint="Each stretch of an agent's changes, and the task it was for. An agent that is only reading doesn't show.">
      <Loading busy={!work.data && !work.error}>
        {work.data?.items.length === 0 && <p className="text-sm text-muted-foreground">None yet.</p>}
        {!work.data && !work.error && (
          <Placeholders>
            {[0, 1, 2].map((i) => (
              <WorkRow key={`loading-${i}`} />
            ))}
          </Placeholders>
        )}
        {work.data?.items.map((r, i) => (
          <WorkRow key={`${i}-${r.actor}-${r.start}`} r={r} />
        ))}
      </Loading>
      <Failed errors={[work.error]} />
      {(cursor || older) && (
        <nav className="mt-3 flex flex-wrap items-center justify-between gap-3" aria-label="Runs">
          <span>
            {cursor && (
              <ActionButton quiet size="sm" onClick={() => setCursor(undefined)}>
                ← Latest
              </ActionButton>
            )}
          </span>
          <span>
            {older && (
              <ActionButton quiet size="sm" onClick={() => setCursor(older)}>
                Older →
              </ActionButton>
            )}
          </span>
        </nav>
      )}
    </Section>
  );
}

function WorkRow({ r }: { r?: components["schemas"]["AgentWork"] }) {
  const { org } = useSession();
  return (
    <div className="border-b border-dashed border-border py-2 text-sm last:border-0">
      <b className="font-mono text-[13px] text-mark">
        <Text value={r && actor(r.actor)} w="12ch" />
      </b>
      {r?.tasks.map((t) => (
        <span key={t.id}>
          {" "}
          <Link to="/o/$org/tasks/$id" params={{ org, id: t.id }} className="font-mono text-xs text-mark underline">
            {t.title}
          </Link>
        </span>
      ))}
      <span className="block text-muted-foreground">
        <Text value={r?.text} w="30ch" />
      </span>
    </div>
  );
}

function Activity() {
  const { org } = useSession();
  // The cursors of the pages after the first that led here, so Previous goes back one.
  const [trail, setTrail] = useState<string[]>([]);
  const activity = $api.useQuery("get", "/o/{org}/api/v1/agent-activity", {
    params: { path: { org }, query: { limit: activityShown, cursor: trail[trail.length - 1] } },
  });
  const n = trail.length + 1;
  const pages = Math.max(1, Math.ceil((activity.data?.total ?? 0) / activityShown));
  const next = activity.data?.next_cursor;
  return (
    <Section title="Recent agent activity">
      <Loading busy={!activity.data && !activity.error}>
        {activity.data?.items.length === 0 && <p className="text-sm text-muted-foreground">None yet.</p>}
        {!activity.data && !activity.error && (
          <Placeholders>
            {Array.from({ length: placeholders }, (_, i) => (
              <ActivityRow key={`loading-${i}`} />
            ))}
          </Placeholders>
        )}
        {activity.data?.items.map((e, i) => (
          <ActivityRow key={`${e.at}-${i}`} e={e} />
        ))}
      </Loading>
      <Failed errors={[activity.error]} />
      {pages > 1 && (
        <nav className="mt-3 flex flex-wrap items-center justify-between gap-3" aria-label="Pages">
          <span>
            {n > 1 && (
              <ActionButton quiet size="sm" onClick={() => setTrail(trail.slice(0, -1))}>
                ← Previous
              </ActionButton>
            )}
          </span>
          <Meta>
            Page {n} of {pages}
          </Meta>
          <span>
            {next && (
              <ActionButton quiet size="sm" onClick={() => setTrail([...trail, next])}>
                Next →
              </ActionButton>
            )}
          </span>
        </nav>
      )}
    </Section>
  );
}

function ActivityRow({ e }: { e?: components["schemas"]["AgentEvent"] }) {
  return (
    <div className="grid grid-cols-[84px_110px_1fr] gap-2 border-b border-dashed border-border py-1.5 text-[13px]">
      <span className="font-mono text-xs text-muted-foreground">
        <Text value={e && stamp(e.at)} w="9ch" />
      </span>
      <span className="truncate font-mono text-xs text-mark">
        <Text value={e?.actor} w="10ch" />
      </span>
      <span>
        <Text value={e?.text} w="24ch" />
      </span>
    </div>
  );
}

// The first day of next month, UTC, when a vendor's lookups start again.
function nextMonth(now = new Date()) {
  return intl.formatDate(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() + 1, 1), { day: "numeric", month: "short", timeZone: "UTC" });
}

function vendorState(e: Enricher): [Rail, string] {
  if (!e.has_key) return ["stop", "no key saved: it isn't asked"];
  if (e.monthly_limit === 0) return ["warn", "key saved · a limit of 0: it isn't asked"];
  if (e.used_this_month >= e.monthly_limit) return ["warn", `key saved · all ${e.monthly_limit} used in ${month()}: skipped until ${nextMonth()}`];
  return ["ok", `key saved · ${e.used_this_month} of ${plural(e.monthly_limit, "lookup")} used in ${month()} (UTC)`];
}

// DataVendors are the enrichment vendors asked, top first, for what research can't find.
function DataVendors() {
  const { org, owner } = useSession();
  const refresh = useRefresh();
  const list = $api.useQuery("get", "/o/{org}/api/v1/enrichers", { params: { path: { org } } });
  const save = $api.useMutation("put", "/o/{org}/api/v1/enrichers");
  const vendors = list.data?.items ?? [];
  const [moving, setMoving] = useState(false);

  // Every vendor is saved at its new place, so equal positions can't leave the order unchanged.
  const move = async (from: number, to: number) => {
    const order = [...vendors];
    order.splice(to, 0, ...order.splice(from, 1));
    setMoving(true);
    try {
      for (const [i, e] of order.entries()) {
        if (e.position !== i) {
          await save.mutateAsync({ params: { path: { org } }, body: { kind: e.kind, api_key: "", position: i, monthly_limit: e.monthly_limit } });
        }
      }
    } finally {
      setMoving(false);
      refresh();
    }
  };

  return (
    <Section
      title="Data vendors"
      hint="When research can't find someone's email, mobile or LinkedIn profile, you, and agents whose key has enrich:spend, can ask these, top first, on your own account with each. What they find waits in Research for a person to accept."
    >
      <div className="flex flex-col gap-4">
        {list.data && vendors.length === 0 && <p className="text-sm text-muted-foreground">No vendors yet.</p>}
        {!list.data && !list.error && (
          <Placeholders className="flex flex-col gap-4">
            {[0, 1].map((i) => (
              <VendorCard key={`loading-${i}`} n={i + 1} />
            ))}
          </Placeholders>
        )}
        {vendors.map((e, i) => (
          <VendorCard
            key={e.id}
            vendor={e}
            n={i + 1}
            up={owner && i > 0 && !moving ? () => move(i, i - 1) : undefined}
            down={owner && i < vendors.length - 1 && !moving ? () => move(i, i + 1) : undefined}
          />
        ))}
        {owner && list.data && <AddVendor vendors={vendors} />}
      </div>
      <Failed errors={[list.error, save.error]} />
      {!owner && <Meta>Only an owner adds, changes or removes a vendor.</Meta>}
      <p className="flex flex-wrap gap-x-4 gap-y-1 font-mono text-xs text-muted-foreground">
        <span>
          <Tag quiet>POST contacts/{"{id}"}/enrich</Tag> asks
        </span>
        <span>
          <Tag quiet>enrich_contact</Tag> for agents
        </span>
      </p>
    </Section>
  );
}

// VendorCard is one vendor, or its outline while the list loads.
function VendorCard({ vendor: e, n, up, down }: { vendor?: Enricher; n: number; up?: () => void; down?: () => void }) {
  const { org, owner } = useSession();
  const refresh = useRefresh();
  const [rail, line] = e ? vendorState(e) : [undefined, undefined];
  const [key, setKey] = useState("");
  const [limit, setLimit] = useState(String(e?.monthly_limit ?? ""));
  const save = $api.useMutation("put", "/o/{org}/api/v1/enrichers", {
    onSuccess: () => {
      setKey("");
      refresh();
    },
  });
  const remove = $api.useMutation("delete", "/o/{org}/api/v1/enrichers/{id}", { onSuccess: refresh });
  const used = !e ? 0 : e.monthly_limit > 0 ? Math.min(100, (e.used_this_month / e.monthly_limit) * 100) : 100;
  const submit = (ev: FormEvent) => {
    ev.preventDefault();
    if (!e) return;
    save.mutate({ params: { path: { org } }, body: { kind: e.kind, api_key: key, position: e.position, monthly_limit: Number(limit) } });
  };
  const button = (go: () => void) => (ev: React.MouseEvent) => {
    ev.preventDefault();
    go();
  };

  return (
    <details className={cardClass} data-state={rail}>
      {/* oxlint-disable-next-line jsx-a11y/control-has-associated-label -- labelled by the agent's name inside it */}
      <summary className="cursor-pointer list-none [&::-webkit-details-marker]:hidden">
        <span className="flex flex-wrap items-start justify-between gap-3 px-4 pt-4 pb-3">
          <span className="flex min-w-0 flex-col gap-0.5">
            <span className="flex flex-wrap items-center gap-2 font-heading text-[17px] font-bold">
              <span className="font-mono text-xs font-normal text-muted-foreground">{n}</span>
              <Text value={e && vendorName(e.kind)} w="9ch" />
              {e?.fields.map((f) => (
                <Tag key={f} quiet>
                  finds {detailNames[f] ?? f}
                </Tag>
              ))}
            </span>
            <span className={cn("font-mono text-xs", rail && stateText[rail])}>
              <Text value={line} w="30ch" />
            </span>
          </span>
          <span className="flex items-center gap-2">
            {e && up && (
              <ActionButton quiet size="xs" aria-label={`Ask ${vendorName(e.kind)} sooner`} onClick={button(up)}>
                ↑
              </ActionButton>
            )}
            {e && down && (
              <ActionButton quiet size="xs" aria-label={`Ask ${vendorName(e.kind)} later`} onClick={button(down)}>
                ↓
              </ActionButton>
            )}
            <span className="rounded-lg border-[1.5px] border-ink bg-card px-2.5 py-1 text-xs font-semibold">{owner ? "Change" : "Details"}</span>
          </span>
        </span>
        <span className="block px-4 pb-3">
          <span className="block overflow-hidden rounded-full bg-secondary" style={{ height: 6 }}>
            <span className={cn("block rounded-full", rail && fill[rail])} style={{ width: `${used}%`, height: "100%" }} />
          </span>
        </span>
      </summary>
      {e && (
        <div className="flex flex-col gap-3 rounded-b-[12px] border-t-2 border-ink bg-muted px-4 py-4">
          {owner ? (
            <form className="flex flex-col gap-3" onSubmit={submit}>
              <label className="flex flex-col gap-1.5">
                <Eyebrow>API key</Eyebrow>
                <Input
                  type="password"
                  autoComplete="off"
                  value={key}
                  onChange={(ev) => setKey(ev.target.value)}
                  placeholder={e.has_key ? "saved: leave empty to keep it" : "the key from your account with them"}
                  className={fieldClass}
                />
              </label>
              <label className="flex flex-col gap-1.5">
                <Eyebrow>Lookups a month, at most</Eyebrow>
                <Input
                  type="number"
                  min={0}
                  max={100000}
                  required
                  value={limit}
                  onChange={(ev) => setLimit(ev.target.value)}
                  className={cn(fieldClass, "w-24")}
                />
                <span className="text-xs text-muted-foreground">0 stops it being asked.</span>
              </label>
              <div className="flex flex-wrap items-center gap-2">
                <ActionButton size="sm" type="submit" disabled={save.isPending}>
                  Save
                </ActionButton>
                <ActionButton quiet size="sm" type="button" disabled={remove.isPending} onClick={() => remove.mutate({ params: { path: { org, id: e.id } } })}>
                  Remove {vendorName(e.kind)}
                </ActionButton>
                {save.isSuccess && <span className="text-sm text-[var(--ok)]">Saved.</span>}
              </div>
              <Failed errors={[save.error, remove.error]} />
            </form>
          ) : (
            <Meta>
              Asked at most {plural(e.monthly_limit, "time")} a month. Changed by {actor(e.updated_by)}, {stamp(e.updated_at)}.
            </Meta>
          )}
          <p className="text-xs text-muted-foreground">
            {vendorTold[e.kind]} {vendorCosts[e.kind]} You are bound by its terms for what it returns, and it may keep what it finds in your account with it.
          </p>
        </div>
      )}
    </details>
  );
}

function AddVendor({ vendors }: { vendors: Enricher[] }) {
  const { org } = useSession();
  const refresh = useRefresh();
  const free = vendorKinds.filter((k) => !vendors.some((e) => e.kind === k));
  const [kind, setKind] = useState("");
  const [key, setKey] = useState("");
  const [limit, setLimit] = useState("100");
  const add = $api.useMutation("put", "/o/{org}/api/v1/enrichers", {
    onSuccess: () => {
      setKind("");
      setKey("");
      refresh();
    },
  });
  const chosen = free.find((k) => k === kind);
  return (
    <form
      className="flex flex-col gap-3 rounded-[14px] border-2 border-dashed border-input p-4"
      onSubmit={(ev) => {
        ev.preventDefault();
        if (!chosen) return;
        const position = vendors.reduce((most, e) => Math.max(most, e.position + 1), 0);
        add.mutate({ params: { path: { org } }, body: { kind: chosen, api_key: key, position, monthly_limit: Number(limit) } });
      }}
    >
      <span className="font-heading text-[15px] font-semibold">Add a vendor</span>
      {free.length === 0 ? (
        <Meta>Every vendor Prospector can ask is added.</Meta>
      ) : (
        <div className="flex flex-wrap items-end gap-3">
          <label className="flex flex-col gap-1.5">
            <Eyebrow>Vendor</Eyebrow>
            <select required value={kind} onChange={(ev) => setKind(ev.target.value)} className={fieldClass}>
              <option value="">Choose…</option>
              {free.map((k) => (
                <option key={k} value={k}>
                  {vendorName(k)}
                </option>
              ))}
            </select>
          </label>
          <label className="flex min-w-[200px] flex-1 flex-col gap-1.5">
            <Eyebrow>API key</Eyebrow>
            <Input type="password" autoComplete="off" required value={key} onChange={(ev) => setKey(ev.target.value)} className={fieldClass} />
          </label>
          <label className="flex flex-col gap-1.5">
            <Eyebrow>Lookups a month</Eyebrow>
            <Input type="number" min={0} max={100000} required value={limit} onChange={(ev) => setLimit(ev.target.value)} className={cn(fieldClass, "w-24")} />
          </label>
          <ActionButton size="sm" type="submit" disabled={add.isPending}>
            Add
          </ActionButton>
        </div>
      )}
      <p className="text-xs text-muted-foreground">
        Keys are sealed under the server's key and never shown again. A new vendor goes to the bottom of the list.
      </p>
      <Failed errors={[add.error]} />
    </form>
  );
}
