// Command prospector is a self-hosted, account-based prospecting workbench: the web
// app, its REST API and MCP server for agents, and this CLI.
//
//	prospector serve [addr]                  the app (127.0.0.1:8766, or :$PORT when set)
//	prospector user add <email> <name>       password from stdin
//	prospector user password <email>         password from stdin
//	prospector org add <slug> <name>
//	prospector member add <org> <email> [owner|member]
//	prospector key add <org> <name> <scope>…   prints the key once
//	prospector version
//
// Per organisation, chosen with --org=<slug> or $PROSPECTOR_ORG:
//
//	prospector import [--dir=.] [--own-domain=example.com]   load markdown drafts (see README)
//	prospector export <file>                 write the contacts snapshot
//	prospector restore <file>                replace the organisation's contacts with a snapshot
//	prospector import-legacy <file.sqlite>   the same, from a legacy tracker database (read-only)
//	prospector mark <name> <step> [date]
//	prospector set <name> <field> <value>
//	prospector list [step]
//
// Against a running server, with an API key holding the import scope in $PROSPECTOR_KEY:
//
//	prospector push https://host/o/<org> [--dir=.] [--own-domain=…]
//	prospector pull https://host/o/<org> <file>
//
// DATABASE_URL picks the database: a postgres:// URL or a SQLite path
// (default prospector.sqlite in the current directory).
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
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
		return nil, fmt.Errorf("%s: %s %s", url, res.Status, strings.TrimSpace(string(msg)))
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
		fmt.Println("usage: prospector serve | user | org | member | key | import | export | restore | import-legacy | mark | set | list | push | pull | version\nSee the README for details.")
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
		return serve(&App{s: s}, arg(rest, 0, addr))
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
			return fmt.Errorf("usage: prospector org add <slug> <name>")
		}
		o, err := s.CreateOrg(rest[1], strings.Join(rest[2:], " "))
		if err != nil {
			return err
		}
		fmt.Println("added /o/" + o.Slug)
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
