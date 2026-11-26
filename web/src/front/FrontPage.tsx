import { useEffect, useState, type ReactNode } from "react";
import { EnquiryForm } from "./EnquiryForm";
import { formShows, hostedPrice, perUser, seatTotal } from "./enquiry";

export const repoURL = "https://github.com/shutthegoatup/prospector";

type Tone = "gold" | "coral" | "green" | "violet";

export type FrontProps = { version: string; intake?: string; origin?: string };

export function FrontPage({ version, intake = "", origin = "" }: FrontProps) {
  const asset = (path: string) => `/assets/${path}?v=${version}`;
  // The prerendered page has no form: whether it can send depends on the address the
  // browser reached it at, which only the browser knows.
  const [canSend, setCanSend] = useState(false);
  useEffect(() => setCanSend(formShows(intake, origin, location.origin)), [intake, origin]);

  return (
    <>
      <header className="lp-hero">
        <img className="lp-contours" src={asset("img/contours.svg")} alt="" />
        <div className="lp-wrap">
          <nav className="lp-nav" aria-label="Site">
            <Brand asset={asset} />
            <div className="lp-links">
              <a href="#loop">How it works</a>
              <a href="#customers">Customers</a>
              <a href="#agents">Agents</a>
              <a href="#pricing">Pricing</a>
              {canSend && <a href="#contact">Contact</a>}
            </div>
            <a className="lp-btn is-small" href="/login">
              Sign in
            </a>
          </nav>
          <div className="lp-hero-grid">
            <div className="lp-hero-copy">
              <p className="lp-label">Prospects and customers for small teams</p>
              <h1>
                <span>A few hundred accounts.</span>
                <span>One queue a day.</span>
                <span>
                  Every fact <em>sourced.</em>
                </span>
              </h1>
              <p className="lp-lede">
                Prospector is a workbench for teams selling to named accounts, from the first fact about a company to its signed agreement. Agents research and draft. People qualify, decide
                and send.
              </p>
              <div className="lp-cta">
                <a className="lp-btn is-primary" href="#pricing">
                  Start for {hostedPrice} a user
                </a>
                <a className="lp-btn" href="#start">
                  Self-host free
                </a>
              </div>
            </div>
            <div className="lp-stage">
              <div className="lp-window">
                <div className="lp-window-bar">
                  <i></i>
                  <i></i>
                  <i></i>
                  <span>prospector.example.com/o/example-co/today</span>
                </div>
                <img src={asset("img/today.webp")} width="1440" height="900" alt="The Today page: everything due, best first, with a Do now list beside them" />
              </div>
              <span className="lp-chip is-green a">Tue 16:39 in London: good time to call</span>
              <span className="lp-chip is-violet b">agent · research-agent</span>
            </div>
          </div>
        </div>
      </header>

      <div className="lp-wrap">
        <div className="lp-facts">
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
      </div>

      <section className="lp-section" id="loop">
        <div className="lp-wrap">
          <SectionHead label="How it works" title="From a company name to a signed deal, in three pages.">
            Research fills the list. Today works it. Conversations closes it. The screenshots show invented companies and people.
          </SectionHead>
          <Step tone="green" tag="01 · Research" title="Facts with the link they came from" shot={<Shot asset={asset} file="research.webp" height="900" alt="Research: a brief for an agent at the top, its search, and two companies it found, each with sourced facts" />}>
            <p>
              Say who you're looking for and send an agent after them. Ask for about so many companies and watch how far the search has got. What it finds that you already have is left out,
              so the queue holds only what is new.
            </p>
            <p>Sell more than one thing? Each offering gets its own look-for list and queue, so a company that is wrong for one can still be right for another. Every fact keeps its source.</p>
          </Step>
          <Step tone="gold" flipped tag="02 · Today" title="One queue, best first" shot={<Shot asset={asset} file="linkedin.webp" height="900" alt="The LinkedIn queue: the connection note ready to copy, with keyboard shortcuts" />}>
            <p>Everything due across LinkedIn, email and phone, in this order:</p>
            <ol>
              <li>Conversations that need a next step</li>
              <li>Calls where it's office hours for the contact</li>
              <li>LinkedIn messages, emails and follow-ups</li>
              <li>LinkedIn adds</li>
            </ol>
            <div className="lp-keys">
              <span>
                <kbd>o</kbd>open and copy the note
              </span>
              <span>
                <kbd>a</kbd>added
              </span>
              <span>
                <kbd>s</kbd>skip
              </span>
            </div>
          </Step>
          <Step tone="coral" tag="03 · Conversations" title="Replies become deals" shot={<Shot asset={asset} file="conversations.webp" height="560" alt="Conversations: a board from replied to won" />}>
            <p>Replies move across a board: replied, meeting, proposal, won, lost.</p>
            <p>Each one needs a next step with a date. Any without one, or due today, come back to the top of Today.</p>
          </Step>
        </div>
      </section>

      <section className="lp-section" id="customers">
        <div className="lp-wrap">
          <SectionHead label="After it's won" title="The customer stays on the same record.">
            Deals, the contract, what they're on, getting them live, what they write in about and what you write to them, all on the company you prospected. Each has its operations in the
            published API.
          </SectionHead>
          <div className="lp-grid">
            <Cell tone="coral" title="Deals with a value">
              Each conversation carries what it's worth, who has it and when it should close. Start a second one for a renewal or an expansion.
            </Cell>
            <Cell tone="violet" title="Agreements, ready to sign">
              Upload the contract as a PDF. It is kept exactly as uploaded and checked against its hash on every download. Choose who signs and in what order, and drag their fields onto the
              pages. Sending for signature comes next.
            </Cell>
            <Cell tone="gold" title="Subscriptions and their notice">
              What each customer is on, for how long, and the notice it needs. Today flags one a month before its date, and a renewal deal starts from it.
            </Cell>
            <Cell tone="green" title="Onboarding to go-live">
              Your steps and theirs, dated from the start. The customer gets a page of their own steps from an emailed link, no password, and marks theirs done. Late steps come back to Today.
            </Cell>
            <Cell tone="coral" title="Enquiries answered">
              Site forms and your product's sign-ups arrive open, to be given an owner. Reply by email from the card, and paste their answer into the thread.
            </Cell>
            <Cell tone="gold" title="Campaigns to people you know">
              Write once to everyone in conversation or at a current customer, never to cold prospects. It goes through your own mail server with your postal address and a one-click
              unsubscribe, and nobody is mailed twice. A service notice, such as a price change, reaches customers who unsubscribed from marketing.
            </Cell>
          </div>
        </div>
      </section>

      <section className="lp-night" id="agents">
        <div className="lp-wrap">
          <div className="lp-section-head">
            <span className="lp-tag is-violet">Agents and API</span>
            <h2>Agents do the reading. You decide what they may send.</h2>
            <p>
              Each organisation has an MCP endpoint and a REST API with scoped keys. Connect Claude or your own agent in one command, and decide what each key may do. Or let Prospector run
              the agents itself, through Claude, any OpenAI-compatible endpoint or a model you host, with free search from your own SearXNG. Each agent's card says whether it is ready, and
              why not.
            </p>
          </div>
          <div className="lp-agents">
            <div className="lp-agents-side">
              <div className="lp-term">
                <div className="lp-term-head">
                  <span className="lp-label">Connect Claude Code</span>
                </div>
                <div className="lp-term-body">
                  <span>
                    <b>$ </b>claude mcp add --transport http prospector \
                  </span>
                  <span className="more">https://&lt;host&gt;/o/&lt;org&gt;/mcp \</span>
                  <span className="more">
                    --header <i>"Authorization: Bearer $PROSPECTOR_KEY"</i>
                  </span>
                </div>
              </div>
              <Shot asset={asset} file="agents.webp" height="765" tone="violet" alt="Agents: each of Prospector's agents on a card saying whether it is ready, with its model, search, tools and fallback" />
            </div>
            <div className="lp-rules">
              <div>
                <h3 className="lp-tag is-green">A key can</h3>
                <ul>
                  <Can scope="read">Search companies and people, and read what's due and the tasks people wrote</Can>
                  <Can scope="research:write">Add companies, facts and suggested contacts, each with a source URL</Can>
                  <Can scope="drafts:write">Draft notes, emails and follow-ups, shown as the agent's</Can>
                  <Can scope="outcomes:write">Record an outcome, shown as the agent's in the history</Can>
                  <Can scope="pipeline:write">Value deals, work a renewal or expansion through its stages, and say where answering an enquiry stands</Can>
                  <Can scope="enquiries:triage">Say where answering an enquiry stands, without deals</Can>
                  <Can scope="customers:write">Record subscriptions and work customers' onboarding</Can>
                  <Can scope="agreements:write">Set out who signs an agreement and place their fields</Can>
                  <Can scope="enquiries:reply">Email a reply to someone who wrote in</Can>
                  <Can scope="customers:invite">Email a customer the link to their onboarding steps</Can>
                  <Can scope="campaigns:send">Send a campaign to everyone in conversation or at a customer, through your own mail server, each with an unsubscribe link</Can>
                </ul>
              </div>
              <div>
                <h3 className="lp-tag is-coral">No key can</h3>
                <ul>
                  <li>Sign an agreement</li>
                  <li>Send mail without a scope that says so, or to anyone on your never-contact list or erased on request</li>
                  <li>
                    Make or revoke API keys. Only <code>settings:write</code> can change your organisation's settings, <code>team:write</code> its members, <code>mail:configure</code> its mail
                    server and <code>sso:configure</code> its single sign-on, and only <code>agents:configure</code> Prospector's own agents
                  </li>
                  <li>See another organisation</li>
                  <li>Add anyone on your never-contact list</li>
                </ul>
              </div>
            </div>
          </div>
        </div>
      </section>

      <section className="lp-section" id="team">
        <div className="lp-wrap">
          <SectionHead label="Built for a small team" title="The details that keep a list clean." />
          <div className="lp-grid">
            <Cell tone="coral" title="A never-contact list that holds">
              Domains, companies, email addresses and LinkedIn profiles. A never-contact company stays on file but out of review and outreach. It applies to agents, imports and edits alike.
            </Cell>
            <Cell tone="gold" title="Calls at a sensible hour">
              Office hours come from the contact's phone number, so Zurich gets called when Zurich is at work.
            </Cell>
            <Cell tone="green" title="The full history">
              Every company shows who did what and when, people and agents alike.
            </Cell>
            <Cell tone="violet" title="Several organisations, one login">
              Each has its own prospects, wording, team and keys. None can see another's. Sign in with a password, Google, Microsoft, LinkedIn or your organisation's own single sign-on.
            </Cell>
            <Cell tone="coral" title="Bring your own files">
              Import companies from CSV and drafts from markdown. Re-importing never clears a date or an edit made in the app.
            </Cell>
            <Cell tone="gold" title="Your data stays yours">
              Host it yourself on SQLite or Postgres, or let us. An owner can download everything the organisation holds as one file, and erase a person who asks to be forgotten.
            </Cell>
          </div>
        </div>
      </section>

      <Pricing canSend={canSend} />

      <section className="lp-install" id="start">
        <div className="lp-wrap lp-install-grid">
          <div>
            <p className="lp-label">Self-host</p>
            <h2>Set up in four commands.</h2>
            <p className="lp-lede">Prospector is one program for Linux or macOS. Download it, name your organisation, add yourself and start working the list.</p>
            <div className="lp-cta">
              <a className="lp-btn is-primary" href={repoURL + "/releases/latest"}>
                Download Prospector ↗
              </a>
              <a className="lp-btn" href={repoURL + "#readme"}>
                Read the docs ↗
              </a>
            </div>
          </div>
          <div className="lp-term">
            <div className="lp-term-head">
              <span className="lp-label">Terminal</span>
            </div>
            <div className="lp-term-body">
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
              <span className="note"># http://127.0.0.1:8766</span>
            </div>
          </div>
        </div>
      </section>

      {canSend && <EnquiryForm intake={intake} />}

      <footer className="lp-foot">
        <div className="lp-wrap">
          <Brand asset={asset} />
          <span>Prospects and customers for small teams with a named list.</span>
          <nav aria-label="Footer">
            <a href="#pricing">Pricing</a>
            <a href={repoURL}>GitHub</a>
            <a href={repoURL + "/releases"}>Releases</a>
            <a href="/login">Sign in</a>
          </nav>
        </div>
      </footer>
    </>
  );
}

