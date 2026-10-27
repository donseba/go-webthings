// Package mediaadmin is the Media section of the admin: the media library,
// where editors upload files, write their titles and alternative texts per
// language and choose the focal point of images.
package mediaadmin

import (
	"embed"
	"errors"
	"fmt"
	"image"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/locales"
	"github.com/donseba/go-webthings/media"

	form "github.com/donseba/go-form/v2"
)

//go:embed assets templates translations
var files embed.FS

// LibraryView is the data of the media library: a page of files as cards.
type LibraryView struct {
	Cards []Card
	// Upload is the upload form, for users who may create media; nil for
	// others.
	Upload       *form.RenderModel
	UploadErrors form.FieldErrors
	// MaxMB is the largest upload in megabytes.
	MaxMB int64
	// TrashURL shows the deleted files in the admin's trash.
	TrashURL string
	// Pager pages through a large library.
	Pager admin.Pager
}

// libraryPage is the number of files per page of the library, a multiple of
// its two and three columns.
const libraryPage = 48

// Card is one file in the library: its name, a thumbnail for images and
// Details such as "image/png · 12 KB · 800×600".
type Card struct {
	ID       string
	Filename string
	URL      string
	EditURL  string
	Image    bool
	Details  string
}

// AssetView is the data of the screen of one file, with its texts in one
// language. Markdown is the snippet that places the file in Markdown
// content.
type AssetView struct {
	Card     Card
	Markdown string
	Tabs     []admin.LocaleTab
	Form     form.RenderModel
	Errors   form.FieldErrors
	// ReadOnly is set for users who may view the file but not edit it: Form
	// is then read-only (disabled, without its button).
	ReadOnly bool
	Delete   *form.RenderModel
	// Focus places the focal point of an image; nil for other files.
	Focus *FocusView
}

// FocusView shows an image with a marker on its focal point, previews of
// common crops and a form with the point in percent, which works without
// JavaScript; admin/assets/focalpoint.js lets editors click, drag or move
// the marker with the arrow keys instead. Nothing is placed with a style
// attribute, which a Content-Security-Policy blocks: the marker is placed
// by the script, the previews by classes until the script places them
// exactly.
type FocusView struct {
	URL string
	// Width and Height are the image's size as shown.
	Width  int
	Height int
	// X and Y place the marker, in percent from the top left.
	X        string
	Y        string
	Previews []CropPreview
	Form     form.RenderModel
	Errors   form.FieldErrors
}

// CropPreview shows the image cropped to Width:Height by the browser, with
// Class, the class of focal-points.css (media.FocalPoint.PositionClass)
// that shows about the part a crop variant of those proportions shows.
// The box's proportions are the class focal-crop-<Width>-<Height> of
// focalpoint.css, one of previewRatios.
type CropPreview struct {
	Width  int
	Height int
	Class  string
}

// previewRatios are the crops editors see before they save a focal point;
// assets/focalpoint.css has a class for each.
var previewRatios = []image.Point{{1, 1}, {4, 3}, {16, 9}, {3, 4}}

type textInput struct {
	Title   string `form:"input,text" label:"Title"            maxLength:"200"`
	AltText string `form:"textarea"   label:"Alternative text" maxLength:"2000" description:"Describes the image for visitors who cannot see it, such as people who use a screen reader. Texts that show the image use it unless they give their own."`
}

// focusInput is the focal point in percent of the image's width and height.
type focusInput struct {
	FocalX float64 `form:"input,number" label:"From the left (%)" min:"0" max:"100"`
	FocalY float64 `form:"input,number" label:"From the top (%)"  min:"0" max:"100"`
}

// assetForms are the forms of the asset screen as submitted, with their
// errors; nil inputs show what is stored.
type assetForms struct {
	text        *textInput
	textErrors  form.FieldErrors
	focus       *focusInput
	focusErrors form.FieldErrors
	// message shows as a toast.
	message string
	status  int
}

type section struct {
	admin   *admin.Admin
	library *media.Service
	locales locales.Provider
}

