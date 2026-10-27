// Package people presents the people behind an organisation: staff, a team,
// a board or volunteers. Each person has a name, photo, contact details and
// links, and per language a group (such as "Board" or "Staff"), a role and a
// Markdown biography. People are kept in a manually ordered list and
// published one by one. Add an instance with:
//
//	app.Use(people.Module("team", "Team"))
//
// When the module is mounted on a page it serves the list at "/" and one
// person at "/{slug}". The slug is made from the name when the editor leaves
// it empty and is unique per instance. The website renders:
//
//	people/list.gohtml    ListView: everyone, also grouped by Group
//	people/person.gohtml  Person: one person
//
// Templates anywhere on the site can show people without Go code:
//
//	{{range people "team"}}<a href="{{.URL}}">{{.Name}}</a>{{end}}
//	{{range peopleIn "team" "Board"}}{{.Name}}, {{.Role}}{{end}}
//	{{with person "team" "jane-doe"}}<img src="{{.PhotoURL}}" alt="{{.Name}}">{{end}}
//
// The site search finds published people by name, role, group and biography
// once the module is on a page. Group names compare case-insensitively in the
// current language. Person.URL is empty while the module is not mounted on a
// page. Use Person.PhoneURL for
// tel: links, because html/template blocks the tel: scheme in plain strings.
package people

import (
	"context"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"regexp"
	"strings"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/mail"
	"github.com/donseba/go-webthings/markdown"
	"github.com/donseba/go-webthings/pages"

	form "github.com/donseba/go-form/v2"
	"github.com/donseba/go-router"
)

//go:embed schema/*.sql
var schemaFiles embed.FS

// Fields are the language-independent parts of a person.
type Fields struct {
	Slug     string `db:"slug"`
	Name     string `db:"name"`
	Photo    string `db:"photo_id,null"`
	Email    string `db:"email"`
	Phone    string `db:"phone"`
	Website  string `db:"website"`
	LinkedIn string `db:"linkedin"`
}

// Text is a person in one language. Bio is Markdown.
type Text struct {
	Group string `db:"group_name"`
	Role  string `db:"role_title"`
	Bio   string `db:"bio"`
}

// Item is a person with their translations, as the store keeps it.
type Item = content.Item[Fields, Text]

var (
	errSlug  = errors.New("people_invalid_slug")
	errEmail = errors.New("people_invalid_email")
	errPhone = errors.New("people_invalid_phone")
	errURL   = errors.New("people_invalid_url")
)

var (
	slugPattern  = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	phonePattern = regexp.MustCompile(`^\+?[0-9 ()./-]{3,50}$`)
)

func spec() content.Spec[Fields, Text] {
	return content.Spec[Fields, Text]{
		Table: "wt_person",
		// A person needs nothing translated: a written translation is enough
		// to show them in that language.
		Complete: func(Text) bool {
			return true
		},
		Check: check,
		Media: func(fields Fields, translations []content.Translation[Text]) []string {
			ids := []string{fields.Photo}

			for _, translation := range translations {
				ids = append(ids, markdown.MediaIDs(translation.Text.Bio)...)
			}

			return ids
		},
	}
}

func check(fields Fields, translations []content.Translation[Text]) error {
	if len(fields.Slug) > 190 || !slugPattern.MatchString(fields.Slug) {
		return errSlug
	}

	if fields.Name == "" {
		return content.ErrInvalid
	}

	if content.TooLong(200, fields.Name) || content.TooLong(254, fields.Email) {
		return content.ErrTooLong
	}

	if fields.Email != "" && !mail.Valid(fields.Email) {
		return errEmail
	}

	if fields.Phone != "" && !phonePattern.MatchString(fields.Phone) {
		return errPhone
	}

	if !validURL(fields.Website) || !validURL(fields.LinkedIn) {
		return errURL
	}

	for _, translation := range translations {
		text := translation.Text

		if content.TooLong(200, text.Group, text.Role) || content.TooLong(256<<10, text.Bio) {
			return content.ErrTooLong
		}
	}

	return nil
}

// validURL accepts an empty value or an absolute http(s) address of at most
// 500 bytes.
func validURL(value string) bool {
	if value == "" {
		return true
	}

	return len(value) <= 500 && form.ValidURL("http,https", value)
}

// Service is one People instance, for the website's own code.
type Service struct {
	key      string
	items    *content.Collection[Fields, Text]
	frontend *frontend.Frontend
	pages    *pages.Service
	// adminURL builds admin addresses by route name (webthings.Env.AdminURL).
	adminURL func(name string, args ...any) string
	// public are the routes below the module's page (see handler), api
	// those of its endpoints in the content API (see apiRoutes).
	public *router.Router
	api    *router.Router
}

// Store gives write access to this instance's people, for imports and seeds.
// Admin edits go through the same store.
func (s *Service) Store() content.Store[Fields, Text] {
	return s.items.In(s.key)
}

// Of returns the People instance with key from a started app, or nil.
func Of(app *webthings.App, key string) *Service {
	service, _ := app.Service(key).(*Service)

	return service
}

