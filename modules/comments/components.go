package comments

import (
	"context"
	"html/template"
	"log/slog"

	"github.com/donseba/go-webthings/frontend"

	form "github.com/donseba/go-form/v2"
)

// ThreadView is the data of the thread component: the approved comments on
// a subject, followed by the form (the form component).
type ThreadView struct {
	// Key is the instance key, for {{commentForm .Key .Subject}}.
	Key     string
	Subject string
	// Comments are oldest first, each with its approved Replies.
	Comments []Comment
	// Count is the number of approved comments and replies.
	Count int
}

// FormView is the data of the form component: the comment form of a subject
// with the result of the visitor's last post.
type FormView struct {
	Key     string
	Subject string
	// Action is the address the form posts to.
	Action string
	// Form is the form's model (PostInput) for go-form's template
	// functions, such as {{form_open .Form}} and {{form_field .Form "name"
	// nil}}; it returns the visitor to the element with the id Key.
	Form form.RenderModel
	// Result is "sent", "pending", "invalid", "closed", "throttled" or "".
	Result string
	// Closed is set when the instance accepts no new comments.
	Closed bool
	// Moderated is set when new comments wait for approval.
	Moderated bool
}

// components are the parts of a page Comments offers to every template.
func components() []frontend.Template {
	return []frontend.Template{
		{Name: "thread", Data: ThreadView{}, Example: threadComponent},
		{Name: "form", Data: FormView{}, Example: formComponent},
	}
}

// thread renders the thread component, for example {{commentThread
// "remarks" .ID}} below a post.
func commentThread(ctx context.Context, key, subject string) (template.HTML, error) {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return "", nil
	}

	view := ThreadView{Key: key, Subject: subject}

	var err error

	view.Comments, err = service.List(ctx, subject)
	if err != nil {
		slog.ErrorContext(ctx, "template commentThread", "module", key, "error", err)
	}

	view.Count, err = service.Count(ctx, subject)
	if err != nil {
		slog.ErrorContext(ctx, "template commentThread", "module", key, "error", err)
	}

	return frontend.Component(ctx, key, "thread", view)
}

// commentForm renders the form component, for example {{commentForm
// "guestbook" "guestbook"}} on its own.
func commentForm(ctx context.Context, key, subject string) (template.HTML, error) {
	service, ok := frontend.ServiceOf[*Service](ctx, key)
	if !ok {
		return "", nil
	}

	settings, err := service.Settings(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "template commentForm", "module", key, "error", err)
	}

	view := FormView{
		Key:       key,
		Subject:   subject,
		Action:    service.postURL(),
		Form:      postForm(ctx, service.postURL(), key, subject),
		Result:    result(ctx, key),
		Closed:    settings.Closed,
		Moderated: settings.Moderated,
	}

	return frontend.Component(ctx, key, "form", view)
}

// threadComponent is the default comments/thread.gohtml.
const threadComponent = `{{/* @dot github.com/donseba/go-webthings/modules/comments.ThreadView */}}
{{/* The comments on a subject, then the form: {{commentThread "remarks" .ID}}. */}}
<section class="wt-comments" id="{{.Key}}-comments" aria-labelledby="{{.Key}}-comments-title">
    <h2 class="wt-comments__title" id="{{.Key}}-comments-title">
        {{if .Count}}{{tl localizer "Comments (%d)" .Count}}{{else}}{{tl localizer "Comments"}}{{end}}
    </h2>

    {{if .Comments}}
        <ol class="wt-comments__list">
            {{range .Comments}}
                <li class="wt-comment" id="comment-{{.ID}}">
                    <header class="wt-comment__header">
                        <strong class="wt-comment__name">{{.Name}}</strong>
                        <time class="wt-comment__date wt-meta" datetime="{{.CreatedAt.Format "2006-01-02T15:04:05Z07:00"}}">{{date .CreatedAt}}</time>
                    </header>
                    <div class="wt-comment__body">{{.HTML}}</div>

                    {{with .Replies}}
                        <ol class="wt-comment__replies">
                            {{range .}}
                                <li class="wt-comment wt-comment--reply{{if .Staff}} wt-comment--staff{{end}}" id="comment-{{.ID}}">
                                    <header class="wt-comment__header">
                                        <strong class="wt-comment__name">{{.Name}}</strong>
                                        {{if .Staff}}<span class="wt-comment__badge">{{tl localizer "Team"}}</span>{{end}}
                                        <time class="wt-comment__date wt-meta" datetime="{{.CreatedAt.Format "2006-01-02T15:04:05Z07:00"}}">{{date .CreatedAt}}</time>
                                    </header>
                                    <div class="wt-comment__body">{{.HTML}}</div>
                                </li>
                            {{end}}
                        </ol>
                    {{end}}
                </li>
            {{end}}
        </ol>
    {{else}}
        <p class="wt-comments__empty wt-meta">{{tl localizer "No comments yet."}}</p>
    {{end}}

    {{commentForm .Key .Subject}}
</section>
`

// formComponent is the default comments/form.gohtml.
const formComponent = `{{/* @dot github.com/donseba/go-webthings/modules/comments.FormView */}}
{{/* The comment form of a subject with the result of the last post: {{commentForm "remarks" .ID}}. */}}
<div class="wt-comment-form" id="{{.Key}}">
    {{if .Closed}}
        <p class="wt-meta">{{tl localizer "Comments are closed."}}</p>
    {{else}}
        {{with .Result}}
            <p class="wt-notice" role="status">
                {{if eq . "sent"}}{{tl localizer "Thank you for your comment."}}
                {{else if eq . "pending"}}{{tl localizer "Thank you. Your comment is shown after approval."}}
                {{else if eq . "throttled"}}{{tl localizer "You posted several comments already. Please try again later."}}
                {{else if eq . "closed"}}{{tl localizer "Comments were closed when you posted. Please try again."}}
                {{else if eq . "invalid"}}{{tl localizer "Please fill in your name and a comment."}}{{end}}
            </p>
        {{end}}

        {{/* The form returns the visitor to the element with the id {{.Key}}. */}}
        {{form_open .Form}}
            {{form_field .Form "subject" nil}}

            <h3 class="wt-form__title">{{tl localizer "Leave a comment"}}</h3>

            {{form_field .Form "name" nil}}
            {{form_field .Form "email" nil}}
            {{form_field .Form "body" nil}}

            {{if .Moderated}}<p class="wt-form__help">{{tl localizer "Comments are shown after approval."}}</p>{{end}}

            <button class="wt-button" type="submit">{{tl localizer "Post comment"}}</button>
        {{form_close}}
    {{end}}
</div>
`
