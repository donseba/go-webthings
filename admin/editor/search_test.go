package editor_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/donseba/go-webthings/admin/editor"
)

// TestIndexSearch filters the index on the label, ignoring case and
// accents, and answers an HTMX search with the content only.
func TestIndexSearch(t *testing.T) {
	t.Parallel()

	grace := ada()
	grace.ID = "grace"
	grace.Fields.Name = "Grâce Hopper"

	b := run(t, editor.Config[person, text, personInput]{
		Store: newStore(ada(), grace),
		Label: func(value text) string {
			return value.Role
		},
		ItemLabel: func(it item, _ string) string {
			return it.Fields.Name
		},
	})

	_, body := b.get("/admin/people?q=GRACE")
	if !strings.Contains(body, ">Grâce Hopper</a>") || strings.Contains(body, ">Ada Lovelace</a>") {
		t.Fatalf("search for GRACE:\n%s", body)
	}

	if !strings.Contains(body, `name="q" value="GRACE"`) ||
		!strings.Contains(body, `class="admin-search-clear" href="/admin/people"`) {
		t.Fatal("the search field does not keep the search or offer to clear it")
	}

	_, body = b.get("/admin/people?q=nobody")
	if !strings.Contains(body, "Geen resultaten voor “nobody”") {
		t.Fatalf("empty search result:\n%s", body)
	}

	request, err := http.NewRequest(http.MethodGet, b.server.URL+"/admin/people?q=lovelace", nil)
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("HX-Request", "true")
	request.Header.Set("HX-Target", "main#content")

	response, err := b.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = response.Body.Close() }()

	data, _ := io.ReadAll(response.Body)
	body = string(data)

	if strings.Contains(body, "<body") || !strings.Contains(body, ">Ada Lovelace</a>") ||
		strings.Contains(body, ">Grâce Hopper</a>") {
		t.Fatalf("HTMX search:\n%s", body)
	}
}
