package directory

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"log/slog"
	"strings"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/admin/editor"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/db"
	"github.com/donseba/go-webthings/frontend"

	form "github.com/donseba/go-form/v2"
)

//go:embed templates translations
var files embed.FS

// maxImportBytes limits the CSV import request.
const maxImportBytes = 4 << 20

// Module adds a Directory instance. key is used in URLs of the admin and must
// be unique on the site; title is shown in the admin and the page editor.
func Module(key, title string) webthings.Module {
	return webthings.Module{
		Type:   "directory",
		Key:    key,
		Title:  title,
		Schema: schema(),
		Setup:  setup,
		Funcs:  funcs(),
		Templates: []frontend.Template{
			{Name: "list", Data: ListView{}},
			{Name: "category", Data: CategoryView{}},
			{Name: "entry", Data: Entry{}},
		},
	}
}

func setup(env webthings.Env) (webthings.Parts, error) {
	entriesSpec := entrySpec()
	entriesSpec.Changed = env.Changed

	entries, err := content.NewCollection(env.DB, entriesSpec)
	if err != nil {
		return webthings.Parts{}, err
	}

	categoriesSpec := deletableCategories(env.DB, categorySpec())
	categoriesSpec.Changed = env.Changed

	categories, err := content.NewCollection(env.DB, categoriesSpec)
	if err != nil {
		return webthings.Parts{}, err
	}

	service := &Service{
		key:        env.Key,
		entries:    entries,
		categories: categories,
		frontend:   env.Frontend,
		pages:      env.Pages,
		site:       env.Site,
		locales:    env.Locales,
		adminURL:   env.AdminURL,
	}

	service.public = service.handler()
	service.api = service.apiRoutes()

	return webthings.Parts{
		Admin: []admin.Section{
			entriesSection(env, service),
			categoriesSection(env, service),
		},
		Public:  service.public,
		API:     service.api,
		Service: service,
		Sitemap: service.sitemap,
		Search:  service.search,
		Links:   service.linkSources,
	}, nil
}

// entryInput is one language of an entry in the editor. Everything but the
// texts is shared by all languages.
type entryInput struct {
	Name        string                    `form:"input,text"  label:"Name"          required:"true" maxLength:"200"    trim:"true"`
	Slug        string                    `form:"input,text"  label:"Web address"                   maxLength:"190"                pattern:"^(?:[a-z0-9]+(?:-[a-z0-9]+)*)?$" patternMsg:"Use lowercase letters, digits and dashes for the web address." description:"The last part of the address, such as open-studio in /en/news/open-studio. Made from the name when left empty."`
	Category    form.SortedSelect[string] `form:"dropdown"    label:"Category"      required:"true"`
	Summary     string                    `form:"textarea"    label:"Summary"                       maxLength:"2000"`
	Description string                    `form:"textarea"    label:"Description"                   maxLength:"262144"                                                                                                                                                                                                                                                               class:"markdown"`
	Keywords    string                    `form:"input,text"  label:"Keywords"                      maxLength:"1000"                                                                                                                                    description:"Words visitors may search for, separated by commas."`
	Image       form.SortedSelect[string] `form:"dropdown"    label:"Logo or image"                                                                                                                                                                                                                                                                                                                   placeholder:"—"`
	Website     string                    `form:"input,url"   label:"Website"                       maxLength:"500"    trim:"true"                                                                                                                                                                                                                                                                                    url:"http,https" urlMsg:"The website must be a web address starting with http:// or https://."`
	Email       string                    `form:"input,email" label:"E-mail"                        maxLength:"254"    trim:"true"                                                                                                                                                                                                                                                                                                                                                                                   validate:"mail"`
	Phone       string                    `form:"input,tel"   label:"Phone"                         maxLength:"50"     trim:"true"`
	Address     addressInput              `                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          legend:"Address"`
	Hours       hoursInput                `                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          legend:"Opening hours"`
}

