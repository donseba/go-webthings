package frontend

import (
	"context"
	"log/slog"

	"github.com/donseba/go-webthings/mail"
)

// Letter is a module e-mail to send with SendMail: the mail template Name
// of the module instance Key (see Mail), written in Locale with Data.
type Letter struct {
	// Locale is the language the mail is written in, such as the visitor's
	// language, or the admin language for a notification to staff.
	Locale string
	// Key is the module instance, Name the mail template of its Module.Mail.
	Key  string
	Name string
	// Data is what the template gets as its dot.
	Data any
	// To are the recipients' addresses.
	To []string
	// ReplyTo is the address answers go to, such as the visitor who wrote a
	// message; empty for the sender's own address.
	ReplyTo string
}

// SendMail writes letter with its mail template and sends it with sender.
//
// It is meant for mail a visitor's post or an editor's action causes, after
// the post itself was stored: the mail is sent without the request's
// cancellation, so it still goes out when the visitor leaves the page
// (the sender's own timeout bounds it), and a failure is logged with the
// module and the mail's name. The error is returned for callers that tell
// the visitor; most ignore it.
func (f *Frontend) SendMail(ctx context.Context, sender mail.Sender, letter Letter) error {
	message, err := f.Mail(ctx, letter.Locale, letter.Key, letter.Name, letter.Data)
	if err != nil {
		slog.ErrorContext(ctx, "write mail", "module", letter.Key, "mail", letter.Name, "error", err)

		return err
	}

	message.To = letter.To
	message.ReplyTo = letter.ReplyTo

	err = sender.Send(context.WithoutCancel(ctx), message)
	if err != nil {
		slog.ErrorContext(ctx, "send mail", "module", letter.Key, "mail", letter.Name, "error", err)
	}

	return err
}