// New returns the Media section of the admin: the library, uploads, the
// texts of each file per language and the focal point of images.
func New(library *media.Service, provider locales.Provider) admin.Section {
	return admin.Section{
		ID:    "media",
		Title: "Media",
		Icon:  "image",
		Files: files,
		// The focal point marker and crop previews (admin/assets/focalpoint.js).
		Stylesheet:   "assets/focalpoint.css",
		MaxBodyBytes: library.MaxUploadSize() + 128<<10,
		Trash:        trash(library),
		Routes: func(routes *admin.Routes) {
			s := section{admin: routes.Admin(), library: library, locales: provider}

			routes.Get("", admin.View, s.list).As("index")
			routes.Post("", admin.Create, s.upload)
			routes.Get("/picker", admin.View, s.picker).As("picker")
			routes.Get("/{id}", admin.View, s.edit).As("edit")
			routes.Post("/{id}", admin.Edit, s.save)
			routes.Post("/{id}/focus", admin.Edit, s.saveFocus).As("focus")
			routes.Post("/{id}/delete", admin.Delete, s.delete).As("delete")
		},
	}
}

func (s section) card(r *http.Request, asset media.Asset) Card {
	details := fmt.Sprintf("%s · %d KB", asset.MIMEType, (asset.SizeBytes+1023)/1024)
	if asset.IsImage() {
		details += fmt.Sprintf(" · %d×%d", asset.Width, asset.Height)
	}

	return Card{
		ID:       asset.ID,
		Filename: asset.Filename,
		URL:      s.library.URL(r.Context(), asset.ID),
		EditURL:  s.admin.URL("admin.media.edit", "id", asset.ID),
		Image:    asset.IsImage(),
		Details:  details,
	}
}

// uploadInput is the upload form. The library detects the type of a file
// from its content (media.Upload); accept guides the browser's file chooser,
// and maxSize is the library's limit (media.Service.MaxUploadSize).
type uploadInput struct {
	File *multipart.FileHeader `form:"input,file" label:"File" name:"file" required:"true" requiredMsg:"Choose a file to upload." maxSize:"10MB" accept:".jpg,.jpeg,.png,.gif,.pdf,.txt,.csv,.zip,.docx,.xlsx,.pptx,.odt,.ods,.odp,image/jpeg,image/png,image/gif,application/pdf,text/plain,text/csv" acceptMsg:"This file type is not supported. Upload JPEG, PNG or GIF images, PDF, plain text, CSV, ZIP, Word, Excel, PowerPoint or OpenDocument files; WebP and SVG are not accepted." maxSizeMsg:"This file is too large."`
}

func (s section) list(w http.ResponseWriter, r *http.Request) {
	s.renderList(w, r, nil, "", http.StatusOK)
}

// renderList shows the library, with errs at the upload form.
func (s section) renderList(w http.ResponseWriter, r *http.Request, errs form.FieldErrors, message string, status int) {
	// The library is read whole, as a search looks in every translation,
	// and paged in memory; only the shown page builds its cards.
	page, err := admin.ListPage[media.Asset]{
		Path:   s.admin.URL("admin.media.index"),
		Size:   libraryPage,
		All:    s.library.ListWithTranslations,
		Fields: searchFields,
	}.Read(r)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	// The message shows as a toast.
	r = admin.WithError(r, message)

	view := LibraryView{
		UploadErrors: errs,
		MaxMB:        s.library.MaxUploadSize() >> 20,
		TrashURL:     s.admin.TrashURL("media"),
		Pager:        page.Pager,
	}

	if s.admin.Can(r, admin.Permission{Section: "media", Action: admin.Create}) {
		upload := s.admin.Form(r, uploadInput{}, s.admin.URL("admin.media.index"), "Upload")
		view.Upload = &upload
	}

	for _, asset := range page.Rows {
		view.Cards = append(view.Cards, s.card(r, asset))
	}

	s.admin.Render(w, r, admin.Page{
		Title:    "Media",
		Template: "templates/library.gohtml",
		Files:    files,
		Data:     view,
		Status:   status,
	})
}

