import { useState, type FormEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
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

const rise = "animate-in fade-in slide-in-from-bottom-3 fill-mode-both duration-600 motion-reduce:animate-none";

// Landing is the front page, drawn when the site is built. It has no enquiry form:
// main.tsx mounts one in its place where the server has somewhere for it to send.
export function Landing() {
  return (
    <>
      <header className="front-backdrop relative">
        <img className="pointer-events-none absolute inset-0 h-full w-full object-cover opacity-20" src="/assets/img/contours.svg" alt="" />
        <Wrap className="relative">
          <nav className="relative z-10 flex items-center justify-between gap-7 py-5" aria-label="Site">
            <Brand />
            <div id="site-links" className="ml-auto hidden gap-6 text-[15px] font-medium sm:flex [&_a]:opacity-75 [&_a:hover]:opacity-100">
              <a href="#loop">How it works</a>
              <a href="#customers">Customers</a>
              <a href="#agents">Agents</a>
              <a href="#pricing">Pricing</a>
            </div>
            <SiteButton look="outline" small href="/login">
              Sign in
            </SiteButton>
          </nav>
          <div className="grid items-center gap-12 pt-8 pb-[88px] lg:grid-cols-[minmax(0,620px)_minmax(0,1fr)] lg:pt-14 lg:pb-[104px]">
            <div>
              <Eyebrow className={cn("text-[#4a3d1c]", rise)}>Prospects and customers for small teams</Eyebrow>
              <h1
                className={cn(
                  "mt-5 mb-6 font-heading text-[clamp(38px,4.6vw,56px)] font-bold leading-[1.06] tracking-[-0.025em] [&>span]:block",
                  rise,
                  "delay-75",
                )}
              >
                <span>A few hundred accounts.</span>
                <span>One queue a day.</span>
                <span>
                  Every fact <em className="-ml-[0.06em] inline-block -rotate-[1.5deg] rounded-lg bg-ink px-[0.18em] not-italic text-primary">sourced.</em>
                </span>
              </h1>
              <p className={cn("mb-8 max-w-[34em] text-[19px] text-[#3a3122]", rise, "delay-150")}>
                Prospector is a workbench for teams selling to named accounts, from the first fact about a company to the agreement it signs. Agents research
                and draft. People qualify, decide and send.
              </p>
              <div className={cn("flex flex-wrap gap-3", rise, "delay-200")}>
                <SiteButton big href="#pricing">
                  Free for {freeUsers} users
                </SiteButton>
                <SiteButton look="outline" big href="#loop">
                  See how it works
                </SiteButton>
              </div>
            </div>
            <div className="relative min-w-0 [perspective:1800px]">
              <Panel className="overflow-hidden bg-sidebar! shadow-[-30px_50px_90px_rgba(92,52,0,0.5)]! lg:w-[900px] lg:origin-left lg:[transform:rotateY(-13deg)_rotateX(5deg)_rotateZ(1deg)]">
                <div className="flex items-center gap-[7px] bg-ink px-3.5 py-[11px]">
                  <i className="size-2.5 rounded-full bg-chart-3" />
                  <i className="size-2.5 rounded-full bg-primary" />
                  <i className="size-2.5 rounded-full bg-chart-2" />
                  <span className="ml-3 min-w-0 truncate font-mono text-xs text-[#b9b0a0]">prospector.example.com/o/example-co/today</span>
                </div>
                <img
                  className="block h-auto w-full"
                  src="/assets/img/today.webp"
                  width={1440}
                  height={900}
                  alt="The Today page: everything due, best first, with a Do now list beside them"
                />
              </Panel>
              <Chip tone="green" className="-bottom-4 left-3 lg:bottom-8 lg:-left-[30px]">
                Tue 16:39 in London: good time to call
              </Chip>
              <Chip tone="violet" className="-top-4 right-3 lg:-top-[18px] lg:right-auto lg:left-[150px]">
                agent · research-agent
              </Chip>
            </div>
          </div>
        </Wrap>
      </header>

      <main>
        <Wrap>
          <div className="relative -mt-11 grid gap-5 md:grid-cols-3">
            <Fact tone="coral" n="1" title="queue a day">
              across LinkedIn, email and phone.
            </Fact>
            <Fact tone="violet" n="0" title="messages sent by agents">
              They draft. Only people send.
            </Fact>
            <Fact tone="green" n="1" title="source link per fact">
              An agent's fact without one is refused.
            </Fact>
          </div>
        </Wrap>

        <section className="pt-[72px] lg:pt-[104px]" id="loop">
          <Wrap>
            <SectionHead label="How it works" title="From a company name to a signed deal, in three pages.">
              Research fills the list. Today works it. Conversations closes it. The screenshots show invented companies and people.
            </SectionHead>
            <Step
              tone="green"
              tag="01 · Research"
              title="Facts with the link they came from"
              shot={
                <Shot
                  file="research.webp"
                  width={1440}
                  height={900}
                  alt="Research: a brief for an agent at the top, its search, and two companies it found, each with sourced facts"
                  tone="green"
                />
              }
            >
              <p>
                Say who you're looking for and send an agent after them. Ask for about so many companies and watch how far the search has got. What it finds
                that you already have is left out, and nothing reaches outreach until someone qualifies it.
              </p>
              <p>
                Sell more than one thing? Each offering gets its own look-for list and queue, so a company that is wrong for one can still be right for another.
                Every fact keeps its source.
              </p>
            </Step>
            <Step
              tone="gold"
              flipped
              tag="02 · Today"
              title="One queue, best first"
              shot={
                <Shot
                  file="linkedin.webp"
                  width={1440}
                  height={900}
                  alt="The LinkedIn queue: the connection note ready to copy, with keyboard shortcuts"
                  tone="gold"
                />
              }
            >
              <p>Everything due across LinkedIn, email and phone, in this order:</p>
              <ol className="grid gap-2 text-base text-foreground">
                {[
                  "Conversations that need a next step",
                  "Calls where it's office hours for the contact",
                  "LinkedIn messages, emails and follow-ups",
                  "LinkedIn adds",
                ].map((item, i) => (
                  <li key={item} className="flex items-center gap-3">
                    <span className="grid size-[22px] flex-none place-items-center rounded-full bg-primary font-mono text-xs">{i + 1}</span>
                    {item}
                  </li>
                ))}
              </ol>
              <div className="mt-5 flex flex-wrap gap-[18px] text-[15px]">
                {[
                  ["o", "open and copy the note"],
                  ["a", "added"],
                  ["s", "skip"],
                ].map(([key, what]) => (
                  <span key={key}>
                    <kbd className="mr-2 inline-grid h-7 min-w-7 place-items-center rounded-[7px] border-2 border-b-4 border-ink bg-card font-mono text-[13px] text-foreground">
                      {key}
                    </kbd>
                    {what}
                  </span>
                ))}
              </div>
            </Step>
            <Step
              tone="coral"
              tag="03 · Conversations"
              title="Replies become deals"
              shot={<Shot file="conversations.webp" width={1440} height={560} alt="Conversations: a board from replied to won" tone="coral" />}
            >
              <p>Replies move across a board: replied, meeting, proposal, won, lost.</p>
              <p>Each one needs a next step with a date. Any without one, or due today, come back to the top of Today.</p>
            </Step>
          </Wrap>
        </section>

        <section className="pt-[72px] lg:pt-[104px]" id="customers">
          <Wrap>
            <SectionHead label="After it's won" title="The customer stays on the same record.">
              Deals, the contract, what they're on, getting them live, what they write in about and what you write to them, all on the company you prospected.
              Each has its operations in the published API.
            </SectionHead>
            <div className="grid gap-5 md:grid-cols-2 lg:grid-cols-3">
              <Feature tone="coral" title="Deals with a value">
                Each conversation carries what it's worth, who has it and when it should close. Start a second one for a renewal or an expansion.
              </Feature>
              <Feature tone="violet" title="Agreements, ready to sign">
                Upload the contract as a PDF. It is kept exactly as uploaded and checked against its hash on every download. Choose who signs and in what order,
                and drag their fields onto the pages. Sending for signature comes next.
              </Feature>
              <Feature tone="gold" title="Subscriptions and their notice">
                What each customer is on, for how long, and the notice it needs. Today flags one a month before its date, and a renewal deal starts from it.
              </Feature>
              <Feature tone="green" title="Onboarding to go-live">
                Your steps and theirs, dated from the start. The customer gets a page of their own steps from an emailed link, no password, and marks theirs
                done. Late steps come back to Today.
              </Feature>
              <Feature tone="coral" title="Enquiries answered">
                Site forms and your product's sign-ups arrive open, to be given an owner. Reply by email from the card, and paste their answer into the thread.
              </Feature>
              <Feature tone="gold" title="Campaigns to people you know">
                Write once to everyone in conversation or at a current customer, never to cold prospects. It goes through your own mail server with your postal
                address and a one-click unsubscribe, and nobody is mailed twice. A service notice, such as a price change, reaches customers who unsubscribed
                from marketing. With Postmark, an address that bounces or calls a mail spam gets no more.
              </Feature>
            </div>
          </Wrap>
        </section>

        <section className="mt-[72px] bg-sidebar py-[72px] text-background lg:mt-[104px] lg:py-24" id="agents">
          <Wrap>
            <SectionHead label={<Tag tone="violet">Agents and API</Tag>} title="Agents do the reading. You decide what they may send.">
              Each organisation has an MCP endpoint and a REST API with scoped keys. Connect Claude or your own agent in one command, and decide what each key
              may do. Or let Prospector run the agents itself, through Claude, any OpenAI-compatible endpoint or a model you host, with free search from your
              own SearXNG. Each agent's card says whether it is ready, and why not.
            </SectionHead>
            <div className="grid gap-8 lg:grid-cols-2">
              <div className="grid content-start gap-8">
                <Terminal title="Connect Claude Code">
                  <span>
                    <b>$ </b>claude mcp add --transport http prospector \
                  </span>
                  <span className="pl-[2.4em]">https://&lt;host&gt;/o/&lt;org&gt;/mcp \</span>
                  <span className="pl-[2.4em]">
                    --header <i>"Authorization: Bearer $PROSPECTOR_KEY"</i>
                  </span>
                </Terminal>
                <Shot
                  file="agents.webp"
                  width={1440}
                  height={765}
                  alt="Agents: each of Prospector's agents on a card saying whether it is ready, with its model, search, tools and fallback"
                  tone="violet"
                />
              </div>
              <Panel className="grid self-start border-sidebar-border! bg-[#211b11]! text-background md:grid-cols-2">
                <div className="min-w-0 p-[26px]">
                  <Tag tone="green" asChild>
                    <h3>A key can</h3>
                  </Tag>
                  <ul className="mt-[18px] grid gap-3.5 text-base leading-snug [&_code]:mt-[3px] [&_code]:block [&_code]:font-mono [&_code]:text-xs [&_code]:text-[#b9b0a0]">
                    <li>
                      Search companies and people, and read what's due and the tasks people wrote<code>read</code>
                    </li>
                    <li>
                      Add companies, facts and suggested contacts, each with a source URL<code>research:write</code>
                    </li>
                    <li>
                      Draft notes, emails and follow-ups, shown as the agent's<code>drafts:write</code>
                    </li>
                    <li>
                      Record an outcome, shown as the agent's in the history<code>outcomes:write</code>
                    </li>
                    <li>
                      Value deals, work a renewal or expansion through its stages, and say where answering an enquiry stands<code>pipeline:write</code>
                    </li>
                    <li>
                      Say where answering an enquiry stands, without deals<code>enquiries:triage</code>
                    </li>
                    <li>
                      Record subscriptions and work customers' onboarding<code>customers:write</code>
                    </li>
                    <li>
                      Set out who signs an agreement and place their fields<code>agreements:write</code>
                    </li>
                    <li>
                      Email a reply to someone who wrote in<code>enquiries:reply</code>
                    </li>
                    <li>
                      Email a customer the link to their onboarding steps<code>customers:invite</code>
                    </li>
                    <li>
                      Send a campaign to everyone in conversation or at a customer, through your own mail server, each with an unsubscribe link
                      <code>campaigns:send</code>
                    </li>
                  </ul>
                </div>
                <div className="min-w-0 border-t-2 border-sidebar-border p-[26px] md:border-t-0 md:border-l-2">
                  <Tag tone="coral" asChild>
                    <h3>No key can</h3>
                  </Tag>
                  <ul className="mt-[18px] grid gap-3.5 text-base leading-snug [&_code]:font-mono [&_code]:text-xs [&_code]:text-[#b9b0a0]">
                    <li>Sign an agreement</li>
                    <li>Send mail without a scope that says so, or to anyone on your never-contact list or erased on request</li>
                    <li>
                      Make or revoke API keys. Only <code>settings:write</code> can change your organisation's settings, <code>team:write</code> its members,{" "}
                      <code>mail:configure</code> its mail server and <code>sso:configure</code> its single sign-on, and only <code>agents:configure</code>{" "}
                      Prospector's own agents
                    </li>
                    <li>See another organisation</li>
                    <li>Add anyone on your never-contact list</li>
                  </ul>
                </div>
              </Panel>
            </div>
          </Wrap>
        </section>

        <section className="pt-[72px] lg:pt-[104px]" id="team">
          <Wrap>
            <SectionHead label="Built for a small team" title="The details that keep a list clean." />
            <div className="grid gap-5 md:grid-cols-2 lg:grid-cols-3">
              <Feature tone="coral" title="A never-contact list that holds">
                Domains, email addresses and LinkedIn profiles. A company on a listed domain stays on file but out of review and outreach. It applies to agents,
                imports and edits alike.
              </Feature>
              <Feature tone="gold" title="Calls at a sensible hour">
                Office hours come from the contact's phone number, so Zurich gets called when Zurich is at work.
              </Feature>
              <Feature tone="green" title="The full history">
                Every company shows who did what and when, people and agents alike.
              </Feature>
              <Feature tone="violet" title="Several organisations, one login">
                Each has its own prospects, wording, team and keys. None can see another's. Sign in with a password, Google, Microsoft, LinkedIn or your
                organisation's own single sign-on.
              </Feature>
              <Feature tone="coral" title="Bring your own files">
                Import companies from CSV and drafts from markdown. Re-importing never clears a date or an edit made in the app.
              </Feature>
              <Feature tone="gold" title="Your data stays yours">
                Host it yourself on SQLite or Postgres, or let us. An owner can download everything the organisation holds as one file, and erase a person who
                asks to be forgotten.
              </Feature>
            </div>
          </Wrap>
        </section>

        <Pricing />

        <section className="mt-[72px] border-y-2 border-ink bg-chart-3 lg:mt-[104px]" id="start">
          <Wrap className="grid items-center gap-14 py-16 lg:grid-cols-[minmax(0,5fr)_minmax(0,7fr)] lg:py-[88px]">
            <div>
              <Eyebrow className="text-[#4d1c0e]">Rather run it yourself?</Eyebrow>
              <h2 className="mt-3.5 mb-4 font-heading text-[clamp(32px,4.2vw,52px)] font-bold leading-[1.05] tracking-[-0.02em] text-balance">
                Self-host in four commands.
              </h2>
              <p className="mb-7 text-lg text-[#2c120a]">
                Prospector is one program for Linux or macOS, on SQLite or Postgres, with every feature. Download it, name your organisation, add yourself and
                start working the list. Your server, your mail, your models.
              </p>
              <div className="flex flex-wrap gap-3">
                <SiteButton big href={`${repoURL}/releases/latest`} className="hover:shadow-[4px_4px_0_var(--primary)]!">
                  Download Prospector ↗
                </SiteButton>
                <SiteButton look="outline" big href={`${repoURL}#readme`}>
                  Read the docs ↗
                </SiteButton>
              </div>
            </div>
            <Terminal title="Terminal" className="border-ink! bg-sidebar! shadow-[12px_12px_0_var(--ink)]!">
              <span>
                <b>$ </b>prospector org add acme <i>"Acme"</i>
              </span>
              <span>
                <b>$ </b>prospector user add you@example.com <i>"Your Name"</i>
              </span>
              <span>
                <b>$ </b>prospector member add acme you@example.com owner
              </span>
              <span>
                <b>$ </b>prospector serve
              </span>
              <span className="text-[#8a8170]"># http://127.0.0.1:8766</span>
            </Terminal>
          </Wrap>
        </section>
        <div id="enquiry" />
      </main>
      <Footer blurb="Prospects and customers for small teams with a named list.">
        <a href="#pricing">Pricing</a>
        <a href="#start">Self-host</a>
        <a href={repoURL}>GitHub</a>
        <a href={`${repoURL}/releases`}>Releases</a>
        <a href="/login">Sign in</a>
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
  return <small className="mr-0.5 align-[0.55em] text-[0.5em]">£</small>;
}

// A plan's button leads to the enquiry form, so the form's script draws it into the
// plan's data-plan-cta slot, labelled as that says; with no form, the slot stays empty.
function Plan({
  dark,
  tone,
  name,
  note,
  audience,
  price,
  per,
  items,
  cta,
}: {
  dark?: boolean;
  tone: Tone;
  name: string;
  note: string;
  audience: string;
  price: ReactNode;
  per: string;
  items: string[];
  cta: string;
}) {
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
  return (
    <section className="pt-[72px] lg:pt-[104px]" id="pricing">
      <Wrap>
        <SectionHead label="Pricing" title={`Free for your first ${freeUsers} users.`}>
          Every plan has every feature, run by us: the server, its backups and each release as it ships.
        </SectionHead>
        <div className="grid gap-7 lg:grid-cols-3">
          <Plan
            tone="green"
            name="Free"
            note="To start"
            audience="For a small team finding its first customers."
            price={
              <>
                <Pound />0
              </>
            }
            per={`up to ${freeUsers} users`}
            items={[
              "Every feature, including agents, campaigns and agreements",
              "Hosting, backups, sign-in mail and every release",
              "Help from the docs and GitHub issues",
            ]}
            cta="Start free"
          />
          <Plan
            dark
            tone="gold"
            name="Team"
            note="As you grow"
            audience={`For a team past ${freeUsers}. Your first ${freeUsers} users stay free.`}
            price={
              <>
                <Pound />
                {perUser}
              </>
            }
            per="a user a month"
            items={[
              "Everything in Free, for any number of users",
              `Pay only for users past your first ${freeUsers}`,
              "Help by email from the people who build it",
            ]}
            cta="Start with your team"
          />
          <Plan
            tone="violet"
            name="Larger teams"
            note="Talk to us"
            audience="For a bigger team, or terms of your own."
            price="Let's talk"
            per="a price that fits"
            items={["Everything in Team", "A price for your number of users", "Terms and support to suit you"]}
            cta="Talk to us"
          />
        </div>
        <p className="mt-8 text-sm text-muted-foreground">
          A user is anyone who signs in to one of your organisations. API keys and agents are free. Billed monthly; prices exclude VAT. Campaigns go through
          your own mail server.
        </p>
        <div role="note" className="mt-10 flex flex-wrap items-center gap-x-6 gap-y-3.5 rounded-[14px] border-2 border-dashed border-ink px-6 py-5">
          <strong className="font-heading text-[17px] font-semibold">No lock-in.</strong>
          <p className="min-w-0 flex-[1_1_24em] text-base text-muted-foreground">
            An owner can download everything the organisation holds as one file, whenever they like. Or{" "}
            <a className="underline decoration-primary underline-offset-[3px]" href="#start">
              run Prospector yourself
            </a>
            , free.
          </p>
        </div>
      </Wrap>
    </section>
  );
}

type Outcome = "sent" | "invalid" | "busy" | "refused" | "failed";

const said: Record<Outcome, string> = {
  sent: "Sent. Someone will write back to you.",
  invalid: "That couldn't be taken. Check the email address and try again.",
  busy: "Too many messages just now. Your message is still here: try again in an hour.",
  refused: "This form can't send from here at the moment.",
  failed: "It didn't send. Your message is still here: try again in a minute.",
};

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
  const [sending, setSending] = useState(false);
  const [status, setStatus] = useState("");
  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    const f = new FormData(form);
    const value = (name: string) => String(f.get(name) ?? "").trim();
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
    setStatus("Sending…");
    const outcome = await send(intake, body);
    setSending(false);
    if (outcome === "sent") form.reset();
    setStatus(said[outcome]);
  };
  return (
    <>
      {nav && createPortal(<a href="#contact">Contact</a>, nav)}
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
            <Eyebrow>Contact</Eyebrow>
            <h2 className="mt-3.5 mb-4 font-heading text-[clamp(32px,4.2vw,52px)] font-bold leading-[1.05] tracking-[-0.02em] text-balance">
              Ask us anything.
            </h2>
            <p className="text-[19px] text-[#3a3122]">
              Hosting, a question, a demo, or help getting it running. Someone will write back to the address you give.
            </p>
          </div>
          <Panel tone="gold" className="p-[26px] md:shadow-[12px_12px_0_var(--chart-1)]!">
            <form className="grid gap-[18px] sm:grid-cols-2" onSubmit={submit}>
              <Label className={fieldLabel}>
                Your name
                <Input className={field} name="name" autoComplete="name" maxLength={200} />
              </Label>
              <Label className={fieldLabel}>
                Email
                <Input className={field} name="email" type="email" autoComplete="email" maxLength={200} required />
              </Label>
              <Label className={fieldLabel}>
                Company
                <Input className={field} name="company" autoComplete="organization" maxLength={200} />
              </Label>
              <Label className={fieldLabel}>
                What it's about
                <select className={cn(field, "w-full")} name="kind" defaultValue="sales">
                  <option value="signup">Hosted Prospector</option>
                  <option value="sales">A question</option>
                  <option value="demo">See it running</option>
                  <option value="support">Help running it</option>
                  <option value="feature">Something it should do</option>
                </select>
              </Label>
              <Label className={cn(fieldLabel, "sm:col-span-2")}>
                Message
                <Textarea className={cn(field, "h-auto min-h-32 py-2")} name="message" rows={5} maxLength={5000} required />
              </Label>
              <label className="absolute -left-[9999px]" aria-hidden>
                Leave this empty
                <input name="ref_note" tabIndex={-1} autoComplete="off" />
              </label>
              <div className="flex flex-wrap items-center gap-x-[18px] gap-y-3 sm:col-span-2">
                <Button
                  type="submit"
                  disabled={sending}
                  className="h-11 rounded-[10px] border-2 border-ink bg-ink px-5 text-[15px] font-semibold text-background hover:bg-ink"
                >
                  Send
                </Button>
                <p className="text-[15px] text-muted-foreground" role="status">
                  {status}
                </p>
              </div>
            </form>
          </Panel>
        </Wrap>
      </section>
    </>
  );
}
