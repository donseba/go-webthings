package testsuite

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/modules/banners"
	"github.com/donseba/go-webthings/modules/comments"
	"github.com/donseba/go-webthings/modules/directory"
	"github.com/donseba/go-webthings/modules/documents"
	"github.com/donseba/go-webthings/modules/events"
	"github.com/donseba/go-webthings/modules/forms"
	"github.com/donseba/go-webthings/modules/gallery"
	"github.com/donseba/go-webthings/modules/lists"
	"github.com/donseba/go-webthings/modules/newsletter"
	"github.com/donseba/go-webthings/modules/people"
	"github.com/donseba/go-webthings/modules/polls"
	"github.com/donseba/go-webthings/modules/publishing"
	"github.com/donseba/go-webthings/modules/redirects"
	"github.com/donseba/go-webthings/modules/registrations"
	"github.com/donseba/go-webthings/modules/stats"
	"github.com/donseba/go-webthings/modules/submissions"
	"github.com/donseba/go-webthings/modules/testimonials"

	translator "github.com/donseba/go-translator/v2"
	"github.com/donseba/go-translator/v2/extract"
)

func translationModule(key string) webthings.Module {
	return webthings.Module{
		Type: "shop",
		Key:  key,
		Setup: func(webthings.Env) (webthings.Parts, error) {
			return webthings.Parts{}, nil
		},
		Templates: []frontend.Template{
			{Name: "cart", Example: `{{tl localizer "Your cart"}}`},
			{Name: "product", Example: `{{tl localizer "Add to cart"}}`},
		},
		Components: []frontend.Template{
			{Name: "badge", Example: `{{tl localizer "In stock"}}`},
		},
		Mail: []frontend.Template{
			{Name: "order", Example: `{{define "subject"}}{{tl localizer "Thank you"}}{{end}}` +
				`{{define "text"}}{{tn localizer "%d product" "%d products" .N .N}}{{end}}`},
			{Name: "shipped", Example: `{{define "subject"}}{{tl localizer "Shipped"}}{{end}}{{define "text"}}{{end}}`},
		},
	}
}

// referenceNames are the names of the catalog's references, without line
// numbers, sorted.
func referenceNames(catalog *extract.Catalog) []string {
	var names []string

	for _, message := range catalog.Messages {
		for _, reference := range message.References {
			name := reference
			if before, line, ok := strings.Cut(reference, ".gohtml:"); ok && line != "" {
				name = before + ".gohtml"
			}

			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}

	slices.Sort(names)

	return names
}

// TestTranslationCatalog checks that extraction covers the theme templates
// and the modules' default templates that a theme does not replace, without
// starting the app.
func TestTranslationCatalog(t *testing.T) {
	t.Parallel()

	templates := fstest.MapFS{
		"default/layout.gohtml": {Data: []byte(`{{tl localizer "Menu"}}{{content}}`)},
		// The themes replace the product page of every shop (by type) and
		// the store's cart (by key); the outlet still uses the module's cart.
		// "night" inherits these from the default theme.
		"default/shop/product.gohtml": {Data: []byte(`{{tl localizer "Buy"}}`)},
		"default/store/cart.gohtml":   {Data: []byte(`{{tl localizer "Basket"}}`)},
		"default/mail/shop/shipped.gohtml": {
			Data: []byte(`{{define "subject"}}{{tl localizer "On its way"}}{{end}}{{define "text"}}{{end}}`),
		},
		"night/theme.json": {Data: []byte(`{"title": "Night"}`)},
	}

	app := webthings.New(webthings.Config{Templates: templates})

	if err := app.Use(translationModule("store"), translationModule("outlet")); err != nil {
		t.Fatal(err)
	}

	catalog, err := app.TranslationCatalog()
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"core:calendar",
		"core:site/share.gohtml",
		"core:texts",
		"default/layout.gohtml",
		"default/mail/shop/shipped.gohtml",
		"default/shop/product.gohtml",
		"default/store/cart.gohtml",
		"module:shop/badge.gohtml",
		"module:shop/cart.gohtml",
		"module:shop/mail/order.gohtml",
	}

	if got := strings.Join(referenceNames(catalog), " "); got != strings.Join(want, " ") {
		t.Fatalf("references = %s\nwant %s", got, strings.Join(want, " "))
	}

	dir := t.TempDir()

	if err := app.Translations(dir, nil); err != nil {
		t.Fatal(err)
	}

	pot, err := os.ReadFile(filepath.Join(dir, translator.DefaultPotFile))
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range []string{
		"#: default/layout.gohtml:1\nmsgid \"Menu\"\n",
		"#: module:shop/cart.gohtml:1\nmsgid \"Your cart\"\n",
		"#: module:shop/badge.gohtml:1\nmsgid \"In stock\"\n",
		"#: module:shop/mail/order.gohtml:1\nmsgid \"%d product\"\nmsgid_plural \"%d products\"\n",
		"#: core:texts\nmsgid \"Not found\"\n",
		"#: core:calendar\nmsgctxt \"month\"\nmsgid \"May\"\n",
		"#: core:calendar\nmsgctxt \"month-short\"\nmsgid \"May\"\n",
		"#: core:calendar\nmsgctxt \"date\"\nmsgid \"short\"\n",
		"msgid \"E-mail\"\n",
		"msgid \"LinkedIn\"\n",
	} {
		if !strings.Contains(string(pot), entry) {
			t.Errorf("POT misses %q:\n%s", entry, pot)
		}
	}

	for _, replaced := range []string{"Add to cart", "Shipped"} {
		if strings.Contains(string(pot), replaced) {
			t.Errorf("POT has %q from a template every theme replaces:\n%s", replaced, pot)
		}
	}
}

