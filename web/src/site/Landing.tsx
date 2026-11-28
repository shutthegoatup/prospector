import { useState, type FormEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { defineMessage, defineMessages, FormattedMessage, useIntl } from "react-intl";
import { Brand, Eyebrow, Footer, Panel, SectionHead, Shot, SiteButton, Tag, Terminal, toneFill, Wrap, type Tone } from "@/components/app/site";
import { Button } from "@/components/ui/button";
import { CardDescription } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

const repoURL = "https://github.com/shutthegoatup/prospector";

const perUser = 5;
const freeUsers = 5;
export const pricing = `Free for up to ${freeUsers} users, then £${perUser} a user a month.`;

export const head = defineMessages({
  title: {
    defaultMessage: "Prospector · A few hundred accounts, one queue a day",
    id: "52CSdy",
    description: "The front page's title, in the browser tab and search results. Prospector is the product's name",
  },
  description: {
    defaultMessage:
      "A prospecting and customer workbench for small teams selling to named accounts: research, deals, agreements, subscriptions and onboarding on one company record. Agents research and draft; you decide what they may send.",
    id: "c21Z0e",
    description: "The front page's description for search results and link previews",
  },
});

const m = defineMessages({
  siteNav: { defaultMessage: "Site", id: "lxa+wR", description: "What a screen reader calls the front page's top navigation" },
  howLink: { defaultMessage: "How it works", id: "eu18cr", description: "Front page navigation link to the section on how Prospector works" },
  customersLink: { defaultMessage: "Customers", id: "MNkSRl", description: "Front page navigation link to the section on working with customers" },
  agentsLink: { defaultMessage: "Agents", id: "u91Ufh", description: "Front page navigation link to the section on AI agents and the API" },
  pricingLink: { defaultMessage: "Pricing", id: "pcDz+Y", description: "Front page navigation link to the pricing section" },
  signIn: { defaultMessage: "Sign in", id: "qydMvc", description: "Front page link to the sign-in page" },
  eyebrow: { defaultMessage: "Prospects and customers for small teams", id: "8xJZ/y", description: "Small heading above the front page's main headline" },
  line1: { defaultMessage: "A few hundred accounts.", id: "SwuXVA", description: "First line of the front page's main headline" },
  line2: { defaultMessage: "One queue a day.", id: "1tBHRP", description: "Second line of the front page's main headline" },
  lede: {
    defaultMessage:
      "Prospector is a workbench for teams selling to named accounts, from the first fact about a company to the agreement it signs. Agents research and draft. People qualify, decide and send.",
    id: "1WOqSh",
    description: "The front page's opening paragraph, under the headline",
  },
  seeHow: { defaultMessage: "See how it works", id: "nW5AbG", description: "Front page button that scrolls to the section on how it works" },
  todayAlt: {
    defaultMessage: "The Today page: everything due, best first, with a Do now list beside them",
    id: "+YJ41H",
    description: "Alt text of a screenshot of the app's Today page. 'Do now' is the name of a list on that page",
  },
  queueTitle: { defaultMessage: "queue a day", id: "Er8yGs", description: "Beside a large number 1 on the front page: one queue a day" },
  queueText: { defaultMessage: "across LinkedIn, email and phone.", id: "sUzOXT", description: "Under 'queue a day' on the front page" },
  sentTitle: {
    defaultMessage: "messages sent by agents",
    id: "pnotm2",
    description: "Beside a large number 0 on the front page: no messages are sent by agents",
  },
  sentText: { defaultMessage: "They draft. Only people send.", id: "S5tChX", description: "Under 'messages sent by agents' on the front page" },
  sourceTitle: {
    defaultMessage: "source link per fact",
    id: "iuz4f9",
    description: "Beside a large number 1 on the front page: each fact has one source link",
  },
  sourceText: { defaultMessage: "An agent's fact without one is refused.", id: "FCW3rl", description: "Under 'source link per fact' on the front page" },
  howLabel: { defaultMessage: "How it works", id: "Tyec9L", description: "Small heading of the front page's section on how Prospector works" },
  howTitle: {
    defaultMessage: "From a company name to a signed deal, in three pages.",
    id: "iq/mRr",
    description: "Heading of the front page's section on how it works",
  },
  howIntro: {
    defaultMessage: "Research fills the list. Today works it. Conversations closes it. The screenshots show invented companies and people.",
    id: "m1Gk6c",
    description: "Intro of the section on how it works. Research, Today and Conversations are the names of pages in the app",
  },
  researchTag: {
    defaultMessage: "01 · Research",
    id: "Jmexbw",
    description: "Tag of the first step on the front page. Research is the name of a page in the app",
  },
  researchTitle: {
    defaultMessage: "Facts with the link they came from",
    id: "J7RWLh",
    description: "Heading of the front page's step about the Research page",
  },
  researchAlt: {
    defaultMessage: "Research: a brief for an agent at the top, its search, and two companies it found, each with sourced facts",
    id: "s/s0Nw",
    description: "Alt text of a screenshot of the app's Research page",
  },
  researchText1: {
    defaultMessage:
      "Say who you're looking for and send an agent after them. Ask for about so many companies and watch how far the search has got. What it finds that you already have is left out, and nothing reaches outreach until someone qualifies it.",
    id: "pc1G6v",
    description: "First paragraph of the front page's step about the Research page",
  },
  researchText2: {
    defaultMessage:
      "Sell more than one thing? Each offering gets its own look-for list and queue, so a company that is wrong for one can still be right for another. Every fact keeps its source.",
    id: "fxtUXV",
    description: "Second paragraph of the front page's step about the Research page",
  },
  todayTag: { defaultMessage: "02 · Today", id: "XH8vj/", description: "Tag of the second step on the front page. Today is the name of a page in the app" },
  todayTitle: { defaultMessage: "One queue, best first", id: "XXpPYS", description: "Heading of the front page's step about the Today page" },
  linkedinAlt: {
    defaultMessage: "The LinkedIn queue: the connection note ready to copy, with keyboard shortcuts",
    id: "O+HnZh",
    description: "Alt text of a screenshot of the app's LinkedIn queue",
  },
  todayOrder: {
    defaultMessage: "Everything due across LinkedIn, email and phone, in this order:",
    id: "mhUdf/",
    description: "Before a numbered list of what the Today page shows first",
  },
  orderConversations: {
    defaultMessage: "Conversations that need a next step",
    id: "nv53Jo",
    description: "First in the list of what the Today page shows first",
  },
  orderCalls: {
    defaultMessage: "Calls where it's office hours for the contact",
    id: "cw0EMg",
    description: "Second in the list of what the Today page shows first",
  },
  orderMessages: {
    defaultMessage: "LinkedIn messages, emails and follow-ups",
    id: "Anm6Lz",
    description: "Third in the list of what the Today page shows first",
  },
  orderAdds: {
    defaultMessage: "LinkedIn adds",
    id: "9wLRa3",
    description: "Fourth in the list of what the Today page shows first: connection requests to send on LinkedIn",
  },
  keyOpen: { defaultMessage: "open and copy the note", id: "P37KKy", description: "What the keyboard shortcut o does in the LinkedIn queue" },
  keyAdded: { defaultMessage: "added", id: "FS8zJF", description: "What the keyboard shortcut a does in the LinkedIn queue: marks the person as added" },
  keySkip: { defaultMessage: "skip", id: "o09nP+", description: "What the keyboard shortcut s does in the LinkedIn queue" },
  conversationsTag: {
    defaultMessage: "03 · Conversations",
    id: "kZmmkX",
    description: "Tag of the third step on the front page. Conversations is the name of a page in the app",
  },
  conversationsTitle: { defaultMessage: "Replies become deals", id: "8MVInx", description: "Heading of the front page's step about the Conversations page" },
  conversationsAlt: {
    defaultMessage: "Conversations: a board from replied to won",
    id: "v0HOOZ",
    description: "Alt text of a screenshot of the app's Conversations board",
  },
  conversationsText1: {
    defaultMessage: "Replies move across a board: replied, meeting, proposal, won, lost.",
    id: "lD82WV",
    description: "On the front page, the stages of the Conversations board",
  },
  conversationsText2: {
    defaultMessage: "Each one needs a next step with a date. Any without one, or due today, come back to the top of Today.",
    id: "aIfCtj",
    description: "On the front page, about the Conversations board. Today is the name of a page in the app",
  },
  wonLabel: { defaultMessage: "After it's won", id: "ba8ibw", description: "Small heading of the front page's section on customers, after a deal is won" },
  wonTitle: { defaultMessage: "The customer stays on the same record.", id: "GaKi1r", description: "Heading of the front page's section on customers" },
  wonIntro: {
    defaultMessage:
      "Deals, the contract, what they're on, getting them live, what they write in about and what you write to them, all on the company you prospected. Each has its operations in the published API.",
    id: "2IlrPu",
    description: "Intro of the front page's section on customers",
  },
  dealsTitle: { defaultMessage: "Deals with a value", id: "Z8g7Hx", description: "Heading of a feature card on the front page" },
  dealsText: {
    defaultMessage: "Each conversation carries what it's worth, who has it and when it should close. Start a second one for a renewal or an expansion.",
    id: "xcOSPc",
    description: "Text of the front page's card about deals",
  },
  agreementsTitle: { defaultMessage: "Agreements, ready to sign", id: "em6+Ov", description: "Heading of a feature card on the front page" },
  agreementsText: {
    defaultMessage:
      "Upload the contract as a PDF. It is kept exactly as uploaded and checked against its hash on every download. Choose who signs and in what order, and drag their fields onto the pages. Sending for signature comes next.",
    id: "j9C4ax",
    description: "Text of the front page's card about agreements",
  },
  subscriptionsTitle: { defaultMessage: "Subscriptions and their notice", id: "XvcPD6", description: "Heading of a feature card on the front page" },
  subscriptionsText: {
    defaultMessage:
      "What each customer is on, for how long, and the notice it needs. Today flags one a month before its date, and a renewal deal starts from it.",
    id: "P6XntI",
    description: "Text of the front page's card about subscriptions. Today is the name of a page in the app",
  },
  onboardingTitle: { defaultMessage: "Onboarding to go-live", id: "xbe2Sy", description: "Heading of a feature card on the front page" },
  onboardingText: {
    defaultMessage:
      "Your steps and theirs, dated from the start. The customer gets a page of their own steps from an emailed link, no password, and marks theirs done. Late steps come back to Today.",
    id: "c8CUG4",
    description: "Text of the front page's card about onboarding. Today is the name of a page in the app",
  },
  enquiriesTitle: { defaultMessage: "Enquiries answered", id: "0qqw/u", description: "Heading of a feature card on the front page" },
  enquiriesText: {
    defaultMessage:
      "Site forms and your product's sign-ups arrive open, to be given an owner. Reply by email from the card, and paste their answer into the thread.",
    id: "Fmb078",
    description: "Text of the front page's card about enquiries",
  },
  campaignsTitle: { defaultMessage: "Campaigns to people you know", id: "L6AqVU", description: "Heading of a feature card on the front page" },
  campaignsText: {
    defaultMessage:
      "Write once to everyone in conversation or at a current customer, never to cold prospects. It goes through your own mail server with your postal address and a one-click unsubscribe, and nobody is mailed twice. A service notice, such as a price change, reaches customers who unsubscribed from marketing. With Postmark, an address that bounces or calls a mail spam gets no more.",
    id: "ZtFBeW",
    description: "Text of the front page's card about campaigns. Postmark is the name of a mail service",
  },
  agentsLabel: { defaultMessage: "Agents and API", id: "8JCXDc", description: "Small heading of the front page's section on AI agents and the API" },
  agentsTitle: {
    defaultMessage: "Agents do the reading. You decide what they may send.",
    id: "Dv39bL",
    description: "Heading of the front page's section on agents",
  },
  agentsIntro: {
    defaultMessage:
      "Each organisation has an MCP endpoint and a REST API with scoped keys. Connect Claude or your own agent in one command, and decide what each key may do. Or let Prospector run the agents itself, through Claude, any OpenAI-compatible endpoint or a model you host, with free search from your own SearXNG. Each agent's card says whether it is ready, and why not.",
    id: "Yuv5oH",
    description: "Intro of the front page's section on agents. MCP, REST, Claude, OpenAI and SearXNG are names to keep",
  },
  connectClaude: {
    defaultMessage: "Connect Claude Code",
    id: "tNT4bP",
    description: "Title over shell commands that connect Claude Code (a product's name) to Prospector",
  },
  agentsAlt: {
    defaultMessage: "Agents: each of Prospector's agents on a card saying whether it is ready, with its model, search, tools and fallback",
    id: "l2ksjH",
    description: "Alt text of a screenshot of the app's Agents page",
  },
  keyCan: { defaultMessage: "A key can", id: "/vu1Zq", description: "Heading of the list of what an API key can be allowed to do" },
  canRead: {
    defaultMessage: "Search companies and people, and read what's due and the tasks people wrote",
    id: "MPYBMP",
    description: "What an API key with the read scope can do",
  },
  canResearch: {
    defaultMessage: "Add companies, facts and suggested contacts, each with a source URL",
    id: "ITQdyJ",
    description: "What an API key with the research:write scope can do",
  },
  canDrafts: {
    defaultMessage: "Draft notes, emails and follow-ups, shown as the agent's",
    id: "Zie3DC",
    description: "What an API key with the drafts:write scope can do",
  },
  canOutcomes: {
    defaultMessage: "Record an outcome, shown as the agent's in the history",
    id: "dHfUeW",
    description: "What an API key with the outcomes:write scope can do",
  },
  canPipeline: {
    defaultMessage: "Value deals, work a renewal or expansion through its stages, and say where answering an enquiry stands",
    id: "eU5tL+",
    description: "What an API key with the pipeline:write scope can do",
  },
  canTriage: {
    defaultMessage: "Say where answering an enquiry stands, without deals",
    id: "Y281lY",
    description: "What an API key with the enquiries:triage scope can do",
  },
  canCustomers: {
    defaultMessage: "Record subscriptions and work customers' onboarding",
    id: "IHsWKh",
    description: "What an API key with the customers:write scope can do",
  },
  canAgreements: {
    defaultMessage: "Set out who signs an agreement and place their fields",
    id: "zDd24Z",
    description: "What an API key with the agreements:write scope can do",
  },
  canReply: { defaultMessage: "Email a reply to someone who wrote in", id: "Amq20g", description: "What an API key with the enquiries:reply scope can do" },
  canInvite: {
    defaultMessage: "Email a customer the link to their onboarding steps",
    id: "Z1e1wV",
    description: "What an API key with the customers:invite scope can do",
  },
  canCampaigns: {
    defaultMessage: "Send a campaign to everyone in conversation or at a customer, through your own mail server, each with an unsubscribe link",
    id: "JEshDK",
    description: "What an API key with the campaigns:send scope can do",
  },
  keyCannot: { defaultMessage: "No key can", id: "zHN//b", description: "Heading of the list of what no API key can do" },
  cannotSign: { defaultMessage: "Sign an agreement", id: "tAwscG", description: "In the list of what no API key can do" },
  cannotMail: {
    defaultMessage: "Send mail without a scope that says so, or to anyone on your never-contact list or erased on request",
    id: "wIwV2t",
    description: "In the list of what no API key can do",
  },
  cannotSee: { defaultMessage: "See another organisation", id: "vDAK3/", description: "In the list of what no API key can do" },
  cannotAdd: { defaultMessage: "Add anyone on your never-contact list", id: "vwsqU/", description: "In the list of what no API key can do" },
  teamLabel: { defaultMessage: "Built for a small team", id: "z7jFrM", description: "Small heading of the front page's section on details for small teams" },
  teamTitle: {
    defaultMessage: "The details that keep a list clean.",
    id: "Oy5xR9",
    description: "Heading of the front page's section on details for small teams",
  },
  neverTitle: { defaultMessage: "A never-contact list that holds", id: "ftqOQi", description: "Heading of a feature card on the front page" },
  neverText: {
    defaultMessage:
      "Domains, email addresses and LinkedIn profiles. A company on a listed domain stays on file but out of review and outreach. It applies to agents, imports and edits alike.",
    id: "JxKi4r",
    description: "Text of the front page's card about the never-contact list",
  },
  callsTitle: { defaultMessage: "Calls at a sensible hour", id: "S4FfM/", description: "Heading of a feature card on the front page" },
  callsText: {
    defaultMessage: "Office hours come from the contact's phone number, so Zurich gets called when Zurich is at work.",
    id: "ad/3Ql",
    description: "Text of the front page's card about calling at office hours. Zurich is an example city",
  },
  historyTitle: { defaultMessage: "The full history", id: "N3Nc7T", description: "Heading of a feature card on the front page" },
  historyText: {
    defaultMessage: "Every company shows who did what and when, people and agents alike.",
    id: "hGV56V",
    description: "Text of the front page's card about history",
  },
  orgsTitle: { defaultMessage: "Several organisations, one login", id: "47dYdS", description: "Heading of a feature card on the front page" },
  orgsText: {
    defaultMessage:
      "Each has its own prospects, wording, team and keys. None can see another's. Sign in with a password, Google, Microsoft, LinkedIn or your organisation's own single sign-on.",
    id: "gczrAX",
    description: "Text of the front page's card about organisations and signing in",
  },
  filesTitle: { defaultMessage: "Bring your own files", id: "QpPC7N", description: "Heading of a feature card on the front page" },
  filesText: {
    defaultMessage: "Import companies from CSV and drafts from markdown. Re-importing never clears a date or an edit made in the app.",
    id: "oMCXeP",
    description: "Text of the front page's card about importing",
  },
  dataTitle: { defaultMessage: "Your data stays yours", id: "S4Idq2", description: "Heading of a feature card on the front page" },
  dataText: {
    defaultMessage:
      "Host it yourself on SQLite or Postgres, or let us. An owner can download everything the organisation holds as one file, and erase a person who asks to be forgotten.",
    id: "v/6xeR",
    description: "Text of the front page's card about owning your data",
  },
  pricingLabel: { defaultMessage: "Pricing", id: "1f+u6p", description: "Small heading of the front page's pricing section" },
  englishHolds: {
    defaultMessage: "Prices and terms are given in English, and the English is the version that holds.",
    id: "M0bQxp",
    description:
      "Under the pricing, shown in every language but English: the plans and their terms are deliberately left in English, and the English text is the binding one",
  },
  noLockIn: { defaultMessage: "No lock-in.", id: "63zNMm", description: "Heading of a note under the pricing: you are never tied to Prospector" },
  selfHostLabel: { defaultMessage: "Rather run it yourself?", id: "rCr15Q", description: "Small heading of the front page's section on self-hosting" },
  selfHostTitle: { defaultMessage: "Self-host in four commands.", id: "LG+8gx", description: "Heading of the front page's section on self-hosting" },
  selfHostText: {
    defaultMessage:
      "Prospector is one program for Linux or macOS, on SQLite or Postgres, with every feature. Download it, name your organisation, add yourself and start working the list. Your server, your mail, your models.",
    id: "bDmlI2",
    description: "Text of the front page's section on self-hosting",
  },
  download: { defaultMessage: "Download Prospector", id: "hLm9qE", description: "Button to download Prospector's latest release from GitHub" },
  docs: { defaultMessage: "Read the docs", id: "gzG4sZ", description: "Button to Prospector's documentation on GitHub" },
  terminal: { defaultMessage: "Terminal", id: "YS8/Zq", description: "Title over example shell commands" },
  blurb: {
    defaultMessage: "Prospects and customers for small teams with a named list.",
    id: "ys5NYV",
    description: "Line in the front page's footer saying what Prospector is",
  },
  selfHostLink: { defaultMessage: "Self-host", id: "mrqp5v", description: "Footer link to the section on running Prospector yourself" },
  releasesLink: { defaultMessage: "Releases", id: "V0EDl0", description: "Footer link to Prospector's releases on GitHub" },
});

const v = {
  freeFor: defineMessage<{ n: number }>({
    defaultMessage: "Free for {n, plural, one {# user} other {# users}}",
    id: "hBspLb",
    description: "Front page button that scrolls to the pricing. {n} is how many users are free",
  }),
  callTime: defineMessage<{ when: string }>({
    defaultMessage: "{when} in London: good time to call",
    id: "ubXCYi",
    description: "A badge on the front page's screenshot, as the app shows it. {when} is a weekday and time, such as Tue 16:39",
  }),
  agentChip: defineMessage<{ name: string }>({
    defaultMessage: "agent · {name}",
    id: "Nk501a",
    description: "A badge on the front page's screenshot, naming who did something: an agent, and {name} is its name",
  }),
};

const rise = "animate-in fade-in slide-in-from-bottom-3 fill-mode-both duration-600 motion-reduce:animate-none";
const arrow = "↗";
const prompt = "$ ";
const addressBar = "prospector.example.com/o/example-co/today";
const serving = "# http://127.0.0.1:8766";
const github = "GitHub";
const pound = "£";
const scopes = { settings: "settings:write", team: "team:write", mail: "mail:configure", sso: "sso:configure", agents: "agents:configure" };
const callAt = new Date("2026-03-03T16:39:00Z");

const sourced = (chunks: ReactNode[]) => (
  <em className="-ml-[0.06em] inline-block -rotate-[1.5deg] rounded-lg bg-ink px-[0.18em] not-italic text-primary">{chunks}</em>
);
const startLink = (chunks: ReactNode[]) => (
  <a className="underline decoration-primary underline-offset-[3px]" href="#start">
    {chunks}
  </a>
);

const keyCan: [keyof typeof m, string][] = [
  ["canRead", "read"],
  ["canResearch", "research:write"],
  ["canDrafts", "drafts:write"],
  ["canOutcomes", "outcomes:write"],
  ["canPipeline", "pipeline:write"],
  ["canTriage", "enquiries:triage"],
  ["canCustomers", "customers:write"],
  ["canAgreements", "agreements:write"],
  ["canReply", "enquiries:reply"],
  ["canInvite", "customers:invite"],
  ["canCampaigns", "campaigns:send"],
];

const connectLines = {
  add: "claude mcp add --transport http prospector \\",
  url: "https://<host>/o/<org>/mcp \\",
  header: "--header ",
  bearer: '"Authorization: Bearer $PROSPECTOR_KEY"',
};

// Example arguments in the commands are code, so they stay as written.
const selfHostLines: [string, string?][] = [
  ["prospector org add acme ", '"Acme"'],
  ["prospector user add you@example.com ", '"Your Name"'],
  ["prospector member add acme you@example.com owner"],
  ["prospector serve"],
];

// Landing is the front page, drawn when the site is built. It has no enquiry form:
// main.tsx mounts one in its place where the server has somewhere for it to send.
export function Landing() {
  const intl = useIntl();
  const say = intl.formatMessage;
  return (
    <>
      <header className="front-backdrop relative">
        <img className="pointer-events-none absolute inset-0 h-full w-full object-cover opacity-20" src="/assets/img/contours.svg" alt="" />
        <Wrap className="relative">
          <nav className="relative z-10 flex items-center justify-between gap-7 py-5" aria-label={say(m.siteNav)}>
            <Brand />
            <div id="site-links" className="ml-auto hidden gap-6 text-[15px] font-medium sm:flex [&_a]:opacity-75 [&_a:hover]:opacity-100">
              <a href="#loop">{say(m.howLink)}</a>
              <a href="#customers">{say(m.customersLink)}</a>
              <a href="#agents">{say(m.agentsLink)}</a>
              <a href="#pricing">{say(m.pricingLink)}</a>
            </div>
            <SiteButton look="outline" small href="/login">
              {say(m.signIn)}
            </SiteButton>
          </nav>
          <div className="grid items-center gap-12 pt-8 pb-[88px] lg:grid-cols-[minmax(0,620px)_minmax(0,1fr)] lg:pt-14 lg:pb-[104px]">
            <div>
              <Eyebrow className={cn("text-[#4a3d1c]", rise)}>{say(m.eyebrow)}</Eyebrow>
              <h1
                className={cn(
                  "mt-5 mb-6 font-heading text-[clamp(38px,4.6vw,56px)] font-bold leading-[1.06] tracking-[-0.025em] [&>span]:block",
                  rise,
                  "delay-75",
                )}
              >
                <span>{say(m.line1)}</span>
                <span>{say(m.line2)}</span>
                <span>
                  <FormattedMessage
                    defaultMessage="Every fact <em>sourced.</em>"
                    id="CksL2a"
                    description="Third line of the front page's main headline; <em> marks the highlighted word"
                    values={{ em: sourced }}
                  />
                </span>
              </h1>
              <p className={cn("mb-8 max-w-[34em] text-[19px] text-[#3a3122]", rise, "delay-150")}>{say(m.lede)}</p>
              <div className={cn("flex flex-wrap gap-3", rise, "delay-200")}>
                <SiteButton big href="#pricing">
                  {say(v.freeFor, { n: freeUsers })}
                </SiteButton>
                <SiteButton look="outline" big href="#loop">
                  {say(m.seeHow)}
                </SiteButton>
              </div>
            </div>
            <div className="relative min-w-0 [perspective:1800px]">
              <Panel className="overflow-hidden bg-sidebar! shadow-[-30px_50px_90px_rgba(92,52,0,0.5)]! lg:w-[900px] lg:origin-left lg:[transform:rotateY(-13deg)_rotateX(5deg)_rotateZ(1deg)]">
                <div className="flex items-center gap-[7px] bg-ink px-3.5 py-[11px]">
                  <i className="size-2.5 rounded-full bg-chart-3" />
                  <i className="size-2.5 rounded-full bg-primary" />
                  <i className="size-2.5 rounded-full bg-chart-2" />
                  <span className="ml-3 min-w-0 truncate font-mono text-xs text-[#b9b0a0]">{addressBar}</span>
                </div>
                <img className="block h-auto w-full" src="/assets/img/today.webp" width={1440} height={900} alt={say(m.todayAlt)} />
              </Panel>
              <Chip tone="green" className="-bottom-4 left-3 lg:bottom-8 lg:-left-[30px]">
                {say(v.callTime, {
                  when: intl.formatDate(callAt, { weekday: "short", hour: "2-digit", minute: "2-digit", timeZone: "Europe/London" }),
                })}
              </Chip>
              <Chip tone="violet" className="-top-4 right-3 lg:-top-[18px] lg:right-auto lg:left-[150px]">
                {say(v.agentChip, { name: "research-agent" })}
              </Chip>
            </div>
          </div>
        </Wrap>
      </header>

      <main>
        <Wrap>
          <div className="relative -mt-11 grid gap-5 md:grid-cols-3">
            <Fact tone="coral" n="1" title={say(m.queueTitle)}>
              {say(m.queueText)}
            </Fact>
            <Fact tone="violet" n="0" title={say(m.sentTitle)}>
              {say(m.sentText)}
            </Fact>
            <Fact tone="green" n="1" title={say(m.sourceTitle)}>
              {say(m.sourceText)}
            </Fact>
          </div>
        </Wrap>

        <section className="pt-[72px] lg:pt-[104px]" id="loop">
          <Wrap>
            <SectionHead label={say(m.howLabel)} title={say(m.howTitle)}>
              {say(m.howIntro)}
            </SectionHead>
            <Step
              tone="green"
              tag={say(m.researchTag)}
              title={say(m.researchTitle)}
              shot={<Shot file="research.webp" width={1440} height={900} alt={say(m.researchAlt)} tone="green" />}
            >
              <p>{say(m.researchText1)}</p>
              <p>{say(m.researchText2)}</p>
            </Step>
            <Step
              tone="gold"
              flipped
              tag={say(m.todayTag)}
              title={say(m.todayTitle)}
              shot={<Shot file="linkedin.webp" width={1440} height={900} alt={say(m.linkedinAlt)} tone="gold" />}
            >
              <p>{say(m.todayOrder)}</p>
              <ol className="grid gap-2 text-base text-foreground">
                {[say(m.orderConversations), say(m.orderCalls), say(m.orderMessages), say(m.orderAdds)].map((item, i) => (
                  <li key={item} className="flex items-center gap-3">
                    <span className="grid size-[22px] flex-none place-items-center rounded-full bg-primary font-mono text-xs">{i + 1}</span>
                    {item}
                  </li>
                ))}
              </ol>
              <div className="mt-5 flex flex-wrap gap-[18px] text-[15px]">
                {(
                  [
                    ["o", m.keyOpen],
                    ["a", m.keyAdded],
                    ["s", m.keySkip],
                  ] as const
                ).map(([key, what]) => (
                  <span key={key}>
                    <kbd className="mr-2 inline-grid h-7 min-w-7 place-items-center rounded-[7px] border-2 border-b-4 border-ink bg-card font-mono text-[13px] text-foreground">
                      {key}
                    </kbd>
                    {say(what)}
                  </span>
                ))}
              </div>
            </Step>
            <Step
              tone="coral"
              tag={say(m.conversationsTag)}
              title={say(m.conversationsTitle)}
              shot={<Shot file="conversations.webp" width={1440} height={560} alt={say(m.conversationsAlt)} tone="coral" />}
            >
              <p>{say(m.conversationsText1)}</p>
              <p>{say(m.conversationsText2)}</p>
            </Step>
          </Wrap>
        </section>

        <section className="pt-[72px] lg:pt-[104px]" id="customers">
          <Wrap>
            <SectionHead label={say(m.wonLabel)} title={say(m.wonTitle)}>
              {say(m.wonIntro)}
            </SectionHead>
            <div className="grid gap-5 md:grid-cols-2 lg:grid-cols-3">
              <Feature tone="coral" title={say(m.dealsTitle)}>
                {say(m.dealsText)}
              </Feature>
              <Feature tone="violet" title={say(m.agreementsTitle)}>
                {say(m.agreementsText)}
              </Feature>
              <Feature tone="gold" title={say(m.subscriptionsTitle)}>
                {say(m.subscriptionsText)}
              </Feature>
              <Feature tone="green" title={say(m.onboardingTitle)}>
                {say(m.onboardingText)}
              </Feature>
              <Feature tone="coral" title={say(m.enquiriesTitle)}>
                {say(m.enquiriesText)}
              </Feature>
              <Feature tone="gold" title={say(m.campaignsTitle)}>
                {say(m.campaignsText)}
              </Feature>
            </div>
          </Wrap>
        </section>

        <section className="mt-[72px] bg-sidebar py-[72px] text-background lg:mt-[104px] lg:py-24" id="agents">
          <Wrap>
            <SectionHead label={<Tag tone="violet">{say(m.agentsLabel)}</Tag>} title={say(m.agentsTitle)}>
              {say(m.agentsIntro)}
            </SectionHead>
            <div className="grid gap-8 lg:grid-cols-2">
              <div className="grid content-start gap-8">
                <Terminal title={say(m.connectClaude)}>
                  <span>
                    <b>{prompt}</b>
                    {connectLines.add}
                  </span>
                  <span className="pl-[2.4em]">{connectLines.url}</span>
                  <span className="pl-[2.4em]">
                    {connectLines.header}
                    <i>{connectLines.bearer}</i>
                  </span>
                </Terminal>
                <Shot file="agents.webp" width={1440} height={765} alt={say(m.agentsAlt)} tone="violet" />
              </div>
              <Panel className="grid self-start border-sidebar-border! bg-[#211b11]! text-background md:grid-cols-2">
                <div className="min-w-0 p-[26px]">
                  <Tag tone="green" asChild>
                    <h3>{say(m.keyCan)}</h3>
                  </Tag>
                  <ul className="mt-[18px] grid gap-3.5 text-base leading-snug [&_code]:mt-[3px] [&_code]:block [&_code]:font-mono [&_code]:text-xs [&_code]:text-[#b9b0a0]">
                    {keyCan.map(([what, scope]) => (
                      <li key={scope}>
                        {say(m[what])}
                        <code>{scope}</code>
                      </li>
                    ))}
                  </ul>
                </div>
                <div className="min-w-0 border-t-2 border-sidebar-border p-[26px] md:border-t-0 md:border-l-2">
                  <Tag tone="coral" asChild>
                    <h3>{say(m.keyCannot)}</h3>
                  </Tag>
                  <ul className="mt-[18px] grid gap-3.5 text-base leading-snug [&_code]:font-mono [&_code]:text-xs [&_code]:text-[#b9b0a0]">
                    <li>{say(m.cannotSign)}</li>
                    <li>{say(m.cannotMail)}</li>
                    <li>
                      <FormattedMessage
                        defaultMessage="Make or revoke API keys. Only {settings} can change your organisation's settings, {team} its members, {mail} its mail server and {sso} its single sign-on, and only {agents} Prospector's own agents"
                        id="yYgFLT"
                        description="In the list of what no API key can do. Each {placeholder} is the name of a key scope, such as settings:write, kept as code"
                        values={{
                          settings: <code>{scopes.settings}</code>,
                          team: <code>{scopes.team}</code>,
                          mail: <code>{scopes.mail}</code>,
                          sso: <code>{scopes.sso}</code>,
                          agents: <code>{scopes.agents}</code>,
                        }}
                      />
                    </li>
                    <li>{say(m.cannotSee)}</li>
                    <li>{say(m.cannotAdd)}</li>
                  </ul>
                </div>
              </Panel>
            </div>
          </Wrap>
        </section>

        <section className="pt-[72px] lg:pt-[104px]" id="team">
          <Wrap>
            <SectionHead label={say(m.teamLabel)} title={say(m.teamTitle)} />
            <div className="grid gap-5 md:grid-cols-2 lg:grid-cols-3">
              <Feature tone="coral" title={say(m.neverTitle)}>
                {say(m.neverText)}
              </Feature>
              <Feature tone="gold" title={say(m.callsTitle)}>
                {say(m.callsText)}
              </Feature>
              <Feature tone="green" title={say(m.historyTitle)}>
                {say(m.historyText)}
              </Feature>
              <Feature tone="violet" title={say(m.orgsTitle)}>
                {say(m.orgsText)}
              </Feature>
              <Feature tone="coral" title={say(m.filesTitle)}>
                {say(m.filesText)}
              </Feature>
              <Feature tone="gold" title={say(m.dataTitle)}>
                {say(m.dataText)}
              </Feature>
            </div>
          </Wrap>
        </section>

        <Pricing />

        <section className="mt-[72px] border-y-2 border-ink bg-chart-3 lg:mt-[104px]" id="start">
          <Wrap className="grid items-center gap-14 py-16 lg:grid-cols-[minmax(0,5fr)_minmax(0,7fr)] lg:py-[88px]">
            <div>
              <Eyebrow className="text-[#4d1c0e]">{say(m.selfHostLabel)}</Eyebrow>
              <h2 className="mt-3.5 mb-4 font-heading text-[clamp(32px,4.2vw,52px)] font-bold leading-[1.05] tracking-[-0.02em] text-balance">
                {say(m.selfHostTitle)}
              </h2>
              <p className="mb-7 text-lg text-[#2c120a]">{say(m.selfHostText)}</p>
              <div className="flex flex-wrap gap-3">
                <SiteButton big href={`${repoURL}/releases/latest`} className="hover:shadow-[4px_4px_0_var(--primary)]!">
                  {say(m.download)} {arrow}
                </SiteButton>
                <SiteButton look="outline" big href={`${repoURL}#readme`}>
                  {say(m.docs)} {arrow}
                </SiteButton>
              </div>
            </div>
            <Terminal title={say(m.terminal)} className="border-ink! bg-sidebar! shadow-[12px_12px_0_var(--ink)]!">
              {selfHostLines.map(([command, quoted]) => (
                <span key={command}>
                  <b>{prompt}</b>
                  {command}
                  {quoted && <i>{quoted}</i>}
                </span>
              ))}
              <span className="text-[#8a8170]">{serving}</span>
            </Terminal>
          </Wrap>
        </section>
        <div id="enquiry" />
      </main>
      <Footer blurb={say(m.blurb)}>
        <a href="#pricing">{say(m.pricingLink)}</a>
        <a href="#start">{say(m.selfHostLink)}</a>
        <a href={repoURL}>{github}</a>
        <a href={`${repoURL}/releases`}>{say(m.releasesLink)}</a>
        <a href="/login">{say(m.signIn)}</a>
      </Footer>
    </>
  );
}

function Chip({ tone, className, children }: { tone: Tone; className: string; children: ReactNode }) {
  return (
    <span
      className={cn(
        "absolute inline-flex items-center gap-2 whitespace-nowrap rounded-full border-2 border-ink bg-card px-3 py-2 text-xs font-semibold shadow-[4px_4px_0_var(--ink)] sm:px-[15px] sm:py-2.5 sm:text-sm",
        "animate-in fade-in slide-in-from-bottom-3 fill-mode-both delay-700 duration-500 motion-reduce:animate-none",
        className,
      )}
    >
      <i className={cn("size-[9px] rounded-full", toneFill[tone])} />
      {children}
    </span>
  );
}

function Fact({ tone, n, title, children }: { tone: Tone; n: string; title: string; children: ReactNode }) {
  return (
    <Panel tone={tone} className="flex-row items-center gap-[18px] px-6 py-[22px]">
      <b className={cn("grid size-[62px] flex-none place-items-center rounded-full font-heading text-[34px] tabular-nums", toneFill[tone])}>{n}</b>
      <p className="text-base leading-[1.45] text-muted-foreground">
        <strong className="block font-semibold text-foreground">{title}</strong>
        {children}
      </p>
    </Panel>
  );
}

function Step({
  tone,
  flipped,
  tag,
  title,
  shot,
  children,
}: {
  tone: Tone;
  flipped?: boolean;
  tag: string;
  title: string;
  shot: ReactNode;
  children: ReactNode;
}) {
  return (
    <div
      className={cn(
        "grid items-center gap-9 border-t-2 border-ink py-12 lg:gap-16",
        flipped ? "lg:grid-cols-[minmax(0,7fr)_minmax(0,5fr)]" : "lg:grid-cols-[minmax(0,5fr)_minmax(0,7fr)]",
      )}
    >
      <div className={cn("text-muted-foreground [&>p]:mb-3.5", flipped && "lg:order-2")}>
        <Tag tone={tone}>{tag}</Tag>
        <h3 className="mt-4 mb-3.5 font-heading text-[30px] font-bold leading-[1.12] tracking-[-0.015em] text-foreground">{title}</h3>
        {children}
      </div>
      {shot}
    </div>
  );
}

function Feature({ tone, title, children }: { tone: Tone; title: string; children: ReactNode }) {
  return (
    <Panel className="gap-2 p-[26px]">
      <span className={cn("mb-2.5 block h-2 w-[34px] rounded", toneFill[tone])} />
      <h3 className="font-heading text-[19px] leading-[1.3] font-semibold">{title}</h3>
      <CardDescription className="text-base leading-normal">{children}</CardDescription>
    </Panel>
  );
}

function Pound() {
  return <small className="mr-0.5 align-[0.55em] text-[0.5em]">{pound}</small>;
}

type PlanTerms = {
  dark?: boolean;
  tone: Tone;
  name: string;
  note: string;
  audience: string;
  price: ReactNode;
  per: string;
  items: string[];
  cta: string;
};

// The hosted terms are a public promise, and the English is the version that holds, so
// they stay English in every language and the page marks them lang="en".
const terms = {
  title: `Free for your first ${freeUsers} users.`,
  intro: "Every plan has every feature, run by us: the server, its backups and each release as it ships.",
  finePrint:
    "A user is anyone who signs in to one of your organisations. API keys and agents are free. Billed monthly; prices exclude VAT. Campaigns go through your own mail server.",
  plans: [
    {
      tone: "green",
      name: "Free",
      note: "To start",
      audience: "For a small team finding its first customers.",
      price: (
        <>
          <Pound />
          {0}
        </>
      ),
      per: `up to ${freeUsers} users`,
      items: [
        "Every feature, including agents, campaigns and agreements",
        "Hosting, backups, sign-in mail and every release",
        "Help from the docs and GitHub issues",
      ],
      cta: "Start free",
    },
    {
      dark: true,
      tone: "gold",
      name: "Team",
      note: "As you grow",
      audience: `For a team past ${freeUsers}. Your first ${freeUsers} users stay free.`,
      price: (
        <>
          <Pound />
          {perUser}
        </>
      ),
      per: "a user a month",
      items: ["Everything in Free, for any number of users", `Pay only for users past your first ${freeUsers}`, "Help by email from the people who build it"],
      cta: "Start with your team",
    },
    {
      tone: "violet",
      name: "Larger teams",
      note: "Talk to us",
      audience: "For a bigger team, or terms of your own.",
      price: "Let's talk",
      per: "a price that fits",
      items: ["Everything in Team", "A price for your number of users", "Terms and support to suit you"],
      cta: "Talk to us",
    },
  ] satisfies PlanTerms[],
};

// A plan's button leads to the enquiry form, so the form's script draws it into the
// plan's data-plan-cta slot, labelled as that says; with no form, the slot stays empty.
function Plan({ dark, tone, name, note, audience, price, per, items, cta }: PlanTerms) {
  const soft = dark ? "text-[#b9b0a0]" : "text-muted-foreground";
  return (
    <Panel tone={tone} className={cn("min-w-0 gap-[22px] p-6 sm:p-8", dark && "bg-sidebar! text-background")}>
      <div className="flex flex-wrap items-baseline justify-between gap-3">
        <h3 className="font-heading text-[26px] leading-[1.1] font-bold tracking-[-0.015em]">{name}</h3>
        <Eyebrow className={soft}>{note}</Eyebrow>
      </div>
      <p className={cn("text-base leading-normal", soft)}>{audience}</p>
      <div className={cn("flex flex-wrap items-baseline gap-2.5 border-y-2 py-[18px]", dark ? "border-sidebar-border" : "border-border")}>
        <b
          className={cn(
            "font-heading leading-[0.9] font-bold tracking-[-0.03em] tabular-nums",
            typeof price === "string" ? "text-[44px]" : "text-[60px] sm:text-[72px]",
            dark && "text-primary",
          )}
        >
          {price}
        </b>
        <span className={cn("font-mono text-[13px] font-medium uppercase tracking-[0.06em]", soft)}>{per}</span>
      </div>
      <ul className="grid gap-[11px] text-base leading-[1.45]">
        {items.map((item) => (
          <li key={item} className="grid grid-cols-[26px_1fr] items-start">
            <span
              className={cn(
                "mt-1.5 h-2 w-3.5 -rotate-45 border-b-[3px] border-l-[3px]",
                { gold: "border-primary", coral: "border-chart-3", green: "border-chart-2", violet: "border-chart-4" }[tone],
              )}
            />
            {item}
          </li>
        ))}
      </ul>
      <div className="mt-auto flex flex-wrap gap-3 empty:hidden" data-plan-cta={cta} data-look={dark ? "gold" : "ink"} />
    </Panel>
  );
}

function Pricing() {
  const intl = useIntl();
  const say = intl.formatMessage;
  return (
    <section className="pt-[72px] lg:pt-[104px]" id="pricing">
      <Wrap>
        <div lang="en">
          <SectionHead label={<Eyebrow lang={intl.locale}>{say(m.pricingLabel)}</Eyebrow>} title={terms.title}>
            {terms.intro}
          </SectionHead>
          <div className="grid gap-7 lg:grid-cols-3">
            {terms.plans.map((plan) => (
              <Plan key={plan.name} {...plan} />
            ))}
          </div>
          <p className="mt-8 text-sm text-muted-foreground">{terms.finePrint}</p>
        </div>
        {intl.locale !== "en-GB" && <p className="mt-3 text-sm text-muted-foreground">{say(m.englishHolds)}</p>}
        <div role="note" className="mt-10 flex flex-wrap items-center gap-x-6 gap-y-3.5 rounded-[14px] border-2 border-dashed border-ink px-6 py-5">
          <strong className="font-heading text-[17px] font-semibold">{say(m.noLockIn)}</strong>
          <p className="min-w-0 flex-[1_1_24em] text-base text-muted-foreground">
            <FormattedMessage
              defaultMessage="An owner can download everything the organisation holds as one file, whenever they like. Or <link>run Prospector yourself</link>, free."
              id="KZqqpp"
              description="Under 'No lock-in.' by the pricing; <link> goes to the section on running Prospector yourself"
              values={{ link: startLink }}
            />
          </p>
        </div>
      </Wrap>
    </section>
  );
}

type Outcome = "sent" | "invalid" | "busy" | "refused" | "failed";

const said = defineMessages({
  sending: { defaultMessage: "Sending…", id: "a6OlUA", description: "Beside the enquiry form's button while it sends" },
  sent: { defaultMessage: "Sent. Someone will write back to you.", id: "6E9hpO", description: "Beside the enquiry form's button once the message is sent" },
  invalid: {
    defaultMessage: "That couldn't be taken. Check the email address and try again.",
    id: "TOMOWW",
    description: "Beside the enquiry form's button when the server refused what was filled in",
  },
  busy: {
    defaultMessage: "Too many messages just now. Your message is still here: try again in an hour.",
    id: "XVUPCU",
    description: "Beside the enquiry form's button when too many enquiries were sent",
  },
  refused: {
    defaultMessage: "This form can't send from here at the moment.",
    id: "38qEvi",
    description: "Beside the enquiry form's button when the server won't take it",
  },
  failed: {
    defaultMessage: "It didn't send. Your message is still here: try again in a minute.",
    id: "KS+dKG",
    description: "Beside the enquiry form's button when it couldn't reach the server",
  },
});

const f = defineMessages({
  contact: { defaultMessage: "Contact", id: "4PE95e", description: "Front page navigation link to the enquiry form, and the small heading above it" },
  ask: { defaultMessage: "Ask us anything.", id: "k+Yd9p", description: "Heading of the front page's enquiry form" },
  intro: {
    defaultMessage: "Hosting, a question, a demo, or help getting it running. Someone will write back to the address you give.",
    id: "V4ji5l",
    description: "Text beside the front page's enquiry form",
  },
  name: { defaultMessage: "Your name", id: "FiVE0K", description: "Label of the enquiry form's name field" },
  email: { defaultMessage: "Email", id: "DW1meq", description: "Label of the enquiry form's email field" },
  company: { defaultMessage: "Company", id: "Vjpit0", description: "Label of the enquiry form's company field" },
  about: { defaultMessage: "What it's about", id: "HblqzJ", description: "Label of the enquiry form's choice of what the enquiry is about" },
  signup: { defaultMessage: "Hosted Prospector", id: "/v2qca", description: "Choice in the enquiry form: asking for Prospector hosted by us" },
  sales: { defaultMessage: "A question", id: "lwvUKS", description: "Choice in the enquiry form: a general question" },
  demo: { defaultMessage: "See it running", id: "3IYZLh", description: "Choice in the enquiry form: asking for a demo" },
  support: { defaultMessage: "Help running it", id: "i69Ymq", description: "Choice in the enquiry form: asking for help running Prospector" },
  feature: { defaultMessage: "Something it should do", id: "x6tAYp", description: "Choice in the enquiry form: asking for a feature" },
  message: { defaultMessage: "Message", id: "ExMpnf", description: "Label of the enquiry form's message field" },
  trap: {
    defaultMessage: "Leave this empty",
    id: "wIq9T8",
    description: "Label of a hidden field in the enquiry form that only spam robots fill in; people never see it",
  },
  send: { defaultMessage: "Send", id: "1F/aO5", description: "Button to send the enquiry form" },
});

const kinds = ["signup", "sales", "demo", "support", "feature"] as const;

// send tries a few times: the server keeps a repeated enquiry once, so a dropped answer
// is safe to send again. An answer that says no would say no again.
async function send(to: string, body: string): Promise<Outcome> {
  for (let attempt = 0; attempt < 3; attempt++) {
    if (attempt) await new Promise((done) => setTimeout(done, attempt * 1500));
    try {
      const res = await fetch(to, { method: "POST", headers: { "content-type": "application/json" }, body });
      if (res.ok) return "sent";
      if (res.status === 400) return "invalid";
      if (res.status === 429) return "busy";
      if (res.status < 500) return "refused";
    } catch {
      // offline or cut off: try again
    }
  }
  return "failed";
}

const fieldLabel = "flex flex-col items-stretch gap-1.5 leading-[1.4] font-mono text-[11px] font-medium uppercase tracking-[0.08em] text-muted-foreground";
const field = "h-11 rounded-lg border-2 border-ink bg-background px-3 font-sans text-base normal-case tracking-normal text-foreground";

// EnquiryForm is the front page's contact section, with its link in the page's nav and
// the plans' buttons, which lead nowhere where there is no form.
export function EnquiryForm({ intake, nav, plans }: { intake: string; nav: Element | null; plans: HTMLElement[] }) {
  const say = useIntl().formatMessage;
  const [sending, setSending] = useState(false);
  const [status, setStatus] = useState<keyof typeof said | null>(null);
  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    const data = new FormData(form);
    const value = (name: string) => String(data.get(name) ?? "").trim();
    const body = JSON.stringify({
      kind: value("kind"),
      name: value("name"),
      email: value("email"),
      company: value("company"),
      message: value("message"),
      trap: value("ref_note"),
      page: window.location.origin + window.location.pathname,
    });
    setSending(true);
    setStatus("sending");
    const outcome = await send(intake, body);
    setSending(false);
    if (outcome === "sent") form.reset();
    setStatus(outcome);
  };
  return (
    <>
      {nav && createPortal(<a href="#contact">{say(f.contact)}</a>, nav)}
      {plans.map((slot) =>
        createPortal(
          <SiteButton look={slot.dataset.look === "gold" ? "gold" : "ink"} href="#contact">
            {slot.dataset.planCta}
          </SiteButton>,
          slot,
        ),
      )}
      <section className="border-b-2 border-ink" id="contact">
        <Wrap className="grid items-start gap-14 py-16 lg:grid-cols-[minmax(0,5fr)_minmax(0,7fr)] lg:py-[88px]">
          <div>
            <Eyebrow>{say(f.contact)}</Eyebrow>
            <h2 className="mt-3.5 mb-4 font-heading text-[clamp(32px,4.2vw,52px)] font-bold leading-[1.05] tracking-[-0.02em] text-balance">{say(f.ask)}</h2>
            <p className="text-[19px] text-[#3a3122]">{say(f.intro)}</p>
          </div>
          <Panel tone="gold" className="p-[26px] md:shadow-[12px_12px_0_var(--chart-1)]!">
            <form className="grid gap-[18px] sm:grid-cols-2" onSubmit={submit}>
              <Label className={fieldLabel}>
                {say(f.name)}
                <Input className={field} name="name" autoComplete="name" maxLength={200} />
              </Label>
              <Label className={fieldLabel}>
                {say(f.email)}
                <Input className={field} name="email" type="email" autoComplete="email" maxLength={200} required />
              </Label>
              <Label className={fieldLabel}>
                {say(f.company)}
                <Input className={field} name="company" autoComplete="organization" maxLength={200} />
              </Label>
              <Label className={fieldLabel}>
                {say(f.about)}
                <select className={cn(field, "w-full")} name="kind" defaultValue="sales">
                  {kinds.map((kind) => (
                    <option key={kind} value={kind}>
                      {say(f[kind])}
                    </option>
                  ))}
                </select>
              </Label>
              <Label className={cn(fieldLabel, "sm:col-span-2")}>
                {say(f.message)}
                <Textarea className={cn(field, "h-auto min-h-32 py-2")} name="message" rows={5} maxLength={5000} required />
              </Label>
              <label className="absolute -left-[9999px]" aria-hidden>
                {say(f.trap)}
                <input name="ref_note" tabIndex={-1} autoComplete="off" />
              </label>
              <div className="flex flex-wrap items-center gap-x-[18px] gap-y-3 sm:col-span-2">
                <Button
                  type="submit"
                  disabled={sending}
                  className="h-11 rounded-[10px] border-2 border-ink bg-ink px-5 text-[15px] font-semibold text-background hover:bg-ink"
                >
                  {say(f.send)}
                </Button>
                <p className="text-[15px] text-muted-foreground" role="status">
                  {status && say(said[status])}
                </p>
              </div>
            </form>
          </Panel>
        </Wrap>
      </section>
    </>
  );
}
