package forms

import (
	"net/http"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/mail"
)

// mailNotify is the mail template that tells the notification address about
// a new submission, see webthings.Module.Mail.
const mailNotify = "notify"

// NotifyView is the data of the mail template notify, sent to the form's
// notification address in the admin's language. Replying answers the
// visitor when the form has an email field.
type NotifyView struct {
	// Title is the form's title in the language the visitor used.
	Title string
	// Locale is the language the visitor used.
	Locale string
	// Answers are the submitted values, each with the label the visitor saw.
	Answers Answers
	// ReplyTo is the first address the visitor gave in an email field; empty
	// when there is none.
	ReplyTo string
	// SubmissionURL opens the submission in the admin.
	SubmissionURL string
}

// notifyMail is the default notify template. It lists every answer under the
// label the visitor saw and has no fixed wording, so it reads well in any
// language.
const notifyMail = `{{/* @dot github.com/donseba/go-webthings/modules/forms.NotifyView */}}
{{/* Sent to the form's notification address for every submission, in the admin's language. */}}
{{define "subject"}}{{.Title}}{{end}}

{{define "text"}}
{{range .Answers -}}
{{.Label}}:
{{or .Value "-"}}

{{end -}}
{{.SubmissionURL}}
{{end}}
`

// notify mails a new submission to the form's notification address. The
// submission is already stored, so a failed mail is only logged.
func (s *Service) notify(r *http.Request, item Item, locale, id string, answers Answers) {
	if item.Fields.Notify == "" {
		return
	}

	ctx := r.Context()
	text, _ := item.Text(locale)
	var link string

	if s.adminURL != nil {
		link = s.adminURL(admin.RouteName(s.key, "submission"), "submission", id)
	}

	if s.absolute != nil && link != "" {
		link = s.absolute(r, link)
	}

	view := NotifyView{
		Title:         text.Title,
		Locale:        locale,
		Answers:       answers,
		ReplyTo:       replyTo(item.Fields.Fields, answers),
		SubmissionURL: link,
	}

	_ = s.frontend.SendMail(ctx, s.mail, frontend.Letter{
		Locale:  s.adminLanguage,
		Key:     s.key,
		Name:    mailNotify,
		Data:    view,
		To:      []string{item.Fields.Notify},
		ReplyTo: view.ReplyTo,
	})
}

// replyTo is the first address the visitor gave in an email field, so
// replying to the notification answers the visitor.
func replyTo(fields FieldList, answers Answers) string {
	for _, field := range fields {
		if field.Type != "email" {
			continue
		}

		for _, answer := range answers {
			if answer.Name == field.Name && mail.Valid(answer.Value) {
				return answer.Value
			}
		}
	}

	return ""
}
