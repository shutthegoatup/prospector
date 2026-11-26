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
// DATABASE_URL picks the database: a postgres:// URL or a SQLite path
// (default prospector.sqlite in the current directory).
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

// version is set at release time by -ldflags "-X main.version=…".
var version = "dev"

func defaultDSN() string {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return dsn
	}
	return "prospector.sqlite"
}

func arg(args []string, i int, fallback string) string {
	if len(args) > i {
		return args[i]
	}
	return fallback
}

// flags takes --name=value options out of args.
func flags(args []string) (map[string]string, []string) {
	opts := map[string]string{"org": os.Getenv("PROSPECTOR_ORG"), "dir": "."}
	var rest []string
	for _, a := range args {
		if name, value, ok := strings.Cut(strings.TrimPrefix(a, "--"), "="); ok && strings.HasPrefix(a, "--") {
			opts[name] = value
		} else {
			rest = append(rest, a)
		}
	}
	return opts, rest
}

func remote(method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	if key := os.Getenv("PROSPECTOR_KEY"); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(res.Body)
		res.Body.Close()
		said := strings.TrimSpace(string(msg))
		var p problem
		if json.Unmarshal(msg, &p) == nil && p.Detail != "" {
			said = p.Detail
		}
		return nil, fmt.Errorf("%s: %s %s", url, res.Status, said)
	}
	return res, nil
}

func readPassword() string {
	fmt.Fprint(os.Stderr, "password: ")
	pass, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimRight(pass, "\r\n")
}

// redact hides a database URL's password before it is printed.
func redact(dsn string) string {
	if u, err := url.Parse(dsn); err == nil && u.User != nil {
		if _, has := u.User.Password(); has {
			u.User = url.UserPassword(u.User.Username(), "xxxxx")
			return u.String()
		}
	}
	return dsn
}

