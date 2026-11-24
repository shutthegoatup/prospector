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

### Conversations: replies to deals

Replies move across a board: replied, meeting, proposal, won, lost. Each needs a next step with a date. Ones without, or due today, come back on Today.

![Conversations: a board from replied to won](docs/images/conversations.png)

### Agents and API

Each organisation has an MCP endpoint and a REST API, with scoped keys. Agents can search, add companies, facts and suggested contacts, and draft messages. They can't send anything or change settings.

![Agents & API: the MCP endpoint, REST calls and the tools each key gets](docs/images/agents.png)

### Also

- **Companies:** search, plus each company's people, facts and full history: who did what, people and agents alike.
- **Erase a person:** an owner can erase someone from their company's page (or `prospector erase <name>`) when they ask to be forgotten. Their details, drafts, notes and facts are deleted, the history keeps only that something happened and when, and an import, a restore or an agent can't add them again. A row that names several people goes whole. Company facts and other people's notes that mention them, your own lead files and database backups aren't touched: take them out there too.
- **Add person:** paste a LinkedIn profile link and the name fills in. A bookmarklet opens the same form pre-filled from the profile you're viewing.
- **Settings, per organisation:** waits between steps, connection-note template, call-script pitch, never-contact list and team. Each member sets their own caller name and signature.
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
| `PROSPECTOR_GOOGLE_CLIENT_ID`, `PROSPECTOR_GOOGLE_CLIENT_SECRET` | Offer "Continue with Google" on the sign-in page. The same pair exists for `MICROSOFT` and `LINKEDIN`. Each needs `PROSPECTOR_URL` (see [Signing in](#signing-in)) |
| `TZ` | The time zone "today" is counted in |
| `PROSPECTOR_ORG` | Default organisation for CLI commands (`--org=` overrides it) |
| `PROSPECTOR_KEY` | API key for `push` and `pull` |

**Before exposing it beyond your machine:**
- Put it behind HTTPS. Copy to clipboard needs a secure page, and the session cookie is marked Secure on HTTPS. Set `PROSPECTOR_URL` to the `https://` address so the cookie is always marked Secure; without it, that depends on the proxy sending `X-Forwarded-Proto: https`.
- Pages carry a Content-Security-Policy that allows only what the app itself serves. The typefaces are bundled, so a visitor's browser contacts no other site.
- A request's body has five minutes to arrive and its answer two minutes to be read, so a stalled client doesn't hold a connection open. An agent's MCP stream, once its key is accepted, stays open.
- Repeated wrong passwords for one email are slowed, up to a minute between tries. A browser that has signed in to that account before is counted separately, so someone guessing elsewhere can't hold its owner up.
- An owner adds and removes members in Settings. Removing someone ends their sign-in access to that organisation; their account, and other organisations, are untouched. API keys they created keep working until revoked on the Agents page. If the organisation lets people join through its single sign-on, someone removed here joins again while its provider still lets them in: remove them there too.
- Someone added from Settings gets a first password that the person adding them sees; they are made to choose their own the first time they sign in.
- Members change their own password in Settings, which signs out their other sessions. `prospector user password` signs them out everywhere.
- Every page needs a sign-in except the sign-in pages and the front page at `/`, which describes Prospector to a signed-out visitor and shows nothing from your data. The single sign-on page does name the organisations that have verified an email domain to anyone who types an address at it. Every browser POST must be JSON, which blocks cross-site form posts, and a request that a browser marks as coming from another site is refused, sign-in and sign-out included.

There's a Dockerfile for container hosting.

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
2. **Enter the issuer, client ID and client secret.** The issuer is read before anything is saved. The secret is kept in the database and never shown again; it has to be typed again when the issuer or client ID changes.
3. Optionally **require it**: members at your domains can no longer open the organisation on a password. Owners still can, so a broken provider can be put right, and so can members at other domains, whom your provider couldn't sign in. API keys are unaffected. If no owner can get in, `prospector sso off <org>` stops requiring it.
4. Optionally **let people join**: anyone at your domains becomes a member, never an owner, the first time they sign in. Without this, the provider signs in only people an owner has already added.

People start from "Use your organisation's single sign-on" on the sign-in page and give their work address.

- A session that began at an organisation's provider opens that organisation only, since the provider vouches for nothing else. Someone already signed in who goes through it keeps their session.
- Going through the provider counts for a day, then it is asked again. Someone the provider has turned away is out within that day; to end it sooner, remove them from the team.
- With Google as the provider, the account must also be in a Workspace for that domain (`hd`). An issuer that signs in every tenant under one registration, such as Microsoft's `/common`, is refused: use your own tenant's.
- An issuer must be `https` at a public address; one inside the server's network is never contacted.
- Someone invited with a first password who arrives through a provider instead isn't asked to change it: that password stops working.
- Each change to the connection, and each person who joins through it, is recorded in the organisation's events with who and when. SAML and SCIM aren't supported.

## Agents: MCP and REST

Each organisation has an MCP endpoint at `/o/<slug>/mcp` (streamable HTTP) and a JSON API at `/o/<slug>/api/v1/…`. Create keys on the Agents & API page or with `prospector key add <org> <name> <scope>…`.

| Scope | Allows |
|---|---|
| `read` | `search`, `get_company`, `list_due` |
| `research:write` | `add_company`, `add_fact`, `add_contact`; these wait in Research for a person |
| `drafts:write` | `draft_message`, shown as written by the agent |
| `import` | `push` and `pull`: a full sync, for a person's own files rather than an agent |
| `outcomes:write` | `mark`: record an outcome (sent, accepted, called, replied, declined, hold), shown as the agent's |

- **What no key can do:** send anything, change settings, or see another organisation. A key with `outcomes:write` can record what happened (sent, accepted, replied, declined…); each mark shows as the agent's, with its note, on Today and in the company's history. `import` is the one scope that skips review: it puts people from your own files straight into outreach and marks those in `sent-log.md` as added.
- **Writes are checked:** every fact and contact an agent adds needs a source URL. The never-contact list applies to agents, imports and edits alike: an import that would bring in an address on it is refused whole, though people already stored stay as they are. Someone already stored who falls under the list drops out of the outreach queues on Today and the agents' due list until the entry is removed, and Settings names them; a person who has already replied stays in Conversations. An entry is a domain, an email address or a LinkedIn profile link, matched whole: a company name or a loose word matches nobody.
- **Doing it twice is safe:** an agent that runs again, or a second agent on the same company, adds nothing that is already there, and the reply says `existed`. A company matches on its name, or on its domain when one company alone has it; a fact on its field, wording and source page; a person on their profile, address or name at that company. The same fact worded differently is still a second fact, and new details for a person already there are not merged in. An agent's draft is refused once a person has edited that text or it has gone out.
- **Sessions:** an MCP session works only with the key that opened it.

```sh
claude mcp add --transport http prospector https://<host>/o/<slug>/mcp --header "Authorization: Bearer $PROSPECTOR_KEY"
```

## Importing drafts from markdown

`prospector import --org=<slug> --dir=<folder>` reads this layout. `push` sends the same data to a running server.

- `leads/prospects.csv`: one company per row. Columns: `name`, `segment`, `country`, `website`, `configurator_url`, `fit_reason`, `buyer_role`, `priority`, `configurator_built_by`.
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
go tool shadcn-templ add <component>                     # UI components (shadcn/ui for templ)
node build-css.mjs <node_modules with tailwindcss v4>   # or the standalone tailwindcss CLI
```

`assets/css/output.css`, the script bundle and `*_templ.go` are committed, so a plain `go build` needs neither Node nor Tailwind.

## Releases

Every push to `main` that passes CI is released as the next patch version (`v0.1.1`, `v0.1.2`, …) by `ci.yml`. Push a `v*` tag by hand for a minor or major bump; `release.yml` builds that one. Each release has binaries for Linux and macOS (amd64 and arm64) and `checksums.txt`. `prospector version` prints the version a binary was built from.

`deploy.yml` then installs that release on a host, if the repository's `DEPLOY_*` secrets name one; without them it does nothing. It holds no key: it asks the host's certificate authority for a short-lived SSH certificate, and tells the host which version to fetch. Run it by hand with an earlier version to go back.

## Data

The repository holds code and invented test fixtures only. Databases, snapshots and exports contain the people you prospect, so `.gitignore` keeps `*.sqlite`, `*.sql`, `*.dump` and `*.csv` out, apart from `testdata/`.
