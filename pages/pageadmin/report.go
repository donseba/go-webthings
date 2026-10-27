package pageadmin

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/admin/editor"
	"github.com/donseba/go-webthings/links"
	"github.com/donseba/go-webthings/pages"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-partial/exp/flash"
)

// LinksView is the report of the broken links: the addresses that links in
// published content lead to and that the website does not answer.
type LinksView struct {
	Title   string
	BackURL string
	Rows    []BrokenRow
	// Redirects is set when the site has a module that redirects old
	// addresses and the user may add redirects to it.
	Redirects bool
	// Hint tells, without Redirects, what redirecting an address here
	// needs: the Redirects module, or the right to create redirects; it is
	// a translation key.
	Hint string
}

// BrokenRow is an address that published content links to and that the
// website does not answer, with the content that links there.
type BrokenRow struct {
	Address string
	// Reason says why nothing answers there; it is a translation key.
	Reason string
	// Page is the page with the address that is not published, or is in
	// the trash, linked to its editor or to the trash; nil when no page has
	// the address.
	Page *editor.NoticeItem
	// Sources are the content that links to the address, linked to its
	// editor.
	Sources []editor.NoticeItem
	// Redirect redirects the address to a page; nil without a module that
	// redirects.
	Redirect *form.RenderModel
}

// Why the website does not answer at an address that links lead to.
const (
	reasonNoLanguage  = "The address does not start with a language of the website."
	reasonUnpublished = "The page with this address is not published."
	reasonTrashed     = "The page with this address is in the trash."
	reasonMissing     = "No page has this address."
)

// What redirecting an address in the report needs, when the user cannot.
const (
	hintModule = "With the Redirects module, you can redirect such an address to another page here."
	hintRight  = "You need the right to create redirects."
)

// absent is a page with an address that the website does not answer.
type absent struct {
	page    pages.Page
	trashed bool
}

// report shows the broken links (see LinksView).
func (l pageLinks) report(e pageEditor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := e.Admin()
		ctx := r.Context()

		_, config, err := e.Locale(r)
		if err != nil {
			a.Fail(w, r, err)

			return
		}

		broken, err := l.check.Broken(ctx)
		if err != nil {
			a.Fail(w, r, err)

			return
		}

		table, err := l.tree.Table(ctx)
		if err != nil {
			a.Fail(w, r, err)

			return
		}

		absentPages, err := l.absent(r, table, config.Enabled)
		if err != nil {
			a.Fail(w, r, err)

			return
		}

		view := LinksView{
			Title:     "Broken links",
			BackURL:   a.URL("admin.pages.index"),
			Redirects: l.redirector(r, e) != nil,
		}

		if installed, _ := l.check.Redirects(); installed == nil {
			view.Hint = hintModule
		} else if !view.Redirects {
			view.Hint = hintRight
		}

		rows := map[string]*BrokenRow{}

		var order []string

		for _, link := range broken {
			row, ok := rows[link.Address]
			if !ok {
				row = l.brokenRow(r, e, link.Address, config.Enabled, absentPages)
				rows[link.Address] = row
				order = append(order, link.Address)
			}

			item := editor.LinkItem(r, a, l.check, link)
			if !slices.Contains(row.Sources, item) {
				row.Sources = append(row.Sources, item)
			}
		}

		slices.Sort(order)

		for i, address := range order {
			row := rows[address]

			if view.Redirects {
				in := addressInput{
					Address: address,
					Target:  editor.RedirectTargets(table, addressLocale(address, config.Enabled, config.Default), "", ""),
				}
				model := a.Form(r, in, a.URL("admin.pages.links-redirect"), "Create redirect")
				model.Info.IDPrefix = fmt.Sprintf("redirect-%d", i)
				row.Redirect = &model
			}

			view.Rows = append(view.Rows, *row)
		}

		a.Render(w, r, admin.Page{
			Title:    view.Title,
			Template: "templates/links.gohtml",
			Files:    files,
			Data:     view,
		})
	}
}

