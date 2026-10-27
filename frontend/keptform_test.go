package frontend

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/db/dbtest"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/site"

	form "github.com/donseba/go-form/v2"
)

// accountInput is a visitor form with a password, a file and a field named
// like the spam trap.
type accountInput struct {
	Group    string `form:"input,hidden"   name:"group"`
	Email    string `form:"input,email"    name:"email"    required:"true"`
	Password string `form:"input,password" name:"password" required:"true"`
	Website  string `form:"input,url"      name:"website"`
	Avatar   string `form:"input,file"     name:"avatar"`
}

// keptFormsSite is a frontend with a site, for the site's key, and the
// table of kept posts in database.
func keptFormsSite(t *testing.T, database *db.DB) *Frontend {
	t.Helper()

	ctx := context.Background()

	if err := database.ApplySchema(ctx, "media", media.Schema()); err != nil {
		t.Fatal(err)
	}

	if err := database.ApplySchema(ctx, "site", site.Schema()); err != nil {
		t.Fatal(err)
	}

	if err := database.ApplySchema(ctx, "frontend", Schema()); err != nil {
		t.Fatal(err)
	}

	f := New(site.New(database), nil, nil, Missing())
	f.SetDatabase(database)

	return f
}

// keepFrom posts values to /_/club/join from the page /en/club, as the
// visitor at address, and keeps the post of model with errs; it returns
// the cookie.
func keepFrom(t *testing.T, f *Frontend, address string, model any, values url.Values, errs form.FieldErrors) *http.Cookie {
	t.Helper()

	values.Set("back", "/en/club#club")

	request := httptest.NewRequest(http.MethodPost, "/_/club/join", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.RemoteAddr = address + ":1234"

	if err := request.ParseForm(); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	f.KeepForm(response, request, "club", model, errs)

	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies: %v", cookies)
	}

	cookie := cookies[0]
	if cookie.Name != "wt_form_club" || cookie.Path != "/en/club" || cookie.MaxAge != 300 || !cookie.HttpOnly ||
		cookie.SameSite != http.SameSiteLaxMode || !keptFormToken.MatchString(cookie.Value) {
		t.Fatalf("cookie: %+v", cookie)
	}

	return cookie
}

// keep is keepFrom for accountInput from 192.0.2.1.
func keep(t *testing.T, f *Frontend, values url.Values, errs form.FieldErrors) *http.Cookie {
	t.Helper()

	return keepFrom(t, f, "192.0.2.1", accountInput{}, values, errs)
}

// visit asks for path with cookie and returns the response and the form
// VisitorForm makes for the club's form there.
func visit(f *Frontend, path string, cookie *http.Cookie) (*httptest.ResponseRecorder, form.RenderModel) {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.AddCookie(cookie)

	response := httptest.NewRecorder()
	request = f.restoreForms(response, request)

	return response, VisitorForm(request.Context(), accountInput{Group: "a"}, "/_/club/join", "club")
}

// untouched fails unless response says nothing about kept posts: no
// cookie and no Cache-Control.
func untouched(t *testing.T, what string, response *httptest.ResponseRecorder) {
	t.Helper()

	if cookies := response.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("%s: cookies %v", what, cookies)
	}

	if got := response.Header().Get("Cache-Control"); got != "" {
		t.Fatalf("%s: Cache-Control %q", what, got)
	}
}

// keptRows counts the kept posts in database.
func keptRows(t *testing.T, database *db.DB) int {
	t.Helper()

	var count int
	if err := database.Get(context.Background(), &count, `SELECT COUNT(*) FROM wt_kept_form`); err != nil {
		t.Fatal(err)
	}

	return count
}

