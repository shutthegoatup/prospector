# Prospector

**A self-hosted, account-based prospecting workbench for small teams selling to a few hundred named accounts.**

People and agents research companies, keeping a source link for every fact. People then work one daily queue across LinkedIn, email and phone, from first touch to deal. Agents research and draft over MCP; only people send.

One binary, one database (SQLite or Postgres), several organisations per login.

[![CI](https://github.com/shutthegoatup/prospector/actions/workflows/ci.yml/badge.svg)](https://github.com/shutthegoatup/prospector/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/shutthegoatup/prospector)](https://github.com/shutthegoatup/prospector/releases/latest)

![Today: everything due, best first, with a "Do now" list beside it](docs/images/today.png)

<sub>Screenshots use invented companies and people.</sub>

## What it does

### Today: one queue, best first

Everything due, in this order:
1. conversations needing a next step;
2. calls where it's office hours for the contact, from their phone number's time zone;
3. LinkedIn messages, emails and follow-ups;
4. LinkedIn adds.

The side panel says what to do now, and counts the last week's touches, replies and meetings.

On LinkedIn adds, `o` opens the profile and copies the note, `a` marks it added and `s` skips it. That's the whole loop for working through a list:

![The LinkedIn queue: the note ready to copy, with keyboard shortcuts](docs/images/linkedin.png)

### Research: facts with sources

Companies found by agents, imports and people wait here to be qualified or rejected. Every fact keeps the link it came from. "Pick" moves a suggested person into outreach, with a note from your template. Rejecting a company takes its people out of the outreach queues on Today; qualifying it again brings them back as they were. Someone already in a conversation stays on the board.

![Research: two companies an agent found, with sourced facts and a suggested contact](docs/images/research.png)

### Find: write a brief, hand it to an agent

At the top of Research, write who you're looking for, tick which of Prospector's own agents go after it, and press Find. The brief becomes a task: its search shows above the queue with who is working on it and what it has found, and "Show these" narrows the queue to its companies, beside the agents' notes and a box to ask for more. A task's own page keeps every request, note and run.

Write what you want found. An agent picks the task up and what it finds waits in Research, tied to the task. Anyone can ask for more later, and a different agent can carry on: it is shown the brief, every request, what has already been found and what a person rejected, and the notes earlier agents left on what they skipped.

Your own agent starts on a task from the prompt on its page (clients that show MCP prompts as commands have it as `work_task`). Or Prospector works it with its own agents: see [Prospector's own agents](#prospectors-own-agents).

A task shows as waiting, being worked on, or done. Nothing an agent says while working is stored: its status and results are read from what it added. Agents often stop without saying so, so each request shows what has been added since it was asked, and you can mark one done or reopen it yourself.

An agent that picks a task up is kept the oldest open request no other agent has, so two agents started on the same task take different requests. The claim lasts 15 minutes, is renewed by everything the agent adds for the task, and ends when the request is done. It lapses by itself if the agent stops. It is advice to other agents: what anyone adds is still accepted.

### Conversations: replies to deals

Replies move across a board: replied, meeting, proposal, won, lost. Each needs a next step with a date. Ones without, or due today, come back on Today.

A conversation can carry a deal: what it's worth, when you expect it to close, whether it's new business, a renewal or an expansion, and which teammate owns it. Each stage then shows what its deals add up to. Values are in the currency set in Settings (GBP unless you change it).

A renewal or an expansion with someone already on the board is a deal of its own: start one from their card and it gets its own card, stage, next step and value, beside the first. One started by mistake can be removed; one that ran its course is marked won or lost.

![Conversations: a board from replied to won](docs/images/conversations.png)

### Agents and API

Each organisation has an MCP endpoint and a REST API, with scoped keys. Agents can search, add companies, facts and suggested contacts, and draft messages. They send mail or change settings only with a scope that says so.

![Agents & API: the MCP endpoint, REST calls and the tools each key gets](docs/images/agents.png)

### Onboarding: the steps to a live customer

An owner writes the steps a new customer goes through under Settings, one a line, in order: who does it (`them:` or `us:`), what it is, and how many days after the start it is due, such as `them: Send your product files, 7 days`.

Start an onboarding from a company's page and it gets its own copy of those steps, dated from the day it starts. Tick each off as it is done, or move its date; the page says who finished it and when. A company can have several, one a site or a product. Onboarding lists everyone being set up, the ones running late first, and Today points at steps that are late or due. A customer is live once every step is done. Changing the steps in Settings later leaves the ones already running as they are.

**The customer's page.** From an onboarding you can invite someone at the company, who has an email address here, to a page of their own. They are emailed a link; nobody outside gets an account or a password. The page shows the steps that are theirs and whether each is done, and nothing else: not your own steps, not your teammates' names. They can mark their own steps done, and take back a mark they made; it shows on the onboarding and in the company's history as the customer's, with the person who did it. A step you marked stays as you left it.

- A link opens the page once, within 7 days, in the browser it is used in, which then keeps it for 30 days. Looking at a link does nothing until its button is pressed, because mail scanners follow links.
- Another browser needs a link of its own. A used or expired link offers to send a fresh one, which goes only to the address that was invited, at most once every five minutes.
- Withdraw someone's access from the onboarding and their page closes in every browser at once. Removing the onboarding, or erasing the person, does the same.
- Inviting needs a mail server (the organisation's own, in Settings, or the server's) and `PROSPECTOR_URL`, since a link is never built from what a request says its host is.

### Subscriptions: what a customer is on

Once a company is a customer, record what they are on from its page: what it is, the price for a month or a year, when it starts, how many months they committed for, what happens when that is up (it renews, rolls on a period at a time, or ends) and how many days' notice ends it. A company can have several, each with its own dates.

Subscriptions lists them all, the one that needs someone soonest first, with the day the term ends and the last day to give notice, and what is contracted for the year. Notice given, or an early end, is recorded on the subscription and it works out the day it stops. Nothing here takes payment or sends an invoice, and a subscription belongs to the company, so erasing a person leaves it.

### Agreements: the contract as a PDF

From a company's page, start an agreement and upload the contract as a PDF, 10 MB at most. Prospector keeps the file exactly as uploaded with its SHA-256, checks it against that hash whenever it is downloaded, and always hands it back as a download. It doesn't read what the contract says, and it is not for deeds or anything that must be witnessed or notarised. A PDF that is password-protected, encrypted or already signed is refused, as it couldn't later be filled in and sealed.

**Prepare for signing** sets out who signs and where. Add signers from the company's people (one email address each, not on the never-contact list) or from your members, in the order they sign or all at once, and anyone else who gets a copy. Then place each signer's fields on the pages (signature, initials, name, date signed, text, tick box) by dragging them on. The page shows what is still missing before it could go. Recipients and fields are in the API (`agreements:write`), and erasing a person or removing a member takes them off a draft.

**Send for signature** (`agreements:send`) freezes who it goes to and where each signs, and emails the first signer (or all of them, when they sign in any order) a link through the organisation's own mail server, for 1 to 60 days. It is refused while anything is missing, anyone on it is on the never-contact list, or a dry run of sealing fails. Someone outside the organisation opens their link, asks for a code emailed to the same address, enters it, reads the document, fills in their places and types their name to sign; nothing is shown of it but its name before the code. A member signs their own place signed in; no API key can sign, whatever its scopes. Each signer can decline, with a reason the sender is emailed. The sender can withdraw it, or email a signer their link again, until the last one signs.

When the last one signs, Prospector seals it: each signer's places are drawn onto the pages, a **certificate of signing** is added as the last page (who signed, how they proved who they were, from which address and browser, and everything that happened, in order), and the whole is signed with the organisation's own key, made on first use and kept under the server's secret key. Everyone on it is then emailed a link to the sealed copy, good for 90 days. A PDF reader's signature panel shows whether it has changed since. These are simple electronic signatures: an emailed code or a sign-in, and a typed name. A lawyer should look at whether that is right for your agreements.

What happens to an agreement once it is sent is kept as a chain of events, each holding the hash of the one before, so a change after the fact shows. Erasing someone it went to withdraws it if it is still out, and keeps the record with them left out of exports. The organisation's export carries each agreement, its hash, its recipients, fields and events, not the files.

### Also

- **Companies:** search, plus each company's people, facts and full history: who did what, people and agents alike.
- **Take everything with you:** an owner can download all the organisation holds from Settings (or `prospector export-org <file>`): companies, people, facts, enquiries, history, settings and who has access, as one JSON file. It holds no passwords (a mail server's included), API keys or sign-on secrets, and nothing that identifies people you erased.
- **Export a person:** when someone asks what you hold on them, an owner can download it from their company's page (or `prospector export-person <name> <file>`): their details, drafts, notes, the facts recorded against them, what they wrote to you and their history. The history names the teammates and agents who acted, so read it through before you send it on. The export is itself noted in the history.
- **Erase a person:** an owner can erase someone from their company's page (or `prospector erase <name>`) when they ask to be forgotten. Their details, drafts, notes and facts are deleted, the history keeps only that something happened and when, and an import, a restore or an agent can't add them again. A row that names several people goes whole. Agents' notes about their company, and on any task that touched it, are deleted too, since free text can't be searched for a name. Company facts and other people's notes that mention them, the briefs and requests people wrote on tasks, your own lead files and database backups aren't touched: take them out there too. What stays, to keep them from being added again, is a hash of their name at that company, each address and their profile, salted with the organisation. It names nobody by itself, but someone holding a copy of the database could test a guessed name, address or profile against it.
- **Add person:** paste a LinkedIn profile link and the name fills in. A bookmarklet opens the same form pre-filled from the profile you're viewing.
- **Settings, per organisation:** waits between steps, connection-note template, call-script pitch, never-contact list, what you look for at a company and team. Each member sets their own caller name and signature.
- **What you look for:** each organisation names the things that make a company a fit, one per line in Settings, such as `configurator: a product configurator on their site` or `careers_page: a jobs page`. Put `(must)` after the name of anything a company is no fit without. Agents are told the list and answer each with a fact under that name: `yes` with the page that shows it (the default), `no` with the page that shows they don't, or `unknown` when they looked and couldn't tell, saying where they looked; `inferred` marks an answer read from a job advert, a third party or an old page rather than their own site. Research shows each answer with its source and a fit such as "2 of 3 · 1 not found", and a search's companies to review come best fit first: none missing a must, then most met, then fewest left unknown. The latest answer for a thing stands.
- **Several organisations per login:** each has its own prospects, wording, team and keys, and none can see another's.

## Run it

```sh
go install github.com/shutthegoatup/prospector@latest   # or download a release
prospector org add acme "Acme"
prospector user add you@example.com "Your Name"          # asks for a password
prospector member add acme you@example.com owner
prospector serve                                          # http://127.0.0.1:8766
```

| Variable | Meaning |
|---|---|
| `DATABASE_URL` | A `postgres://` URL or a SQLite file path (default `prospector.sqlite`) |
| `PORT` | Listen on `:PORT` instead of `127.0.0.1:8766` |
| `PROSPECTOR_URL` | The address people reach it at, e.g. `https://prospector.example.com`. With an `https://` address the session cookie is always marked Secure, and links shown in the app use this address. Set it if a proxy in front changes the `Host` header |
| `SMTP_ADDR`, `MAIL_FROM` | The mail server the app's own mail leaves through, e.g. `smtp.example.com:587` and `Prospector <prospector@example.com>`. Set both or neither; see [Mail](#mail) |
| `SMTP_USERNAME`, `SMTP_PASSWORD` | What that server asks for, if anything |
| `PROSPECTOR_SECRET_KEYS` | The server's secret keys, newest first: `2:<64 hex>,1:<64 hex>`. Needed for an organisation's single sign-on and for a mail server of its own (see [Secret keys](#secret-keys)) |
| `PROSPECTOR_GOOGLE_CLIENT_ID`, `PROSPECTOR_GOOGLE_CLIENT_SECRET` | Offer "Continue with Google" on the sign-in page. The same pair exists for `MICROSOFT` and `LINKEDIN`. Each needs `PROSPECTOR_URL` (see [Signing in](#signing-in)) |
| `PROSPECTOR_SECURITY_CONTACT` | Where to report a security problem with this server, as a `mailto:`, `https://` or `tel:` address. It is served as the `Contact` of `/.well-known/security.txt`, which isn't served without it |
| `TZ` | The time zone "today" is counted in |
| `PROSPECTOR_ORG` | Default organisation for CLI commands (`--org=` overrides it) |
| `PROSPECTOR_KEY` | API key for `push` and `pull` |

Search uses a trigram index. On Postgres that needs the `pg_trgm` extension, which is created on start-up if the database user may; without it search still works, by scanning.

**Before exposing it beyond your machine:**
- Put it behind HTTPS. Copy to clipboard needs a secure page, and the session cookie is marked Secure on HTTPS. Set `PROSPECTOR_URL` to the `https://` address so the cookie is always marked Secure; without it, that depends on the proxy sending `X-Forwarded-Proto: https`.
- Pages carry a Content-Security-Policy that allows only what the app itself serves. The typefaces are bundled, so a visitor's browser contacts no other site.
- A request's body has five minutes to arrive and its answer two minutes to be read, so a stalled client doesn't hold a connection open. An agent's MCP stream, once its key is accepted, stays open.
- Repeated wrong passwords for one email are slowed, up to a minute between tries. A browser that has signed in to that account before is counted separately, so someone guessing elsewhere can't hold its owner up.
- An owner adds and removes members in Settings. Removing someone ends their sign-in access to that organisation; their account, and other organisations, are untouched. API keys they created keep working until revoked on the Agents page. If the organisation lets people join through its single sign-on, someone removed here joins again while its provider still lets them in: remove them there too.
- Everyone in an organisation reads all of it. A member changes only the work of their areas: **sales** (research, outreach, deals and tasks for agents) or **support** (enquiries and their replies, subscriptions and onboarding). An owner ticks a member's areas on the Team list, and a member with none reads only. Owners have every area, and API keys go by their scopes alone.
- Someone added from Settings gets a first password that the person adding them sees; they are made to choose their own the first time they sign in.
- With [mail](#mail) set up, whoever reads a member's mailbox can choose that member's password. Without it there is no reset but `prospector user password`.
- Members change their own password in Settings, which signs out their other sessions. `prospector user password` signs them out everywhere.
- Every page needs a sign-in except the sign-in pages and the front page at `/`, which describes Prospector to a signed-out visitor and shows nothing from your data. The single sign-on page does name the organisations that have verified an email domain to anyone who types an address at it. Every browser POST must be JSON, which blocks cross-site form posts, and a request that a browser marks as coming from another site is refused, sign-in and sign-out included.
- The one exception is the enquiry route, `/o/<slug>/intake`, which exists to be posted to from your own sites. It answers only the sites an owner lists in Settings, reads no session, and can add an enquiry and nothing else. With no sites listed it refuses everything. The front page's own contact form posts there too, for the `prospector` organisation. A server with `PROSPECTOR_URL` set makes that organisation with its own address listed, so there the route is open from the first start: up to 300 enquiries an hour are kept, read by nobody until someone is added to it. Remove the address from its sites if you don't want the form.
- Asked to stop (`SIGTERM`, or Ctrl-C), the server takes no new connections and gives requests in hand, and mail on its way out, ten seconds to finish. An agent's MCP stream is cut when that runs out, and the agent has to reconnect and start a new session. Prospector's own agents aren't waited for: a run in hand loses the turn it was in and carries on, here or on another server, within about three and a half minutes. Asking a second time stops the server at once.
- More than one server can share a Postgres database. Sign-ins, API keys, which agent holds a request and Prospector's own agents' runs are all kept there, so any server can answer for them, as long as every server is given the same `PROSPECTOR_SECRET_KEYS`. Without them, a sign-in through Google, Microsoft, LinkedIn or an organisation's provider has to come back to the server it started on. Two things are each server's own:
  - its limits: the slowing of wrong passwords, the hourly limit on enquiries, and how many of Prospector's own agents run at once are counted per server, so two servers allow twice as many;
  - an agent's MCP session, which lives on the server that opened it, so a load balancer has to keep sending `/o/<slug>/mcp` for one client to the same server.

  A restart forgets both, as it does with one server. A SQLite file is for one server.

There's a Dockerfile for container hosting.

## Mail

The app mails its own members; people outside an organisation when someone in it asks for that one mail; and the people a campaign is sent to. It never sends outreach to prospects: people do that from their own mailbox.

- **Password resets.** The sign-in page offers "Forgot your password?", which emails a link that lasts an hour and works once.
- **Invitations.** Someone an owner adds is emailed a link to choose their password, good for 7 days, instead of the owner being shown a first password to pass on.
- **A customer's link to their onboarding steps**, when a member or a key with `customers:invite` asks. See [the customer's page](#onboarding-the-steps-to-a-live-customer).
- **A reply to an enquiry**, when a member or a key with `enquiries:reply` writes one. See [replying](#enquiries-website-forms-and-sign-ups).

**Campaigns.** A campaign is a marketing mail written once for many, under Campaigns or at `/api/v1/campaigns`. It goes only to people you already have something with: everyone in conversation, or everyone at a company whose subscription has started and not ended, whether or not they ever replied. Never to a prospect who hasn't replied, since outreach stays one person writing to one person. A draft shows who it would go to now and who is left out and why: no address, on the list below, on the never-contact list, erased, or sharing an address with someone already on it. Its foot will carry your postal address, set by an owner in Settings, and an unsubscribe link. **Sending** (`campaigns:send`) fixes who it goes to as its audience stands then, and sends it a few dozen a minute through the organisation's own mail server, never the server's: anyone can make an organisation, and the host's mail isn't theirs to send marketing through. It needs that mail server to be one you sign in to, with a username, on port 587, 465 or 2525 (never port 25 straight to a recipient's server, which would make this host a relay), the postal address and an `https` `PROSPECTOR_URL`. One organisation sends at most 2,000 campaign mails a day; past that the rest wait for the next day. Each mail is checked again as it goes, so someone who unsubscribes meanwhile gets nothing, and carries the postal address and a one-click unsubscribe link. Nobody is mailed twice: each mail is claimed just before it goes, and one that may have gone when the mail server stopped answering is marked unknown and never sent again. When three mails in a row aren't plainly sent (the server can't be reached, refuses the address, or doesn't confirm it) the campaign pauses, with the reason, until someone resumes it; it can be cancelled at any time. A server that puts an address off for now (a 4xx) has that mail tried again. Prospector doesn't sign mail with DKIM (your mail server does) and doesn't hear about bounces after the server took a mail.

**Service notices.** A campaign can instead be a service notice: about what customers already have, such as a price or terms change, an outage or a date that matters. It goes only to everyone at a current customer, reaches people who unsubscribed from your marketing, and carries no unsubscribe link; its foot says it came because they are a customer. It never goes to anyone on the never-contact list or erased. Keep it to the service: anything promotional in it makes it marketing, whatever it is called, and the law treats it so.

**Not to be mailed.** Each organisation keeps one list of addresses its marketing mail never goes to, under Settings and at `/api/v1/suppressions`. Someone who unsubscribes by the link in a mail is put on it and stays there: no member or key can take them off. An address a member or key added can be taken off again. Marketing mail is also kept from anyone on the never-contact list and anyone erased: erasing someone deletes their address from the list, and the erasure's own record of it keeps them unmailed.

The unsubscribe link, `/u/<token>`, is one per mail and never runs out. Opening it shows a button and changes nothing, since mail scanners follow links. Pressing it, or a mail provider's one-click request ([RFC 8058](https://www.rfc-editor.org/rfc/rfc8058): a form `POST` with no cookie, answered without a redirect), unsubscribes the address it was sent to from that organisation's marketing mail. The page never says whose link it is, because mail gets forwarded. The one-click headers go only into a mail whose link is `https`.

Both need `SMTP_ADDR`, `MAIL_FROM` and `PROSPECTOR_URL`: a link is only ever built from the address you gave, never from what a request claims. With any of them missing there is no reset and first passwords are shown as before. `prospector mail test you@example.com` sends a message through the server you set.

Any SMTP submission service that takes a username and password (`AUTH PLAIN`) works, a transactional mail provider's included. Mail goes over TLS (STARTTLS, or implicit TLS on port 465) or not at all; only a server on the same machine is spoken to in the clear.

**An organisation can use a server of its own**, set by an owner under Settings → Outgoing mail. Invitations to that organisation then leave through it, from its address. Resets belong to the account, not to an organisation, so they always use the app's. The password is kept sealed under the server's [secret key](#secret-keys), which must be set first, and is never shown again. The server must be on port 587, 465, 2525 or 25. It carries an invitation only for an account that the invitation itself creates: whoever runs a mail server reads what passes through it, so someone who already has an account, and hasn't chosen a password yet, is sent the link by the app's own server or not at all. A second invitation to the same person waits five minutes and ends the first.

A reset link gives a password to an account that had none because it signs in through a provider. An organisation that requires its single sign-on refuses that password as it does any other.

## Signing in

Everyone can sign in with an email and password. Two more ways can be turned on, both OpenID Connect, and both needing `PROSPECTOR_URL`: the provider sends people back to `<PROSPECTOR_URL>/login/callback`, which is the redirect URI to register with it.

### Google, Microsoft and LinkedIn, for the whole server

Whoever runs the server registers an app with the provider and sets its `PROSPECTOR_<PROVIDER>_CLIENT_ID` and `_CLIENT_SECRET`. The sign-in page then offers it to everybody.

- It signs in people who already have an account, matched the first time by an email address the provider has verified and after that by the provider's own ID for them. It never creates an account.
- Microsoft's `email` claim is whatever a tenant's admin typed, so an address counts only when the token carries `xms_edov`. Add `xms_edov` to the ID token's optional claims in the app registration, or nobody can be matched.
- LinkedIn is asked without PKCE or a nonce, which it doesn't document.

### An organisation's own single sign-on

An owner sets this up in Settings, for Okta, Entra ID, Google Workspace or any other OpenID Connect provider:

1. **Add the email domains that are yours** and prove each with the DNS TXT record Settings shows. The provider is believed only for addresses at a verified domain, matched whole.
2. **Enter the issuer, client ID and client secret.** The issuer is read before anything is saved. The secret is kept sealed under the server's [secret key](#secret-keys), which must be set first, and is never shown again; it has to be typed again when the issuer or client ID changes.
3. Optionally **require it**: members at your domains can no longer open the organisation on a password. Owners still can, so a broken provider can be put right, and so can members at other domains, whom your provider couldn't sign in. API keys are unaffected. If no owner can get in, `prospector sso off <org>` stops requiring it.
4. Optionally **let people join**: anyone at your domains becomes a member, never an owner, the first time they sign in. Without this, the provider signs in only people an owner has already added.

People start from "Use your organisation's single sign-on" on the sign-in page and give their work address.

- A session that began at an organisation's provider opens that organisation only, since the provider vouches for nothing else. Someone already signed in who goes through it keeps their session.
- Going through the provider counts for a day, then it is asked again. Someone the provider has turned away is out within that day; to end it sooner, remove them from the team.
- With Google as the provider, the account must also be in a Workspace for that domain (`hd`). An issuer that signs in every tenant under one registration, such as Microsoft's `/common`, is refused: use your own tenant's.
- An issuer must be `https` at a public address; one inside the server's network is never contacted.
- Someone invited with a first password who arrives through a provider instead isn't asked to change it: that password stops working.
- Each change to the connection, and each person who joins through it, is recorded in the organisation's events with who and when. SAML and SCIM aren't supported.

## Secret keys

`PROSPECTOR_SECRET_KEYS` holds the server's master keys, each with a version: `1:<64 hex characters>`. `prospector secret-key` prints a first entry, and `prospector secret-key 2` the one to replace it with. It is the only secret of its own the server is given.

- **A master key is never used directly.** Each job gets its own key derived from it with HKDF-SHA256 (RFC 5869).
- **What it seals** is each organisation's single sign-on client secret and the password of its own mail server, each stored as a compact JWE (RFC 7516, `dir` with `A256GCM`) whose `kid` is the key's version.
- **What it keys** is how a session and an API key are found: by an HMAC of the token under the server's key, where without a key it is the token's plain hash. Someone who can write to the database but hasn't the key can't mint a session. An API key is not protected that far: one made before the server had a key is still accepted by its plain hash, because an agent can't sign in again, so a row written with a plain hash is accepted too.
- **What it signs** is the cookie that carries a sign-in to a provider and back.
- **Passwords** are bcrypt hashes and take no part: a key mixed into them couldn't be replaced without everyone choosing a new password.
- **Giving a server its first key signs everybody out**, once. API keys keep working: each is found by its plain hash and moves to the keyed one the first time it is used. That move is one-way: going back to a release from before it, or running a server with no key beside one that has, signs everybody out again and refuses every API key that has been used since, which then have to be made again.
- **Without it** the server runs, but no client secret can be saved, so no new single sign-on connection. A secret stored in clear by an earlier release still works, its connection's other settings can still be changed, and it is sealed the first time the server starts with a key. After that, going back to any release from before sealing breaks those sign-ins until each owner types the secret again.
- **Back it up apart from the database.** A database without the key can't read the secrets; a key that is lost takes them with it, and each owner types theirs again.

The first key in the list seals and signs; every key in the list opens and verifies. A new key always gets a new, higher version: one swapped in under an old version opens nothing, and the server says so in its log when it starts. Only `prospector serve` reads the keys, so a mistyped one doesn't stop `sso off` or `user password`. To replace a key:

1. **Publish.** Add the new key after the current one, `1:<old>,2:<new>`, and restart. Needed only when several servers share the database, so each can open version 2 before any seals with it.
2. **Promote.** Put the new key first, `2:<new>,1:<old>`, and restart. At start-up every stored secret is sealed again under the new key. A server still listing the old key first leaves those alone.
3. **Retire.** Once every server has been restarted with the new key first and its log says `none is under a key older than 2`, remove the old key and restart. Sign-ins in flight under the old key, at most ten minutes old, have to start again. Sessions and API keys move to the new key as they are used; the log counts the API keys that haven't yet, and one whose key is removed first stops working and has to be made again.

## Agents: MCP and REST

Each organisation has an MCP endpoint at `/o/<slug>/mcp` (streamable HTTP) and a JSON API at `/o/<slug>/api/v1/…`. Create keys on the Agents & API page or with `prospector key add <org> <name> <scope>…`.

The API is described by an OpenAPI spec, [`api/openapi.yaml`](api/openapi.yaml), served at `/api/openapi.json` and drawn as a readable contract at `/api`. Both are public, like the front page, since they hold no data. `/llms.txt` lists the same operations for a language model, with links into `/api`; `/robots.txt` and `/sitemap.xml` let crawlers read the public pages and keep them out of an organisation's, and `/.well-known/security.txt` says where to report a security problem once `PROSPECTOR_SECURITY_CONTACT` is set. Prospector's own pages use the same API with the signed-in session, so whatever the app can do a key can be given. The spec is moving over a part at a time; agents and their runs are first.

| Scope | Allows |
|---|---|
| `read` | `search`, `get_company`, `list_due`, `list_tasks`, `get_task`; the Research queue at `/api/v1/research`, each company with its answers and fit, a search's best fit first |
| `read` (API) | also `GET /api/v1/deals`, `/subscriptions`, `/onboardings`, `/enquiries` and `/enquiries/{id}/messages`: deal values, terms, invited contacts, and what people sent in with our notes on it; and `GET /api/v1/settings`, the never-contact list and agent guidance included |
| `research:write` | `add_company`, `add_fact`, `add_contact`; these wait in Research for a person. `task_note` and `finish_request` on tasks |
| `drafts:write` | `draft_message`, shown as written by the agent |
| `import` | `push` and `pull`: a full sync, for a person's own files rather than an agent |
| `outcomes:write` | `mark`: record an outcome (sent, accepted, called, replied, declined, hold), shown as the agent's |
| `enquiries:write` | `POST /api/v1/enquiries`: record an enquiry or sign-up from your own product or site backend |
| `enquiries:reply` | `POST /api/v1/enquiries/{id}/replies` emails a reply to whoever sent an enquiry, through your mail server; `…/answers` records what they said back |
| `customers:write` | record, change and remove subscriptions and start their renewals (`/api/v1/subscriptions`); start, change and remove onboardings (`/api/v1/onboardings`) and mark their steps (`/api/v1/onboarding-steps/{id}`) |
| `customers:invite` | `POST /api/v1/onboardings/{id}/invites` emails a customer a link to their steps, through your mail server; `DELETE /api/v1/onboarding-invites/{id}` withdraws it |
| `campaigns:write` | write campaign drafts and see their audience (`/api/v1/campaigns`); add an address to the list marketing mail never goes to, or take off one that was added (`/api/v1/suppressions`) |
| `campaigns:send` | send, cancel and resume a campaign (`/api/v1/campaigns/{id}/send`, `…/cancel`, `…/resume`), through your own mail server |
| `agreements:write` | start agreements for companies and upload their PDFs (`/api/v1/agreements`, `…/{id}/document`); nothing is sent for signature yet |
| `pipeline:write` | work deals (`/api/v1/deals`, `/api/v1/contacts/{id}/deal`); set an enquiry's state, owner and note (`PATCH /api/v1/enquiries/{id}`) and put its sender on the board (`…/conversation`) |
| `enquiries:triage` | the enquiry half of `pipeline:write` alone: `PATCH /api/v1/enquiries/{id}` and `…/conversation`, for a support agent that shouldn't touch deals |
| `enquiries:delete` | `DELETE /api/v1/enquiries/{id}`: delete an enquiry for good |
| `contacts:write` | Add people straight to outreach (`POST /api/v1/people`) and change their details and where their conversation stands (`PATCH /api/v1/contacts/{id}`) |
| `research:review` | The review a person otherwise does: qualify or reject companies, pick suggested people for outreach, accept or dismiss details agents found. An agent's `research:write` key can't approve its own research |
| `enrich:spend` | `enrich_contact`, and `POST /api/v1/contacts/{id}/enrich`: ask the organisation's enrichment vendors for an email, phone or profile someone lacks, at its cost. What is found waits under **Details to check** |
| `tasks:write` | Start tasks (naming agents to start on one at once needs `agents:run` too), ask them for more, mark their requests done or not, and close and reopen them, through `/api/v1/tasks` and `/api/v1/task-requests` |
| `agents:run` | Start and stop Prospector's own agents on a task, and list a task's runs |
| `agents:configure` | Add, change and delete Prospector's own agents: endpoint, key, model, tools and fallback. Owner-level: a signed-in member needs to be an owner |
| `settings:write` | `PATCH /api/v1/settings`: change the organisation's settings, from its waits and wording to its never-contact list and enquiry sites; send only what changes. `PUT` and `DELETE /api/v1/enrichers` add, change and remove enrichment vendors and their keys. Owner-level, like `agents:configure`. `GET /api/v1/settings` reads them with `read` |
| `team:write` | `POST /api/v1/members`, `PATCH` and `DELETE /api/v1/members/{user}`: add people, change their roles and areas, take them out. Owner-level, and as strong as an owner: it can make one, and a new account's first password comes back when there is no mail to send it by. `GET /api/v1/members` lists the team with `read` |
| `mail:configure` | `GET` and `PUT /api/v1/mail-server`: the organisation's outgoing mail server; its password is write-only. Owner-level |
| `sso:configure` | `/api/v1/sso` and `/api/v1/sso/domains`: the single sign-on provider, its write-only client secret, and the domains it speaks for. Owner-level |
| `org:export` | `POST /api/v1/export`: everything the organisation holds, in one file. Owner-level |

- **Every list comes a page at a time.** Each list in `/api/openapi.json` (agents, runs, the Research queue, deals, enquiries and their messages, onboardings, subscriptions), `search`, `tasks` and `due`, and the lists inside one company (`companies/<id>`: facts, people, history, notes) and one task (`tasks/<id>`: companies, people, notes), take `limit` (50 if left out, 200 at most) and `cursor`. When there is more, the answer has `next_cursor` to pass back as `cursor`, and the HTTP response names the next page in a `Link` header with `rel="next"` ([RFC 8288](https://www.rfc-editor.org/rfc/rfc8288)). A cursor is only good for the list that gave it out. The MCP tools take and return the same two fields.
- **Errors from the JSON API** come as `application/problem+json` ([RFC 9457](https://www.rfc-editor.org/rfc/rfc9457)), with what went wrong in `detail`. `400` is something to fix in the request, `404` a company, contact or task that isn't in this organisation, `409` something that clashes with what is already here, or a person who was erased on request, and `500` the server's own failure, which is worth retrying. A `401` or `403` about the key says which in `WWW-Authenticate`: `invalid_token` for a key to replace, `insufficient_scope` for one that can't do this.
- **What no key can do:** send anything, change settings other than what a scope above names, or see another organisation. A key with `outcomes:write` can record what happened (sent, accepted, replied, declined…); each mark shows as the agent's, with its note, on Today and in the company's history. `import` is the one scope that skips review: it puts people from your own files straight into outreach and marks those in `sent-log.md` as added.
- **Writes are checked:** every fact and contact an agent adds needs a source URL. The never-contact list applies to agents, imports and edits alike: an import that would bring in an address on it is refused whole, though people already stored stay as they are. Someone already stored who falls under the list drops out of the outreach queues on Today and the agents' due list until the entry is removed, and Settings names them; a person who has already replied stays in Conversations. An entry is a domain, an email address or a LinkedIn profile link, matched whole: a company name or a loose word matches nobody.
- **Enrichment vendors, your choice and your keys:** when research can't find someone's email, a key with `enrich:spend` (or a person, through the API) can ask the vendors an owner has added, each under the organisation's own account. Hunter is the first; more are to come. Vendors are asked in the owner's order until one finds it, and each no more than its monthly limit (UTC calendar months; 0 stops it). Nothing about someone on the never-contact list, at a domain on it, or who declined is sent, and a find that is on the list, belongs to someone erased, or is at a domain that takes no mail is dropped. What is found waits under **Details to check**, like an agent's find, and the API and the exports name the vendor that found it. Each lookup is in the person's history, and a vendor isn't asked about the same person and field again for 30 days. There is no page for vendors yet: an owner adds them through the API. Keys are sealed under the server's key, so a server without one can't hold a vendor, and are never shown or exported. The vendor learns the name and company domain of each person looked up, and you are bound by its terms for what it returns.
- **Addresses and numbers are checked:** an address an agent sends through `add_contact` whose domain takes no mail (no mail server in DNS, or one that says it takes none) is turned away, so a made-up domain never reaches Research. The server asks DNS about the domains of addresses already stored too, a few at a time, and keeps each definite answer for a while; a lookup that fails says nothing either way. Today's cards then warn of an address whose domain takes no mail, a shared inbox such as `info@` given for a person, and a phone number written with its country code that no country uses, and a campaign leaves out an address whose domain takes no mail. None of this shows that a person reads an address: only a reply does.
- **Doing it twice is safe:** an agent that runs again, or a second agent on the same company, adds nothing that is already there, and the reply says `existed`. A company matches on its name, or on its domain when one company alone has it; a fact on its field, wording and source page; a person on their profile, address or name at that company. The same fact worded differently is still a second fact, and an email, phone or profile found for a person already there is not filled in: it waits under **Details to check** in Research, and on the company's page, for someone to accept or dismiss. One that was dismissed isn't raised again, and details sent under a different name from the person they matched are left out, since they may be a colleague's. An agent's draft is refused once a person has edited that text or it has gone out.
- **Agents see what came before:** `get_company` returns the company's last 20 history entries (who, what, when, and for whom) and each person's next step, and it and `search` say which agent last changed a company and whether that was the same key. Only writes show: an agent that has looked but not yet added anything is invisible to the others. Free text from the history, such as the note on a mark, is left out.
- **Sessions:** an MCP session works only with the key that opened it.

```sh
claude mcp add --transport http prospector https://<host>/o/<slug>/mcp --header "Authorization: Bearer $PROSPECTOR_KEY"
```

## Prospector's own agents

An organisation can have Prospector work its tasks with models of its choosing: Claude, ChatGPT and open models side by side. An owner adds each on the Agents page, where every agent, Prospector's own and those that connect with a key, has a card whose colour says whether it is ready, missing something such as search, or unable to run, and why. Each has a name and:

- **Endpoint:** any OpenAI-compatible endpoint: its base URL (the one `/models` and `/chat/completions` are under), an API key and a model. Saving reads the endpoint's model list to choose from, and a router model that picks one per request works as well as a single model. OpenAI's is `https://api.openai.com/v1` and OpenRouter's `https://openrouter.ai/api/v1`; an LLM gateway of your own works the same way.
- **Or Anthropic:** an agent of the Anthropic kind reaches Claude through Anthropic's own API with its official Go SDK, given an Anthropic API key. Leave the endpoint empty for Anthropic's own address. Saving reads Anthropic's model list and offers Claude Opus 5.5 first. The conversation is cached between turns, so a long run pays full price only for what is new. On the models that support it, a request Claude declines is passed to another model in the same call (Anthropic's server-side fallback); a decline that still stands ends the run, saying so. The SDK is given the agent's key and nothing else: the server's own `ANTHROPIC_*` settings and profiles are never used.
- **Fallback:** an agent can name another to take its task over when its own endpoint stays limited or away longer than a set time, 15 minutes unless an owner says otherwise. The run waiting ends, saying who it handed to, and the fallback starts on the task as a fresh run that picks up from its notes. With no fallback, a run waits however long its endpoint asks.
- **Search, free:** every agent can read pages with its built-in `fetch_page`, which shows the model a page's title, text and links and follows redirects. Give it a [SearXNG](https://github.com/searxng/searxng) instance you run and it also has `web_search`. SearXNG is free, open-source metasearch, and its JSON answer needs no key, so research needs no paid search service. The instance must list `json` under `search.formats` in its `settings.yml`, and saving checks it with a search. Both tools go through the same public-address guard as everything else an agent reaches; an instance on a private address needs `PROSPECTOR_AGENT_ALLOW_PRIVATE=1`. A busy instance is waited out like any other limit. From the shell, `PROSPECTOR_AGENT_SEARCH_URL` sets it.
- **Tools:** an MCP server whose further tools the agent may use, such as a gateway's federated `/mcp`. Saving lists its tools; the agent uses only the ones ticked, and a tool that appears later starts unticked. The server is sent the endpoint's key only when it is on the same address. LinkedIn's terms forbid automated access, logged-in browsers included, so prefer search and fetch tools to browser and captcha ones.
- **Keys:** sealed under the server's [secret key](#secret-keys), never shown again, and left out of exports. Changing the endpoint needs the key entered again. `prospector agent set <org> <name> <endpoint> <model> [<tools-url> [<tool>…]]` does the same from the shell, adding the agent or changing the one with that name, with the key in `PROSPECTOR_AGENT_API_KEY` and, for Claude, `PROSPECTOR_AGENT_KIND=anthropic` (an empty endpoint, `""`, is Anthropic's own); without a kind, a new agent is OpenAI-compatible and an existing one keeps its own.
- **Thinking off:** a thinking model run locally, such as Qwen behind llama.cpp, can be too slow to think before every tool call. Ticking "thinking off" on an OpenAI-compatible agent sends `chat_template_kwargs: {"enable_thinking": false}`, which llama.cpp and vLLM hand to the model's chat template. From the shell, `PROSPECTOR_AGENT_THINKING=off` sets it and `on` clears it.
- **Deleting** an agent stops its runs and revokes its key. What it found stays, as any agent's does.
- **Addresses:** the endpoint and tool server must be `https` at public addresses. An operator whose gateway sits on a private network sets `PROSPECTOR_AGENT_ALLOW_PRIVATE=1`.

Any member can then press "Run" with an agent's name on a task. Several agents can work one task at once, each on a different open request; one agent works a task once at a time. A run:

- works the open request the task keeps for it, like any agent, under its agent's built-in key (named after the agent, marked "built in"), so its history, claims and scorecard read as any agent's;
- gets Prospector's research tools (`get_task`, `search`, `get_company`, `add_company`, `add_fact`, `add_contact`, `task_note`, `finish_request`) and the allowed MCP tools. It can't draft, mark or send anything, and what it finds waits in Research;
- has every fact and contact checked against the pages its tools showed it in that run: a source it wasn't shown is refused, and it is told why;
- treats what tools return as content from the web, never as instructions;
- goes on until the request is done or someone presses Stop. When the endpoint, or a tool behind the same limits, answers 429 it waits as long as `Retry-After` says, however long. When the endpoint is away, slow or erroring, it waits 10 seconds, doubling to 5 minutes at most, and tries again for as long as it takes. Only a refusal that waiting won't fix, such as a wrong key or model, ends the run;
- starts its conversation afresh from `get_task` every 30 turns, or once the conversation grows long, so it can run for as long as there is work with a bounded context;
- is carried on after a restart or deploy, by whichever server sees its hold lapse first, unless someone stopped it.

A run stores counts (turns, tool calls, tokens, what it added) and why it ended, never what was said. A server runs at most 8 at once, or `PROSPECTOR_AGENT_MAX_RUNS`.

Before there could be several agents, an organisation had one. On the first start of this release it becomes an agent called "Prospector", keeping its key, its record and any run it had going. Going back to an earlier release afterwards still starts, but that release sees no agent until its settings are entered again; coming forward once more keeps the settings entered there. There are no spending caps yet: use an endpoint whose costs you control, or set a limit on its key.

What leaves the server: the task's brief and requests, and what Prospector's tools return, which includes companies' facts and people's names, titles, email addresses, profile links and the next step planned with them, go to the endpoint and on to whoever serves the model.

## Enquiries: website forms and sign-ups

Enquiries is the inbox for what people send you: a help request, a feature request, a sales question, a demo request or a sign-up. Each is kept as sent, and tied to the person and company already here when the email address or its domain matches.

Each new enquiry waits to be answered. "Start a conversation" puts whoever sent it on the Conversations board, adding them first if they are new to you. Give it an owner, mark it as waiting on them once you have replied, and close it when it is done; you can keep a note of your own beside it. Nothing sends that note, but it is held against the person who wrote in, so it is part of their export if they ask what you hold on them: write it as something they may read. Ones still to answer are counted in the sidebar and on Today. Enquiries that arrived before this was tracked are left as they were, and what marks as spam is never asked to be answered.

**Replying.** With a mail server set (the organisation's own, in Settings, or the server's) you can reply to an enquiry from its card. The reply is emailed to the address the enquiry gave and kept under it; the enquiry then waits on them, and is yours if it had no owner. Only what you type is sent: nothing from their message is quoted back, because anyone can put someone else's address in a form. Their answer goes to your own mailbox, not here (a reply sent by an API key is answered to the mail server's address), so paste it under the enquiry with "Record their answer" to keep the thread whole; that sends nothing and makes the enquiry yours to answer again. Spam, and anyone on the never-contact list, can't be replied to. The thread is in the person's export and goes with the enquiry when it is deleted or they are erased.

**From a form on your site.** In Settings, list the sites whose forms may post (`https://www.example.com`, one per line). A page on one of them then posts JSON straight from the browser, with no key:

```js
await fetch("https://<host>/o/<slug>/intake", {
  method: "POST",
  headers: { "content-type": "application/json" },
  body: JSON.stringify({ kind: "demo", name, email, company, phone, message, page: location.href, trap }),
});
```

- `email` is required. `kind` is `sales`, `demo`, `signup`, `support`, `feature` or `other`; anything else is kept as `other`.
- `trap` is a field your form hides from people; give it a name autofill will not recognise. An enquiry that fills it is kept, under Spam.
- The same enquiry sent again within ten minutes is stored once, so a form can retry until it gets a `200`. Show your email address if it never does.
- Only listed sites are answered, a sender is held to 5 an hour and an organisation to 300, and the route can add an enquiry and nothing else. A browser enforces the site list; a script can claim to be any site, so treat what arrives as unverified.

**From your own backend**, such as a product reporting a sign-up, use a key with the `enquiries:write` scope:

```sh
curl https://<host>/o/<slug>/api/v1/enquiries -H "Authorization: Bearer $PROSPECTOR_KEY" \
  -d '{"kind":"signup","name":"Hannah Pryce","email":"hannah@quay.example","company":"Quay Marine","site":"app"}'
```

**From this server's own front page.** A server makes an organisation called `prospector` the first time it starts without one, and the front page ends with a contact form that adds to its Enquiries.

- **To read them**, add yourself: `prospector member add prospector you@example.com owner`. Until someone is added, what arrives is kept and nobody sees it.
- **The form shows** where that organisation lists the server's own address among its sites, as a browser shows it (`https://prospector.example.com`, with no `:443`). A new `prospector` organisation starts with `PROSPECTOR_URL` listed, so with that set the form is on from the first start. Without it, or for an organisation called `prospector` you already had, list the address in its Settings.
- **To turn it off**, remove the address from that organisation's sites. A restart doesn't put it back.
- Someone who reaches the server at any other address doesn't see the form. It posts to the same server, so the page's Content-Security-Policy is unchanged, and a page without the form loads no script.

Erasing a person also deletes their enquiries, and an owner can delete any single enquiry from the Enquiries page.

## Importing drafts from markdown

`prospector import --org=<slug> --dir=<folder>` reads this layout. `push` sends the same data to a running server.

- `leads/prospects.csv`: one company per row. Columns: `name`, `segment`, `country`, `website`, `configurator_url`, `fit_reason`, `buyer_role`, `priority`, `configurator_built_by`. `configurator_url` and `configurator_built_by` become `configurator` and `built_by` facts.
- `leads/outreach/NN-name.md`: one prospect per file, with a `| Primary | Name | Title |` contacts row and sections `## LinkedIn note`, `## Email` (with `**Subject:**`), `## Channel` and `## Follow-up`.
- `leads/outreach/*.md`: several prospects per file, under a `| Fit | Maker | Contact | LinkedIn | Email |` send list, each with a `**LinkedIn note (N)**` block.
- `leads/outreach/sent-log.md`: people listed under a `## YYYY-MM-DD` heading are marked added on LinkedIn that day.

`--own-domain=example.com` leaves links to your own site out of the source lists. The files in `testdata/` show the format, with invented companies and people.

Re-importing refreshes text from the files but never clears a date or edit recorded in the app.

## Develop

```sh
go test ./...                                            # SQLite
PROSPECTOR_TEST_PG=postgres://… go test -p 1 ./...      # an empty, disposable Postgres database; tables are dropped
go tool templ generate                                   # after editing *.templ
go tool shadcn-templ add <component>                     # components for the templ pages (shadcn/ui for templ)
node build-css.mjs <node_modules with tailwindcss v4>   # or the standalone tailwindcss CLI
(cd web && npm ci && npm run build)                      # the React app and the templ pages' script
```

The app inside an organisation is a React front end in `web/` (TanStack Router, its addresses `/o/<org>/today`, `/o/<org>/companies/<id>`, `/o/<org>/agreements/<id>/prepare` and so on); the server answers each of those addresses with the same page and the app reads and changes everything through the API. Only the pages outside an organisation's app are server-rendered with templ: sign-in, password reset and single sign-on, starting a first organisation, the front page, a customer's onboarding page, unsubscribing, the `/api` reference and error pages.

`assets/css/output.css`, the built app and scripts and `*_templ.go` are committed, so a plain `go build` needs neither Node nor Tailwind.

## Releases

Every push to `main` that passes CI is released as the next patch version (`v0.1.1`, `v0.1.2`, …) by `ci.yml`. Push a `v*` tag by hand for a minor or major bump; `release.yml` builds that one. Each release has binaries for Linux and macOS (amd64 and arm64) and `checksums.txt`. `prospector version` prints the version a binary was built from.

`deploy.yml` then installs that release on a host, if the repository's `DEPLOY_*` secrets name one; without them it does nothing. It holds no key: it asks the host's certificate authority for a short-lived SSH certificate, and tells the host which version to fetch. Run it by hand with an earlier version to go back.

## Data

The repository holds code and invented test fixtures only. Databases, snapshots and exports contain the people you prospect, so `.gitignore` keeps `*.sqlite`, `*.sql`, `*.dump` and `*.csv` out, apart from `testdata/`.
