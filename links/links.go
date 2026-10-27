// Package links knows which published content of a site links to which
// address on it: the Markdown of the published pages, and what modules
// report of their published content (Func), such as the text of a post or
// the address of a banner's button. The admin uses it to warn editors
// before a page leaves its address, to offer redirects for the addresses
// links lead to, and to list the links that lead nowhere.
//
// Links are compared as addresses on the site, such as "/en/about": a
// relative link is resolved from the address of the content it is in, the
// way a browser resolves it, an absolute URL to the site's own host
// (Options.BaseURL) counts as a link on the site, and the query and the
// fragment are dropped. Links to other websites, e-mail and phone links,
// media files (which cannot be deleted while content uses them) and the
// addresses the app serves itself (Options.IsCorePath) are not indexed.
//
// The index is made when it is asked for, never while a visitor's page is
// served, and kept until what visitors see changes in this process
// (Options.Version) or the published page tree does; changes by other
// processes show within Options.TTL. The links of a published revision are
// kept apart, per revision and language, as revisions never change: like
// the search texts, they are found once.
package links

import (
	"cmp"
	"context"
	"log/slog"
	"net/url"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/locales"
	"github.com/donseba/go-webthings/markdown"
	"github.com/donseba/go-webthings/pages"
)

// Source is published content in one language with the links in it, as a
// module reports it (Func).
type Source struct {
	// ID identifies the content in its module, such as a post's ID; for a
	// page it is the page's ID.
	ID string
	// Title names the content for editors.
	Title string
	// URL is the content's own address on the site, such as
	// "/en/news/hello": its relative links lead from there. Content without
	// an address of its own, such as a banner, leaves it empty; its
	// relative links lead from the home page of its language.
	URL string
	// EditURL opens the content in the admin, such as the editor of a post
	// (see webthings.Env.AdminURL). Pages leave it empty: the admin knows
	// their editor.
	EditURL string
	// Markdown holds the content's texts in Markdown, such as a body; their
	// links count (see markdown.Links).
	Markdown []string
	// Addresses are the content's link fields, such as a button's address.
	Addresses []string
	// Revision names the revision Markdown comes from, for content with
	// revisions: the links found in it are then kept with the revision, as
	// those of pages are. Empty finds them again for every index.
	Revision string
}

// Func lists the published content of a module instance in locale with the
// links in it, for webthings.Parts.Links: what visitors can see, and no
// drafts.
type Func func(ctx context.Context, locale string) ([]Source, error)

// Redirector is a module that redirects old addresses to new ones, such as
// the redirects module (webthings.Parts.Redirects): the admin offers to
// redirect the addresses that links lead to when a page leaves them.
type Redirector interface {
	// Redirected returns where a redirect sends the visitors of address,
	// an address on the site such as "/en/about"; ok is false when no
	// redirect has it.
	Redirected(ctx context.Context, address string) (target string, ok bool)
	// Redirect sends the visitors of address to target, an address on the
	// site, from now on, with a permanent redirect (301), replacing a
	// redirect of address to elsewhere. A redirect of target itself, which
	// would hide the page that is there now, is removed.
	Redirect(ctx context.Context, address, target string) error
}

// Link is a link in published content to an address on the site.
type Link struct {
	// Module is the instance key of the module the content belongs to;
	// empty for a page.
	Module string
	// Locale is the language of the content.
	Locale string
	// Source is the content the link is in; it is shared by its links.
	Source *Source
	// Href is the link as written, such as "../contact#form".
	Href string
	// Address is where the link leads on the site, cleaned and without
	// query and fragment, such as "/en/contact".
	Address string
	// Page is the ID of the published page that answers at Address, or of
	// the module page above it; empty when none does.
	Page string
	// Found reports whether the site answers at Address with a page: a
	// published page, an address below a module page, which the module
	// answers, or the search screen.
	Found bool
}

// Options configure a Check.
type Options struct {
	// BaseURL is the site's address, such as "https://example.com": full
	// URLs to its host are links on the site.
	BaseURL string
	// IsCorePath reports the addresses the app serves itself, such as the
	// media files and visitor actions (webthings.App.IsCorePath): links to
	// them do not lead to pages.
	IsCorePath func(address string) bool
	// Version moves on with every change in this process of what visitors
	// see, such as the app's output cache version: the index is made again
	// once it did. Nil only follows the page tree and TTL.
	Version func() uint64
	// TTL is how long an index is kept at most, so changes by other
	// processes show; zero is 30 seconds, less than zero keeps none.
	TTL time.Duration
}

// defaultTTL is how long an index is kept without Options.TTL.
const defaultTTL = 30 * time.Second

// module is a module instance that reports its links.
type module struct {
	key    string
	title  string
	source Func
}

// Check finds the links between the published content of a site. It is
// safe for concurrent use once the modules are added (Use, SetRedirects).
type Check struct {
	pages   *pages.Service
	locales locales.Provider
	options Options
	host    string

	modules      []module
	redirects    Redirector
	redirectsKey string

	mu      sync.Mutex
	index   *Index
	version uint64
	table   *pages.Table
	expires time.Time

	texts revisionLinks
}

