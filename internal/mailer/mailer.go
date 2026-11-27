package mailer

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	gomail "github.com/wneessen/go-mail"
)

// Server is an SMTP submission service and the address mail leaves from. Every
// provider worth using offers SMTP, so it is the one transport.
type Server struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

type Message struct {
	To, Subject, Body string
	// ReplyTo is where an answer should go when that isn't the sender; empty for none.
	ReplyTo string
	// Unsubscribe is the link that stops marketing mail to this address, for mail providers'
	// one-click button; empty for mail that isn't marketing.
	Unsubscribe string
}

// SendFunc is how mail leaves; tests put a recorder in its place.
type SendFunc func(ctx context.Context, via Server, m Message) error

const SendLimit = 30 * time.Second

// mailPorts are the only ones a server anywhere but this machine may be reached on,
// so a mail setting can't be pointed at some other service beside the app.
var mailPorts = map[int]bool{25: true, 465: true, 587: true, 2525: true}

func Loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (m Server) Check() error {
	if strings.TrimSpace(m.Host) == "" || strings.ContainsAny(m.Host, " /@\r\n") {
		return fmt.Errorf("%q isn't a mail server's name", m.Host)
	}
	if m.Port < 1 || m.Port > 65535 || (!Loopback(m.Host) && !mailPorts[m.Port]) {
		return fmt.Errorf("mail servers listen on 587, 465, 2525 or 25, not %d", m.Port)
	}
	if strings.ContainsAny(m.From, "\r\n") {
		return errors.New("the From address can't span lines")
	}
	if _, err := mail.ParseAddress(m.From); err != nil {
		return fmt.Errorf("the From address should look like Name <name@example.com>, not %q", m.From)
	}
	return nil
}

// FromEnv reads the app's own server: SMTP_ADDR with MAIL_FROM, or neither.
func FromEnv(env func(string) string) (*Server, error) {
	addr, from := strings.TrimSpace(env("SMTP_ADDR")), strings.TrimSpace(env("MAIL_FROM"))
	if addr == "" && from == "" {
		return nil, nil
	}
	if addr == "" || from == "" {
		return nil, errors.New("SMTP_ADDR and MAIL_FROM go together: set both, or neither")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("SMTP_ADDR should look like smtp.example.com:587, not %q", addr)
	}
	n, _ := strconv.Atoi(port)
	m := &Server{Host: host, Port: n, Username: env("SMTP_USERNAME"), Password: env("SMTP_PASSWORD"), From: from}
	return m, m.Check()
}

var (
	// ErrUnsent is a failure before the message was handed over: it certainly didn't go,
	// and can be tried again.
	ErrUnsent = errors.New("not sent")
	// ErrRefusedRecipient is the server refusing the one address: nothing went, and
	// trying again won't change that.
	ErrRefusedRecipient = errors.New("the mail server refused the address")
)

func unsent(err error) error { return fmt.Errorf("%w: %w", ErrUnsent, err) }

// Send never sends a password or a message in the clear, except to this machine, and
// waits no longer than SendLimit. A failure before the message is handed over wraps
// ErrUnsent, or ErrRefusedRecipient; any other leaves it unknown whether the mail went.
func Send(ctx context.Context, via Server, m Message) error {
	if err := via.Check(); err != nil {
		return unsent(err)
	}
	from, _ := mail.ParseAddress(via.From)
	to, err := mail.ParseAddress(m.To)
	if err != nil || strings.ContainsAny(m.To, "\r\n") {
		return fmt.Errorf("%w: %q isn't an email address", ErrRefusedRecipient, m.To)
	}
	ctx, cancel := context.WithTimeout(ctx, SendLimit)
	defer cancel()
	secure := &tls.Config{ServerName: via.Host, MinVersion: tls.VersionTLS12}
	return submit(ctx, (&net.Dialer{}).DialContext, via, from, to, m, secure, !Loopback(via.Host))
}