// TestTranslationCatalogWithoutTemplates checks that a site without
// templates (for example with its own Renderer) still gets the modules' texts.
func TestTranslationCatalogWithoutTemplates(t *testing.T) {
	t.Parallel()

	app := webthings.New(webthings.Config{})

	if err := app.Use(translationModule("store")); err != nil {
		t.Fatal(err)
	}

	catalog, err := app.TranslationCatalog()
	if err != nil {
		t.Fatal(err)
	}

	// The module's two pages, component and two mails, the core share bar
	// and the texts core translates in Go.
	want := "core:calendar core:site/share.gohtml core:texts module:shop/badge.gohtml module:shop/cart.gohtml " +
		"module:shop/mail/order.gohtml module:shop/mail/shipped.gohtml module:shop/product.gohtml"

	if got := strings.Join(referenceNames(catalog), " "); got != want || catalog.Sources != 6 {
		t.Fatalf("references = %s from %d templates, want %s from six default templates", got, catalog.Sources, want)
	}
}

// TestBundledTemplatesExtract checks that the texts of every bundled
// module's default templates can be extracted: a warning here shows up in
// "translations check" of every site that uses the module.
func TestBundledTemplatesExtract(t *testing.T) {
	t.Parallel()

	app := webthings.New(webthings.Config{})

	err := app.Use(
		banners.Module("banners", "Banners"),
		comments.Module("comments", "Comments"),
		directory.Module("directory", "Directory"),
		documents.Module("documents", "Documents"),
		events.Module("agenda", "Agenda"),
		forms.Module("contact", "Contact"),
		gallery.Module("gallery", "Gallery"),
		lists.Module("faq", "FAQ"),
		newsletter.Module("newsletter", "Newsletter"),
		people.Module("team", "Team"),
		polls.Module("polls", "Polls"),
		publishing.Module("news", "News"),
		redirects.Module(),
		registrations.Module("signup", "Sign-ups", registrations.ForEvents("agenda")),
		stats.Module(),
		submissions.Module("proposals", "Proposals", "news"),
		testimonials.Module("quotes", "Testimonials"),
	)
	if err != nil {
		t.Fatal(err)
	}

	catalog, err := app.TranslationCatalog()
	if err != nil {
		t.Fatal(err)
	}

	if len(catalog.Warnings) > 0 {
		t.Fatalf("extraction warnings:\n%v", catalog.Warnings)
	}

	if len(catalog.Messages) == 0 {
		t.Fatal("no texts extracted from the bundled modules")
	}
}

// TestFormTranslationCatalog checks that the texts of the modules' visitor
// forms (Module.Forms) are extracted once a template renders fields with
// go-form's functions, a theme's or a module's default one, and not before.
func TestFormTranslationCatalog(t *testing.T) {
	t.Parallel()

	// The theme replaces the module's templates and components, whose
	// defaults render the form with go-form.
	formTexts := func(page string, replaced bool) []string {
		t.Helper()

		files := fstest.MapFS{
			"layout.gohtml": {Data: []byte(`{{content}}`)},
			"page.gohtml":   {Data: []byte(page)},
		}

		if replaced {
			for _, name := range []string{"comments/list.gohtml", "comments/thread.gohtml", "comments/form.gohtml"} {
				files[name] = &fstest.MapFile{Data: []byte(`<p>{{.Key}}</p>`)}
			}
		}

		app := webthings.New(webthings.Config{Templates: files})

		if err := app.Use(comments.Module("remarks", "Remarks")); err != nil {
			t.Fatal(err)
		}

		catalog, err := app.TranslationCatalog()
		if err != nil {
			t.Fatal(err)
		}

		var texts []string

		for _, message := range catalog.Messages {
			if slices.Contains(message.References, "module:comments/forms") {
				texts = append(texts, message.ID)
			}
		}

		return texts
	}

	if texts := formTexts(`{{.Title}}`, true); len(texts) > 0 {
		t.Fatalf("form texts without forms: %q", texts)
	}

	if texts := formTexts(`{{.Title}}`, false); !slices.Contains(texts, "Name") {
		t.Fatalf("form texts of the default components: %q", texts)
	}

	texts := formTexts(`{{form_field (commentInput "remarks" .ID) "name" nil}}`, true)

	for _, want := range []string{"Name", "E-mail address", "Optional, never shown on the website.", "Message"} {
		if !slices.Contains(texts, want) {
			t.Errorf("form texts lack %q: %q", want, texts)
		}
	}
}
