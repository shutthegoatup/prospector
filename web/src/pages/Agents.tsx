import { Link } from "@tanstack/react-router";
import { type FormEvent, type ReactNode, useState } from "react";
import { defineMessage, defineMessages, FormattedMessage, type MessageDescriptor, useIntl } from "react-intl";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Eyebrow, Failed, Meta, PageHead, Section, useRefresh } from "@/components/app";
import { Bar, Loading, Placeholders, placeholders, Settled, Text } from "@/components/app/skeleton";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { actor, stamp } from "@/lib/format";
import { intl } from "@/i18n";
import { cn } from "@/lib/utils";
import { detailName, month, vendorCosts, vendorKinds, vendorName, vendorTold } from "@/lib/vendors";
import { useSession } from "@/session";
import { WebhooksSection } from "./Webhooks";

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

const m = defineMessages({
  title: { defaultMessage: "Agents & API", id: "zD1vHd", description: "Title of the page of agents, their API keys and data vendors" },
  toolsAgentsGet: { defaultMessage: "Tools agents get", id: "V2aK35", description: "Heading of the list of tools an agent connecting to the product can call" },
  toolsHint: {
    defaultMessage: "Writes need a source URL, and go through this organisation's never-contact list.",
    id: "LUBDah",
    description: "Under the heading of the tools agents get",
  },
  whoWorksHere: { defaultMessage: "Who works here", id: "JahRcu", description: "Heading of the list of every agent, built in or connecting with a key" },
  ready: { defaultMessage: "ready", id: "rTk8xe", description: "In the key to the agent cards' colours: an agent that is ready" },
  missingSomething: {
    defaultMessage: "runs, missing something",
    id: "XVpLSg",
    description: "In the key to the agent cards' colours: an agent that runs but lacks something",
  },
  cantRun: {
    defaultMessage: "can't run: no key, endpoint or model",
    id: "CYhB9h",
    description: "In the key to the agent cards' colours: an agent that can't run",
  },
  noAgents: { defaultMessage: "No agents yet.", id: "i9tD0N", description: "On the agents page when there are no agents" },
  setup: { defaultMessage: "Setup", id: "1Fq8UA", description: "On an agent's card, for an owner: opens its settings" },
  details: {
    defaultMessage: "Details",
    id: "LrAR8w",
    description: "On an agent's or data vendor's card, for a member who isn't an owner: opens more about it",
  },
  thinkingOff: { defaultMessage: "thinking off", id: "Sqp77K", description: "Tag on an agent's card: it answers without thinking first" },
  model: { defaultMessage: "Model", id: "M16a9r", description: "Label of an agent's model, on its card and in its settings" },
  nonePicked: { defaultMessage: "none picked", id: "tKLNtr", description: "On an agent's card in place of its model when none is chosen" },
  search: { defaultMessage: "Search", id: "uzd8gu", description: "Label of the search an agent uses, on its card and in its settings" },
  tools: { defaultMessage: "Tools", id: "hZL+hX", description: "Label of the tools an agent uses, on its card and in its settings" },
  fallsBackTo: {
    defaultMessage: "Falls back to",
    id: "IxCGjS",
    description: "Label on an agent's card of the agent that takes over its task when it can't go on",
  },
  anthropicOwn: {
    defaultMessage: "Anthropic's own API",
    id: "MnCw5g",
    description: "Where an agent's requests go when it has no endpoint of its own; Anthropic is a company's name",
  },
  connectsWithKey: {
    defaultMessage: "Connects with a key",
    id: "HCSvrN",
    description: "Tag on the card of the organisation's own agent, which connects with an API key",
  },
  changeScopes: { defaultMessage: "Change scopes", id: "OHmves", description: "On an API key's card, for an owner: opens what the key may do" },
  can: { defaultMessage: "Can", id: "zW089N", description: "On an API key's card, before the scopes it has" },
  saveScopes: { defaultMessage: "Save scopes", id: "FBJjUi", description: "Button saving what an API key may do" },
  revoke: { defaultMessage: "Revoke", id: "/njgd9", description: "Button revoking an API key" },
  addAnAgent: { defaultMessage: "Add an agent", id: "nhv3JJ", description: "Heading of the ways to add an agent" },
  runsItHint: {
    defaultMessage: "An OpenAI-compatible endpoint or Anthropic, a model and a key. It works the tasks you hand it, with search and the tools you allow.",
    id: "jizAtl",
    description: "Under the choice to add an agent the product runs itself. OpenAI and Anthropic are companies' names",
  },
  ownAgent: {
    defaultMessage: "Your own agent connects",
    id: "hGXKjJ",
    description: "The choice to add the organisation's own agent, which connects with a key",
  },
  mcpEndpoint: {
    defaultMessage: "MCP endpoint",
    id: "gHMa/n",
    description: "Label of the address an MCP client connects to; MCP is the Model Context Protocol",
  },
  ownerMakesKeys: {
    defaultMessage: "An owner makes keys.",
    id: "/44NV6",
    description: "In place of the form for a new API key, for a member who isn't an owner",
  },
  newKeyName: { defaultMessage: "New key name", id: "EcWDyr", description: "Label of the name of a new API key" },
  scopes: { defaultMessage: "Scopes", id: "MKASt/", description: "Label of what a new API key may do" },
  createKey: { defaultMessage: "Create key", id: "oR0KaM", description: "Button making a new API key" },
  name: { defaultMessage: "Name", id: "iz5wxg", description: "Label of an agent's name in its settings" },
  namePlaceholder: {
    defaultMessage: "Claude, ChatGPT, the gateway…",
    id: "o8wiUL",
    description: "Examples in the empty field for an agent's name; Claude and ChatGPT are products' names",
  },
  nameHint: { defaultMessage: "What it is called on the task page and in the record.", id: "SD8+Tu", description: "Under the field for an agent's name" },
  kind: { defaultMessage: "Kind", id: "YalWPe", description: "Label of the choice of what kind of endpoint an agent reaches" },
  kindHint: {
    defaultMessage:
      "Anthropic reaches Claude through Anthropic's own API with its official SDK; OpenAI-compatible is ChatGPT, OpenRouter, a gateway of your own, or a model you run yourself behind llama.cpp or vLLM.",
    id: "ERWuf3",
    description: "Under the choice of an agent's kind. Anthropic, Claude, OpenAI, ChatGPT, OpenRouter, llama.cpp and vLLM are names of companies and products",
  },
  endpoint: { defaultMessage: "Endpoint", id: "5iNVjr", description: "Label of the address an agent sends its requests to" },
  endpointHint: {
    defaultMessage:
      "For OpenAI-compatible, the address its /models and /chat/completions are under, such as https://api.openai.com/v1. For Anthropic, leave it empty for Anthropic's own.",
    id: "8nC0vG",
    description: "Under the field for an agent's endpoint. /models, /chat/completions and the address are kept as they are",
  },
  apiKey: { defaultMessage: "API key", id: "jnMAw2", description: "Label of the field for an API key, an agent's or a data vendor's" },
  keySaved: { defaultMessage: "saved: leave empty to keep it", id: "Eu88wV", description: "In the empty field for an API key when one is already saved" },
  apiKeyHint: {
    defaultMessage: "Never shown again. Changing the endpoint needs it entered again.",
    id: "wfUDAH",
    description: "Under the field for an agent's API key",
  },
  modelPlaceholder: { defaultMessage: "one the endpoint lists", id: "cS2ej5", description: "In the empty field for an agent's model" },
  modelsUnknown: {
    defaultMessage: "Saving fetches the endpoint's list to choose from.",
    id: "lrK2QX",
    description: "Under the field for an agent's model before its endpoint's models are known",
  },
  thinking: { defaultMessage: "Thinking", id: "8/sx8x", description: "Label of the setting for whether an agent thinks before answering" },
  thinkingOffBox: { defaultMessage: "Off: answer without thinking first", id: "cBzW8J", description: "Beside the box that turns an agent's thinking off" },
  thinkingHint: {
    defaultMessage: "For OpenAI-compatible only. A thinking model you run yourself, such as Qwen, can be too slow to think before every tool call.",
    id: "Lams6/",
    description: "Under the setting for an agent's thinking; Qwen is a model's name",
  },
  searchHint: {
    defaultMessage:
      "A SearXNG instance you run: free, open-source search, with json among its search.formats. The agent searches through it, and reads pages itself, with no paid search service.",
    id: "NqTzWu",
    description: "Under the field for an agent's search. SearXNG is a product's name; json and search.formats are its settings, kept as they are",
  },
  toolsHintField: {
    defaultMessage: "An MCP server, such as a gateway's federated /mcp. It's sent the endpoint's key only when it's on the same address.",
    id: "ATkIdV",
    description: "Under the field for the MCP server an agent gets its tools from",
  },
  fallback: { defaultMessage: "Fallback", id: "bv+kEI", description: "Label of the agent that takes over a task when this one can't go on" },
  noFallback: { defaultMessage: "None: wait however long it takes", id: "473+QS", description: "The choice of no agent to fall back to" },
  minutes: { defaultMessage: "Minutes", id: "u1umDV", description: "What a screen reader calls the field for how many minutes to wait before falling back" },
  fallbackHint: {
    defaultMessage:
      "When its endpoint stays limited or away this long, the agent hands its task to the one chosen here, which carries on from the task's notes.",
    id: "a/FGNc",
    description: "Under the setting for an agent's fallback",
  },
  allowedTools: { defaultMessage: "Allowed tools", id: "IoBZJU", description: "Label of the tools an agent may use" },
  saveAndCheck: { defaultMessage: "Save and check", id: "FlkLbK", description: "Button saving an agent's settings and checking it can run" },
  addAndCheck: { defaultMessage: "Add and check", id: "6H+N3d", description: "Button adding an agent and checking it can run" },
  delete: { defaultMessage: "Delete", id: "UI3KYo", description: "Button deleting an agent" },
  scoresTitle: {
    defaultMessage: "How each agent is doing",
    id: "6YZVi4",
    description: "Heading of each agent's record of what people did with its suggestions",
  },
  scoresHint: {
    defaultMessage: "What people did with each agent's suggestions. Use one key per outside agent to compare them.",
    id: "DsU/HG",
    description: "Under the heading of how each agent is doing",
  },
  nothingSuggested: { defaultMessage: "Nothing suggested yet.", id: "2pt7cw", description: "In place of agents' scores when no agent has suggested anything" },
  workTitle: { defaultMessage: "Recent agent work", id: "8sYn7A", description: "Heading of the stretches of changes agents made" },
  workHint: {
    defaultMessage: "Each stretch of an agent's changes, and the task it was for. An agent that is only reading doesn't show.",
    id: "O3gk5w",
    description: "Under the heading of recent agent work",
  },
  noneYet: { defaultMessage: "None yet.", id: "JBdCBL", description: "In place of agents' recent work or activity when there is none" },
  runs: { defaultMessage: "Runs", id: "ztNs+d", description: "What a screen reader calls the buttons moving through agents' recent work" },
  latest: { defaultMessage: "← Latest", id: "JJUxhB", description: "Button back to agents' latest work" },
  older: { defaultMessage: "Older →", id: "/IPV1J", description: "Button to agents' older work" },
  activityTitle: { defaultMessage: "Recent agent activity", id: "KWZmkc", description: "Heading of the log of what agents did" },
  pages: { defaultMessage: "Pages", id: "dfoqri", description: "What a screen reader calls the buttons moving through pages of agents' activity" },
  previous: { defaultMessage: "← Previous", id: "gd4kEc", description: "Button to the previous page of agents' activity" },
  next: { defaultMessage: "Next →", id: "6rNwfw", description: "Button to the next page of agents' activity" },
  noKey: { defaultMessage: "no key saved: it isn't asked", id: "f4Q2nK", description: "On a data vendor's card when it has no API key" },
  limitZero: {
    defaultMessage: "key saved · a limit of 0: it isn't asked",
    id: "1ddbfD",
    description: "On a data vendor's card when it may be asked no times a month",
  },
  vendorsTitle: { defaultMessage: "Data vendors", id: "GYHhwE", description: "Heading of the enrichment vendors asked for a person's contact details" },
  noVendors: { defaultMessage: "No vendors yet.", id: "wLHbPM", description: "In place of data vendors when there are none" },
  ownerVendors: {
    defaultMessage: "Only an owner adds, changes or removes a vendor.",
    id: "H4Cm3s",
    description: "Under the data vendors, for a member who isn't an owner",
  },
  change: { defaultMessage: "Change", id: "4u7AaJ", description: "On a data vendor's card, for an owner: opens its settings" },
  keyPlaceholder: { defaultMessage: "the key from your account with them", id: "pilWFF", description: "In the empty field for a data vendor's API key" },
  lookupsAtMost: { defaultMessage: "Lookups a month, at most", id: "rpIoVR", description: "Label of the most times a data vendor may be asked a month" },
  zeroStops: { defaultMessage: "0 stops it being asked.", id: "LfTUPr", description: "Under the most times a data vendor may be asked a month" },
  save: { defaultMessage: "Save", id: "x0NHB+", description: "Button saving a data vendor's settings" },
  saved: { defaultMessage: "Saved.", id: "MIObum", description: "Beside the save button once a data vendor's settings are saved" },
  addVendor: { defaultMessage: "Add a vendor", id: "ItO9Ju", description: "Heading of the form adding a data vendor" },
  vendor: { defaultMessage: "Vendor", id: "m7o3OQ", description: "Label of the choice of data vendor to add" },
  choose: { defaultMessage: "Choose…", id: "DcBTm9", description: "The empty choice of data vendor to add" },
  lookups: { defaultMessage: "Lookups a month", id: "LSRtIo", description: "Label of how many times a new data vendor may be asked a month" },
  add: { defaultMessage: "Add", id: "QdkVc6", description: "Button adding a data vendor" },
  keysSealed: {
    defaultMessage: "Keys are sealed under the server's key and never shown again. A new vendor goes to the bottom of the list.",
    id: "UAg9Qt",
    description: "Under the form adding a data vendor",
  },
});

