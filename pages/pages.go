// Package pages owns the public page tree. Every page is revisioned content
// with a parent and a translated slug; its public path follows from the
// published tree. A module page (for example "Blog") hands its whole subtree
// to a module instance.
//
//	Home (the root page)      /
//	├── About                 /about
//	│   └── Team              /about/team
//	└── Blog [module: blog]   /blog, /blog/<anything below is the module's>
//
// The published routes are read on every public request, so the Service keeps
// them in memory with the page revisions they point to (revisions never
// change). Publishing in this process replaces them at once; changes by other
// processes show up within the cache's time to live (see SetCacheTTL).
package pages

import (
	"cmp"
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/internal/cache"
	"github.com/donseba/go-webthings/locales"
	"github.com/donseba/go-webthings/markdown"

	"github.com/donseba/go-router"
)

//go:embed schema/*.sql
var schemaFiles embed.FS

// Schema holds this package's schema files per dialect (see db.ApplySchema).
func Schema() fs.FS {
	files, _ := fs.Sub(schemaFiles, "schema")

	return files
}

const instance = "pages"

var (
	// ErrNotFound is content.ErrNotFound: a page that does not exist, is
	// in the trash, or is not published where that is asked.
	ErrNotFound = content.ErrNotFound
	// ErrInvalid is a page the tree cannot take as it is; ErrSlug,
	// ErrParent and ErrHome say what is wrong.
	ErrInvalid = errors.New("invalid_page")
	// ErrCycle refuses to place a page below itself or below one of its own
	// subpages.
	ErrCycle = errors.New("page_cycle")
	// ErrHasChildren refuses to delete a page that still has subpages: they
	// would lose their place in the tree.
	ErrHasChildren = errors.New("page_has_children")
	// ErrModuleParent refuses subpages below a module page, whose module
	// owns every address below it.
	ErrModuleParent = errors.New("module_page_cannot_have_children")
	// ErrParentDeleted keeps a page in the trash while its parent page is
	// in the trash too, or gone.
	ErrParentDeleted = errors.New("page_parent_deleted")
	// ErrDuplicate is a published slug that a sibling page has in the same
	// language, or a top-level slug the site uses itself, such as the
	// admin's.
	ErrDuplicate = errors.New("duplicate_page_slug")
	// ErrMounted refuses to publish a module page for a module that another
	// published page shows already: each module instance has one place.
	ErrMounted = errors.New("module_already_mounted")
	// ErrUnknownMount is a module page for a module the site does not use.
	ErrUnknownMount = errors.New("unknown_module")
	// ErrSlug refuses a slug that is not lowercase letters, digits and
	// dashes, or a missing slug when none can be made from the title.
	ErrSlug = fmt.Errorf("%w: slug", ErrInvalid)
	// ErrParent refuses a page other than Home without a parent page.
	ErrParent = fmt.Errorf("%w: parent", ErrInvalid)
	// ErrHome refuses to take the home page off the site: it cannot be
	// unpublished, scheduled to be unpublished or deleted.
	ErrHome = fmt.Errorf("%w: home", ErrInvalid)
)

// Fields place a page in the tree.
type Fields struct {
	ParentID string `db:"parent_id,null"`
	// Position orders the page among its siblings, lower first; siblings
	// with equal positions are ordered by ID, which follows the order they
	// were created in. It belongs to the page, not to a revision: Move
	// changes it at once, also in the published menu. Save places a new
	// page last among its siblings, and a page that changes parent last
	// among its new siblings, unless Position is set to another value than
	// the page has. A published page whose new parent is not published yet
	// keeps its place in the published menu until it is; then it moves to
	// its new place.
	Position int `db:"-"`
	// Module is the instance key of the mounted module, empty for content pages.
	Module string `db:"module"`
}

// Text is a page's translated content. Body is Markdown.
type Text struct {
	Slug        string `db:"slug"`
	Title       string `db:"title"`
	Description string `db:"description"`
	Body        string `db:"body"`
}

type (
	// Page is a page with its translations, as content stores keep items.
	Page = content.Item[Fields, Text]
	// Draft is a page as an editor saves it.
	Draft = content.Draft[Fields, Text]
	// Revision describes one saved version of a page.
	Revision = content.Revision
)

// Route is one published path. Module pages own every path below theirs.
type Route struct {
	Locale     string `db:"locale"`
	Path       string `db:"path"`
	PageID     string `db:"page_id"`
	RevisionID string `db:"revision_id"`
	ParentID   string `db:"parent_id"`
	Module     string `db:"module"`
	Position   int    `db:"position"`
	Depth      int    `db:"depth"`
	Title      string `db:"title"`
}

// Resolution is the published page behind a request path.
type Resolution struct {
	Page  Page
	Route Route
	// Rest is the path below a module page, "/" for the page itself.
	Rest string
}

// Service keeps the page tree: it saves, publishes and moves pages, and
// finds the published page behind a request path. It is safe for
// concurrent use.
type Service struct {
	db       *db.DB
	store    *content.Revisions[Fields, Text]
	locales  locales.Provider
	modules  func() []string
	reserved []string
	table    cache.Value[*Table]
}

