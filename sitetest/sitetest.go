// Package sitetest starts a complete WebThings app for end-to-end tests and
// drives its admin like a browser: sign in, submit forms, publish.
//
// Its own templates are only the generic ones (layout, page, 404 and search).
// A module's tests bring the module's templates in their testdata folder:
//
//	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"}, publishing.Module("news", "News"))
package sitetest

import (
	"bytes"
	"context"
	"html"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/internal/testhooks"
	"github.com/donseba/go-webthings/locales"
	"github.com/donseba/go-webthings/mail"
	"github.com/donseba/go-webthings/media"

	partial "github.com/donseba/go-partial"
	"github.com/donseba/go-partial/connector"
)

// Sites in tests hash passwords with the cheapest cost: the production cost
// takes about 70ms per hash, and every site hashes and checks its admin's
// password. It is set when the package loads, before any password is hashed,
// because hashes made with one cost do not verify with the other.
func init() {
	testhooks.UseCheapPasswords()
}

// Site is a running app with a signed-in admin browser.
type Site struct {
	t   *testing.T
	App *webthings.App
	// Mail holds the e-mail the site sent.
	Mail   *mail.Memory
	server *httptest.Server
	client *http.Client
}

var publishPattern = regexp.MustCompile(`action="([^"]*/publish[^"]*)"`)

// toastPattern is a toast as the admin renders it: its level and its text.
var toastPattern = regexp.MustCompile(
	`(?s)<div class="toast toast-(\w+)" data-flash-level[^>]*>.*?<p class="toast-text">([^<]*)</p>`,
)

// Start runs an app with the given modules on SQLite, signed in as admin.
func Start(t *testing.T, modules ...webthings.Module) *Site {
	t.Helper()

	return StartWith(t, Options{}, modules...)
}

// Options change how StartWith sets up the site.
type Options struct {
	// Templates is a template folder used on top of sitetest's generic
	// templates, holding the module's own, for example comments/list.gohtml.
	Templates string
	// Files replace sitetest's templates entirely, for example a folder of
	// themes.
	Files fs.FS
	// Prepare runs after the modules are added and before the app starts, for
	// example to write templates with App.WriteTemplates.
	Prepare func(app *webthings.App) error
	// Configure changes the app's configuration before it is created, for
	// example its CacheTTL. It runs after OutputCache is applied, so it can
	// set Config.OutputCacheTTL itself.
	Configure func(config *webthings.Config)
	// OutputCache keeps the output cache on, as the site setting says
	// (see webthings.Config.OutputCacheTTL). Sites in tests have it off,
	// so a test that changes content from Go, outside a request, sees the
	// change on the next page also where the app is not told about it.
	OutputCache bool
	// StartingPoints names module instances, such as "news", whose starting
	// points App.WriteTemplates generates for the test. They come below
	// Templates and above sitetest's generic templates, and as they are
	// written for the instance key they take precedence over templates in
	// Templates for the module type. Tests then render the generated
	// templates, which can never drift from the generator, and need no
	// copies of their own.
	StartingPoints []string
	// NoAccount starts the site without accounts, as a fresh install that
	// offers the first-run setup; StartWith then returns a browser that is
	// not signed in. The setup link is <AdminPath>/setup?token=<token> with
	// the token from App.SetupToken, also posted as the form's Token field.
	NoAccount bool
}

