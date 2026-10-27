package frontend

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/donseba/go-webthings/db"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-router"
)

// A visitor's post with mistakes returns to the form's page (Back), where
// the form shows the post again: the visitor's values, and go-form's
// messages at the fields that need a correction. The action keeps the post
// with KeepForm before it redirects:
//
//	errs, err := env.Frontend.BindForm(r, &in)
//	if err != nil || len(errs) > 0 {
//		env.Frontend.KeepForm(w, r, key, in, errs)
//		frontend.Back(w, r, key, "invalid")
//
//		return
//	}
//
// and the form's model, made with VisitorForm for the same key and action,
// shows it on the page the visitor returns to, so {{form_open .Form}},
// form_field and form_render show it with no change to the templates. This
// works without JavaScript and with HTMX alike: HTMX follows the redirect
// and swaps in the form as the page renders it.
//
// The post waits in the database (wt_kept_form) for five minutes at most.
// The visitor gets a cookie for the form's page, wt_form_<key>, that holds
// only a random token; the database keeps the token's SHA-256, so its rows
// do not give away the tokens of posts that wait. The first page view of
// that page with the cookie takes the post, which removes it, and clears
// the cookie, so the post shows once. A token that is unknown, belongs to
// another form or page, or is older than five minutes shows nothing. The
// post never holds passwords, files or the spam trap (go-form's Carry), and
// up to keptFormLimit bytes of it are kept: far more than the longest text
// a visitor's form takes.

//go:embed schema/*.sql
var schemaFiles embed.FS

// Schema holds this package's schema files per dialect (see
// db.ApplySchema): the table of kept posts.
func Schema() fs.FS {
	files, _ := fs.Sub(schemaFiles, "schema")

	return files
}

// KeptFormCookie goes before the form's key in the name of the cookie
// that brings a visitor's kept post to its page (see KeepForm): a request
// with such a cookie may get a page of its own, which caches must not keep
// for others.
const KeptFormCookie = "wt_form_"

const (
	// keptFormMaxAge is how long a kept post waits for its page.
	keptFormMaxAge = 5 * time.Minute
	// keptFormSecret names the site's key that hides the visitor's address
	// in a kept post (see formClient).
	keptFormSecret = "visitor_forms"
	// keptFormLimit is the most bytes of go-form's Carry a kept post holds.
	// A text of 20000 characters, the longest a bundled form takes, needs
	// at most 80000 bytes, also in a script of four bytes per character;
	// a post over the limit leaves out its longest values (Carry.Encode).
	keptFormLimit = 128 << 10
	// keptFormPathLimit is the longest page or action path kept.
	keptFormPathLimit = 2048
	// keptFormsPerClient is the most posts kept for one visitor (by
	// ClientIP) at a time; a newer post removes the oldest.
	keptFormsPerClient = 10
)

var (
	// keptFormKey is the key of a form whose post can be kept: a module
	// instance key or an element id, usable in a cookie's name.
	keptFormKey = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	// keptFormToken is a token as rand.Text makes it.
	keptFormToken = regexp.MustCompile(`^[A-Z2-7]{26}$`)
)

// keptForm is a kept post as the page that takes it reads it.
type keptForm struct {
	// Action is the path the form posted to.
	Action string `db:"action"`
	// Payload is go-form's encoding of the values and the errors.
	Payload string `db:"payload"`
	// ExpiresAt is when the post stops waiting for its page.
	ExpiresAt time.Time `db:"expires_at"`
}

// keptFormsKey holds the posts kept for the page being served.
type keptFormsKey struct{}

// SetDatabase gives the frontend the site's database, which keeps
// visitors' posts with mistakes (KeepForm) for their pages; Schema has its
// table. App calls it at Start. Without it no post is kept.
func (f *Frontend) SetDatabase(database *db.DB) {
	f.database = database
}

