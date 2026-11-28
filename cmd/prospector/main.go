// Command prospector is a self-hosted, account-based prospecting workbench: the web
// app, its REST API and MCP server for agents, and this CLI.
//
//	prospector serve [addr]                  the app (127.0.0.1:8766, or :$PORT when set)
//	prospector user add <email> <name>       password from stdin
//	prospector user password <email>         password from stdin
//	prospector org add <slug> <name> [--owner=<email>]
//	prospector member add <org> <email> [owner|member]
//	prospector key add <org> <name> <scope>…   prints the key once
//	prospector mail test <address>           send a test through SMTP_ADDR
//	prospector sso off <org>                 stop requiring single sign-on, when its provider has broken
//	prospector agent set <org> <name> <endpoint> <model> [<tools-url> [<tool>…]]   key from $PROSPECTOR_AGENT_API_KEY, kind from $PROSPECTOR_AGENT_KIND, SearXNG from $PROSPECTOR_AGENT_SEARCH_URL, $PROSPECTOR_AGENT_THINKING=off|on
//	prospector agent show <org>              each agent's endpoint, model and tools, never its key
//	prospector secret-key [version]          print a new entry for PROSPECTOR_SECRET_KEYS
//	prospector version
//
// Per organisation, chosen with --org=<slug> or $PROSPECTOR_ORG:
//
//	prospector import [--dir=.] [--own-domain=example.com]   load markdown drafts (see README)
//	prospector export <file>                 write the contacts snapshot
//	prospector restore <file>                replace the organisation's contacts with a snapshot
//	prospector import-legacy <file.sqlite>   the same, from a legacy tracker database (read-only)
//	prospector export-org <file>             write everything the organisation holds, without secrets
//	prospector export-person <name> <file>   write everything held on one person, on their request
//	prospector erase <name>                  delete a person for good, on their request
//	prospector mark <name> <step> [date]
//	prospector set <name> <field> <value>
//	prospector list [step]
//
// Against a running server, with an API key holding the import scope in $PROSPECTOR_KEY:
//
//	prospector push https://host/o/<org> [--dir=.] [--own-domain=…]
//	prospector pull https://host/o/<org> <file>
//
// SMTP_ADDR, SMTP_USERNAME, SMTP_PASSWORD and MAIL_FROM name the mail server that
// password resets and invitations leave through; without them there are neither.
//
// DATABASE_URL is the Postgres database, as a postgres:// URL. There is no default.
package main

import "github.com/shutthegoatup/prospector/internal/app"

func main() { app.Main() }
