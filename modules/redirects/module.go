package redirects

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/db"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-partial/exp/flash"
)

//go:embed templates translations
var files embed.FS

const (
	pageSize = 100
	// missedShown is how many missed paths the admin lists.
	missedShown = 100
)

// Option configures the instance.
type Option func(*options)

type options struct {
	key   string
	title string
}

// Named gives the instance its own key, used in URLs of the admin, and
// title, shown in the admin, instead of "redirects" and "Redirects".
func Named(key, title string) Option {
	return func(o *options) {
		o.key = key
		o.title = title
	}
}

// Module adds the Redirects instance, with the key "redirects" and the title
// "Redirects" (translated in the admin) unless Named says otherwise. A site
// needs one: it redirects every old path.
func Module(opts ...Option) webthings.Module {
	config := options{key: "redirects", title: "Redirects"}

	for _, option := range opts {
		option(&config)
	}

	return webthings.Module{
		Type:   "redirects",
		Key:    config.key,
		Title:  config.title,
		Schema: schema(),
		Setup:  setup,
	}
}

func setup(env webthings.Env) (webthings.Parts, error) {
	service := &Service{
		key:        env.Key,
		db:         env.DB,
		isCorePath: env.IsCorePath,
	}

	if base, err := url.Parse(env.BaseURL); err == nil {
		service.host = base.Host
	}

	return webthings.Parts{
		Admin:      []admin.Section{section(env, service)},
		Service:    service,
		Middleware: service.middleware,
		Close:      service.Flush,
		Redirects:  service,
	}, nil
}

func section(env webthings.Env, service *Service) admin.Section {
	return admin.Section{
		ID:    env.Key,
		Title: env.Title,
		Icon:  "link",
		Files: files,
		Trash: service.trash(),
		Routes: func(routes *admin.Routes) {
			s := screens{service: service, admin: routes.Admin(), routes: routes, title: env.Title}

			routes.Get("", admin.View, s.index).As("index")
			routes.Get("/new", admin.Create, s.edit).As("new")
			routes.Post("/new", admin.Create, s.save)
			routes.Get("/import", admin.Create, s.importForm).As("import")
			routes.Post("/import", admin.Create, s.importCSV)
			routes.Get("/missed", admin.View, s.missed).As("missed")
			routes.Post("/missed/clear", admin.Delete, s.clearMissed).As("missed-clear")
			routes.Get("/{id}", admin.View, s.edit).As("edit")
			routes.Post("/{id}", admin.Edit, s.save)
			routes.Post("/{id}/delete", admin.Delete, s.delete).As("delete")
		},
	}
}

// screens are the admin pages of one instance.
type screens struct {
	service *Service
	admin   *admin.Admin
	routes  *admin.Routes
	title   string
}

// IndexView is the data of templates/admin/index.gohtml: the redirects,
// with a box to test where a path leads.
type IndexView struct {
	Title     string
	NewURL    string
	ImportURL string
	MissedURL string
	// TrashURL shows the deleted redirects in the admin's trash.
	TrashURL string
	// TestURL is where the test box sends ?test=<path>.
	TestURL string
	// TestForm is the test box, a form that gets TestURL.
	TestForm form.RenderModel
	Test     *TestResult
	Rows     []IndexRow
	// Pager pages through long lists.
	Pager admin.Pager
}

// IndexRow is one redirect in the list, with how often it was used.
type IndexRow struct {
	URL     string
	From    string
	To      string
	Status  int
	Hits    int64
	LastHit string
	// Chained is set when To leads to another redirect.
	Chained bool
}

// testInput is the test box: the path to follow.
type testInput struct {
	Test string `form:"input,text" label:"Test a path" name:"test" maxLength:"2000" trim:"true" placeholder:"/old-page"`
}

// TestResult is what the test box found for a path.
type TestResult struct {
	Path     string
	Reserved bool
	Steps    []IndexRow
}