// KeepForm keeps a visitor's failed post of the form key for the page the
// form was on (see BackPath), for VisitorForm to show it again there: the
// values r posted of model's fields (r.PostForm), without passwords, files
// and the spam trap (Honeypot), and errs at their fields, such as those
// BindForm returned. model is the input struct, a go-form Schema, or the
// form VisitorForm made; key and the address r posted to must be those
// VisitorForm made the form with. Call it before the redirect, Back or
// BackWith. Nothing is kept when the form was on no page of this site;
// problems are logged, and the visitor then returns with the result alone.
// A failed post changed nothing: KeepForm tells the app so
// (NothingChanged).
//
// One visitor (by ClientIP) has at most ten posts kept at a time, and none
// for more than five minutes; the actions' throttle (Limits) bounds how
// fast they come.
func (f *Frontend) KeepForm(w http.ResponseWriter, r *http.Request, key string, model any, errs form.FieldErrors) {
	NothingChanged(r)

	ctx := r.Context()

	page := backPage(r)
	if page == "" || len(page) > keptFormPathLimit || len(r.URL.Path) > keptFormPathLimit {
		return
	}

	if !keptFormKey.MatchString(key) || f.database == nil || f.site == nil {
		return
	}

	carry, err := form.NewCarry(keptModel(model), r.PostForm, errs)
	if err != nil {
		slog.ErrorContext(ctx, "keep visitor form", "form", key, "error", err)

		return
	}

	payload, err := carry.Encode(keptFormLimit)
	if err != nil {
		slog.WarnContext(ctx, "keep visitor form", "form", key, "error", err)

		return
	}

	client, err := f.formClient(ctx, r)
	if err != nil {
		slog.ErrorContext(ctx, "keep visitor form", "form", key, "error", err)

		return
	}

	token := rand.Text()

	err = f.storeForm(ctx, token, client, key, page, r.URL.Path, string(payload))
	if err != nil {
		slog.ErrorContext(ctx, "keep visitor form", "form", key, "error", err)

		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     KeptFormCookie + key,
		Value:    token,
		Path:     page,
		MaxAge:   int(keptFormMaxAge / time.Second),
		HttpOnly: true,
		Secure:   Secure(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// keptModel is model as a form with the visitor forms' spam trap
// (Honeypot), so that go-form's Carry leaves the trap out also when the
// model is a plain input struct.
func keptModel(model any) any {
	switch wrapped := model.(type) {
	case form.RenderModel:
		wrapped.Info.Honeypot = Honeypot

		return wrapped
	case *form.RenderModel:
		if wrapped == nil {
			return model
		}

		copied := *wrapped
		copied.Info.Honeypot = Honeypot

		return copied
	}

	return form.WithInfo(model, form.Info{Honeypot: Honeypot})
}

// formClient identifies the visitor of r in a kept post without storing
// their address: an HMAC-SHA256 of ClientIP with a key of the site.
func (f *Frontend) formClient(ctx context.Context, r *http.Request) (string, error) {
	key, err := f.site.Secret(ctx, keptFormSecret)
	if err != nil {
		return "", err
	}

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(ClientIP(r)))

	return hex.EncodeToString(mac.Sum(nil)), nil
}

// tokenDigest is what the database keeps of a kept post's token.
func tokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])
}