// Table is one version of the published route table, indexed for each
// lookup. Publishing, unpublishing or moving a page, and the end of the
// cache's time to live (see SetCacheTTL), replace it with a new one, so
// what is made from the routes, such as a menu, can be kept alongside the
// Table it was made from and made again for another one. A Table never
// changes; nothing may change what its methods return either.
type Table struct {
	// ordered holds each locale's routes in tree order.
	ordered map[string][]Route
	// paths maps locale and path to a route.
	paths map[string]map[string]Route
	// alternates maps a page to its path in each locale.
	alternates map[string]map[string]string
	// mounts maps a module and locale to the path of its page.
	mounts map[string]map[string]string
	// revisions keeps the published page revisions loaded so far.
	revisions sync.Map
}

// SetCacheTTL sets how long the published routes are kept in memory (30
// seconds by default). Publishing in this process applies at once. Zero or
// less disables the cache.
func (s *Service) SetCacheTTL(ttl time.Duration) {
	s.table.SetTTL(ttl)
}

// Generation counts the changes to the published routes in this process:
// publishing, unpublishing or moving a page. It only goes up, so a cache
// of what is made from published pages, such as the app's output cache,
// keeps it with what it made and makes it again once it moved on. Changes
// by other processes show when the cache's time to live (see SetCacheTTL)
// has passed, without changing it.
func (s *Service) Generation() uint64 {
	return s.table.Generation()
}

// Table returns the published route table, the same one until the
// routes change (see Table), loading it when needed.
func (s *Service) Table(ctx context.Context) (*Table, error) {
	return s.table.Get(ctx, s.loadRoutes)
}

// Routes lists the published paths in a locale in tree order, as
// Service.Routes does, without copying them: the list is the Table's own,
// to read and never to change.
func (t *Table) Routes(locale string) []Route {
	return t.ordered[locale]
}

// Alternates maps each locale to the published path of a page, as
// Service.Alternates does, without copying: the map is the Table's own, to
// read and never to change; nil for a page that is not published.
func (t *Table) Alternates(pageID string) map[string]string {
	return t.alternates[pageID]
}

func (s *Service) loadRoutes(ctx context.Context) (*Table, error) {
	var routes []Route

	err := s.db.Select(ctx, &routes, `
		SELECT locale, path, page_id, revision_id, parent_id, module, position, depth, title
		FROM wt_page_route`,
	)
	if err != nil {
		return nil, err
	}

	routes = treeOrder(routes)

	table := &Table{
		ordered:    map[string][]Route{},
		paths:      map[string]map[string]Route{},
		alternates: map[string]map[string]string{},
		mounts:     map[string]map[string]string{},
	}

	for _, route := range routes {
		table.ordered[route.Locale] = append(table.ordered[route.Locale], route)

		if table.paths[route.Locale] == nil {
			table.paths[route.Locale] = map[string]Route{}
		}

		table.paths[route.Locale][route.Path] = route

		if table.alternates[route.PageID] == nil {
			table.alternates[route.PageID] = map[string]string{}
		}

		table.alternates[route.PageID][route.Locale] = route.Path

		if route.Module == "" {
			continue
		}

		if table.mounts[route.Module] == nil {
			table.mounts[route.Module] = map[string]string{}
		}

		table.mounts[route.Module][route.Locale] = route.Path
	}

	return table, nil
}

// treeOrder sorts routes per locale in tree order, like the admin's page
// tree: each page followed by its children, siblings in sibling order (see
// siblingOrder).
func treeOrder(routes []Route) []Route {
	groups := map[string][]Route{}

	for _, route := range routes {
		key := route.Locale + "\x00" + route.ParentID
		groups[key] = append(groups[key], route)
	}

	for _, group := range groups {
		slices.SortFunc(group, func(a, b Route) int {
			return siblingOrder(a.Position, b.Position, a.PageID, b.PageID)
		})
	}

	ordered := make([]Route, 0, len(routes))

	var walk func(locale, parent string)

	walk = func(locale, parent string) {
		for _, route := range groups[locale+"\x00"+parent] {
			ordered = append(ordered, route)
			walk(locale, route.PageID)
		}
	}

	var homes []Route

	for _, route := range routes {
		if route.Depth == 0 {
			homes = append(homes, route)
		}
	}

	slices.SortFunc(homes, func(a, b Route) int {
		return strings.Compare(a.Locale, b.Locale)
	})

	for _, home := range homes {
		ordered = append(ordered, home)
		walk(home.Locale, home.PageID)
	}

	return ordered
}

// published returns a published page revision, from the table when loaded
// before, with the page's current position.
func (s *Service) published(ctx context.Context, table *Table, route Route) (Page, error) {
	if page, ok := table.revisions.Load(route.RevisionID); ok {
		published := page.(Page)
		published.Fields.Position = route.Position

		return published, nil
	}

	page, err := s.store.Revision(ctx, instance, route.PageID, route.RevisionID)
	if err != nil {
		return Page{}, err
	}

	if !db.InTx(ctx) {
		table.revisions.Store(route.RevisionID, page)
	}

	page.Fields.Position = route.Position

	return page, nil
}

// New creates the service. modules lists the instance keys that may be mounted;
// reserved lists top-level slugs the application uses itself.
func New(database *db.DB, provider locales.Provider, modules func() []string, reserved ...string) (*Service, error) {
	store, err := content.NewRevisions(database, content.Spec[Fields, Text]{
		Table: "wt_page",
		Complete: func(text Text) bool {
			return text.Title != ""
		},
		Check: check,
		Media: func(_ Fields, translations []content.Translation[Text]) []string {
			var ids []string

			for _, translation := range translations {
				ids = append(ids, markdown.MediaIDs(translation.Text.Body)...)
			}

			return ids
		},
	})
	if err != nil {
		return nil, err
	}

	return &Service{
		db:       database,
		store:    store,
		locales:  provider,
		modules:  modules,
		reserved: reserved,
	}, nil
}