// StartWith is Start with options.
func StartWith(t *testing.T, options Options, modules ...webthings.Module) *Site {
	t.Helper()

	files := fs.FS(os.DirFS(templates()))

	var startingPoints string

	if len(options.StartingPoints) > 0 {
		startingPoints = t.TempDir()
		files = partial.Overlay(os.DirFS(startingPoints), files)
	}

	if options.Templates != "" {
		files = partial.Overlay(os.DirFS(options.Templates), files)
	}

	if options.Files != nil {
		files = options.Files
	}

	outbox := &mail.Memory{}

	config := webthings.Config{
		Mail:          outbox,
		Name:          "Test site",
		StateDir:      t.TempDir(),
		Locales:       locales.Config{Default: "en", Enabled: []string{"en", "nl"}},
		AdminEmail:    "admin@example.test",
		AdminPassword: "test-only long passphrase",
		Insecure:      true,
		Templates:     files,
	}

	if options.NoAccount {
		config.AdminEmail, config.AdminPassword = "", ""
	}

	if !options.OutputCache {
		config.OutputCacheTTL = -1
	}

	if options.Configure != nil {
		options.Configure(&config)
	}

	app := webthings.New(config)

	if err := app.Use(modules...); err != nil {
		t.Fatal(err)
	}

	if startingPoints != "" {
		writeStartingPoints(t, app, startingPoints, options.StartingPoints)
	}

	if options.Prepare != nil {
		if err := options.Prepare(app); err != nil {
			t.Fatal(err)
		}
	}

	if err := app.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = app.Close() })

	server := httptest.NewServer(app)
	t.Cleanup(server.Close)

	s := &Site{t: t, App: app, Mail: outbox, server: server}

	if options.NoAccount {
		return s.Visitor()
	}

	return s.As("admin@example.test", "test-only long passphrase")
}

// writeStartingPoints writes the generated templates of the module
// instances keys into dir.
func writeStartingPoints(t *testing.T, app *webthings.App, dir string, keys []string) {
	t.Helper()

	for _, key := range keys {
		if _, err := app.WriteTemplates(dir, key); err != nil {
			t.Fatal(err)
		}
	}
}

// Mails waits until the site has sent at least count e-mails, as some are
// sent in the background, and returns all it sent. It fails the test after
// five seconds.
func (s *Site) Mails(count int) []mail.Message {
	s.t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	for {
		sent := s.Mail.Sent()
		if len(sent) >= count {
			return sent
		}

		if time.Now().After(deadline) {
			s.t.Fatalf("want %d e-mails, the site sent %d", count, len(sent))
		}

		time.Sleep(10 * time.Millisecond)
	}
}

// As returns a second browser on the same site, signed in with another account.
func (s *Site) As(email, password string) *Site {
	s.t.Helper()

	other := s.Visitor()
	other.Submit(s.App.AdminURL("admin.login"), url.Values{"Email": {email}, "Password": {password}})

	return other
}

// Visitor returns a browser on the same site that is not signed in.
func (s *Site) Visitor() *Site {
	jar, _ := cookiejar.New(nil)

	return &Site{t: s.t, App: s.App, Mail: s.Mail, server: s.server, client: &http.Client{Jar: jar}}
}

// Response returns the response to a GET of path, with its headers, and its
// body.
func (s *Site) Response(path string) (*http.Response, string) {
	s.t.Helper()

	response, err := s.client.Get(s.server.URL + path)
	if err != nil {
		s.t.Fatal(err)
	}

	defer func() { _ = response.Body.Close() }()

	body, _ := io.ReadAll(response.Body)

	return response, string(body)
}

// Get returns the status and body of a page.
func (s *Site) Get(path string) (int, string) {
	s.t.Helper()

	response, err := s.client.Get(s.server.URL + path)
	if err != nil {
		s.t.Fatal(err)
	}

	defer func() { _ = response.Body.Close() }()

	body, _ := io.ReadAll(response.Body)

	return response.StatusCode, string(body)
}

// Submit opens the form at path, posts values to it and returns the page it
// ended on. It fails the test on any error status.
func (s *Site) Submit(path string, values url.Values) string {
	s.t.Helper()

	return s.Post(path, path, values)
}

// Post opens the page at formPage and sends values to action from it, as a
// browser submits a form of that page.
func (s *Site) Post(formPage, action string, values url.Values) string {
	s.t.Helper()

	status, body, path := s.send(formPage, action, values)
	if status >= 400 {
		s.t.Fatalf("POST %s: %d\n%s", action, status, body)
	}

	return path
}

