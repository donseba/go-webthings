package testsuite

import (
	"net/http"
	"net/url"
	"path"
	"strings"
	"testing"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/api"
	"github.com/donseba/go-webthings/media"
	"github.com/donseba/go-webthings/modules/directory"
	"github.com/donseba/go-webthings/modules/documents"
	"github.com/donseba/go-webthings/modules/gallery"
	"github.com/donseba/go-webthings/modules/lists"
	"github.com/donseba/go-webthings/modules/people"
	"github.com/donseba/go-webthings/modules/testimonials"
	"github.com/donseba/go-webthings/sitetest"
)

// moduleSecrets are words in the titles, names and contact details of
// drafts, unpublished content and content of unpublished categories, which
// no endpoint may show.
var moduleSecrets = []string{"Secret", "Hidden", "hidden@example.test", "Ghost", "Draft deli", "Unpublished"}

// modulesSite is a site with the content API on and an instance of the
// gallery, lists, people, testimonials, directory and documents modules,
// each with published content in English and Dutch and drafts. The
// testimonials are on a page in English only; configure, if any, changes
// the configuration further. It returns the IDs of the content by name.
func modulesSite(t *testing.T, configure func(*webthings.Config)) (*sitetest.Site, map[string]string) {
	t.Helper()

	keys := []string{"photos", "faq", "team", "quotes", "guide", "docs"}

	s := sitetest.StartWith(t, sitetest.Options{
		StartingPoints: keys,
		Configure: func(config *webthings.Config) {
			config.BaseURL = apiBase
			config.API = api.Config{Enabled: true}

			if configure != nil {
				configure(config)
			}
		},
	},
		gallery.Module("photos", "Photos"),
		lists.Module("faq", "FAQ"),
		people.Module("team", "Team"),
		testimonials.Module("quotes", "Quotes"),
		directory.Module("guide", "Guide"),
		documents.Module("docs", "Documents"),
	)

	ids := map[string]string{}
	photo := s.Upload("harbour.png", sitetest.PNG(t, 1600, 900))
	boat := s.Upload("boat.png", sitetest.PNG(t, 800, 600))
	ids["photo"] = photo.ID

	for _, translation := range []media.Translation{
		{Locale: "en", Title: "The harbour", AltText: "Boats in the harbour"},
		{Locale: "nl", Title: "De haven", AltText: "Boten in de haven"},
	} {
		if err := s.App.Media.SaveTranslation(t.Context(), photo.ID, translation); err != nil {
			t.Fatal(err)
		}
	}

	s.MountIn("photos", sitetest.Translation{Locale: "en", Title: "Photos", Slug: "photos"},
		sitetest.Translation{Locale: "nl", Title: "Foto's", Slug: "fotos"})
	s.MountIn("faq", sitetest.Translation{Locale: "en", Title: "FAQ", Slug: "faq"},
		sitetest.Translation{Locale: "nl", Title: "Vragen", Slug: "vragen"})
	s.MountIn("team", sitetest.Translation{Locale: "en", Title: "Team", Slug: "team"},
		sitetest.Translation{Locale: "nl", Title: "Team", Slug: "team"})
	s.Mount("quotes", "Quotes", "quotes")
	s.MountIn("guide", sitetest.Translation{Locale: "en", Title: "Guide", Slug: "guide"},
		sitetest.Translation{Locale: "nl", Title: "Gids", Slug: "gids"})
	s.MountIn("docs", sitetest.Translation{Locale: "en", Title: "Documents", Slug: "documents"},
		sitetest.Translation{Locale: "nl", Title: "Documenten", Slug: "documenten"})

	addGallery(s, ids, photo.ID, boat.ID)
	addLists(s, ids, photo.ID)
	addPeople(s, ids, photo.ID)
	addTestimonials(s, ids, photo.ID)
	addDirectory(s, ids, photo.ID)
	addDocuments(t, s, ids)

	return s, ids
}

// addGallery adds a published album in English and Dutch and a draft.
func addGallery(s *sitetest.Site, ids map[string]string, photo, boat string) {
	album := s.Submit("/admin/photos/new?locale=en", url.Values{
		"Title":       {"Harbour"},
		"Slug":        {"harbour"},
		"Description": {"Boats in *summer*. ![](media:" + photo + ")"},
		"Cover":       {boat},
		"Images":      {photo, boat},
	})
	s.Submit(album+"?locale=nl", url.Values{
		"Title":       {"Haven"},
		"Slug":        {"haven"},
		"Description": {"Boten in de *zomer*."},
		"Cover":       {boat},
		"Images":      {photo, boat},
	})
	s.Publish(album + "?locale=en")
	ids["album"] = path.Base(album)

	draft := s.Submit("/admin/photos/new?locale=en", url.Values{
		"Title": {"Secret album"}, "Slug": {"secret"}, "Cover": {""}, "Images": {photo},
	})
	ids["album-draft"] = path.Base(draft)
}

