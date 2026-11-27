import { type FormEvent, type ReactNode } from "react";
import { Link, useParams } from "@tanstack/react-router";
import { defineMessage, defineMessages, type MessageDescriptor, useIntl } from "react-intl";
import type { components } from "@/api";
import { $api } from "@/client";
import { ActionButton, Eyebrow, Failed, Meta, Pill, type Tone, usePageTitle, useRefresh } from "@/components/app";
import { standingText, SubscriptionUpdate, termText, thenLabel, thens } from "@/components/app/subscription";
import { Placeholders, Text } from "@/components/app/skeleton";
import { actor, stamp, today } from "@/lib/format";
import { cn } from "@/lib/utils";
import { Pager, StageTag, usePages } from "@/pages/Companies";
import { OnboardingCard } from "@/pages/Onboarding";
import { safeHref } from "@/lib/links";
import { useOfferings } from "@/lib/offerings";
import { useSession } from "@/session";

type Overview = components["schemas"]["CompanyOverview"];
type Person = components["schemas"]["CompanyPerson"];
type Pending = components["schemas"]["PendingDetail"];
type Criterion = components["schemas"]["Criterion"];
type Subscription = components["schemas"]["Subscription"];
type Agreement = components["schemas"]["Agreement"];
type Say = ReturnType<typeof useIntl>["formatMessage"];