// PostStatus is Post for submissions that may fail: it returns the status and
// body instead of failing the test.
func (s *Site) PostStatus(formPage, action string, values url.Values) (int, string) {
	s.t.Helper()

	status, body, _ := s.send(formPage, action, values)

	return status, body
}

// send opens formPage, posts values to action from it and returns the
// status, body and final path after redirects.
func (s *Site) send(formPage, action string, values url.Values) (int, string, string) {
	s.t.Helper()

	if status, page := s.Get(formPage); status >= 400 {
		s.t.Fatalf("GET %s: %d\n%s", formPage, status, page)
	}

	response := s.post(action, values, formPage)
	defer func() { _ = response.Body.Close() }()

	body, _ := io.ReadAll(response.Body)

	return response.StatusCode, string(body), response.Request.URL.Path
}

// File is a file in a form PostFiles sends, as a browser sends the file of
// a file input: Field is the input's name.
type File struct {
	Field string
	Name  string
	Data  []byte
}

// PostFiles opens formPage and sends values and files to action from it as
// a multipart form, as a browser submits a form with a file input. It
// returns the status and body after redirects instead of failing the test.
func (s *Site) PostFiles(formPage, action string, values url.Values, files ...File) (int, string) {
	s.t.Helper()

	if status, page := s.Get(formPage); status >= 400 {
		s.t.Fatalf("GET %s: %d\n%s", formPage, status, page)
	}

	var body bytes.Buffer

	writer := multipart.NewWriter(&body)

	for name, list := range values {
		for _, value := range list {
			if err := writer.WriteField(name, value); err != nil {
				s.t.Fatal(err)
			}
		}
	}

	for _, file := range files {
		part, err := writer.CreateFormFile(file.Field, file.Name)
		if err != nil {
			s.t.Fatal(err)
		}

		if _, err := part.Write(file.Data); err != nil {
			s.t.Fatal(err)
		}
	}

	if err := writer.Close(); err != nil {
		s.t.Fatal(err)
	}

	request, err := http.NewRequest(http.MethodPost, s.server.URL+action, &body)
	if err != nil {
		s.t.Fatal(err)
	}

	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Origin", s.server.URL)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set("Referer", s.server.URL+formPage)

	response, err := s.client.Do(request)
	if err != nil {
		s.t.Fatal(err)
	}

	defer func() { _ = response.Body.Close() }()

	page, _ := io.ReadAll(response.Body)

	return response.StatusCode, string(page)
}

// post sends a form to path as a browser does from a page of the same site
// at from: with the Origin and Sec-Fetch-Site headers that tell the site's
// cross-origin protection the post is its own.
func (s *Site) post(path string, values url.Values, from string) *http.Response {
	s.t.Helper()

	request, err := http.NewRequest(http.MethodPost, s.server.URL+path, strings.NewReader(values.Encode()))
	if err != nil {
		s.t.Fatal(err)
	}

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", s.server.URL)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set("Referer", s.server.URL+from)

	response, err := s.client.Do(request)
	if err != nil {
		s.t.Fatal(err)
	}

	return response
}

// Publish presses the publish button in the editor at path.
func (s *Site) Publish(path string) {
	s.t.Helper()

	_, page := s.Get(path)

	match := publishPattern.FindStringSubmatch(page)
	if match == nil {
		s.t.Fatalf("no publish button on %s", path)
	}

	s.Post(path, strings.ReplaceAll(match[1], "&amp;", "&"), url.Values{})
}

// Expect checks that a public page answers with status and contains all parts.
func (s *Site) Expect(path string, status int, parts ...string) {
	s.t.Helper()

	code, body := s.Get(path)
	if code != status {
		s.t.Fatalf("GET %s: want %d, got %d\n%s", path, status, code, body)
	}

	for _, part := range parts {
		if !strings.Contains(body, part) {
			s.t.Fatalf("GET %s: missing %q\n%s", path, part, body)
		}
	}
}

