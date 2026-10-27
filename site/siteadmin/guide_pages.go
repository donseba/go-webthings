package siteadmin

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/auth"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/pages"
	"github.com/donseba/go-webthings/site"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-form/v2/types"
	"github.com/donseba/go-partial/exp/flash"
)

// PagesView is the guide's Pages step: a row for each module instance the
// site can show on a page, and for a few ordinary pages most sites have.
// Module pages are made below Home and published at once, so visitors can
// reach the modules; ordinary pages are made as drafts, to write first.
type PagesView struct {
	Rows   []PageRow
	Form   *form.RenderModel
	Errors form.FieldErrors
	// Actions are the dashboard's quick actions, "Write the first …",
	// once there are pages.
	Actions []admin.QuickAction
	// Modules is set when the site has modules that can be on a page.
	Modules bool
}

// PageRow is one page the step offers.
type PageRow struct {
	// Title is the module's title or the page's.
	Title string
	// Module is the module instance's key; empty for an ordinary page.
	Module string
	// OnPage is the title of the page that has it already, with the
	// address of its editor; empty while there is none.
	OnPage  string
	EditURL string
	// Fields are the names of the row's fields in Form: the box to create
	// the page and its title in each language.
	Fields []string
}

// plainPages are the ordinary pages the step offers, by row ID; their
// titles are translation keys, written in the admin language.
var plainPages = []struct {
	id    string
	title string
}{
	{"about", "About us"},
	{"contact", "Contact"},
	{"privacy", "Privacy"},
}

// pageRow is a row with what the step needs to make its page.
type pageRow struct {
	PageRow

	id string
	// titles are the titles the row is known by, besides those posted:
	// the plain page's title in the admin language.
	titles []string
}

// pageRows are the step's rows: the module instances, then the plain
// pages, each with the page that has it, if any.
func (g *guide) pageRows(r *http.Request, current site.Settings) ([]pageRow, []pages.Page, string, error) {
	ctx := r.Context()

	home, err := g.options.Pages.Home(ctx)
	if err != nil {
		return nil, nil, "", err
	}

	latest, err := g.options.Pages.ListLatest(ctx)
	if err != nil {
		return nil, nil, "", err
	}

	var rows []pageRow

	if g.options.Modules != nil {
		for _, module := range g.options.Modules() {
			row := pageRow{id: "module-" + module.Key, PageRow: PageRow{Title: module.Title, Module: module.Key}}

			for _, page := range latest {
				if page.Fields.Module == module.Key {
					g.onPage(&row, page, current.DefaultLocale)

					break
				}
			}

			rows = append(rows, row)
		}
	}

	for _, plain := range plainPages {
		title := g.admin.T(r, plain.title)
		row := pageRow{id: "page-" + plain.id, PageRow: PageRow{Title: title}, titles: []string{title}}

		if page, ok := childTitled(latest, home, row.titles); ok {
			g.onPage(&row, page, current.DefaultLocale)
		}

		rows = append(rows, row)
	}

	return rows, latest, home, nil
}

// onPage notes the page that has the row.
func (g *guide) onPage(row *pageRow, page pages.Page, locale string) {
	text, _ := page.Text(locale)
	if text.Title == "" && len(page.Translations) > 0 {
		text = page.Translations[0].Text
	}

	row.OnPage = text.Title
	row.EditURL, _ = g.admin.TryURL("admin.pages.edit", "id", page.ID)
}

// childTitled is the page below home with one of titles in any language,
// ignoring case.
func childTitled(latest []pages.Page, home string, titles []string) (pages.Page, bool) {
	for _, page := range latest {
		if page.Fields.ParentID != home || page.Fields.Module != "" {
			continue
		}

		for _, translation := range page.Translations {
			for _, title := range titles {
				if title != "" && strings.EqualFold(strings.TrimSpace(translation.Text.Title), strings.TrimSpace(title)) {
					return page, true
				}
			}
		}
	}

	return pages.Page{}, false
}

// pagesSchema is the step's form: per row without a page, a box to create
// it and its title in each language, filled with the row's title.
func (g *guide) pagesSchema(r *http.Request, rows []pageRow, current site.Settings) (form.Schema, url.Values) {
	var schema form.Schema

	values := url.Values{}

	for i := range rows {
		row := &rows[i]
		if row.OnPage != "" {
			continue
		}

		// A module's page is ticked: without one, visitors cannot reach
		// the module.
		create := row.id + ".create"
		if row.Module != "" {
			values.Set(create, "on")
		}

		schema.Fields = append(schema.Fields, form.Spec{
			Name:  create,
			Label: g.admin.T(r, "Create page"),
			Type:  types.FieldTypeCheckbox,
		})
		row.Fields = append(row.Fields, create)

		for _, locale := range current.Locales {
			name := row.id + ".title." + locale
			schema.Fields = append(schema.Fields, form.Spec{
				Name:       name,
				Label:      g.admin.T(r, "Title in %s", admin.LanguageLabel(locale)),
				MaxLength:  200,
				SingleLine: true,
			})
			row.Fields = append(row.Fields, name)
			values.Set(name, row.Title)
		}
	}

	return schema, values
}