const m = defineMessages({
  companies: { defaultMessage: "← Companies", id: "RMzu66", description: "Link from a company's page back to the list of companies" },
  notHere: { defaultMessage: "That company isn't here.", id: "6Dz4/z", description: "Heading of a company's page when there is no such company" },
  unreadable: { defaultMessage: "The company couldn't be read.", id: "GhomIh", description: "Heading of a company's page when it failed to load" },
  reload: { defaultMessage: "Reload to try again.", id: "6LtUo/", description: "Under the heading of a company's page that failed to load" },
  company: { defaultMessage: "Company", id: "sAXDZu", description: "What a screen reader calls a company's page while it loads" },
  lookingFor: {
    defaultMessage: "Looking for",
    id: "pZxxbI",
    description: "Small heading over what the organisation looks for in a company, and whether this one has it",
  },
  foundBy: { defaultMessage: "Found by", id: "lfp5kR", description: "Small heading over who found the company" },
  touches: { defaultMessage: "Touches", id: "/lC2L6", description: "Small heading over how many times the company's people have been contacted" },
  facts: { defaultMessage: "Facts", id: "T5fiHW", description: "Small heading over what research found about the company" },
  source: { defaultMessage: "source ↗", id: "9aNa1O", description: "Link to the web page a fact or suggestion came from" },
  agentNotes: {
    defaultMessage: "Agents' notes, unchecked",
    id: "EkP4JJ",
    description: "Small heading over notes AI agents left about the company, which no one has checked",
  },
  history: { defaultMessage: "History", id: "5YGxNE", description: "Small heading over what has happened with the company" },
  nothingRecorded: { defaultMessage: "Nothing recorded yet.", id: "dr8gK1", description: "When a company has no history" },
  qualify: { defaultMessage: "Qualify", id: "ALXPUa", description: "Button to mark a company as worth pursuing" },
  toReview: { defaultMessage: "To review", id: "3kx7Ri", description: "A company's verdict for an offering: still to be looked at" },
  qualified: { defaultMessage: "Qualified", id: "nXFGEy", description: "A company's verdict for an offering: worth pursuing" },
  junked: { defaultMessage: "Junked", id: "UkanjM", description: "A company's verdict for an offering: ruled out" },
  rejected: { defaultMessage: "Rejected", id: "PiieMg", description: "A company's verdict for the organisation's own offering: ruled out" },
  whereItStands: { defaultMessage: "Where it stands", id: "hD6tPx", description: "Small heading over a company's verdict for each offering" },
  yourOwn: { defaultMessage: "Your own", id: "oyGplP", description: "Name of the organisation's own offering, when its name hasn't loaded" },
  reviewForThis: { defaultMessage: "Review for this", id: "N9IPcM", description: "Button to put a company up for review for one offering" },
  putBack: { defaultMessage: "Put back", id: "3+5BFJ", description: "Button to undo ruling a company out for the organisation's own offering" },
  junk: { defaultMessage: "Junk", id: "5JeqPz", description: "Button to rule a company out for one offering" },
  undo: { defaultMessage: "Undo", id: "BsG9Is", description: "Button to undo ruling a company out for one offering" },
  notLookedAt: { defaultMessage: "Not looked at", id: "chI4zo", description: "A company with no verdict yet for an offering" },
  nameWhatYouLookFor: {
    defaultMessage: "Name what you look for",
    id: "Njrfrv",
    description: "Link to settings, for an owner, when nothing is named to look for in a company",
  },
  nothingNamed: { defaultMessage: "Nothing named yet", id: "1VB59v", description: "When the organisation has named nothing to look for in a company" },
  people: { defaultMessage: "People", id: "0jL+c4", description: "Small heading over the people at a company" },
  add: { defaultMessage: "+ add", id: "4SUazY", description: "Link to add a person at this company" },
  noOneYet: { defaultMessage: "No one yet.", id: "AJlMQH", description: "When no one at a company has been added" },
  export: { defaultMessage: "Export", id: "SO6qGo", description: "Button to download everything held about a person" },
  erase: { defaultMessage: "Erase", id: "WWGyJJ", description: "Button to delete a person and what is held about them for good" },
  aGuess: { defaultMessage: "a guess", id: "URkHiD", description: "Under a suggested email address that was worked out rather than checked" },
  accept: { defaultMessage: "Accept", id: "DGGnaQ", description: "Button to accept a suggested detail for a person" },
  dismiss: { defaultMessage: "Dismiss", id: "CjdkS3", description: "Button to turn down a suggested detail for a person" },
  onboarding: { defaultMessage: "Onboarding", id: "fh5d6n", description: "Small heading over a company's onboardings" },
  notOnboarded: { defaultMessage: "Not being onboarded.", id: "5jhLCi", description: "When a company has no onboarding" },
  startOnboarding: { defaultMessage: "Start an onboarding", id: "8inZFI", description: "Line that opens the form to start an onboarding" },
  forWhich: {
    defaultMessage: "For, if they have several",
    id: "s1zF84",
    description: "Label of the field naming which of a customer's sites or teams an onboarding is for",
  },
  forWhichExample: { defaultMessage: "e.g. the Leith site", id: "JLb18k", description: "Example in the field naming what an onboarding is for" },
  starts: { defaultMessage: "Starts", id: "GkKZVw", description: "Label of the date an onboarding or subscription starts" },
  fromYourSteps: {
    defaultMessage: "From your steps",
    id: "06TjCf",
    description: "Over the button that starts an onboarding from the organisation's own steps",
  },
  start: { defaultMessage: "Start", id: "jy0OnA", description: "Button to start an onboarding or an agreement" },
  noSteps: {
    defaultMessage: "An owner writes the onboarding steps under Settings; then one can be started here.",
    id: "OnlYXy",
    description: "When the organisation has no onboarding steps yet",
  },
  subscriptions: { defaultMessage: "Subscriptions", id: "gCBhX9", description: "Small heading over a company's subscriptions" },
  nothingRecordedSupport: {
    defaultMessage: "Nothing recorded. Once they are a customer, record what they are on.",
    id: "2zl7VT",
    description: "When a company has no subscriptions, to someone who can record one",
  },
  nothingRecordedShort: { defaultMessage: "Nothing recorded.", id: "Ho/O7f", description: "When a company has no subscriptions" },
  recordSubscription: { defaultMessage: "Record a subscription", id: "cvX7jy", description: "Line that opens the form to record a subscription" },
  whatTheyreOn: { defaultMessage: "What they're on", id: "QfMDej", description: "Label of the field naming a subscription" },
  whatTheyreOnExample: { defaultMessage: "e.g. Standard plan, 2 sites", id: "crEQSv", description: "Example in the field naming a subscription" },
  price: { defaultMessage: "Price", id: "heKwYR", description: "Label of a subscription's price" },
  priceExample: { defaultMessage: "e.g. 14,000", id: "En6Qg4", description: "Example in the field for a subscription's price" },
  per: { defaultMessage: "For", id: "Eccjcs", description: "Label of the period a subscription's price is for" },
  aYear: { defaultMessage: "a year", id: "4S4kkc", description: "Choice of the period a subscription's price is for" },
  aMonth: { defaultMessage: "a month", id: "SUlfGp", description: "Choice of the period a subscription's price is for" },
  monthsCommitted: { defaultMessage: "Months committed", id: "PtEfl7", description: "Label of how many months a subscription commits to" },
  thenLabel: { defaultMessage: "Then", id: "BQ+Tmb", description: "Label of what happens when a subscription's term ends" },
  daysNotice: { defaultMessage: "Days' notice", id: "MQ49SY", description: "Label of how many days' notice a subscription needs" },
  whenItsRight: { defaultMessage: "When it's right", id: "gGeerE", description: "Over the button that records a subscription" },
  record: { defaultMessage: "Record", id: "Kxpprw", description: "Button to record a subscription" },
  within30Days: { defaultMessage: "within 30 days", id: "5nZjIa", description: "Tag on a subscription whose next change is within 30 days" },
  scheduled: { defaultMessage: "scheduled", id: "y6Xtb8", description: "A subscription's status: not started yet" },
  active: { defaultMessage: "active", id: "DMQIfb", description: "A subscription's status: running" },
  noticeGiven: { defaultMessage: "notice given", id: "ldGibd", description: "A subscription's status: the customer gave notice" },
  ending: { defaultMessage: "ending", id: "c//kQy", description: "A subscription's status: ends when its term does" },
  ended: { defaultMessage: "ended", id: "MZV8ni", description: "A subscription's status: over" },
  noDocument: { defaultMessage: "No document yet", id: "z5Md+V", description: "When an agreement has no PDF uploaded" },
  agreements: { defaultMessage: "Agreements", id: "2ZkiXy", description: "Small heading over a company's agreements" },
  noAgreements: { defaultMessage: "None yet. Start one, then upload the contract as a PDF.", id: "gU6T/w", description: "When a company has no agreements" },
  prepare: { defaultMessage: "Prepare for signing", id: "UJC7aR", description: "Button to set out who signs an agreement and where" },
  whoSigns: { defaultMessage: "Who signs it", id: "mjBZXe", description: "Link to see who signs an agreement" },
  download: { defaultMessage: "Download", id: "Guehlt", description: "Link to download an agreement's PDF" },
  replacePdf: { defaultMessage: "Replace the PDF", id: "xGB6d2", description: "Label of the file field to replace an agreement's PDF" },
  uploadPdf: { defaultMessage: "Upload the PDF", id: "MEtZ08", description: "Label of the file field to upload an agreement's PDF" },
  remove: { defaultMessage: "Remove", id: "uNYUoZ", description: "Button to remove a draft agreement" },
  startAgreement: { defaultMessage: "Start an agreement", id: "7RJwhX", description: "Line that opens the form to start an agreement" },
  agreementExample: { defaultMessage: "e.g. Annual service agreement 2027", id: "doI3QL", description: "Example in the field naming a new agreement" },
  agreementName: { defaultMessage: "Name of the agreement", id: "vcAVds", description: "What a screen reader calls the field naming a new agreement" },
  skipped: { defaultMessage: "Skipped", id: "cL1i3L", description: "Kind of an agent's note: something it chose not to do" },
  deadEnd: { defaultMessage: "Dead end", id: "q+kFC6", description: "Kind of an agent's note: something it tried that led nowhere" },
  next: { defaultMessage: "Next", id: "JZP4gH", description: "Kind of an agent's note: what to do next" },
  none: { defaultMessage: "None.", id: "ILygsp", description: "When there are no agents' notes" },
  deleteNote: { defaultMessage: "Delete this note?", id: "Fnd5y4", description: "Asked before deleting an agent's note" },
  delete: { defaultMessage: "Delete", id: "lXKsMn", description: "Button to delete an agent's note" },
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
  fit: defineMessage<{ grade: string }>({
    defaultMessage: "fit {grade}",
    id: "tnYITz",
    description: "In the line under a company's name: how well it fits. {grade} is a letter grade, such as A",
  }),
  saidWithReason: defineMessage<{ reason: string; who: string; when: string }>({
    defaultMessage: "“{reason}” {who}, {when}",
    id: "6K2Fti",
    description: "Beside a company's verdict for an offering. {reason} is why, {who} who decided, {when} the date and time",
  }),
  saidBy: defineMessage<{ who: string; when: string }>({
    defaultMessage: "{who}, {when}",
    id: "2FVp9+",
    description: "Beside a company's verdict for an offering. {who} is who decided, {when} the date and time",
  }),
  notMet: defineMessage<{ name: string }>({
    defaultMessage: "no {name}",
    id: "se8WBZ",
    description: "Tag for something the organisation looks for that the company lacks. {name} is what it is",
  }),
  notFound: defineMessage<{ name: string }>({
    defaultMessage: "{name} not found",
    id: "zLXyuu",
    description: "Tag for something the organisation looks for that research couldn't find out. {name} is what it is",
  }),
  unchecked: defineMessage<{ name: string }>({
    defaultMessage: "{name} unchecked",
    id: "OYVII4",
    description: "Tag for something the organisation looks for that no one has checked yet. {name} is what it is",
  }),
  erase: defineMessage<{ whom: string; name: string; several: string }>({
    defaultMessage:
      "Erase {whom, select, row {everyone on this row ({several})} other {{name}}}? Their details, drafts and notes are deleted for good, along with agents' notes about this company and its tasks, and they can't be added again. Facts about the company, and the briefs and requests on tasks, that mention them aren't found for you, and nor are your own lead files: take them out there too.",
    id: "RtYccE",
    description:
      "Asked before erasing a person. {whom} is row when the row stands for several people, {several} the names it stands for, {name} the person's name otherwise",
  }),
  title: defineMessage<{ title: string }>({
    defaultMessage: "· {title}",
    id: "ADTSld",
    description: "After a person's name: their job title",
  }),
  suggestedDetail: defineMessage<{ field: string; linkedin: string }>({
    defaultMessage: "suggested {field, select, linkedin {{linkedin} profile} phone {phone number} other {{field}}}",
    id: "K2NMhm",
    description: "Over a detail suggested for a person. {field} is which detail, such as email; {linkedin} is the name LinkedIn",
  }),
  inPlaceOf: defineMessage<{ current: string }>({
    defaultMessage: "in place of {current}",
    id: "74idfF",
    description: "Under a suggested detail: the one the person holds now, which it would replace",
  }),
  notes: defineMessage<{ first: string; second: string }>({
    defaultMessage: "{first}, {second}",
    id: "9viMdO",
    description: "Two notes under a suggested detail, such as 'a guess' and 'in place of …'",
  }),
  pricePer: defineMessage<{ figure: string; per: string }>({
    defaultMessage: "{per, select, year {{figure} a year} month {{figure} a month} other {{figure} a {per}}}",
    id: "O3zy1f",
    description: "A subscription's price. {figure} is the amount with its currency, {per} the period (year or month)",
  }),
  statusBy: defineMessage<{ status: string; who: string }>({
    defaultMessage: "{status, select, draft {draft} other {{status}}} · by {who}",
    id: "cEBHnw",
    description: "Beside an agreement's name. {status} is where it stands (draft), {who} who started it",
  }),
  fileLine: defineMessage<{ name: string; kb: number; hash: string }>({
    defaultMessage: "{name} · {kb} KB · SHA-256 {hash}…",
    id: "zL5xPp",
    description: "An agreement's PDF. {name} is its file name, {kb} its size in kilobytes, {hash} the start of its SHA-256 fingerprint",
  }),
  noteBy: defineMessage<{ who: string; when: string }>({
    defaultMessage: "{who} · {when}",
    id: "6UuOrU",
    description: "Under an agent's note. {who} is the agent that left it, {when} the date and time",
  }),
  removeDraft: defineMessage<{ name: string }>({
    defaultMessage: "Remove the draft {name} and its document?",
    id: "mAMWFh",
    description: "Asked before removing a draft agreement. {name} is the agreement's name",
  }),
  keepsThePdf: defineMessage<{ brand: string }>({
    defaultMessage:
      "{brand} keeps the PDF as uploaded, with its SHA-256, and doesn't read what it says. It is not for deeds, or anything that must be witnessed or notarised. You can set out who signs it and where; sending it for signature comes later.",
    id: "GaTn8A",
    description: "Under the form to start an agreement. {brand} is the product's name, Prospector",
  }),
};

// Names are the same in every language.
const brand = "Prospector";
const linkedin = "LinkedIn";
const linkedinLink = "LinkedIn ↗";

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

// named says a key from the server in the reader's words, or as it came for one the page doesn't know.
function named(say: Say, table: Record<string, MessageDescriptor>, key: string) {
  const message = table[key];
  return message ? say(message) : key;
}

const subscriptionStatus: Record<Subscription["status"], MessageDescriptor> = {
  scheduled: m.scheduled,
  active: m.active,
  "notice given": m.noticeGiven,
  ending: m.ending,
  ended: m.ended,
};

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
  const say = useIntl().formatMessage;
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
      {say(m.companies)}
    </Link>
  );
  if (overview.error?.status === 404) {
    return (
      <>
        {back}
        <h1 className="mt-3 font-heading text-3xl font-bold tracking-tight">{say(m.notHere)}</h1>
      </>
    );
  }
  // Half a company's page would read as the whole of it, so any part failing fails the page.
  if (overview.error || history.error || notes.error) {
    return (
      <>
        {back}
        <div role="alert">
          <h1 className="mt-3 font-heading text-3xl font-bold tracking-tight">{say(m.unreadable)}</h1>
          <p className="text-sm text-[var(--stop)]">{say(m.reload)}</p>
        </div>
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
        {/* The outline's heading is hidden with the rest of it, so the page is named here meanwhile. */}
        <h1 className="sr-only">{say(m.company)}</h1>
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
            <Eyebrow>{say(m.lookingFor)}</Eyebrow>
            <LookingFor found={c.looking_for} />
          </div>
          <div className="rounded-lg bg-muted p-3">
            <Eyebrow>{say(m.foundBy)}</Eyebrow>
            <div className="mt-1 text-sm">{actor(c.found_by)}</div>
          </div>
          <div className="rounded-lg bg-muted p-3">
            <Eyebrow>{say(m.touches)}</Eyebrow>
            <div className="mt-1 font-mono text-lg">{c.touches}</div>
          </div>
        </div>
        <People c={c} />
        <Onboardings c={c} />
        <Subscriptions c={c} />
        <Agreements c={c} />
        {c.facts.length > 0 && (
          <div>
            <Eyebrow>{say(m.facts)}</Eyebrow>
            {c.facts.map((f) => (
              <div key={f.id} className="grid grid-cols-[130px_1fr_auto] items-baseline gap-3 border-b border-dashed border-border py-2 text-sm">
                <span className="font-mono text-xs text-muted-foreground">{f.field}</span>
                <span>{f.value}</span>
                {f.source_url ? (
                  <a href={safeHref(f.source_url)} target="_blank" rel="noopener" className="font-mono text-xs">
                    {say(m.source)}
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
            <Eyebrow>{say(m.agentNotes)}</Eyebrow>
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
          <Eyebrow>{say(m.history)}</Eyebrow>
          {history.data.items.length === 0 && <p className="py-2 text-sm text-muted-foreground">{say(m.nothingRecorded)}</p>}
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
  const say = useIntl().formatMessage;
  const { org, sales } = useSession();
  usePageTitle(c?.name);
  const refresh = useRefresh();
  const review = $api.useMutation("patch", "/o/{org}/api/v1/companies/{id}", { onSuccess: refresh });
  return (
    <div className="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h1 className="font-heading text-3xl font-bold tracking-tight">
          <Text value={c?.name} w="12ch" />
        </h1>
        <Meta>
          <Text value={c && [c.domain, c.country, c.segment, c.grade ? say(v.fit, { grade: c.grade }) : ""].filter((p) => p.trim()).join(" · ")} w="30ch" />
        </Meta>
      </div>
      <div className="flex min-h-8 items-center gap-2">
        <StageTag stage={c?.pipeline} />
        {c && c.stage !== "qualified" && sales && (
          <ActionButton size="sm" onClick={() => review.mutate({ params: { path: { org, id: c.id } }, body: { stage: "qualified" } })}>
            {say(m.qualify)}
          </ActionButton>
        )}
      </div>
      <Failed errors={[review.error]} />
    </div>
  );
}

const verdictPill: Record<string, [MessageDescriptor, Tone]> = {
  review: [m.toReview, "warn"],
  qualified: [m.qualified, "ok"],
  rejected: [m.junked, "stop"],
};

// WhereItStands is the company's verdict for each offering, shown once there is more
// than the organisation's own: ruled out for one, it can still be right for another.
function WhereItStands({ c }: { c: Overview }) {
  const say = useIntl().formatMessage;
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
  const by = new Map((verdicts.data?.items ?? []).map((verdict) => [verdict.offering_id, verdict]));
  const pill = (stage: string): [string, Tone] | null => {
    const p = verdictPill[stage];
    return p ? [say(p[0]), p[1]] : null;
  };
  const ownPill: [string, Tone] = c.stage === "rejected" ? [say(m.rejected), "stop"] : (pill(c.stage) ?? ["", ""]);
  return (
    <div>
      <Eyebrow>{say(m.whereItStands)}</Eyebrow>
      <div className="mt-1 flex flex-col">
        <StandingRow name={settings.data?.name ?? say(m.yourOwn)} pill={c.found_for ? null : ownPill}>
          {sales && c.found_for !== 0 && (
            <ActionButton quiet size="xs" onClick={() => judge("review")}>
              {say(m.reviewForThis)}
            </ActionButton>
          )}
          {sales && !c.found_for && c.stage === "rejected" && (
            <ActionButton quiet size="xs" onClick={() => judge("review")}>
              {say(m.putBack)}
            </ActionButton>
          )}
        </StandingRow>
        {offerings.map((o) => {
          const verdict = by.get(o.id);
          const who = verdict && actor(verdict.by);
          const when = verdict && stamp(verdict.updated_at);
          return (
            <StandingRow
              key={o.id}
              name={o.name}
              pill={verdict ? pill(verdict.stage) : null}
              said={
                verdict && (verdict.reason || verdict.by)
                  ? verdict.reason
                    ? say(v.saidWithReason, { reason: verdict.reason, who: who ?? "", when: when ?? "" })
                    : say(v.saidBy, { who: who ?? "", when: when ?? "" })
                  : ""
              }
            >
              {sales && !verdict && (
                <ActionButton quiet size="xs" onClick={() => judge("review", o.id)}>
                  {say(m.reviewForThis)}
                </ActionButton>
              )}
              {sales && verdict?.stage === "review" && (
                <ActionButton size="xs" onClick={() => judge("qualified", o.id)}>
                  {say(m.qualify)}
                </ActionButton>
              )}
              {sales && verdict && verdict.stage !== "rejected" && (
                <ActionButton quiet size="xs" onClick={() => judge("rejected", o.id)}>
                  {say(m.junk)}
                </ActionButton>
              )}
              {sales && verdict?.stage === "rejected" && (
                <ActionButton quiet size="xs" onClick={() => judge("review", o.id)}>
                  {say(m.undo)}
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
  const say = useIntl().formatMessage;
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-dashed border-border py-2 text-sm">
      <span className="min-w-[140px] font-medium">{name}</span>
      {pill ? <Pill tone={pill[1]}>{pill[0]}</Pill> : <span className="text-muted-foreground">{say(m.notLookedAt)}</span>}
      {said && <span className="min-w-0 text-xs text-muted-foreground">{said}</span>}
      <span className="ml-auto flex gap-2">{children}</span>
    </div>
  );
}

function assetPill(say: Say, a: Criterion): [string, Tone] {
  const name = a.field.replaceAll("_", " ");
  if (a.state === "met") return [name, "ok"];
  if (a.state === "not_met") return [say(v.notMet, { name }), "stop"];
  if (a.state === "not_found") return [say(v.notFound, { name }), ""];
  return [say(v.unchecked, { name }), ""];
}

function LookingFor({ found }: { found: Criterion[] }) {
  const say = useIntl().formatMessage;
  const { org, owner } = useSession();
  if (found.length === 0) {
    return (
      <div className="mt-1 text-sm text-muted-foreground">
        {owner ? (
          <Link to="/o/$org/settings" params={{ org }}>
            {say(m.nameWhatYouLookFor)}
          </Link>
        ) : (
          say(m.nothingNamed)
        )}
      </div>
    );
  }
  return (
    <div className="mt-1 flex flex-wrap gap-1.5">
      {found.map((a) => {
        const [text, tone] = assetPill(say, a);
        return (
          <Pill key={a.field} tone={tone}>
            {text}
          </Pill>
        );
      })}
    </div>
  );
}

function eraseConfirm(say: Say, p: Person) {
  return say(v.erase, { whom: p.several ? "row" : "one", name: p.name, several: p.several });
}

function People({ c }: { c: Overview }) {
  const say = useIntl().formatMessage;
  const { org, sales, owner } = useSession();
  const refresh = useRefresh();
  const exportPerson = $api.useMutation("post", "/o/{org}/api/v1/contacts/{id}/export");
  const erase = $api.useMutation("post", "/o/{org}/api/v1/contacts/{id}/erase", { onSuccess: refresh });
  return (
    <div>
      <div className="flex items-center justify-between">
        <Eyebrow>{say(m.people)}</Eyebrow>
        {sales && (
          <Link to="/o/$org/add" params={{ org }} search={{ company: c.name }} className="font-mono text-xs">
            {say(m.add)}
          </Link>
        )}
      </div>
      {c.people.length === 0 && <p className="py-2 text-sm text-muted-foreground">{say(m.noOneYet)}</p>}
      {c.people.map((p) => (
        <div key={p.id}>
          <div className="flex flex-wrap justify-between gap-2 border-b border-dashed border-border py-2.5 text-sm">
            <span>
              <b>{p.name}</b> <span className="text-muted-foreground">{p.title ? say(v.title, { title: p.title }) : ""}</span>
            </span>
            <span className="flex items-center gap-2">
              {p.linkedin && (
                <a href={safeHref(p.linkedin)} target="_blank" rel="noopener" className="font-mono text-xs">
                  {linkedinLink}
                </a>
              )}
              <Meta>{named(say, personStages, p.stage)}</Meta>
              {owner && (
                <>
                  <ActionButton
                    quiet
                    size="xs"
                    disabled={exportPerson.isPending}
                    onClick={() => exportPerson.mutate({ params: { path: { org, id: p.id } } }, { onSuccess: (held) => save(held, `person-${p.id}.json`) })}
                  >
                    {say(m.export)}
                  </ActionButton>
                  <ActionButton
                    quiet
                    size="xs"
                    disabled={erase.isPending}
                    onClick={() => window.confirm(eraseConfirm(say, p)) && erase.mutate({ params: { path: { org, id: p.id } } })}
                  >
                    {say(m.erase)}
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

function suggestedNote(say: Say, d: Pending) {
  const guess = d.email_status === "guess" ? say(m.aGuess) : "";
  const current = d.current ? say(v.inPlaceOf, { current: d.current }) : "";
  return guess && current ? say(v.notes, { first: guess, second: current }) : guess || current;
}

function SuggestedDetails({ list }: { list: Pending[] }) {
  const say = useIntl().formatMessage;
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const decide = $api.useMutation("patch", "/o/{org}/api/v1/suggestions/{id}", { onSuccess: refresh });
  return (
    <>
      {list.map((d) => {
        const note = suggestedNote(say, d);
        return (
          <div key={d.id} className="flex flex-wrap items-center gap-2 rounded-lg bg-muted p-3 text-sm">
            <span className="min-w-0 flex-[1_1_220px]">
              <span className="font-mono text-xs text-muted-foreground">{say(v.suggestedDetail, { field: d.field, linkedin })}</span>
              <span className="block break-all">{d.value}</span>
              {note && <span className="block text-muted-foreground">{note}</span>}
              <span className="flex flex-wrap items-center gap-2">
                <span className="font-mono text-xs text-mark">{actor(d.by)}</span>
                {d.source_url && (
                  <a href={safeHref(d.source_url)} target="_blank" rel="noopener" className="font-mono text-xs">
                    {say(m.source)}
                  </a>
                )}
              </span>
            </span>
            {sales && (
              <>
                <ActionButton size="xs" onClick={() => decide.mutate({ params: { path: { org, id: d.id } }, body: { decision: "accepted" } })}>
                  {say(m.accept)}
                </ActionButton>
                <ActionButton quiet size="xs" onClick={() => decide.mutate({ params: { path: { org, id: d.id } }, body: { decision: "dismissed" } })}>
                  {say(m.dismiss)}
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
  const say = useIntl().formatMessage;
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
      <Eyebrow>{say(m.onboarding)}</Eyebrow>
      {c.onboardings.length === 0 ? (
        <p className="py-2 text-sm text-muted-foreground">{say(m.notOnboarded)}</p>
      ) : (
        <div className="mt-1 flex flex-col gap-3">
          {c.onboardings.map((o) => (
            <OnboardingCard key={o.id} o={o} day={day} named={false} />
          ))}
        </div>
      )}
      {c.planned_steps > 0 && (owner || sales || support) ? (
        <Disclosure label={say(m.startOnboarding)}>
          <form onSubmit={submit} className="mt-2 flex flex-wrap items-start gap-2">
            <label className="flex min-w-[220px] flex-1 flex-col gap-1">
              <Eyebrow>{say(m.forWhich)}</Eyebrow>
              <input name="name" placeholder={say(m.forWhichExample)} className={field} />
            </label>
            <label className="flex flex-col gap-1">
              <Eyebrow>{say(m.starts)}</Eyebrow>
              <input name="start" type="date" required defaultValue={day} className={field} />
            </label>
            <span className="flex flex-col gap-1">
              <Eyebrow>{say(m.fromYourSteps)}</Eyebrow>
              <ActionButton type="submit" size="sm" className="h-9" disabled={start.isPending}>
                {say(m.start)}
              </ActionButton>
            </span>
            <Failed errors={[start.error]} />
          </form>
        </Disclosure>
      ) : (
        c.planned_steps === 0 && <Meta>{say(m.noSteps)}</Meta>
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
  const say = useIntl().formatMessage;
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
      <Eyebrow>{say(m.subscriptions)}</Eyebrow>
      {c.subscriptions.length === 0 ? (
        <p className="py-2 text-sm text-muted-foreground">{support ? say(m.nothingRecordedSupport) : say(m.nothingRecordedShort)}</p>
      ) : (
        <div className="mt-1 overflow-hidden rounded-lg border border-border">
          {c.subscriptions.map((s) => (
            <SubscriptionLine key={s.id} s={s} />
          ))}
        </div>
      )}
      {support && (
        <Disclosure label={say(m.recordSubscription)}>
          <form onSubmit={submit} className="mt-2 flex flex-wrap items-start gap-2">
            <label className="flex min-w-[220px] flex-1 flex-col gap-1">
              <Eyebrow>{say(m.whatTheyreOn)}</Eyebrow>
              <input name="name" required placeholder={say(m.whatTheyreOnExample)} className={field} />
            </label>
            <label className="flex flex-col gap-1">
              <Eyebrow>{say(m.price)}</Eyebrow>
              <input name="amount" placeholder={say(m.priceExample)} className={cn(field, "w-44")} />
            </label>
            <label className="flex flex-col gap-1">
              <Eyebrow>{say(m.per)}</Eyebrow>
              <select name="per" className={select}>
                <option value="year">{say(m.aYear)}</option>
                <option value="month">{say(m.aMonth)}</option>
              </select>
            </label>
            <label className="flex flex-col gap-1">
              <Eyebrow>{say(m.starts)}</Eyebrow>
              <input name="start" type="date" required defaultValue={day} className={field} />
            </label>
            <label className="flex flex-col gap-1">
              <Eyebrow>{say(m.monthsCommitted)}</Eyebrow>
              <input name="term_months" type="number" min={0} max={120} defaultValue={12} className={cn(field, "w-24")} />
            </label>
            <label className="flex flex-col gap-1">
              <Eyebrow>{say(m.thenLabel)}</Eyebrow>
              <select name="then" className={select}>
                {thens.map((t) => (
                  <option key={t} value={t}>
                    {thenLabel(t)}
                  </option>
                ))}
              </select>
            </label>
            <label className="flex flex-col gap-1">
              <Eyebrow>{say(m.daysNotice)}</Eyebrow>
              <input name="notice_days" type="number" min={0} max={730} defaultValue={30} className={cn(field, "w-24")} />
            </label>
            <span className="flex flex-col gap-1">
              <Eyebrow>{say(m.whenItsRight)}</Eyebrow>
              <ActionButton type="submit" size="sm" className="h-9" disabled={add.isPending}>
                {say(m.record)}
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
  const say = useIntl().formatMessage;
  const { support } = useSession();
  const price = s.figure ? say(v.pricePer, { figure: s.figure, per: s.per }) : "";
  return (
    <div className="border-b border-border px-4 py-3 text-sm last:border-0">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <span className="min-w-0">
          <b>{s.name}</b>
          <span className="block font-mono text-xs text-muted-foreground">{[price, termText(s), standingText(s)].filter((p) => p.trim()).join(" · ")}</span>
        </span>
        <span className="flex items-center gap-2">
          {s.soon && <Pill tone="warn">{say(m.within30Days)}</Pill>}
          <Pill tone={subscriptionTone(s)}>{say(subscriptionStatus[s.status])}</Pill>
        </span>
      </div>
      {support && <SubscriptionUpdate s={s} />}
    </div>
  );
}

function fileLine(say: Say, a: Agreement) {
  if (!a.file_hash) return say(m.noDocument);
  return say(v.fileLine, { name: a.file_name, kb: Math.floor((a.file_size + 1023) / 1024), hash: a.file_hash.slice(0, 12) });
}

function Agreements({ c }: { c: Overview }) {
  const say = useIntl().formatMessage;
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const create = $api.useMutation("post", "/o/{org}/api/v1/agreements", { onSuccess: refresh });
  const upload = $api.useMutation("put", "/o/{org}/api/v1/agreements/{id}/document", { onSuccess: refresh });
  const remove = $api.useMutation("delete", "/o/{org}/api/v1/agreements/{id}", { onSuccess: refresh });
  return (
    <div>
      <Eyebrow>{say(m.agreements)}</Eyebrow>
      {c.agreements.length === 0 && <p className="py-2 text-sm text-muted-foreground">{say(m.noAgreements)}</p>}
      {c.agreements.map((a) => (
        <div key={a.id} className="mt-1 flex flex-col gap-2 rounded-lg border border-border p-3 text-sm">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <b>{a.name}</b>
            <span className="font-mono text-xs text-muted-foreground">{say(v.statusBy, { status: a.status, who: actor(a.created_by) })}</span>
          </div>
          <Meta>{fileLine(say, a)}</Meta>
          <div className="flex flex-wrap items-center gap-2">
            {a.file_hash && sales && a.status === "draft" ? (
              <ActionButton size="xs" asChild>
                <Link to="/o/$org/agreements/$id/prepare" params={{ org, id: a.id }}>
                  {say(m.prepare)}
                </Link>
              </ActionButton>
            ) : (
              a.file_hash && (
                <Link to="/o/$org/agreements/$id/prepare" params={{ org, id: a.id }} className="font-mono text-xs text-mark underline">
                  {say(m.whoSigns)}
                </Link>
              )
            )}
            {a.file_hash && (
              <a href={`/o/${org}/api/v1/agreements/${a.id}/document`} className="font-mono text-xs text-mark underline">
                {say(m.download)}
              </a>
            )}
            {sales && a.status === "draft" && (
              <>
                <label className="font-mono text-xs text-muted-foreground">
                  {a.file_hash ? say(m.replacePdf) : say(m.uploadPdf)}
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
                  onClick={() => window.confirm(say(v.removeDraft, { name: a.name })) && remove.mutate({ params: { path: { org, id: a.id } } })}
                >
                  {say(m.remove)}
                </ActionButton>
              </>
            )}
          </div>
        </div>
      ))}
      <Failed errors={[upload.error, remove.error]} />
      {sales && (
        <Disclosure label={say(m.startAgreement)}>
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
              placeholder={say(m.agreementExample)}
              aria-label={say(m.agreementName)}
              className={cn(field, "min-w-[220px] flex-1")}
            />
            <ActionButton quiet size="sm" type="submit" disabled={create.isPending}>
              {say(m.start)}
            </ActionButton>
            <Failed errors={[create.error]} />
          </form>
          <Meta>{say(v.keepsThePdf, { brand })}</Meta>
        </Disclosure>
      )}
    </div>
  );
}

const noteKinds: Record<string, MessageDescriptor> = { skipped: m.skipped, dead_end: m.deadEnd, next: m.next };

function Notes({ notes }: { notes: components["schemas"]["Note"][] }) {
  const say = useIntl().formatMessage;
  const { org, sales } = useSession();
  const refresh = useRefresh();
  const remove = $api.useMutation("delete", "/o/{org}/api/v1/notes/{id}", { onSuccess: refresh });
  return (
    <>
      {notes.length === 0 && <p className="text-sm text-muted-foreground">{say(m.none)}</p>}
      {notes.map((n) => (
        <div key={n.id} className="flex flex-col gap-1.5 border-b border-dashed border-border py-2.5 text-sm last:border-0">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="flex flex-wrap items-center gap-2">
              <Pill tone="agent">{named(say, noteKinds, n.kind)}</Pill>
            </span>
            {sales && (
              <ActionButton quiet size="xs" onClick={() => window.confirm(say(m.deleteNote)) && remove.mutate({ params: { path: { org, id: n.id } } })}>
                {say(m.delete)}
              </ActionButton>
            )}
          </div>
          <p className="whitespace-pre-wrap">{n.text}</p>
          <Meta>{say(v.noteBy, { who: actor(n.actor), when: stamp(n.created_at) })}</Meta>
        </div>
      ))}
      <Failed errors={[remove.error]} />
    </>
  );
}