func check(fields Fields, translations []content.Translation[Text]) error {
	if fields.Position < 0 || fields.Position > 100000 {
		return ErrInvalid
	}

	for _, translation := range translations {
		text := translation.Text
		if len(text.Title) > 200 || len(text.Description) > 1000 || len(text.Body) > 256<<10 {
			return ErrInvalid
		}

		root := fields.ParentID == ""
		if root != (text.Slug == "") && text.Title != "" {
			// Home has no slug; every other titled translation needs one.
			return ErrSlug
		}

		if text.Slug != "" && !validSlug(text.Slug) {
			return ErrSlug
		}
	}

	return nil
}

func validSlug(slug string) bool {
	for _, r := range slug {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}

	return !strings.HasPrefix(slug, "-") && !strings.HasSuffix(slug, "-")
}

// Home returns the ID of the root page, which is served at "/".
func (s *Service) Home(ctx context.Context) (string, error) {
	var id string

	// Only the root page ever has revisions without a parent.
	err := s.db.Get(ctx, &id, `SELECT entity_id FROM wt_page_revision WHERE parent_id IS NULL ORDER BY created_at LIMIT 1`)
	if errors.Is(err, db.ErrNotFound) {
		return "", ErrNotFound
	}

	return id, err
}

// Init creates and publishes the Home page when it does not exist yet.
// titles gives the Home title per locale; "Home" is used otherwise.
func (s *Service) Init(ctx context.Context, titles map[string]string) error {
	if _, err := s.Home(ctx); !errors.Is(err, ErrNotFound) {
		return err
	}

	config, err := s.locales.Configuration(ctx)
	if err != nil {
		return err
	}

	var translations []content.Translation[Text]

	for _, locale := range config.Enabled {
		title := titles[locale]
		if title == "" {
			title = "Home"
		}

		translations = append(translations, content.Translation[Text]{Locale: locale, Text: Text{Title: title}})
	}

	home, err := s.Save(ctx, Draft{Translations: translations, Note: "Initial home page"})
	if err != nil {
		return err
	}

	return s.Publish(ctx, home.ID, home.Revision.ID)
}

// Content returns the revision store behind the pages, for code that needs
// its rules, such as which translations are complete (Spec). Changes to
// the tree go through the Service, which keeps the tree valid.
func (s *Service) Content() *content.Revisions[Fields, Text] {
	return s.store
}

// Latest returns the newest revision of a page, published or not, as the
// admin edits it. It returns ErrNotFound for a page that does not exist or
// is in the trash.
func (s *Service) Latest(ctx context.Context, id string) (Page, error) {
	page, err := s.store.Latest(ctx, instance, id)
	if err != nil {
		return Page{}, err
	}

	return s.place(ctx, page)
}

// Revision returns one revision of a page, such as an older one shown in
// its history.
func (s *Service) Revision(ctx context.Context, id, revisionID string) (Page, error) {
	page, err := s.store.Revision(ctx, instance, id, revisionID)
	if err != nil {
		return Page{}, err
	}

	return s.place(ctx, page)
}

// History lists the revisions of a page, newest first.
func (s *Service) History(ctx context.Context, id string) ([]Revision, error) {
	return s.store.History(ctx, instance, id)
}

// ListLatest returns the latest revision of every page, newest page first;
// Tree returns them in tree order.
func (s *Service) ListLatest(ctx context.Context) ([]Page, error) {
	latest, err := s.store.ListLatest(ctx, instance)
	if err != nil {
		return nil, err
	}

	return s.placeAll(ctx, latest)
}

// outline returns the latest revision of every page like ListLatest, with
// only the slugs and titles of the translations: checking and changing the
// tree needs no descriptions and bodies, which can be long.
func (s *Service) outline(ctx context.Context) ([]Page, error) {
	latest, err := s.store.Outline(ctx, instance, "slug", "title")
	if err != nil {
		return nil, err
	}

	return s.placeAll(ctx, latest)
}

// placement is a page's position among its siblings.
type placement struct {
	ID       string `db:"id"`
	Position int    `db:"position"`
}

// place fills in the page's position, which is not part of its revisions.
func (s *Service) place(ctx context.Context, page Page) (Page, error) {
	err := s.db.Get(ctx, &page.Fields.Position, `SELECT position FROM wt_page WHERE id=?`, page.ID)
	if err != nil {
		return Page{}, err
	}

	return page, nil
}

// placeAll fills in the positions of pages.
func (s *Service) placeAll(ctx context.Context, list []Page) ([]Page, error) {
	var placements []placement

	err := s.db.Select(ctx, &placements, `SELECT id, position FROM wt_page WHERE instance=?`, instance)
	if err != nil {
		return nil, err
	}

	positions := map[string]int{}

	for _, placement := range placements {
		positions[placement.ID] = placement.Position
	}

	for i := range list {
		list[i].Fields.Position = positions[list[i].ID]
	}

	return list, nil
}