// Person is a published person as templates see it.
type Person struct {
	ID    string
	Slug  string
	Name  string
	Group string
	Role  string
	Bio   template.HTML
	Email string
	Phone string
	// PhoneURL is a tel: link for Phone, empty without a phone number.
	PhoneURL template.URL
	Website  string
	LinkedIn string
	// PhotoURL is the photo as pages show it, at most media.ImageWidth
	// wide (see frontend.Frontend.Image); PhotoSrcset lists smaller
	// copies for a srcset attribute, PhotoWidth and PhotoHeight are the
	// size of PhotoURL and PhotoOriginalURL is the uploaded file, for
	// downloads. All are empty when the person has no photo.
	PhotoURL         string
	PhotoSrcset      string
	PhotoOriginalURL string
	PhotoWidth       int
	PhotoHeight      int
	// URL is the person's page, empty when the module is not mounted.
	URL string
}

// Group is a group of people with the same Group name, in list order.
type Group struct {
	Name   string
	People []Person
}

// ListView is the data of people/list.gohtml.
type ListView struct {
	// Title is the title of the page the module is mounted on.
	Title string
	// People are all published people in their order.
	People []Person
	// Groups hold the same people grouped by Group, in order of the first
	// person of each group. People without a group form a group without name.
	Groups []Group
}

// List returns the published people in locale, in their order.
func (s *Service) List(ctx context.Context, locale string) ([]Person, error) {
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	return s.list(ctx, locale, pages.Links(s.public, base))
}

// InGroup returns the published people in locale whose group is group,
// ignoring case.
func (s *Service) InGroup(ctx context.Context, locale, group string) ([]Person, error) {
	all, err := s.List(ctx, locale)
	if err != nil {
		return nil, err
	}

	var out []Person

	for _, person := range all {
		if strings.EqualFold(person.Group, strings.TrimSpace(group)) {
			out = append(out, person)
		}
	}

	return out, nil
}

// BySlug returns the published person with slug in locale, or
// content.ErrNotFound.
func (s *Service) BySlug(ctx context.Context, locale, slug string) (Person, error) {
	base, err := s.pages.MountURL(ctx, s.key, locale)
	if err != nil {
		return Person{}, err
	}

	item, err := s.published(ctx, locale, slug)
	if err != nil {
		return Person{}, err
	}

	return s.person(ctx, item, locale, pages.Links(s.public, base)), nil
}

// list returns the published people in locale, their URLs by route name
// from paths.
func (s *Service) list(ctx context.Context, locale string, paths router.PathFunc) ([]Person, error) {
	items, err := s.items.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	people := make([]Person, 0, len(items))

	for _, item := range items {
		people = append(people, s.person(ctx, item, locale, paths))
	}

	return people, nil
}

// published finds a published person by slug. Instances are small, so the
// published list is searched instead of adding a query.
func (s *Service) published(ctx context.Context, locale, slug string) (Item, error) {
	items, err := s.items.ListPublished(ctx, s.key, locale)
	if err != nil {
		return Item{}, err
	}

	for _, item := range items {
		if item.Fields.Slug == slug {
			return item, nil
		}
	}

	return Item{}, content.ErrNotFound
}

func (s *Service) person(ctx context.Context, item Item, locale string, paths router.PathFunc) Person {
	text, _ := item.Text(locale)
	fields := item.Fields
	shown := s.frontend.Image(ctx, fields.Photo)

	person := Person{
		ID:               item.ID,
		Slug:             fields.Slug,
		Name:             fields.Name,
		Group:            text.Group,
		Role:             text.Role,
		Bio:              s.frontend.Markdown(ctx, text.Bio),
		Email:            fields.Email,
		Phone:            fields.Phone,
		Website:          fields.Website,
		LinkedIn:         fields.LinkedIn,
		PhotoURL:         shown.URL,
		PhotoSrcset:      shown.Srcset,
		PhotoOriginalURL: shown.OriginalURL,
		PhotoWidth:       shown.Width,
		PhotoHeight:      shown.Height,
	}

	// Published people have a slug.
	person.URL, _ = paths("person", "slug", fields.Slug)

	if fields.Phone != "" {
		// Safe: check allows only digits, spaces, "+" and punctuation, and
		// only digits and "+" are kept.
		person.PhoneURL = template.URL("tel:" + dialable(fields.Phone)) //nolint:gosec // See above.
	}

	return person
}

// dialable keeps the digits of a phone number and a leading "+".
func dialable(phone string) string {
	var b strings.Builder

	for i, r := range strings.TrimSpace(phone) {
		if r >= '0' && r <= '9' || r == '+' && i == 0 {
			b.WriteRune(r)
		}
	}

	return b.String()
}

// groups groups people by their group name, keeping the list order.
func groups(people []Person) []Group {
	var (
		out   []Group
		index = map[string]int{}
	)

	for _, person := range people {
		name := strings.ToLower(person.Group)

		i, ok := index[name]
		if !ok {
			i = len(out)
			index[name] = i
			out = append(out, Group{Name: person.Group})
		}

		out[i].People = append(out[i].People, person)
	}

	return out
}

func schema() fs.FS {
	files, _ := fs.Sub(schemaFiles, "schema")

	return files
}
