package banners

import (
	"context"
	"embed"
	"errors"
	"time"

	webthings "github.com/donseba/go-webthings"
	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/admin/editor"
	"github.com/donseba/go-webthings/content"

	form "github.com/donseba/go-form/v2"
)

//go:embed translations
var files embed.FS

// Module adds a Banners instance. key is used in URLs of the admin and in the
// template functions and must be unique on the site; title is shown in the
// admin.
func Module(key, title string) webthings.Module {
	return webthings.Module{
		Type:   "banners",
		Key:    key,
		Title:  title,
		Schema: schema(),
		Setup:  setup,
		Funcs:  funcs(),
	}
}

func setup(env webthings.Env) (webthings.Parts, error) {
	itemsSpec := spec()
	itemsSpec.Changed = env.Changed

	items, err := content.NewCollection(env.DB, itemsSpec)
	if err != nil {
		return webthings.Parts{}, err
	}

	service := &Service{
		key:      env.Key,
		items:    items,
		frontend: env.Frontend,
		adminURL: env.AdminURL,
	}

	return webthings.Parts{
		Admin:   []admin.Section{section(env, service)},
		Service: service,
		Links:   service.linkSources,
	}, nil
}

// bannerInput is one language of a banner in the editor. Image, tone and the
// window are shared by all languages. Editors enter Starts and Ends in the
// site's timezone (see admin.Admin.MapForm).
type bannerInput struct {
	Title       string                    `form:"input,text"           label:"Title"          required:"true" maxLength:"200"   trim:"true"`
	Subtitle    string                    `form:"input,text"           label:"Subtitle"                       maxLength:"500"   trim:"true"`
	Body        string                    `form:"textarea"             label:"Text"                           maxLength:"65536"             class:"markdown"`
	ButtonLabel string                    `form:"input,text"           label:"Button label"                   maxLength:"100"   trim:"true"`
	ButtonURL   string                    `form:"input,text"           label:"Button address"                 maxLength:"2000"  trim:"true"                  url:"link" urlMsg:"The button address must be a path on this site, such as /en/contact, an anchor such as #contact, a full URL, or a mailto: or tel: link." description:"A path on this site, such as /en/contact, an anchor on the page, such as #contact, a full URL, or a mailto: or tel: link."`
	Image       form.SortedSelect[string] `form:"dropdown"             label:"Image"                                                                                                                                                                                                                                                                                                                                                                             placeholder:"—"`
	Tone        form.SortedSelect[string] `form:"dropdown"             label:"Tone"                                                                                                                                                                                                                                                                                                                                                                                              translate:"true"`
	Starts      *time.Time                `form:"input,datetime-local" label:"Show from"                                                                                                                                                                                                                                 description:"Empty shows the banner as soon as it is published."`
	Ends        *time.Time                `form:"input,datetime-local" label:"Show until"                                                                                                                                                                                                                                description:"Empty shows the banner until it is unpublished."`
}

func section(env webthings.Env, service *Service) admin.Section {
	return editor.New(editor.Config[Fields, Text, bannerInput]{
		ID:       env.Key,
		Title:    env.Title,
		Icon:     "megaphone",
		NewLabel: "New banner",
		Store:    service.items.In(env.Key),
		Locales:  env.Locales,
		Files:    files,
		Label: func(text Text) string {
			return text.Title
		},
		Notes: func(ctx context.Context, items []Item, _ string) map[string]string {
			now := time.Now()
			notes := make(map[string]string, len(items))

			for _, item := range items {
				if note := windowNote(item.Fields, now); note != "" {
					notes[item.ID] = admin.T(ctx, note)
				}
			}

			return notes
		},
		Form: func(ctx context.Context, item Item, locale string) (bannerInput, error) {
			return bannerForm(ctx, env, item, locale)
		},
		Apply: func(_ context.Context, in bannerInput, draft *content.Draft[Fields, Text], locale string) error {
			draft.Fields = Fields{
				Image:  in.Image.Get(),
				Tone:   in.Tone.Get(),
				Starts: utc(in.Starts),
				Ends:   utc(in.Ends),
			}

			draft.Translations = content.SetText(draft.Translations, locale, Text{
				Title:       in.Title,
				Subtitle:    in.Subtitle,
				Body:        in.Body,
				ButtonLabel: in.ButtonLabel,
				ButtonURL:   in.ButtonURL,
			})

			return nil
		},
		Issue: issue,
	})
}

func bannerForm(ctx context.Context, env webthings.Env, item Item, locale string) (bannerInput, error) {
	text, _ := item.Text(locale)
	fields := item.Fields

	image, err := editor.MediaChoices(ctx, env.Media, true, fields.Image)
	if err != nil {
		return bannerInput{}, err
	}

	if fields.Tone == "" {
		fields.Tone = ToneInfo
	}

	tone := toneChoices(fields.Tone)

	return bannerInput{
		Title:       text.Title,
		Subtitle:    text.Subtitle,
		Body:        text.Body,
		ButtonLabel: text.ButtonLabel,
		ButtonURL:   text.ButtonURL,
		Image:       image,
		Tone:        tone,
		Starts:      fields.Starts,
		Ends:        fields.Ends,
	}, nil
}

// toneChoices are the tones of a banner; go-form translates the labels
// (translate:"true").
func toneChoices(selected string) form.SortedSelect[string] {
	return form.NewSortedSelect(map[string]string{
		ToneInfo:    "Information",
		ToneSuccess: "Success",
		ToneWarning: "Warning",
	}).Selected(selected)
}

// utc is t in UTC, as banners keep their window; nil stays nil.
func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}

	inUTC := t.UTC()

	return &inUTC
}

// windowNote tells editors in the overview that a banner is not shown because
// of its window: it has not started yet or it has ended.
func windowNote(fields Fields, now time.Time) string {
	switch {
	case fields.Starts != nil && now.Before(*fields.Starts):
		return "Upcoming"
	case fields.Ends != nil && !now.Before(*fields.Ends):
		return "Expired"
	}

	return ""
}

func issue(err error) string {
	switch {
	case errors.Is(err, errTone):
		return "Choose a tone."
	case errors.Is(err, errWindow):
		return "Show until must be after show from."
	case errors.Is(err, errButton):
		return "The button address must be a path on this site, such as /en/contact, or a full URL."
	}

	return ""
}
