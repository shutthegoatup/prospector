package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// MailServer is an SMTP submission service and the address mail leaves from. Every
// provider worth using offers SMTP, so it is the one transport.
type MailServer struct {
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
}

// sendFunc is how mail leaves; tests put a recorder in its place.
type sendFunc func(ctx context.Context, via MailServer, m Message) error

const sendLimit = 30 * time.Second

// mailPorts are the only ones a server anywhere but this machine may be reached on,
// so a mail setting can't be pointed at some other service beside the app.
var mailPorts = map[int]bool{25: true, 465: true, 587: true, 2525: true}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (m MailServer) check() error {
	if strings.TrimSpace(m.Host) == "" || strings.ContainsAny(m.Host, " /@\r\n") {
		return fmt.Errorf("%q isn't a mail server's name", m.Host)
	}
	if m.Port < 1 || m.Port > 65535 || (!loopback(m.Host) && !mailPorts[m.Port]) {
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

// mailFromEnv reads the app's own server: SMTP_ADDR with MAIL_FROM, or neither.
func mailFromEnv(env func(string) string) (*MailServer, error) {
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
	m := &MailServer{Host: host, Port: n, Username: env("SMTP_USERNAME"), Password: env("SMTP_PASSWORD"), From: from}
	return m, m.check()
}

// smtpSend never sends a password or a message in the clear, except to this machine:
// net/smtp's own SendMail only uses TLS when offered, and waits forever.
func smtpSend(ctx context.Context, via MailServer, m Message) error {
	if err := via.check(); err != nil {
		return err
	}
	from, _ := mail.ParseAddress(via.From)
	to, err := mail.ParseAddress(m.To)
	if err != nil || strings.ContainsAny(m.To, "\r\n") {
		return fmt.Errorf("%q isn't an email address", m.To)
	}
	ctx, cancel := context.WithTimeout(ctx, sendLimit)
	defer cancel()
	addr := net.JoinHostPort(via.Host, strconv.Itoa(via.Port))
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("reach %s: %w", addr, err)
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)
	secure := &tls.Config{ServerName: via.Host, MinVersion: tls.VersionTLS12}
	return submit(conn, via, from, to, m, secure, !loopback(via.Host))
}

// submit holds the conversation with a server already reached. Port 465 is TLS from
// the first byte; any other port must offer STARTTLS when needTLS.
func submit(conn net.Conn, via MailServer, from, to *mail.Address, m Message, secure *tls.Config, needTLS bool) error {
	addr := net.JoinHostPort(via.Host, strconv.Itoa(via.Port))
	if via.Port == 465 {
		conn = tls.Client(conn, secure)
	}
	c, err := smtp.NewClient(conn, via.Host)
	if err != nil {
		return fmt.Errorf("%s: %w", addr, err)
	}
	defer c.Close()
	// A server on this machine is spoken to in the clear even when it offers TLS: its
	// certificate is usually self-signed, and nothing leaves the machine.
	if via.Port != 465 && needTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("%s doesn't offer TLS, so nothing was sent", addr)
		}
		if err := c.StartTLS(secure); err != nil {
			return fmt.Errorf("%s: %w", addr, err)
		}
	}
	if via.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", via.Username, via.Password, via.Host)); err != nil {
			return fmt.Errorf("%s refused the username and password: %w", addr, err)
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("%s refused mail from %s: %w", addr, from.Address, err)
	}
	if err := c.Rcpt(to.Address); err != nil {
		return fmt.Errorf("%s refused mail to %s: %w", addr, to.Address, err)
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(compose(from, to, m, time.Now())); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func compose(from, to *mail.Address, m Message, now time.Time) []byte {
	var b bytes.Buffer
	_, domain, _ := strings.Cut(from.Address, "@")
	subject := strings.Join(strings.Fields(m.Subject), " ")
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\n", from.String(), to.String(), mime.QEncoding.Encode("utf-8", subject))
	// Parsed and written back out, so nothing in it can start a header of its own.
	if answer, err := mail.ParseAddress(m.ReplyTo); err == nil {
		fmt.Fprintf(&b, "Reply-To: %s\r\n", (&mail.Address{Address: answer.Address}).String())
	}
	fmt.Fprintf(&b, "Date: %s\r\nMessage-ID: <%s@%s>\r\n", now.Format(time.RFC1123Z), randomToken(16), domain)
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
	body := quotedprintable.NewWriter(&b)
	body.Write([]byte(strings.ReplaceAll(strings.ReplaceAll(m.Body, "\r\n", "\n"), "\n", "\r\n")))
	body.Close()
	return b.Bytes()
}
