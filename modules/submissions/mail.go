package submissions

import (
	"context"

	"github.com/donseba/go-webthings/frontend"
)

// Mail templates of the module, see webthings.Module.Mail.
const (
	// mailReceived confirms a proposal to the submitter.
	mailReceived = "received"
	// mailAccepted and mailRejected tell the submitter the editors' decision.
	mailAccepted = "accepted"
	mailRejected = "rejected"
	// mailNotify tells the editors about a new proposal.
	mailNotify = "notify"
)

// MailView is the data of the mail templates: the submission, and for the
// editors' notification the links to its image and to the admin.
type MailView struct {
	Submission
	// ImageURL is the absolute address of the image; empty without one, and
	// in mail to the submitter.
	ImageURL string
	// AdminURL opens the submission in the admin; empty in mail to the
	// submitter.
	AdminURL string
}

// mailTemplates are the module's mail templates with their default text.
func mailTemplates() []frontend.Template {
	return []frontend.Template{
		{Name: mailReceived, Data: MailView{}, Example: receivedMail},
		{Name: mailAccepted, Data: MailView{}, Example: acceptedMail},
		{Name: mailRejected, Data: MailView{}, Example: rejectedMail},
		{Name: mailNotify, Data: MailView{}, Example: notifyMail},
	}
}

const receivedMail = `{{/* @dot github.com/donseba/go-webthings/modules/submissions.MailView */}}
{{/* Sent to the submitter after sending a proposal, when the settings ask for it. */}}
{{define "subject"}}{{tl localizer "We received your proposal"}}: {{.Title}}{{end}}

{{define "text"}}
{{tl localizer "Hello %s," .Name}}

{{tl localizer "Thank you for your proposal. The editors will read it and decide whether to publish it."}}

{{.Title}}
{{end}}
`

const acceptedMail = `{{/* @dot github.com/donseba/go-webthings/modules/submissions.MailView */}}
{{/* Sent to the submitter when an editor accepts the proposal, when the settings ask for it. */}}
{{define "subject"}}{{tl localizer "Your proposal was accepted"}}: {{.Title}}{{end}}

{{define "text"}}
{{tl localizer "Hello %s," .Name}}

{{tl localizer "Good news: the editors accepted your proposal. It will be published after editing."}}

{{.Title}}
{{end}}
`

const rejectedMail = `{{/* @dot github.com/donseba/go-webthings/modules/submissions.MailView */}}
{{/* Sent to the submitter when an editor rejects the proposal, when the settings ask for it. */}}
{{define "subject"}}{{tl localizer "About your proposal"}}: {{.Title}}{{end}}

{{define "text"}}
{{tl localizer "Hello %s," .Name}}

{{tl localizer "Thank you for your proposal. The editors decided not to publish it."}}

{{.Title}}
{{end}}
`

const notifyMail = `{{/* @dot github.com/donseba/go-webthings/modules/submissions.MailView */}}
{{/* Sent to the notification address of the settings, in the admin's language. Replying answers the submitter. */}}
{{define "subject"}}{{tl localizer "New proposal"}}: {{.Title}}{{end}}

{{define "text"}}
{{.Name}} <{{.Email}}>

{{.Title}}

{{.Body}}
{{with .ImageURL}}
{{.}}
{{end}}
{{tl localizer "Review the proposal:"}}
{{.AdminURL}}
{{end}}
`

// notifyEditors mails a new submission to the instance's notification
// address, in the admin's language. Replying answers the submitter.
func (s *Service) notifyEditors(ctx context.Context, settings Settings, view MailView) {
	if settings.Notify == "" {
		return
	}

	s.send(ctx, s.adminLanguage, mailNotify, view, settings.Notify, view.Email)
}

// tellSubmitter sends the submitter the mail named kind in the submission's
// language, when the settings ask for it.
func (s *Service) tellSubmitter(ctx context.Context, settings Settings, submission Submission, kind string) {
	if kind == mailReceived && !settings.ConfirmMail || kind != mailReceived && !settings.DecisionMail {
		return
	}

	s.send(ctx, submission.Locale, kind, MailView{Submission: submission}, submission.Email, "")
}

// send writes the mail template name in locale and delivers it to to. The
// submission is stored either way, so a failed mail is only logged.
func (s *Service) send(ctx context.Context, locale, name string, view MailView, to, replyTo string) {
	_ = s.frontend.SendMail(ctx, s.mail, frontend.Letter{
		Locale:  locale,
		Key:     s.key,
		Name:    name,
		Data:    view,
		To:      []string{to},
		ReplyTo: replyTo,
	})
}
