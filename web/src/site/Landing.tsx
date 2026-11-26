import { useState, type FormEvent } from "react";
import { createPortal } from "react-dom";

const repoURL = "https://github.com/shutthegoatup/prospector";

const perUser = 5;
export const hostedPrice = `£${perUser}`;

function Brand() {
  return (
    <a className="lp-brand" href="/">
      <img src="/assets/img/mark.svg" width={30} height={30} alt="" />
      <span>Prospector.</span>
    </a>
  );
}

function Shot({ file, width, height, alt, tone }: { file: string; width: number; height: number; alt: string; tone: string }) {
  return (
    <div className={tone ? `lp-shot ${tone}` : "lp-shot"}>
      <img src={`/assets/img/${file}`} width={width} height={height} loading="lazy" alt={alt} />
    </div>
  );
}

// Landing is the front page, drawn when the site is built. It has no enquiry form:
// main.tsx mounts one in its place where the server has somewhere for it to send.
export function Landing() {
  return (
    <>
      <header className="lp-hero">
        <img className="lp-contours" src="/assets/img/contours.svg" alt="" />
        <div className="lp-wrap">
          <nav className="lp-nav" aria-label="Site">
            <Brand />
            <div className="lp-links">
              <a href="#loop">How it works</a>
              <a href="#customers">Customers</a>
              <a href="#agents">Agents</a>
              <a href="#pricing">Pricing</a>
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
                Prospector is a workbench for teams selling to named accounts, from the first fact about a company to its signed agreement. Agents research and
                draft. People qualify, decide and send.
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
                <img src="/assets/img/today.webp" width={1440} height={900} alt="The Today page: everything due, best first, with a Do now list beside them" />
              </div>
              <span className="lp-chip is-green a">Tue 16:39 in London: good time to call</span>
              <span className="lp-chip is-violet b">agent · research-agent</span>
            </div>
          </div>
        </div>
      </header>
      <div className="lp-wrap">
        <div className="lp-facts">
          <div className="lp-fact is-coral">
            <b>1</b>
            <p>
              <strong>queue a day</strong>across LinkedIn, email and phone.
            </p>
          </div>
          <div className="lp-fact is-violet">
            <b>0</b>
            <p>
              <strong>messages sent by agents</strong>They draft. Only people send.
            </p>
          </div>
          <div className="lp-fact is-green">
            <b>1</b>
            <p>
              <strong>source link per fact</strong>An agent's fact without one is refused.
            </p>
          </div>
        </div>
      </div>
      <section className="lp-section" id="loop">
        <div className="lp-wrap">
          <div className="lp-section-head">
            <p className="lp-label">How it works</p>
            <h2>From a company name to a signed deal, in three pages.</h2>
            <p>Research fills the list. Today works it. Conversations closes it. The screenshots show invented companies and people.</p>
          </div>
          <div className="lp-step is-green">
            <div className="lp-step-copy">
              <span className="lp-tag">01 · Research</span>
              <h3>Facts with the link they came from</h3>
              <p>
                Say who you're looking for and send an agent after them. Ask for about so many companies and watch how far the search has got. What it finds
                that you already have is left out, and nothing reaches outreach until someone qualifies it.
              </p>
              <p>
                Sell more than one thing? Each offering gets its own look-for list and queue, so a company that is wrong for one can still be right for another.
                Every fact keeps its source.
              </p>
            </div>
            <Shot
              file="research.webp"
              width={1440}
              height={900}
              alt="Research: a brief for an agent at the top, its search, and two companies it found, each with sourced facts"
              tone=""
            />
          </div>
          <div className="lp-step is-flipped is-gold">
            <div className="lp-step-copy">
              <span className="lp-tag">02 · Today</span>
              <h3>One queue, best first</h3>
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
            </div>
            <Shot file="linkedin.webp" width={1440} height={900} alt="The LinkedIn queue: the connection note ready to copy, with keyboard shortcuts" tone="" />
          </div>
          <div className="lp-step is-coral">
            <div className="lp-step-copy">
              <span className="lp-tag">03 · Conversations</span>
              <h3>Replies become deals</h3>
              <p>Replies move across a board: replied, meeting, proposal, won, lost.</p>
              <p>Each one needs a next step with a date. Any without one, or due today, come back to the top of Today.</p>
            </div>
            <Shot file="conversations.webp" width={1440} height={560} alt="Conversations: a board from replied to won" tone="" />
          </div>
        </div>
      </section>
      <section className="lp-section" id="customers">
        <div className="lp-wrap">
          <div className="lp-section-head">
            <p className="lp-label">After it's won</p>
            <h2>The customer stays on the same record.</h2>
            <p>
              Deals, the contract, what they're on, getting them live, what they write in about and what you write to them, all on the company you
              prospected. Each has its operations in the published API.
            </p>
          </div>
          <div className="lp-grid">
            <div className="lp-cell is-coral">
              <h3>Deals with a value</h3>
              <p>Each conversation carries what it's worth, who has it and when it should close. Start a second one for a renewal or an expansion.</p>
            </div>
            <div className="lp-cell is-violet">
              <h3>Agreements, ready to sign</h3>
              <p>
                Upload the contract as a PDF. It is kept exactly as uploaded and checked against its hash on every download. Choose who signs and in what
                order, and drag their fields onto the pages. Sending for signature comes next.
              </p>
            </div>
            <div className="lp-cell is-gold">
              <h3>Subscriptions and their notice</h3>
              <p>What each customer is on, for how long, and the notice it needs. Today flags one a month before its date, and a renewal deal starts from it.</p>
            </div>
            <div className="lp-cell is-green">
              <h3>Onboarding to go-live</h3>
              <p>
                Your steps and theirs, dated from the start. The customer gets a page of their own steps from an emailed link, no password, and marks theirs
                done. Late steps come back to Today.
              </p>
            </div>
            <div className="lp-cell is-coral">
              <h3>Enquiries answered</h3>
              <p>
                Site forms and your product's sign-ups arrive open on the card, to be given an owner. Reply by email from it, and paste their answer into the
                thread.
              </p>
            </div>
            <div className="lp-cell is-gold">
              <h3>Campaigns to people you know</h3>
              <p>
                Write once to everyone in conversation or at a current customer, never to cold prospects. It goes through your own mail server with your postal
                address and a one-click unsubscribe, and nobody is mailed twice. A service notice, such as a price change, reaches customers who unsubscribed
                from marketing.
              </p>
            </div>
          </div>
        </div>
      </section>
      <section className="lp-night" id="agents">
        <div className="lp-wrap">
          <div className="lp-section-head">
            <span className="lp-tag is-violet">Agents and API</span>
            <h2>Agents do the reading. You decide what they may send.</h2>
            <p>
              Each organisation has an MCP endpoint and a REST API with scoped keys. Connect Claude or your own agent in one command, and decide what each key
              may do. Or let Prospector run the agents itself, through Claude, any OpenAI-compatible endpoint or a model you host, with free search from your
              own SearXNG. Each agent's card says whether it is ready, and why not.
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
              <Shot
                file="agents.webp"
                width={1440}
                height={765}
                alt="Agents: each of Prospector's agents on a card saying whether it is ready, with its model, search, tools and fallback"
                tone="is-violet"
              />
            </div>
            <div className="lp-rules">
              <div>
                <h3 className="lp-tag is-green">A key can</h3>
                <ul>
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
              <div>
                <h3 className="lp-tag is-coral">No key can</h3>
                <ul>
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
            </div>
          </div>
        </div>
      </section>
      <section className="lp-section" id="team">
        <div className="lp-wrap">
          <div className="lp-section-head">
            <p className="lp-label">Built for a small team</p>
            <h2>The details that keep a list clean.</h2>
          </div>
          <div className="lp-grid">
            <div className="lp-cell is-coral">
              <h3>A never-contact list that holds</h3>
              <p>
                Domains, companies, email addresses and LinkedIn profiles. A never-contact company stays on file but out of review and outreach. It applies to
                agents, imports and edits alike.
              </p>
            </div>
            <div className="lp-cell is-gold">
              <h3>Calls at a sensible hour</h3>
              <p>Office hours come from the contact's phone number, so Zurich gets called when Zurich is at work.</p>
            </div>
            <div className="lp-cell is-green">
              <h3>The full history</h3>
              <p>Every company shows who did what and when, people and agents alike.</p>
            </div>
            <div className="lp-cell is-violet">
              <h3>Several organisations, one login</h3>
              <p>
                Each has its own prospects, wording, team and keys. None can see another's. Sign in with a password, Google, Microsoft, LinkedIn or your
                organisation's own single sign-on.
              </p>
            </div>
            <div className="lp-cell is-coral">
              <h3>Bring your own files</h3>
              <p>Import companies from CSV and drafts from markdown. Re-importing never clears a date or an edit made in the app.</p>
            </div>
            <div className="lp-cell is-gold">
              <h3>Your data stays yours</h3>
              <p>
                Host it yourself on SQLite or Postgres, or let us. An owner can download everything the organisation holds as one file, and erase a person who
                asks to be forgotten.
              </p>
            </div>
          </div>
        </div>
      </section>
      <Pricing />
      <section className="lp-install" id="start">
        <div className="lp-wrap lp-install-grid">
          <div>
            <p className="lp-label">Self-host</p>
            <h2>Set up in four commands.</h2>
            <p className="lp-lede">
              Prospector is one program for Linux or macOS. Download it, name your organisation, add yourself and start working the list.
            </p>
            <div className="lp-cta">
              <a className="lp-btn is-primary" href={`${repoURL}/releases/latest`}>
                Download Prospector ↗
              </a>
              <a className="lp-btn" href={`${repoURL}#readme`}>
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
      <div id="enquiry" />
      <footer className="lp-foot">
        <div className="lp-wrap">
          <Brand />
          <span>Prospects and customers for small teams with a named list.</span>
          <nav aria-label="Footer">
            <a href="#pricing">Pricing</a>
            <a href={repoURL}>GitHub</a>
            <a href={`${repoURL}/releases`}>Releases</a>
            <a href="/login">Sign in</a>
          </nav>
        </div>
      </footer>
    </>
  );
}