// HasToast reports whether an admin page shows text as a toast of level
// ("success", "info", "warn" or "error").
func HasToast(body, level, text string) bool {
	for _, match := range toastPattern.FindAllStringSubmatch(body, -1) {
		if match[1] == level && match[2] == html.EscapeString(text) {
			return true
		}
	}

	return false
}

// Mount creates and publishes a page with a module below Home, in English,
// and returns its editor address.
func (s *Site) Mount(module, title, slug string) string {
	s.t.Helper()

	return s.MountIn(module, Translation{Locale: "en", Title: title, Slug: slug})
}

// Translation is a page's title and slug in one language, for MountIn.
type Translation struct {
	Locale string
	Title  string
	Slug   string
}

// MountIn creates a page with a module below Home with a translation in
// every given language, publishes it and returns its editor address. The
// page is created in the first language.
func (s *Site) MountIn(module string, translations ...Translation) string {
	s.t.Helper()

	if len(translations) == 0 {
		s.t.Fatal("MountIn needs at least one translation")
	}

	home, err := s.App.Pages.Home(context.Background())
	if err != nil {
		s.t.Fatal(err)
	}

	first := translations[0]

	editor := s.Submit(s.App.AdminURL("admin.pages.new", url.Values{"locale": {first.Locale}}), url.Values{
		"Title":  {first.Title},
		"Slug":   {first.Slug},
		"Parent": {home},
		"Module": {module},
	})

	for _, translation := range translations[1:] {
		s.Submit(editor+"?locale="+translation.Locale, url.Values{
			"Title":  {translation.Title},
			"Slug":   {translation.Slug},
			"Parent": {home},
			"Module": {module},
		})
	}

	s.Publish(editor + "?locale=" + first.Locale)

	return editor
}

// DraftPage saves a new, unpublished English title and slug for the page
// titled title, leaving its published version as it is.
func (s *Site) DraftPage(title, draft, slug string) {
	s.t.Helper()

	ctx := context.Background()

	home, err := s.App.Pages.Home(ctx)
	if err != nil {
		s.t.Fatal(err)
	}

	list, err := s.App.Pages.ListLatest(ctx)
	if err != nil {
		s.t.Fatal(err)
	}

	for _, page := range list {
		text, _ := page.Text("en")
		if text.Title != title {
			continue
		}

		s.Submit(s.App.AdminURL("admin.pages.edit", "id", page.ID, url.Values{"locale": {"en"}}), url.Values{
			"Title":  {draft},
			"Slug":   {slug},
			"Parent": {home},
			"Module": {page.Fields.Module},
		})

		return
	}

	s.t.Fatalf("no page %q", title)
}

// PNG returns a blank PNG image of width × height pixels.
func PNG(t testing.TB, width, height int) []byte {
	t.Helper()

	var buffer bytes.Buffer

	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}

// JPEG returns a grey JPEG image of width × height pixels.
func JPEG(t testing.TB, width, height int) []byte {
	t.Helper()

	var buffer bytes.Buffer

	if err := jpeg.Encode(&buffer, image.NewGray(image.Rect(0, 0, width, height)), nil); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}

// Upload adds a file to the site's media library, as the admin's upload
// does, and returns the new asset.
func (s *Site) Upload(name string, data []byte) media.Asset {
	s.t.Helper()

	asset, err := s.App.Media.Upload(context.Background(), name, bytes.NewReader(data))
	if err != nil {
		s.t.Fatal(err)
	}

	return asset
}

// ExpectMissing checks that a page does not contain part.
func (s *Site) ExpectMissing(path, part string) {
	s.t.Helper()

	_, body := s.Get(path)
	if strings.Contains(body, part) {
		s.t.Fatalf("GET %s: unexpected %q\n%s", path, part, body)
	}
}

// templates is the shared test template folder, independent of the test's
// working directory.
func templates() string {
	_, file, _, _ := runtime.Caller(0)

	return filepath.Join(filepath.Dir(file), "templates")
}

