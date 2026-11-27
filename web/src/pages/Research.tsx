import { useState } from "react";
import { keepPreviousData } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { defineMessage, defineMessages, FormattedMessage, useIntl } from "react-intl";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Eyebrow, Failed, FilterChip, Meta, Meter, Pill, Section, stop, type Tone, usePageTitle, useRefresh, Whose } from "@/components/app";
import { Bar, Loading, Placeholders, placeholders, Text } from "@/components/app/skeleton";
import { actor, day, stamp } from "@/lib/format";
import { useKeyedState } from "@/lib/keyed";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { useSession } from "@/session";
import { detailName, month, vendorName } from "@/lib/vendors";
import { type Offering, useOfferings } from "@/lib/offerings";
import { cn } from "@/lib/utils";

type Company = components["schemas"]["ResearchCompany"];
type Criterion = components["schemas"]["Criterion"];
type Run = components["schemas"]["Run"];
type Agent = components["schemas"]["Agent"];
type TaskBrief = components["schemas"]["TaskBrief"];
type TaskCompany = components["schemas"]["TaskCompany"];
type Person = Company["people"][number];
type Enricher = components["schemas"]["Enricher"];

// Names are the same in every language.
const prospector = "Prospector";
const linkedIn = "LinkedIn";
const cross = "×";
const dot = " · ";
const chevron = "›";
const space = " ";
const comma = ", ";
const outward = " ↗";

const bold = (c: React.ReactNode[]) => <b>{c}</b>;