// addLists adds two published entries, one with a path of the site as its
// link, and a draft.
func addLists(s *sitetest.Site, ids map[string]string, photo string) {
	hours := s.Submit("/admin/faq/new?locale=en", url.Values{
		"Title": {"When are you open?"},
		"Body":  {"Every day, see the [guide](/en/guide)."},
		"Image": {photo},
		"Link":  {"/en/guide"},
	})
	s.Submit(hours+"?locale=nl", url.Values{
		"Title": {"Wanneer zijn jullie open?"},
		"Body":  {"Elke dag."},
		"Image": {photo},
		"Link":  {"/en/guide"},
	})
	s.Publish(hours + "?locale=en")
	ids["entry"] = path.Base(hours)

	contact := s.Submit("/admin/faq/new?locale=en", url.Values{
		"Title": {"How do I reach you?"},
		"Body":  {"By mail."},
		"Image": {""},
		"Link":  {"mailto:info@example.test"},
	})
	s.Publish(contact + "?locale=en")

	draft := s.Submit("/admin/faq/new?locale=en", url.Values{
		"Title": {"Unpublished question"}, "Body": {"Not yet."}, "Image": {""},
	})
	ids["entry-draft"] = path.Base(draft)
}

// addPeople adds a published person with contact details in English and
// Dutch, one without them, and an unpublished person.
func addPeople(s *sitetest.Site, ids map[string]string, photo string) {
	jane := url.Values{
		"Name":     {"Jane Doe"},
		"Slug":     {"jane-doe"},
		"Group":    {"Board"},
		"Role":     {"Chair"},
		"Bio":      {"Leads the *board*."},
		"Photo":    {photo},
		"Email":    {"jane@example.test"},
		"Phone":    {"+31 20 123 4567"},
		"Website":  {"https://jane.example.test"},
		"LinkedIn": {"https://www.linkedin.com/in/jane"},
	}

	person := s.Submit("/admin/team/new?locale=en", jane)

	jane.Set("Group", "Bestuur")
	jane.Set("Role", "Voorzitter")
	jane.Set("Bio", "Leidt het *bestuur*.")
	s.Submit(person+"?locale=nl", jane)
	s.Publish(person + "?locale=en")
	ids["person"] = path.Base(person)

	john := s.Submit("/admin/team/new?locale=en", url.Values{
		"Name": {"John Smith"}, "Group": {"Staff"}, "Role": {"Developer"}, "Photo": {""},
	})
	s.Publish(john + "?locale=en")
	ids["person-plain"] = path.Base(john)

	hidden := s.Submit("/admin/team/new?locale=en", url.Values{
		"Name": {"Hidden Person"}, "Email": {"hidden@example.test"}, "Photo": {""},
	})
	ids["person-draft"] = path.Base(hidden)
}

// addTestimonials adds a published testimonial in English and Dutch and a
// draft.
func addTestimonials(s *sitetest.Site, ids map[string]string, photo string) {
	quote := s.Submit("/admin/quotes/new?locale=en", url.Values{
		"Quote":        {"The best harbour in town."},
		"Author":       {"Ann Visitor"},
		"Role":         {"Sailor"},
		"Organisation": {"Sailing club"},
		"Photo":        {photo},
	})
	s.Submit(quote+"?locale=nl", url.Values{
		"Quote":        {"De beste haven van de stad."},
		"Author":       {"Ann Visitor"},
		"Role":         {"Zeiler"},
		"Organisation": {"Zeilclub"},
		"Photo":        {photo},
	})
	s.Publish(quote + "?locale=en")
	ids["testimonial"] = path.Base(quote)

	draft := s.Submit("/admin/quotes/new?locale=en", url.Values{
		"Quote": {"Secret praise"}, "Author": {"Hidden author"}, "Photo": {""},
	})
	ids["testimonial-draft"] = path.Base(draft)
}

