package siteadmin

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/mail"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-partial/exp/flash"
)

// MailView is the guide's Mail step: how the site sends e-mail, and a test
// e-mail to the administrator.
type MailView struct {
	// Sender says how the site sends, as its mail.Describer describes it;
	// empty for a sender that does not describe itself. Logged is set when
	// mail is written to the server log instead of sent (mail.Log).
	Sender string
	Logged bool
	// NoBaseURL is set when the site has no public address: links in
	// e-mail then use the address each request names, and outside local
	// development the admin offers no password reset.
	NoBaseURL bool
	// To is the administrator's address, which the test e-mail goes to.
	To   string
	Test form.RenderModel
}

const (
	// testMailTimeout bounds sending a test e-mail: the administrator
	// waits for it.
	testMailTimeout = 30 * time.Second
	// testMailInterval is how long an administrator waits between test
	// e-mails, so the button cannot flood an inbox or a mail server.
	testMailInterval = 30 * time.Second
)

// mailView is the Mail step for the signed-in administrator.
func (g *guide) mailView(r *http.Request) *MailView {
	user, _ := auth.CurrentUser(r.Context())
	sender := g.options.Mail
	view := &MailView{
		NoBaseURL: g.options.BaseURL == "",
		To:        user.Email,
		Test: g.admin.Form(
			r,
			struct{}{},
			g.admin.URL("admin.site.guide-mail"),
			g.admin.T(r, "Send a test e-mail to %s", user.Email),
		),
	}

	switch sender := sender.(type) {
	case nil, mail.Log, *mail.Log:
		view.Logged = true
	case mail.Describer:
		view.Sender = sender.Describe()
	}

	return view
}

// testMail sends a test e-mail to the signed-in administrator and waits
// for it, at most testMailTimeout, so the toast says whether it went: the
// step is then finished. Each administrator may send one every
// testMailInterval.
func (g *guide) testMail(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.CurrentUser(r.Context())
	r = withStep(r, stepMail)

	if !g.mayTest(user.ID, time.Now()) {
		r = admin.WithError(r, g.admin.T(r, "Wait half a minute before you send another test e-mail."))
		g.render(w, r, stepMail, guideParts{status: http.StatusTooManyRequests})

		return
	}

	sender := g.options.Mail
	if sender == nil {
		sender = mail.Log{}
	}

	ctx, cancel := context.WithTimeout(r.Context(), testMailTimeout)
	defer cancel()

	title := g.admin.T(r, "your site")
	if current, err := g.settings.Settings(r.Context()); err == nil {
		if text := current.Text(current.DefaultLocale).Title; text != "" {
			title = text
		}
	}

	err := sender.Send(ctx, mail.Message{
		To:      []string{user.Email},
		Subject: g.admin.T(r, "Test e-mail from %s", title),
		Text: g.admin.T(
			r,
			"This is a test e-mail from the admin of %s. It arrived, so the site can send e-mail.",
			title,
		) + "\n",
	})
	if err != nil {
		slog.WarnContext(r.Context(), "send test e-mail", "error", err)

		r = admin.WithError(r, g.admin.T(r, "The test e-mail could not be sent: %s", err.Error()))
		g.render(w, r, stepMail, guideParts{status: http.StatusOK})

		return
	}

	if err := g.finishStep(r.Context(), stepMail); err != nil {
		g.admin.Fail(w, r, err)

		return
	}

	g.admin.RedirectFlash(w, r, g.admin.URL("admin.site.guide-step", "step", stepMail),
		flash.Success(g.admin.T(r, "Test e-mail sent to %s.", user.Email)))
}

// mayTest reports whether the user may send a test e-mail at now, and
// counts it when they may.
func (g *guide) mayTest(userID string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if last, ok := g.tests[userID]; ok && now.Sub(last) < testMailInterval {
		return false
	}

	// Old entries go, so the map stays as small as the number of
	// administrators who test now.
	for id, last := range g.tests {
		if now.Sub(last) >= testMailInterval {
			delete(g.tests, id)
		}
	}

	g.tests[userID] = now

	return true
}
