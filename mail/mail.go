// Package mail sends e-mail for modules: confirmations, notifications and
// newsletters. A site picks the Sender in webthings.Config.Mail; SMTP is
// built in, other providers implement Sender. Without one, messages are
// logged instead of sent, which suits development.
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	form "github.com/donseba/go-form/v2"
)

// Message is one e-mail. Text is required; HTML is optional.
type Message struct {
	// To are the recipients' addresses; every recipient sees them all.
	To []string
	// ReplyTo is where replies go instead of the sender, such as the
	// visitor who filled in a contact form.
	ReplyTo string
	Subject string
	// Text is the plain text body, which every mail program can show.
	Text string
	// HTML is the optional rich body, sent next to Text.
	HTML string
	// Headers are extra header fields, for example List-Unsubscribe and
	// List-Unsubscribe-Post (RFC 8058) on newsletters. Compose refuses names
	// it writes itself, such as From or Content-Type, and line breaks.
	Headers map[string]string
}

// Sender delivers messages.
type Sender interface {
	Send(ctx context.Context, message Message) error
}

// Describer is a Sender that can say how it sends, for people who check
// the site's set-up, such as the admin's setup guide: "SMTP
// mail.example.com:587, from Site <noreply@example.com>". The description
// never holds a secret, such as a password or an API key. SMTP, Log and
// Memory implement it.
type Describer interface {
	Describe() string
}

// Log writes messages to the log instead of sending them.
type Log struct {
	Logger *slog.Logger
}

// Send implements Sender: it logs the recipients, subject and text at info
// level and never fails, so development needs no mail server.
func (l Log) Send(_ context.Context, message Message) error {
	logger := l.Logger
	if logger == nil {
		logger = slog.Default()
	}

	logger.Info("mail not sent (no sender configured)",
		"to", strings.Join(message.To, ", "),
		"subject", message.Subject,
		"text", message.Text,
	)

	return nil
}

// Describe implements Describer.
func (l Log) Describe() string {
	return "the server log, not sent"
}

// Memory keeps sent messages, for tests.
type Memory struct {
	mu   sync.Mutex
	sent []Message
}

// Send implements Sender: it keeps the message for Sent and never fails.
func (m *Memory) Send(_ context.Context, message Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sent = append(m.sent, message)

	return nil
}

// Sent returns the messages sent so far.
func (m *Memory) Sent() []Message {
	m.mu.Lock()
	defer m.mu.Unlock()

	return append([]Message(nil), m.sent...)
}

// Describe implements Describer.
func (m *Memory) Describe() string {
	return "memory, for tests, not sent"
}

// DefaultSMTPTimeout bounds one SMTP.Send when SMTP.Timeout is zero.
const DefaultSMTPTimeout = 30 * time.Second

// SMTP sends through a mail server, with PLAIN authentication when Username
// is set. STARTTLS is used when the server offers it (ports 587 and 25);
// implicit TLS on port 465 is not supported.
//
// Send stops when its context ends or after Timeout, whichever comes first,
// so a slow or hanging mail server cannot hold the caller indefinitely.
type SMTP struct {
	// Host is the mail server's name, also checked against its
	// certificate.
	Host string
	// Port is the server's port; 587 when zero.
	Port int
	// Username and Password sign in to the server; an empty Username
	// sends without signing in.
	Username string
	Password string
	// From is the sender's address, such as "Site <noreply@example.com>".
	From string
	// Timeout bounds the whole conversation with the server, from dialling
	// to QUIT. Zero uses DefaultSMTPTimeout.
	Timeout time.Duration
}

// Describe implements Describer: the server, its port and the From
// address, without the username and password.
func (s SMTP) Describe() string {
	port := s.Port
	if port == 0 {
		port = 587
	}

	return fmt.Sprintf("SMTP %s, from %s", net.JoinHostPort(s.Host, strconv.Itoa(port)), s.From)
}

// Send implements Sender: it delivers message to every recipient in one
// conversation with the server, from the From address.
func (s SMTP) Send(ctx context.Context, message Message) error {
	if len(message.To) == 0 {
		return errors.New("mail: no recipients")
	}

	from, err := mail.ParseAddress(s.From)
	if err != nil {
		return fmt.Errorf("mail: sender: %w", err)
	}

	port := s.Port
	if port == 0 {
		port = 587
	}

	var auth smtp.Auth
	if s.Username != "" {
		auth = smtp.PlainAuth("", s.Username, s.Password, s.Host)
	}

	body, err := Compose(s.From, message)
	if err != nil {
		return err
	}

	timeout := s.Timeout
	if timeout <= 0 {
		timeout = DefaultSMTPTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var dialer net.Dialer

	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(s.Host, fmt.Sprint(port)))
	if err != nil {
		return fmt.Errorf("mail: connect: %w", err)
	}

	// net/smtp knows no context. The deadline bounds every read and write,
	// and closing the connection when ctx is cancelled ends a conversation
	// that waits on the server.
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)

	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	err = s.converse(conn, auth, from.Address, message.To, body)
	if err == nil {
		return nil
	}

	if ctx.Err() != nil {
		return fmt.Errorf("mail: send: %w", ctx.Err())
	}

	// The connection's deadline can pass a moment before ctx reports it.
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return fmt.Errorf("mail: send: %w", context.DeadlineExceeded)
	}

	return err
}