type addressInput struct {
	Street    string   `form:"input,text"   label:"Street and number" maxLength:"200" trim:"true"`
	Postcode  string   `form:"input,text"   label:"Postcode"          maxLength:"20"  trim:"true"`
	City      string   `form:"input,text"   label:"City"              maxLength:"100" trim:"true"`
	Latitude  *float64 `form:"input,number" label:"Latitude"                                      step:"any" min:"-90"  max:"90"  description:"Optional, for a map. For example 52.3731."`
	Longitude *float64 `form:"input,number" label:"Longitude"                                     step:"any" min:"-180" max:"180" description:"Optional, for a map. For example 4.8922."`
}

// hoursInput has an opening and a closing time per day; a day without times
// is closed.
type hoursInput struct {
	MondayOpen     string `form:"input,time" label:"Monday opens"`
	MondayClose    string `form:"input,time" label:"Monday closes"`
	TuesdayOpen    string `form:"input,time" label:"Tuesday opens"`
	TuesdayClose   string `form:"input,time" label:"Tuesday closes"`
	WednesdayOpen  string `form:"input,time" label:"Wednesday opens"`
	WednesdayClose string `form:"input,time" label:"Wednesday closes"`
	ThursdayOpen   string `form:"input,time" label:"Thursday opens"`
	ThursdayClose  string `form:"input,time" label:"Thursday closes"`
	FridayOpen     string `form:"input,time" label:"Friday opens"`
	FridayClose    string `form:"input,time" label:"Friday closes"`
	SaturdayOpen   string `form:"input,time" label:"Saturday opens"`
	SaturdayClose  string `form:"input,time" label:"Saturday closes"`
	SundayOpen     string `form:"input,time" label:"Sunday opens"`
	SundayClose    string `form:"input,time" label:"Sunday closes"`
	Note           string `form:"input,text" label:"Note"             maxLength:"500" description:"For example: closed on public holidays."`
}

// days returns pointers to the open and close inputs per time.Weekday.
func (h *hoursInput) days() [7][2]*string {
	return [7][2]*string{
		{&h.SundayOpen, &h.SundayClose},
		{&h.MondayOpen, &h.MondayClose},
		{&h.TuesdayOpen, &h.TuesdayClose},
		{&h.WednesdayOpen, &h.WednesdayClose},
		{&h.ThursdayOpen, &h.ThursdayClose},
		{&h.FridayOpen, &h.FridayClose},
		{&h.SaturdayOpen, &h.SaturdayClose},
	}
}

func entriesSection(env webthings.Env, service *Service) admin.Section {
	linked := editor.Linked[EntryFields, EntryText, entryInput]{Check: env.Links(), Address: service.address}

	section := editor.New(editor.Config[EntryFields, EntryText, entryInput]{
		ID:       env.Key,
		Title:    env.Title,
		Icon:     "map",
		NewLabel: "New entry",
		Links: []editor.Link{
			{Label: "Categories", Route: admin.RouteName(env.Key+"-categories", "index")},
			{Label: "Import CSV", Route: admin.RouteName(env.Key, "import")},
		},
		Store:   adminEntries{Store: service.entries.In(env.Key)},
		Locales: env.Locales,
		Files:   files,
		// The name is shared by all languages, so it is not in the text.
		ItemLabel: func(item EntryItem, _ string) string {
			return item.Fields.Name
		},
		Notes: service.categoryNotes,
		Form: func(ctx context.Context, item EntryItem, locale string) (entryInput, error) {
			return entryForm(ctx, env, service, item, locale)
		},
		Apply: applyEntry,
		Issue: issue,
		Edit:  linked.Edit,
		Saved: linked.Saved,
		Routes: func(routes *admin.Routes, e editor.Editor[EntryFields, EntryText, entryInput]) {
			screen := importer{service: service, admin: routes.Admin(), routes: routes, title: env.Title}

			routes.Get("/import", admin.Create, screen.form).As("import")
			routes.Post("/import", admin.Create, screen.run)
			linked.Routes(routes, e)
		},
	})

	section.MaxBodyBytes = maxImportBytes

	return section
}