// submit holds the conversation through dial. Port 465 is TLS from the first byte; any
// other port must offer STARTTLS when needTLS. A server on this machine is spoken to in
// the clear even when it offers TLS: its certificate is usually self-signed, and nothing
// leaves the machine.
func submit(ctx context.Context, dial gomail.DialContextFunc, via Server, from, to *mail.Address, m Message, secure *tls.Config, needTLS bool) error {
	addr := net.JoinHostPort(via.Host, strconv.Itoa(via.Port))
	msg, err := compose(from, to, m, time.Now())
	if err != nil {
		return unsent(err)
	}
	// Greeted as localhost, as net/smtp did, rather than with this machine's own name.
	opts := []gomail.Option{gomail.WithPort(via.Port), gomail.WithTLSConfig(secure), gomail.WithDialContextFunc(dial),
		gomail.WithTimeout(SendLimit), gomail.WithHELO("localhost")}
	switch {
	case via.Port == 465:
		opts = append(opts, gomail.WithSSL())
	case needTLS:
		opts = append(opts, gomail.WithTLSPolicy(gomail.TLSMandatory))
	default:
		opts = append(opts, gomail.WithTLSPolicy(gomail.NoTLS))
	}
	if via.Username != "" {
		// The strongest of the mechanisms the server lists, over TLS. Only a server on
		// this machine, spoken to in the clear, is given a plain password without it.
		auth := gomail.SMTPAuthAutoDiscover
		if via.Port != 465 && !needTLS {
			auth = gomail.SMTPAuthPlainNoEnc
		}
		opts = append(opts, gomail.WithSMTPAuth(auth), gomail.WithUsername(via.Username), gomail.WithPassword(via.Password))
	}
	c, err := gomail.NewClient(via.Host, opts...)
	if err != nil {
		return unsent(err)
	}
	// Reaching the server, securing the line and signing in all come before anything of
	// the message is sent.
	if err := c.DialWithContext(ctx); err != nil {
		return unsent(fmt.Errorf("%s: %w", addr, err))
	}
	// The server took the message when the data closed. A goodbye that fails after that
	// isn't a failed send, and calling it one would have the same mail sent twice.
	defer func() { _ = c.Close() }()
	return settled(addr, to, c.Send(msg))
}

// settled says what a failed send means: refused before the message was handed over,
// it didn't go; a 5xx to the address is the address refused for good, a 4xx only "not
// now" (a rate limit, greylisting). Anything after the data began is unknown.
func settled(addr string, to *mail.Address, err error) error {
	var failed *gomail.SendError
	if err == nil || !errors.As(err, &failed) {
		return err
	}
	switch failed.Reason {
	case gomail.ErrSMTPReset:
		// Only asked once the server has taken the message, to ready the line for
		// another: it went.
		return nil
	case gomail.ErrSMTPRcptTo:
		if failed.IsTemp() {
			return unsent(fmt.Errorf("%s put off mail to %s: %w", addr, to.Address, err))
		}
		return fmt.Errorf("%w: %s refused mail to %s: %w", ErrRefusedRecipient, addr, to.Address, err)
	case gomail.ErrGetSender, gomail.ErrGetRcpts, gomail.ErrSMTPMailFrom, gomail.ErrSMTPData, gomail.ErrNoUnencoded, gomail.ErrConnCheck:
		return unsent(fmt.Errorf("%s: %w", addr, err))
	}
	return err
}

func compose(from, to *mail.Address, m Message, now time.Time) (*gomail.Msg, error) {
	// No User-Agent or X-Mailer: what software sends it is nobody's business.
	msg := gomail.NewMsg(gomail.WithCharset(gomail.CharsetUTF8), gomail.WithEncoding(gomail.EncodingQP), gomail.WithNoDefaultUserAgent())
	if err := msg.From(from.String()); err != nil {
		return nil, err
	}
	if err := msg.To(to.String()); err != nil {
		return nil, err
	}
	msg.Subject(strings.Join(strings.Fields(m.Subject), " "))
	// Parsed and written back out, so nothing in it can start a header of its own.
	if answer, err := mail.ParseAddress(m.ReplyTo); err == nil {
		if err := msg.ReplyTo(answer.Address); err != nil {
			return nil, err
		}
	}
	// RFC 8058 one-click: only over HTTPS, and only a link we made, never one with room
	// in it to start a header of its own.
	if u, err := url.Parse(m.Unsubscribe); err == nil && u.Scheme == "https" && printableASCII(m.Unsubscribe) && !strings.ContainsAny(m.Unsubscribe, "<> ,") {
		if err := msg.SetListUnsubscribeOneClick(m.Unsubscribe); err != nil {
			return nil, err
		}
	}
	_, domain, _ := strings.Cut(from.Address, "@")
	msg.SetMessageIDWithValue(rand.Text() + "@" + domain)
	msg.SetDateWithValue(now)
	msg.SetBodyString(gomail.TypeTextPlain, m.Body)
	return msg, nil
}

// Compose is the message as it goes over the wire, for reading it back.
func Compose(from, to *mail.Address, m Message, now time.Time) []byte {
	msg, err := compose(from, to, m, now)
	if err != nil {
		return nil
	}
	var b bytes.Buffer
	if _, err := msg.WriteTo(&b); err != nil {
		return nil
	}
	return b.Bytes()
}

// printableASCII is text a header can carry as it is: no control characters, nothing
// outside ASCII.
func printableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return true
}