// converse is smtp.SendMail on an open connection: greeting, STARTTLS when
// offered, authentication, envelope, message and QUIT.
func (s SMTP) converse(conn net.Conn, auth smtp.Auth, from string, to []string, body []byte) error {
	client, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		_ = conn.Close()

		return err
	}

	defer func() { _ = client.Close() }()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: s.Host}); err != nil {
			return err
		}
	}

	if auth != nil {
		if ok, _ := client.Extension("AUTH"); !ok {
			return errors.New("mail: the server does not support AUTH")
		}

		if err := client.Auth(auth); err != nil {
			return err
		}
	}

	if err := client.Mail(from); err != nil {
		return err
	}

	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}

	writer, err := client.Data()
	if err != nil {
		return err
	}

	if _, err := writer.Write(body); err != nil {
		return err
	}

	if err := writer.Close(); err != nil {
		return err
	}

	return client.Quit()
}

// reserved are the header fields Compose writes itself; Message.Headers may
// not replace them, and Bcc would leak or add recipients.
var reserved = []string{
	"bcc", "cc", "content-transfer-encoding", "content-type", "date", "from",
	"mime-version", "reply-to", "sender", "subject", "to",
}

// Compose renders a message as RFC 5322 bytes, multipart when HTML is set.
func Compose(from string, message Message) ([]byte, error) {
	for _, value := range append([]string{from, message.ReplyTo, message.Subject}, message.To...) {
		if strings.ContainsAny(value, "\r\n") {
			return nil, errors.New("mail: header contains a line break")
		}
	}

	extra, err := extraHeaders(message.Headers)
	if err != nil {
		return nil, err
	}

	var b strings.Builder

	header := func(name, value string) {
		if value != "" {
			b.WriteString(name + ": " + value + "\r\n")
		}
	}

	header("From", from)
	header("To", strings.Join(message.To, ", "))
	header("Reply-To", message.ReplyTo)
	header("Subject", mime.QEncoding.Encode("utf-8", message.Subject))
	header("Date", time.Now().Format(time.RFC1123Z))

	for _, name := range extra {
		header(name, mime.QEncoding.Encode("utf-8", message.Headers[name]))
	}

	header("MIME-Version", "1.0")

	if message.HTML == "" {
		header("Content-Type", `text/plain; charset="utf-8"`)
		b.WriteString("\r\n" + message.Text)

		return []byte(b.String()), nil
	}

	boundary := fmt.Sprintf("webthings-%d", time.Now().UnixNano())

	header("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	b.WriteString("\r\n--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n\r\n" + message.Text + "\r\n")
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n\r\n" + message.HTML + "\r\n")
	b.WriteString("--" + boundary + "--\r\n")

	return []byte(b.String()), nil
}

// extraHeaders checks Message.Headers and returns their names in a fixed
// order. A name must be a plain field name (RFC 5322: printable ASCII
// without colon) that Compose does not write itself, and no value may hold a
// line break, so headers cannot be injected.
func extraHeaders(headers map[string]string) ([]string, error) {
	names := make([]string, 0, len(headers))

	for name, value := range headers {
		if !fieldName(name) {
			return nil, fmt.Errorf("mail: invalid header name %q", name)
		}

		if slices.Contains(reserved, strings.ToLower(name)) {
			return nil, fmt.Errorf("mail: header %s is set by the message itself", name)
		}

		if strings.ContainsAny(value, "\r\n") {
			return nil, errors.New("mail: header contains a line break")
		}

		names = append(names, name)
	}

	slices.Sort(names)

	return names, nil
}

func fieldName(name string) bool {
	if name == "" {
		return false
	}

	for _, r := range name {
		if r < 33 || r > 126 || r == ':' {
			return false
		}
	}

	return true
}

// Valid reports whether address is a single plain e-mail address, as go-form
// checks email inputs (form.ValidEmail), with a dot in it: mail is sent to
// addresses on the internet, not to local hosts.
func Valid(address string) bool {
	return form.ValidEmail(address) && strings.Contains(address, ".")
}

// Rule is the name of the go-form validation of addresses mail can be sent
// to, validate:"mail" on a field, which the site's visitor forms and the
// admin's forms register (see FormRule):
//
//	Email string `form:"input,email" name:"email" required:"true" validate:"mail"`
const Rule = "mail"

// FormRule is the go-form validation Rule (a form.LocalizedValidationFunc):
// an address that is not Valid gets go-form's message of an invalid e-mail
// address (form.TranslationKeyInvalidEmail), translated like go-form's own
// messages, at its field. An empty value passes; required checks that. An
// address go-form already refuses itself, the value of an email input
// (form:"input,email" or email:"true") that is no address at all, gets no
// second message.
func FormRule(value any, field reflect.StructField, _ form.Localizer) form.FieldErrors {
	var address string

	switch v := value.(type) {
	case string:
		address = v
	case *string:
		if v != nil {
			address = *v
		}
	}

	if address == "" || Valid(address) {
		return nil
	}

	emailInput := strings.TrimSpace(field.Tag.Get("form")) == "input,email" || field.Tag.Get("email") == "true"
	if emailInput && !form.ValidEmail(address) {
		return nil
	}

	return form.FieldErrors{form.FieldValidationError{Field: field.Name, Err: form.TranslationKeyInvalidEmail}}
}