function Pricing() {
  return (
    <section className="lp-section" id="pricing">
      <div className="lp-wrap">
        <div className="lp-section-head">
          <p className="lp-label">Pricing</p>
          <h2>Run it yourself for nothing, or let us run it.</h2>
          <p>Both are the same program with every feature. The only difference is who looks after the server.</p>
        </div>
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
            <div className="lp-cta" id="hosted-cta" />
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

// EnquiryForm is the front page's contact section, with its link in the page's nav and
// the hosted plan's buttons, which lead nowhere where there is no form.
export function EnquiryForm({ intake, nav, hosted }: { intake: string; nav: Element | null; hosted: Element | null }) {
  const [sending, setSending] = useState(false);
  const [status, setStatus] = useState("");
  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    const f = new FormData(form);
    const field = (name: string) => String(f.get(name) ?? "").trim();
    const body = JSON.stringify({
      kind: field("kind"),
      name: field("name"),
      email: field("email"),
      company: field("company"),
      message: field("message"),
      trap: field("ref_note"),
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
      {hosted &&
        createPortal(
          <>
            <a className="lp-btn is-primary" href="#contact">
              Start hosted
            </a>
            <a className="lp-btn" href="#contact">
              Ask a question
            </a>
          </>,
          hosted,
        )}
      <section className="lp-contact" id="contact">
        <div className="lp-wrap lp-contact-grid">
          <div>
            <p className="lp-label">Contact</p>
            <h2>Ask us anything.</h2>
            <p className="lp-lede">Hosting, a question, a demo, or help getting it running. Someone will write back to the address you give.</p>
          </div>
          <form className="lp-form" onSubmit={submit}>
            <label>
              Your name
              <input name="name" autoComplete="name" maxLength={200} />
            </label>
            <label>
              Email
              <input name="email" type="email" autoComplete="email" maxLength={200} required />
            </label>
            <label>
              Company
              <input name="company" autoComplete="organization" maxLength={200} />
            </label>
            <label>
              What it's about
              <select name="kind" defaultValue="sales">
                <option value="signup">Hosted Prospector</option>
                <option value="sales">A question</option>
                <option value="demo">See it running</option>
                <option value="support">Help running it</option>
                <option value="feature">Something it should do</option>
              </select>
            </label>
            <label className="is-wide">
              Message<textarea name="message" rows={5} maxLength={5000} required></textarea>
            </label>
            <label className="lp-trap" aria-hidden>
              Leave this empty
              <input name="ref_note" tabIndex={-1} autoComplete="off" />
            </label>
            <div className="lp-form-foot is-wide">
              <button className="lp-btn is-primary" type="submit" disabled={sending}>
                Send
              </button>
              <p className="lp-form-status" role="status">
                {status}
              </p>
            </div>
          </form>
        </div>
      </section>
    </>
  );
}