func run(args []string) error {
	opts, args := flags(args)
	cmd := arg(args, 0, "help")
	rest := args[min(1, len(args)):]

	switch cmd {
	case "version":
		fmt.Println(version)
		return nil
	case "help", "-h", "--help":
		fmt.Println("usage: prospector serve | user | org | member | key | sso | secret-key | import | export | restore | import-legacy | mark | set | list | push | pull | version\nSee the README for details.")
		return nil
	case "push":
		if len(rest) < 1 {
			return fmt.Errorf("usage: prospector push https://host/o/<org> [--dir=.] [--own-domain=…]")
		}
		p, err := ReadRepo(opts["dir"], opts["own-domain"])
		if err != nil {
			return err
		}
		b, _ := json.Marshal(p)
		res, err := remote("POST", strings.TrimRight(rest[0], "/")+"/api/v1/import", bytes.NewReader(b))
		if err != nil {
			return err
		}
		defer res.Body.Close()
		_, err = io.Copy(os.Stdout, res.Body)
		return err
	case "pull":
		if len(rest) < 2 {
			return fmt.Errorf("usage: prospector pull https://host/o/<org> <file>")
		}
		res, err := remote("GET", strings.TrimRight(rest[0], "/")+"/api/v1/snapshot", nil)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		text, err := io.ReadAll(res.Body)
		if err != nil {
			return err
		}
		return os.WriteFile(rest[1], text, 0o600)
	}

	if cmd == "secret-key" {
		// Printed as an entry of PROSPECTOR_SECRET_KEYS, version and all.
		version := arg(rest, 0, "1")
		if v, err := strconv.Atoi(version); err != nil || v < 0 || strconv.Itoa(v) != version {
			return fmt.Errorf("usage: prospector secret-key [version], a whole number one more than the key it replaces")
		}
		fmt.Println(version + ":" + newSecretKey())
		return nil
	}
	dsn := defaultDSN()
	s, err := Open(dsn)
	if err != nil {
		return fmt.Errorf("open %s: %w", redact(dsn), err)
	}
	switch cmd {
	case "serve":
		addr := "127.0.0.1:8766"
		if port := os.Getenv("PORT"); port != "" {
			addr = ":" + port
		}
		base, err := publicURL(os.Getenv("PROSPECTOR_URL"))
		if err != nil {
			return err
		}
		social, err := socialFromEnv(os.Getenv)
		if err != nil {
			return err
		}
		via, err := mailFromEnv(os.Getenv)
		if err != nil {
			return err
		}
		// Only the server uses the keys, so a mistyped one doesn't stop the other
		// commands: `sso off` and `user password` are how a locked-out host is put right.
		if s.keys, err = parseKeys(os.Getenv("PROSPECTOR_SECRET_KEYS")); err != nil {
			return err
		}
		s.resealAtStart()
		// Not a reason to stay down: the page is served without its form meanwhile.
		if err := s.EnsureFrontOrg(base); err != nil {
			fmt.Fprintf(os.Stderr, "the front page has no contact form: %v\n", err)
		} else if why := s.frontFormOff(base); why != "" {
			fmt.Fprintf(os.Stderr, "the front page has no contact form: %s\n", why)
		}
		if os.Getenv("PROSPECTOR_FRONT_ORG") != "" {
			fmt.Fprintf(os.Stderr, "PROSPECTOR_FRONT_ORG is no longer read: the front page's form sends to the %q organisation\n", frontOrgSlug)
		}
		return serve(&App{s: s, base: base, frontOrg: frontOrgSlug, social: social, mail: via}, arg(rest, 0, addr))
	case "sso":
		return ssoCommand(s, rest)
	case "agent":
		return agentCommand(s, rest)
	case "mail":
		if arg(rest, 0, "") != "test" || len(rest) < 2 {
			return fmt.Errorf("usage: prospector mail test <address>")
		}
		via, err := mailFromEnv(os.Getenv)
		if err != nil {
			return err
		}
		if via == nil {
			return fmt.Errorf("set SMTP_ADDR and MAIL_FROM first")
		}
		err = smtpSend(context.Background(), *via, Message{To: rest[1], Subject: "Prospector can send mail",
			Body: "This is the test from `prospector mail test`.\n\nIt was sent through " + via.Host + " as " + via.From + ".\n"})
		if err != nil {
			return err
		}
		fmt.Println("sent to", rest[1], "through", via.Host)
		return nil
	case "user":
		switch arg(rest, 0, "") {
		case "add":
			if len(rest) < 3 {
				return fmt.Errorf("usage: prospector user add <email> <name>")
			}
			u, err := s.CreateUser(rest[1], strings.Join(rest[2:], " "), readPassword())
			if err != nil {
				return err
			}
			fmt.Println("added", u.Email)
			return nil
		case "password":
			if len(rest) < 2 {
				return fmt.Errorf("usage: prospector user password <email>")
			}
			return s.SetPassword(rest[1], readPassword())
		}
		return fmt.Errorf("usage: prospector user add|password …")
	case "org":
		if arg(rest, 0, "") != "add" || len(rest) < 3 {
			return fmt.Errorf("usage: prospector org add <slug> <name> [--owner=<email>]")
		}
		name := strings.Join(rest[2:], " ")
		if opts["owner"] == "" {
			o, err := s.CreateOrg(rest[1], name)
			if err != nil {
				return err
			}
			fmt.Println("added /o/" + o.Slug)
			fmt.Fprintf(os.Stderr, "nobody can open it yet: prospector member add %s <email> owner\n", o.Slug)
			return nil
		}
		u, err := s.UserByEmail(opts["owner"])
		if err != nil {
			return fmt.Errorf("no user %q", opts["owner"])
		}
		o, err := s.CreateOrgWithOwner(rest[1], name, u)
		if err != nil {
			return err
		}
		fmt.Printf("added /o/%s, owned by %s\n", o.Slug, u.Email)
		return nil
	case "member":
		if arg(rest, 0, "") != "add" || len(rest) < 3 {
			return fmt.Errorf("usage: prospector member add <org> <email> [owner|member]")
		}
		o, err := s.OrgBySlug(rest[1])
		if err != nil {
			return fmt.Errorf("no organisation %q", rest[1])
		}
		u, err := s.UserByEmail(rest[2])
		if err != nil {
			return fmt.Errorf("no user %q", rest[2])
		}
		return s.AddMember(o.ID, u.ID, arg(rest, 3, "owner"), u.Name)
	case "key":
		if arg(rest, 0, "") != "add" || len(rest) < 4 {
			return fmt.Errorf("usage: prospector key add <org> <name> <scope>…")
		}
		// So a key made here is stored the way the server stores one.
		if s.keys, err = parseKeys(os.Getenv("PROSPECTOR_SECRET_KEYS")); err != nil {
			return err
		}
		o, err := s.OrgBySlug(rest[1])
		if err != nil {
			return fmt.Errorf("no organisation %q", rest[1])
		}
		secret, _, err := s.CreateKey(o.ID, rest[2], rest[3:], "cli")
		if err != nil {
			return err
		}
		fmt.Println(secret)
		return nil
	}

	if opts["org"] == "" {
		return fmt.Errorf("%s needs an organisation: --org=<slug> or PROSPECTOR_ORG", cmd)
	}
	org, err := s.OrgBySlug(opts["org"])
	if err != nil {
		return fmt.Errorf("no organisation %q: create it with `prospector org add %s <name>`", opts["org"], opts["org"])
	}
	switch cmd {
	case "import":
		p, err := ReadRepo(opts["dir"], opts["own-domain"])
		if err != nil {
			return err
		}
		total, err := s.Import(org.ID, p)
		if err != nil {
			return err
		}
		fmt.Printf("%d contacts in %s\n", total, org.Slug)
	case "export":
		if len(rest) < 1 {
			return fmt.Errorf("usage: prospector export <file>")
		}
		text, err := s.SnapshotText(org.ID)
		if err != nil {
			return err
		}
		if err := os.WriteFile(rest[0], []byte(text), 0o600); err != nil {
			return err
		}
		fmt.Println("wrote", rest[0])
	case "restore":
		if len(rest) < 1 {
			return fmt.Errorf("usage: prospector restore <file>")
		}
		text, err := os.ReadFile(rest[0])
		if err != nil {
			return err
		}
		n, err := s.Restore(org.ID, string(text))
		if err != nil {
			return err
		}
		fmt.Printf("restored %d contacts into %s from %s\n", n, org.Slug, rest[0])
	case "import-legacy":
		if len(rest) < 1 {
			return fmt.Errorf("usage: prospector import-legacy <file.sqlite>")
		}
		n, err := s.ImportLegacy(org.ID, rest[0])
		if err != nil {
			return err
		}
		fmt.Printf("copied %d contacts from %s into %s\n", n, rest[0], org.Slug)
	case "export-org":
		if len(rest) < 1 {
			return fmt.Errorf("usage: prospector export-org <file>")
		}
		out, err := s.ExportOrg(org.ID, "cli")
		if err != nil {
			return err
		}
		text, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(rest[0], text, 0o600); err != nil {
			return err
		}
		fmt.Println("wrote", rest[0])
	case "export-person":
		if len(rest) < 2 {
			return fmt.Errorf("usage: prospector export-person <name> <file>")
		}
		c, err := s.FindOne(org.ID, rest[0])
		if err != nil {
			return err
		}
		if !strings.EqualFold(strings.TrimSpace(rest[0]), displayName(*c)) {
			return fmt.Errorf("export-person needs the full name, to be sure it's them: %q", displayName(*c))
		}
		out, err := s.ExportPerson(org.ID, c.ID, "cli")
		if err != nil {
			return err
		}
		text, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(rest[1], text, 0o600); err != nil {
			return err
		}
		fmt.Println("wrote", rest[1])
	case "erase":
		if len(rest) < 1 {
			return fmt.Errorf("usage: prospector erase <name>")
		}
		c, err := s.FindOne(org.ID, rest[0])
		if err != nil {
			return err
		}
		if !strings.EqualFold(strings.TrimSpace(rest[0]), displayName(*c)) {
			return fmt.Errorf("erase needs the full name, to be sure it's them: %q", displayName(*c))
		}
		if err := s.ErasePerson(org.ID, c.ID, "cli"); err != nil {
			return err
		}
		fmt.Printf("erased %s @ %s; take them out of your lead files too\n", deref(c.Person), deref(c.Maker))
	case "mark":
		if len(rest) < 2 {
			return fmt.Errorf("usage: prospector mark <name> <step> [date]")
		}
		c, err := s.FindOne(org.ID, rest[0])
		if err != nil {
			return err
		}
		date := arg(rest, 2, today())
		if err := s.Mark(org.ID, c.ID, rest[1], date, "cli"); err != nil {
			return err
		}
		fmt.Printf("%s @ %s: %s = %s\n", deref(c.First), deref(c.Maker), Marks[rest[1]], date)
	case "set":
		if len(rest) < 3 {
			return fmt.Errorf("usage: prospector set <name> <field> <value>")
		}
		c, err := s.FindOne(org.ID, rest[0])
		if err != nil {
			return err
		}
		if err := s.Update(org.ID, c.ID, rest[1], rest[2], "cli"); err != nil {
			return err
		}
		fmt.Printf("%s @ %s: %s = %s\n", deref(c.First), deref(c.Maker), rest[1], rest[2])
	case "list":
		rows, err := s.Due(org.ID, arg(rest, 0, "linkedin"), time.Now(), org.Settings)
		if err != nil {
			return err
		}
		for _, r := range rows {
			maker := []rune(deref(r.Maker))
			fmt.Printf("%-6s %-28s %-12s %-10s %s\n", deref(r.Fit), string(maker[:min(28, len(maker))]),
				deref(r.First), deref(r.LinkedinAdded), deref(r.Email))
		}
	default:
		return fmt.Errorf("unknown command %q (try prospector help)", cmd)
	}
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "prospector:", err)
		os.Exit(1)
	}
}