const m = defineMessages({
  research: { defaultMessage: "Research", id: "Tjl3hP", description: "Title and heading of the research page" },
  connectAgent: { defaultMessage: "Connect an agent", id: "00b7Sq", description: "Button on the research page to the page for setting up agents" },
  addPerson: { defaultMessage: "+ Add person", id: "Fj4Bnp", description: "Button on the research page to add a person by hand" },
  intro: {
    defaultMessage:
      "Say who you're looking for and an agent goes to find them. What agents, jobs and imports find waits here: nothing reaches outreach until someone qualifies it and picks a contact. Every fact keeps the link it came from.",
    id: "fzP2kj",
    description: "Paragraph under the research page's heading saying what the page is for",
  },
  whose: {
    defaultMessage: "Research is sales work, so here you read it.",
    id: "12dWYp",
    description: "On the research page, to a member who doesn't do sales: why they can only read it",
  },
  searches: {
    defaultMessage: "Searches",
    id: "jVc7oA",
    description: "Heading of the research page's list of open searches, and what a screen reader calls it",
  },
  everySearch: { defaultMessage: "Every search, closed ones too", id: "54Xc1m", description: "Link from the research page to the list of every search" },
  noSearch: { defaultMessage: "No search open.", id: "L6iW3x", description: "On the research page when no search is open" },
  selling: {
    defaultMessage: "Selling",
    id: "vHcjmH",
    description: "Label of the research page's row of offerings to pick whose queue is shown, and what a screen reader calls it",
  },
  yourOwn: {
    defaultMessage: "Your own",
    id: "k4na53",
    description: "In the research page's row of offerings: the organisation's own, when its name hasn't loaded",
  },
  filter: { defaultMessage: "Filter", id: "iT6cv9", description: "What a screen reader calls the research page's row of filters" },
  showEvery: {
    defaultMessage: "Show every search",
    id: "Y0fsY5",
    description: "Link or button to stop showing only one search's companies on the research page",
  },
  ranked: {
    defaultMessage: "Best fit first: no must missed, then most met, then fewest left unknown.",
    id: "ut+JNH",
    description: "On the research page: the order the companies of a search are listed in. A must is a criterion a company has to meet",
  },
  failed: {
    defaultMessage: "The queue couldn't be read. Reload to try again.",
    id: "yAIlAK",
    description: "When the research page's list of companies failed to load",
  },
  first: { defaultMessage: "← First", id: "k4kThJ", description: "Button back to the start of the research page's list" },
  older: { defaultMessage: "Older →", id: "+BZXGt", description: "Button to the next page of the research page's list" },
  filterReview: { defaultMessage: "To review", id: "IgLv+t", description: "Filter on the research page: companies waiting to be qualified or rejected" },
  filterContact: {
    defaultMessage: "Needs a contact",
    id: "dplTOl",
    description: "Filter on the research page: qualified companies with nobody picked to contact",
  },
  filterVerify: { defaultMessage: "Needs verifying", id: "WQEfWF", description: "Filter on the research page: contacts whose email is a guess" },
  filterDetails: {
    defaultMessage: "Details to check",
    id: "dbFG9c",
    description: "Filter on the research page: details found for people, waiting to be accepted",
  },
  filterRejected: {
    defaultMessage: "Rejected",
    id: "31wBYy",
    description: "Filter on the research page: companies rejected for the organisation's own offering",
  },
  junked: { defaultMessage: "Junked", id: "1UVqh/", description: "Filter on the research page: companies binned for another offering, in place of Rejected" },
  emptyReview: { defaultMessage: "Nothing to review", id: "p3QEcq", description: "Heading on the research page when nothing is waiting to be reviewed" },
  emptyReviewHint: {
    defaultMessage: "Companies your agents and discovery jobs add will wait here.",
    id: "i28Gr1",
    description: "On the research page when nothing is waiting to be reviewed",
  },
  emptyContact: {
    defaultMessage: "Every qualified company has a contact",
    id: "8+qEML",
    description: "Heading on the research page when no qualified company lacks a contact",
  },
  emptyContactHint: {
    defaultMessage: "Add someone with + Add person, or let an agent suggest people.",
    id: "2CydC5",
    description: "On the research page when no qualified company lacks a contact. + Add person is the button of that name",
  },
  emptyVerify: { defaultMessage: "No emails to verify", id: "awkoz5", description: "Heading on the research page when no contact's email is a guess" },
  emptyVerifyHint: {
    defaultMessage: "Contacts whose email is a pattern guess show up here.",
    id: "PoLBvs",
    description: "On the research page when no contact's email is a guess",
  },
  emptyDetails: { defaultMessage: "No details to check", id: "WnjdPv", description: "Heading on the research page when no found detail waits to be accepted" },
  emptyDetailsHint: {
    defaultMessage: "An email, phone or profile an agent finds for someone already here waits for you to accept it.",
    id: "594pUG",
    description: "On the research page when no found detail waits to be accepted",
  },
  emptyRejected: { defaultMessage: "Nothing here", id: "I6brYQ", description: "Heading on the research page when no company was rejected" },
  lookingFor: { defaultMessage: "Who are you looking for?", id: "np+FZ1", description: "Label of the box where a search is described" },
  briefExample: {
    defaultMessage: "Heads of procurement at UK kitchen makers with their own showrooms. About twenty, with the page that shows each one's role.",
    id: "zba2Hc",
    description: "Example in the box where a search is described",
  },
  howMany: {
    defaultMessage: "About how many companies",
    id: "eu9lWY",
    description: "What a screen reader calls the field for how many companies a search should find",
  },
  with: { defaultMessage: "with", id: "VMAH42", description: "Before the agents a search is sent to, as in: Find about 20 companies with Claude" },
  didntWork: { defaultMessage: "That didn't work.", id: "JPpPnr", description: "When starting a search failed with nothing from the server to say why" },
  find: { defaultMessage: "Find", id: "fGmy+l", description: "Button to start a search" },
  must: { defaultMessage: "must", id: "fsVQKi", description: "Beside a criterion a company has to meet" },
  showThese: { defaultMessage: "Show these", id: "ToaiqE", description: "Button to show only one search's companies on the research page" },
  notHere: { defaultMessage: "That search isn't here.", id: "Y/oI8e", description: "When the search being shown on the research page can't be found" },
  notes: { defaultMessage: "Agents' notes", id: "JjxF8R", description: "Heading of the notes agents left on a search" },
  everyRequest: { defaultMessage: "Every request, note and run", id: "22ESKg", description: "Link to a search's own page" },
  askMore: { defaultMessage: "Ask for more", id: "JX+D3g", description: "Label of the box for asking a search for more" },
  askExample: { defaultMessage: "Twenty more, in Scotland this time.", id: "F2Pytr", description: "Example in the box for asking a search for more" },
  ask: { defaultMessage: "Ask", id: "km4QGa", description: "Button to send a request for more to a search" },
  asked: { defaultMessage: "Asked.", id: "t3TOt+", description: "After a request for more was sent to a search" },
  heldRejected: { defaultMessage: "Rejected before", id: "jSsJTy", description: "Why a company a search came to was left out: it was rejected before" },
  heldCustomer: { defaultMessage: "Existing customer", id: "TTEOSE", description: "Why a company a search came to was left out: it is a customer" },
  heldOpenDeal: {
    defaultMessage: "Open conversation",
    id: "mEwBdB",
    description: "Why a company a search came to was left out: a conversation with it is open",
  },
  heldQualified: { defaultMessage: "Qualified already", id: "a/lXfh", description: "Why a company a search came to was left out: it was qualified already" },
  heldNeverContact: {
    defaultMessage: "Never contact, kept on file",
    id: "epZJG2",
    description: "Why a company a search came to was left out: it is on the never-contact list",
  },
  leftOut: {
    defaultMessage: "Left out",
    id: "0Fcu5u",
    description: "What a screen reader calls the panel of companies a search came to that were already here",
  },
  leftOutWhy: {
    defaultMessage: "Already here when this search came to them, or on a domain the never-contact list covers: kept, never up for review.",
    id: "v8v3AO",
    description: "In the panel of companies a search left out: why they were",
  },
  reviewAgain: { defaultMessage: "Review again", id: "lp7maQ", description: "Button to put a company rejected before back up for review" },
  neverContact: {
    defaultMessage: "Never contact",
    id: "ra3/v2",
    description: "In the panel of what a search left out: people refused as on the never-contact list",
  },
  erased: {
    defaultMessage: "Erased on request",
    id: "lXwsoK",
    description: "In the panel of what a search left out: people refused as erased at their request",
  },
  refusedWhy: {
    defaultMessage:
      "People an agent tried to add and was refused. They can't be included, and who they were isn't kept, so one person tried twice counts twice.",
    id: "B1gCLF",
    description: "In the panel of what a search left out, under the people refused",
  },
  met: { defaultMessage: "met", id: "5E4w2C", description: "A company's criterion is met" },
  notMet: { defaultMessage: "not met", id: "UaJIIV", description: "A company's criterion is not met" },
  notFound: { defaultMessage: "not found", id: "nOfqx2", description: "An agent looked for a company's criterion and didn't find it" },
  unchecked: { defaultMessage: "unchecked", id: "DN9gF+", description: "No agent has checked a company's criterion" },
  looked: { defaultMessage: "looked", id: "PdSfEa", description: "How a criterion was checked: an agent looked and found nothing" },
  inferred: { defaultMessage: "inferred", id: "oDszhZ", description: "How a criterion was checked: an agent inferred it" },
  seen: { defaultMessage: "seen on their site", id: "3iAFDY", description: "How a criterion was checked: an agent saw it on the company's site" },
  source: { defaultMessage: "source ↗", id: "vq2NQX", description: "Link to the page a fact came from" },
  notChecked: { defaultMessage: "No agent has checked this yet.", id: "PaLl0s", description: "Beside a criterion no agent has checked" },
  fit: { defaultMessage: "Fit", id: "s2mEtH", description: "Heading of how well a company fits, and what a screen reader calls the grade to qualify it at" },
  foundBy: { defaultMessage: "Found by", id: "NCrA9f", description: "Heading of who found a company" },
  qualify: { defaultMessage: "Qualify", id: "mp4wvV", description: "Button to qualify a company" },
  putBack: { defaultMessage: "Put back", id: "4qzFVJ", description: "Button to put a rejected company back up for review" },
  junk: { defaultMessage: "Junk", id: "5aDLnG", description: "Button to bin a company for another offering; nothing is sent to anyone" },
  reject: { defaultMessage: "Reject", id: "KoActI", description: "Button to reject a company" },
  facts: { defaultMessage: "Facts", id: "cuZbNR", description: "Heading of the facts found about a company" },
  whyItFits: { defaultMessage: "why it fits", id: "S7V2h5", description: "Beside an agent's reason a company fits" },
  people: { defaultMessage: "People", id: "V8IBGd", description: "Heading of the people found at a company" },
  noneYet: { defaultMessage: "None yet.", id: "a1XYeP", description: "When no one at a company has been found" },
  addSomeone: { defaultMessage: "Add someone", id: "d2Cw+R", description: "Link to add a person at a company" },
  emailVerified: { defaultMessage: "email verified", id: "msXQvr", description: "A person's email address has been checked" },
  emailGuess: { defaultMessage: "email is a guess", id: "xUQKbh", description: "A person's email address was worked out from the company's pattern" },
  pick: { defaultMessage: "Pick", id: "SiTJ6W", description: "Button to pick a suggested person as the company's contact" },
  suggested: { defaultMessage: "suggested", id: "8Ijl1I", description: "A person an agent suggested, for a member who can't pick them" },
  accept: { defaultMessage: "Accept", id: "HVKPd7", description: "Button to accept a detail found for a person" },
  dismiss: { defaultMessage: "Dismiss", id: "SmcTbd", description: "Button to dismiss a detail found for a person" },
});

type Msg = (typeof m)[keyof typeof m];