// Save stores a new revision after checking its place in the tree. The root
// page stays the root; every other page needs a parent. A translation with a
// title and without a slug gets one made from the title, unique among the
// page's siblings in the latest tree.
func (s *Service) Save(ctx context.Context, draft Draft) (Page, error) {
	home, err := s.Home(ctx)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Page{}, err
	}

	switch {
	case home == "" || draft.ID != "" && draft.ID == home:
		draft.Fields = Fields{}
	case draft.Fields.ParentID == "":
		return Page{}, ErrParent
	}

	var saved Page

	err = s.db.Tx(ctx, func(ctx context.Context) error {
		latest, err := s.outline(ctx)
		if err != nil {
			return err
		}

		if err := s.checkTree(draft, latest); err != nil {
			return err
		}

		draft.Translations = s.slugs(draft, latest, home)

		saved, err = s.store.Save(ctx, instance, draft)
		if err != nil {
			return err
		}

		saved.Fields.Position, err = s.placeDraft(ctx, draft, saved.ID, latest)

		return err
	})
	if err != nil {
		return Page{}, err
	}

	return saved, nil
}

// livePlace is where the published revision of a page is in the menu.
type livePlace struct {
	// RevisionID is the published revision, nil for an unpublished page.
	RevisionID *string `db:"published_revision_id"`
	// ParentID is the published revision's parent, nil for Home.
	ParentID *string `db:"parent_id"`
	// Position is the published place while the latest revision has another
	// parent; nil when the page's position is its place in the menu too.
	Position *int `db:"published_position"`
}

func (s *Service) livePlace(ctx context.Context, id string) (livePlace, error) {
	var place livePlace

	err := s.db.Get(ctx, &place, `
		SELECT p.published_revision_id, r.parent_id, p.published_position
		FROM wt_page p
		LEFT JOIN wt_page_revision r ON r.id=p.published_revision_id
		WHERE p.id=?`, id)

	return place, err
}

// parent is the published parent, "" for Home and unpublished pages.
func (p livePlace) parent() string {
	if p.ParentID == nil {
		return ""
	}

	return *p.ParentID
}

// placeDraft stores the position of a page whose latest revision is now
// draft, with latest the tree before it was saved, and returns it.
//
// A page with a new parent goes last among its new siblings, also in the
// admin's tree, but while its published revision has the old parent, the
// menu keeps it where it was (published_position) until the parent change
// is published. Other changes of place show in the menu at once.
func (s *Service) placeDraft(ctx context.Context, draft Draft, id string, latest []Page) (int, error) {
	place, err := s.livePlace(ctx, id)
	if err != nil {
		return 0, err
	}

	previous := -1

	for _, page := range latest {
		if page.ID == id {
			previous = page.Fields.Position
		}
	}

	published := place.RevisionID != nil
	newPosition := position(draft, latest)
	explicit := draft.Fields.Position > 0 && draft.Fields.Position != previous

	// Back under the published parent, the page returns to its place in the
	// menu.
	if published && place.Position != nil && draft.Fields.ParentID == place.parent() && !explicit {
		newPosition = *place.Position
	}

	var pending *int

	if published && draft.Fields.ParentID != place.parent() {
		pending = place.Position
		if pending == nil {
			pending = &previous
		}
	}

	live := func(position int, pending *int) int {
		if pending != nil {
			return *pending
		}

		return position
	}

	before := live(previous, place.Position)
	after := live(newPosition, pending)

	if newPosition == previous && equalPositions(pending, place.Position) {
		return newPosition, nil
	}

	_, err = s.db.Exec(ctx, `UPDATE wt_page SET position=?, published_position=? WHERE id=?`, newPosition, pending, id)
	if err != nil {
		return 0, err
	}

	// A new place in the published menu shows at once.
	if !published || before == after {
		return newPosition, nil
	}

	defer s.table.Invalidate()

	return newPosition, s.rebuild(ctx)
}

func equalPositions(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}

	return *a == *b
}

// settlePublished keeps the published place of a page in step with the
// revision just published or unpublished: once the published revision has
// the latest revision's parent, the page's position is its place in the menu
// too. A published revision with another parent than the latest one, such
// as an older revision published again, keeps its place in the menu or, new
// under that parent, goes last there.
func (s *Service) settlePublished(ctx context.Context, id string) error {
	place, err := s.livePlace(ctx, id)
	if err != nil {
		return err
	}

	latest, err := s.store.Latest(ctx, instance, id)
	if err != nil {
		return err
	}

	if place.RevisionID == nil || place.parent() == latest.Fields.ParentID {
		if place.Position == nil {
			return nil
		}

		_, err := s.db.Exec(ctx, `UPDATE wt_page SET published_position=NULL WHERE id=?`, id)

		return err
	}

	if place.Position != nil {
		return nil
	}

	var last int

	err = s.db.Get(ctx, &last, `
		SELECT COALESCE(MAX(COALESCE(p.published_position, p.position)), 0)
		FROM wt_page p
		JOIN wt_page_revision r ON r.id=p.published_revision_id
		WHERE p.instance=? AND p.id<>? AND p.deleted_at IS NULL AND r.parent_id=?`, instance, id, place.parent())
	if err != nil {
		return err
	}

	_, err = s.db.Exec(ctx, `UPDATE wt_page SET published_position=? WHERE id=?`, last+1, id)

	return err
}

