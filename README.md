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

Companies found by agents, imports and people wait here to be qualified or rejected. Every fact keeps the link it came from. "Pick" moves a suggested person into outreach, with a note from your template.

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
| `TZ` | The time zone "today" is counted in |
| `PROSPECTOR_ORG` | Default organisation for CLI commands (`--org=` overrides it) |
| `PROSPECTOR_KEY` | API key for `push` and `pull` |

**Before exposing it beyond your machine:**
- Put it behind HTTPS. Copy to clipboard needs a secure page, and the session cookie is marked Secure on HTTPS. Set `PROSPECTOR_URL` to the `https://` address so the cookie is always marked Secure; without it, that depends on the proxy sending `X-Forwarded-Proto: https`.
- Pages carry a Content-Security-Policy that allows only what the app itself serves. The typefaces are bundled, so a visitor's browser contacts no other site.
- A request's body has five minutes to arrive and its answer two minutes to be read, so a stalled client doesn't hold a connection open. An agent's MCP stream, once its key is accepted, stays open.
- Repeated wrong passwords for one email are slowed, up to a minute between tries. A browser that has signed in to that account before is counted separately, so someone guessing elsewhere can't hold its owner up.
- An owner adds and removes members in Settings. Removing someone ends their sign-in access to that organisation; their account, and other organisations, are untouched. API keys they created keep working until revoked on the Agents page.
- Someone added from Settings gets a first password that the person adding them sees; they are made to choose their own the first time they sign in.
- Members change their own password in Settings, which signs out their other sessions. `prospector user password` signs them out everywhere.
- Every page needs a sign-in except the sign-in form and the front page at `/`, which describes Prospector to a signed-out visitor and shows nothing from your data. Every browser POST must be JSON, which blocks cross-site form posts, and a request that a browser marks as coming from another site is refused, sign-in and sign-out included.
- The one exception is the enquiry route, `/o/<slug>/intake`, which exists to be posted to from your own sites. It answers only the sites an owner lists in Settings, reads no session, and can add an enquiry and nothing else. With no sites listed it refuses everything.

There's a Dockerfile for container hosting.

## Agents: MCP and REST

Each organisation has an MCP endpoint at `/o/<slug>/mcp` (streamable HTTP) and a JSON API at `/o/<slug>/api/v1/…`. Create keys on the Agents & API page or with `prospector key add <org> <name> <scope>…`.

| Scope | Allows |
|---|---|
| `read` | `search`, `get_company`, `list_due` |
| `research:write` | `add_company`, `add_fact`, `add_contact`; these wait in Research for a person |
| `drafts:write` | `draft_message`, shown as written by the agent |
| `import` | `push` and `pull`: a full sync, for a person's own files rather than an agent |
| `outcomes:write` | `mark`: record an outcome (sent, accepted, called, replied, declined, hold), shown as the agent's |
| `enquiries:write` | `POST /api/v1/enquiries`: record an enquiry or sign-up from your own product or site backend |

- **What no key can do:** send anything, change settings, or see another organisation. A key with `outcomes:write` can record what happened (sent, accepted, replied, declined…); each mark shows as the agent's, with its note, on Today and in the company's history. `import` is the one scope that skips review: it puts people from your own files straight into outreach and marks those in `sent-log.md` as added.
- **Writes are checked:** every fact and contact an agent adds needs a source URL. The never-contact list applies to agents, imports and edits alike: an import that would bring in an address on it is refused whole, though people already stored stay as they are. Someone already stored who falls under the list drops out of the outreach queues on Today and the agents' due list until the entry is removed, and Settings names them; a person who has already replied stays in Conversations. An entry is a domain, an email address or a LinkedIn profile link, matched whole: a company name or a loose word matches nobody.
- **Sessions:** an MCP session works only with the key that opened it.

```sh
claude mcp add --transport http prospector https://<host>/o/<slug>/mcp --header "Authorization: Bearer $PROSPECTOR_KEY"
```

## Enquiries: website forms and sign-ups

Enquiries is the inbox for what people send you: a help request, a feature request, a sales question, a demo request or a sign-up. Each is kept as sent, and tied to the person and company already here when the email address or its domain matches.

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

Erasing a person also deletes their enquiries, and an owner can delete any single enquiry from the Enquiries page.

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