const v = {
  lookingForFor: defineMessage<{ offering: string }>({
    defaultMessage: "Who are you looking for, for {offering}?",
    id: "z6/sFR",
    description: "Label of the box where a search is described. {offering} is the name of what the search is for",
  }),
  from: defineMessage<{ title: string }>({
    defaultMessage: "From “{title}”",
    id: "KoD14D",
    description: "On the research page when only one search's companies are shown. {title} is the search's title",
  }),
  searchNumber: defineMessage<{ n: number }>({
    defaultMessage: "search {n}",
    id: "ssENcA",
    description: "Stands for a search's title while it loads. {n} is its number",
  }),
  runState: defineMessage<{ state: string }>({
    defaultMessage:
      "{state, select, stopping {stopping} waiting {waiting for its endpoint} running {running} finished {finished} stopped {stopped} failed {failed} other {{state}}}",
    id: "W5kGDV",
    description: "Where an agent's run on a search is. {state} is one of stopping, waiting, running, finished, stopped, failed",
  }),
  agentRun: defineMessage<{ agent: string; state: string }>({
    defaultMessage: "{agent} · {state}",
    id: "n8d2rY",
    description: "An agent running on a search. {agent} is its name; {state} where its run is, such as running",
  }),
  foundOf: defineMessage<{ found: number; want: number }>({
    defaultMessage: "{found} of about {want} found",
    id: "djs9DB",
    description: "How far a search has got. {found} is the new companies found; {want} how many were asked for",
  }),
  foundNew: defineMessage<{ found: number }>({
    defaultMessage: "{found, plural, one {# new company found} other {# new companies found}}",
    id: "WfYycz",
    description: "How many new companies a search has found",
  }),
  toReview: defineMessage<{ n: number }>({
    defaultMessage: "{n} to review",
    id: "vuSEJ7",
    description: "After how many companies a search found: how many of them wait for review",
  }),
  leftOutN: defineMessage<{ n: number }>({
    defaultMessage: "{n} left out",
    id: "/xCPC3",
    description: "After how many companies a search found: how many it came to that were already here",
  }),
  stopAgent: defineMessage<{ agent: string }>({
    defaultMessage: "Stop {agent}",
    id: "gHQcsa",
    description: "Button to stop an agent's run on a search. {agent} is its name",
  }),
  runAgent: defineMessage<{ agent: string }>({
    defaultMessage: "Run {agent}",
    id: "OPFGR7",
    description: "Button to set an agent going on a search. {agent} is its name",
  }),
  noteKind: defineMessage<{ kind: string; other: string }>({
    defaultMessage: "{kind, select, skipped {skipped} dead_end {dead end} next {next} other {{other}}}",
    id: "rKb9Yi",
    description: "What kind of note an agent left on a search. {other} is the kind as the server names it, for one not listed",
  }),
  leftOutCount: defineMessage<{ n: number }>({
    defaultMessage: "Left out · {n}",
    id: "My4lQ/",
    description: "Heading of the panel of what a search left out. {n} is how many",
  }),
  more: defineMessage<{ n: number }>({
    defaultMessage: "{n} more on the search's own page",
    id: "eD55lX",
    description: "Link to a search's own page, for the companies it left out that aren't listed here",
  }),
  refused: defineMessage<{ n: number }>({
    defaultMessage: "refused {n}×",
    id: "rtXs/J",
    description: "How many times an agent was refused adding people of this kind",
  }),
  fitGrade: defineMessage<{ grade: string }>({
    defaultMessage: "Fit {grade}",
    id: "QwNVvH",
    description: "Choice of the grade to qualify a company at. {grade} is A, B or C",
  }),
  factsPeople: defineMessage<{ facts: number; people: number }>({
    defaultMessage: "{facts, plural, one {# other fact} other {# other facts}} · {people, plural, one {# person} other {# people}}",
    id: "H46vQV",
    description: "Summary of a company's card that opens to show its facts and people",
  }),
  foundByVendor: defineMessage<{ vendor: string }>({
    defaultMessage: "found by {vendor}",
    id: "Eiyhbl",
    description: "Which enrichment vendor found a person's detail. {vendor} is its name",
  }),
  now: defineMessage<{ current: string }>({
    defaultMessage: "now {current}",
    id: "TOtIK2",
    description: "What a person's detail is now, beside a new one found for it",
  }),
  vendorValid: defineMessage<{ vendor: string }>({
    defaultMessage: "{vendor} checked it: valid",
    id: "JqNSxB",
    description: "An enrichment vendor checked the email address it found. {vendor} is its name",
  }),
  vendorGuess: defineMessage<{ vendor: string }>({
    defaultMessage: "{vendor} couldn't confirm it: a guess",
    id: "usAvp4",
    description: "An enrichment vendor couldn't confirm the email address it found. {vendor} is its name",
  }),
  missed: defineMessage<{ outcome: string; vendor: string; detail: string; on: string; again: string }>({
    defaultMessage:
      "{outcome, select, not_kept {{vendor} found one that can't be kept: {detail} on {on} · it can be asked again from {again}} other {{vendor} found no {detail} on {on} · it can be asked again from {again}}}",
    id: "deNoGE",
    description:
      "An enrichment vendor's lookup that found nothing to keep. {outcome} is not_kept when what it found may not be kept, otherwise it found nothing; {vendor} is its name; {detail} the detail, such as email; {on} and {again} are days",
  }),
  lookUp: defineMessage<{ detail: string }>({
    defaultMessage: "Look up {detail}",
    id: "+auOns",
    description: "Button to ask an enrichment vendor for a person's detail. {detail} is the detail, such as email",
  }),
  vendorLeft: defineMessage<{ vendor: string; left: number; limit: number }>({
    defaultMessage: "{vendor} ({left} of {limit} left)",
    id: "XUzj4e",
    description: "A vendor a lookup would go to. {vendor} is its name; {left} the lookups left this month of {limit}",
  }),
  asks: defineMessage<{ vendors: string; month: string }>({
    defaultMessage: "asks {vendors} in {month}",
    id: "uFqR/b",
    description: "Under the buttons to look up a person's details: who they ask. {vendors} are vendors joined with or; {month} the month",
  }),
};

const filters = [
  { key: "review", label: m.filterReview },
  { key: "contact", label: m.filterContact },
  { key: "verify", label: m.filterVerify },
  { key: "details", label: m.filterDetails },
  { key: "rejected", label: m.filterRejected },
] as const;
type Filter = (typeof filters)[number]["key"];

const empty: Record<Filter, [Msg, Msg | undefined]> = {
  review: [m.emptyReview, m.emptyReviewHint],
  contact: [m.emptyContact, m.emptyContactHint],
  verify: [m.emptyVerify, m.emptyVerifyHint],
  details: [m.emptyDetails, m.emptyDetailsHint],
  rejected: [m.emptyRejected, undefined],
};