func (s screens) index(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Show the hits counted in memory too.
	if err := s.service.Flush(ctx); err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	// Redirects are read whole and paged in memory.
	page, err := admin.ListPage[Redirect]{
		Path: s.routes.URL("index"),
		Size: pageSize,
		All:  s.service.List,
		Fields: func(redirect Redirect) []string {
			return []string{redirect.From, redirect.To}
		},
	}.Read(r)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	view := IndexView{
		Title:     s.title,
		MissedURL: s.routes.URL("missed"),
		TrashURL:  s.admin.TrashURL(s.service.key),
		TestURL:   s.routes.URL("index"),
		Pager:     page.Pager,
	}

	if s.admin.Can(r, admin.Permission{Section: s.service.key, Action: admin.Create}) {
		view.NewURL = s.routes.URL("new")
		view.ImportURL = s.routes.URL("import")
	}

	for _, redirect := range page.Rows {
		view.Rows = append(view.Rows, s.row(r, redirect))
	}

	// The test box only reads; a path it cannot follow shows no result.
	var test testInput

	if errs, err := s.admin.MapForm(r, &test); err == nil && len(errs) == 0 && test.Test != "" {
		view.Test = s.test(r, test.Test)
	}

	// The box asks with GET, so the path shows in the address and the
	// form carries no token.
	view.TestForm = form.WithInfo(test, form.Info{Target: view.TestURL, Method: "get", SubmitText: "Test"})

	s.admin.Render(w, r, admin.Page{
		Title:    s.title,
		Template: "templates/admin/index.gohtml",
		Files:    files,
		Data:     view,
	})
}

func (s screens) row(r *http.Request, redirect Redirect) IndexRow {
	row := IndexRow{
		URL:     s.routes.URL("edit", "id", redirect.ID),
		From:    redirect.From,
		To:      redirect.To,
		Status:  redirect.Status,
		Hits:    redirect.Hits,
		LastHit: s.admin.T(r, "Never"),
	}

	if redirect.LastHit != nil {
		row.LastHit = admin.DateTime(r.Context(), *redirect.LastHit)
	}

	if next := s.service.local(redirect.To); next != "" {
		_, row.Chained = s.service.Resolve(r.Context(), next)
	}

	return row
}

func (s screens) test(r *http.Request, address string) *TestResult {
	result := &TestResult{Path: address}

	cleaned, err := Normalize(address)
	if err != nil {
		return result
	}

	result.Path = cleaned
	result.Reserved = s.service.reserved(cleaned)

	for _, step := range s.service.Trace(r.Context(), cleaned) {
		result.Steps = append(result.Steps, s.row(r, step))
	}

	return result
}

// redirectInput is the editor of one redirect.
type redirectInput struct {
	From   string                 `form:"input,text" label:"Old path"         required:"true" maxLength:"500"  placeholder:"/old-page" description:"The address of the old page. The host and query are ignored."`
	To     string                 `form:"input,text" label:"New address"      required:"true" maxLength:"2000"                         description:"A path on this site, such as /en/news, or a full URL."`
	Status form.SortedSelect[int] `form:"dropdown"   label:"Kind of redirect"                                                                                                                                     translate:"true"`
}

// statusChoices are the kinds of redirect; go-form translates the labels
// (translate:"true").
func statusChoices(_ context.Context, selected int) form.SortedSelect[int] {
	return form.NewSortedSelect(map[int]string{
		http.StatusMovedPermanently:  "301 Moved permanently",
		http.StatusFound:             "302 Found (temporary)",
		http.StatusTemporaryRedirect: "307 Temporary redirect",
		http.StatusPermanentRedirect: "308 Permanent redirect",
	}).Selected(selected)
}

// EditView is the data of templates/admin/edit.gohtml: the form of one
// redirect.
type EditView struct {
	Title   string
	BackURL string
	// BackLabel names the list BackURL leads to, as a translation key.
	BackLabel string
	// Warnings point out chains of redirects.
	Warnings []string
	// Saved is set for an existing redirect, which has hits.
	Saved   bool
	Hits    int64
	LastHit string
	Form    form.RenderModel
	Errors  form.FieldErrors
	Delete  *form.RenderModel
}

func (s screens) edit(w http.ResponseWriter, r *http.Request) {
	redirect := Redirect{Status: http.StatusMovedPermanently, From: r.URL.Query().Get("from")}

	if id := r.PathValue("id"); id != "" {
		var err error

		redirect, err = s.service.Get(r.Context(), id)
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)

			return
		}

		if err != nil {
			s.admin.Fail(w, r, err)

			return
		}
	}

	input := redirectInput{
		From:   redirect.From,
		To:     redirect.To,
		Status: statusChoices(r.Context(), redirect.Status),
	}

	s.render(w, r, redirect, input, nil, "", http.StatusOK)
}