// slugs gives titled translations without a slug one made from the title,
// unique among the page's siblings in that language; top-level pages also
// avoid the reserved slugs. Home keeps no slug.
func (s *Service) slugs(draft Draft, latest []Page, home string) []content.Translation[Text] {
	translations := slices.Clone(draft.Translations)

	if draft.Fields.ParentID == "" {
		return translations
	}

	for i, translation := range translations {
		text := translation.Text
		text.Slug = strings.TrimSpace(text.Slug)

		if text.Slug == "" && text.Title != "" {
			text.Slug = s.uniqueSlug(content.Slug(text.Title), draft, latest, home, translation.Locale)
		}

		translations[i].Text = text
	}

	return translations
}

// uniqueSlug returns base, or base with the first free number appended, such
// as "contact-2", when a sibling already uses it.
func (s *Service) uniqueSlug(base string, draft Draft, latest []Page, home, locale string) string {
	if base == "" {
		return ""
	}

	taken := map[string]bool{}

	if draft.Fields.ParentID == home {
		for _, reserved := range s.reserved {
			taken[reserved] = true
		}
	}

	for _, page := range latest {
		if page.ID == draft.ID || page.Fields.ParentID != draft.Fields.ParentID {
			continue
		}

		if text, ok := page.Text(locale); ok {
			taken[text.Slug] = true
		}
	}

	return content.UniqueSlug(base, func(slug string) bool {
		return taken[slug]
	})
}

// position is where a saved page goes among its siblings: an explicitly
// changed Position, the page's current place, or last among the siblings of
// a new page and of a page with a new parent. Home has no siblings.
func position(draft Draft, latest []Page) int {
	if draft.Fields.ParentID == "" {
		return 0
	}

	last := 0

	for _, page := range latest {
		if page.ID == draft.ID {
			if draft.Fields.Position > 0 && draft.Fields.Position != page.Fields.Position {
				return draft.Fields.Position
			}

			if page.Fields.ParentID == draft.Fields.ParentID {
				return page.Fields.Position
			}

			continue
		}

		if page.Fields.ParentID == draft.Fields.ParentID {
			last = max(last, page.Fields.Position)
		}
	}

	if draft.ID == "" && draft.Fields.Position > 0 {
		return draft.Fields.Position
	}

	return last + 1
}

// Move moves a page offset places among its siblings in the latest tree:
// -1 is one place up, 1 one place down; moves past the first or last place
// stop there. The order is not revisioned, so the published menu follows at
// once. Home has no siblings and does not move.
func (s *Service) Move(ctx context.Context, id string, offset int) error {
	defer s.table.Invalidate()

	return s.db.Tx(ctx, func(ctx context.Context) error {
		latest, err := s.outline(ctx)
		if err != nil {
			return err
		}

		index := slices.IndexFunc(latest, func(page Page) bool {
			return page.ID == id
		})
		if index < 0 {
			return ErrNotFound
		}

		parent := latest[index].Fields.ParentID
		if parent == "" {
			return nil
		}

		siblings := children(latest)[parent]
		from := slices.IndexFunc(siblings, func(page Page) bool {
			return page.ID == id
		})

		return s.renumber(ctx, siblings, id, max(from+offset, 0))
	})
}

// children groups pages by parent ID, each group in sibling order (see
// siblingOrder).
func children(list []Page) map[string][]Page {
	groups := map[string][]Page{}

	for _, page := range list {
		groups[page.Fields.ParentID] = append(groups[page.Fields.ParentID], page)
	}

	for _, group := range groups {
		slices.SortFunc(group, func(a, b Page) int {
			return siblingOrder(a.Fields.Position, b.Fields.Position, a.ID, b.ID)
		})
	}

	return groups
}

// siblingOrder orders pages among their siblings, the same in the admin's
// tree, for Move and in the published menu (Routes): by position, then
// pages with equal positions by ID, which follows the order the pages were
// created in (IDs are UUIDv7) and is the same in every language.
func siblingOrder(firstPosition, secondPosition int, firstID, secondID string) int {
	if order := cmp.Compare(firstPosition, secondPosition); order != 0 {
		return order
	}

	return strings.Compare(firstID, secondID)
}

// Node is a page in the latest tree.
type Node struct {
	Page Page
	// Depth is 0 for Home, 1 for its children and so on.
	Depth int
	// Path is the page's path in the requested language from the latest
	// slugs, such as "/about/team"; empty when the page or a page above it
	// has no slug in that language.
	Path string
	// First and Last tell whether the page can move up or down among its
	// siblings.
	First bool
	Last  bool
}

// Tree returns the latest revision of every page in tree order: Home first,
// then each page followed by its children, siblings in menu order (by
// position, then by ID; see siblingOrder). Pages whose parent no longer
// exists follow at the end. locale chooses the slugs of Node.Path. The
// translations of Node.Page hold only slugs and titles; Latest loads a
// page in full.
func (s *Service) Tree(ctx context.Context, locale string) ([]Node, error) {
	latest, err := s.outline(ctx)
	if err != nil {
		return nil, err
	}

	groups := children(latest)
	seen := map[string]bool{}

	var nodes []Node

	var walk func(siblings []Page, depth int, path string)

	walk = func(siblings []Page, depth int, path string) {
		for i, page := range siblings {
			if seen[page.ID] {
				continue
			}

			seen[page.ID] = true
			text, _ := page.Text(locale)

			pagePath := "/"
			if depth > 0 {
				pagePath = ""
			}

			if depth > 0 && path != "" && text.Slug != "" {
				pagePath = strings.TrimSuffix(path, "/") + "/" + text.Slug
			}

			nodes = append(nodes, Node{
				Page:  page,
				Depth: depth,
				Path:  pagePath,
				First: i == 0,
				Last:  i == len(siblings)-1,
			})

			walk(groups[page.ID], depth+1, pagePath)
		}
	}

	walk(groups[""], 0, "")

	for _, page := range latest {
		if !seen[page.ID] {
			walk([]Page{page}, 1, "")
		}
	}

	return nodes, nil
}