const v = {
  readyCount: defineMessage<{ n: number }>({
    defaultMessage: "{n} ready",
    id: "dMA4Dr",
    description: "Count of the product's own agents that are ready to run",
  }),
  missingCount: defineMessage<{ n: number }>({
    defaultMessage: "{n} missing something",
    id: "wcdSOw",
    description: "Count of the product's own agents that run but lack something",
  }),
  cantRunCount: defineMessage<{ n: number }>({
    defaultMessage: "{n} can't run",
    id: "Zj/wNE",
    description: "Count of the product's own agents that can't run",
  }),
  whoWorksHint: defineMessage<{ prospector: string }>({
    defaultMessage: "Every agent in one list: those {prospector} runs and those that connect with a key. The colour behind each card is its state.",
    id: "FD+/u9",
    description: "Under the heading of every agent. {prospector} is the product's name, Prospector",
  }),
  noSecret: defineMessage<{ variable: string }>({
    defaultMessage: "This server has no secret key, so it can't keep an API key: whoever runs it needs to set {variable} first.",
    id: "46dQ9t",
    description: "To an owner when the server can't keep API keys. {variable} is the name of a setting, PROSPECTOR_SECRET_KEYS",
  }),
  ownAgents: defineMessage<{ prospector: string }>({
    defaultMessage: "{prospector}'s agents",
    id: "F/wbYX",
    description: "What a screen reader calls the counts of the product's own agents by state. {prospector} is the product's name, Prospector",
  }),
  runsIt: defineMessage<{ prospector: string }>({
    defaultMessage: "{prospector} runs it",
    id: "5czU92",
    description: "Tag on an agent's card, and the choice to add one: an agent the product runs itself. {prospector} is the product's name, Prospector",
  }),
  reaches: defineMessage<{ where: string }>({
    defaultMessage: "Reaches {where}. Only an owner changes its setup.",
    id: "c2Y0by",
    description: "On an agent's card, for a member who isn't an owner. {where} is the address it sends requests to, or Anthropic's own API",
  }),
  usedKey: defineMessage<{ when: string; prefix: string; who: string }>({
    defaultMessage: "used {when} · key {prefix}… · made by {who}",
    id: "8wALzm",
    description: "On an API key's card. {when} is when it was last used, {prefix} the start of the key, {who} the member who made it",
  }),
  unusedKey: defineMessage<{ prefix: string; who: string }>({
    defaultMessage: "never used · key {prefix}… · made by {who}",
    id: "YPPYUG",
    description: "On an API key's card. {prefix} is the start of the key, {who} the member who made it",
  }),
  revokeKey: defineMessage<{ name: string }>({
    defaultMessage: "Revoke {name}? Agents using it stop working.",
    id: "xfLdqu",
    description: "Asked before revoking an API key. {name} is the key's name",
  }),
  ownAgentHint: defineMessage<{ claudeCode: string; scope: string }>({
    defaultMessage:
      "{claudeCode} or any MCP client, with a key that has only the scopes it needs. No key can mark anything sent or reach another organisation, and only one with {scope} can change settings.",
    id: "QzOhFx",
    description:
      "Under the choice to add the organisation's own agent. {claudeCode} is a product's name, Claude Code; {scope} is the name of a key's scope, settings:write",
  }),
  modelsListed: defineMessage<{ n: number }>({
    defaultMessage: "The endpoint lists {n, plural, one {# model} other {# models}}; a router model that picks one per request works too.",
    id: "Uw3mGa",
    description: "Under the field for an agent's model, once its endpoint's models are known",
  }),
  allowedToolsHint: defineMessage<{ linkedin: string }>({
    defaultMessage:
      "The agent uses only the tools ticked here. {linkedin}'s terms forbid automated access, logged-in browsers included: prefer search and fetch tools to browser and captcha ones.",
    id: "Xxl9hq",
    description: "Under the tools an agent may use. {linkedin} is LinkedIn",
  }),
  deleteAgent: defineMessage<{ name: string }>({
    defaultMessage: "Delete {name}? Its runs stop and its key is revoked; what it found stays.",
    id: "/bDFuM",
    description: "Asked before deleting an agent. {name} is the agent's name",
  }),
  page: defineMessage<{ n: number; pages: number }>({
    defaultMessage: "Page {n} of {pages}",
    id: "PvjeZn",
    description: "Between the buttons moving through pages of agents' activity",
  }),
  allUsed: defineMessage<{ limit: number; month: string; next: string }>({
    defaultMessage: "key saved · all {limit} used in {month}: skipped until {next}",
    id: "MQw84a",
    description:
      "On a data vendor's card when this month's lookups are used up. {limit} is how many it may make, {month} the month's name, {next} the first day of next month, such as 1 Dec",
  }),
  used: defineMessage<{ used: number; limit: number; month: string }>({
    defaultMessage: "key saved · {used} of {limit, plural, one {# lookup} other {# lookups}} used in {month} (UTC)",
    id: "6rim9G",
    description: "On a data vendor's card: how many of this month's lookups are used. {month} is the month's name",
  }),
  vendorsHint: defineMessage<{ scope: string }>({
    defaultMessage:
      "When research can't find someone's email, mobile or LinkedIn profile, you, and agents whose key has {scope}, can ask these, top first, on your own account with each. What they find waits in Research for a person to accept.",
    id: "DJYPVl",
    description: "Under the heading of data vendors. {scope} is the name of a key's scope, enrich:spend; Research is the name of a page",
  }),
  finds: defineMessage<{ detail: string }>({
    defaultMessage: "finds {detail}",
    id: "zdJWTU",
    description: "Tag on a data vendor's card. {detail} is what it finds: email, mobile or LinkedIn profile",
  }),
  sooner: defineMessage<{ vendor: string }>({
    defaultMessage: "Ask {vendor} sooner",
    id: "8VnVDl",
    description: "What a screen reader calls the button moving a data vendor up the list. {vendor} is its name",
  }),
  later: defineMessage<{ vendor: string }>({
    defaultMessage: "Ask {vendor} later",
    id: "SSIErc",
    description: "What a screen reader calls the button moving a data vendor down the list. {vendor} is its name",
  }),
  remove: defineMessage<{ vendor: string }>({
    defaultMessage: "Remove {vendor}",
    id: "UTdde7",
    description: "Button removing a data vendor. {vendor} is its name",
  }),
  askedAtMost: defineMessage<{ n: number; who: string; when: string }>({
    defaultMessage: "Asked at most {n, plural, one {# time} other {# times}} a month. Changed by {who}, {when}.",
    id: "bFkWVL",
    description: "On a data vendor's card, for a member who isn't an owner. {who} is who last changed it, {when} the date and time",
  }),
  bound: defineMessage<{ told: string; costs: string }>({
    defaultMessage: "{told} {costs} You are bound by its terms for what it returns, and it may keep what it finds in your account with it.",
    id: "kBWQrG",
    description: "Under a data vendor's settings. {told} is a sentence on what it is sent about a person, {costs} one on what it charges",
  }),
  everyVendor: defineMessage<{ prospector: string }>({
    defaultMessage: "Every vendor {prospector} can ask is added.",
    id: "nvR3+z",
    description: "In place of the form adding a data vendor when all are added. {prospector} is the product's name, Prospector",
  }),
};