// pagesView is the step with the rows' state; posted and errs are those of
// a refused post, nil to show the form filled anew.
func (g *guide) pagesView(r *http.Request, current site.Settings, posted url.Values, errs form.FieldErrors) (*PagesView, error) {
	rows, _, _, err := g.pageRows(r, current)
	if err != nil {
		return nil, err
	}

	schema, values := g.pagesSchema(r, rows, current)
	if posted != nil {
		values = posted
	}

	view := &PagesView{Errors: errs, Modules: g.options.Modules != nil && len(g.options.Modules()) > 0}

	for _, row := range rows {
		view.Rows = append(view.Rows, row.PageRow)
	}

	if len(schema.Fields) > 0 {
		model := g.admin.Form(r, schema.Bind(values), g.admin.URL("admin.site.guide-pages"), "Create the ticked pages")
		view.Form = &model
	}

	// What to write first, once there is somewhere to show it.
	if slices.ContainsFunc(rows, func(row pageRow) bool {
		return row.OnPage != ""
	}) {
		view.Actions = g.admin.QuickActions(r)
	}

	return view, nil
}

// createPages makes the ticked pages: module pages published below Home,
// ordinary pages as drafts. A row that has a page already is left alone,
// so posting the form again makes nothing twice; a page that cannot be
// made is refused at its row.
func (g *guide) createPages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r = withStep(r, stepPages)

	current, err := g.settings.Settings(ctx)
	if err != nil {
		g.admin.Fail(w, r, err)

		return
	}

	rows, latest, home, err := g.pageRows(r, current)
	if err != nil {
		g.admin.Fail(w, r, err)

		return
	}

	schema, _ := g.pagesSchema(r, rows, current)
	posted := form.MapValues(r, schema)
	errs := g.admin.ValidateSchema(r, schema, posted)
	user, _ := auth.CurrentUser(ctx)
	created := 0

	for _, row := range rows {
		create := row.id + ".create"
		// A row with a page has no fields: it is never ticked.
		if posted.Get(create) != "on" || len(errs.Messages(create)) > 0 {
			continue
		}

		var translations []content.Translation[pages.Text]

		titles := slices.Clone(row.titles)

		for _, locale := range current.Locales {
			if title := posted.Get(row.id + ".title." + locale); title != "" {
				titles = append(titles, title)
				translations = append(
					translations,
					content.Translation[pages.Text]{Locale: locale, Text: pages.Text{Title: title}},
				)
			}
		}

		if posted.Get(row.id+".title."+current.DefaultLocale) == "" {
			errs = append(errs, admin.FieldError(row.id+".title."+current.DefaultLocale,
				g.admin.T(r, "Enter a title in the default language, or untick the page."))...)

			continue
		}

		// A plain page made by an earlier post, under a title of this one,
		// is the row's page.
		if row.Module == "" {
			if _, ok := childTitled(latest, home, titles); ok {
				continue
			}
		}

		err := g.createPage(r, home, row, translations, user.ID)

		switch {
		case err == nil:
			created++
		case errors.Is(err, pages.ErrMounted):
			errs = append(errs, admin.FieldError(create, g.admin.T(r, "Another page shows this module already."))...)
		case errors.Is(err, pages.ErrSlug), errors.Is(err, pages.ErrDuplicate):
			errs = append(errs, admin.FieldError(create,
				g.admin.T(r, "A page with this address exists already, or the title makes no address. Choose another title."))...)
		case errors.Is(err, content.ErrNotApproved):
			// With the review on for Pages, a module page cannot be
			// published at once; createPage removed it, so the row is
			// offered again.
			errs = append(errs, admin.FieldError(
				create,
				g.admin.T(
					r,
					"Pages need a review before they are published. Create this page under Pages, or switch off the review for Pages first.",
				),
			)...)
		default:
			g.admin.Fail(w, r, err)

			return
		}
	}

	if len(errs) > 0 {
		view, err := g.pagesView(r, current, posted, errs)
		if err != nil {
			g.admin.Fail(w, r, err)

			return
		}

		g.render(w, r, stepPages, guideParts{pages: view, status: http.StatusUnprocessableEntity})

		return
	}

	if err := g.finishStep(ctx, stepPages); err != nil {
		g.admin.Fail(w, r, err)

		return
	}

	message := flash.Success(admin.TN(ctx, "%d page created.", "%d pages created.", created, created))
	if created == 0 {
		message = flash.Info(g.admin.T(r, "No page was ticked: nothing was created."))
	}

	g.admin.RedirectFlash(w, r, g.admin.URL("admin.site.guide-step", "step", stepPages), message)
}

// createPage saves the row's page below home; a module page is published.
func (g *guide) createPage(
	r *http.Request,
	home string,
	row pageRow,
	translations []content.Translation[pages.Text],
	author string,
) error {
	ctx := r.Context()

	page, err := g.options.Pages.Save(ctx, pages.Draft{
		Fields:       pages.Fields{ParentID: home, Module: row.Module},
		Translations: translations,
		Author:       author,
		Note:         g.admin.T(r, "Made by the setup guide"),
	})
	if err != nil {
		return err
	}

	if row.Module == "" {
		return nil
	}

	err = g.options.Pages.Publish(ctx, page.ID, page.Revision.ID)
	if err == nil {
		return nil
	}

	// A module page that cannot be published is removed again, so the row
	// offers it anew.
	if remove := g.options.Pages.Delete(ctx, page.ID); remove == nil {
		_ = g.options.Pages.Purge(ctx, page.ID)
	}

	return err
}