// addDirectory adds a published category in English and Dutch with a
// published entry with every detail and a draft entry, and an unpublished
// category with a published entry.
func addDirectory(s *sitetest.Site, ids map[string]string, photo string) {
	food := s.Submit("/admin/guide-categories/new?locale=en", url.Values{"Name": {"Food"}, "Summary": {"Eat well."}})
	s.Submit(food+"?locale=nl", url.Values{"Name": {"Eten"}, "Summary": {"Lekker eten."}})
	s.Publish(food + "?locale=en")
	ids["category"] = path.Base(food)

	bakery := url.Values{
		"Name":              {"Bakery Jansen"},
		"Slug":              {""},
		"Category":          {ids["category"]},
		"Summary":           {"Fresh bread."},
		"Description":       {"**Since 1920**"},
		"Keywords":          {"bread, croissant"},
		"Image":             {photo},
		"Website":           {"https://bakery.example"},
		"Email":             {"info@bakery.example"},
		"Phone":             {"020 123 4567"},
		"Address.Street":    {"Main street 1"},
		"Address.Postcode":  {"1011 AB"},
		"Address.City":      {"Amsterdam"},
		"Address.Latitude":  {"52.3731"},
		"Address.Longitude": {"4.8922"},
		"Hours.MondayOpen":  {"08:00"},
		"Hours.MondayClose": {"18:00"},
		"Hours.FridayOpen":  {"08:00"},
		"Hours.FridayClose": {"21:30"},
		"Hours.Note":        {"Closed on public holidays."},
	}

	entry := s.Submit("/admin/guide/new?locale=en", bakery)

	bakery.Set("Summary", "Vers brood.")
	bakery.Set("Description", "**Sinds 1920**")
	bakery.Set("Hours.Note", "Gesloten op feestdagen.")
	s.Submit(entry+"?locale=nl", bakery)
	s.Publish(entry + "?locale=en")
	ids["directory-entry"] = path.Base(entry)

	s.Submit("/admin/guide/new?locale=en", url.Values{
		"Name": {"Draft deli"}, "Slug": {""}, "Category": {ids["category"]}, "Image": {""},
	})

	closed := s.Submit("/admin/guide-categories/new?locale=en", url.Values{"Name": {"Hidden places"}})

	ghost := s.Submit("/admin/guide/new?locale=en", url.Values{
		"Name": {"Ghost shop"}, "Slug": {""}, "Category": {path.Base(closed)}, "Image": {""},
	})
	s.Publish(ghost + "?locale=en")
	ids["directory-ghost"] = path.Base(ghost)
}

// addDocuments adds a published file in English and Dutch, a published
// link in English only and an unpublished document.
func addDocuments(t *testing.T, s *sitetest.Site, ids map[string]string) {
	t.Helper()

	file, err := s.App.Media.Upload(t.Context(), "minutes-march.pdf", strings.NewReader("%PDF-1.4\n% the minutes\n"))
	if err != nil {
		t.Fatal(err)
	}

	minutes := s.Submit("/admin/docs/new?locale=en", url.Values{
		"Title":       {"Minutes March"},
		"Category":    {"Minutes"},
		"Date":        {"2026-03-10"},
		"File":        {file.ID},
		"Description": {"Approved the *budget*."},
	})
	s.Submit(minutes+"?locale=nl", url.Values{
		"Title":    {"Notulen maart"},
		"Category": {"Notulen"},
		"Date":     {"2026-03-10"},
		"File":     {file.ID},
	})
	s.Publish(minutes + "?locale=en")
	ids["document"] = path.Base(minutes)

	report := s.Submit("/admin/docs/new?locale=en", url.Values{
		"Title": {"Annual report 2025"},
		"Date":  {"2025-12-01"},
		"File":  {""},
		"Link":  {"https://example.org/report.pdf"},
	})
	s.Publish(report + "?locale=en")
	ids["document-link"] = path.Base(report)

	memo := s.Submit("/admin/docs/new?locale=en", url.Values{
		"Title":    {"Secret memo"},
		"Category": {"Hidden"},
		"Date":     {"2026-04-01"},
		"File":     {file.ID},
	})
	ids["document-draft"] = path.Base(memo)
}