func TestKeepForm(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		f := keptFormsSite(t, database)

		errs := form.FieldErrors{form.FieldValidationError{Field: "password", Err: "Fill in a password"}}
		cookie := keep(
			t,
			f,
			url.Values{
				"group":    {"a"},
				"email":    {"jane@example.com"},
				"password": {"hunter22"},
				"website":  {"spam"},
				"avatar":   {"me.png"},
			},
			errs,
		)

		// The cookie holds a token alone; the database its digest, the
		// post without the password, the spam trap, the file or the way
		// back, and no address of the visitor.
		var row struct {
			ID      string `db:"id"`
			Client  string `db:"client"`
			Key     string `db:"form_key"`
			Page    string `db:"page"`
			Action  string `db:"action"`
			Payload string `db:"payload"`
		}

		err := database.Get(context.Background(), &row, `SELECT id, client, form_key, page, action, payload FROM wt_kept_form`)
		if err != nil {
			t.Fatal(err)
		}

		if row.ID != tokenDigest(cookie.Value) || row.Key != "club" || row.Page != "/en/club" || row.Action != "/_/club/join" {
			t.Fatalf("row: %+v", row)
		}

		stored := strings.Join([]string{row.ID, row.Client, row.Key, row.Page, row.Action, row.Payload}, " ")
		for _, secret := range []string{cookie.Value, "hunter22", "spam", "me.png", "back", "192.0.2.1"} {
			if strings.Contains(stored, secret) {
				t.Fatalf("the database holds %q: %s", secret, stored)
			}
		}

		if !strings.Contains(row.Payload, "jane@example.com") {
			t.Fatalf("payload: %s", row.Payload)
		}

		// A page below the form's page gets the cookie too, but leaves it.
		response, model := visit(f, "/en/club/rules", cookie)
		if model.Carry != nil {
			t.Fatalf("restored below the page: %+v", model.Carry)
		}

		untouched(t, "below the page", response)

		// A prefetch leaves it for the visit.
		request := httptest.NewRequest(http.MethodGet, "/en/club", nil)
		request.Header.Set("Sec-Purpose", "prefetch")
		request.AddCookie(cookie)

		if restored := f.restoreForms(httptest.NewRecorder(), request); restored != request {
			t.Fatal("a prefetch read the post")
		}

		// The page shows it once, and clears the cookie; no cache keeps it.
		response, model = visit(f, "/en/club", cookie)
		if model.Carry == nil || model.Carry.Values.Get("email") != "jane@example.com" ||
			len(model.Carry.Errors["password"]) != 1 {
			t.Fatalf("carry: %+v", model.Carry)
		}

		cleared := response.Result().Cookies()
		if len(cleared) != 1 || cleared[0].Name != "wt_form_club" || cleared[0].MaxAge >= 0 || cleared[0].Path != "/en/club" {
			t.Fatalf("cleared: %v", cleared)
		}

		if got := response.Header().Get("Cache-Control"); got != "private, no-store" {
			t.Fatalf("Cache-Control: %q", got)
		}

		if rows := keptRows(t, database); rows != 0 {
			t.Fatalf("the post stays after it showed: %d rows", rows)
		}

		// Shown once: the same cookie again brings nothing.
		response, model = visit(f, "/en/club", cookie)
		if model.Carry != nil {
			t.Fatal("shown twice")
		}

		untouched(t, "second view", response)

		// The form of another action with the same key shows nothing.
		cookie = keep(t, f, url.Values{"email": {"jane@example.com"}}, nil)

		request = httptest.NewRequest(http.MethodGet, "/en/club", nil)
		request.AddCookie(cookie)
		request = f.restoreForms(httptest.NewRecorder(), request)

		if other := VisitorForm(request.Context(), accountInput{}, "/_/club/leave", "club"); other.Carry != nil {
			t.Fatal("restored into the form of another action")
		}
	})
}

// TestKeptFormIgnored checks that cookies without a post of their own show
// nothing and leave the answer as it is.
func TestKeptFormIgnored(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		f := keptFormsSite(t, database)
		cookie := keep(t, f, url.Values{"email": {"jane@example.com"}}, nil)

		// A changed token.
		changed := *cookie
		replacement := "A"

		if changed.Value[0] == 'A' {
			replacement = "B"
		}

		changed.Value = replacement + changed.Value[1:]

		// The post of one form under the name of another.
		renamed := *cookie
		renamed.Name = "wt_form_other"

		cases := map[string]*http.Cookie{
			"changed": &changed,
			"renamed": &renamed,
			"unknown": {Name: "wt_form_club", Value: "ABCDEFGHIJKLMNOPQRSTUVWXYZ"},
			"garbage": {Name: "wt_form_club", Value: "abc"},
		}

		for name, other := range cases {
			request := httptest.NewRequest(http.MethodGet, "/en/club", nil)
			request.AddCookie(other)

			response := httptest.NewRecorder()
			request = f.restoreForms(response, request)

			for _, key := range []string{"club", "other"} {
				if model := VisitorForm(request.Context(), accountInput{}, "/_/club/join", key); model.Carry != nil {
					t.Fatalf("%s: restored", name)
				}
			}

			untouched(t, name, response)
		}

		// The real cookie still brings its post.
		if _, model := visit(f, "/en/club", cookie); model.Carry == nil {
			t.Fatal("the post went with a wrong cookie")
		}
	})
}