function Pricing({ canSend }: { canSend: boolean }) {
  const [users, setUsers] = useState("4");
  return (
    <section className="lp-section" id="pricing">
      <div className="lp-wrap">
        <SectionHead label="Pricing" title="Run it yourself for nothing, or let us run it.">
          Both are the same program with every feature. The only difference is who looks after the server.
        </SectionHead>
        <div className="lp-prices">
          <div className="lp-plan is-green">
            <div className="lp-plan-top">
              <h3>Self-hosted</h3>
              <span className="lp-label">Linux or macOS</span>
            </div>
            <p className="lp-plan-for">For a team with a server and someone happy to look after it.</p>
            <div className="lp-price">
              <b>
                <small>£</small>0
              </b>
              <span>for any number of users</span>
            </div>
            <ul>
              <li>Every feature, including agents, campaigns and agreements</li>
              <li>One program on your own server, with SQLite or Postgres</li>
              <li>Your own mail server, models and search</li>
              <li>Upgrade when you choose, from the releases page</li>
              <li>Help through GitHub issues</li>
            </ul>
            <div className="lp-cta">
              <a className="lp-btn is-primary" href="#start">
                Download Prospector
              </a>
            </div>
          </div>
          <div className="lp-plan is-dark is-gold">
            <div className="lp-plan-top">
              <h3>Hosted</h3>
              <span className="lp-label">We run it</span>
            </div>
            <p className="lp-plan-for">For a team that would rather sell than run a server.</p>
            <div className="lp-price">
              <b>
                <small>£</small>
                {perUser}
              </b>
              <span>a user a month</span>
            </div>
            <ul>
              <li>Every feature, the same as self-hosted</li>
              <li>We run the server and its Postgres database, with backups</li>
              <li>Each release installed as it ships</li>
              <li>Sign-in and reset mail sent for you. Campaigns still go through your own mail server</li>
              <li>Help by email from the people who build it</li>
            </ul>
            <div className="lp-sum">
              <label htmlFor="seats">
                Users
                <input id="seats" type="number" min="1" max="500" inputMode="numeric" value={users} onChange={(e) => setUsers(e.target.value)} />
              </label>
              <output htmlFor="seats">{seatTotal(users)}</output>
            </div>
            {canSend && (
              <div className="lp-cta">
                <a className="lp-btn is-primary" href="#contact">
                  Start hosted
                </a>
                <a className="lp-btn" href="#contact">
                  Ask a question
                </a>
              </div>
            )}
            <p className="lp-plan-small">A user is anyone who signs in to one of your organisations. API keys and agents are free. Prices exclude VAT.</p>
          </div>
        </div>
        <div className="lp-same">
          <strong>No lock-in.</strong>
          <p>An owner can download everything the organisation holds as one file, whenever they like, hosted or not.</p>
        </div>
      </div>
    </section>
  );
}

