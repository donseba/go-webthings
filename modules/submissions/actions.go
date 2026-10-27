package submissions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"slices"
	"time"

	"github.com/donseba/go-webthings/admin"
	"github.com/donseba/go-webthings/frontend"
	"github.com/donseba/go-webthings/media"

	form "github.com/donseba/go-form/v2"
)

const (
	// maxImage is the largest accepted image in bytes.
	maxImage = 5 << 20
	// bodyLimit leaves room for the text fields next to the largest image.
	bodyLimit = maxImage + 256<<10
	// Each visitor can send a few proposals per hour.
	throttleLimit  = 5
	throttleWindow = time.Hour
)

// imageTypes are the accepted image formats, detected from the bytes.
var imageTypes = []string{"image/jpeg", "image/png", "image/gif"}

// Results are the values of ?<key>= after a visitor's post.
const (
	// ResultSent is a proposal that was received and waits for an editor.
	ResultSent = "sent"
	// ResultInvalid is a proposal with a missing or wrong field, or an
	// image of another type; nothing was saved.
	ResultInvalid = "invalid"
	// ResultThrottled is the answer to a visitor who sent too many proposals.
	ResultThrottled = frontend.ResultThrottled
)

// FormView is the data of submissions/form.gohtml.
type FormView struct {
	// Key is the instance key; results arrive as ?<key>=<result>, and the
	// form returns the visitor to the element with the id Key.
	Key string
	// Title is the title of the page the module is placed on.
	Title string
	// Action is the address the form posts to.
	Action string
	// Result is "sent" after a proposal was received, "invalid" when it was
	// refused, "throttled" after too many proposals, empty otherwise.
	Result string
	// MaxImageMB is the largest image visitors can add, in megabytes.
	MaxImageMB int
	// Form is the form's model (ProposalInput) for go-form's template
	// functions, such as {{form_open .Form}} and {{form_field .Form "title"
	// nil}}.
	Form form.RenderModel
}

// proposalForm is the form of ProposalInput, posting to the instance's
// address for proposals, for templates (frontend.VisitorForm): it returns
// to the element with the id of the instance key, which goes before its
// fields' ids, such as tips-title.
func (s *Service) proposalForm(ctx context.Context) form.RenderModel {
	return frontend.VisitorForm(ctx, ProposalInput{}, s.submitURL(), s.key)
}

// ProposalInput is the form a visitor posts to /_/<key>/submit: a name, an
// e-mail address, a title, the text and optionally an image. Themes render
// its fields with form_field, with their limits (FormView.Form), or write
// inputs with these names; a form with the image is sent as
// multipart/form-data.
type ProposalInput struct {
	Name  string                `form:"input,text"  name:"name"  label:"Name"           required:"true" maxLength:"200"   line:"true" autocomplete:"name"`
	Email string                `form:"input,email" name:"email" label:"E-mail address" required:"true" maxLength:"254"               autocomplete:"email" validate:"mail"`
	Title string                `form:"input,text"  name:"title" label:"Title"          required:"true" maxLength:"200"   line:"true"`
	Text  string                `form:"textarea"    name:"text"  label:"Text"           required:"true" maxLength:"20000"`
	Image *multipart.FileHeader `form:"input,file"  name:"image" label:"Image"                                                                                             accept:"image/jpeg,image/png,image/gif,.jpg,.jpeg,.png,.gif" maxSize:"5MB"`
}

// registerActions registers the action that receives the visitors'
// proposals, at /_/<key>/submit, on the instance's action router
// (webthings.Env.Actions).
func (s *Service) registerActions() {
	routes := s.actions

	routes.Use(frontend.Limits(s.key, frontend.ActionLimits{
		Body:   bodyLimit,
		Posts:  throttleLimit,
		Window: throttleWindow,
	}))
	routes.Post("/submit", s.submit).As("submit")
}