// TestKeptFormExpired checks that a post older than five minutes does not
// show, is removed, and leaves the answer as it is.
func TestKeptFormExpired(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		f := keptFormsSite(t, database)
		cookie := keep(t, f, url.Values{"email": {"jane@example.com"}}, nil)

		_, err := database.Exec(context.Background(), `UPDATE wt_kept_form SET expires_at=?`, time.Now().UTC().Add(-time.Second))
		if err != nil {
			t.Fatal(err)
		}

		response, model := visit(f, "/en/club", cookie)
		if model.Carry != nil {
			t.Fatal("restored an expired post")
		}

		untouched(t, "expired", response)

		if rows := keptRows(t, database); rows != 0 {
			t.Fatalf("the expired post stays: %d rows", rows)
		}
	})
}

// TestKeptFormOnce checks that of several views at once, one shows the
// post.
func TestKeptFormOnce(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		f := keptFormsSite(t, database)
		cookie := keep(t, f, url.Values{"email": {"jane@example.com"}}, nil)

		const views = 8

		var (
			wait  sync.WaitGroup
			mutex sync.Mutex
			shown int
		)

		for range views {
			wait.Go(func() {
				if _, model := visit(f, "/en/club", cookie); model.Carry != nil {
					mutex.Lock()
					shown++
					mutex.Unlock()
				}
			})
		}

		wait.Wait()

		if shown != 1 {
			t.Fatalf("shown %d times", shown)
		}
	})
}

// TestKeepFormLong checks that a long text is kept whole: here 19990
// characters of three bytes each.
func TestKeepFormLong(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		f := keptFormsSite(t, database)

		type story struct {
			Name string `form:"input,text" name:"name"`
			Text string `form:"textarea"   name:"text"`
		}

		text := strings.Repeat("字", 19990)
		errs := form.FieldErrors{form.FieldValidationError{Field: "name", Err: "Fill in your name"}}
		cookie := keepFrom(t, f, "192.0.2.1", story{}, url.Values{"text": {text}}, errs)

		_, model := visit(f, "/en/club", cookie)
		if model.Carry == nil || model.Carry.Values.Get("text") != text || len(model.Carry.Errors["name"]) != 1 {
			t.Fatal("the long text was not kept")
		}
	})
}

// TestKeptFormsPerClient checks that one visitor has at most
// keptFormsPerClient posts kept, the newest, while others keep theirs.
func TestKeptFormsPerClient(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		f := keptFormsSite(t, database)
		other := keepFrom(t, f, "192.0.2.2", accountInput{}, url.Values{"email": {"other@example.com"}}, nil)

		var cookies []*http.Cookie

		for range keptFormsPerClient + 2 {
			cookies = append(cookies, keep(t, f, url.Values{"email": {"jane@example.com"}}, nil))
		}

		if rows := keptRows(t, database); rows != keptFormsPerClient+1 {
			t.Fatalf("rows: %d", rows)
		}

		if _, model := visit(f, "/en/club", cookies[0]); model.Carry != nil {
			t.Fatal("the oldest post stayed")
		}

		if _, model := visit(f, "/en/club", cookies[len(cookies)-1]); model.Carry == nil {
			t.Fatal("the newest post went")
		}

		if _, model := visit(f, "/en/club", other); model.Carry == nil {
			t.Fatal("another visitor's post went")
		}
	})
}

// TestCleanupForms checks that posts past their five minutes are removed
// when another post is kept, and by CleanupForms, which leaves the others.
func TestCleanupForms(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		f := keptFormsSite(t, database)

		expire := func(cookie *http.Cookie) {
			t.Helper()

			_, err := database.Exec(ctx, `UPDATE wt_kept_form SET expires_at=? WHERE id=?`,
				time.Now().UTC().Add(-time.Second), tokenDigest(cookie.Value))
			if err != nil {
				t.Fatal(err)
			}
		}

		expire(keep(t, f, url.Values{"email": {"old@example.com"}}, nil))
		expire(keepFrom(t, f, "192.0.2.2", accountInput{}, url.Values{"email": {"old@example.com"}}, nil))

		left := keepFrom(t, f, "192.0.2.3", accountInput{}, url.Values{"email": {"left@example.com"}}, nil)
		if rows := keptRows(t, database); rows != 1 {
			t.Fatalf("keeping a post left %d rows", rows)
		}

		keepFrom(t, f, "192.0.2.4", accountInput{}, url.Values{"email": {"new@example.com"}}, nil)
		expire(left)

		if err := f.CleanupForms(ctx); err != nil {
			t.Fatal(err)
		}

		if rows := keptRows(t, database); rows != 1 {
			t.Fatalf("cleanup left %d rows", rows)
		}
	})
}