// checkTree rejects unknown modules, cycles and children below module pages.
func (s *Service) checkTree(draft Draft, latest []Page) error {
	if draft.Fields.Module != "" && !slices.Contains(s.modules(), draft.Fields.Module) {
		return ErrUnknownMount
	}

	tree := map[string]Fields{}
	for _, page := range latest {
		tree[page.ID] = page.Fields
	}

	if draft.ID != "" {
		tree[draft.ID] = draft.Fields
	}

	if draft.Fields.Module != "" {
		for _, fields := range tree {
			if fields.ParentID == draft.ID && draft.ID != "" {
				return ErrModuleParent
			}
		}
	}

	for current, steps := draft.Fields.ParentID, 0; current != ""; steps++ {
		parent, ok := tree[current]
		if !ok {
			return ErrNotFound
		}

		if current == draft.ID || steps > 32 {
			return ErrCycle
		}

		if parent.Module != "" {
			return ErrModuleParent
		}

		current = parent.ParentID
	}

	return nil
}

// Publish makes a revision public and rebuilds the published routes. It
// cancels the page's planned publication changes, like Unpublish.
func (s *Service) Publish(ctx context.Context, id, revisionID string) error {
	config, err := s.locales.Configuration(ctx)
	if err != nil {
		return err
	}

	defer s.table.Invalidate()

	return s.db.Tx(ctx, func(ctx context.Context) error {
		if err := s.store.Publish(ctx, instance, id, revisionID, config.Default); err != nil {
			return err
		}

		if err := s.settlePublished(ctx, id); err != nil {
			return err
		}

		return s.rebuild(ctx)
	})
}

// Unpublish takes a page off the public site; its revisions stay, so it
// can be published again. The home page cannot be unpublished (ErrHome).
func (s *Service) Unpublish(ctx context.Context, id string) error {
	if err := s.notHome(ctx, id); err != nil {
		return err
	}

	defer s.table.Invalidate()

	return s.db.Tx(ctx, func(ctx context.Context) error {
		if err := s.store.Unpublish(ctx, instance, id); err != nil {
			return err
		}

		if err := s.settlePublished(ctx, id); err != nil {
			return err
		}

		return s.rebuild(ctx)
	})
}

// Schedule plans the publication of a page revision and/or the page's
// unpublication (see content.Schedule).
func (s *Service) Schedule(ctx context.Context, id string, schedule content.Schedule) error {
	config, err := s.locales.Configuration(ctx)
	if err != nil {
		return err
	}

	if !schedule.UnpublishAt.IsZero() {
		if err := s.notHome(ctx, id); err != nil {
			return err
		}
	}

	return s.store.Schedule(ctx, instance, id, schedule, config.Default)
}

// Unschedule cancels a page's planned publication change.
func (s *Service) Unschedule(ctx context.Context, id string) error {
	return s.store.Unschedule(ctx, instance, id)
}

// RunDue publishes and unpublishes the pages whose time has come, rebuilding
// the routes like Publish and Unpublish do; see content.Scheduler.
func (s *Service) RunDue(ctx context.Context, now time.Time, required string) (int, error) {
	// Changes commit inside RunDue's transactions, after Publish invalidated.
	defer s.table.Invalidate()

	return s.store.RunDue(ctx, instance, now, required, content.Publisher{
		Publish: func(ctx context.Context, id, revisionID, _ string) error {
			return s.Publish(ctx, id, revisionID)
		},
		Unpublish: s.Unpublish,
	})
}

// Restore copies an older revision of a page into a new latest revision
// by author, as if it had been saved again. It does not publish it. The
// page keeps its current place among its siblings, and the old revision's
// parent must still be a valid place.
func (s *Service) Restore(ctx context.Context, id, revisionID, author string) (Page, error) {
	old, err := s.store.Revision(ctx, instance, id, revisionID)
	if err != nil {
		return Page{}, err
	}

	var restored Page

	err = s.db.Tx(ctx, func(ctx context.Context) error {
		latest, err := s.outline(ctx)
		if err != nil {
			return err
		}

		draft := Draft{ID: id, Fields: old.Fields}
		draft.Fields.Position = 0

		if err := s.checkTree(draft, latest); err != nil {
			return err
		}

		restored, err = s.store.Restore(ctx, instance, id, revisionID, author)
		if err != nil {
			return err
		}

		// The restored revision is placed like a saved one.
		restored.Fields.Position, err = s.placeDraft(ctx, draft, id, latest)

		return err
	})
	if err != nil {
		return Page{}, err
	}

	return restored, nil
}

// notHome rejects changes that would remove the root page from the site.
func (s *Service) notHome(ctx context.Context, id string) error {
	home, err := s.Home(ctx)
	if err != nil {
		return err
	}

	if id == home {
		return ErrHome
	}

	return nil
}