// Send posts a visitor's form to path, as a public form of the site does,
// without opening a page first, and returns the status, body and final path.
func (s *Site) Send(path string, values url.Values) (int, string, string) {
	s.t.Helper()

	response := s.post(path, values, "/")
	defer func() { _ = response.Body.Close() }()

	body, _ := io.ReadAll(response.Body)

	return response.StatusCode, string(body), response.Request.URL.RequestURI()
}

// SendHTMX is Send for a form that posts with HTMX and swaps the element
// with ID target: the post and the redirect HTMX follows carry its request
// headers. It returns the status, the body, which is a part of the page
// without the layout, and the final path.
func (s *Site) SendHTMX(path string, values url.Values, target string) (int, string, string) {
	s.t.Helper()

	request, err := http.NewRequest(http.MethodPost, s.server.URL+path, strings.NewReader(values.Encode()))
	if err != nil {
		s.t.Fatal(err)
	}

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", s.server.URL)
	request.Header.Set("Sec-Fetch-Site", "same-origin")

	// The client follows the redirect with the same headers, as HTMX does.
	connector.SetRequestTarget(connector.NewHTMX4(nil), request, target)

	response, err := s.client.Do(request)
	if err != nil {
		s.t.Fatal(err)
	}

	defer func() { _ = response.Body.Close() }()

	body, _ := io.ReadAll(response.Body)

	return response.StatusCode, string(body), response.Request.URL.RequestURI()
}

// CrossSite posts to path as a page on another website would and returns the
// status.
func (s *Site) CrossSite(path string, values url.Values) int {
	s.t.Helper()

	request, err := http.NewRequest(http.MethodPost, s.server.URL+path, strings.NewReader(values.Encode()))
	if err != nil {
		s.t.Fatal(err)
	}

	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Sec-Fetch-Site", "cross-site")

	response, err := s.client.Do(request)
	if err != nil {
		s.t.Fatal(err)
	}

	_ = response.Body.Close()

	return response.StatusCode
}

// Fragment returns the status and body of the part of the page at path
// with element ID id, asked for as HTMX does for an element that loads on
// its own, such as a panel of the admin dashboard.
func (s *Site) Fragment(path, id string) (int, string) {
	s.t.Helper()

	request, err := http.NewRequest(http.MethodGet, s.server.URL+path, nil)
	if err != nil {
		s.t.Fatal(err)
	}

	// The request asks for the element as the admin's HTMX 4 does.
	connector.SetRequestTarget(connector.NewHTMX4(nil), request, id)

	response, err := s.client.Do(request)
	if err != nil {
		s.t.Fatal(err)
	}

	defer func() { _ = response.Body.Close() }()

	body, _ := io.ReadAll(response.Body)

	return response.StatusCode, string(body)
}

// Dashboard returns the admin dashboard with its panels loaded, as the
// browser shows it once they arrived.
func (s *Site) Dashboard() string {
	s.t.Helper()

	dashboard := s.App.AdminURL("admin.dashboard")

	status, body := s.Get(dashboard)
	if status != http.StatusOK {
		s.t.Fatalf("GET %s: %d\n%s", dashboard, status, body)
	}

	for _, panel := range []string{"dashboard-recent", "dashboard-attention", "dashboard-scheduled"} {
		status, fragment := s.Fragment(dashboard, panel)
		if status != http.StatusOK {
			s.t.Fatalf("dashboard panel %s: %d\n%s", panel, status, fragment)
		}

		body += fragment
	}

	return body
}

// Request sends request with the browser's cookies, its URL being a path
// of the site such as App.AdminURL("admin.presence"), and returns the response unread, for
// streams and other responses a test reads itself. Close its body.
func (s *Site) Request(request *http.Request) *http.Response {
	s.t.Helper()

	target, err := url.Parse(s.server.URL)
	if err != nil {
		s.t.Fatal(err)
	}

	request.URL.Scheme = target.Scheme
	request.URL.Host = target.Host
	request.Host = target.Host

	response, err := s.client.Do(request)
	if err != nil {
		s.t.Fatal(err)
	}

	return response
}