func (s screens) render(
	w http.ResponseWriter,
	r *http.Request,
	redirect Redirect,
	input redirectInput,
	errs form.FieldErrors,
	message string,
	status int,
) {
	action := s.routes.URL("new")
	title := s.admin.T(r, "New redirect")

	if redirect.ID != "" {
		action = s.routes.URL("edit", "id", redirect.ID)
		title = redirect.From
	}

	// The message shows as a toast; errors of single fields stay inline.
	r = admin.WithError(r, message)

	view := EditView{
		Title:     title,
		BackURL:   s.routes.URL("index"),
		BackLabel: s.title,
		Hits:      redirect.Hits,
		LastHit:   s.admin.T(r, "Never"),
		Form:      s.admin.Form(r, input, action, "Save"),
		Errors:    errs,
	}

	if redirect.LastHit != nil {
		view.LastHit = admin.DateTime(r.Context(), *redirect.LastHit)
	}

	if redirect.ID != "" {
		view.Saved = true
		view.Warnings = s.warnings(r, redirect)

		if s.admin.Can(r, admin.Permission{Section: s.service.key, Action: admin.Delete}) {
			remove := s.admin.Form(r, struct{}{}, s.routes.URL("delete", "id", redirect.ID), "Move to trash")
			view.Delete = &remove
		}
	}

	s.admin.Render(w, r, admin.Page{
		Title:    title,
		Template: "templates/admin/edit.gohtml",
		Files:    files,
		Data:     view,
		Status:   status,
	})
}

// warnings explain chains: a redirect to a path that redirects again sends
// visitors through two redirects, which is slower and loses search ranking.
func (s screens) warnings(r *http.Request, redirect Redirect) []string {
	var warnings []string

	if next := s.service.local(redirect.To); next != "" {
		if onward, ok := s.service.Resolve(r.Context(), next); ok {
			warnings = append(warnings, fmt.Sprintf(
				s.admin.T(r, "The new address redirects again, to %s. Point this redirect to the final address."),
				onward.To,
			))
		}
	}

	var incoming []string

	for _, other := range s.service.current(r.Context()).byPath {
		if other.ID != redirect.ID && s.service.local(other.To) == redirect.From {
			incoming = append(incoming, other.From)
		}
	}

	if len(incoming) > 0 {
		warnings = append(warnings, fmt.Sprintf(
			s.admin.T(r, "Other redirects lead to this path and now take two steps: %s."),
			strings.Join(incoming, ", "),
		))
	}

	return warnings
}

func (s screens) save(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	redirect := Redirect{ID: r.PathValue("id"), Status: http.StatusMovedPermanently}

	if redirect.ID != "" {
		var err error

		redirect, err = s.service.Get(ctx, redirect.ID)
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)

			return
		}

		if err != nil {
			s.admin.Fail(w, r, err)

			return
		}
	}

	input := redirectInput{Status: statusChoices(r.Context(), redirect.Status)}

	errs, err := s.admin.Bind(r, &input)
	if err != nil {
		s.admin.BadRequest(w, r)

		return
	}

	if len(errs) > 0 {
		s.render(w, r, redirect, input, errs, "", http.StatusUnprocessableEntity)

		return
	}

	change := redirect
	change.From = input.From
	change.To = input.To
	change.Status = input.Status.Get()

	saved, err := s.service.Save(ctx, change)
	if err != nil {
		message := s.issue(r, err)
		if message == "" {
			s.admin.Fail(w, r, err)

			return
		}

		s.render(w, r, redirect, input, nil, message, http.StatusUnprocessableEntity)

		return
	}

	s.admin.Redirect(w, r, s.routes.URL("edit", "id", saved.ID))
}

func (s screens) delete(w http.ResponseWriter, r *http.Request) {
	err := s.service.Delete(r.Context(), r.PathValue("id"))
	if errors.Is(err, db.ErrNotFound) {
		http.NotFound(w, r)

		return
	}

	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	s.admin.Redirect(w, r, s.routes.URL("index"))
}

// importInput is the CSV import form.
type importInput struct {
	CSV string `form:"textarea" label:"Redirects" required:"true" maxLength:"262144" rows:"12" placeholder:"/old-page,/en/new-page,301" description:"One redirect per line: old path, new address and, optionally, the status (301 when empty). Existing redirects with the same old path are replaced."`
}

// ImportView is the data of templates/admin/import.gohtml: the form that
// imports redirects from CSV.
type ImportView struct {
	Title   string
	BackURL string
	// Problems lists the refused lines; nothing was imported.
	Problems []string
	Form     form.RenderModel
	Errors   form.FieldErrors
}

func (s screens) importForm(w http.ResponseWriter, r *http.Request) {
	s.renderImport(w, r, importInput{}, nil, "", nil, http.StatusOK)
}

func (s screens) renderImport(
	w http.ResponseWriter,
	r *http.Request,
	input importInput,
	errs form.FieldErrors,
	message string,
	problems []string,
	status int,
) {
	// The message shows as a toast; errors of single fields stay inline.
	r = admin.WithError(r, message)

	s.admin.Render(w, r, admin.Page{
		Title:    "Import redirects",
		Template: "templates/admin/import.gohtml",
		Files:    files,
		Status:   status,
		Data: ImportView{
			Title:    s.title,
			BackURL:  s.routes.URL("index"),
			Problems: problems,
			Form:     s.admin.Form(r, input, s.routes.URL("import"), "Import"),
			Errors:   errs,
		},
	})
}

