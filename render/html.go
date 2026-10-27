package render

import (
	"context"
	"html/template"
	"net/http"

	partial "github.com/donseba/go-partial"
	"github.com/donseba/go-partial/exp/flash"
)

// RenderPartial renders p on its own, as a part of the layout without the
// layout: with the renderer's functions, stages and diagnostics, its failure
// policy, and models for @model. Use it for parts such as components and
// the HTML part of e-mail (RenderText writes the text part); req may be nil
// outside a request. During a page render, pass the context the template
// function received, so the part shares the page's bound functions and a
// failing part fails the page.
//
// p is made a part of the renderer (go-partial's SetParent), with its
// template cache (see SetReload), and given the models, so it is meant for
// one render: build a new one for the next.
func (r *Renderer) RenderPartial(
	ctx context.Context,
	req *http.Request,
	p *partial.Partial,
	models ...any,
) (template.HTML, error) {
	p.SetParent(r.root).SetModel(models...)

	return partial.RenderSelf(ctx, req, p)
}

// RenderText renders p as plain text, unescaped, with go-partial's
// RenderText, as RenderPartial renders HTML: with the renderer's functions,
// stages and template cache, and models for @model. Use it for text that is
// not HTML, such as the subject and the text part of an e-mail.
func (r *Renderer) RenderText(ctx context.Context, p *partial.Partial, models ...any) (string, error) {
	p.SetParent(r.root).SetModel(models...)

	return partial.RenderText(ctx, p)
}

// HasEntry reports whether the template of p defines the template p renders
// (go-partial's SetEntry and HasEntry), such as the "html" part of an
// e-mail, without rendering it: from the scan go-partial keeps with the
// renderer's template cache (see SetReload). p is made a part of the
// renderer, as RenderPartial does.
func (r *Renderer) HasEntry(p *partial.Partial) (bool, error) {
	return p.SetParent(r.root).HasEntry()
}

// Respond answers a request that is refused rather than failed to render,
// such as one without permission, with messages and status, never cached
// (go-partial's flash.Respond): an HTMX request gets them as flash messages
// added to the page out of band, with the template and target of the
// renderer's exp/flash stage (see Use), leaving its own target as it is;
// any other request gets their texts as text/plain.
func (r *Renderer) Respond(w http.ResponseWriter, req *http.Request, status int, messages ...flash.Message) error {
	return flash.Respond(w, req, r.root, status, messages...)
}