func entryForm(ctx context.Context, env webthings.Env, service *Service, item EntryItem, locale string) (entryInput, error) {
	text, _ := item.Text(locale)
	fields := item.Fields

	image, err := editor.MediaChoices(ctx, env.Media, true, fields.Image)
	if err != nil {
		return entryInput{}, err
	}

	choices, err := service.categoryChoices(ctx, locale)
	if err != nil {
		return entryInput{}, err
	}

	category := form.NewSortedSelect(choices).Selected(fields.Category)

	input := entryInput{
		Name:        fields.Name,
		Slug:        fields.Slug,
		Category:    category,
		Summary:     text.Summary,
		Description: text.Description,
		Keywords:    text.Keywords,
		Image:       image,
		Website:     fields.Website,
		Email:       fields.Email,
		Phone:       fields.Phone,
		Address: addressInput{
			Street:    fields.Street,
			Postcode:  fields.Postcode,
			City:      fields.City,
			Latitude:  coordinate(fields.Latitude),
			Longitude: coordinate(fields.Longitude),
		},
		Hours: hoursInput{Note: text.HoursNote},
	}

	for weekday, day := range input.Hours.days() {
		*day[0] = fields.Hours[weekday].Open
		*day[1] = fields.Hours[weekday].Close
	}

	return input, nil
}

func applyEntry(_ context.Context, in entryInput, draft *content.Draft[EntryFields, EntryText], locale string) error {
	var hours OpeningHours

	for weekday, day := range in.Hours.days() {
		open, okOpen := clockTime(*day[0])
		closing, okClose := clockTime(*day[1])

		if !okOpen || !okClose || (open == "") != (closing == "") {
			return errHours
		}

		hours[weekday] = Day{Open: open, Close: closing}
	}

	draft.Fields = EntryFields{
		Category:  in.Category.Get(),
		Slug:      slugOf(in.Slug, draft.Fields.Slug, in.Name),
		Name:      in.Name,
		Image:     in.Image.Get(),
		Website:   in.Website,
		Email:     in.Email,
		Phone:     in.Phone,
		Street:    in.Address.Street,
		Postcode:  in.Address.Postcode,
		City:      in.Address.City,
		Latitude:  storedCoordinate(in.Address.Latitude),
		Longitude: storedCoordinate(in.Address.Longitude),
		Hours:     hours,
	}

	draft.Translations = content.SetText(draft.Translations, locale, EntryText{
		Summary:     in.Summary,
		Description: in.Description,
		Keywords:    in.Keywords,
		HoursNote:   in.Hours.Note,
	})

	return nil
}

// slugOf is the slug the editor typed, else the current slug, else one
// derived from the name. Keeping the current slug means that saving another
// language with an empty slug field does not rename the URL.
func slugOf(typed, current, name string) string {
	if typed = strings.TrimSpace(typed); typed != "" {
		return typed
	}

	if current != "" {
		return current
	}

	return content.Slug(name)
}

// website adds https:// to an imported address written without a scheme.
func website(address string) string {
	address = strings.TrimSpace(address)
	if address != "" && !strings.Contains(address, "://") {
		address = "https://" + address
	}

	return address
}

// coordinate is a stored latitude or longitude for the editor; nil without
// one.
func coordinate(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}

	return &value.Float64
}

// storedCoordinate is a latitude or longitude from the editor as stored;
// an empty field is none.
func storedCoordinate(value *float64) sql.NullFloat64 {
	if value == nil {
		return sql.NullFloat64{}
	}

	return sql.NullFloat64{Float64: *value, Valid: true}
}

// categoryChoices are the categories for the entry dropdown, named in locale
// or else in any language they have.
func (s *Service) categoryChoices(ctx context.Context, locale string) (map[string]string, error) {
	items, err := s.categories.List(ctx, s.key)
	if err != nil {
		return nil, err
	}

	choices := map[string]string{}

	for _, item := range items {
		choices[item.ID] = categoryName(item, locale)
	}

	return choices, nil
}

// categoryName is the category's name in locale, or in its first language, or
// its slug.
func categoryName(item CategoryItem, locale string) string {
	if text, ok := item.Text(locale); ok && text.Name != "" {
		return text.Name
	}

	for _, translation := range item.Translations {
		if translation.Text.Name != "" {
			return translation.Text.Name
		}
	}

	return item.Fields.Slug
}