// brokenRow describes an address that links lead to and nothing answers.
func (l pageLinks) brokenRow(
	r *http.Request,
	e pageEditor,
	address string,
	enabled []string,
	absentPages map[string]absent,
) *BrokenRow {
	a := e.Admin()
	row := &BrokenRow{Address: address, Reason: reasonMissing}

	locale, _, _ := strings.Cut(strings.TrimPrefix(address, "/"), "/")
	if !slices.Contains(enabled, locale) {
		row.Reason = reasonNoLanguage

		return row
	}

	found, ok := absentPages[address]
	if !ok {
		return row
	}

	row.Page = &editor.NoticeItem{Label: title(found.page, locale)}

	if found.trashed {
		row.Reason = reasonTrashed
		row.Page.URL = a.TrashURL("pages")
	} else {
		row.Reason = reasonUnpublished
		row.Page.URL = a.URL("admin.pages.edit", "id", found.page.ID, url.Values{"locale": {locale}})
	}

	return row
}

// absent maps the addresses of pages that are not on the website to them:
// the addresses of the latest tree, and those trashed pages had below their
// parent's latest or published address.
func (l pageLinks) absent(r *http.Request, table *pages.Table, enabled []string) (map[string]absent, error) {
	ctx := r.Context()
	found := map[string]absent{}

	trashed, err := l.tree.Trashed(ctx)
	if err != nil {
		return nil, err
	}

	for _, locale := range enabled {
		nodes, err := l.tree.Tree(ctx, locale)
		if err != nil {
			return nil, err
		}

		paths := map[string]string{}

		for _, node := range nodes {
			paths[node.Page.ID] = node.Path

			if node.Path != "" {
				found[links.Address(locale, node.Path)] = absent{page: node.Page}
			}
		}

		for _, page := range trashed {
			text, ok := page.Text(locale)
			if !ok || text.Slug == "" {
				continue
			}

			parents := []string{paths[page.Fields.ParentID], table.Alternates(page.Fields.ParentID)[locale]}

			for _, parent := range parents {
				if parent == "" {
					continue
				}

				address := links.Address(locale, strings.TrimSuffix(parent, "/")+"/"+text.Slug)
				if _, taken := found[address]; !taken {
					found[address] = absent{page: page, trashed: true}
				}
			}
		}
	}

	return found, nil
}

// addressLocale is the language of an address, its first segment, or
// fallback for an address without one.
func addressLocale(address string, enabled []string, fallback string) string {
	locale, _, _ := strings.Cut(strings.TrimPrefix(address, "/"), "/")
	if slices.Contains(enabled, locale) {
		return locale
	}

	return fallback
}

// redirectAddress redirects an address of the report to the chosen page,
// in the address's language or else in the default one.
func (l pageLinks) redirectAddress(e pageEditor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := e.Admin()
		ctx := r.Context()
		back := a.URL("admin.pages.links")

		redirects := l.redirector(r, e)
		if redirects == nil {
			a.Forbidden(w, r)

			return
		}

		_, config, err := e.Locale(r)
		if err != nil {
			a.Fail(w, r, err)

			return
		}

		table, err := l.tree.Table(ctx)
		if err != nil {
			a.Fail(w, r, err)

			return
		}

		locale := addressLocale(r.PostFormValue("address"), config.Enabled, config.Default)
		in := addressInput{Target: editor.RedirectTargets(table, locale, "", "")}

		if errs, err := a.Bind(r, &in); err != nil || len(errs) > 0 || in.Target.Get() == "" {
			a.BadRequest(w, r)

			return
		}

		destination := table.Alternates(in.Target.Get())

		path, ok := destination[locale]
		if !ok {
			locale = config.Default
			path, ok = destination[locale]
		}

		if !ok {
			a.BadRequest(w, r)

			return
		}

		if err := redirects.Redirect(ctx, in.Address, links.Address(locale, path)); err != nil {
			a.RedirectFlash(w, r, back, flash.Error(a.T(r, "This address cannot be redirected to that page.")))

			return
		}

		a.RedirectNotice(w, r, back, "Redirect saved.")
	}
}