// New returns the Check of the published pages of tree, in the languages
// provider enables, and of the modules added with Use.
func New(tree *pages.Service, provider locales.Provider, options Options) *Check {
	c := &Check{pages: tree, locales: provider, options: options}

	if base, err := url.Parse(options.BaseURL); err == nil {
		c.host = base.Host
	}

	return c
}

// Use adds the links of a module instance, with its key and title. Call it
// before the Check is used.
func (c *Check) Use(key, title string, source Func) {
	if source != nil {
		c.modules = append(c.modules, module{key: key, title: title, source: source})
	}
}

// Title is the title of the module instance with key, as Use was given it;
// empty for pages and unknown keys.
func (c *Check) Title(key string) string {
	for _, module := range c.modules {
		if module.key == key {
			return module.title
		}
	}

	return ""
}

// SetRedirects sets the module instance, with its key, that redirects old
// addresses. Call it before the Check is used.
func (c *Check) SetRedirects(key string, redirects Redirector) {
	c.redirectsKey = key
	c.redirects = redirects
}

// Redirects returns the module instance that redirects old addresses, with
// its key, such as for the rights of its admin section; nil without one.
func (c *Check) Redirects() (Redirector, string) {
	return c.redirects, c.redirectsKey
}

// Table returns the published page table the links are resolved against,
// such as for the pages an address can redirect to.
func (c *Check) Table(ctx context.Context) (*pages.Table, error) {
	return c.pages.Table(ctx)
}

// Address is the address of a page's published path in locale, as links
// are compared: "/en/about" for "/about", and "/en" for Home ("/").
func Address(locale, pagePath string) string {
	return path.Clean("/" + locale + "/" + strings.TrimPrefix(pagePath, "/"))
}

// pageURL is the address at which a page's path is served in locale:
// Address, but "/en/" for Home, as relative links in it lead from there.
func pageURL(locale, pagePath string) string {
	if pagePath == "/" {
		return "/" + locale + "/"
	}

	return Address(locale, pagePath)
}

// Within reports whether address is base or an address below it, such as
// "/en/about/team" below "/en/about".
func Within(address, base string) bool {
	return address == base || strings.HasPrefix(address, strings.TrimSuffix(base, "/")+"/")
}

