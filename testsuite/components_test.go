package testsuite

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/modules/comments"
	"github.com/donseba/go-webthings/modules/newsletter"
	"github.com/donseba/go-webthings/modules/polls"
	"github.com/donseba/go-webthings/modules/registrations"
	"github.com/donseba/go-webthings/sitetest"
)

// TestComponents renders the default components of the interactive modules
// on a page, posts to them, and lets a theme replace them.
func TestComponents(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"layout.gohtml": `<head><link rel="stylesheet" href="{{asset "webthings.css"}}"></head><body>{{content}}</body>`,
		"page.gohtml": `<main id="content"><h1>{{.Title}}</h1>
{{commentThread "remarks" "home"}}
{{pollBox "survey" ""}}
{{newsletterForm "letters"}}
{{registrationForm "signup" "open-day"}}
{{registrationForm "signup" "not-configured"}}
</main>`,
		// The theme designs one instance's form itself: by key first.
		"letters/form.gohtml": `<p class="own-signup">{{.Title}} → {{.Action}}</p>`,
	})

	s := sitetest.StartWith(t, sitetest.Options{Templates: dir},
		comments.Module("remarks", "Comments"),
		polls.Module("survey", "Polls"),
		newsletter.Module("letters", "Newsletter"),
		registrations.Module("signup", "Sign-ups"),
	)

	ctx := context.Background()

	remarks := s.App.Service("remarks").(*comments.Service)
	if err := remarks.SaveSettings(ctx, comments.Settings{Moderated: false}); err != nil {
		t.Fatal(err)
	}

	survey := s.App.Service("survey").(*polls.Service)

	poll, err := survey.Store().Save(ctx, content.Draft[polls.Fields, polls.Text]{
		Fields: polls.Fields{Options: polls.Lines{"tea", "coffee"}},
		Translations: []content.Translation[polls.Text]{{
			Locale: "en",
			Text:   polls.Text{Question: "Tea or coffee?", Options: polls.Lines{"Tea", "Coffee"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := survey.Store().Publish(ctx, poll.ID, "", "en"); err != nil {
		t.Fatal(err)
	}

	signup := s.App.Service("signup").(*registrations.Service)

	err = signup.SaveSettings(ctx, registrations.Settings{
		SubjectID:  "open-day",
		Title:      "Open day",
		Open:       true,
		Capacity:   10,
		AdultPrice: 1250,
	})
	if err != nil {
		t.Fatal(err)
	}

	visitor := s.Visitor()

	visitor.Expect("/en/", http.StatusOK,
		`<link rel="stylesheet" href="/_/webthings.css?v=`,
		`<section class="wt-comments" id="remarks-comments"`,
		`<p class="wt-comments__empty wt-meta">No comments yet.</p>`,
		`<div class="wt-comment-form" id="remarks">`,
		`action="/_/remarks/post"`,
		`class="wt-form"`,
		`<input type="hidden" name="back" value="/en/#remarks">`,
		`id="remarks-subject"`,
		`value="home"`,
		`<h3 class="wt-poll__question" id="survey-`+poll.ID+`-question">Tea or coffee?</h3>`,
		// The options are a fieldset, the question its legend.
		`<fieldset class="wt-form__field wt-form__fieldset">`,
		`class="wt-form__label"`,
		`id="survey-`+poll.ID+`-option_label">`,
		`<div class="wt-form__options">`,
		`value="coffee"`,
		`<p class="own-signup">`,
		`→ /_/letters/subscribe</p>`,
		`<section class="wt-registration" id="signup"`,
		`id="signup-subject"`,
		`value="open-day"`,
		// At most the places left.
		`id="signup-adults"`,
		`max="10"`,
		`<dd>10 of 10</dd>`,
	)
	visitor.ExpectMissing("/en/", "wt-newsletter")

	// A comment shows up in the thread, with the result of the post.
	status, _, landed := visitor.Send("/_/remarks/post", url.Values{
		"subject": {"home"},
		"name":    {"Ada"},
		"body":    {"Lovely <b>place</b>"},
		"back":    {"/en/#remarks"},
	})
	if status != http.StatusOK || !strings.HasPrefix(landed, "/en/?remarks=sent") {
		t.Fatalf("comment: %d %s", status, landed)
	}

	visitor.Expect(landed, http.StatusOK,
		`Comments (1)`,
		`<strong class="wt-comment__name">Ada</strong>`,
		`Lovely &lt;b&gt;place&lt;/b&gt;`,
		`<p class="wt-notice" role="status">`,
		`Thank you for your comment.`,
	)

	// A vote shows the results with bars: meters of the percentages,
	// without a style attribute.
	status, _, landed = visitor.Send("/_/survey/vote", url.Values{
		"poll":   {poll.ID},
		"option": {"coffee"},
		"back":   {"/en/#poll-" + poll.ID},
	})
	if status != http.StatusOK || !strings.Contains(landed, "survey=voted") {
		t.Fatalf("vote: %d %s", status, landed)
	}

	visitor.Expect(landed, http.StatusOK,
		"Thank you for voting.",
		`<li class="wt-poll__result wt-poll__result--chosen">`,
		`<meter class="wt-poll__bar" min="0" max="100" value="100" aria-hidden="true">100%</meter>`,
		"Votes: 1",
	)

	// A registration shows the code and the places left.
	status, _, landed = visitor.Send("/_/signup/register", url.Values{
		"subject": {"open-day"},
		"name":    {"Ada"},
		"email":   {"ada@example.test"},
		"adults":  {"2"},
		"back":    {"/en/#signup-registration"},
	})
	if status != http.StatusOK || !strings.Contains(landed, "signup=registered") {
		t.Fatalf("register: %d %s", status, landed)
	}

	visitor.Expect(landed, http.StatusOK, "Your registration code is ", "<dd>8 of 10</dd>")

	// The stylesheet is served by core; its current version may be cached
	// for long, any other request revalidates.
	_, home := visitor.Response("/en/")

	link := regexp.MustCompile(`href="(/_/webthings\.css\?v=[0-9a-f]{16})"`).FindStringSubmatch(home)
	if link == nil {
		t.Fatal("webthings.css is not linked by its version")
	}

	response, css := visitor.Response(link[1])
	if response.StatusCode != http.StatusOK || !strings.Contains(css, "--wt-accent") ||
		!strings.HasPrefix(response.Header.Get("Content-Type"), "text/css") ||
		!strings.Contains(response.Header.Get("Cache-Control"), "immutable") || response.Header.Get("ETag") == "" {
		t.Fatalf("webthings.css: %d %v\n%s", response.StatusCode, response.Header, css)
	}

	if old, _ := visitor.Response("/_/webthings.css?v=1"); old.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("an old version is cached: %v", old.Header)
	}
}

// TestComponentOverrides lets a theme replace a module's component by module
// type and core's share bar, and writes the defaults as starting points.
func TestComponentOverrides(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	writeFiles(t, dir, map[string]string{
		"layout.gohtml": `<link href="{{asset "webthings.css"}}"><body>{{content}}</body>`,
		"page.gohtml": `<main id="content">{{commentThread "remarks" "home"}}{{newsletterForm "letters"}}` +
			`{{shareBar "" .Title}}</main>`,
		"comments/form.gohtml":     `<p class="type-form">{{.Subject}} {{template "partials/note.gohtml"}}</p>`,
		"site/share.gohtml":        `<p class="own-share">{{len .Links}} {{.URL}}</p>`,
		"partials/note.gohtml":     `with a partial`,
		"assets/webthings.css":     `.own {}`,
		"newsletter/signup.gohtml": `<main id="content">signup</main>`,
	})

	var written []string

	s := sitetest.StartWith(t, sitetest.Options{
		Templates: dir,
		Configure: func(config *webthings.Config) {
			config.BaseURL = "https://example.com"
		},
		Prepare: func(app *webthings.App) error {
			for _, key := range []string{"site", "remarks", "letters"} {
				files, err := app.WriteTemplates(filepath.Join(dir, "scaffold"), key)
				if err != nil {
					return err
				}

				written = append(written, files...)
			}

			return nil
		},
	},
		comments.Module("remarks", "Comments"),
		newsletter.Module("letters", "Newsletter"),
	)

	// The theme's own stylesheet replaces core's.
	s.Expect("/en/", http.StatusOK,
		`<link href="/theme/default/webthings.css?v=`,
		`<section class="wt-comments" id="remarks-comments"`,
		`<p class="type-form">home with a partial</p>`,
		`<p class="own-share">7 https://example.com/en/</p>`,
		`<section class="wt-newsletter" id="letters"`,
	)

	for _, file := range []string{
		"site/share.gohtml",
		"remarks/thread.gohtml",
		"remarks/form.gohtml",
		"letters/form.gohtml",
		"letters/signup.gohtml",
	} {
		source, err := os.ReadFile(filepath.Join(dir, "scaffold", file))
		if err != nil || !strings.HasPrefix(string(source), "{{/* @dot ") {
			t.Fatalf("%s: %v\n%s (wrote %v)", file, err, source, written)
		}
	}

	// A module may not give a component the name of one of its templates.
	app := webthings.New(webthings.Config{StateDir: t.TempDir()})
	module := comments.Module("other", "Other")
	module.Components = append(module.Components, module.Templates[0])

	if err := app.Use(module); err == nil {
		t.Fatal("component named like a template was accepted")
	}
}
