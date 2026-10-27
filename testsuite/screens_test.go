package testsuite

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/sitetest"

	form "github.com/donseba/go-form/v2"
)

type greetingInput struct {
	Greeting string `form:"input,text" label:"Greeting" required:"true" maxLength:"50"`
}

// TestFormScreen runs an admin.FormScreen: it shows the stored value, keeps
// invalid posts with their errors at the fields and saves valid ones.
func TestFormScreen(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		greeting = "Hello"
	)

	module := webthings.Module{
		Type: "greeting",
		Key:  "greeting",
		Setup: func(env webthings.Env) (webthings.Parts, error) {
			return webthings.Parts{Admin: []admin.Section{{
				ID:    "greeting",
				Title: "Greeting",
				Path:  "/greeting",
				Routes: func(routes *admin.Routes) {
					screen := admin.FormScreen[greetingInput]{
						Admin:     routes.Admin(),
						Title:     "Settings",
						BackURL:   "/admin/",
						BackLabel: "Dashboard",
						Heading:   "Mail",
						Action:    "/admin/greeting",
						Load: func(context.Context) (greetingInput, error) {
							mu.Lock()
							defer mu.Unlock()

							return greetingInput{Greeting: greeting}, nil
						},
						Save: func(r *http.Request, in greetingInput) (form.FieldErrors, error) {
							if strings.Contains(in.Greeting, "!") {
								return admin.FieldError("Greeting", "Speak calmly."), nil
							}

							mu.Lock()
							defer mu.Unlock()

							greeting = in.Greeting

							return nil, nil
						},
					}

					routes.Get("", admin.View, screen.Show)
					routes.Post("", admin.Edit, screen.Post)
				},
			}}}, nil
		},
	}

	s := sitetest.Start(t, module)

	status, page := s.Get("/admin/greeting")
	if status != http.StatusOK || !strings.Contains(page, `value="Hello"`) ||
		!strings.Contains(page, `class="card-title">Mail</h2>`) {
		t.Fatalf("form: %d\n%s", status, page)
	}

	status, page = s.PostStatus("/admin/greeting", "/admin/greeting", url.Values{"Greeting": {"Hi!"}})
	if status != http.StatusUnprocessableEntity || !strings.Contains(page, "Speak calmly.") ||
		!strings.Contains(page, `value="Hi!"`) {
		t.Fatalf("refused: %d\n%s", status, page)
	}

	status, page = s.PostStatus("/admin/greeting", "/admin/greeting", url.Values{"Greeting": {"Good day"}})
	if status != http.StatusOK || !sitetest.HasToast(page, "success", "Changes saved.") ||
		!strings.Contains(page, `value="Good day"`) {
		t.Fatalf("saved: %d\n%s", status, page)
	}
}

// listView is the data of the list screen below.
type listView struct {
	Tabs  admin.Tabs
	Rows  []int
	Pager admin.Pager
}

// TestListScreen renders a list with admin.ListPage and the shared
// "admin-tabs" and "admin-pager" templates.
func TestListScreen(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{"templates/list.gohtml": &fstest.MapFile{Data: []byte(
		`<main id="content">{{template "admin-tabs" .Tabs}}<ul>{{range .Rows}}<li>row {{.}}</li>{{end}}</ul>{{template "admin-pager" .Pager}}</main>`,
	)}}

	module := webthings.Module{
		Type: "numbers",
		Key:  "numbers",
		Setup: func(env webthings.Env) (webthings.Parts, error) {
			return webthings.Parts{Admin: []admin.Section{{
				ID:    "numbers",
				Title: "Numbers",
				Path:  "/numbers",
				Files: files,
				Routes: func(routes *admin.Routes) {
					routes.Get("", admin.View, func(w http.ResponseWriter, r *http.Request) {
						filter := r.URL.Query().Get("filter")

						page, err := admin.ListPage[int]{
							Path: "/admin/numbers",
							Size: 2,
							All: func(context.Context) ([]int, error) {
								if filter == "small" {
									return []int{1, 2}, nil
								}

								return []int{1, 2, 3, 4, 5}, nil
							},
						}.Read(r)
						if err != nil {
							routes.Admin().Fail(w, r, err)

							return
						}

						routes.Admin().Render(w, r, admin.Page{
							Title:    "Numbers",
							Template: "templates/list.gohtml",
							Files:    files,
							Data: listView{
								Tabs: admin.FilterTabs(r, "Filter", "/admin/numbers", "filter", filter,
									admin.Tab{Label: "All", Count: 5, Counted: true},
									admin.Tab{Label: "Small", Value: "small"},
								),
								Rows:  page.Rows,
								Pager: page.Pager,
							},
						})
					})
				},
			}}}, nil
		},
	}

	s := sitetest.Start(t, module)

	s.Expect("/admin/numbers?page=2", http.StatusOK,
		`<nav class="filter-tabs" aria-label="Filter">`,
		`<a href="/admin/numbers" aria-current="page">`,
		`All <span class="badge">5</span>`,
		`<a href="/admin/numbers?filter=small">`,
		`<li>row 3</li><li>row 4</li>`,
		`<a class="button-secondary" href="/admin/numbers" rel="prev">Previous</a>`,
		`<a class="button-secondary" href="/admin/numbers?page=3" rel="next">Next</a>`,
	)

	// A list on one page shows no pager.
	s.ExpectMissing("/admin/numbers?filter=small", `class="admin-pager"`)
}