// storeForm keeps a post under token's digest. It first removes the posts
// that waited too long, and the oldest of client's posts beyond
// keptFormsPerClient.
func (f *Frontend) storeForm(ctx context.Context, token, client, key, page, action, payload string) error {
	now := time.Now().UTC()

	if err := f.CleanupForms(ctx); err != nil {
		return err
	}

	var older []string

	err := f.database.Select(ctx, &older,
		`SELECT id FROM wt_kept_form WHERE client=? ORDER BY created_at DESC, id DESC`,
		client,
	)
	if err != nil {
		return err
	}

	if len(older) >= keptFormsPerClient {
		for _, id := range older[keptFormsPerClient-1:] {
			if _, err := f.database.Exec(ctx, `DELETE FROM wt_kept_form WHERE id=?`, id); err != nil {
				return err
			}
		}
	}

	_, err = f.database.Exec(ctx,
		`INSERT INTO wt_kept_form (id, client, form_key, page, action, payload, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		tokenDigest(token), client, key, page, action, payload, now, now.Add(keptFormMaxAge),
	)

	return err
}

// CleanupForms removes the kept posts (KeepForm) whose five minutes have
// passed without a view of their page. Keeping a post does this too; App
// also runs it with its hourly cleanup, so posts nobody came back for do
// not stay when no new ones come.
func (f *Frontend) CleanupForms(ctx context.Context) error {
	if f.database == nil {
		return nil
	}

	_, err := f.database.Exec(ctx, `DELETE FROM wt_kept_form WHERE expires_at<=?`, time.Now().UTC())

	return err
}

// errKeptForm is a token without a post for the form and page asked for.
var errKeptForm = errors.New("no kept form")

// takeForm takes the post kept under token for the form key on page: it
// removes it, so that it shows once. Of several views at once, only the one
// whose delete removes the row gets the post. A post past its five minutes
// is removed too, but not returned. Any other token, also one kept for
// another page or form, returns errKeptForm and leaves its post.
func (f *Frontend) takeForm(ctx context.Context, token, key, page string) (keptForm, error) {
	id := tokenDigest(token)

	var kept keptForm

	err := f.database.Get(ctx, &kept,
		`SELECT action, payload, expires_at FROM wt_kept_form WHERE id=? AND form_key=? AND page=?`,
		id, key, page,
	)
	if errors.Is(err, db.ErrNotFound) {
		return keptForm{}, errKeptForm
	}

	if err != nil {
		return keptForm{}, err
	}

	err = db.Affected(f.database.Exec(ctx, `DELETE FROM wt_kept_form WHERE id=?`, id))
	if errors.Is(err, db.ErrNotFound) {
		return keptForm{}, errKeptForm
	}

	if err != nil {
		return keptForm{}, err
	}

	if !time.Now().Before(kept.ExpiresAt) {
		return keptForm{}, errKeptForm
	}

	return kept, nil
}

// backPage is the escaped path of the page a visitor's form was on (see
// BackPath), as the browser asks for it after Back, or "".
func backPage(r *http.Request) string {
	page, ok := router.LocalURL(backTarget(r))
	if !ok {
		return ""
	}

	return page.EscapedPath()
}

// restoreForms takes the posts kept for the page r asks for (KeepForm)
// into its context, for VisitorForm, and clears their cookies, so each
// shows once. A page with a kept post is the visitor's own: no cache may
// keep it. A cookie that brings no post, such as an old one or one of a
// page above, changes nothing about the answer. Only a GET by the visitor
// takes posts; a prefetch leaves them for the visit itself.
func (f *Frontend) restoreForms(w http.ResponseWriter, r *http.Request) *http.Request {
	if r.Method != http.MethodGet || prefetched(r) || f.database == nil {
		return r
	}

	ctx := r.Context()
	page := r.URL.EscapedPath()

	var kept map[string]keptForm

	for _, cookie := range r.Cookies() {
		key, ok := strings.CutPrefix(cookie.Name, KeptFormCookie)
		if !ok || !keptFormKey.MatchString(key) || !keptFormToken.MatchString(cookie.Value) {
			continue
		}

		// The cookie of a page also goes to the pages below it, which
		// leave the post for the page.
		state, err := f.takeForm(ctx, cookie.Value, key, page)
		if errors.Is(err, errKeptForm) {
			continue
		}

		if err != nil {
			slog.ErrorContext(ctx, "kept visitor form", "form", key, "error", err)

			continue
		}

		http.SetCookie(
			w,
			&http.Cookie{
				Name:     cookie.Name,
				Path:     page,
				MaxAge:   -1,
				HttpOnly: true,
				Secure:   Secure(r),
				SameSite: http.SameSiteLaxMode,
			},
		)
		w.Header().Set("Cache-Control", "private, no-store")

		if kept == nil {
			kept = map[string]keptForm{}
		}

		kept[key] = state
	}

	if kept == nil {
		return r
	}

	return r.WithContext(context.WithValue(ctx, keptFormsKey{}, kept))
}

// prefetched reports whether the browser asks for the page ahead of a
// visit, such as for a link the visitor may follow.
func prefetched(r *http.Request) bool {
	purpose := r.Header.Get("Sec-Purpose") + r.Header.Get("Purpose")

	return strings.Contains(purpose, "prefetch")
}

// keptCarry is the post kept for the form key that posts to action, for
// the page being served, if there is one.
func keptCarry(ctx context.Context, key, action string) (*form.Carry, bool) {
	kept, _ := ctx.Value(keptFormsKey{}).(map[string]keptForm)

	state, ok := kept[key]
	if !ok {
		return nil, false
	}

	target, err := url.Parse(action)
	if err != nil || target.Path != state.Action {
		return nil, false
	}

	carry, err := form.DecodeCarry([]byte(state.Payload))
	if err != nil {
		return nil, false
	}

	return &carry, true
}