// searchFields are what a search of the library looks through: the file
// name, and the titles and alternative texts in every language.
func searchFields(asset media.Asset) []string {
	fields := []string{asset.Filename}

	for _, text := range asset.Translations {
		fields = append(fields, text.Title, text.AltText)
	}

	return fields
}

func (s section) upload(w http.ResponseWriter, r *http.Request) {
	var input uploadInput

	errs, err := s.admin.Bind(r, &input)
	if err != nil {
		s.admin.BadRequest(w, r)

		return
	}

	if len(errs) > 0 {
		s.renderList(w, r, errs, "", http.StatusUnprocessableEntity)

		return
	}

	file, err := input.File.Open()
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	defer func() { _ = file.Close() }()

	asset, err := s.library.Upload(r.Context(), input.File.Filename, file)

	var message string

	switch {
	case errors.Is(err, media.ErrUnsupportedType):
		message = "This file type is not supported. Upload JPEG, PNG or GIF images, PDF, plain text, CSV, ZIP, Word, Excel, PowerPoint or OpenDocument files; WebP and SVG are not accepted."
	case errors.Is(err, media.ErrTooLarge):
		message = "This file is too large."
	case errors.Is(err, media.ErrInvalid):
		message = "This file could not be read."
	case err != nil:
		s.admin.Fail(w, r, err)

		return
	}

	if message != "" {
		s.renderList(w, r, nil, s.admin.T(r, message), http.StatusUnprocessableEntity)

		return
	}

	s.admin.Redirect(w, r, s.admin.URL("admin.media.edit", "id", asset.ID))
}

func (s section) locale(r *http.Request) (string, locales.Config, error) {
	config, err := s.locales.Configuration(r.Context())
	if err != nil {
		return "", config, err
	}

	return admin.ContentLocale(r, config), config, nil
}

func (s section) edit(w http.ResponseWriter, r *http.Request) {
	s.renderAsset(w, r, assetForms{status: http.StatusOK})
}

func (s section) renderAsset(w http.ResponseWriter, r *http.Request, forms assetForms) {
	asset, err := s.library.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, media.ErrNotFound) {
		http.NotFound(w, r)

		return
	}

	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	locale, config, err := s.locale(r)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	text := asset.Text(locale)
	if forms.text == nil {
		forms.text = &textInput{Title: text.Title, AltText: text.AltText}
	}

	// The message shows as a toast; errors of single fields stay inline.
	r = admin.WithError(r, forms.message)

	// Only editors see the focal point editor and may change the texts;
	// saving them needs Edit.
	canEdit := s.admin.Can(r, admin.Permission{Section: "media", Action: admin.Edit})
	query := url.Values{"locale": {locale}}
	view := AssetView{
		Card:     s.card(r, asset),
		Markdown: fmt.Sprintf("![%s](media:%s)", text.AltText, asset.ID),
		Form:     s.admin.Form(r, *forms.text, s.admin.URL("admin.media.edit", "id", asset.ID, query), "Save"),
		Errors:   forms.textErrors,
		ReadOnly: !canEdit,
	}

	view.Form.Info.ReadOnly = view.ReadOnly

	if asset.IsImage() && canEdit {
		view.Focus = s.focusView(r, asset, forms.focus, forms.focusErrors, query)
	}

	if !asset.IsImage() {
		view.Markdown = fmt.Sprintf("[%s](media:%s)", asset.Filename, asset.ID)
	}

	if s.admin.Can(r, admin.Permission{Section: "media", Action: admin.Delete}) {
		remove := s.admin.Form(r, struct{}{}, s.admin.URL("admin.media.delete", "id", asset.ID), "Move to trash")
		view.Delete = &remove
	}

	view.Tabs = admin.LocaleTabs(
		config.Enabled,
		locale,
		s.admin.URL("admin.media.edit", "id", asset.ID),
		func(code string) (bool, bool) {
			text := asset.Text(code)

			return text.AltText != "" || !asset.IsImage() && text.Title != "", false
		},
	)

	// A file name is shown as written, never translated.
	s.admin.Render(w, r, admin.Page{
		Title:    "Media",
		Label:    asset.Filename,
		Template: "templates/asset.gohtml",
		Files:    files,
		Data:     view,
		Status:   forms.status,
	})
}