function Brand({ asset }: { asset: (p: string) => string }) {
  return (
    <a className="lp-brand" href="/">
      <img src={asset("img/mark.svg")} width="30" height="30" alt="" />
      <span>Prospector.</span>
    </a>
  );
}

function Shot({ asset, file, height, alt, tone }: { asset: (p: string) => string; file: string; height: string; alt: string; tone?: Tone }) {
  return (
    <div className={tone ? `lp-shot is-${tone}` : "lp-shot"}>
      <img src={asset("img/" + file)} width="1440" height={height} loading="lazy" alt={alt} />
    </div>
  );
}

function Fact({ tone, n, title, children }: { tone: Tone; n: string; title: string; children: ReactNode }) {
  return (
    <div className={`lp-fact is-${tone}`}>
      <b>{n}</b>
      <p>
        <strong>{title}</strong>
        {children}
      </p>
    </div>
  );
}

function SectionHead({ label, title, children }: { label: string; title: string; children?: ReactNode }) {
  return (
    <div className="lp-section-head">
      <p className="lp-label">{label}</p>
      <h2>{title}</h2>
      {children && <p>{children}</p>}
    </div>
  );
}

function Step({ tone, flipped, tag, title, shot, children }: { tone: Tone; flipped?: boolean; tag: string; title: string; shot: ReactNode; children: ReactNode }) {
  return (
    <div className={`lp-step${flipped ? " is-flipped" : ""} is-${tone}`}>
      <div className="lp-step-copy">
        <span className="lp-tag">{tag}</span>
        <h3>{title}</h3>
        {children}
      </div>
      {shot}
    </div>
  );
}

function Cell({ tone, title, children }: { tone: Tone; title: string; children: ReactNode }) {
  return (
    <div className={`lp-cell is-${tone}`}>
      <h3>{title}</h3>
      <p>{children}</p>
    </div>
  );
}

function Can({ scope, children }: { scope: string; children: ReactNode }) {
  return (
    <li>
      {children}
      <code>{scope}</code>
    </li>
  );
}
