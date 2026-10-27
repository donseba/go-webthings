// Package banners shows editor-managed banners anywhere on the site: hero
// slides on the home page, a carousel, or a scheduled announcement bar at the
// top of every page. A banner has a title, subtitle, Markdown text, a button
// (label and address), an image, a tone and an optional window in which it is
// shown. Banners are kept in a manually ordered list and published one by one.
//
// One site usually has several instances, one per place banners appear:
//
//	app.Use(
//		banners.Module("hero", "Hero"),
//		banners.Module("notice", "Notices"),
//	)
//
// The module is not placed on a page; templates show banners with template
// functions:
//
//	{{range banners "hero"}}
//		<section class="slide">
//			{{with .ImageURL}}<img src="{{.}}" alt="">{{end}}
//			<h2>{{.Title}}</h2>
//			<p>{{.Subtitle}}</p>
//			{{.Text}}
//			{{if .ButtonURL}}<a href="{{.ButtonURL}}">{{.ButtonLabel}}</a>{{end}}
//		</section>
//	{{end}}
//
//	{{with banner "notice"}}
//		<div class="notice notice-{{.Tone}}">{{.Title}} {{.Text}}</div>
//	{{end}}
//
// banners returns the active banners of an instance in their order; banner
// returns the first active one, or nil. A banner is active when it is
// published, has a title in the page's language and the current time is
// inside its window (from Starts, until Ends; either may be open). The
// fields templates can use are those of Banner.
package banners

import (
	"context"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"slices"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/content"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/markdown"

	form "github.com/donseba/go-form/v2"
)

//go:embed schema/*.sql
var schemaFiles embed.FS

// Tones are the looks an editor can give a banner; templates use them as a
// CSS class, for example notice-warning.
const (
	// ToneInfo is a neutral announcement; it is the default.
	ToneInfo = "info"
	// ToneSuccess is good news.
	ToneSuccess = "success"
	// ToneWarning asks for attention, such as a closure.
	ToneWarning = "warning"
)

var tones = []string{ToneInfo, ToneSuccess, ToneWarning}

// Fields are the language-independent parts of a banner. Starts and Ends are
// UTC; nil means the window is open on that side.
type Fields struct {
	Image  string     `db:"image_id,null"`
	Tone   string     `db:"tone"`
	Starts *time.Time `db:"starts_at"`
	Ends   *time.Time `db:"ends_at"`
}

// Text is a banner in one language. Body is Markdown. ButtonURL is a path on
// the site, such as /en/contact, or a full URL.
type Text struct {
	Title       string `db:"title"`
	Subtitle    string `db:"subtitle"`
	Body        string `db:"body"`
	ButtonLabel string `db:"button_label"`
	ButtonURL   string `db:"button_url"`
}

// Item is a banner with its translations, as the store keeps it.
type Item = content.Item[Fields, Text]

var (
	errTone   = errors.New("banners_invalid_tone")
	errWindow = errors.New("banners_end_before_start")
	errButton = errors.New("banners_invalid_button_url")
)

func spec() content.Spec[Fields, Text] {
	return content.Spec[Fields, Text]{
		Table: "wt_banner",
		Complete: func(text Text) bool {
			return text.Title != ""
		},
		Check: check,
		Media: func(fields Fields, translations []content.Translation[Text]) []string {
			ids := []string{fields.Image}

			for _, translation := range translations {
				ids = append(ids, markdown.MediaIDs(translation.Text.Body)...)
			}

			return ids
		},
	}
}

func check(fields Fields, translations []content.Translation[Text]) error {
	if !slices.Contains(tones, fields.Tone) {
		return errTone
	}

	if fields.Starts != nil && fields.Ends != nil && !fields.Ends.After(*fields.Starts) {
		return errWindow
	}

	for _, translation := range translations {
		text := translation.Text

		if content.TooLong(200, text.Title) || content.TooLong(500, text.Subtitle) || content.TooLong(64<<10, text.Body) {
			return content.ErrTooLong
		}

		if content.TooLong(100, text.ButtonLabel) || content.TooLong(2000, text.ButtonURL) {
			return content.ErrTooLong
		}

		if !validButtonURL(text.ButtonURL) {
			return errButton
		}
	}

	return nil
}

