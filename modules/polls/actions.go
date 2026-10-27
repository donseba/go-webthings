package polls

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/frontend"

	form "github.com/donseba/go-form/v2"
)

// Results of a vote, added to the page address as ?<key>=<result>.
const (
	// ResultVoted is a vote that was counted.
	ResultVoted = "voted"
	// ResultAlready is a visitor who voted on this poll before.
	ResultAlready = "already"
	// ResultClosed is a poll that no longer takes votes, or is gone.
	ResultClosed = "closed"
	// ResultInvalid is a vote for an option the poll does not have.
	ResultInvalid = "invalid"
	// ResultThrottled is sent to a visitor who posted too many votes in a
	// short time; nothing was counted.
	ResultThrottled = frontend.ResultThrottled
)

// VoteInput is the vote a visitor posts to /_/<key>/vote: the poll's ID
// and the chosen option's ID. Poll.Form is its model for a poll, with the
// poll's options (RenderModel.Options) and its question as the label of the
// options: form_open, {{form_field .Form "poll" nil}} and {{form_field .Form
// "option" nil}} render the form, the options as radio buttons. A poll that
// does not exist answers "closed"; a vote without an option, or with one
// the poll does not have, answers "invalid", and the poll shows the vote
// again with the message at its options (frontend.KeepForm).
type VoteInput struct {
	Poll   string `form:"input,hidden" name:"poll"   maxLength:"64" line:"true"`
	Option string `form:"radios"       name:"option"                            required:"true"`
}

// pollSuffix makes the name of the query value that tells which poll a
// result belongs to: ?<key>=voted&<key>_poll=<poll ID>. Instance keys cannot
// contain "_", so the name never is another instance's key.
const pollSuffix = "_poll"

// results are the results a page shows; see frontend.Result.
var voteResults = []string{ResultVoted, ResultAlready, ResultClosed, ResultInvalid, ResultThrottled}

// back returns the visitor to the page with the result of their vote on the
// poll they posted. Only a vote that counted changed what pages show
// (frontend.NothingChanged).
func (s *Service) back(w http.ResponseWriter, r *http.Request, result string) {
	if result != ResultVoted {
		frontend.NothingChanged(r)
	}

	var extra url.Values

	if poll := r.PostFormValue("poll"); idPattern.MatchString(poll) {
		extra = url.Values{s.key + pollSuffix: {poll}}
	}

	frontend.BackWith(w, r, s.key, result, extra)
}

const (
	// cookieName holds the visitor id; it is shared by all instances.
	cookieName   = "wt_poll"
	cookieMaxAge = 365 * 24 * 60 * 60
	bodyLimit    = 16 << 10
)

var visitorPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// registerActions registers the action that receives the visitors' votes,
// at /_/<key>/vote, on the instance's action router
// (webthings.Env.Actions).
func (s *Service) registerActions() {
	routes := s.actions

	routes.Use(frontend.Limits(s.key, frontend.ActionLimits{
		Body:   bodyLimit,
		Posts:  20,
		Window: time.Minute,
		// The way back names the poll the visitor voted on.
		Throttled: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.back(w, r, ResultThrottled)
		}),
	}))
	routes.Post("/vote", s.vote).As("vote")
}

func (s *Service) vote(w http.ResponseWriter, r *http.Request) {
	// Bots that fill in the trap are told their vote counted.
	if frontend.IsSpam(r) {
		s.back(w, r, ResultVoted)

		return
	}

	visitor := visitorCookie(r)
	known := visitor != ""

	if !known {
		address, err := s.addressID(r)
		if err != nil {
			s.frontend.Fail(w, r, fmt.Errorf("poll vote: %w", err))

			return
		}

		visitor = address
	}

	var in VoteInput

	// The poll's options make an option it does not have a field error, as
	// the form showed them.
	model := form.RenderModel{Model: &in, Options: s.voteOptions(r)}

	// The result names the poll from the post itself (back).
	if errs, err := s.frontend.BindForm(r, model); err != nil || len(errs) > 0 {
		if err == nil && idPattern.MatchString(in.Poll) {
			s.frontend.KeepForm(w, r, s.anchor(in.Poll), model, errs)
		}

		s.back(w, r, ResultInvalid)

		return
	}

	err := s.Vote(r.Context(), in.Poll, in.Option, visitor)

	switch {
	case err == nil:
		// The cookie keeps this visitor's identity when their address changes.
		if !known {
			remember(w, r, visitor)
		}

		s.back(w, r, ResultVoted)
	case errors.Is(err, ErrAlreadyVoted):
		// Someone else on the same address may have voted. With a cookie of
		// their own, people who accept cookies can vote next time.
		if !known {
			remember(w, r, randomID())
		}

		s.back(w, r, ResultAlready)
	case errors.Is(err, ErrClosed), errors.Is(err, content.ErrNotFound):
		s.back(w, r, ResultClosed)
	case errors.Is(err, ErrInvalidOption):
		s.back(w, r, ResultInvalid)
	default:
		s.frontend.Fail(w, r, fmt.Errorf("poll vote: %w", err))
	}
}

// voteOptions are the options of the poll a visitor votes on, by the poll's
// ID in the post, to bind the vote with; none for a poll that does not
// exist, which Vote answers.
func (s *Service) voteOptions(r *http.Request) form.FieldOptions {
	id := r.PostFormValue("poll")
	if !idPattern.MatchString(id) {
		return nil
	}

	item, err := s.items.Get(r.Context(), s.key, id)
	if err != nil {
		return nil
	}

	return form.FieldOptions{"option": optionValues(item, s.frontend.BackLocale(r))}
}

// visitorCookie is the visitor id from the cookie, or "".
func visitorCookie(r *http.Request) string {
	cookie, err := r.Cookie(cookieName)
	if err != nil || !visitorPattern.MatchString(cookie.Value) {
		return ""
	}

	return cookie.Value
}

// addressID is the visitor id of a visitor without the cookie, derived from
// their IP address with the instance's secret key (webthings.Env.VisitorID),
// so the stored id cannot be traced back to an address by trying them all.
// It is as long as the cookie's random id.
func (s *Service) addressID(r *http.Request) (string, error) {
	if s.visitorID == nil {
		return "", errors.New("polls: no secret for visitors without a cookie")
	}

	id, err := s.visitorID(r.Context(), r)
	if err != nil {
		return "", err
	}

	return id[:32], nil
}

func randomID() string {
	var id [16]byte

	_, _ = rand.Read(id[:])

	return hex.EncodeToString(id[:])
}

func remember(w http.ResponseWriter, r *http.Request, visitor string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    visitor,
		Path:     "/",
		MaxAge:   cookieMaxAge,
		HttpOnly: true,
		Secure:   frontend.Secure(r),
		SameSite: http.SameSiteLaxMode,
	})
}