// Index returns the links of the published content, made again when what
// visitors see has changed (see Options).
func (c *Check) Index(ctx context.Context) (*Index, error) {
	table, err := c.pages.Table(ctx)
	if err != nil {
		return nil, err
	}

	var version uint64
	if c.options.Version != nil {
		version = c.options.Version()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.index != nil && c.version == version && c.table == table && time.Now().Before(c.expires) {
		return c.index, nil
	}

	index, err := c.build(ctx, table)
	if err != nil {
		return nil, err
	}

	ttl := cmp.Or(c.options.TTL, defaultTTL)
	if ttl < 0 {
		return index, nil
	}

	c.index, c.version, c.table = index, version, table
	c.expires = time.Now().Add(ttl)

	return index, nil
}

// build reads the links of every published page and module source.
func (c *Check) build(ctx context.Context, table *pages.Table) (*Index, error) {
	config, err := c.locales.Configuration(ctx)
	if err != nil {
		return nil, err
	}

	index := &Index{}
	resolver := resolver{check: c, table: table, locales: config.Enabled}

	for _, locale := range config.Enabled {
		for _, route := range table.Routes(locale) {
			page, err := c.pages.Published(ctx, route)
			if err != nil {
				return nil, err
			}

			text, _ := page.Text(locale)
			source := &Source{
				ID:       page.ID,
				Title:    route.Title,
				URL:      pageURL(locale, route.Path),
				Markdown: []string{text.Body},
				Revision: route.RevisionID,
			}

			index.add(resolver, "", locale, source, c.texts.links(source, locale))
		}

		for _, module := range c.modules {
			sources, err := module.source(ctx, locale)
			if err != nil {
				// One module cannot break the check of the others.
				slog.ErrorContext(ctx, "links", "module", module.key, "locale", locale, "error", err)

				continue
			}

			for i := range sources {
				source := &sources[i]
				index.add(resolver, module.key, locale, source, c.texts.links(source, locale))
			}
		}
	}

	return index, nil
}

// Broken returns the links in published content to addresses the site does
// not answer: no page is there (see Link.Found), and no redirect leads from
// there to a page or another website.
func (c *Check) Broken(ctx context.Context) ([]Link, error) {
	index, err := c.Index(ctx)
	if err != nil {
		return nil, err
	}

	table, err := c.pages.Table(ctx)
	if err != nil {
		return nil, err
	}

	config, err := c.locales.Configuration(ctx)
	if err != nil {
		return nil, err
	}

	resolver := resolver{check: c, table: table, locales: config.Enabled}

	var broken []Link

	for _, link := range index.links {
		if link.Found || c.redirected(ctx, resolver, link.Address) {
			continue
		}

		broken = append(broken, link)
	}

	return broken, nil
}

// maxHops is how far Broken follows redirects.
const maxHops = 5

// redirected reports whether a redirect leads from address to a page or to
// another website, in at most maxHops steps.
func (c *Check) redirected(ctx context.Context, resolver resolver, address string) bool {
	if c.redirects == nil {
		return false
	}

	for range maxHops {
		target, ok := c.redirects.Redirected(ctx, address)
		if !ok {
			return false
		}

		next, local := resolver.address("/", target)
		if !local {
			return true
		}

		if _, found := resolver.lookup(next); found {
			return true
		}

		address = next
	}

	return false
}

// Index is the links of the published content at one moment (see
// Check.Index). It never changes.
type Index struct {
	links []Link
}

// Links returns every link, those of pages first, in tree order, then
// those of modules, per language: the Index's own list, to read and never
// to change.
func (i *Index) Links() []Link {
	return i.links
}

// To returns the links to one of addresses or below it, such as
// "/en/about" and "/en/about/team" for "/en/about", leaving out the links
// in content that is there itself (Source.URL), which leaves or moves with
// it.
func (i *Index) To(addresses ...string) []Link {
	var found []Link

	for _, link := range i.links {
		if !within(link.Address, addresses) || within(link.Source.URL, addresses) {
			continue
		}

		found = append(found, link)
	}

	return found
}

// within reports whether address is one of bases or below one of them.
func within(address string, bases []string) bool {
	if address == "" {
		return false
	}

	return slices.ContainsFunc(bases, func(base string) bool {
		return Within(address, base)
	})
}

// add indexes the links hrefs of source, in locale, made by module.
func (i *Index) add(resolver resolver, module, locale string, source *Source, hrefs []string) {
	base := cmp.Or(source.URL, pageURL(locale, "/"))

	for _, href := range hrefs {
		address, local := resolver.address(base, href)
		if !local {
			continue
		}

		link := Link{
			Module:  module,
			Locale:  locale,
			Source:  source,
			Href:    href,
			Address: address,
		}

		route, found := resolver.lookup(address)
		link.Page = route.PageID
		link.Found = found

		i.links = append(i.links, link)
	}
}

// resolver turns links into addresses on the site and finds the pages
// there, in one version of the published routes.
type resolver struct {
	check   *Check
	table   *pages.Table
	locales []string
}

// address resolves href from base, the address of the content it is in,
// as a browser does, and returns where it leads on the site; local is
// false for links that lead to no page of the site.
func (r resolver) address(base, href string) (string, bool) {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") {
		return "", false
	}

	reference, err := url.Parse(href)
	if err != nil {
		return "", false
	}

	switch {
	case reference.Scheme != "" && reference.Scheme != "http" && reference.Scheme != "https":
		// E-mail, phone and media links, among others.
		return "", false
	case reference.Host != "" && !strings.EqualFold(reference.Host, r.check.host):
		return "", false
	case reference.Scheme == "" && reference.Host == "" && reference.Path == "":
		// Only a query: the same page.
		return "", false
	}

	from, err := url.Parse(base)
	if err != nil {
		return "", false
	}

	address := path.Clean("/" + from.ResolveReference(reference).Path)

	// The site's root sends visitors to a language; the app's own
	// addresses are not pages.
	if address == "/" || r.check.options.IsCorePath != nil && r.check.options.IsCorePath(address) {
		return "", false
	}

	return address, true
}

// lookup finds the published route that answers at address; found also
// holds for the search screen, which no route has.
func (r resolver) lookup(address string) (pages.Route, bool) {
	segment, rest, _ := strings.Cut(strings.TrimPrefix(address, "/"), "/")
	if !slices.Contains(r.locales, segment) {
		return pages.Route{}, false
	}

	target := "/" + rest

	route, _, found := r.table.Lookup(segment, target)
	if found {
		return route, true
	}

	return pages.Route{}, target == frontend.SearchPath
}

// revisionLinks keeps the links found in the Markdown of revisions, which
// never change, per revision and language. It holds at most
// maxRevisionLinks revisions and starts over when full.
type revisionLinks struct {
	mu      sync.Mutex
	entries map[revisionKey][]string
}

type revisionKey struct {
	revision string
	locale   string
}

const maxRevisionLinks = 50000

// links returns the links of source in locale: those in its Markdown,
// found once per revision when it names one, then its Addresses.
func (c *revisionLinks) links(source *Source, locale string) []string {
	var found []string

	key := revisionKey{revision: source.Revision, locale: locale}

	c.mu.Lock()
	cached, ok := c.entries[key]
	c.mu.Unlock()

	if ok && source.Revision != "" {
		found = cached
	} else {
		for _, text := range source.Markdown {
			found = append(found, markdown.Links(text)...)
		}

		if source.Revision != "" {
			c.keep(key, found)
		}
	}

	return slices.Concat(found, source.Addresses)
}

// keep stores the links of a revision.
func (c *revisionLinks) keep(key revisionKey, found []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.entries == nil || len(c.entries) >= maxRevisionLinks {
		c.entries = map[revisionKey][]string{}
	}

	c.entries[key] = found
}
