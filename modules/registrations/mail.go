package registrations

import (
	"context"
	"errors"
	"log/slog"

	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/mail"
)

// Mail templates of the module, see webthings.Module.Mail.
const (
	mailConfirmation = "confirmation"
	mailCancellation = "cancellation"
)

// MailView is the data of the mail templates confirmation and cancellation,
// in the language the visitor registered in.
type MailView struct {
	// Subject is what the visitor registered for; Start is set for events.
	Subject  Subject
	Code     string
	Name     string
	Adults   int
	Children int
	// Total is in Currency, for {{money .Total .Currency}}.
	Total    Cents
	Currency string
	// Payment is none, pending, paid or cancelled.
	Payment PaymentStatus
	// PaymentLink is the editor's payment link while payment is pending.
	PaymentLink string
	// Notes are the visitor's own remarks; Info the editor's notes for
	// registrants, as the Markdown they wrote, which reads well as text.
	Notes string
	Info  string
	// URL is the registration's page, to view or cancel it; empty when the
	// module is on no page.
	URL string
}

// mailTemplates are the module's mail templates with their default text.
func mailTemplates() []frontend.Template {
	return []frontend.Template{
		{Name: mailConfirmation, Data: MailView{}, Example: confirmationMail},
		{Name: mailCancellation, Data: MailView{}, Example: cancellationMail},
	}
}

const confirmationMail = `{{/* @dot github.com/donseba/go-webthings/modules/registrations.MailView */}}
{{/* Sent to the visitor after registering. */}}
{{define "subject"}}{{tl localizer "Registration"}} {{.Code}}: {{.Subject.Title}}{{end}}

{{define "text"}}
{{tl localizer "Hello %s," .Name}}

{{tl localizer "Thank you for registering for %s." .Subject.Title}}
{{- with .Subject.When}}{{if not .IsZero}}
{{if .AllDay}}{{dateRange .Start .End "full"}}{{else if .SameDay}}{{date .Start "full"}} · {{timeRange .Start .End}}{{else}}{{date .Start "full"}}, {{time .Start}} – {{date .End "full"}}, {{time .End}}{{end}}
{{- end}}{{end}}

{{tl localizer "Registration code"}}: {{.Code}}
{{tl localizer "Adults"}}: {{.Adults}}
{{- if .Children}}
{{tl localizer "Children"}}: {{.Children}}
{{- end}}
{{- if .Total}}
{{tl localizer "Total"}}: {{money .Total .Currency}}
{{- end}}
{{- with .PaymentLink}}

{{tl localizer "Please pay using:"}}
{{.}}
{{- end}}
{{- with .Info}}

{{.}}
{{- end}}
{{- with .URL}}

{{tl localizer "View or cancel your registration:"}}
{{.}}
{{- end}}
{{end}}
`

const cancellationMail = `{{/* @dot github.com/donseba/go-webthings/modules/registrations.MailView */}}
{{/* Sent to the visitor after cancelling a registration on the site. */}}
{{define "subject"}}{{tl localizer "Registration cancelled"}} {{.Code}}: {{.Subject.Title}}{{end}}

{{define "text"}}
{{tl localizer "Hello %s," .Name}}

{{tl localizer "Your registration for %s is cancelled." .Subject.Title}}

{{tl localizer "Registration code"}}: {{.Code}}
{{- if eq .Payment "paid"}}

{{tl localizer "You already paid; we will contact you about a refund."}}
{{- end}}
{{end}}
`

// mailView is the mail data of a registration, in its language; page is
// the absolute address of the registration's page, or "".
func (s *Service) mailView(ctx context.Context, registration Registration, page string) (MailView, error) {
	view, err := s.registrationView(ctx, registration, registration.Locale)
	if err != nil {
		return MailView{}, err
	}

	settings, err := s.Settings(ctx, registration.SubjectID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return MailView{}, err
	}

	return MailView{
		Subject:     view.Subject,
		Code:        view.Code,
		Name:        view.Name,
		Adults:      view.Adults,
		Children:    view.Children,
		Total:       view.Total,
		Currency:    view.Currency,
		Payment:     view.Payment,
		PaymentLink: view.PaymentLink,
		Notes:       view.Notes,
		Info:        s.text(ctx, settings, registration.Locale).Notes,
		URL:         page,
	}, nil
}

// sendMail writes the mail template name for a registration and sends it to
// the visitor. A failure is logged: the registration stands, and the
// visitor sees it on the page.
func (s *Service) sendMail(ctx context.Context, name string, registration Registration, page string) {
	view, err := s.mailView(ctx, registration, page)
	if err != nil {
		slog.ErrorContext(ctx, "registration mail", "module", s.key, "mail", name, "error", err)

		return
	}

	_ = s.frontend.SendMail(ctx, s.mail, frontend.Letter{
		Locale:  registration.Locale,
		Key:     s.key,
		Name:    name,
		Data:    view,
		To:      []string{registration.Email},
		ReplyTo: s.replyTo(ctx),
	})
}

// replyTo is the site's e-mail address, so visitors can answer the
// confirmation.
func (s *Service) replyTo(ctx context.Context) string {
	settings, err := s.site.Settings(ctx)
	if err != nil || !mail.Valid(settings.Email) {
		return ""
	}

	return settings.Email
}