// Delete moves a page without children to the trash; it leaves the menu
// and its address at once. Home cannot be deleted.
func (s *Service) Delete(ctx context.Context, id string) error {
	if err := s.notHome(ctx, id); err != nil {
		return err
	}

	defer s.table.Invalidate()

	return s.db.Tx(ctx, func(ctx context.Context) error {
		latest, err := s.store.Outline(ctx, instance)
		if err != nil {
			return err
		}

		for _, page := range latest {
			if page.Fields.ParentID == id {
				return ErrHasChildren
			}
		}

		if err := s.store.Delete(ctx, instance, id); err != nil {
			return err
		}

		return s.rebuild(ctx)
	})
}

// Undelete brings a page back from the trash, at its place in the tree.
// Its parent must be there (ErrParentDeleted); a published page must not
// take a slug its siblings use now (ErrDuplicate) nor a module placed on
// another page meanwhile (ErrMounted).
func (s *Service) Undelete(ctx context.Context, id string) error {
	defer s.table.Invalidate()

	return s.db.Tx(ctx, func(ctx context.Context) error {
		trashed, err := s.store.Trashed(ctx, instance)
		if err != nil {
			return err
		}

		at := slices.IndexFunc(trashed, func(page Page) bool {
			return page.ID == id
		})
		if at < 0 {
			return ErrNotFound
		}

		latest, err := s.outline(ctx)
		if err != nil {
			return err
		}

		err = s.checkTree(Draft{ID: id, Fields: trashed[at].Fields}, latest)
		if errors.Is(err, ErrNotFound) {
			return ErrParentDeleted
		}

		if err != nil {
			return err
		}

		if err := s.store.Undelete(ctx, instance, id); err != nil {
			return err
		}

		return s.rebuild(ctx)
	})
}

// Purge removes a page in the trash for good, with its revisions.
func (s *Service) Purge(ctx context.Context, id string) error {
	return s.store.Purge(ctx, instance, id)
}

// Trashed returns the latest revision of the pages in the trash, most
// recently deleted first.
func (s *Service) Trashed(ctx context.Context) ([]Page, error) {
	return s.store.Trashed(ctx, instance)
}

// Recent returns the latest revision of at most limit pages, the most
// recently changed first.
func (s *Service) Recent(ctx context.Context, limit int) ([]Page, error) {
	return s.store.Recent(ctx, instance, limit)
}

// Planned returns the latest revision of the pages with a planned
// publication change.
func (s *Service) Planned(ctx context.Context) ([]Page, error) {
	return s.store.Planned(ctx, instance)
}