func (s *Service) submit(w http.ResponseWriter, r *http.Request) {
	// A form without an image may be sent url-encoded.
	err := r.ParseMultipartForm(1 << 20)
	if err != nil && !errors.Is(err, http.ErrNotMultipart) {
		frontend.NothingChanged(r)
		frontend.Back(w, r, s.key, ResultInvalid)

		return
	}

	if frontend.IsSpam(r) {
		frontend.Back(w, r, s.key, ResultSent)

		return
	}

	var in ProposalInput

	errs, err := s.frontend.BindForm(r, &in)
	if err != nil || len(errs) > 0 {
		s.frontend.KeepForm(w, r, s.key, in, errs)
		frontend.Back(w, r, s.key, ResultInvalid)

		return
	}

	ctx := r.Context()
	proposal := Proposal{
		Name:   in.Name,
		Email:  in.Email,
		Title:  in.Title,
		Body:   in.Text,
		Locale: s.frontend.BackLocale(r),
	}

	// Check the text first, so a refused proposal leaves no image behind;
	// the service checks it again for code that adds proposals itself.
	if _, err := clean(proposal); err != nil {
		s.frontend.KeepForm(w, r, s.key, in, nil)
		frontend.Back(w, r, s.key, ResultInvalid)

		return
	}

	proposal.MediaID, err = s.upload(ctx, in.Image)
	if err != nil {
		s.frontend.KeepForm(w, r, s.key, in, nil)
		frontend.Back(w, r, s.key, ResultInvalid)

		return
	}

	submission, err := s.Create(ctx, proposal)
	if err != nil {
		s.discard(ctx, proposal.MediaID)
		s.frontend.Fail(w, r, fmt.Errorf("save submission: %w", err))

		return
	}

	s.received(r, submission)

	frontend.Back(w, r, s.key, ResultSent)
}

// received sends the confirmation and the editors' notification.
func (s *Service) received(r *http.Request, submission Submission) {
	ctx := r.Context()

	settings, err := s.Settings(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "submission settings", "module", s.key, "error", err)

		return
	}

	view := MailView{
		Submission: submission,
		AdminURL:   s.absolute(r, s.adminURL(admin.RouteName(s.key, "show"), "id", submission.ID)),
	}

	if submission.MediaID != "" && s.media != nil {
		view.ImageURL = s.absolute(r, s.media.URL(ctx, submission.MediaID))
	}

	s.tellSubmitter(ctx, settings, submission, mailReceived)
	s.notifyEditors(ctx, settings, view)
}

// upload stores the visitor's image in the media library and returns its ID,
// or "" without an image. Only JPEG, PNG and GIF up to maxImage are
// accepted, by their content: the type the browser names was only checked
// by the form.
func (s *Service) upload(ctx context.Context, header *multipart.FileHeader) (string, error) {
	if header == nil {
		return "", nil
	}

	file, err := header.Open()
	if err != nil {
		return "", err
	}

	defer func() { _ = file.Close() }()

	data, err := read(file, header)
	if err != nil {
		return "", err
	}

	if !slices.Contains(imageTypes, http.DetectContentType(data)) {
		return "", ErrInvalid
	}

	if s.media == nil {
		return "", ErrInvalid
	}

	asset, err := s.media.Upload(ctx, header.Filename, bytes.NewReader(data))
	if err != nil {
		return "", err
	}

	return asset.ID, nil
}

// read returns the uploaded file's bytes, refusing empty and too large files.
func read(file multipart.File, header *multipart.FileHeader) ([]byte, error) {
	if header.Size > maxImage {
		return nil, ErrInvalid
	}

	data, err := io.ReadAll(io.LimitReader(file, maxImage+1))
	if err != nil {
		return nil, err
	}

	if len(data) == 0 || len(data) > maxImage {
		return nil, ErrInvalid
	}

	return data, nil
}

// discard removes an image uploaded for a proposal that could not be stored.
func (s *Service) discard(ctx context.Context, id string) {
	if id == "" || s.media == nil {
		return
	}

	err := s.media.Delete(context.WithoutCancel(ctx), id)
	if err != nil && !errors.Is(err, media.ErrNotFound) {
		slog.WarnContext(ctx, "discard submission image", "module", s.key, "error", err)
	}
}

// handler shows the form on the page the module is placed on.
func (s *Service) handler() http.Handler {
	// Paths without a route get the site's 404 page.
	routes := s.frontend.Router()

	routes.Get("/{$}", s.formPage).As("form")

	return routes
}

func (s *Service) formPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	mount := frontend.MountFrom(ctx)
	view := FormView{
		Key:        s.key,
		Action:     s.submitURL(),
		Result:     frontend.Result(r, s.key, results...),
		MaxImageMB: maxImage >> 20,
		Title:      mount.Title,
		Form:       s.proposalForm(ctx),
	}

	s.frontend.Render(w, r, frontend.Content{
		Template: "form",
		Title:    view.Title,
		Data:     view,
	})
}

// results are the results the form page shows; see frontend.Result.
var results = []string{ResultSent, ResultInvalid, ResultThrottled}