// validButtonURL accepts an empty address, a path on the site, an anchor or
// a web, mail or phone link (go-form's "link" rule). Other schemes, such as
// javascript:, are refused.
func validButtonURL(address string) bool {
	return address == "" || form.ValidURL("link", address)
}

// live reports whether a published banner is shown at now.
func live(fields Fields, now time.Time) bool {
	if fields.Starts != nil && now.Before(*fields.Starts) {
		return false
	}

	return fields.Ends == nil || now.Before(*fields.Ends)
}

// Service is one Banners instance, for the website's own code.
type Service struct {
	key      string
	items    *content.Collection[Fields, Text]
	frontend *frontend.Frontend
	// adminURL builds admin addresses by route name (webthings.Env.AdminURL).
	adminURL func(name string, args ...any) string
}

// Store gives write access to this instance's banners, for imports and seeds.
// Admin edits go through the same store.
func (s *Service) Store() content.Store[Fields, Text] {
	return s.items.In(s.key)
}

// Of returns the Banners instance with key from a started app, or nil.
func Of(app *webthings.App, key string) *Service {
	service, _ := app.Service(key).(*Service)

	return service
}

// Banner is an active banner as templates see it.
type Banner struct {
	ID       string
	Title    string
	Subtitle string
	// Text is the rendered Markdown text.
	Text        template.HTML
	ButtonLabel string
	// ButtonURL is empty when the banner has no button. It is a template.URL,
	// checked when the banner was saved, so mailto: and tel: links work in
	// href attributes.
	ButtonURL template.URL
	// ImageURL is the banner's image as pages show it, at most media.ImageWidth
	// wide (see frontend.Frontend.Image); ImageSrcset lists smaller
	// copies for a srcset attribute, ImageWidth and ImageHeight are the
	// size of ImageURL and ImageOriginalURL is the uploaded file, for
	// downloads. All are empty when the banner has no image.
	ImageURL         string
	ImageSrcset      string
	ImageOriginalURL string
	ImageWidth       int
	ImageHeight      int
	// Tone is ToneInfo, ToneSuccess or ToneWarning.
	Tone string
	// Starts and Ends are the window the banner is shown in; zero when open.
	Starts time.Time
	Ends   time.Time
}

// Active returns the banners shown now in locale, in their order.
func (s *Service) Active(ctx context.Context, locale string) ([]Banner, error) {
	return s.activeAt(ctx, locale, time.Now())
}

func (s *Service) activeAt(ctx context.Context, locale string, now time.Time) ([]Banner, error) {
	items, err := s.items.ListPublished(ctx, s.key, locale)
	if err != nil {
		return nil, err
	}

	banners := make([]Banner, 0, len(items))

	for _, item := range items {
		if live(item.Fields, now) {
			banners = append(banners, s.banner(ctx, item, locale))
		}
	}

	return banners, nil
}

func (s *Service) banner(ctx context.Context, item Item, locale string) Banner {
	text, _ := item.Text(locale)
	shown := s.frontend.Image(ctx, item.Fields.Image)

	banner := Banner{
		ID:               item.ID,
		Title:            text.Title,
		Subtitle:         text.Subtitle,
		Text:             s.frontend.Markdown(ctx, text.Body),
		ButtonLabel:      text.ButtonLabel,
		ImageURL:         shown.URL,
		ImageSrcset:      shown.Srcset,
		ImageOriginalURL: shown.OriginalURL,
		ImageWidth:       shown.Width,
		ImageHeight:      shown.Height,
		Tone:             item.Fields.Tone,
	}

	// Saving checked the address already; checking again keeps anything
	// written around the store out of href attributes.
	if validButtonURL(text.ButtonURL) {
		banner.ButtonURL = template.URL(text.ButtonURL) //nolint:gosec // validButtonURL allows only safe schemes.
	}

	if item.Fields.Starts != nil {
		banner.Starts = *item.Fields.Starts
	}

	if item.Fields.Ends != nil {
		banner.Ends = *item.Fields.Ends
	}

	return banner
}

func schema() fs.FS {
	files, _ := fs.Sub(schemaFiles, "schema")

	return files
}