export function ResearchPage() {
  const say = useIntl().formatMessage;
  const { org, sales } = useSession();
  const { f: filter = "review", task, of } = useSearch({ from: "/o/$org/research" });
  const navigate = useNavigate();
  // A cursor is a place in one list: another filter, search or offering starts at its top.
  const [cursor, setCursor] = useKeyedState<string | undefined>([filter, task, of], () => undefined);

  // A search is for one offering, so its queue is that offering's.
  const research = $api.useQuery(
    "get",
    "/o/{org}/api/v1/research",
    {
      params: { path: { org }, query: { filter, task, offering: task ? undefined : of, cursor } },
    },
    { placeholderData: keepPreviousData },
  );
  // The queue in view as the server has said, not one kept on screen while another loads.
  const settled = research.isPlaceholderData ? undefined : research.data;
  const offerings = useOfferings();
  // The same query useOfferings makes, to know when the offerings have come.
  const offered = $api.useQuery("get", "/o/{org}/api/v1/offerings", { params: { path: { org }, query: { limit: 200 } } });
  const settings = $api.useQuery("get", "/o/{org}/api/v1/settings", { params: { path: { org } } });
  const agents = $api.useQuery("get", "/o/{org}/api/v1/agents", { params: { path: { org } } });
  const enrichersQuery = $api.useQuery("get", "/o/{org}/api/v1/enrichers", { params: { path: { org } } });
  const enrichers = enrichersQuery.data?.items ?? [];
  const ready = (agents.data?.items ?? []).filter((a) => a.ready);
  // The same query SearchPanel makes, so the search is read once; closed searches aren't in tasks.
  const chosenQuery = $api.useQuery(
    "get",
    "/o/{org}/api/v1/tasks/{id}",
    { params: { path: { org, id: task ?? 0 }, query: { limit: 3 } } },
    { enabled: !!task },
  );
  const chosen = chosenQuery.data;
  // And the runs it lists, so what comes under it waits for them rather than being pushed down.
  const chosenRuns = $api.useQuery(
    "get",
    "/o/{org}/api/v1/tasks/{task}/runs",
    { params: { path: { org, task: task ?? 0 }, query: { limit: 3 } } },
    { enabled: !!task },
  );
  // Whose queue is shown is the server's to say: what is done on it goes to that offering,
  // whether or not the list of offerings has loaded yet.
  const sellingId = research.data?.offering_id ?? (task ? (chosen?.offering_id ?? 0) : (of ?? 0));
  const selling = offerings.find((o) => o.id === sellingId);
  const label = (key: Filter, text: Msg) => say(key === "rejected" && sellingId ? m.junked : text);
  const tasks = $api.useQuery(
    "get",
    "/o/{org}/api/v1/tasks",
    { params: { path: { org }, query: { limit: 5, offering: sellingId } } },
    { placeholderData: keepPreviousData },
  );
  const searches = tasks.data?.items ?? [];
  // What sits above the queue is as tall as these make it, so it is drawn, and the queue's
  // outline under it, only once they have all come; after that, picking another search
  // changes it in place rather than taking the page away.
  const [drawn, setDrawn] = useState(false);
  const above =
    drawn ||
    (!offered.isPending && !settings.isPending && !agents.isPending && !tasks.isPending && !tasks.isPlaceholderData && (!task || !chosenQuery.isPending));
  if (above && !drawn) setDrawn(true);
  const cardsReady = research.data && !enrichersQuery.isPending;
  const ranked = settled ? settled.ranked && settled.items.length > 0 : !research.error && !!task && filter === "review";
  const [emptyTitle, emptyHint] = empty[filter];

  usePageTitle(say(m.research));
  return (
    <>
      <header className="mb-5 flex flex-wrap items-baseline justify-between gap-3">
        <h1 className="font-heading text-4xl font-bold tracking-tight">
          {say(m.research)}
          <span className="text-coral">{stop}</span>
        </h1>
        <div className="flex flex-wrap gap-2">
          <ActionButton quiet size="sm" asChild>
            <Link to="/o/$org/agents" params={{ org }}>
              {say(m.connectAgent)}
            </Link>
          </ActionButton>
          {sales && (
            <ActionButton size="sm" asChild>
              <Link to="/o/$org/add" params={{ org }}>
                {say(m.addPerson)}
              </Link>
            </ActionButton>
          )}
        </div>
      </header>
      <p className="mb-4 max-w-[70ch] text-sm text-muted-foreground">{say(m.intro)}</p>

      <Whose area="sales" said={say(m.whose)} can={sales} />
      {!above && <Loading busy>{null}</Loading>}
      {above && (
        <>
          {sales && (
            <FindBox
              agents={ready}
              offering={settled?.offering_id}
              selling={selling}
              lookingFor={selling ? selling.looking_for : (settings.data?.looking_for ?? [])}
            />
          )}

          <div className="mb-5 flex flex-col gap-2" aria-label={say(m.searches)}>
            <div className="flex flex-wrap items-baseline justify-between gap-2">
              <Eyebrow>{say(m.searches)}</Eyebrow>
              <Link to="/o/$org/tasks" params={{ org }} className="text-[13px] text-mark underline">
                {say(m.everySearch)}
              </Link>
            </div>
            {searches.map((t) => (
              <SearchStrip key={t.id} task={t} agents={ready} chosen={t.id === task} />
            ))}
            {tasks.data && searches.length === 0 && <p className="text-sm text-muted-foreground">{say(m.noSearch)}</p>}
          </div>

          <div className="flex flex-wrap items-start gap-6">
            <div className="flex min-w-0 flex-[999_1_520px] flex-col gap-3">
              {offerings.length > 0 && (
                <nav
                  className="flex flex-wrap items-center gap-1.5 rounded-lg border border-dashed border-border bg-muted px-2.5 py-2"
                  aria-label={say(m.selling)}
                >
                  <span className="mr-1 font-mono text-[11px] uppercase tracking-wider text-muted-foreground">{say(m.selling)}</span>
                  {[{ id: 0, name: settings.data?.name ?? say(m.yourOwn) }, ...offerings].map((o) => (
                    <Link
                      key={o.id}
                      to="/o/$org/research"
                      params={{ org }}
                      search={{ f: filter === "review" ? undefined : filter, of: o.id || undefined }}
                      aria-current={o.id === sellingId ? "true" : undefined}
                      className={cn(
                        "rounded-md border px-2.5 py-1 text-[13px] font-medium text-foreground no-underline",
                        o.id === sellingId ? "border-ink bg-card" : "border-transparent hover:border-border",
                      )}
                    >
                      {o.name}
                    </Link>
                  ))}
                </nav>
              )}
              <nav className="flex flex-wrap items-center gap-2" aria-label={say(m.filter)}>
                {filters.map((f) => (
                  <FilterChip
                    key={f.key}
                    to="/o/$org/research"
                    params={{ org }}
                    search={{ f: f.key === "review" ? undefined : f.key, task, of }}
                    label={label(f.key, f.label)}
                    count={research.data && (research.data.counts[f.key] ?? 0)}
                    on={f.key === filter}
                  />
                ))}
                {task && (
                  <span className="inline-flex items-center gap-1">
                    <Pill tone="agent">{say(v.from, { title: chosen?.title ?? say(v.searchNumber, { n: task }) })}</Pill>
                    <Link
                      to="/o/$org/research"
                      params={{ org }}
                      search={{ f: filter === "review" ? undefined : filter, of: sellingId || undefined }}
                      className="px-1 text-sm text-muted-foreground no-underline"
                      aria-label={say(m.showEvery)}
                    >
                      {cross}
                    </Link>
                  </span>
                )}
              </nav>
              {ranked && (
                <Meta>
                  <Text value={settled && say(m.ranked)} w="52ch" />
                </Meta>
              )}
              {settled && settled.items.length === 0 && (
                <div className="panel p-6 text-center">
                  <p className="font-heading font-semibold">{say(emptyTitle)}</p>
                  <p className="text-sm text-muted-foreground">{emptyHint && say(emptyHint)}</p>
                </div>
              )}
              {research.error && (
                <p className="text-sm text-[var(--stop)]" role="alert">
                  {say(m.failed)}
                </p>
              )}
              {!cardsReady && !research.error && (
                <Placeholders className="flex flex-col gap-3">
                  {Array.from({ length: placeholders }, (_, i) => (
                    <CompanyCard key={i} open={false} enrichers={[]} offering={0} />
                  ))}
                </Placeholders>
              )}
              <div className="flex flex-col gap-3">
                {cardsReady &&
                  research.data?.items.map((c, i) => (
                    <CompanyCard key={`${sellingId}-${c.id}`} company={c} open={i === 0} enrichers={enrichers} offering={research.data?.offering_id ?? 0} />
                  ))}
              </div>
              <nav className="flex justify-between gap-3">
                <span>
                  {cursor && (
                    <ActionButton quiet size="sm" onClick={() => setCursor(undefined)}>
                      {say(m.first)}
                    </ActionButton>
                  )}
                </span>
                <span>
                  {research.data?.next_cursor && (
                    <ActionButton quiet size="sm" onClick={() => setCursor(research.data?.next_cursor)}>
                      {say(m.older)}
                    </ActionButton>
                  )}
                </span>
              </nav>
            </div>
            {task && (
              <aside className="flex min-w-0 flex-[1_1_280px] flex-col gap-4">
                <SearchPanel id={task} onGone={() => navigate({ to: "/o/$org/research", params: { org }, search: { of: sellingId || undefined } })} />
                {!chosenRuns.isPending && <LeftOutPanel id={task} />}
              </aside>
            )}
          </div>
        </>
      )}
    </>
  );
}

