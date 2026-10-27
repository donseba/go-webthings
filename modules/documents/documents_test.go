package documents_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/modules/documents"
	"github.com/donseba/go-webthings/sitetest"
)

func TestDocuments(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"}, documents.Module("documents", "Documents"))
	ctx := context.Background()

	file, err := s.App.Media.Upload(ctx, "minutes-march.pdf", strings.NewReader("%PDF-1.4\n% the minutes\n"))
	if err != nil {
		t.Fatal(err)
	}

	minutes := s.Submit("/admin/documents/new?locale=en", url.Values{
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

	report := s.Submit("/admin/documents/new?locale=en", url.Values{
		"Title":    {"Annual report 2025"},
		"Category": {"Reports"},
		"Date":     {"2025-12-01"},
		"Link":     {"https://example.test/report.pdf"},
	})

	s.Expect("/admin/documents?locale=en", http.StatusOK, "Minutes March", "Annual report 2025")

	// A document needs a file or a link, but not both.
	for _, values := range []url.Values{
		{"Title": {"Nothing"}, "Date": {"2026-01-01"}},
		{"Title": {"Both"}, "Date": {"2026-01-01"}, "File": {file.ID}, "Link": {"https://example.test"}},
	} {
		code, body := s.PostStatus("/admin/documents/new?locale=en", "/admin/documents/new?locale=en", values)
		if code != http.StatusUnprocessableEntity ||
			!strings.Contains(body, "Choose a file or enter an external link, not both.") {
			t.Fatalf("%s: %d\n%s", values.Get("Title"), code, body)
		}
	}

	minutesID := strings.TrimPrefix(minutes, "/admin/documents/")
	reportID := strings.TrimPrefix(report, "/admin/documents/")
	minutesURL := "/_/documents/" + minutesID + "/download"
	reportURL := "/_/documents/" + reportID + "/download"

	s.MountIn("documents",
		sitetest.Translation{Locale: "en", Title: "Documents", Slug: "documents"},
		sitetest.Translation{Locale: "nl", Title: "Documenten", Slug: "documents"},
	)
	s.ExpectMissing("/en/documents", "Minutes March")

	// Drafts cannot be downloaded.
	expectRedirect(t, s, http.MethodGet, minutesURL, http.StatusNotFound, "")

	s.Publish(minutes + "?locale=en")
	s.Publish(report + "?locale=en")

	// The list is grouped per year, newest first, with category filters.
	s.Expect("/en/documents", http.StatusOK,
		"<h1>Documents</h1>",
		`<a class="all" href="/en/documents">all</a>`,
		`<a class="category" href="/en/documents/category/minutes">Minutes (1)</a>`,
		`<a class="category" href="/en/documents/category/reports">Reports (1)</a>`,
		"<h2>2026</h2>",
		`<a class="document" href="`+minutesURL+`">Minutes March</a> 2026-03-10 Minutes PDF 23 B downloads: 0`,
		"<em>budget</em>",
		"<h2>2025</h2>",
		`<a class="document" href="`+reportURL+`">Annual report 2025</a> 2025-12-01 Reports`,
	)

	_, body := s.Get("/en/documents")
	if strings.Index(body, "<h2>2026</h2>") > strings.Index(body, "<h2>2025</h2>") {
		t.Fatalf("years are not newest first:\n%s", body)
	}

	s.Expect("/en/documents/category/minutes", http.StatusOK,
		"<h1>Minutes</h1>",
		`<a class="all" href="/en/documents">all</a>`,
		`<a class="category active" href="/en/documents/category/minutes">`,
		"Minutes March",
	)
	s.ExpectMissing("/en/documents/category/minutes", "Annual report")
	s.Expect("/en/documents/category/nope", http.StatusNotFound)

	// The Dutch list has the Dutch text and only documents translated to Dutch.
	s.Expect("/nl/documents", http.StatusOK, "<h1>Documenten</h1>", "Notulen maart", "/nl/documents/category/notulen")
	s.ExpectMissing("/nl/documents", "Annual report")
	s.Expect("/nl/documents/category/minutes", http.StatusNotFound)

	// Downloads redirect to the file or the link and are counted; HEAD is not.
	location := expectRedirect(t, s, http.MethodGet, minutesURL, http.StatusFound, "/media/")
	expectRedirect(t, s, http.MethodGet, minutesURL, http.StatusFound, "/media/")
	expectRedirect(t, s, http.MethodHead, minutesURL, http.StatusFound, "/media/")
	expectRedirect(t, s, http.MethodGet, reportURL, http.StatusFound, "https://example.test/report.pdf")
	expectRedirect(t, s, http.MethodGet, "/_/documents/nope/download", http.StatusNotFound, "")

	// An unknown document gets the site's not-found page.
	s.Visitor().Expect("/_/documents/nope/download", http.StatusNotFound, "<h1>not found</h1>")

	s.Expect(location, http.StatusOK, "the minutes")
	s.Expect(minutesURL, http.StatusOK, "the minutes")

	// Editing a document keeps its count.
	s.Submit(minutes+"?locale=en", url.Values{
		"Title":    {"Minutes March 2026"},
		"Category": {"Minutes"},
		"Date":     {"2026-03-10"},
		"File":     {file.ID},
	})

	s.Expect("/en/documents", http.StatusOK, "Minutes March 2026</a> 2026-03-10 Minutes PDF 23 B downloads: 3", "downloads: 1")

	// The admin lists the counts too; the order stays editable.
	s.Expect("/admin/documents?locale=en", http.StatusOK,
		"Minutes March 2026</a>", "· 3 downloads</span>", "Annual report 2025</a>", "· 1 download</span>", "Move down")

	// The site search finds documents by title, description and category and
	// links to them on the list of their category.
	s.Expect("/en/search?q=budget", http.StatusOK,
		`class="result documents"><a href="/en/documents/category/minutes#`+minutesID+`">Minutes March 2026</a>`)
	s.Expect("/en/search?q=annual+report", http.StatusOK,
		`<a href="/en/documents/category/reports#`+reportID+`">Annual report 2025</a>`)
	s.Expect("/en/search?q=reports", http.StatusOK, "Annual report 2025")
	s.Expect("/nl/search?q=notulen", http.StatusOK, `<a href="/nl/documents/category/notulen#`+minutesID+`">Notulen maart</a>`)
	s.Expect("/nl/search?q=annual", http.StatusOK, "nothing found")

	// Signed-in editors get a link to the admin.
	s.Expect("/en/documents", http.StatusOK, `id="edit" href="/admin/documents?locale=en"`)
	s.Visitor().ExpectMissing("/en/documents", `id="edit"`)

	latest, err := documents.Of(s.App, "documents").Latest(ctx, "en", 5)
	if err != nil || len(latest) != 2 || latest[0].Downloads != 3 || latest[1].Downloads != 1 || !latest[1].External {
		t.Fatalf("latest: %+v %v", latest, err)
	}

	s.Expect("/sitemap.xml", http.StatusOK,
		"/en/documents/category/minutes</loc>",
		"/en/documents/category/reports</loc>",
		"/nl/documents/category/notulen</loc>",
	)

	// Template functions list documents anywhere, here on the home page.
	s.Expect("/en/", http.StatusOK,
		`<ul class="latest"><li><a href="`+minutesURL+`">Minutes March 2026</a></li></ul>`,
		`<ul class="minutes"><li>Minutes March 2026</li></ul>`,
	)
	s.Expect("/nl/", http.StatusOK, `<ul class="minutes"></ul>`)
}

// expectRedirect requests path without following redirects and returns the
// Location, which must start with prefix.
func expectRedirect(t *testing.T, s *sitetest.Site, method, path string, status int, prefix string) string {
	t.Helper()

	recorder := httptest.NewRecorder()
	s.App.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))

	location := recorder.Header().Get("Location")
	if recorder.Code != status || !strings.HasPrefix(location, prefix) {
		t.Fatalf("%s %s: want %d to %q, got %d to %q", method, path, status, prefix, recorder.Code, location)
	}

	return location
}

// TestDocumentsOnNoPage lists the documents of an instance on no page
// without category addresses, instead of links to a page that does not
// exist; downloads keep their own address.
func TestDocumentsOnNoPage(t *testing.T) {
	s := sitetest.StartWith(t, sitetest.Options{Templates: "testdata/templates"}, documents.Module("unlisted", "Unlisted"))
	ctx := context.Background()

	report := s.Submit("/admin/unlisted/new?locale=en", url.Values{
		"Title":    {"Annual report 2025"},
		"Category": {"Reports"},
		"Date":     {"2025-12-01"},
		"Link":     {"https://example.test/report.pdf"},
	})
	s.Publish(report + "?locale=en")

	service := documents.Of(s.App, "unlisted")

	latest, err := service.Latest(ctx, "en", 0)
	if err != nil || len(latest) != 1 || latest[0].Category != "Reports" || latest[0].CategoryURL != "" || latest[0].URL == "" {
		t.Fatalf("latest: %+v %v", latest, err)
	}

	reports, err := service.InCategory(ctx, "en", "reports")
	if err != nil || len(reports) != 1 || reports[0].CategoryURL != "" {
		t.Fatalf("in category: %+v %v", reports, err)
	}
}
