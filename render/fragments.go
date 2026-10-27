package render

import (
	"io/fs"

	partial "github.com/donseba/go-partial"
	"github.com/donseba/go-partial/exp/slots"
)

// Fragment is a part of a page that HTMX can ask for on its own, by the ID of
// its element, such as a comment thread that a form updates or a list that
// loads when it scrolls into view. On a full page the layout or a content
// template places it with {{slot "ID"}}, or with {{requiredSlot "ID"}},
// which fails when the page has no such fragment; a request
// whose target is the ID gets only the fragment, rendered from the same
// template with the same dot, or with the regions out of band for a view
// with FragmentRegions. The template's outer element carries the ID.
//
// Fragments are go-partial exp/slots slots that answer requests on their
// own: a request for a fragment is answered with the fragment alone when the
// page has what the fragment declares (slots.CheckContracts): data of its
// @dot type, and a value for every @model among the view's models. A page
// without, such as the one a form's action returned to after the form's own
// page, answers with its content instead, as a navigation (see
// Renderer.Render).
type Fragment struct {
	ID       string
	Template string
	// Data is the fragment's dot, on the page and on its own.
	Data any
	// Dot, when set, replaces Data, such as a dot computed only when the
	// fragment renders (partial.DotFuncOf): only a request for the fragment
	// pays for its data. A Dot of a known type is checked against the
	// fragment's @dot before it is computed.
	Dot partial.Dot
	// Deferred fragments load after the page, such as a panel whose data is
	// expensive: on the page, {{slot "ID"}} places a placeholder that asks
	// for the fragment from the page's own address as soon as the page
	// loads (go-partial's slots.Deferred), so the page itself never
	// computes the fragment's dot. The default placeholder is an empty
	// element with the fragment's ID. Placeholder, a template of the view's
	// file system, defers the fragment with a placeholder of the site's own,
	// whose dot is a go-partial slots.PlaceholderData and whose outer
	// element carries its ID and Attrs; it has the view's includes.
	Deferred    bool
	Placeholder string
	// PlaceholderData is the placeholder's PlaceholderData.Data, such as
	// the heading of a panel, so one placeholder template serves several
	// fragments (go-partial's slots.Data).
	PlaceholderData any
}

// options are the go-partial slot options of the fragment, with its
// placeholder from files or, without, the renderer's file system; alone
// answers a request for it without the regions (see View.FragmentRegions).
func (f Fragment) options(files fs.FS, alone bool) []slots.Option {
	options := []slots.Option{slots.CheckContracts()}
	if alone {
		options = append(options, slots.OmitOOB())
	}

	switch {
	case f.Placeholder != "":
		placeholder := partial.NewID(f.ID+"-placeholder", f.Placeholder)
		if files != nil {
			placeholder.SetFileSystem(files)
		}

		options = append(options, slots.Placeholder(placeholder))
	case f.Deferred:
		options = append(options, slots.Deferred())
	}

	if f.PlaceholderData != nil {
		options = append(options, slots.Data(f.PlaceholderData))
	}

	return options
}