// offering is the queue in view, undefined until the server has said which it is.
function FindBox({
  agents,
  offering,
  selling,
  lookingFor,
}: {
  agents: Agent[];
  offering?: number;
  selling?: Offering;
  lookingFor: components["schemas"]["LookFor"][];
}) {
  const say = useIntl().formatMessage;
  const { org } = useSession();
  const navigate = useNavigate();
  const refresh = useRefresh();
  const [brief, setBrief] = useState("");
  const [picked, setPicked] = useState<number[] | null>(null);
  const [want, setWant] = useState("");
  const send = picked ?? agents.slice(0, 1).map((a) => a.id);
  const create = $api.useMutation("post", "/o/{org}/api/v1/tasks", {
    onSuccess: (t) => {
      setBrief("");
      setWant("");
      refresh();
      navigate({ to: "/o/$org/research", params: { org }, search: { task: t.id } });
    },
  });
  const agentsLink = (c: React.ReactNode[]) => (
    <Link to="/o/$org/agents" params={{ org }} className="text-mark underline">
      {c}
    </Link>
  );
  const settingsLink = (c: React.ReactNode[]) => (
    <Link to="/o/$org/settings" params={{ org }} className="text-mark underline">
      {c}
    </Link>
  );

  return (
    <form
      className="panel mb-5 overflow-hidden shadow-[4px_4px_0_var(--edge)]"
      onSubmit={(e) => {
        e.preventDefault();
        create.mutate({
          params: { path: { org } },
          body: { title: "", brief, agents: send, offering: offering || undefined, want: want === "" ? undefined : Number(want) },
        });
      }}
    >
      <div className="flex flex-col gap-2.5 px-5 py-4">
        <label htmlFor="find-brief">
          <Eyebrow>{selling ? say(v.lookingForFor, { offering: selling.name }) : say(m.lookingFor)}</Eyebrow>
        </label>
        <Textarea
          id="find-brief"
          required
          maxLength={4000}
          rows={2}
          value={brief}
          onChange={(e) => setBrief(e.target.value)}
          placeholder={say(m.briefExample)}
          className="rounded-lg border-input bg-muted text-base leading-relaxed"
        />
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
            <label className="contents" htmlFor="find-want">
              <FormattedMessage
                defaultMessage="Find about {count} companies"
                id="9VnAyh"
                description="Beside the search box. {count} is a field for how many companies to find"
                values={{
                  count: (
                    <Input
                      id="find-want"
                      type="number"
                      min={1}
                      max={500}
                      value={want}
                      onChange={(e) => setWant(e.target.value)}
                      aria-label={say(m.howMany)}
                      className="h-8 w-16 bg-muted px-2"
                    />
                  ),
                }}
              />
            </label>
            {agents.length === 0 ? (
              <span>
                <FormattedMessage
                  defaultMessage="No agent of {prospector}'s own is ready, so this waits for one of yours. <link>Set one up</link>"
                  id="4T465k"
                  description="Beside the search box when no built-in agent is ready. {prospector} is the product's name; the link goes to the agents page"
                  values={{
                    prospector,
                    link: agentsLink,
                  }}
                />
              </span>
            ) : (
              <>
                <span>{say(m.with)}</span>
                {agents.map((a) => (
                  <label
                    key={a.id}
                    className="inline-flex cursor-pointer items-center gap-1.5 rounded-full border-[1.5px] border-ink bg-card px-2.5 py-1 text-foreground"
                  >
                    <Checkbox checked={send.includes(a.id)} onCheckedChange={(on) => setPicked(on ? [...send, a.id] : send.filter((id) => id !== a.id))} />
                    {a.name}
                  </label>
                ))}
              </>
            )}
          </div>
          <div className="flex items-center gap-3">
            {create.error && <p className="text-sm text-[var(--stop)]">{create.error.detail ?? say(m.didntWork)}</p>}
            <ActionButton type="submit" disabled={create.isPending || offering === undefined}>
              {say(m.find)}
            </ActionButton>
          </div>
        </div>
      </div>
      <div className="flex flex-wrap gap-x-6 gap-y-2 border-t border-border bg-secondary px-5 py-3">
        {lookingFor.map((a) => (
          <div key={a.field} className="flex min-w-0 flex-[1_1_280px] items-baseline gap-2">
            <span className="w-[104px] shrink-0 font-mono text-xs">{a.field}</span>
            <span className="min-w-0 text-sm">{a.hint}</span>
            {a.must && <span className="font-mono text-[11px] text-mark">{say(m.must)}</span>}
          </div>
        ))}
        <p className="w-full text-[13px] text-muted-foreground">
          {lookingFor.length === 0 && selling ? (
            <FormattedMessage
              defaultMessage="Agents look for what you write. Settings › What you sell adds what they check at every company. <link>Change them</link>"
              id="ws4nzJ"
              description="Under the search box, with nothing set to check, for another offering. Settings › What you sell is a place in Settings"
              values={{ link: settingsLink }}
            />
          ) : lookingFor.length === 0 ? (
            <FormattedMessage
              defaultMessage="Agents look for what you write. Settings › What you look for adds what they check at every company. <link>Change them</link>"
              id="VwX+MR"
              description="Under the search box, with nothing set to check. Settings › What you look for is a place in Settings"
              values={{ link: settingsLink }}
            />
          ) : selling ? (
            <FormattedMessage
              defaultMessage="Agents check each of these, from Settings › What you sell › {offering}, and record what they find as a fact with its source. <link>Change them</link>"
              id="0Mqlku"
              description="Under the search box, below what agents check at every company, for another offering. Settings › What you sell › {offering} is a place in Settings; {offering} is its name"
              values={{ offering: selling.name, link: settingsLink }}
            />
          ) : (
            <FormattedMessage
              defaultMessage="Agents check each of these, from Settings › What you look for, and record what they find as a fact with its source. <link>Change them</link>"
              id="RHnee5"
              description="Under the search box, below what agents check at every company. Settings › What you look for is a place in Settings"
              values={{ link: settingsLink }}
            />
          )}
        </p>
      </div>
    </form>
  );
}

function runState(say: ReturnType<typeof useIntl>["formatMessage"], r: Run) {
  return say(v.runState, { state: r.stop_asked ? "stopping" : r.status });
}