// TestContentAPIModules checks the endpoints of the content modules in
// English and Dutch against golden files, and that drafts, unpublished
// content and unknown IDs are not found.
func TestContentAPIModules(t *testing.T) {
	t.Parallel()

	s, ids := modulesSite(t, nil)

	for _, check := range []struct{ path, golden string }{
		{"/api/v1/photos/albums", "gallery.albums.en"},
		{"/api/v1/photos/albums?locale=nl", "gallery.albums.nl"},
		{"/api/v1/photos/albums/" + ids["album"], "gallery.album.en"},
		{"/api/v1/faq/entries", "lists.entries.en"},
		{"/api/v1/faq/entries?locale=nl", "lists.entries.nl"},
		{"/api/v1/faq/entries/" + ids["entry"], "lists.entry.en"},
		{"/api/v1/team/people", "people.people.en"},
		{"/api/v1/team/people/" + ids["person"], "people.person.en"},
		{"/api/v1/team/people/" + ids["person"] + "?locale=nl", "people.person.nl"},
		{"/api/v1/quotes/testimonials", "testimonials.testimonials.en"},
		{"/api/v1/quotes/testimonials?locale=nl", "testimonials.testimonials.nl"},
		{"/api/v1/guide/entries", "directory.entries.en"},
		{"/api/v1/guide/entries/" + ids["directory-entry"] + "?locale=nl", "directory.entry.nl"},
		{"/api/v1/guide/categories", "directory.categories.en"},
		{"/api/v1/docs/documents", "documents.documents.en"},
		{"/api/v1/docs/documents?locale=nl", "documents.documents.nl"},
		{"/api/v1/docs/documents/" + ids["document-link"], "documents.document.en"},
		{"/api/v1/docs/categories", "documents.categories.en"},
	} {
		goldenAPI(t, s, check.path, check.golden)
	}

	// Lists of every module show no drafts, unpublished content or content
	// of unpublished categories.
	for _, list := range []string{
		"/api/v1/photos/albums",
		"/api/v1/faq/entries",
		"/api/v1/team/people",
		"/api/v1/quotes/testimonials",
		"/api/v1/guide/entries",
		"/api/v1/guide/categories",
		"/api/v1/docs/documents",
		"/api/v1/docs/categories",
	} {
		response := apiGet(t, s, list)

		for _, secret := range moduleSecrets {
			if strings.Contains(response.body, secret) {
				t.Fatalf("GET %s shows %q:\n%s", list, secret, response.body)
			}
		}
	}

	// Their IDs are not found, as are IDs that do not exist.
	for _, missing := range []string{
		"/api/v1/photos/albums/" + ids["album-draft"],
		"/api/v1/faq/entries/" + ids["entry-draft"],
		"/api/v1/team/people/" + ids["person-draft"],
		"/api/v1/quotes/testimonials/" + ids["testimonial-draft"],
		"/api/v1/guide/entries/" + ids["directory-ghost"],
		"/api/v1/docs/documents/" + ids["document-draft"],
		"/api/v1/photos/albums/nope",
		"/api/v1/faq/entries/nope",
		"/api/v1/team/people/nope",
		"/api/v1/quotes/testimonials/nope",
		"/api/v1/guide/entries/nope",
		"/api/v1/docs/documents/nope",
		// The link is in English only, the gallery's IDs are not the list's.
		"/api/v1/docs/documents/" + ids["document-link"] + "?locale=nl",
		"/api/v1/faq/entries/" + ids["album"],
		// Unknown categories, as on the site.
		"/api/v1/guide/entries?category=nope",
		"/api/v1/docs/documents?category=hidden",
	} {
		expectAPIError(t, s, missing, http.StatusNotFound, api.CodeNotFound)
	}

	// Filters by slug, category and group, the latter in the request's
	// language and ignoring case.
	for _, filter := range []struct {
		path  string
		count int
	}{
		{"/api/v1/photos/albums?slug=harbour", 1},
		{"/api/v1/photos/albums?slug=haven&locale=nl", 1},
		{"/api/v1/photos/albums?slug=secret", 0},
		{"/api/v1/team/people?slug=jane-doe", 1},
		{"/api/v1/team/people?slug=nope", 0},
		{"/api/v1/team/people?group=board", 1},
		{"/api/v1/team/people?group=%20Staff%20", 1},
		{"/api/v1/team/people?group=bestuur&locale=nl", 1},
		{"/api/v1/team/people?group=Bestuur", 0},
		{"/api/v1/team/people?group=board&slug=jane-doe", 1},
		{"/api/v1/team/people?group=staff&slug=jane-doe", 0},
		{"/api/v1/guide/entries?category=food", 1},
		{"/api/v1/guide/entries?slug=bakery-jansen", 1},
		{"/api/v1/docs/documents?category=minutes", 1},
		{"/api/v1/docs/documents?category=notulen&locale=nl", 1},
	} {
		list := expectAPI(t, s, filter.path, http.StatusOK)
		if list["total"] != float64(filter.count) {
			t.Fatalf("GET %s: %v items, want %d", filter.path, list["total"], filter.count)
		}
	}
}

