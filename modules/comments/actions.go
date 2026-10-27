package comments

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/frontend"

	form "github.com/donseba/go-form/v2"
)

// Results of a visitor's post, added to the page as ?<key>=<result>.
const (
	// ResultSent is a comment that shows at once.
	ResultSent = "sent"
	// ResultPending is a comment that waits for moderation.
	ResultPending = "pending"
	// ResultInvalid is a post with a missing or too long field; nothing
	// was saved.
	ResultInvalid = "invalid"
	// ResultClosed is a post to a post whose comments are closed.
	ResultClosed = "closed"
	// ResultThrottled is the answer to a visitor who posted too often.
	ResultThrottled = frontend.ResultThrottled
)

const (
	bodyLimit = 64 << 10
	// Each visitor may post postLimit times per postWindow.
	postLimit  = 5
	postWindow = 10 * time.Minute
)

// PostInput is the form a visitor posts to /_/<key>/post: the subject the
// message belongs to, optionally the message it answers, a name, an
// optional e-mail address and the message. Themes render its fields with
// form_field, with their limits ({{form_field .Form "name" nil}}, see
// ListView, FormView and commentInput), or write inputs with these names.
type PostInput struct {
	Subject string `form:"input,hidden" name:"subject" required:"true" maxLength:"200"  line:"true"`
	Parent  string `form:"input,hidden" name:"parent"                  maxLength:"64"   line:"true"`
	Name    string `form:"input,text"   name:"name"    required:"true" maxLength:"100"  line:"true" label:"Name"           autocomplete:"name"`
	Email   string `form:"input,email"  name:"email"                   maxLength:"254"              label:"E-mail address" autocomplete:"email" validate:"mail" description:"Optional, never shown on the website."`
	Body    string `form:"textarea"     name:"body"    required:"true" maxLength:"5000"             label:"Message"`
}

// postForm is the form of PostInput for subject, posting to action, the
// instance's address for posts (see Service.postURL), for templates
// (frontend.VisitorForm): it returns to the element with the id key, and
// key goes before its fields' ids, such as remarks-name.
func postForm(ctx context.Context, action, key, subject string) form.RenderModel {
	return frontend.VisitorForm(ctx, PostInput{Subject: subject}, action, key)
}

// visitors receives the comment form at /_/<key>/post.
type visitors struct {
	service *Service
	env     webthings.Env
}

// register registers the actions on the instance's action router
// (webthings.Env.Actions).
func (v *visitors) register() {
	routes := v.service.actions

	routes.Use(frontend.Limits(v.service.key, frontend.ActionLimits{
		Body:   bodyLimit,
		Posts:  postLimit,
		Window: postWindow,
	}))
	routes.Post("/post", v.post).As("post")
}

func (v *visitors) post(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	key := v.service.key

	settings, err := v.service.Settings(ctx)
	if err != nil {
		v.fail(w, r, err)

		return
	}

	if settings.Closed {
		frontend.NothingChanged(r)
		frontend.Back(w, r, key, ResultClosed)

		return
	}

	result := ResultSent
	if settings.Moderated {
		result = ResultPending
	}

	// Bots that fill in the trap get the answer a person would get.
	if frontend.IsSpam(r) {
		frontend.Back(w, r, key, result)

		return
	}

	// The sender is known for one day only.
	visitor, err := v.env.VisitorID(ctx, r, time.Now().UTC().Format(time.DateOnly))
	if err != nil {
		v.fail(w, r, err)

		return
	}

	var in PostInput

	// The service checks the entry again, for code that adds comments
	// without a form.
	errs, err := v.env.Frontend.BindForm(r, &in)
	if err != nil || len(errs) > 0 {
		v.env.Frontend.KeepForm(w, r, key, in, errs)
		frontend.Back(w, r, key, ResultInvalid)

		return
	}

	entry := Entry{
		Subject:  in.Subject,
		ParentID: in.Parent,
		Name:     in.Name,
		Email:    in.Email,
		Body:     in.Body,
		Locale:   v.env.Frontend.BackLocale(r),
		Path:     frontend.BackPath(r),
		Approved: !settings.Moderated,
		IPHash:   visitor,
	}

	comment, err := v.service.Add(ctx, entry)
	if errors.Is(err, ErrInvalid) {
		v.env.Frontend.KeepForm(w, r, key, in, nil)
		frontend.Back(w, r, key, ResultInvalid)

		return
	}

	if err != nil {
		v.fail(w, r, err)

		return
	}

	v.notify(r, settings, entry, comment)
	frontend.Back(w, r, key, result)
}

// fail answers a post that could not be saved with the theme's error page
// (frontend.Frontend.Fail), which logs err.
func (v *visitors) fail(w http.ResponseWriter, r *http.Request, err error) {
	v.env.Frontend.Fail(w, r, fmt.Errorf("save comment: %w", err))
}

// mailNotify is the mail template that tells the moderators about a new
// message, see webthings.Module.Mail.
const mailNotify = "notify"

// NotifyView is the data of the mail template notify, sent to the
// notification address in the admin's language. Replying answers the
// visitor when they gave an address.
type NotifyView struct {
	Comment
	// Email is the visitor's address; empty when they gave none.
	Email string
	// Title is the instance's title, for example "Comments".
	Title string
	// Pending is set when the message waits for approval.
	Pending bool
	// PageURL is the page the message was written on; empty when unknown.
	PageURL string
	// ReviewURL opens the messages in the admin.
	ReviewURL string
}

const notifyMail = `{{/* @dot github.com/donseba/go-webthings/modules/comments.NotifyView */}}
{{/* Sent to the notification address of the settings, in the admin's language. */}}
{{define "subject"}}{{.Title}}: {{.Name}}{{end}}

{{define "text"}}
{{if .Pending}}{{tl localizer "A new message waits for approval."}}{{else}}{{tl localizer "A new message was posted."}}{{end}}

{{.Name}}{{with .Email}} <{{.}}>{{end}}

{{.Body}}
{{with .PageURL}}
{{tl localizer "Page:"}} {{.}}
{{- end}}
{{tl localizer "Review:"}} {{.ReviewURL}}
{{end}}
`

// notify mails the instance's notification address about a new message. The
// message is already stored, so a failed mail is only logged.
func (v *visitors) notify(r *http.Request, settings Settings, entry Entry, comment Comment) {
	if settings.Notify == "" {
		return
	}

	service := v.service

	ctx := r.Context()

	query := url.Values{}
	if !entry.Approved {
		query.Set("filter", filterPending)
	}

	review := v.env.AdminURL(admin.RouteName(service.key, "index"), query)

	view := NotifyView{
		Comment:   comment,
		Email:     strings.TrimSpace(entry.Email),
		Title:     service.title,
		Pending:   !entry.Approved,
		ReviewURL: v.env.AbsoluteURL(r, review),
	}

	if entry.Path != "" {
		view.PageURL = v.env.AbsoluteURL(r, entry.Path)
	}

	_ = service.frontend.SendMail(ctx, service.mail, frontend.Letter{
		Locale:  v.env.AdminLanguage,
		Key:     service.key,
		Name:    mailNotify,
		Data:    view,
		To:      []string{settings.Notify},
		ReplyTo: view.Email,
	})
}

// results are the results a page shows; see frontend.Result.
var results = []string{ResultSent, ResultPending, ResultInvalid, ResultClosed, ResultThrottled}