// rebuild recomputes every published path from the published tree.
func (s *Service) rebuild(ctx context.Context) error {
	published, err := s.store.AllPublished(ctx, instance)
	if err != nil {
		return err
	}

	var placements []placement

	// A page whose parent change is not published yet keeps its published
	// place in the menu.
	err = s.db.Select(ctx, &placements, `
		SELECT id, COALESCE(published_position, position) AS position
		FROM wt_page WHERE instance=?`, instance)
	if err != nil {
		return err
	}

	positions := map[string]int{}

	for _, placement := range placements {
		positions[placement.ID] = placement.Position
	}

	for i := range published {
		published[i].Fields.Position = positions[published[i].ID]
	}

	routes, err := s.routes(published)
	if err != nil {
		return err
	}

	if _, err := s.db.Exec(ctx, `DELETE FROM wt_page_route`); err != nil {
		return err
	}

	for _, route := range routes {
		_, err := s.db.Exec(ctx, `
			INSERT INTO wt_page_route(locale,path,page_id,revision_id,parent_id,module,position,depth,title)
			VALUES(?,?,?,?,?,?,?,?,?)`,
			route.Locale, route.Path, route.PageID, route.RevisionID, route.ParentID,
			route.Module, route.Position, route.Depth, route.Title,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// routes walks the published tree per locale. A page is reachable in a locale
// only when it and all its ancestors are published and translated there.
func (s *Service) routes(published []Page) ([]Route, error) {
	children := map[string][]Page{}
	mounted := map[string]string{}

	var home *Page

	for i, page := range published {
		if page.Fields.ParentID == "" {
			home = &published[i]

			continue
		}

		if owner, ok := mounted[page.Fields.Module]; ok && page.Fields.Module != "" && owner != page.ID {
			return nil, ErrMounted
		}

		if page.Fields.Module != "" {
			mounted[page.Fields.Module] = page.ID
		}

		children[page.Fields.ParentID] = append(children[page.Fields.ParentID], page)
	}

	if home == nil {
		return nil, nil
	}

	var routes []Route

	var walk func(page Page, locale, path string, depth int) error

	walk = func(page Page, locale, path string, depth int) error {
		text, ok := page.Text(locale)
		if !ok || text.Title == "" {
			return nil
		}

		if depth > 32 {
			return ErrCycle
		}

		routes = append(routes, Route{
			Locale:     locale,
			Path:       path,
			PageID:     page.ID,
			RevisionID: page.Revision.ID,
			ParentID:   page.Fields.ParentID,
			Module:     page.Fields.Module,
			Position:   page.Fields.Position,
			Depth:      depth,
			Title:      text.Title,
		})

		seen := map[string]bool{}

		for _, child := range children[page.ID] {
			childText, ok := child.Text(locale)
			if !ok || childText.Title == "" {
				continue
			}

			if page.Fields.Module != "" {
				return ErrModuleParent
			}

			if seen[childText.Slug] || depth == 0 && slices.Contains(s.reserved, childText.Slug) {
				return ErrDuplicate
			}

			seen[childText.Slug] = true

			if err := walk(child, locale, strings.TrimSuffix(path, "/")+"/"+childText.Slug, depth+1); err != nil {
				return err
			}
		}

		return nil
	}

	for _, translation := range home.Translations {
		if err := walk(*home, translation.Locale, "/", 0); err != nil {
			return nil, err
		}
	}

	return routes, nil
}

// Resolve finds the published page for a path. Paths below a module page
// resolve to that page with the remainder in Rest.
func (s *Service) Resolve(ctx context.Context, locale, path string) (Resolution, error) {
	table, err := s.Table(ctx)
	if err != nil {
		return Resolution{}, err
	}

	route, rest, ok := table.Lookup(locale, path)
	if !ok {
		return Resolution{}, ErrNotFound
	}

	page, err := s.published(ctx, table, route)
	if err != nil {
		return Resolution{}, err
	}

	return Resolution{Page: page, Route: route, Rest: rest}, nil
}

// Lookup finds the published route of a path in locale, as Resolve does,
// without loading the page: the page at the path, or the module page above
// it with the path below that page in rest ("/" for the page itself).
func (t *Table) Lookup(locale, path string) (route Route, rest string, ok bool) {
	candidates := []string{"/"}

	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i := range parts {
		if parts[0] != "" {
			candidates = append(candidates, "/"+strings.Join(parts[:i+1], "/"))
		}
	}

	exact := "/" + strings.Trim(path, "/")

	var (
		best  Route
		found bool
	)

	for _, candidate := range candidates {
		at, ok := t.paths[locale][candidate]
		if !ok {
			continue
		}

		matches := at.Path == exact || at.Module != ""
		if matches && (!found || len(at.Path) > len(best.Path)) {
			best, found = at, true
		}
	}

	if !found {
		return Route{}, "", false
	}

	rest = strings.TrimPrefix(exact, strings.TrimSuffix(best.Path, "/"))
	if rest == "" {
		rest = "/"
	}

	return best, rest, true
}

// Routes lists all published paths in a locale in tree order, for
// navigation: a copy, which the caller may change (see Table.Routes).
func (s *Service) Routes(ctx context.Context, locale string) ([]Route, error) {
	table, err := s.Table(ctx)
	if err != nil {
		return nil, err
	}

	return slices.Clone(table.ordered[locale]), nil
}

// Published returns the published revision a route points to, for example
// to search the text of published pages.
func (s *Service) Published(ctx context.Context, route Route) (Page, error) {
	table, err := s.Table(ctx)
	if err != nil {
		return Page{}, err
	}

	return s.published(ctx, table, route)
}

// Alternates maps each locale to the published path of the same page.
func (s *Service) Alternates(ctx context.Context, pageID string) (map[string]string, error) {
	table, err := s.Table(ctx)
	if err != nil {
		return nil, err
	}

	paths := maps.Clone(table.alternates[pageID])
	if paths == nil {
		paths = map[string]string{}
	}

	return paths, nil
}

// MountPath is the published path of a module instance in locale.
func (s *Service) MountPath(ctx context.Context, module, locale string) (string, error) {
	table, err := s.Table(ctx)
	if err != nil {
		return "", err
	}

	path, ok := table.mounts[module][locale]
	if !ok {
		return "", ErrNotFound
	}

	return path, nil
}

// MountURL is the address of the page a module instance is placed on in
// locale, with the language prefix, such as "/en/news". Home never holds a
// module (Save clears it), so the address always has a path after the
// prefix. It is "" without an error while the instance is on no published
// page in locale, so modules can leave their links empty. A module gives the
// addresses of its routes below it with Links.
func (s *Service) MountURL(ctx context.Context, module, locale string) (string, error) {
	path, err := s.MountPath(ctx, module, locale)
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}

	if err != nil {
		return "", err
	}

	return joinURL("/"+locale+"/", path), nil
}

// Links gives the addresses of a module's routes on the page it is placed
// on, for links built outside its own handlers, such as in a block on
// another page or a search index. base is the page's MountURL: the paths
// are those of routes.At(base), and NoLinks while base is "".
//
//	base, err := s.pages.MountURL(ctx, s.key, locale)
//	...
//	paths := pages.Links(s.public, base)
//	url, _ := paths("post", "slug", "hello") // "/en/news/hello"
func Links(routes *router.Router, base string) router.PathFunc {
	if base == "" {
		return NoLinks
	}

	return routes.At(base).RoutePathArgs
}

// NoLinks is a router.PathFunc that gives every route the empty address,
// for an instance on no page (MountURL is ""): its items are still listed,
// without links to pages that do not exist.
func NoLinks(string, ...any) (string, error) {
	return "", nil
}

// joinURL appends relative to base with one slash between them, whether or
// not base ends with a slash: "/en/news" and "news-item" give
// "/en/news/news-item", and so does "/en/news/". An empty base, such as the
// MountURL of an instance on no page, stays empty, and an empty relative
// returns base unchanged.
func joinURL(base, relative string) string {
	relative = strings.Trim(relative, "/")

	switch {
	case base == "":
		return ""
	case relative == "":
		return base
	}

	return strings.TrimSuffix(base, "/") + "/" + relative
}