// categoryNotes shows each entry's category in the overview, named in
// locale; the categories are read once. The overview still works without
// them when reading fails.
func (s *Service) categoryNotes(ctx context.Context, items []EntryItem, locale string) map[string]string {
	names, err := s.categoryChoices(ctx, locale)
	if err != nil {
		slog.ErrorContext(ctx, "directory categories", "module", s.key, "error", err)

		return nil
	}

	notes := make(map[string]string, len(items))

	for _, item := range items {
		notes[item.ID] = names[item.Fields.Category]
	}

	return notes
}

// adminEntries is the entry store as the editor sees it: sorted by name
// without manual ordering, because the website lists entries by name.
type adminEntries struct {
	content.Store[EntryFields, EntryText]
}

func (s adminEntries) List(ctx context.Context) ([]EntryItem, error) {
	items, err := s.Store.List(ctx)
	if err != nil {
		return nil, err
	}

	sortByName(items)

	return items, nil
}

type categoryInput struct {
	Name    string `form:"input,text" label:"Name"        required:"true" maxLength:"200"  trim:"true"`
	Slug    string `form:"input,text" label:"Web address"                 maxLength:"190"              pattern:"^(?:[a-z0-9]+(?:-[a-z0-9]+)*)?$" patternMsg:"Use lowercase letters, digits and dashes for the web address." description:"The last part of the address, such as open-studio in /en/news/open-studio. Made from the name when left empty."`
	Summary string `form:"textarea"   label:"Summary"                     maxLength:"2000"`
}

func categoriesSection(env webthings.Env, service *Service) admin.Section {
	linked := editor.Linked[CategoryFields, CategoryText, categoryInput]{
		Check:   env.Links(),
		Address: service.categoryAddress,
		Below:   service.entryAddresses,
	}

	section := editor.New(editor.Config[CategoryFields, CategoryText, categoryInput]{
		ID:         env.Key + "-categories",
		Permission: env.Key,
		Title:      "Categories",
		NewLabel:   "New category",
		Links:      []editor.Link{{Label: env.Title, Route: admin.RouteName(env.Key, "index")}},
		Store:      service.categories.In(env.Key),
		Locales:    env.Locales,
		Files:      files,
		Label: func(text CategoryText) string {
			return text.Name
		},

		Form: func(_ context.Context, item CategoryItem, locale string) (categoryInput, error) {
			text, _ := item.Text(locale)

			return categoryInput{Name: text.Name, Slug: item.Fields.Slug, Summary: text.Summary}, nil
		},
		Apply: func(_ context.Context, in categoryInput, draft *content.Draft[CategoryFields, CategoryText], locale string) error {
			draft.Fields = CategoryFields{Slug: slugOf(in.Slug, draft.Fields.Slug, in.Name)}
			draft.Translations = content.SetText(draft.Translations, locale, CategoryText{
				Name:    in.Name,
				Summary: in.Summary,
			})

			return nil
		},
		Issue:  categoryIssue,
		Edit:   linked.Edit,
		Routes: linked.Routes,
		Saved:  linked.Saved,
	})

	// Categories are reached from the entries overview, not from the navigation.
	section.Hidden = true

	return section
}

// categoryIssue explains refused changes of categories: a category with
// entries cannot be deleted, because its entries would disappear from the
// site.
func categoryIssue(err error) string {
	if errors.Is(err, errCategoryUsed) || db.IsForeignKey(err) {
		return "This category still has entries. Move them to another category or delete them first."
	}

	return issue(err)
}

// issue turns module errors into messages for the editor and the import.
func issue(err error) string {
	switch {
	case errors.Is(err, content.ErrTooLong):
		// The import explains skipped rows with these messages too, so
		// the editor's default is not enough.
		return "The text is too long."
	case errors.Is(err, errName):
		return "The name is missing."
	case errors.Is(err, errSlug):
		return "Use lowercase letters, digits and dashes for the web address."
	case errors.Is(err, errEmail):
		return "The e-mail address is not valid."
	case errors.Is(err, errWebsite):
		return "The website must be a web address starting with http:// or https://."
	case errors.Is(err, errLocation):
		return "Latitude and longitude must both be filled in, as decimal degrees."
	case errors.Is(err, errHours):
		return "Fill in both times of a day as hours and minutes, or leave both empty."
	case errors.Is(err, errCategory):
		return "Choose a category."
	case db.IsUnique(err):
		return "This web address is already used."
	}

	return ""
}
