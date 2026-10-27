package render

import (
	"errors"
	"log/slog"
	"net/http"

	partial "github.com/donseba/go-partial"
	"github.com/donseba/go-partial/ext/debug"
	partialerrors "github.com/donseba/go-partial/ext/errors"
	"github.com/donseba/go-partial/ext/logger"
)

// Diagnostics configures what a renderer reports about its renders.
type Diagnostics struct {
	// Development shows a failing template in detail, with the template and
	// line, on go-partial's ext/errors error page, and makes
	// {{debug .}} show a value (ext/debug). Never in production: there a
	// failing render writes nothing and its caller answers a plain error,
	// unless the renderer has error templates (ErrorPage, ErrorFragment).
	Development bool
	// ErrorPage is a template that answers, outside development, a request
	// whose page failed, such as a theme's 500.gohtml, with status 500 and
	// never cached (go-partial's ext/errors WithTemplate). It renders on its
	// own, without the layout, as a child of the part that failed, with its
	// functions, the view's includes and models; its dot is an ext/errors
	// Data, whose Error it must not show (Data.Detailed is false; its
	// Message is a safe sentence, see ErrorMessage). ErrorFragment is the
	// same for a partial request, such as an HTMX navigation, in the place
	// of the part requested. When only one of them is set, it answers both.
	// When an error template fails itself, go-partial's own error markup
	// answers, without the error's details.
	ErrorPage     string
	ErrorFragment string
	// ErrorFlash, when set, answers a partial request that failed to render,
	// outside development, with an error message of the renderer's exp/flash
	// stage (see Renderer.Use) instead of ErrorFragment: added to the flash
	// target out of band, with the text ErrorFlash returns for the request,
	// leaving the request's own target as it is (go-partial's ext/errors
	// WithFlash). The response keeps status 500, so with HTMX 4 the
	// page lets 5xx responses swap their out-of-band parts only (the
	// clientAttrs "keepOnServerError" setting).
	ErrorFlash func(r *http.Request) string
	// ErrorMessage, when set, returns the text of the safe sentence
	// message (go-partial's ext/errors SafeMessage) for the request, such as
	// its translation in the request's language: the Message of the error
	// templates' Data, also the text of the error flash message when
	// ErrorFlash returns none (go-partial's ext/errors WithMessage). An
	// empty text keeps the sentence.
	ErrorMessage func(r *http.Request, message string) string
	// ErrorTitle, when set, returns the text of the title, the status text
	// of the error (http.StatusText), for the request, such as its
	// translation in the request's language: the heading of go-partial's
	// built-in error page, and the Title of the error templates' Data
	// (go-partial's ext/errors WithTitle). An empty text keeps the status
	// text.
	ErrorTitle func(r *http.Request, title string) string
	// ErrorLanguage, when set, returns the language of the error page for
	// the request, such as the language of the page that failed: the lang
	// attribute of go-partial's built-in error page, and the Lang of the
	// error templates' Data (go-partial's ext/errors WithLang).
	ErrorLanguage func(r *http.Request) string
	// Logger receives go-partial's warnings, such as a function it ignored;
	// nil uses slog.Default at the time of each warning. Errors are returned
	// by Render instead.
	Logger *slog.Logger
	// Stages run in every render, around the templates of the layout, the
	// content and every region, for example go-partial's exp/metrics.
	Stages []partial.RenderStage
	// Events receive go-partial's diagnostic events, for example exp/otel's
	// sink, which adds them to the request's trace span.
	Events []partial.EventSink
}

// ErrShown wraps the error Render returns after an error stage answered the
// request, such as the development error page (see Diagnostics.Development)
// or an error template (Diagnostics.ErrorPage), with status 500 and never
// cached: callers log it and write nothing more.
var ErrShown = errors.New("render error answered")