function SearchStrip({ task, agents, chosen }: { task: TaskBrief; agents: Agent[]; chosen: boolean }) {
  const say = useIntl().formatMessage;
  const { org, sales } = useSession();
  const { id, title, want } = task;
  // Companies already held when the search came to them aren't new, so they don't count towards the number.
  const found = task.found.companies - task.left_out;
  const refresh = useRefresh();
  const runs = $api.useQuery("get", "/o/{org}/api/v1/tasks/{task}/runs", { params: { path: { org, task: id }, query: { limit: 3 } } });
  const going = (runs.data?.items ?? []).filter((r) => r.status === "running" || r.status === "waiting");
  const start = $api.useMutation("post", "/o/{org}/api/v1/tasks/{task}/runs", { onSuccess: refresh });
  const halt = $api.useMutation("post", "/o/{org}/api/v1/runs/{id}/stop", { onSuccess: refresh });
  const progress = [
    want ? say(v.foundOf, { found, want }) : say(v.foundNew, { found }),
    task.to_review > 0 && say(v.toReview, { n: task.to_review }),
    task.left_out > 0 && say(v.leftOutN, { n: task.left_out }),
  ].filter(Boolean);

  return (
    <div
      className={`flex flex-wrap items-center gap-x-4 gap-y-2 rounded-xl border-[1.5px] border-ink bg-card px-4 py-2.5 ${going.length > 0 ? "shadow-[4px_4px_0_#b9aef2]" : ""}`}
    >
      <b className="font-heading text-[15px]">{title}</b>
      {going.map((r) => (
        <Pill key={r.id} tone="agent">
          {say(v.agentRun, { agent: r.agent_name, state: runState(say, r) })}
        </Pill>
      ))}
      {want ? <Meter value={found} of={want} /> : null}
      <span className="font-mono text-xs text-muted-foreground">{progress.join(" · ")}</span>
      <span className="ml-auto flex flex-wrap gap-2">
        {!chosen && (
          <ActionButton quiet size="sm" asChild>
            <Link to="/o/$org/research" params={{ org }} search={{ task: id }}>
              {say(m.showThese)}
            </Link>
          </ActionButton>
        )}
        {sales &&
          going.map((r) => (
            <ActionButton key={r.id} quiet size="sm" onClick={() => halt.mutate({ params: { path: { org, id: r.id } } })}>
              {say(v.stopAgent, { agent: r.agent_name })}
            </ActionButton>
          ))}
        {sales &&
          agents
            .filter((a) => !going.some((r) => r.agent === a.id))
            .map((a) => (
              <ActionButton key={a.id} quiet size="sm" onClick={() => start.mutate({ params: { path: { org, task: id } }, body: { agent: a.id } })}>
                {say(v.runAgent, { agent: a.name })}
              </ActionButton>
            ))}
      </span>
      <Failed errors={[start.error, halt.error]} />
    </div>
  );
}

function SearchPanel({ id, onGone }: { id: number; onGone: () => void }) {
  const say = useIntl().formatMessage;
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
        <p className="text-sm">{say(m.notHere)}</p>
        <ActionButton quiet size="sm" onClick={onGone}>
          {say(m.showEvery)}
        </ActionButton>
      </Section>
    );
  }
  const t = task.data;
  return (
    <Section className="gap-2.5 p-5">
      <Eyebrow>
        <Text value={t?.title} w="16ch" />
      </Eyebrow>
      {/* The brief runs to any number of lines, so it and all that follows come together. */}
      {t && !runs.isPending && (
        <>
          <p className="text-sm">{t.brief}</p>
          {(runs.data?.items ?? []).map((r) => (
            <div key={r.id} className="flex flex-col gap-0.5 border-t border-dashed border-border pt-2 text-sm">
              <span>
                <FormattedMessage
                  defaultMessage="<b>{agent}</b> · {state}"
                  id="nYBbXx"
                  description="An agent's run on a search. {agent} is its name; {state} where its run is, such as finished"
                  values={{ agent: r.agent_name, state: runState(say, r), b: bold }}
                />
              </span>
              <Meta>{[r.started_by, stamp(r.started_at), r.stop_reason].filter(Boolean).join(" · ")}</Meta>
            </div>
          ))}
          {t && t.notes.length > 0 && (
            <div className="flex flex-col gap-2 border-t border-dashed border-border pt-2">
              <Eyebrow>{say(m.notes)}</Eyebrow>
              {t.notes.map((n) => (
                <p key={n.id} className="text-[13px]">
                  {n.text}
                  <span className="block font-mono text-xs text-muted-foreground">
                    {[say(v.noteKind, { kind: n.kind, other: n.kind.replace("_", " ") }), actor(n.actor), stamp(n.at)].join(" · ")}
                  </span>
                </p>
              ))}
            </div>
          )}
          <Link to="/o/$org/tasks/$id" params={{ org, id }} className="text-[13px] text-mark underline">
            {say(m.everyRequest)}
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
                <Eyebrow>{say(m.askMore)}</Eyebrow>
              </label>
              <Textarea
                id="ask-more"
                required
                maxLength={2000}
                rows={2}
                value={text}
                onChange={(e) => setText(e.target.value)}
                placeholder={say(m.askExample)}
                className="rounded-lg border-input bg-muted text-sm leading-relaxed"
              />
              <div className="flex items-center gap-3">
                <ActionButton type="submit" size="sm" disabled={ask.isPending}>
                  {say(m.ask)}
                </ActionButton>
                {ask.isSuccess && <p className="text-sm text-[var(--ok)]">{say(m.asked)}</p>}
                <Failed errors={[ask.error]} />
              </div>
            </form>
          )}
        </>
      )}
    </Section>
  );
}

const heldLabel: Record<NonNullable<TaskCompany["held"]>, Msg> = {
  rejected: m.heldRejected,
  customer: m.heldCustomer,
  open_deal: m.heldOpenDeal,
  qualified: m.heldQualified,
  never_contact: m.heldNeverContact,
};