// focusView is the focal point editor of an image. The marker and the
// previews show the submitted point when the form is shown again with
// errors, and the stored one otherwise.
func (s section) focusView(
	r *http.Request,
	asset media.Asset,
	input *focusInput,
	errs form.FieldErrors,
	query url.Values,
) *FocusView {
	point := asset.FocalPoint()

	if input == nil {
		input = &focusInput{FocalX: percent(point.X), FocalY: percent(point.Y)}
	} else {
		submitted := media.FocalPoint{X: input.FocalX / 100, Y: input.FocalY / 100}
		if submitted.Valid() {
			point = submitted
		}
	}

	view := &FocusView{
		URL:    s.library.URL(r.Context(), asset.ID),
		Width:  asset.Width,
		Height: asset.Height,
		X:      strconv.FormatFloat(percent(point.X), 'f', -1, 64),
		Y:      strconv.FormatFloat(percent(point.Y), 'f', -1, 64),
		Form:   s.admin.Form(r, *input, s.admin.URL("admin.media.focus", "id", asset.ID, query), "Save focal point"),
		Errors: errs,
	}

	for _, ratio := range previewRatios {
		view.Previews = append(view.Previews, CropPreview{
			Width:  ratio.X,
			Height: ratio.Y,
			Class:  point.PositionClass(asset.Width, asset.Height, ratio),
		})
	}

	return view
}

// percent is a fraction in percent with one decimal, as the form shows it.
func percent(fraction float64) float64 {
	return math.Round(fraction*1000) / 10
}

func (s section) save(w http.ResponseWriter, r *http.Request) {
	var input textInput

	errs, err := s.admin.Bind(r, &input)
	if err != nil {
		s.admin.BadRequest(w, r)

		return
	}

	if len(errs) > 0 {
		s.renderAsset(w, r, assetForms{text: &input, textErrors: errs, status: http.StatusUnprocessableEntity})

		return
	}

	locale, _, err := s.locale(r)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	err = s.library.SaveTranslation(r.Context(), r.PathValue("id"), media.Translation{
		Locale:  locale,
		Title:   input.Title,
		AltText: input.AltText,
	})
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	s.admin.Redirect(w, r, s.admin.URL("admin.media.edit", "id", r.PathValue("id"), url.Values{"locale": {locale}}))
}

// saveFocus stores the focal point of an image, which crops keep in view.
func (s section) saveFocus(w http.ResponseWriter, r *http.Request) {
	var input focusInput

	errs, err := s.admin.Bind(r, &input)
	if err != nil {
		s.admin.BadRequest(w, r)

		return
	}

	if len(errs) > 0 {
		s.renderAsset(w, r, assetForms{focus: &input, focusErrors: errs, status: http.StatusUnprocessableEntity})

		return
	}

	point := media.FocalPoint{X: input.FocalX / 100, Y: input.FocalY / 100}

	err = s.library.SetFocalPoint(r.Context(), r.PathValue("id"), point)
	if errors.Is(err, media.ErrNotFound) || errors.Is(err, media.ErrInvalid) {
		// Validation keeps the point inside the image, so only files that
		// are not images in the library remain.
		http.NotFound(w, r)

		return
	}

	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	locale, _, err := s.locale(r)
	if err != nil {
		s.admin.Fail(w, r, err)

		return
	}

	s.admin.Redirect(w, r, s.admin.URL("admin.media.edit", "id", r.PathValue("id"), url.Values{"locale": {locale}}))
}

func (s section) delete(w http.ResponseWriter, r *http.Request) {
	err := s.library.Delete(r.Context(), r.PathValue("id"))
	if errors.Is(err, media.ErrInUse) {
		s.renderAsset(w, r, assetForms{
			message: s.admin.T(r, "This file is used by content, perhaps in the trash, and cannot be deleted."),
			status:  http.StatusConflict,
		})

		return
	}

	if err != nil && !errors.Is(err, media.ErrNotFound) {
		s.admin.Fail(w, r, err)

		return
	}

	s.admin.Redirect(w, r, s.admin.URL("admin.media.index"))
}