func (s screens) importCSV(w http.ResponseWriter, r *http.Request) {
	var input importInput

	errs, err := s.admin.Bind(r, &input)
	if err != nil {
		s.admin.BadRequest(w, r)

		return
	}

	if len(errs) > 0 {
		s.renderImport(w, r, input, errs, "", nil, http.StatusUnprocessableEntity)

		return
	}

	imported, err := s.service.Import(r.Context(), strings.NewReader(input.CSV))

	var refused *ImportError

	switch {
	case errors.As(err, &refused):
		var problems []string

		for _, line := range refused.Lines {
			problems = append(problems, fmt.Sprintf(s.admin.T(r, "Line %d: %s"), line.Line, s.issue(r, line.Err)))
		}

		message := s.admin.T(r, "Nothing was imported. Correct these lines and try again.")
		s.renderImport(w, r, input, nil, message, problems, http.StatusUnprocessableEntity)
	case err != nil && s.issue(r, err) != "":
		s.renderImport(w, r, input, nil, s.issue(r, err), nil, http.StatusUnprocessableEntity)
	case err != nil:
		s.admin.Fail(w, r, err)
	default:
		// The notice is translated here because it carries the number, so
		// it is shown as written.
		s.admin.RedirectFlash(w, r, s.routes.URL("index"), flash.Success(s.admin.T(r, "Redirects imported: %d", imported)))
	}
}

// MissedView is the data of templates/admin/missed.gohtml: the paths
// visitors asked for that the site does not have.
type MissedView struct {
	Title   string
	BackURL string
	Rows    []MissedRow
	Clear   *form.RenderModel
}

// MissedRow is one path that was not found, with how often it was asked
// for.
type MissedRow struct {
	Path    string
	Hits    int64
	LastHit string
	// AddURL opens a new redirect for the path; empty without the right.
	AddURL string
}

func (s screens) missed(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := s.service.Flush(ctx); err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	list, err := s.service.Missed(ctx, missedShown)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	view := MissedView{Title: s.title, BackURL: s.routes.URL("index")}
	canCreate := s.admin.Can(r, admin.Permission{Section: s.service.key, Action: admin.Create})

	search := admin.SearchOf(r)

	for _, missed := range list {
		if !search.Matches(missed.Path) {
			continue
		}

		row := MissedRow{Path: missed.Path, Hits: missed.Hits, LastHit: admin.DateTime(r.Context(), missed.LastHit)}

		if canCreate {
			row.AddURL = s.routes.URL("new", url.Values{"from": {missed.Path}})
		}

		view.Rows = append(view.Rows, row)
	}

	if len(list) > 0 && s.admin.Can(r, admin.Permission{Section: s.service.key, Action: admin.Delete}) {
		clearForm := s.admin.Form(r, struct{}{}, s.routes.URL("missed-clear"), "Clear list")
		clearForm = s.admin.Confirm(r, clearForm, "Clear the list of missed paths?")
		view.Clear = &clearForm
	}

	s.admin.Render(w, r, admin.Page{
		Title:    "Missed paths",
		Template: "templates/admin/missed.gohtml",
		Files:    files,
		Data:     view,
	})
}

func (s screens) clearMissed(w http.ResponseWriter, r *http.Request) {
	if err := s.service.ClearMissed(r.Context()); err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	s.admin.RedirectNotice(w, r, s.routes.URL("missed"), "The list was cleared.")
}

// issue turns module errors into messages for the editor; empty means unknown.
func (s screens) issue(r *http.Request, err error) string {
	messages := []struct {
		err     error
		message string
	}{
		{errFrom, "The old path is not valid."},
		{errReserved, "This path belongs to the website itself and cannot be redirected."},
		{errTo, "The new address must be a path on this site, such as /en/news, or a full URL."},
		{errStatus, "The status must be 301, 302, 307 or 308."},
		{errLoop, "This redirect would lead back to its own path."},
		{errDuplicate, "A redirect for this path already exists."},
		{errRepeated, "This old path is already on an earlier line."},
		{errColumns, "Use two or three columns: old path, new address, status."},
		{errEmpty, "There are no redirects to import."},
		{errTooMany, "Import at most 5000 redirects at a time."},
		{errTooLarge, "Import at most 1 MB of redirects at a time."},
	}

	for _, known := range messages {
		if errors.Is(err, known.err) {
			return s.admin.T(r, known.message)
		}
	}

	return ""
}