// LeftOutPanel is what a search came to that was already held, and the people it was
// refused: matched on web domain, name, email address and profile against what's here.
function LeftOutPanel({ id }: { id: number }) {
  const say = useIntl().formatMessage;
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const left = $api.useQuery("get", "/o/{org}/api/v1/tasks/{id}/left-out", { params: { path: { org, id }, query: { limit: 200 } } });
  const again = $api.useMutation("patch", "/o/{org}/api/v1/companies/{id}", { onSuccess: refresh });
  const d = left.data;
  if (!d) return null;
  const held = d.rejected + d.customer + d.open_deal + d.qualified + d.listed;
  const refused = d.never_contact + d.erased;
  if (held + refused === 0) return null;
  const counts: [string, number][] = [
    [say(heldLabel.open_deal), d.open_deal],
    [say(heldLabel.customer), d.customer],
    [say(heldLabel.qualified), d.qualified],
    [say(heldLabel.rejected), d.rejected],
    [say(heldLabel.never_contact), d.listed],
  ];
  return (
    <Section className="gap-2.5 p-5" aria-label={say(m.leftOut)}>
      <Eyebrow>{say(v.leftOutCount, { n: held + refused })}</Eyebrow>
      <p className="text-[13px] text-muted-foreground">{say(m.leftOutWhy)}</p>
      {counts
        .filter(([, n]) => n > 0)
        .map(([label, n]) => (
          <div key={label} className="flex justify-between gap-3 text-sm">
            <span>{label}</span>
            <span className="font-mono text-xs text-muted-foreground">{n}</span>
          </div>
        ))}
      {d.items.map((c) => (
        <div key={c.id} className="flex flex-wrap items-baseline justify-between gap-2 border-t border-dashed border-border pt-2 text-sm">
          <Link to="/o/$org/companies/$id" params={{ org, id: c.id }} className="text-mark underline">
            {c.name}
          </Link>
          <span className="flex items-baseline gap-2">
            <Meta>{c.held ? say(heldLabel[c.held]) : ""}</Meta>
            {sales && c.held === "rejected" && c.stage === "rejected" && (
              <ActionButton
                quiet
                size="sm"
                disabled={again.isPending}
                onClick={() => again.mutate({ params: { path: { org, id: c.id } }, body: { stage: "review", offering: d.offering } })}
              >
                {say(m.reviewAgain)}
              </ActionButton>
            )}
          </span>
        </div>
      ))}
      {d.next_cursor && (
        <Link to="/o/$org/tasks/$id" params={{ org, id }} className="text-[13px] text-mark underline">
          {say(v.more, { n: held - d.items.length })}
        </Link>
      )}
      {refused > 0 && (
        <div className="flex flex-col gap-1 border-t border-dashed border-border pt-2 text-sm">
          {d.never_contact > 0 && (
            <div className="flex justify-between gap-3">
              <span>{say(m.neverContact)}</span>
              <span className="font-mono text-xs text-muted-foreground">{say(v.refused, { n: d.never_contact })}</span>
            </div>
          )}
          {d.erased > 0 && (
            <div className="flex justify-between gap-3">
              <span>{say(m.erased)}</span>
              <span className="font-mono text-xs text-muted-foreground">{say(v.refused, { n: d.erased })}</span>
            </div>
          )}
          <p className="text-[13px] text-muted-foreground">{say(m.refusedWhy)}</p>
        </div>
      )}
      <Failed errors={[again.error]} />
    </Section>
  );
}

const stateText: Record<Criterion["state"], [Msg, Tone]> = {
  met: [m.met, "ok"],
  not_met: [m.notMet, "stop"],
  not_found: [m.notFound, ""],
  unchecked: [m.unchecked, ""],
};

