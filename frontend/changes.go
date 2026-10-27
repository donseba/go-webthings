package frontend

import (
	"context"
	"net/http"
	"sync/atomic"
)

// A visitor's post to a module's actions (/_/<key>/...) or to a page, such
// as a comment or a vote, may change what the site's pages show, so the
// app's output cache renders them again after a post that succeeded. Many
// posts change nothing: those that fill in the spam trap, have mistakes,
// come too often or are for a closed form. NothingChanged tells the app so,
// and the cache keeps its pages. IsSpam when it reports true, KeepForm and
// Throttle over its limit tell it themselves; a module tells it for its
// other answers that store nothing, such as a vote on a closed poll.
//
// A post changed something unless it says otherwise: a change a post wrongly
// denies shows only once the cache's time has passed, while a post that
// changed nothing and does not say so only costs the pages a render.

// changesKey holds the *postChanges of a visitor's post.
type changesKey struct{}

// postChanges records whether a visitor's post changed nothing.
type postChanges struct {
	nothing atomic.Bool
}

// TrackChanges returns r with a record of whether the post changes
// something, which NothingChanged clears and Changed reads. The app tracks
// the visitors' posts to the module actions and the pages.
func TrackChanges(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), changesKey{}, &postChanges{}))
}

// NothingChanged tells the app that the visitor's post r changed nothing the
// site's pages show, such as a vote on a closed poll: the output cache keeps
// its pages (see TrackChanges). Call it only for an answer that stored
// nothing a page shows.
func NothingChanged(r *http.Request) {
	if changes, ok := r.Context().Value(changesKey{}).(*postChanges); ok {
		changes.nothing.Store(true)
	}
}

// Changed reports whether the post r may have changed what the site's pages
// show: true unless NothingChanged was called for it.
func Changed(r *http.Request) bool {
	changes, ok := r.Context().Value(changesKey{}).(*postChanges)

	return !ok || !changes.nothing.Load()
}