// TestContentAPIModulesLastModified checks the Last-Modified of the single
// items of the content modules: the latest date of what the answer shows,
// which answers If-Modified-Since with 304 also from the output cache,
// moves on when the item is saved and is left out while an image it shows
// is gone, and for the directory's entries and the documents, whose
// categories count the other items.
func TestContentAPIModulesLastModified(t *testing.T) {
	t.Parallel()

	s, ids := modulesSite(t, cachedSite(time.Hour))

	endpoints := map[string]string{
		"album":       "/api/v1/photos/albums/" + ids["album"],
		"entry":       "/api/v1/faq/entries/" + ids["entry"],
		"person":      "/api/v1/team/people/" + ids["person"],
		"testimonial": "/api/v1/quotes/testimonials/" + ids["testimonial"],
	}

	modified := map[string]time.Time{}

	for name, address := range endpoints {
		// Twice: the output cache misses, then answers from its entry.
		for range 2 {
			response := apiGet(t, s, address)

			date, err := http.ParseTime(response.header.Get("Last-Modified"))
			if response.status != http.StatusOK || err != nil || date.After(time.Now()) {
				t.Fatalf("GET %s: %d, Last-Modified %q", address, response.status, response.header.Get("Last-Modified"))
			}

			modified[name] = date

			since := date.Format(http.TimeFormat)
			if again := apiGet(t, s, address, "If-Modified-Since", since); again.status != http.StatusNotModified {
				t.Fatalf("GET %s since %s: %d", address, since, again.status)
			}

			earlier := date.Add(-time.Second).Format(http.TimeFormat)
			if again := apiGet(t, s, address, "If-Modified-Since", earlier); again.status != http.StatusOK {
				t.Fatalf("GET %s since %s: %d", address, earlier, again.status)
			}
		}
	}

	for _, counted := range []string{
		"/api/v1/guide/entries/" + ids["directory-entry"],
		"/api/v1/docs/documents/" + ids["document-link"],
	} {
		if response := apiGet(t, s, counted); response.status != http.StatusOK || response.header.Get("Last-Modified") != "" {
			t.Fatalf("GET %s: %d, Last-Modified %q", counted, response.status, response.header.Get("Last-Modified"))
		}
	}

	// HTTP dates count seconds.
	time.Sleep(1100 * time.Millisecond)

	edits := []struct {
		name, path string
		values     url.Values
	}{
		{"album", "/admin/photos/" + ids["album"], url.Values{
			"Title":       {"Harbour"},
			"Slug":        {"harbour"},
			"Description": {"Boats in *autumn*."},
			"Cover":       {ids["photo"]},
			"Images":      {ids["photo"]},
		}},
		{"entry", "/admin/faq/" + ids["entry"], url.Values{
			"Title": {"When are you open?"},
			"Body":  {"Every day but Sunday."},
			"Image": {ids["photo"]},
			"Link":  {"/en/guide"},
		}},
		{"person", "/admin/team/" + ids["person"], url.Values{
			"Name":  {"Jane Doe"},
			"Slug":  {"jane-doe"},
			"Group": {"Board"},
			"Role":  {"Treasurer"},
			"Photo": {ids["photo"]},
		}},
		{"testimonial", "/admin/quotes/" + ids["testimonial"], url.Values{
			"Quote":  {"Still the best harbour in town."},
			"Author": {"Ann Visitor"},
			"Photo":  {ids["photo"]},
		}},
	}

	// Saving an item moves its date, and only its date, on.
	for _, edit := range edits {
		s.Submit(edit.path+"?locale=en", edit.values)

		for name, address := range endpoints {
			response := apiGet(t, s, address)

			date, err := http.ParseTime(response.header.Get("Last-Modified"))
			if response.status != http.StatusOK || err != nil {
				t.Fatalf(
					"%s saved: GET %s: %d, Last-Modified %q",
					edit.name,
					address,
					response.status,
					response.header.Get("Last-Modified"),
				)
			}

			if moved := date.After(modified[name]); moved != (name == edit.name) {
				t.Fatalf("%s saved: %s from %v to %v", edit.name, name, modified[name], date)
			}

			modified[name] = date
		}
	}

	// An image that is gone leaves them without a date.
	_, err := s.App.DB.Exec(t.Context(), `UPDATE wt_media_asset SET deleted_at=? WHERE id=?`, time.Now().UTC(), ids["photo"])
	if err != nil {
		t.Fatal(err)
	}

	s.App.Changed()

	for _, address := range endpoints {
		if response := apiGet(t, s, address); response.status != http.StatusOK || response.header.Get("Last-Modified") != "" {
			t.Fatalf(
				"GET %s without its image: %d, Last-Modified %q",
				address,
				response.status,
				response.header.Get("Last-Modified"),
			)
		}
	}
}