// Names, addresses and identifiers are the same in every language.
const prospector = "Prospector";
const linkedin = "LinkedIn";
const claudeCode = "Claude Code";
const rest = "REST";
const secretKeys = "PROSPECTOR_SECRET_KEYS";
const settingsWrite = "settings:write";
const enrichSpend = "enrich:spend";
const enrichCall = "POST contacts/{id}/enrich";
const enrichTool = "enrich_contact";
const exampleKeyName = "research-bot";
const exampleEndpoint = "https://openrouter.ai/api/v1";
const exampleSearch = "https://searx.example.com";
const exampleTools = "https://mcp.example.com/mcp";
const mcpAdd = (url: string) => `claude mcp add --transport http prospector ${url} \\\n  --header "Authorization: Bearer $PROSPECTOR_KEY"`;
const space = " ";
const upArrow = "↑";
const downArrow = "↓";

const muted = (c: ReactNode[]) => <span className="text-sm text-muted-foreground">{c}</span>;

export function AgentsPage() {
  const { org, owner } = useSession();
  const agents = $api.useQuery("get", "/o/{org}/api/v1/agents", { params: { path: { org }, query: { limit: 200 } } });
  const keys = $api.useQuery("get", "/o/{org}/api/v1/keys", { params: { path: { org }, query: { limit: 200 } } });
  const connect = $api.useQuery("get", "/o/{org}/api/v1/connect", { params: { path: { org } } });
  const settings = $api.useQuery("get", "/o/{org}/api/v1/settings", { params: { path: { org } } });
  const say = useIntl().formatMessage;

  return (
    <>
      <PageHead title={say(m.title)} />
      <p className="mb-5 max-w-[72ch] text-sm text-muted-foreground">
        <FormattedMessage
          defaultMessage="Agents research, add and draft for {org}. Some {prospector} runs itself; others are your own and connect with a key, which works for this organisation only. What any of them adds waits in Research for a person; agents can draft, but only people send."
          id="Pgo+4m"
          description="Top of the agents page. {org} is the organisation's name, {prospector} the product's name, Prospector; Research is the name of a page"
          values={{
            prospector,
            org: (
              <b className="text-foreground">
                <Text value={settings.data?.name ?? (settings.error ? "" : undefined)} w="8ch" />
              </b>
            ),
          }}
        />
      </p>
      {/* Its sections stack, and each list's length decides where the next one starts. */}
      <Settled className="flex flex-wrap items-start gap-5">
        <div className="flex min-w-0 flex-[3_1_560px] flex-col gap-5">
          <Roster agents={agents.data?.items} keys={keys.data?.items} connect={connect.data} errors={[agents.error, keys.error, connect.error]} />
          <DataVendors />
          {owner && <WebhooksSection keys={keys.data?.items} />}
        </div>
        <div className="flex min-w-0 flex-[2_1_340px] flex-col gap-5">
          <Scores />
          <Work />
          <Section title={say(m.toolsAgentsGet)} hint={say(m.toolsHint)}>
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
  const say = useIntl().formatMessage;
  return (
    <Section title={say(m.whoWorksHere)} hint={say(v.whoWorksHint, { prospector })}>
      {loaded && !loaded.connect.can_keep_keys && owner && (
        <p className="text-sm text-[var(--stop)]" role="alert">
          {say(v.noSecret, { variable: secretKeys })}
        </p>
      )}
      {loading && (
        <Placeholders className="flex flex-wrap gap-2 font-mono text-xs">
          <TallyChip />
        </Placeholders>
      )}
      {loaded && loaded.agents.length > 0 && (
        <div className="flex flex-wrap gap-2 font-mono text-xs" aria-label={say(v.ownAgents, { prospector })}>
          <TallyChip rail="ok" n={tally("ok")} text={v.readyCount} />
          <TallyChip rail="warn" n={tally("warn")} text={v.missingCount} />
          <TallyChip rail="stop" n={tally("stop")} text={v.cantRunCount} />
        </div>
      )}
      <div className="flex flex-col gap-4">
        {none && <p className="text-sm text-muted-foreground">{say(m.noAgents)}</p>}
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
          {say(m.ready)}
        </span>
        <span>
          <i className="mr-1.5 inline-block h-2.5 w-3.5 rounded-sm border border-ink bg-[var(--warn-fill)] align-[-1px]" />
          {say(m.missingSomething)}
        </span>
        <span>
          <i className="mr-1.5 inline-block h-2.5 w-3.5 rounded-sm border border-ink bg-[var(--stop-fill)] align-[-1px]" />
          {say(m.cantRun)}
        </span>
      </p>
    </Section>
  );
}

// TallyChip without a count is its outline while the roster loads.
function TallyChip({ rail, n, text }: { rail?: Rail; n?: number; text?: MessageDescriptor }) {
  const say = useIntl().formatMessage;
  if (n === 0) return null;
  return (
    <span className="rounded-full border-[1.5px] border-ink bg-card px-2.5 py-0.5">
      <i className={cn("mr-1.5 inline-block size-2 rounded-full", rail ? fill[rail] : "motion-safe:animate-pulse bg-accent")} />
      {n === undefined || !text ? <Bar w="7ch" /> : say(text, { n })}
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
function AgentCard({ agent: a, agents, connect }: { agent?: Agent; agents?: Agent[]; connect?: Connect }) {
  const { owner } = useSession();
  const say = useIntl().formatMessage;
  const st = a?.state;
  return (
    <details className={cardClass} data-state={st?.rail} id={a && `agent-${a.id}`}>
      <summary className="cursor-pointer list-none [&::-webkit-details-marker]:hidden">
        <span className="flex flex-wrap items-start justify-between gap-3 px-4 pt-4 pb-3">
          <span className="flex min-w-0 flex-col gap-0.5">
            <span className="flex flex-wrap items-center gap-2 font-heading text-[17px] font-bold">
              <Text value={a?.name} w="10ch" />
              <Tag>{say(v.runsIt, { prospector })}</Tag>
              {a?.thinking_off && <Tag quiet>{say(m.thinkingOff)}</Tag>}
            </span>
            <span className={cn("font-mono text-xs", st && stateText[st.rail])}>
              <Text value={st?.line} w="22ch" />
            </span>
          </span>
          <span className="rounded-lg border-[1.5px] border-ink bg-card px-2.5 py-1 text-xs font-semibold">{say(owner ? m.setup : m.details)}</span>
        </span>
        <span className="grid grid-cols-1 border-t border-dashed border-border sm:grid-cols-4">
          <AgentFact label={say(m.model)} value={a && (a.model || say(m.nonePicked))} rail={!a || a.model ? "" : "stop"} />
          <AgentFact label={say(m.search)} value={a?.search_text} rail={!a || a.searches ? "" : "warn"} />
          <AgentFact label={say(m.tools)} value={a?.tools_text} />
          <AgentFact label={say(m.fallsBackTo)} value={a?.fallback_text} rail={a?.fallback === 0 ? "none" : ""} />
        </span>
        {st?.todo && (
          <span className={cn("mx-4 mb-3 block rounded-lg border border-dashed border-current px-3 py-2 font-mono text-xs", stateText[st.rail])}>
            {st.todo}
          </span>
        )}
      </summary>
      {a && agents && connect && (
        <div className="rounded-b-[12px] border-t-2 border-ink bg-muted px-4 py-4">
          {owner ? (
            <AgentSetup agent={a} others={agents.filter((o) => o.id !== a.id)} connect={connect} />
          ) : (
            <Meta>{say(v.reaches, { where: a.base_url || say(m.anthropicOwn) })}</Meta>
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
  const say = useIntl().formatMessage;
  return (
    <details className={cardClass} data-state={k.last_used_at ? "ok" : ""}>
      <summary className="cursor-pointer list-none [&::-webkit-details-marker]:hidden">
        <span className="flex flex-wrap items-start justify-between gap-3 px-4 pt-4 pb-3">
          <span className="flex min-w-0 flex-col gap-0.5">
            <span className="flex flex-wrap items-center gap-2 font-heading text-[17px] font-bold">
              {k.name}
              <Tag quiet>{say(m.connectsWithKey)}</Tag>
            </span>
            <span className={cn("font-mono text-xs", k.last_used_at ? "text-[var(--ok)]" : "text-muted-foreground")}>
              {k.last_used_at
                ? say(v.usedKey, { when: stamp(k.last_used_at), prefix: k.prefix, who: k.created_by })
                : say(v.unusedKey, { prefix: k.prefix, who: k.created_by })}
            </span>
          </span>
          {owner && <span className="rounded-lg border-[1.5px] border-ink bg-card px-2.5 py-1 text-xs font-semibold">{say(m.changeScopes)}</span>}
        </span>
        <span className="flex flex-wrap items-center gap-1.5 border-t border-dashed border-border px-4 py-2.5">
          <Eyebrow>{say(m.can)}</Eyebrow>
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
                {say(m.saveScopes)}
              </ActionButton>
              <ActionButton
                quiet
                size="sm"
                type="button"
                onClick={() => window.confirm(say(v.revokeKey, { name: k.name })) && revoke.mutate({ params: { path: { org, id: k.id } } })}
              >
                {say(m.revoke)}
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
  const say = useIntl().formatMessage;
  const base = connect?.base_url;
  const calls = [
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
      <span className="font-heading text-[15px] font-bold">{say(m.addAnAgent)}</span>
      {owner && (
        <details className="rounded-xl border-2 border-ink bg-card p-3.5" open={none}>
          <summary className="cursor-pointer">
            <b>{say(v.runsIt, { prospector })}</b>
            <span className="block text-sm text-muted-foreground">{say(m.runsItHint)}</span>
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
          <b>{say(m.ownAgent)}</b>
          <span className="block text-sm text-muted-foreground">{say(v.ownAgentHint, { claudeCode, scope: settingsWrite })}</span>
        </summary>
        {connect && (
          <div className="mt-3 flex flex-col gap-3">
            <div>
              <Eyebrow>{say(m.mcpEndpoint)}</Eyebrow>
              <Code>{connect.mcp_url}</Code>
            </div>
            <div>
              <Eyebrow>{claudeCode}</Eyebrow>
              <Code>{mcpAdd(connect.mcp_url)}</Code>
            </div>
            <div>
              <Eyebrow>{rest}</Eyebrow>
              <Code>{calls}</Code>
            </div>
            {owner ? <NewKey scopes={connect.scopes} /> : <Meta>{say(m.ownerMakesKeys)}</Meta>}
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
  const say = useIntl().formatMessage;
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
        <Eyebrow>{say(m.newKeyName)}</Eyebrow>
        <Input name="name" required placeholder={exampleKeyName} value={name} onChange={(e) => setName(e.target.value)} className={fieldClass} />
      </label>
      <fieldset className="flex flex-col gap-1.5">
        <legend className="mb-1">
          <Eyebrow>{say(m.scopes)}</Eyebrow>
        </legend>
        <ScopeBoxes scopes={scopes} ticked={ticked} onChange={setTicked} />
      </fieldset>
      <div>
        <ActionButton size="sm" type="submit" disabled={create.isPending}>
          {say(m.createKey)}
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
  const say = useIntl().formatMessage;
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
          onClick={() => window.confirm(say(v.deleteAgent, { name: agent.name })) && remove.mutate({ params: { path: { org, id: agent.id } } })}
        >
          {say(m.delete)}
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
  agent: a,
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
  const [thinkingOff, setThinkingOff] = useState(a?.thinking_off ?? false);
  const [tools, setTools] = useState(a?.allowed_tools ?? []);
  const models = a?.models ?? [];
  const offered = a?.tools ?? [];
  const modelsID = a ? `agent-models-${a.id}` : "agent-models-new";
  const say = useIntl().formatMessage;

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
      <Row label={say(m.name)}>
        <Input name="name" defaultValue={a?.name} placeholder={say(m.namePlaceholder)} autoComplete="off" className={fieldClass} />
        <Meta>{say(m.nameHint)}</Meta>
      </Row>
      <Row label={say(m.kind)}>
        <select name="kind" defaultValue={a?.kind || "openai"} className={fieldClass}>
          {connect.kinds.map((k) => (
            <option key={k.kind} value={k.kind}>
              {k.label}
            </option>
          ))}
        </select>
        <Meta>{say(m.kindHint)}</Meta>
      </Row>
      <Row label={say(m.endpoint)}>
        <Input name="base_url" defaultValue={a?.base_url} placeholder={exampleEndpoint} autoComplete="off" className={fieldClass} />
        <Meta>{say(m.endpointHint)}</Meta>
      </Row>
      <Row label={say(m.apiKey)}>
        <Input name="api_key" type="password" autoComplete="new-password" placeholder={a?.has_key ? say(m.keySaved) : ""} className={fieldClass} />
        <Meta>{say(m.apiKeyHint)}</Meta>
      </Row>
      <Row label={say(m.model)}>
        <Input name="model" defaultValue={a?.model} list={modelsID} autoComplete="off" placeholder={say(m.modelPlaceholder)} className={fieldClass} />
        <datalist id={modelsID}>
          {models.map((id) => (
            // oxlint-disable-next-line jsx-a11y/control-has-associated-label -- a suggestion for the input, which has the label
            <option key={id} value={id} />
          ))}
        </datalist>
        <Meta>{models.length > 0 ? say(v.modelsListed, { n: models.length }) : say(m.modelsUnknown)}</Meta>
      </Row>
      <Row label={say(m.thinking)}>
        <span className="flex items-start gap-2.5 pt-2 text-sm">
          <Checkbox className="mt-0.5" checked={thinkingOff} onCheckedChange={(on) => setThinkingOff(on === true)} />
          <span>{say(m.thinkingOffBox)}</span>
        </span>
        <Meta>{say(m.thinkingHint)}</Meta>
      </Row>
      <Row label={say(m.search)}>
        <Input name="search_url" defaultValue={a?.search_url} placeholder={exampleSearch} autoComplete="off" className={fieldClass} />
        <Meta>{say(m.searchHint)}</Meta>
      </Row>
      <Row label={say(m.tools)}>
        <Input name="tools_url" defaultValue={a?.tools_url} placeholder={exampleTools} autoComplete="off" className={fieldClass} />
        <Meta>{say(m.toolsHintField)}</Meta>
      </Row>
      {others.length > 0 && (
        <Row label={say(m.fallback)} group>
          <div className="flex flex-wrap items-center gap-2">
            <FormattedMessage
              defaultMessage="{agent} <muted>after</muted> {minutes} <muted>minutes</muted>"
              id="qJav4K"
              description="The setting for an agent's fallback. {agent} is the choice of agent to hand over to, {minutes} the field for how many minutes to wait first"
              values={{
                muted,
                agent: (
                  <select name="fallback" aria-label={say(m.fallback)} defaultValue={String(a?.fallback ?? 0)} className={fieldClass}>
                    <option value="0">{say(m.noFallback)}</option>
                    {others.map((o) => (
                      <option key={o.id} value={String(o.id)}>
                        {o.name}
                      </option>
                    ))}
                  </select>
                ),
                minutes: (
                  <Input
                    type="number"
                    name="fallback_minutes"
                    aria-label={say(m.minutes)}
                    min={1}
                    defaultValue={a?.fallback_minutes || 15}
                    className={cn(fieldClass, "w-24")}
                  />
                ),
              }}
            />
          </div>
          <Meta>{say(m.fallbackHint)}</Meta>
        </Row>
      )}
      {offered.length > 0 && (
        <Row label={say(m.allowedTools)} group>
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
          <Meta>{say(v.allowedToolsHint, { linkedin })}</Meta>
        </Row>
      )}
      <div className="flex flex-wrap items-center gap-3">
        <ActionButton size="sm" type="submit" disabled={pending}>
          {say(a ? m.saveAndCheck : m.addAndCheck)}
        </ActionButton>
        {children}
      </div>
    </form>
  );
}

function Scores() {
  const { org } = useSession();
  const scores = $api.useQuery("get", "/o/{org}/api/v1/agent-scores", { params: { path: { org }, query: { limit: 200 } } });
  const say = useIntl().formatMessage;
  return (
    <Section title={say(m.scoresTitle)} hint={say(m.scoresHint)}>
      <Loading busy={!scores.data && !scores.error}>
        {scores.data?.items.length === 0 && <p className="text-sm text-muted-foreground">{say(m.nothingSuggested)}</p>}
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
  const say = useIntl().formatMessage;
  return (
    <Section title={say(m.workTitle)} hint={say(m.workHint)}>
      <Loading busy={!work.data && !work.error}>
        {work.data?.items.length === 0 && <p className="text-sm text-muted-foreground">{say(m.noneYet)}</p>}
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
        <nav className="mt-3 flex flex-wrap items-center justify-between gap-3" aria-label={say(m.runs)}>
          <span>
            {cursor && (
              <ActionButton quiet size="sm" onClick={() => setCursor(undefined)}>
                {say(m.latest)}
              </ActionButton>
            )}
          </span>
          <span>
            {older && (
              <ActionButton quiet size="sm" onClick={() => setCursor(older)}>
                {say(m.older)}
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
          {space}
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
  const say = useIntl().formatMessage;
  return (
    <Section title={say(m.activityTitle)}>
      <Loading busy={!activity.data && !activity.error}>
        {activity.data?.items.length === 0 && <p className="text-sm text-muted-foreground">{say(m.noneYet)}</p>}
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
        <nav className="mt-3 flex flex-wrap items-center justify-between gap-3" aria-label={say(m.pages)}>
          <span>
            {n > 1 && (
              <ActionButton quiet size="sm" onClick={() => setTrail(trail.slice(0, -1))}>
                {say(m.previous)}
              </ActionButton>
            )}
          </span>
          <Meta>{say(v.page, { n, pages })}</Meta>
          <span>
            {next && (
              <ActionButton quiet size="sm" onClick={() => setTrail([...trail, next])}>
                {say(m.next)}
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
  if (!e.has_key) return ["stop", intl.formatMessage(m.noKey)];
  if (e.monthly_limit === 0) return ["warn", intl.formatMessage(m.limitZero)];
  if (e.used_this_month >= e.monthly_limit) return ["warn", intl.formatMessage(v.allUsed, { limit: e.monthly_limit, month: month(), next: nextMonth() })];
  return ["ok", intl.formatMessage(v.used, { used: e.used_this_month, limit: e.monthly_limit, month: month() })];
}

// DataVendors are the enrichment vendors asked, top first, for what research can't find.
function DataVendors() {
  const { org, owner } = useSession();
  const refresh = useRefresh();
  const list = $api.useQuery("get", "/o/{org}/api/v1/enrichers", { params: { path: { org } } });
  const save = $api.useMutation("put", "/o/{org}/api/v1/enrichers");
  const vendors = list.data?.items ?? [];
  const [moving, setMoving] = useState(false);
  const say = useIntl().formatMessage;

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
    <Section title={say(m.vendorsTitle)} hint={say(v.vendorsHint, { scope: enrichSpend })}>
      <div className="flex flex-col gap-4">
        {list.data && vendors.length === 0 && <p className="text-sm text-muted-foreground">{say(m.noVendors)}</p>}
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
      {!owner && <Meta>{say(m.ownerVendors)}</Meta>}
      <p className="flex flex-wrap gap-x-4 gap-y-1 font-mono text-xs text-muted-foreground">
        <span>
          <FormattedMessage
            defaultMessage="{call} asks"
            id="nOR7Iw"
            description="Under the data vendors: the API call that asks them. {call} is the call, POST contacts/{id}/enrich"
            values={{ call: <Tag quiet>{enrichCall}</Tag> }}
          />
        </span>
        <span>
          <FormattedMessage
            defaultMessage="{tool} for agents"
            id="Ol3f5y"
            description="Under the data vendors: the tool agents ask them with. {tool} is the tool's name, enrich_contact"
            values={{ tool: <Tag quiet>{enrichTool}</Tag> }}
          />
        </span>
      </p>
    </Section>
  );
}

// button runs go in place of what the click would do.
const button = (go: () => void) => (ev: React.MouseEvent) => {
  ev.preventDefault();
  go();
};

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
  const say = useIntl().formatMessage;
  const used = !e ? 0 : e.monthly_limit > 0 ? Math.min(100, (e.used_this_month / e.monthly_limit) * 100) : 100;
  const submit = (ev: FormEvent) => {
    ev.preventDefault();
    if (!e) return;
    save.mutate({ params: { path: { org } }, body: { kind: e.kind, api_key: key, position: e.position, monthly_limit: Number(limit) } });
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
                  {say(v.finds, { detail: detailName(f) })}
                </Tag>
              ))}
            </span>
            <span className={cn("font-mono text-xs", rail && stateText[rail])}>
              <Text value={line} w="30ch" />
            </span>
          </span>
          <span className="flex items-center gap-2">
            {e && up && (
              <ActionButton quiet size="xs" aria-label={say(v.sooner, { vendor: vendorName(e.kind) })} onClick={button(up)}>
                {upArrow}
              </ActionButton>
            )}
            {e && down && (
              <ActionButton quiet size="xs" aria-label={say(v.later, { vendor: vendorName(e.kind) })} onClick={button(down)}>
                {downArrow}
              </ActionButton>
            )}
            <span className="rounded-lg border-[1.5px] border-ink bg-card px-2.5 py-1 text-xs font-semibold">{say(owner ? m.change : m.details)}</span>
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
                <Eyebrow>{say(m.apiKey)}</Eyebrow>
                <Input
                  type="password"
                  autoComplete="off"
                  value={key}
                  onChange={(ev) => setKey(ev.target.value)}
                  placeholder={say(e.has_key ? m.keySaved : m.keyPlaceholder)}
                  className={fieldClass}
                />
              </label>
              <label className="flex flex-col gap-1.5">
                <Eyebrow>{say(m.lookupsAtMost)}</Eyebrow>
                <Input
                  type="number"
                  min={0}
                  max={100000}
                  required
                  value={limit}
                  onChange={(ev) => setLimit(ev.target.value)}
                  className={cn(fieldClass, "w-24")}
                />
                <span className="text-xs text-muted-foreground">{say(m.zeroStops)}</span>
              </label>
              <div className="flex flex-wrap items-center gap-2">
                <ActionButton size="sm" type="submit" disabled={save.isPending}>
                  {say(m.save)}
                </ActionButton>
                <ActionButton quiet size="sm" type="button" disabled={remove.isPending} onClick={() => remove.mutate({ params: { path: { org, id: e.id } } })}>
                  {say(v.remove, { vendor: vendorName(e.kind) })}
                </ActionButton>
                {save.isSuccess && <span className="text-sm text-[var(--ok)]">{say(m.saved)}</span>}
              </div>
              <Failed errors={[save.error, remove.error]} />
            </form>
          ) : (
            <Meta>{say(v.askedAtMost, { n: e.monthly_limit, who: actor(e.updated_by), when: stamp(e.updated_at) })}</Meta>
          )}
          <p className="text-xs text-muted-foreground">{say(v.bound, { told: vendorTold(e.kind), costs: vendorCosts(e.kind) })}</p>
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
  const say = useIntl().formatMessage;
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
      <span className="font-heading text-[15px] font-semibold">{say(m.addVendor)}</span>
      {free.length === 0 ? (
        <Meta>{say(v.everyVendor, { prospector })}</Meta>
      ) : (
        <div className="flex flex-wrap items-end gap-3">
          <label className="flex flex-col gap-1.5">
            <Eyebrow>{say(m.vendor)}</Eyebrow>
            <select required value={kind} onChange={(ev) => setKind(ev.target.value)} className={fieldClass}>
              <option value="">{say(m.choose)}</option>
              {free.map((k) => (
                <option key={k} value={k}>
                  {vendorName(k)}
                </option>
              ))}
            </select>
          </label>
          <label className="flex min-w-[200px] flex-1 flex-col gap-1.5">
            <Eyebrow>{say(m.apiKey)}</Eyebrow>
            <Input type="password" autoComplete="off" required value={key} onChange={(ev) => setKey(ev.target.value)} className={fieldClass} />
          </label>
          <label className="flex flex-col gap-1.5">
            <Eyebrow>{say(m.lookups)}</Eyebrow>
            <Input type="number" min={0} max={100000} required value={limit} onChange={(ev) => setLimit(ev.target.value)} className={cn(fieldClass, "w-24")} />
          </label>
          <ActionButton size="sm" type="submit" disabled={add.isPending}>
            {say(m.add)}
          </ActionButton>
        </div>
      )}
      <p className="text-xs text-muted-foreground">{say(m.keysSealed)}</p>
      <Failed errors={[add.error]} />
    </form>
  );
}
