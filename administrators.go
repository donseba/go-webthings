package webthings

import (
	"context"
	"crypto/rand"
	"errors"
	"net/url"
	"strings"

	"github.com/donseba/go-webthings/auth"
)

// ErrNotStarted is returned by what needs a started app (see Start), such as
// InviteAdministrator.
var ErrNotStarted = errors.New("the app has not started")

// InviteAdministrator creates an administrator account for email and returns
// a link with which its owner chooses a password: the admin's password
// reset page with a new reset token, which works for an hour
// (auth.ResetLifetime). The account gets no password anyone knows, so it
// cannot be signed in to before then. The link is absolute on
// Config.BaseURL, or the path below the site's address without one, such as
// /admin/reset-password?token=….
//
// It is for whoever runs the site, such as a command line that adds the
// first administrator without the setup link (see SetupToken), and works
// once the app has started, also on a site with accounts. It returns
// auth.ErrEmail for an address that is no address, auth.ErrDuplicate for one
// that has an account, and ErrNotStarted before Start. Give the link only to
// the owner of the address: it opens the account.
func (a *App) InviteAdministrator(ctx context.Context, email string) (string, error) {
	if !a.started.Load() {
		return "", ErrNotStarted
	}

	// A password nobody knows, replaced through the link.
	user, err := a.Auth.CreateUser(ctx, email, "", rand.Text()+rand.Text(), auth.Admin)
	if err != nil {
		return "", err
	}

	token, _, err := a.Auth.RequestPasswordReset(ctx, user.Email)
	if err != nil {
		return "", err
	}

	path := a.backoffice.URL("admin.reset-password", url.Values{"token": {token}})

	return strings.TrimSuffix(a.config.BaseURL, "/") + path, nil
}