function CriterionRow({ c }: { c: Criterion }) {
  const say = useIntl().formatMessage;
  const [text, tone] = stateText[c.state];
  const how = say(c.state === "not_found" ? m.looked : c.inferred ? m.inferred : m.seen);
  return (
    <div className="grid grid-cols-1 items-baseline gap-1 border-t border-dashed border-border px-5 py-2 sm:grid-cols-[168px_96px_1fr] sm:gap-3">
      <span className="font-mono text-xs text-muted-foreground">
        {c.field}
        {c.must && (
          <span className="text-mark">
            {dot}
            {say(m.must)}
          </span>
        )}
      </span>
      <span>
        <Pill tone={tone}>{say(text)}</Pill>
      </span>
      <span className="min-w-0 text-sm">
        {c.at ? (
          <>
            {c.value}
            <span className="block font-mono text-[11px] text-muted-foreground">
              {[how, actor(c.by ?? ""), day(c.at)].join(" · ")}
              {c.source_url && (
                <>
                  {dot}
                  <SourceLink href={c.source_url}>{say(m.source)}</SourceLink>
                </>
              )}
            </span>
          </>
        ) : (
          <span className="text-muted-foreground">{say(m.notChecked)}</span>
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

// A card is reviewed, and its people picked, for the offering in view; 0 is the organisation's own.
// Without a company it is the card's outline while the queue loads.
function CompanyCard({ company: c, open, enrichers, offering }: { company?: Company; open: boolean; enrichers: Enricher[]; offering: number }) {
  const say = useIntl().formatMessage;
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const [grade, setGrade] = useState(c?.grade || "B");
  const patch = $api.useMutation("patch", "/o/{org}/api/v1/companies/{id}", { onSuccess: refresh });
  const review = {
    mutate: (to: { params: { path: { org: string; id: number } }; body: { stage: "review" | "qualified" | "rejected"; fit?: string } }) =>
      patch.mutate({ ...to, body: { ...to.body, offering: offering || undefined } }),
  };
  const pick = $api.useMutation("post", "/o/{org}/api/v1/contacts/{id}/pick", { onSuccess: refresh });
  const decide = $api.useMutation("patch", "/o/{org}/api/v1/suggestions/{id}", { onSuccess: refresh });

  return (
    <article className="panel">
      <div className="flex flex-wrap items-start gap-x-6 gap-y-3 px-5 py-4">
        <div className="min-w-[200px] flex-[2_1_220px]">
          {c ? (
            <Link to="/o/$org/companies/$id" params={{ org, id: c.id }} className="font-heading text-lg font-semibold text-foreground hover:text-mark">
              {c.name}
            </Link>
          ) : (
            <span className="font-heading text-lg font-semibold">
              <Bar w="16ch" />
            </span>
          )}
          <p className="font-mono text-xs text-muted-foreground">
            <Text value={c && [c.country, c.segment, c.domain].filter(Boolean).join(" · ")} w="24ch" />
          </p>
        </div>
        {c && c.criteria.length > 0 && (
          <div className="flex flex-[1_1_180px] flex-col gap-1">
            <Eyebrow>{say(m.fit)}</Eyebrow>
            <span className="font-mono text-[13px] font-medium">{c.fit.text}</span>
          </div>
        )}
        <div className="flex flex-[1_1_150px] flex-col gap-1">
          <Eyebrow>{say(m.foundBy)}</Eyebrow>
          <div>
            <Pill tone={c?.found_by.startsWith("agent:") ? "agent" : ""}>
              <Text value={c && actor(c.found_by)} w="8ch" />
            </Pill>
          </div>
          <Meta>
            <Text value={c && stamp(c.created_at)} w="14ch" />
          </Meta>
        </div>
        {c && sales && (
          <div className="flex flex-[1_1_220px] flex-wrap items-center justify-end gap-2">
            {c.stage !== "qualified" && (
              <>
                <select
                  aria-label={say(m.fit)}
                  value={grade}
                  onChange={(e) => setGrade(e.target.value)}
                  className="h-8 rounded-md border border-input bg-muted px-2 font-mono text-xs"
                >
                  {["A", "B", "C"].map((g) => (
                    <option key={g} value={g}>
                      {say(v.fitGrade, { grade: g })}
                    </option>
                  ))}
                </select>
                <ActionButton size="sm" onClick={() => review.mutate({ params: { path: { org, id: c.id } }, body: { stage: "qualified", fit: grade } })}>
                  {say(m.qualify)}
                </ActionButton>
              </>
            )}
            {c.stage === "rejected" ? (
              <ActionButton quiet size="sm" onClick={() => review.mutate({ params: { path: { org, id: c.id } }, body: { stage: "review" } })}>
                {say(m.putBack)}
              </ActionButton>
            ) : (
              // For another offering it is binned quietly; nothing is sent to anyone.
              <ActionButton quiet size="sm" onClick={() => review.mutate({ params: { path: { org, id: c.id } }, body: { stage: "rejected" } })}>
                {say(offering ? m.junk : m.reject)}
              </ActionButton>
            )}
          </div>
        )}
        <Failed errors={[patch.error, pick.error]} />
      </div>
      {c?.criteria.map((cr) => (
        <CriterionRow key={cr.field} c={cr} />
      ))}
      <details className="group border-t border-border/70 bg-muted" open={open}>
        <summary className="flex cursor-pointer list-none items-center gap-2 px-5 py-2.5 font-mono text-xs text-muted-foreground hover:text-foreground">
          <span className="transition-transform group-open:rotate-90">{chevron}</span>
          <Text value={c && say(v.factsPeople, { facts: c.facts.length, people: c.people.length })} w="18ch" />
        </summary>
        {c && (
          <div className="flex flex-wrap gap-6 px-5 pb-5">
            <div className="min-w-0 flex-[2_1_380px]">
              <Eyebrow>{say(m.facts)}</Eyebrow>
              {c.why && (
                <div className="grid grid-cols-[130px_1fr] gap-3 border-b border-dashed border-border py-2 text-sm">
                  <span className="font-mono text-xs text-muted-foreground">{say(m.whyItFits)}</span>
                  <span>{c.why}</span>
                </div>
              )}
              {c.facts.map((f) => (
                <div key={f.id} className="grid grid-cols-[130px_1fr_auto] gap-3 border-b border-dashed border-border py-2 text-sm">
                  <span className="font-mono text-xs text-muted-foreground">{f.field}</span>
                  <span>{f.value}</span>
                  <SourceLink href={f.source_url}>{say(m.source)}</SourceLink>
                </div>
              ))}
            </div>
            <div className="min-w-0 flex-[1_1_260px]">
              <Eyebrow>{say(m.people)}</Eyebrow>
              {c.people.length === 0 && (
                <p className="py-2 text-sm text-muted-foreground">
                  {say(m.noneYet)}
                  {space}
                  {sales && (
                    <Link to="/o/$org/add" params={{ org }} search={{ company: c.name }}>
                      {say(m.addSomeone)}
                    </Link>
                  )}
                </p>
              )}
              {c.people.map((p) => (
                <div key={p.id} className="flex flex-col gap-1.5 border-b border-dashed border-border py-2.5 text-sm">
                  <div>
                    <b>{p.name}</b>
                    <span className="text-muted-foreground">
                      {p.title && (
                        <>
                          {comma}
                          {p.title}
                        </>
                      )}
                    </span>
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    {p.email_status === "verified" && <Pill tone="ok">{say(m.emailVerified)}</Pill>}
                    {p.email_status === "guess" && <Pill tone="warn">{say(m.emailGuess)}</Pill>}
                    {p.linkedin && (
                      <SourceLink href={p.linkedin}>
                        {linkedIn}
                        {outward}
                      </SourceLink>
                    )}
                    {p.source_url && <SourceLink href={p.source_url}>{say(m.source)}</SourceLink>}
                  </div>
                  {p.status === "suggested" && sales && (
                    <div>
                      <ActionButton size="xs" onClick={() => pick.mutate({ params: { path: { org, id: p.id }, query: { offering } } })}>
                        {say(m.pick)}
                      </ActionButton>
                    </div>
                  )}
                  {p.status === "suggested" && !sales && <Meta>{say(m.suggested)}</Meta>}
                  {p.pending.map((d) => (
                    <div key={d.id} className="flex flex-wrap items-center gap-2 rounded-lg bg-card px-3 py-2">
                      <span className="font-mono text-xs text-muted-foreground">{d.field}</span>
                      <span>{d.value}</span>
                      {d.vendor && <Pill>{say(v.foundByVendor, { vendor: vendorName(d.vendor) })}</Pill>}
                      {d.current && <Meta>{say(v.now, { current: d.current })}</Meta>}
                      {d.source_url && <SourceLink href={d.source_url}>{say(m.source)}</SourceLink>}
                      {d.vendor && d.email_status && (
                        <Meta className="w-full">{say(d.email_status === "verified" ? v.vendorValid : v.vendorGuess, { vendor: vendorName(d.vendor) })}</Meta>
                      )}
                      {sales && (
                        <span className="ml-auto flex gap-2">
                          <ActionButton size="xs" onClick={() => decide.mutate({ params: { path: { org, id: d.id } }, body: { decision: "accepted" } })}>
                            {say(m.accept)}
                          </ActionButton>
                          <ActionButton quiet size="xs" onClick={() => decide.mutate({ params: { path: { org, id: d.id } }, body: { decision: "dismissed" } })}>
                            {say(m.dismiss)}
                          </ActionButton>
                        </span>
                      )}
                    </div>
                  ))}
                  <Lookups person={p} enrichers={enrichers} />
                </div>
              ))}
            </div>
          </div>
        )}
      </details>
    </article>
  );
}

type Detail = "email" | "phone" | "linkedin";

const details: Detail[] = ["email", "phone", "linkedin"];

const has = (p: Person, field: Detail) => (field === "email" ? p.has_email : field === "phone" ? p.has_phone : !!p.linkedin);

// Lookups ask the organisation's vendors for an email, mobile or profile nobody has
// found, and say when one that found nothing can be asked again.
function Lookups({ person: p, enrichers }: { person: Person; enrichers: Enricher[] }) {
  const intl = useIntl();
  const say = intl.formatMessage;
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const ask = $api.useMutation("post", "/o/{org}/api/v1/contacts/{id}/enrich", { onSuccess: refresh });
  const wanted = p.unlookable ? [] : details.filter((f) => !has(p, f) && !p.pending.some((d) => d.field === f));
  const misses = p.lookups.filter((l) => l.outcome !== "found" && wanted.includes(l.field as Detail));
  // The vendor each detail would go to next: the first, in order, not asked lately and with lookups left.
  const next = wanted.flatMap((field) => {
    const e = enrichers.find(
      (x) => x.fields.includes(field) && x.has_key && x.used_this_month < x.monthly_limit && !p.lookups.some((l) => l.field === field && l.vendor === x.kind),
    );
    return e ? [{ field, vendor: e }] : [];
  });
  const asking = [...new Map(next.map((n) => [n.vendor.kind, n.vendor])).values()];
  const answer = ask.data?.results.find((r) => r.outcome === "failed" || r.outcome === "skipped");
  return (
    <>
      {misses.map((l) => (
        <Meta key={l.field + l.vendor}>
          {say(v.missed, { outcome: l.outcome, vendor: vendorName(l.vendor), detail: detailName(l.field), on: day(l.at), again: day(l.again_at) })}
        </Meta>
      ))}
      {sales && next.length > 0 && (
        <div className="flex flex-col gap-1">
          <div className="flex flex-wrap items-center gap-2">
            {next.map(({ field }) => (
              <ActionButton
                key={field}
                quiet
                size="xs"
                disabled={ask.isPending}
                onClick={() => ask.mutate({ params: { path: { org, id: p.id } }, body: { fields: [field] } })}
              >
                {say(v.lookUp, { detail: field === "linkedin" ? linkedIn : detailName(field) })}
              </ActionButton>
            ))}
          </div>
          <Meta>
            {say(v.asks, {
              vendors: intl.formatList(
                asking.map((e) => say(v.vendorLeft, { vendor: vendorName(e.kind), left: e.monthly_limit - e.used_this_month, limit: e.monthly_limit })),
                { type: "disjunction" },
              ),
              month: month(),
            })}
          </Meta>
        </div>
      )}
      {answer?.detail && <Meta className="text-[var(--stop)]">{answer.detail}</Meta>}
      <Failed errors={[ask.error]} />
    </>
  );
}
