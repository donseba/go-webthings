package admin

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/mail"
)

// ReviewSection is a section whose revisions can be reviewed, for the
// site setting that turns the review workflow on per section.
type ReviewSection struct {
	// ID is the section's permission ID, which the setting stores.
	ID string
	// Title is the section's translated title.
	Title string
}

// ReviewSections lists the sections whose revisions can be reviewed (see
// Section.Review), in the order they were added: Pages first, then the
// modules'.
func (a *Admin) ReviewSections(r *http.Request) []ReviewSection {
	var sections []ReviewSection

	for _, section := range a.sections {
		if section.Review == nil || section.PermissionID() != section.ID {
			continue
		}

		sections = append(sections, ReviewSection{ID: section.ID, Title: a.T(r, section.Title)})
	}

	return sections
}

// NotifyReviewers tells the users who may review in section, other than
// the signed-in user, that a revision of title waits for their review at
// adminPath, such as the editor of a revision. It sends a plain
// e-mail in the admin's language, like the password reset link, and only
// when the admin can send mail (see Options.Mail). It returns at once;
// the mail goes out in the background and failures are logged.
func (a *Admin) NotifyReviewers(r *http.Request, section, title, adminPath string) {
	if a.options.Mail == nil {
		return
	}

	submitter, _ := auth.CurrentUser(r.Context())
	link := a.absoluteURL(r, adminPath)
	subject := a.T(r, "Waiting for your review: %s", title)
	text := strings.Join([]string{
		a.T(r, "%s asks you to review a change of %s at %s.", submitter.DisplayName(), title, a.options.Title),
		a.T(r, "Open it to approve or reject it:") + "\n" + link,
	}, "\n\n") + "\n"

	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), time.Minute)

	go func() {
		defer cancel()

		reviewers, err := a.reviewers(ctx, section)
		if err != nil {
			slog.ErrorContext(ctx, "find reviewers", "section", section, "error", err)

			return
		}

		for _, reviewer := range reviewers {
			if reviewer.ID == submitter.ID || reviewer.Email == "" {
				continue
			}

			message := mail.Message{To: []string{reviewer.Email}, Subject: subject, Text: text}

			if err := a.options.Mail.Send(ctx, message); err != nil {
				slog.ErrorContext(ctx, "send review request", "section", section, "error", err)
			}
		}
	}()
}

// reviewers lists the active users who may review in section.
func (a *Admin) reviewers(ctx context.Context, section string) ([]auth.User, error) {
	users, err := a.auth.Users(ctx)
	if err != nil {
		return nil, err
	}

	permission := Permission{Section: section, Action: Review}

	var reviewers []auth.User

	for _, user := range users {
		if user.Status != auth.Active {
			continue
		}

		allowed, err := a.allows(ctx, user, permission)
		if err != nil {
			return nil, err
		}

		if allowed {
			reviewers = append(reviewers, user)
		}
	}

	return reviewers, nil
}

// allows is Can for a user other than the signed-in one.
func (a *Admin) allows(ctx context.Context, user auth.User, permission Permission) (bool, error) {
	if a.options.Authorize != nil {
		return a.options.Authorize(ctx, user, permission), nil
	}

	if user.IsAdmin() {
		return true, nil
	}

	permissions, err := a.auth.Permissions(ctx, user)
	if err != nil {
		return false, err
	}

	return permissions.Allows(permission.Section, permission.Action), nil
}
