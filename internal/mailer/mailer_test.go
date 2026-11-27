package mailer

import (
	"bufio"
	"cmp"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"testing"

	gomail "github.com/wneessen/go-mail"
)

func TestMailFromEnv(t *testing.T) {
	t.Parallel()
	env := func(pairs ...string) func(string) string {
		return func(k string) string {
			for i := 0; i < len(pairs); i += 2 {
				if pairs[i] == k {
					return pairs[i+1]
				}
			}
			return ""
		}
	}
	if m, err := FromEnv(env()); m != nil || err != nil {
		t.Errorf("nothing set: %+v %v", m, err)
	}
	m, err := FromEnv(env("SMTP_ADDR", "smtp.example.com:587", "SMTP_USERNAME", "u", "SMTP_PASSWORD", "p", "MAIL_FROM", "Prospector <p@example.com>"))
	if err != nil || *m != (Server{"smtp.example.com", 587, "u", "p", "Prospector <p@example.com>"}) {
		t.Errorf("a full set: %+v %v", m, err)
	}
	for name, pairs := range map[string][]string{
		"an address without a sender": {"SMTP_ADDR", "smtp.example.com:587"},
		"a sender without an address": {"MAIL_FROM", "p@example.com"},
		"no port":                     {"SMTP_ADDR", "smtp.example.com", "MAIL_FROM", "p@example.com"},
		"a port mail doesn't use":     {"SMTP_ADDR", "db.example.com:5432", "MAIL_FROM", "p@example.com"},
	} {
		if _, err := FromEnv(env(pairs...)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// fakeSMTP speaks enough of the protocol to take one message at a time.
type fakeSMTP struct {
	addr string
	// tls, when set, is offered as STARTTLS.
	tls *tls.Config
	// rcptReply and dataReply, when set, answer the recipient and the end of the
	// message; hangUp, when set, drops the line after taking a message, with no goodbye.
	rcptReply, dataReply string
	hangUp               bool
	mu                   sync.Mutex
	from                 string
	to                   []string
	auth                 string
	data                 string
	verbs                []string
	// secured is whether the message arrived after STARTTLS.
	secured bool
}

func newFakeSMTP(t *testing.T, secure *tls.Config) *fakeSMTP {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	f := &fakeSMTP{addr: l.Addr().String(), tls: secure}
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	return f
}

func (f *fakeSMTP) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	say := func(s string) { conn.Write([]byte(s + "\r\n")) }
	secured := false
	say("220 fake ready")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		f.mu.Lock()
		f.verbs = append(f.verbs, verb)
		switch verb {
		case "EHLO":
			if f.tls != nil && !secured {
				say("250-fake\r\n250 STARTTLS")
			} else {
				say("250-fake\r\n250 AUTH PLAIN")
			}
		case "STARTTLS":
			say("220 go ahead")
			conn = tls.Server(conn, f.tls)
			r, secured = bufio.NewReader(conn), true
		case "AUTH":
			f.auth = line
			say("235 ok")
		case "MAIL":
			f.from = line
			say("250 ok")
		case "RCPT":
			f.to = append(f.to, line)
			say(cmp.Or(f.rcptReply, "250 ok"))
		case "DATA":
			say("354 go on")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil || l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			f.data, f.secured = b.String(), secured
			say(cmp.Or(f.dataReply, "250 queued"))
			if f.hangUp {
				f.mu.Unlock()
				return
			}
		case "NOOP", "RSET":
			say("250 ok")
		case "QUIT":
			say("221 bye")
			f.mu.Unlock()
			return
		default:
			say("502 what")
		}
		f.mu.Unlock()
	}
}

func TestSMTPSend(t *testing.T) {
	t.Parallel()
	f := newFakeSMTP(t, nil)
	host, port, _ := net.SplitHostPort(f.addr)
	n, _ := strconv.Atoi(port)
	via := Server{Host: host, Port: n, Username: "user", Password: "pass", From: "Prospector <site@prospector.example>"}
	err := Send(context.Background(), via, Message{To: "Grace Hopper <grace@example.com>",
		Subject: "Añadido\r\nBcc: mallory@evil.example", Body: "Choose your password:\nhttps://prospector.example/reset/abc=def\n"})
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.from != "MAIL FROM:<site@prospector.example>" || len(f.to) != 1 || f.to[0] != "RCPT TO:<grace@example.com>" || f.auth == "" {
		t.Errorf("envelope: %q %q, auth %q", f.from, f.to, f.auth)
	}
	head, body, _ := strings.Cut(f.data, "\r\n\r\n")
	// A charset's name, and an encoded word's, are the same whatever their case.
	folded := strings.ToLower(head)
	if strings.Contains(head, "\r\nBcc:") || !strings.Contains(folded, "subject: =?utf-8?q?") {
		t.Errorf("a line break in the subject made a header:\n%s", head)
	}
	for _, want := range []string{`From: "Prospector" <site@prospector.example>`, `To: "Grace Hopper" <grace@example.com>`, "Date: ", "Message-ID: <"} {
		if !strings.Contains(head, want) {
			t.Errorf("headers lack %q:\n%s", want, head)
		}
	}
	if !strings.Contains(folded, "charset=utf-8") {
		t.Errorf("no charset:\n%s", head)
	}
	if strings.Contains(folded, "user-agent:") || strings.Contains(folded, "x-mailer:") {
		t.Errorf("the message names the software that sent it:\n%s", head)
	}
	if !strings.Contains(body, "reset/abc=3Ddef") {
		t.Errorf("body: %q", body)
	}

	if err := Send(context.Background(), via, Message{To: "grace@example.com\r\nBcc: mallory@evil.example", Subject: "x"}); err == nil {
		t.Error("a recipient spanning lines was accepted")
	}
	// Anywhere but this machine, a server that offers no TLS is sent nothing.
	if err := (Server{Host: "db.internal.example", Port: 5432, From: "a@example.com"}).Check(); err == nil {
		t.Error("a port mail doesn't use was accepted for another machine")
	}
}

// A real provider is never on this machine, so these are the paths it takes.
func TestSMTPNeedsTLS(t *testing.T) {
	t.Parallel()
	from, to := &mail.Address{Address: "site@prospector.example"}, &mail.Address{Address: "grace@example.com"}
	via := Server{Host: "example.com", Port: 587, Username: "user", Password: "pass", From: from.Address}
	// Whatever host and port the client is told, it reaches the fake server.
	dial := func(f *fakeSMTP) gomail.DialContextFunc {
		return func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", f.addr)
		}
	}
	ctx := context.Background()
	said := func(f *fakeSMTP) string {
		f.mu.Lock()
		defer f.mu.Unlock()
		return strings.Join(f.verbs, " ")
	}

	// A server that offers no TLS is told nothing: no password, no message.
	plain := newFakeSMTP(t, nil)
	err := submit(ctx, dial(plain), via, from, to, Message{Subject: "x", Body: "y"}, &tls.Config{ServerName: via.Host}, true)
	if !errors.Is(err, ErrUnsent) {
		t.Fatalf("sending without TLS: %v", err)
	}
	if got := said(plain); strings.Contains(got, "AUTH") || strings.Contains(got, "MAIL") {
		t.Errorf("the server without TLS was sent: %s", got)
	}

	// httptest's certificate names example.com, and its client trusts it.
	web := httptest.NewTLSServer(nil)
	t.Cleanup(web.Close)
	roots := web.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	offer := &tls.Config{Certificates: web.TLS.Certificates}

	secure := newFakeSMTP(t, offer)
	if err := submit(ctx, dial(secure), via, from, to, Message{Subject: "x", Body: "y"}, &tls.Config{ServerName: via.Host, RootCAs: roots}, true); err != nil {
		t.Fatalf("sending over STARTTLS: %v", err)
	}
	secure.mu.Lock()
	if !secure.secured || secure.auth == "" || !strings.Contains(secure.data, "Subject: x") {
		t.Errorf("over STARTTLS: secured %v, auth %q, data %q", secure.secured, secure.auth, secure.data)
	}
	secure.mu.Unlock()

	// A certificate nobody vouches for, or for another name, ends it before the password.
	for name, trust := range map[string]*tls.Config{
		"an untrusted certificate": {ServerName: via.Host},
		"another server's name":    {ServerName: "smtp.other.example", RootCAs: roots},
	} {
		f := newFakeSMTP(t, offer)
		if err := submit(ctx, dial(f), via, from, to, Message{Subject: "x", Body: "y"}, trust, true); err == nil {
			t.Errorf("%s was accepted", name)
		}
		if got := said(f); strings.Contains(got, "AUTH") || strings.Contains(got, "MAIL") {
			t.Errorf("%s: the server was sent: %s", name, got)
		}
	}

	// A relay on this machine usually offers TLS under a certificate of its own making.
	local, relay := via, newFakeSMTP(t, offer)
	local.Username = ""
	if err := submit(ctx, dial(relay), local, from, to, Message{Subject: "x", Body: "y"}, &tls.Config{ServerName: via.Host}, false); err != nil {
		t.Fatalf("a local relay with a self-signed certificate: %v", err)
	}
	if got := said(relay); strings.Contains(got, "STARTTLS") || !strings.Contains(got, "DATA") {
		t.Errorf("a local relay was sent: %s", got)
	}
}