// SetDiagnostics sets what the renderer reports. Call it once, before the
// first render.
func (r *Renderer) SetDiagnostics(diagnostics Diagnostics) {
	// Errors are left out of the log: Render returns them, and whoever
	// answers the request logs them once.
	warnings := logger.Sink(diagnostics.Logger, logger.WithLevels(partial.EventWarn))

	r.root.SetEvents(partial.FanoutEvents(append([]partial.EventSink{warnings}, diagnostics.Events...)...))
	r.root.Use(diagnostics.Stages...)

	if !diagnostics.Development {
		r.useErrorTemplates(diagnostics)

		return
	}

	r.failure = nil

	r.root.Use(partialerrors.Stage(partialerrors.WithMode(partialerrors.ModeDetailed)))

	// {{debug .}} renders through ext/debug in the partial being rendered,
	// unless the site has a debug function of its own, which keeps working.
	r.root.Use(debug.Stage(debug.WithFunc(debug.UnlessDefined())))
}

// FailureStages are the stages a partial below the renderer's root adds
// for an error that happened outside a render, answered with go-partial's
// ext/errors Respond, such as a visitor's post that failed to save, or a
// render that failed without an error template: outside development, for
// a renderer without error templates, go-partial's built-in error page
// without the error's details, with the texts of Diagnostics.ErrorMessage
// and ErrorTitle and the language of Diagnostics.ErrorLanguage. A renderer
// with error templates, or in development, answers with its own error
// stage, and FailureStages is empty. Failing renders keep their answer: without error
// templates nothing is written, and the caller answers.
func (r *Renderer) FailureStages() []partial.RenderStage {
	return r.failure
}

// useErrorTemplates answers failing renders with the error templates and
// the error flash message, when there are any, without the error's
// details. go-partial's ext/errors lets one error template answer for both.
// Without any, errors outside a render get the built-in error page (see
// FailureStages).
func (r *Renderer) useErrorTemplates(diagnostics Diagnostics) {
	options := []partialerrors.Option{
		partialerrors.WithMode(partialerrors.ModeSafe),
		partialerrors.WithTemplate(diagnostics.ErrorPage),
		partialerrors.WithFragmentTemplate(diagnostics.ErrorFragment),
	}

	if language := diagnostics.ErrorLanguage; language != nil {
		options = append(options, partialerrors.WithLang(func(ctx *partial.RenderContext, _ partialerrors.Data) string {
			// Without a request, the page is in English.
			if ctx.Request == nil {
				return ""
			}

			return language(ctx.Request)
		}))
	}

	options = withErrorMessage(options, diagnostics)
	options = withErrorTitle(options, diagnostics)

	if diagnostics.ErrorPage == "" && diagnostics.ErrorFragment == "" && diagnostics.ErrorFlash == nil {
		r.failure = []partial.RenderStage{partialerrors.Stage(options...)}

		return
	}

	r.failure = nil

	if message := diagnostics.ErrorFlash; message != nil {
		options = append(options, partialerrors.WithFlash(func(ctx *partial.RenderContext, _ partialerrors.Data) string {
			// Only partial requests are answered with a message; without a
			// request, the safe message of ext/errors stands.
			if ctx.Request == nil {
				return ""
			}

			return message(ctx.Request)
		}))
	}

	r.root.Use(partialerrors.Stage(options...))
}

// withErrorMessage is options with the text of Diagnostics.ErrorMessage.
func withErrorMessage(options []partialerrors.Option, diagnostics Diagnostics) []partialerrors.Option {
	message := diagnostics.ErrorMessage
	if message == nil {
		return options
	}

	return append(options, partialerrors.WithMessage(func(ctx *partial.RenderContext, data partialerrors.Data) string {
		// Without a request, the safe message of ext/errors stands.
		if ctx.Request == nil {
			return ""
		}

		return message(ctx.Request, data.Message)
	}))
}

// withErrorTitle is options with the text of Diagnostics.ErrorTitle.
func withErrorTitle(options []partialerrors.Option, diagnostics Diagnostics) []partialerrors.Option {
	title := diagnostics.ErrorTitle
	if title == nil {
		return options
	}

	return append(options, partialerrors.WithTitle(func(ctx *partial.RenderContext, data partialerrors.Data) string {
		// Without a request, the status text of ext/errors stands.
		if ctx.Request == nil {
			return ""
		}

		return title(ctx.Request, data.Title)
	}))
}
